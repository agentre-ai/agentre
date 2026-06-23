package group_svc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
)

// toolKeyGroupCreate 是 group_create 在 chat_svc 通用工具审批管线里的 ToolKey,
// 与 agenttool 注册表 / 审批卡 ToolKey 同值(引用 canonical 常量防漂移)。
const toolKeyGroupCreate = agenttool.KeyGroupCreate

// BuildCreateTurnMCP 实现 chat_svc.TurnMCPProvider:给普通单聊轮注入 group_create。
// 群成员轮(groupID>0)不注入 —— 防群中拉群套娃(spec §7.1);能力门控(CapMCPTools)
// 由 chat_svc.appendTurnMCP 统一处理,这里不重复判。per-agent 工具开关(group_create)
// 默认关、按需开,镜像 orgtool.BuildTurnMCP 的 a.ToolEnabled 门控。
func (s *groupSvc) BuildCreateTurnMCP(_ context.Context, a *agent_entity.Agent, sessionID, groupID int64) []agentruntime.MCPServerSpec {
	if a == nil || groupID > 0 || s.gatewayBaseURL == "" || !a.ToolEnabled(agenttool.KeyGroupCreate) {
		return nil
	}
	return []agentruntime.MCPServerSpec{{
		Name:    "group",
		URL:     s.gatewayBaseURL + "/mcp/group/",
		Headers: map[string]string{"Authorization": "Bearer " + s.mcp.MintCreateToken(a.ID, sessionID)},
		Tools:   []string{"group_create"},
	}}
}

// HandleGroupCreate 是 group_create MCP tool 的服务端入口:校验发起会话 → 审批门挂起 →
// 批准后建群(发起者=主持人,项目继承发起会话)+ system 拉起消息 + brief 作为首条群消息
// 投主持人触发首轮。返回写回 CLI 的 result 文本;拒绝/超时也是文本(nil err),镜像 orgtool ——
// error 仅用于内部故障/校验失败。
func (s *groupSvc) HandleGroupCreate(ctx context.Context, agentID, sessionID int64, title string, memberNames []string, brief string, workflowID int64, memberNicknames map[string]string) (string, error) {
	// 按 DB 现状校验发起会话(token 无状态,签发后会话可能已归档/换 agent)。
	sess, err := chat_repo.Session().Find(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if sess == nil || sess.Status != consts.ACTIVE || sess.AgentID != agentID {
		return "", i18n.NewError(ctx, code.GroupCreateSessionInvalid)
	}
	if sess.GroupID > 0 { // 群成员轮内禁止再拉群(防套娃);正常注入下走不到,防御伪造/复用 token 场景
		return "", i18n.NewError(ctx, code.GroupCreateNested)
	}
	// 实时复查 group_create 工具开关:create token 无状态,注入后用户可能在设置里关掉开关。
	// 权限门控先于输入校验(成员解析)—— 工具关了就直接拒,不暴露成员解析结果(镜像 org/workflow mcp.go 的实时复查)。
	a, err := agent_repo.Agent().Find(ctx, agentID)
	if err != nil {
		return "", err
	}
	if a == nil || !a.ToolEnabled(agenttool.KeyGroupCreate) {
		return "", i18n.NewError(ctx, code.GroupCreateToolDisabled)
	}
	memberIDs, err := s.resolveCreateMembers(ctx, agentID, memberNames)
	if err != nil {
		return "", err
	}

	// 审批门:挂起当前 MCP 调用直至用户决议/超时(走 chat_svc 通用工具审批管线,
	// ToolKey=group_create;waiter 与前端应答路由 AnswerToolApproval 由 chat_svc 持有)。
	requestID := uuid.NewString()
	blk := &chatblocks.ToolApprovalBlock{ToolKey: toolKeyGroupCreate, RequestID: requestID, ToolName: "group_create",
		ToolInput: map[string]any{"title": title, "memberNames": memberNames, "brief": brief, "workflowId": workflowID, "memberNicknames": memberNicknames}, Status: "pending"}
	ch, err := s.gw.BeginToolApproval(ctx, sessionID, blk)
	if err != nil {
		return "", fmt.Errorf("审批通道不可用: %w", err)
	}
	select {
	case allow := <-ch:
		if !allow {
			_ = s.gw.FinishToolApproval(ctx, sessionID, requestID, "denied", "")
			return "用户拒绝了此操作", nil
		}
	case <-time.After(s.approvalTimeout):
		_ = s.gw.FinishToolApproval(ctx, sessionID, requestID, "expired", "")
		return "审批超时，操作未执行", nil
	case <-ctx.Done():
		// 请求 ctx 已死,用 Background 调 Finish
		_ = s.gw.FinishToolApproval(context.Background(), sessionID, requestID, "expired", "")
		return "", ctx.Err()
	}

	detail, err := s.CreateGroup(ctx, &CreateGroupRequest{
		Title:          title,
		HostAgentID:    agentID,
		ProjectID:      sess.ProjectID, // 群目录 = 发起会话的项目目录
		MemberAgentIDs: memberIDs,
		WorkflowID:     workflowID, // 可选绑定的协作流程(0=不绑定;注入侧 IsActive 软门)
	})
	if err != nil {
		// 业务校验失败也算 approved 终态,错误进 Result 给 agent 纠错(镜像 orgtool)。
		_ = s.gw.FinishToolApproval(ctx, sessionID, requestID, "approved", "执行失败: "+err.Error())
		return "已批准但执行失败: " + err.Error(), nil //nolint:nilerr // 失败编码进返回文本给 agent,不作 RPC error(镜像 orgtool)
	}
	g := detail.Group
	// 群昵称(可选):建群后逐个落库(复用 SetMemberNickname 的唯一性校验),冲突/失败只 Warn。
	s.applyCreateNicknames(ctx, detail.Members, memberNicknames)
	// 群已落库即算成功,后续消息失败只 Warn 不回滚:agent 拿到 group id 后可在群内补发;
	// 回滚反而会让用户已批准的操作凭空消失。
	if _, perr := s.persistMessage(ctx, g, group_entity.SenderKindSystem, 0,
		"本群由 "+s.names(ctx, agentID)+" 自会话拉起", nil, false, 0, 0, ""); perr != nil {
		logger.Ctx(ctx).Warn("group_svc.HandleGroupCreate: system message persist failed", zap.Error(perr))
	}
	// brief 作为首条群消息投主持人(收件人为空默认主持人),触发其群内首轮。
	if serr := s.SendGroupMessage(ctx, &SendGroupMessageRequest{GroupID: g.ID, Text: brief}); serr != nil {
		logger.Ctx(ctx).Warn("group_svc.HandleGroupCreate: brief send failed", zap.Error(serr))
	}
	// 契约:前端 GroupCreateCard 按 "group created: id=<id> title=<title>" 解析渲染跳转卡
	// (同样进审批卡 result);改格式需同步 frontend/src/components/agentre/canonical-tool/group-create/。
	result := fmt.Sprintf("group created: id=%d title=%s", g.ID, g.Title)
	_ = s.gw.FinishToolApproval(ctx, sessionID, requestID, "approved", result)
	logger.Ctx(ctx).Info("group_svc.HandleGroupCreate: created",
		zap.Int64("groupID", g.ID), zap.Int64("hostAgentID", agentID), zap.Int64("sessionID", sessionID))
	return result, nil
}

// applyCreateNicknames 建群后把 group_create 带来的群昵称落到对应成员(best-effort)。
// nicknames 键=成员显示名(建群此刻=agent 全局名,尚无昵称);经 SetMemberNickname 复用唯一性
// 校验与事件推送,冲突/失败只 Warn 不阻断(群已建成,昵称属增益)。
func (s *groupSvc) applyCreateNicknames(ctx context.Context, members []*group_entity.GroupMember, nicknames map[string]string) {
	if len(nicknames) == 0 {
		return
	}
	for _, m := range members {
		nick := strings.TrimSpace(nicknames[s.names(ctx, m.AgentID)])
		if nick == "" {
			continue
		}
		if err := s.SetMemberNickname(ctx, m.ID, nick); err != nil {
			logger.Ctx(ctx).Warn("group_svc.HandleGroupCreate: apply nickname failed",
				zap.Int64("memberId", m.ID), zap.String("nickname", nick), zap.Error(err))
		}
	}
}

// resolveCreateMembers 把成员显示名解析成 agent id(池=全部 active agent,与 invite 同口径;
// 名字找不到 → 显式报错,不静默跳过 —— 自主建群必须让模型知道谁没拉到)。
func (s *groupSvc) resolveCreateMembers(ctx context.Context, hostAgentID int64, names []string) ([]int64, error) {
	pool, err := agent_repo.Agent().List(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]int64{}
	for _, a := range pool {
		if a.IsActive() {
			byName[a.Name] = a.ID
		}
	}
	out := make([]int64, 0, len(names))
	for _, n := range names {
		id, ok := byName[n]
		if !ok {
			return nil, i18n.NewError(ctx, code.GroupCreateMemberUnknown, n)
		}
		if id == hostAgentID {
			continue // 主持人无需自列,CreateGroup 也会跳过
		}
		out = append(out, id)
	}
	return out, nil
}

// SetApprovalTimeoutForTest 测试钩子:缩短 group_create 审批超时(仅测试使用)。
func SetApprovalTimeoutForTest(svc GroupSvc, d time.Duration) {
	if s, ok := svc.(*groupSvc); ok {
		s.approvalTimeout = d
	}
}
