package chat_svc

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/pkg/i18n"
	"gorm.io/gorm"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/project_location_repo"
)

// CwdResolver session → cwd 解析器；project_svc 在启动时调 RegisterCwdResolver
// 注入；未注入时回退到 agentruntime.AgentCwd（保留 Agent 级老行为）。
//
// 引入回调而不是直接 import project_svc 是为了让 chat_svc 不依赖 project_svc
// 的具体实现 —— project_svc 已经依赖 chat_repo，反向再 import 会成环。
type CwdResolver func(ctx context.Context, session *chat_entity.Session) (string, error)

var resolveCwdFn CwdResolver

// RegisterCwdResolver 由 bootstrap 注入 project-aware 实现。
func RegisterCwdResolver(fn CwdResolver) { resolveCwdFn = fn }

// resolveSessionCwd 按 backend 走本地 / 远端两条路径：
//   - be 为 nil 或 be.IsLocal()：沿用 CwdResolver 回调（project.Path / AgentCwd fallback）。
//   - be.IsRemote() + ProjectID > 0：查 project_locations(project_id, device_id)
//     拿远端机器上的 path；找不到 → ProjectLocationMissing（前端引导用户去
//     Project Settings → Members 配置）。
//   - be.IsRemote() + ProjectID = 0（自由会话）：返回空 cwd，把兜底权下放给远端
//     daemon 端的 runtime —— claudecode/codex 在 cwd=="" 时会自己调 AgentCwd
//     落到远端机器的 <AppDataDir>/agents/<agent_id>/。
//
// session 为 nil 时返回 "". be 为 nil 视作本地，是为了 GetLaunchCommand 这种
// 还在拼命令的场景能复用 —— 那条路径目前还没引入 device 概念。
func resolveSessionCwd(ctx context.Context, sess *chat_entity.Session, be *agent_backend_entity.AgentBackend) (string, error) {
	if sess == nil {
		return "", nil
	}
	if be == nil || be.IsLocal() {
		if resolveCwdFn != nil {
			return resolveCwdFn(ctx, sess)
		}
		return agentruntime.AgentCwd(sess.AgentID)
	}
	if sess.ProjectID == 0 {
		return "", nil
	}
	repo := project_location_repo.ProjectLocation()
	if repo == nil {
		return "", errors.New("project_location_repo not registered")
	}
	loc, err := repo.FindByProjectAndDevice(ctx, sess.ProjectID, be.DeviceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", i18n.NewError(ctx, code.ProjectLocationMissing)
		}
		return "", err
	}
	return loc.Path, nil
}
