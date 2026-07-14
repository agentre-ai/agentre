package chat_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/i18n"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
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

// ResolveSessionExecTarget 给定 sessionID，解析出 ! 命令的执行目标（cwd + deviceID）。
// 复用 resolveSessionCwd 的项目/自由会话/远端解析规则，绝不走库以外的旁路。
func (s *chatSvc) ResolveSessionExecTarget(ctx context.Context, sessionID int64) (string, string, error) {
	sess, err := chat_repo.Session().Find(ctx, sessionID)
	if err != nil {
		return "", "", operationFailedWithCause(ctx, err, zap.Int64("sessionId", sessionID))
	}
	if sess == nil {
		return "", "", i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	a, err := agent_repo.Agent().Find(ctx, sess.AgentID)
	if err != nil {
		return "", "", operationFailedWithCause(ctx, err, zap.Int64("agentId", sess.AgentID))
	}
	var be *agent_backend_entity.AgentBackend
	if a != nil && a.AgentBackendID > 0 {
		be, err = agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID)
		if err != nil {
			return "", "", operationFailedWithCause(ctx, err, zap.Int64("backendId", a.AgentBackendID))
		}
	}
	cwd, err := resolveSessionCwd(ctx, sess, be)
	if err != nil {
		return "", "", err
	}
	return cwd, execDeviceID(be), nil
}
