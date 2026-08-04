package chat_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/i18n"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
)

// execDeviceID 返回 ! 命令应在哪台设备执行：remote 后端取其 DeviceID，本地为空串。
func execDeviceID(be *agent_backend_entity.AgentBackend) string {
	if be != nil && be.IsRemote() {
		return be.DeviceID
	}
	return ""
}

// resolveExecTarget 解析一个 session 值对象的当前执行目标。sess 可以是已持久化
// 会话，也可以是只带 AgentID/ProjectID 的未持久化预会话对象；该方法只读仓储。
func (s *chatSvc) resolveExecTarget(ctx context.Context, sess *chat_entity.Session) (*LocalCommandScope, error) {
	if sess == nil || sess.AgentID <= 0 || sess.ProjectID < 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	a, err := agent_repo.Agent().Find(ctx, sess.AgentID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err, zap.Int64("agentId", sess.AgentID))
	}
	if a == nil {
		return nil, i18n.NewError(ctx, code.AgentNotFound)
	}
	if a.AgentBackendID <= 0 {
		return nil, i18n.NewError(ctx, code.ChatAgentNoBackend)
	}
	be, err := agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err, zap.Int64("backendId", a.AgentBackendID))
	}
	if be == nil {
		return nil, i18n.NewError(ctx, code.AgentBackendNotFound)
	}
	cwd, err := resolveSessionCwd(ctx, sess, be)
	if err != nil {
		return nil, err
	}
	return &LocalCommandScope{DeviceID: execDeviceID(be), Cwd: cwd}, nil
}

// ResolveLocalCommandScope 为已有 session，或尚未创建 session 的 agent/project 目标
// 解析当前设备/cwd。预会话分支只构造值对象，不调用 Session.Create/Ensure。
func (s *chatSvc) ResolveLocalCommandScope(ctx context.Context, req *ResolveLocalCommandScopeRequest) (*LocalCommandScope, error) {
	sess, err := localCommandScopeSession(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.resolveExecTarget(ctx, sess)
}

func localCommandScopeSession(ctx context.Context, req *ResolveLocalCommandScopeRequest) (*chat_entity.Session, error) {
	if req == nil || req.SessionID < 0 || req.AgentID < 0 || req.ProjectID < 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	if req.SessionID > 0 {
		if req.AgentID != 0 || req.ProjectID != 0 {
			return nil, i18n.NewError(ctx, code.InvalidParameter)
		}
		sess, err := chat_repo.Session().Find(ctx, req.SessionID)
		if err != nil {
			return nil, operationFailedWithCause(ctx, err, zap.Int64("sessionId", req.SessionID))
		}
		if sess == nil {
			return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
		}
		return sess, nil
	}
	if req.AgentID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	return &chat_entity.Session{AgentID: req.AgentID, ProjectID: req.ProjectID}, nil
}
