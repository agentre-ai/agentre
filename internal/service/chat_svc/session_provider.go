package chat_svc

import (
	"context"
	"strings"

	"github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo"
)

// 会话级 LLM 供应商：切换入口 + 有效供应商解析的唯一口径。
// 规格：docs/specs/2026-08-10-session-provider-switch.md

// effectiveProviderKey 是「这条会话下一轮用哪个供应商」的**唯一**解析口（spec
// 「有效供应商解析（唯一口径）」）：会话级 provider_key 非空取它，否则取 agent 绑定；
// 两者皆空表示无供应商，即 CLI 自身登录态。
//
// 任何消费点（turn 解析、网关 token、CLI env/config 门控、goal 入口、LoadSession 展示、
// 复制启动命令、远端 wire）都必须走这里，不得再各自直接读 be.LLMProviderKey —— 各读各的
// 正是「会话选了供应商却打到 agent 绑定那家」的成因。goal 尤其不能漏：它与 turn 共用同
// 一个 CLI 会话池，两边解析不一致会让启动期比对键（决策 4）反复翻转、把在用的子进程
// evict 掉重 spawn。
//
// 已知残留（本地已收口，远端未）：远端 goal 的 wire.GoalParams 不带 provider key，daemon
// 的 hydrateGoalProvider 仍按 agent 绑定自解 —— 补齐需要给该 RPC 加 wire 字段（与
// RunParams.LLMProviderKey 同形），属协议改动。
func effectiveProviderKey(sess *chat_entity.Session, be *agent_backend_entity.AgentBackend) string {
	var sessKey, agentKey string
	if sess != nil {
		sessKey = sess.ProviderKey
	}
	if be != nil {
		agentKey = be.LLMProviderKey
	}
	return firstNonEmpty(sessKey, agentKey)
}

// providerKeyOf 是「本轮解析出来的这家供应商的 key」，nil（CLI 自身登录态，没有任何
// 供应商）返回空串。turn 侧把它交给网关 token 用：解析后的 prov 才是真正会被请求打到的
// 那家（会话所选缺失/停用时已回退过），拿 effectiveProviderKey 的原始值反而会让回退的
// 那一轮在网关上 502。
func providerKeyOf(prov *llm_provider_entity.LLMProvider) string {
	if prov == nil {
		return ""
	}
	return prov.ProviderKey
}

// resolveEffectiveProvider 把 effectiveProviderKey 解析成 provider 实体，供**展示侧**
// 消费点（LoadSession 的供应商类型/上下文窗口、复制启动命令）使用。
//
// 会话所选供应商缺失/停用/与后端 kind 不兼容时回落 agent 绑定 —— 与 turn 侧
// sessionProviderOverride 同一段代码、同一套回退语义，展示因此不会与真正会执行的那家
// 供应商说两套话（回退 notice 只在真正跑轮时产出，展示侧丢弃）。
//
// 返回的 error 只来自 agent 绑定那一次查询：调用方（复制启动命令）沿用既有的「查询
// 失败即报错」；会话所选 key 的查询失败按回退处理，不把展示整块打挂。
func (s *chatSvc) resolveEffectiveProvider(
	ctx context.Context,
	sess *chat_entity.Session,
	be *agent_backend_entity.AgentBackend,
) (*llm_provider_entity.LLMProvider, error) {
	if be == nil {
		return nil, nil
	}
	var base *llm_provider_entity.LLMProvider
	if be.LLMProviderKey != "" {
		var err error
		base, err = llm_provider_repo.LLMProvider().FindByKey(ctx, be.LLMProviderKey)
		if err != nil {
			return nil, err
		}
	}
	if sess == nil || strings.TrimSpace(sess.ProviderKey) == "" {
		return base, nil
	}
	prov, _ := s.sessionProviderOverride(ctx, be, sess.ProviderKey, base)
	return prov, nil
}

// SetChatSessionProvider 切换已有会话的 LLM 供应商（spec 决策 1）。
//
// 只写 chat_sessions.provider_key 一列：agent / backend / cli_path / reasoning_effort /
// permission mode / cwd / 钉住的执行目标一律不动（硬不变量 2），也不写回 agent 绑定。
// 允许在轮中调用：本轮已 spawn 的子进程不受影响，新供应商自下一轮生效（决策 8），
// 所以这里不加 turn 锁、也不 evict 任何东西。
//
// 校验复用新建会话那一套（存在 / IsActive / ProviderTypeMatch，决策 11）：不通过一律
// 拒绝写库并原样报错，会话保持原供应商 —— 不产生「写进去了但下一轮必然失败」的会话。
//
// 错误码：
//   - SessionID <= 0 → InvalidParameter
//   - 会话不存在 → ChatSessionNotFound
//   - agent/backend 解析不出来 → AgentNotFound / ChatAgentNoBackend
//   - 所选供应商缺失/停用/与后端 kind 不兼容 → ChatAgentNotChattable
func (s *chatSvc) SetChatSessionProvider(ctx context.Context, req *SetSessionProviderRequest) (*SetSessionProviderResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	key := strings.TrimSpace(req.ProviderKey)

	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err, zap.Int64("sessionId", req.SessionID))
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	be, err := sessionProviderBackend(ctx, sess)
	if err != nil {
		return nil, err
	}
	// 选中的就是当前生效的那一项（弹层里点已选中的行）：什么都没变，不写库也不追加
	// notice —— 否则每点一次就往 transcript 里塞一条「已改用 X」。
	if key == sess.ProviderKey {
		return &SetSessionProviderResponse{ProviderKey: key, AgentProviderKey: be.LLMProviderKey}, nil
	}
	// 决策 11：与 validateNewSessionProvider 同一口径（空串跳过校验 = 跟随 agent 绑定）。
	if _, err := s.validateNewSessionProvider(ctx, be, key); err != nil {
		return nil, err
	}
	if err := chat_repo.Session().UpdateProviderKey(ctx, sess.ID, key); err != nil {
		return nil, operationFailedWithCause(ctx, err, zap.Int64("sessionId", sess.ID))
	}
	sess.ProviderKey = key
	logger.Ctx(ctx).Info("chat_svc.SetChatSessionProvider: session provider switched",
		zap.Int64("sessionId", sess.ID),
		zap.String("providerKey", key),
		zap.String("agentProviderKey", be.LLMProviderKey),
		zap.String("backendType", be.Type))
	s.appendProviderSwitchNotice(ctx, sess, be, key)
	return &SetSessionProviderResponse{ProviderKey: key, AgentProviderKey: be.LLMProviderKey}, nil
}

// sessionProviderBackend 解析这条会话「下一轮落在哪一档」的 backend：钉住的那一档优先
// （R15b / 决策36），否则 Agent 的默认档 —— 与 GetLaunchCommand / LoadSession 展示取的
// 是同一档，切换校验因此对的是用户真正会跑的那个后端 kind。只读仓储，无副作用。
func sessionProviderBackend(ctx context.Context, sess *chat_entity.Session) (*agent_backend_entity.AgentBackend, error) {
	a, err := agent_repo.Agent().Find(ctx, sess.AgentID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err, zap.Int64("agentId", sess.AgentID))
	}
	if a == nil {
		return nil, i18n.NewError(ctx, code.AgentNotFound)
	}
	backendID := sessionBackendID(sess, a)
	if backendID <= 0 {
		return nil, i18n.NewError(ctx, code.ChatAgentNoBackend)
	}
	be, err := agent_backend_repo.AgentBackend().Find(ctx, backendID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err, zap.Int64("agentBackendId", backendID))
	}
	if be == nil {
		return nil, i18n.NewError(ctx, code.ChatAgentNoBackend)
	}
	return be, nil
}

// appendProviderSwitchNotice 在 transcript 追加一条持久 notice，标出「从这里起换了供应商」
// （决策 9）：两家供应商配同一个 model 时，逐条消息的 model 字段看不出分界。
// 负载与既有回退 notice 同构（结构化 JSON + 前端 t() 渲染，不把原始 JSON 泄漏给前端）。
//
// 落库失败只记日志：供应商切换本身已经成功落库，为了一条提示把整个切换报成失败，会让
// 用户以为没切成（实际下一轮已经换了）——这里的降级方向必须是「少一条提示」。
func (s *chatSvc) appendProviderSwitchNotice(
	ctx context.Context,
	sess *chat_entity.Session,
	be *agent_backend_entity.AgentBackend,
	providerKey string,
) {
	msg := &chat_entity.Message{
		SessionID:  sess.ID,
		DeviceID:   be.DeviceID,
		Role:       "assistant",
		BlocksJSON: "[]",
	}
	if err := msg.SetBlocks([]blocks.ContentBlock{blocks.NoticeBlock{
		Level: "info",
		Text:  encodeProviderSwitch(providerKey),
	}}); err != nil {
		logger.Ctx(ctx).Warn("chat_svc.appendProviderSwitchNotice: encode notice failed",
			zap.Int64("sessionId", sess.ID), zap.Error(err))
		return
	}
	seq, err := chat_repo.Message().NextSeq(ctx, sess.ID)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.appendProviderSwitchNotice: next seq failed",
			zap.Int64("sessionId", sess.ID), zap.Error(err))
		return
	}
	msg.Seq = seq
	if err := chat_repo.Message().Create(ctx, msg); err != nil {
		logger.Ctx(ctx).Warn("chat_svc.appendProviderSwitchNotice: persist notice failed",
			zap.Int64("sessionId", sess.ID), zap.Error(err))
	}
}
