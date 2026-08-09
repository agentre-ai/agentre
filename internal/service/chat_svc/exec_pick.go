package chat_svc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/utils/httputils"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_location_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_repo"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
)

// ExecTargetChoice 是 PickExecTarget 选中的那一档：目标行本身与它解析出的 backend。
type ExecTargetChoice struct {
	Target  *agent_entity.AgentExecTarget
	Backend *agent_backend_entity.AgentBackend
}

// ExecTargetUnavailable 记录 PickExecTarget 遍历某个 Agent 的执行目标列表时，某一档为
// 什么被跳过 —— 全部不可用时用来逐档报告原因（R15）。
type ExecTargetUnavailable struct {
	AgentBackendID int64
	DeviceID       string // 空 = 本机档
	Reason         BlockReason
	Hint           string
}

// ExecTargetNoneAvailableError 在一个 Agent 的执行目标列表非空、但逐档判定全部不可用时
// 由 PickExecTarget 返回。Reasons 按列表顺序给出每一档的原因，供调用方结构化消费；
// Error() 把同一份信息渲染成文本，是 Wails 边界唯一透给前端的通道（只过 Error() 字符串）。
type ExecTargetNoneAvailableError struct {
	httpErr *httputils.Error
	Reasons []ExecTargetUnavailable
}

func (e *ExecTargetNoneAvailableError) Error() string {
	lines := make([]string, 0, len(e.Reasons)+1)
	lines = append(lines, e.httpErr.Msg)
	for i, r := range e.Reasons {
		label := "本机"
		if r.DeviceID != "" {
			label = "设备 " + r.DeviceID
		}
		lines = append(lines, fmt.Sprintf("%d. backend #%d（%s）：%s", i+1, r.AgentBackendID, label, r.Hint))
	}
	return strings.Join(lines, "\n")
}

func (e *ExecTargetNoneAvailableError) As(target any) bool {
	if p, ok := target.(**httputils.Error); ok {
		*p = e.httpErr
		return true
	}
	return false
}

// PickExecTarget 见 ChatSvc 接口注释。
func (s *chatSvc) PickExecTarget(ctx context.Context, agentID int64, projectID int64) (*ExecTargetChoice, error) {
	targets, err := agent_repo.AgentExecTarget().ListByAgent(ctx, agentID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err, zap.Int64("agentId", agentID))
	}
	if len(targets) == 0 {
		return nil, i18n.NewError(ctx, code.ChatAgentNoBackend)
	}

	unavailable := make([]ExecTargetUnavailable, 0, len(targets))
	for _, target := range targets {
		be, err := agent_backend_repo.AgentBackend().Find(ctx, target.AgentBackendID)
		if err != nil {
			return nil, operationFailedWithCause(ctx, err, zap.Int64("agentBackendId", target.AgentBackendID))
		}
		reason, hint, err := s.evalExecTargetAvailability(ctx, be, projectID)
		if err != nil {
			return nil, err
		}
		if reason == "" {
			return &ExecTargetChoice{Target: target, Backend: be}, nil
		}
		deviceID := ""
		if be != nil {
			deviceID = be.DeviceID
		}
		unavailable = append(unavailable, ExecTargetUnavailable{
			AgentBackendID: target.AgentBackendID,
			DeviceID:       deviceID,
			Reason:         reason,
			Hint:           hint,
		})
	}
	return nil, &ExecTargetNoneAvailableError{
		httpErr: &httputils.Error{
			Status: http.StatusBadRequest,
			Code:   code.ChatAgentNoAvailableExecTarget,
			Msg:    i18n.T(ctx, code.ChatAgentNoAvailableExecTarget),
		},
		Reasons: unavailable,
	}
}

// evalExecTargetAvailability 判一档执行目标是否可用（R15）：
//  1. be 为 nil（目标行引用的 backend 已不存在，仅可能出现在 task 7 之前的软删场景）
//     视同没绑后端。
//  2. 远端档：本机没配对该指纹指向的这台 agentred（判据是本地配对表里有没有这一行，
//     不是有没有配对令牌，R2b），或已配对但不在线 —— 不可用。
//  3. backend 自身是否可用：复用既有 BlockReason 判据（blockReasonForBackend），
//     本地远端一视同仁 —— 远端的供应商类原因（RemoteProviderMissing / OpenClaw）已经
//     在那套判据里。
//  4. projectID > 0 时，这一档所在机器上要配了这个项目的路径（决策 34）：本机档看
//     projects.local_path_missing，agentred 档看 project_locations 里 device_id 缓存
//     列命中的那一行。projectID <= 0（自由会话）不受这条约束。
func (s *chatSvc) evalExecTargetAvailability(
	ctx context.Context, be *agent_backend_entity.AgentBackend, projectID int64,
) (BlockReason, string, error) {
	if be == nil {
		return BlockReasonNoBackend, "该执行目标引用的后端已不存在", nil
	}
	if be.IsRemote() {
		reason, hint, err := s.evalRemoteDeviceAvailability(ctx, be)
		if err != nil {
			return "", "", err
		}
		if reason != "" {
			return reason, hint, nil
		}
	}

	prov, err := lookupProviderForBackend(ctx, be)
	if err != nil {
		return "", "", operationFailedWithCause(ctx, err, zap.Int64("agentBackendId", be.ID))
	}
	gatewayRunning := s.gateway != nil && s.gateway.Status().State == "running"
	if chattable, reason, hint := blockReasonForBackend(be, prov, gatewayRunning); !chattable {
		return reason, hint, nil
	}

	if projectID > 0 {
		reason, hint, err := s.evalExecTargetProjectPath(ctx, be, projectID)
		if err != nil {
			return "", "", err
		}
		if reason != "" {
			return reason, hint, nil
		}
	}
	return "", "", nil
}

// evalRemoteDeviceAvailability 见 evalExecTargetAvailability 步骤 2。与 ListAgents 里
// deviceViews 的取法一致：Get 出错（含未配对时的 not-found）一律当未配对处理，不当成
// 真失败中断挑选 —— 未配对本就是这一档的正常状态之一（R2b），不是异常。
func (s *chatSvc) evalRemoteDeviceAvailability(ctx context.Context, be *agent_backend_entity.AgentBackend) (BlockReason, string, error) {
	deviceID, ok := be.DeviceIDInt()
	if !ok {
		return BlockReasonExecTargetUnpaired, "本机未配对这台 agentred", nil
	}
	rds := remote_device_svc.Default()
	if rds == nil {
		return BlockReasonExecTargetUnpaired, "本机未配对这台 agentred", nil
	}
	dv, derr := rds.Get(ctx, deviceID)
	if derr != nil || dv == nil {
		// derr 不是本函数的错误返回值要透出的失败——它只说明这台设备在本机配对表里
		// 查不到（未配对本身就是这一档的正常状态之一，R2b），与 ListAgents 里
		// deviceViews 的取法一致：错误一律折叠成「未配对」，不当异常中断挑选。
		return BlockReasonExecTargetUnpaired, "本机未配对这台 agentred", nil //nolint:nilerr // 见上方注释
	}
	if !dv.Online {
		return BlockReasonExecTargetOffline, "这台 agentred 当前离线", nil
	}
	return "", "", nil
}

// evalExecTargetProjectPath 见 evalExecTargetAvailability 步骤 4。
func (s *chatSvc) evalExecTargetProjectPath(ctx context.Context, be *agent_backend_entity.AgentBackend, projectID int64) (BlockReason, string, error) {
	if be.IsLocal() {
		p, err := project_repo.Project().Find(ctx, projectID)
		if err != nil {
			return "", "", operationFailedWithCause(ctx, err, zap.Int64("projectId", projectID))
		}
		if p == nil || p.LocalPathMissing {
			return BlockReasonExecTargetProjectPathMissing, "本机没有配置这个项目的路径", nil
		}
		return "", "", nil
	}
	_, err := project_location_repo.ProjectLocation().FindByProjectAndDevice(ctx, projectID, be.DeviceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return BlockReasonExecTargetProjectPathMissing, "这台机器上没有配置这个项目的路径", nil
		}
		return "", "", operationFailedWithCause(ctx, err, zap.Int64("projectId", projectID))
	}
	return "", "", nil
}

// blockReasonForBackend 是「一个 backend 自身是否可对话」的单一判定点，ListAgents（Agent
// 列表的 chattable/blockReason 展示）与 PickExecTarget（R15 逐档挑选）共用 —— 避免两处
// 各写一份、慢慢漂移。prov 是 be.LLMProviderKey 对应的供应商（找不到传 nil）；
// gatewayRunning 只影响本地 CLI 类后端。
//
// 空 LLMProviderKey（CLI 走自身 login 态）在 ClaudeCode/Codex/PiAgent 分支第一条就短路
// 判可用，不做任何可达性预探测——这是既有行为（迁移前的 chat.go:391），必须原样保留。
func blockReasonForBackend(
	be *agent_backend_entity.AgentBackend, prov *llm_provider_entity.LLMProvider, gatewayRunning bool,
) (chattable bool, reason BlockReason, hint string) {
	switch agent_backend_entity.BackendType(be.Type) {
	case agent_backend_entity.TypeBuiltin:
		switch {
		case prov != nil && prov.IsActive():
			return true, "", ""
		case prov == nil:
			// 内置后端没绑 / 找不到绑定的供应商。
			return false, BlockReasonBackendRequiresProvider, "请先在设置 → LLM 供应商激活该 Agent 后端关联的供应商"
		default:
			// 后端绑的供应商存在但未激活/缺 Key。
			return false, BlockReasonProviderInactive, "请先在设置 → LLM 供应商激活该 Agent 后端关联的供应商"
		}
	case agent_backend_entity.TypeClaudeCode, agent_backend_entity.TypeCodex, agent_backend_entity.TypePiAgent:
		if be.LLMProviderKey == "" {
			// 走 CLI 自身 login；这里不做可达性探测，启动失败由 chat turn 兜底报错。
			return true, "", ""
		}
		if prov == nil {
			return false, BlockReasonBackendRequiresProvider, "请先在设置 → LLM 供应商激活该 Agent 后端关联的供应商"
		}
		if !prov.IsActive() {
			return false, BlockReasonProviderInactive, "请先在设置 → LLM 供应商激活该 Agent 后端关联的供应商"
		}
		if kind := be.Kind(); kind == nil || !kind.ProviderTypeMatch(llm_provider_entity.ProviderType(prov.Type)) {
			// 与 resolveAgentBackend 保持一致：激活但类型不匹配的 provider
			// 仍不能启动该 CLI backend，不能继续误报为 gateway 缺失。
			return false, BlockReasonBackendRequiresProvider, "请先在设置 → LLM 供应商选择与该 Agent 后端匹配的类型"
		}
		if remoteProviderKnownMissing(be) {
			return false, BlockReasonRemoteProviderMissing, "远端 agentred 未配置该供应商，请前往「远端设备」页在对应设备上添加并填写 API Key"
		}
		if be.IsRemote() {
			return true, "", ""
		}
		if !gatewayRunning {
			return false, BlockReasonGatewayNotRunning, "本地网关未启动，CLI 后端暂不可用"
		}
		return true, "", ""
	case agent_backend_entity.TypeOpenClaw:
		if be.IsRemote() {
			return false, BlockReasonRemoteOpenClawUnavailable, "远端 OpenClaw 暂不可用：agentred 尚无安全的 secret enrollment/reference"
		}
		return true, "", ""
	default:
		return false, BlockReasonUnknownBackend, "未知 Agent 后端类型"
	}
}

// lookupProviderForBackend 取 backend 绑的 provider；空 LLMProviderKey（CLI 登录态）
// 一律不查——不做任何预探测，直接把 nil 交给 blockReasonForBackend 的 CLI 分支，那里
// 的第一条判断本就是 LLMProviderKey == "" → 可用（这是既有行为，chat.go:391 一样）。
func lookupProviderForBackend(ctx context.Context, be *agent_backend_entity.AgentBackend) (*llm_provider_entity.LLMProvider, error) {
	if be == nil || be.LLMProviderKey == "" {
		return nil, nil
	}
	return llm_provider_repo.LLMProvider().FindByKey(ctx, be.LLMProviderKey)
}
