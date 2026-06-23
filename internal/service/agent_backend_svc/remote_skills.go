package agent_backend_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
)

// RemoteSkillDiscoverer 经 device 连接池调 daemon skills.list,枚举远端 daemon 本机已装
// 技能包,供 skill_svc 注入(结构化满足其 RemoteDiscoverer 端口)。daemon 不可达 / RPC
// 失败时软降级为空 —— 与本地 claudeskill 发现器一致(CLI 不可用→空发现),让技能配置
// 面板在远端离线时照常可用,只是不展 daemon 已装集。
type RemoteSkillDiscoverer struct{}

// NewRemoteSkillDiscoverer 构造远端技能发现器(bootstrap 注入 skill_svc)。
func NewRemoteSkillDiscoverer() *RemoteSkillDiscoverer { return &RemoteSkillDiscoverer{} }

// ListSkills 借 deviceID 的 daemon 连接调 skills.list,返 daemon 本机已装技能包。
func (*RemoteSkillDiscoverer) ListSkills(ctx context.Context, deviceID int64, backendType string) ([]agentskill.SkillPack, error) {
	rds := remote_device_svc.Default()
	if rds == nil || rds.Pool() == nil {
		return []agentskill.SkillPack{}, nil
	}
	lease, err := rds.Pool().Borrow(ctx, deviceID)
	if err != nil {
		logger.Ctx(ctx).Warn("agent_backend_svc.RemoteSkillDiscoverer.ListSkills: dial failed",
			zap.Int64("deviceID", deviceID), zap.Error(err))
		return []agentskill.SkillPack{}, nil
	}
	defer lease.Release()

	var resp handlers.SkillsListResult
	if err := lease.Client().Call(ctx, "skills.list", handlers.SkillsListParams{BackendType: backendType}, &resp); err != nil {
		logger.Ctx(ctx).Warn("agent_backend_svc.RemoteSkillDiscoverer.ListSkills: rpc failed",
			zap.Int64("deviceID", deviceID), zap.Error(err))
		return []agentskill.SkillPack{}, nil
	}
	if resp.Packs == nil {
		return []agentskill.SkillPack{}, nil
	}
	return resp.Packs, nil
}
