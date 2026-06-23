package skill_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
)

//go:generate mockgen -source deps.go -destination mock_skill_svc/mock_deps.go -package mock_skill_svc

type AgentLookup interface {
	Find(ctx context.Context, id int64) (*agent_entity.Agent, error)
}
type BackendLookup interface {
	Find(ctx context.Context, id int64) (*agent_backend_entity.AgentBackend, error)
}

// RemoteDiscoverer 枚举远端 device(daemon)本机已装技能包。远端 backend 的技能包在
// daemon 那台机器上,desktop 本地的 claude plugin list 看不到 —— 经此端口走 daemon
// skills.list RPC 发现。生产实现在 agent_backend_svc(借 device 连接池);测试注入替身。
type RemoteDiscoverer interface {
	ListSkills(ctx context.Context, deviceID int64, backendType string) ([]agentskill.SkillPack, error)
}
