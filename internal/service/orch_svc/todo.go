package orch_svc

import (
	"context"
	"encoding/json"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// runIDForSession 复用派发绑定推出调用者所在 Run(清单与派发共用 Run 作用域)。
func (s *orchSvc) runIDForSession(ctx context.Context, sessionID int64) (int64, error) {
	d, err := s.dispatches.FindBySession(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if d == nil {
		return 0, errRunNotActive
	}
	return d.RunID, nil
}

// TaskAdd 在调用者所在 Run 的待办清单新增一条条目；Run 内任意 agent 可建，与派发/会话零联动。
func (s *orchSvc) TaskAdd(ctx context.Context, sessionID, agentID int64, text string) (int64, error) {
	runID, err := s.runIDForSession(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	maxSeq, err := s.todos.MaxSeq(ctx, runID)
	if err != nil {
		return 0, err
	}
	t := &orch_entity.Task{
		RunID:            runID,
		Seq:              maxSeq + 1,
		Text:             text,
		Status:           orch_entity.TaskStatusPending,
		CreatedByAgentID: agentID,
	}
	if err := s.todos.Create(ctx, t); err != nil {
		return 0, err
	}
	s.emitRunUpdated(ctx, runID)
	return t.ID, nil
}

// TaskUpdate 改一条待办的状态和/或认领；status=="" 表示不改状态，claim=true 时把 assignee 设为 agentID。
func (s *orchSvc) TaskUpdate(ctx context.Context, sessionID, agentID, taskID int64, status string, claim bool) error {
	runID, err := s.runIDForSession(ctx, sessionID)
	if err != nil {
		return err
	}
	t, err := s.todos.Find(ctx, taskID)
	if err != nil {
		return err
	}
	if t == nil || t.RunID != runID {
		return errForeignTask
	}
	if status != "" {
		if !orch_entity.ValidTaskStatus(status) {
			return errInvalidTaskStatus
		}
		t.Status = status
	}
	if claim {
		t.AssigneeAgentID = agentID
	}
	if err := s.todos.Update(ctx, t); err != nil {
		return err
	}
	s.emitRunUpdated(ctx, runID)
	return nil
}

// taskRow 是 TaskList 对外的 JSON 投影(agent 读的是文本，不是结构体)。
type taskRow struct {
	ID       int64  `json:"task_id"`
	Seq      int    `json:"seq"`
	Text     string `json:"text"`
	Status   string `json:"status"`
	Assignee int64  `json:"assignee_agent_id,omitempty"`
}

// TaskList 列出调用者所在 Run 的整份待办清单(JSON 数组文本)。
func (s *orchSvc) TaskList(ctx context.Context, sessionID int64) (string, error) {
	runID, err := s.runIDForSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	rows, err := s.todos.ListByRun(ctx, runID)
	if err != nil {
		return "", err
	}
	out := make([]taskRow, 0, len(rows))
	for _, t := range rows {
		out = append(out, taskRow{ID: t.ID, Seq: t.Seq, Text: t.Text, Status: t.Status, Assignee: t.AssigneeAgentID})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
