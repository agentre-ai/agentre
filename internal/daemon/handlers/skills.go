package handlers

import (
	"context"
	"strings"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
)

// SkillsListParams skills.list RPC 入参:desktop 只传 backend type;CLIPath 一般留空,
// 由 daemon 自己解析本机 CLI 路径(desktop 不知道 daemon 的 claude 在哪)。
type SkillsListParams struct {
	BackendType string `json:"backendType"`
	CLIPath     string `json:"cliPath,omitempty"`
}

// SkillsListResult skills.list RPC 出参:daemon 本机已装技能包。Packs 永远非 nil。
type SkillsListResult struct {
	Packs []agentskill.SkillPack `json:"packs"`
}

// SkillsHandlers 收纳 skills.* RPC。无依赖:发现器从 agentskill 全局注册表反查
// (daemon 启动时 blank import claudeskill/codexskill 触发 init 注册)。
type SkillsHandlers struct{}

// NewSkillsHandlers 构造 skills.* handler。
func NewSkillsHandlers() *SkillsHandlers { return &SkillsHandlers{} }

// List 在 daemon 本机枚举该 backend 已装技能包(= `claude plugin list --json`),供
// desktop 给远端 agent 配 per-agent 技能时展 daemon 真实可用集(而非 desktop 的)。
// 无对应发现器 → 空(向前兼容);CLIPath 缺省时解析 daemon 本机 CLI 路径。
func (h *SkillsHandlers) List(ctx context.Context, p SkillsListParams) (SkillsListResult, error) {
	bt := agent_backend_entity.BackendType(p.BackendType)
	d, ok := agentskill.DiscovererFor(bt)
	if !ok {
		return SkillsListResult{Packs: []agentskill.SkillPack{}}, nil
	}
	cliPath := strings.TrimSpace(p.CLIPath)
	if cliPath == "" {
		if path, found, err := resolveCLIPathFunc(p.BackendType); err == nil && found {
			cliPath = path
		}
	}
	packs, err := d.Discover(ctx, agentskill.DiscoverQuery{BackendType: bt, CLIPath: cliPath})
	if err != nil {
		return SkillsListResult{}, err
	}
	if packs == nil {
		packs = []agentskill.SkillPack{}
	}
	return SkillsListResult{Packs: packs}, nil
}
