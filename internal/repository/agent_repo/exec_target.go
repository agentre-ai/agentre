package agent_repo

import (
	"context"

	"github.com/cago-frame/cago/database/db"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
)

//go:generate mockgen -source exec_target.go -destination mock_agent_repo/mock_exec_target.go

// AgentExecTargetRepo Agent 执行目标列表的持久化访问。列表是有序的，读口一律按
// sort_order 升序给出；写口整表替换（下标即 sort_order）。
type AgentExecTargetRepo interface {
	ListByAgent(ctx context.Context, agentID int64) ([]*agent_entity.AgentExecTarget, error)
	ListByAgents(ctx context.Context, agentIDs []int64) (map[int64][]*agent_entity.AgentExecTarget, error)
	// Replace 整表替换成 targets 给出的顺序：下标即 sort_order，调用方只需给
	// AgentBackendID + SkillsJSON，ID/AgentID/SortOrder 由仓储自己落。技能授权
	// （R15e）与它所在的档同生共死：没有被带进新列表的档连它的 skills_json 一起
	// 消失，不需要一个单独的"清理授权"步骤。
	Replace(ctx context.Context, agentID int64, targets []*agent_entity.AgentExecTarget) error
}

var defaultAgentExecTarget AgentExecTargetRepo

func AgentExecTarget() AgentExecTargetRepo             { return defaultAgentExecTarget }
func RegisterAgentExecTarget(impl AgentExecTargetRepo) { defaultAgentExecTarget = impl }
func NewAgentExecTarget() AgentExecTargetRepo          { return &agentExecTargetRepo{} }

type agentExecTargetRepo struct{}

func (r *agentExecTargetRepo) ListByAgent(ctx context.Context, agentID int64) ([]*agent_entity.AgentExecTarget, error) {
	return listExecTargets(ctx, []int64{agentID})
}

func (r *agentExecTargetRepo) ListByAgents(ctx context.Context, agentIDs []int64) (map[int64][]*agent_entity.AgentExecTarget, error) {
	rows, err := listExecTargets(ctx, agentIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]*agent_entity.AgentExecTarget, len(agentIDs))
	for _, row := range rows {
		out[row.AgentID] = append(out[row.AgentID], row)
	}
	return out, nil
}

func (r *agentExecTargetRepo) Replace(ctx context.Context, agentID int64, targets []*agent_entity.AgentExecTarget) error {
	return db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		return replaceExecTargets(tx, agentID, targets)
	})
}

// listExecTargets 按 (agent_id, sort_order, id) 升序读出给定 Agent 的执行目标行。
// 一次查询覆盖整批 Agent，调用方自己分组。
func listExecTargets(ctx context.Context, agentIDs []int64) ([]*agent_entity.AgentExecTarget, error) {
	if len(agentIDs) == 0 {
		return nil, nil
	}
	var rows []*agent_entity.AgentExecTarget
	err := db.Ctx(ctx).
		Where("agent_id IN ?", agentIDs).
		Order("agent_id ASC, sort_order ASC, id ASC").
		Find(&rows).Error
	return rows, err
}

// replaceExecTargets 把一个 Agent 的执行目标整表替换成 targets 给出的顺序。
// tx 由调用方给出：Agent 行与它的执行目标行必须在同一个事务里落库。delete-then-
// reinsert 是"删档连带删授权"（R15e）成立的原因：没有被带进 targets 的档，连它
// 的 skills_json 一起消失，不存在"档已删、授权没删"的中间态。
func replaceExecTargets(tx *gorm.DB, agentID int64, targets []*agent_entity.AgentExecTarget) error {
	if err := tx.Where("agent_id = ?", agentID).Delete(&agent_entity.AgentExecTarget{}).Error; err != nil {
		return err
	}
	return insertExecTargets(tx, agentID, targets)
}

// insertExecTargets 按下标落 sort_order 插入执行目标行；空列表不发 INSERT。
// 只信 targets 里的 AgentBackendID / SkillsJSON，ID/AgentID/SortOrder 由这里落。
func insertExecTargets(tx *gorm.DB, agentID int64, targets []*agent_entity.AgentExecTarget) error {
	if len(targets) == 0 {
		return nil
	}
	rows := make([]*agent_entity.AgentExecTarget, 0, len(targets))
	for i, t := range targets {
		rows = append(rows, &agent_entity.AgentExecTarget{
			AgentID:        agentID,
			AgentBackendID: t.AgentBackendID,
			SortOrder:      i,
			SkillsJSON:     t.SkillsJSON,
		})
	}
	return tx.Create(&rows).Error
}

// primaryTargetList 把「Agent 当前的那一个 backend + 它这一档的技能授权」表达成
// 执行目标列表：0 = 空列表。
func primaryTargetList(backendID int64, skillsJSON string) []*agent_entity.AgentExecTarget {
	if backendID <= 0 {
		return nil
	}
	return []*agent_entity.AgentExecTarget{{AgentBackendID: backendID, SkillsJSON: skillsJSON}}
}

// hydrateOne 补齐单个 Agent 的派生值；补不齐就不交出这个 Agent —— 交出去的话
// 调用方会拿着历史列的残值（或 0）当真，把有后端的 Agent 当成「未配置后端」。
func hydrateOne(ctx context.Context, a *agent_entity.Agent) (*agent_entity.Agent, error) {
	if err := hydrateExecTargets(ctx, []*agent_entity.Agent{a}); err != nil {
		return nil, err
	}
	return a, nil
}

// hydrateExecTargets 用执行目标行补齐一批 Agent 的 AgentBackendID —— 取 sort_order
// 最小的那一行，没有目标行则为 0。读取一律走这里，agents.agent_backend_id 历史列
// 不再被读。
func hydrateExecTargets(ctx context.Context, agents []*agent_entity.Agent) error {
	ids := make([]int64, 0, len(agents))
	for _, a := range agents {
		if a != nil && a.ID > 0 {
			ids = append(ids, a.ID)
		}
	}
	rows, err := listExecTargets(ctx, ids)
	if err != nil {
		return err
	}
	primary := make(map[int64]int64, len(rows))
	for _, row := range rows {
		if _, ok := primary[row.AgentID]; !ok {
			primary[row.AgentID] = row.AgentBackendID
		}
	}
	for _, a := range agents {
		if a != nil {
			a.AgentBackendID = primary[a.ID]
		}
	}
	return nil
}
