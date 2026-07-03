// Package orch_entity 维护编排 Run 与 Task 的充血实体。Run = 一次编排（会话容器），
// Task = 树上的一个任务（一个 agent + 一段 brief + 一条持久会话）。
package orch_entity

import (
	"encoding/json"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
)

// Run 客观生命周期（与流程无关；Run 不单设 error，根任务技术崩溃由 Task.error 体现）。
const (
	RunPending = "pending"
	RunRunning = "running"
	RunPaused  = "paused"
	RunDone    = "done"
	RunStopped = "stopped"
)

// OrchestrationRun 一次编排 Run。
type OrchestrationRun struct {
	ID              int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Goal            string `gorm:"column:goal;type:text;not null;default:''"`
	LeaderAgentID   int64  `gorm:"column:leader_agent_id;type:bigint;not null;default:0"`
	FlowID          int64  `gorm:"column:flow_id;type:bigint;not null;default:0"`     // 编排流程库引用，0=临时/无
	FlowContent     string `gorm:"column:flow_content;type:text;not null;default:''"` // 创建时快照的流程正文（注入 Leader）
	Status          string `gorm:"column:status;type:text;not null;default:'pending'"`
	RootTaskID      int64  `gorm:"column:root_task_id;type:bigint;not null;default:0"`
	ProjectID       int64  `gorm:"column:project_id;type:bigint;not null;default:0"`
	AllowedAgentIDs string `gorm:"column:allowed_agent_ids;type:text;not null;default:''"` // JSON []int64；空=全部可参与
	Createtime      int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime      int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*OrchestrationRun) TableName() string { return "orchestration_runs" }

// IsActive Run 是否在跑（仅 running）。
func (r *OrchestrationRun) IsActive() bool { return r != nil && r.Status == RunRunning }

// CanAdvance 是否可推进（暂停/停止/完成则不可）。
func (r *OrchestrationRun) CanAdvance() bool { return r != nil && r.Status == RunRunning }

// AllowedSet 解析 allowed_agent_ids JSON 为集合；空/非法/无有效 id → nil（不限制）。
func (r *OrchestrationRun) AllowedSet() map[int64]bool {
	s := strings.TrimSpace(r.AllowedAgentIDs)
	if s == "" || s == "[]" {
		return nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(s), &ids); err != nil {
		return nil
	}
	set := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id != 0 {
			set[id] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// IsAgentAllowed 目标是否可参与：集合空=全部允许；否则须在集合内或为 Leader。
func (r *OrchestrationRun) IsAgentAllowed(agentID, leaderID int64) bool {
	set := r.AllowedSet()
	if len(set) == 0 {
		return true
	}
	return set[agentID] || agentID == leaderID
}

var _ = consts.ACTIVE
