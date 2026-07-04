package orch_svc

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

type statusRow struct {
	TaskID     int64   `json:"task_id"`
	Agent      string  `json:"agent"`
	Kind       string  `json:"kind"`
	Status     string  `json:"status"`
	Brief      string  `json:"brief"`
	CallSeq    int     `json:"call_seq"`
	HasSummary bool    `json:"has_summary"`
	Node       string  `json:"node,omitempty"`
	ParentTask int64   `json:"parent_task_id,omitempty"`
	BlockedOn  []int64 `json:"blocked_on,omitempty"`
}

// formatRunStatus 把 Run 任务树渲染成紧凑 JSON 快照(供 Leader status 工具)。
// 无时钟依赖的纯函数;agentNames 缺省用 "agent#<id>"。
func formatRunStatus(tasks []*orch_entity.Task, agentNames map[int64]string) string {
	// 预计算每个父任务下仍活跃的 dispatch 子任务 id(blocked_on 只给 awaiting-children)。
	activeChildren := map[int64][]int64{}
	for _, t := range tasks {
		if t.Kind == orch_entity.TaskKindDispatch && t.IsActive() && t.ParentTaskID != 0 {
			activeChildren[t.ParentTaskID] = append(activeChildren[t.ParentTaskID], t.ID)
		}
	}
	rows := make([]statusRow, 0, len(tasks))
	for _, t := range tasks {
		name := agentNames[t.AgentID]
		if name == "" {
			name = "agent#" + strconv.FormatInt(t.AgentID, 10)
		}
		row := statusRow{
			TaskID:     t.ID,
			Agent:      name,
			Kind:       t.Kind,
			Status:     t.Status,
			Brief:      t.Brief,
			CallSeq:    t.CallSeq,
			HasSummary: t.Summary != "",
			Node:       t.NodeRef,
			ParentTask: t.ParentTaskID,
		}
		if t.Status == orch_entity.TaskAwaitingChildren {
			row.BlockedOn = activeChildren[t.ID]
		}
		rows = append(rows, row)
	}
	b, _ := json.Marshal(rows)
	return string(b)
}

// RunStatus 返回调用者所在 Run 的任务树 JSON 快照。
func (s *orchSvc) RunStatus(ctx context.Context, sessionID int64) (string, error) {
	caller, err := s.tasks.FindBySession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if caller == nil {
		return "", errRunNotActive
	}
	rows, err := s.tasks.ListByRun(ctx, caller.RunID)
	if err != nil {
		return "", err
	}
	names := map[int64]string{}
	if agents, aerr := s.agents.List(ctx); aerr != nil {
		logger.Ctx(ctx).Warn("orch.RunStatus: 取 agent 花名册失败,退回 agent#id", zap.Error(aerr))
	} else {
		for _, a := range agents {
			names[a.ID] = a.Name
		}
	}
	return formatRunStatus(rows, names), nil
}
