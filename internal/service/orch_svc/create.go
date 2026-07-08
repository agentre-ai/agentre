package orch_svc

import (
	"context"
	"encoding/json"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// CreateRunRequest 创建编排 Run 入参。
type CreateRunRequest struct {
	Goal            string
	LeaderAgentID   int64
	FlowID          int64
	FlowContent     string
	ProjectID       int64
	AllowedAgentIDs []int64 // 可选限定可调度团队（空=全部）
}

// RunDetail 创建结果。
type RunDetail struct {
	Run      *orch_entity.OrchestrationRun
	RootTask *orch_entity.Task
}

// CreateRun 建 Run + Leader 根会话 + 根 Task，注入编排流程并触发 Leader 首轮。
func (s *orchSvc) CreateRun(ctx context.Context, req *CreateRunRequest) (*RunDetail, error) {
	leader, err := s.agents.Find(ctx, req.LeaderAgentID)
	if err != nil {
		return nil, err
	}
	if leader == nil {
		return nil, errLeaderNotFound
	}

	allowed := marshalAllowedAgentIDs(req.AllowedAgentIDs)

	// 库模式(只传 FlowID、未直传 FlowContent)→ 快照该流程已投影的正文进 run.FlowContent，
	// 否则 turn.go 只读 run.FlowContent、库流程将注入为空。adhoc 直传 FlowContent 时跳过。
	flowContent := req.FlowContent
	if flowContent == "" && req.FlowID > 0 && s.wf != nil {
		if c, err := s.wf.FlowContentByID(ctx, req.FlowID); err == nil {
			flowContent = c
		} else {
			logger.Ctx(ctx).Warn("orch.CreateRun: 取流程正文失败,按无流程继续", zap.Int64("flow", req.FlowID), zap.Error(err))
		}
	}

	run := &orch_entity.OrchestrationRun{
		Goal: req.Goal, LeaderAgentID: req.LeaderAgentID,
		FlowID: req.FlowID, FlowContent: flowContent,
		ProjectID: req.ProjectID, Status: orch_entity.RunRunning,
		AllowedAgentIDs: allowed,
	}
	if err := s.runs.Create(ctx, run); err != nil {
		return nil, err
	}

	rootSessionID, err := s.chat.EnsureOrchSession(ctx, EnsureOrchSessionInput{
		AgentID: req.LeaderAgentID, ParentSessionID: 0, ProjectID: req.ProjectID, RunID: run.ID,
		Title: req.Goal, // 根会话标题 = Run 目标(首条消息), 与普通会话首消息起标题对齐。
	})
	if err != nil {
		return nil, err
	}

	root := &orch_entity.Task{
		RunID: run.ID, AgentID: req.LeaderAgentID, SessionID: rootSessionID,
		Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskRunning, Brief: req.Goal,
	}
	if err := s.tasks.Create(ctx, root); err != nil {
		return nil, err
	}

	run.RootTaskID = root.ID
	if err := s.runs.Update(ctx, run); err != nil {
		return nil, err
	}

	// 触发 Leader 首轮：把目标作为首条消息送进根会话；编排流程经 BuildTurnExtras 注入 system-prompt（Task 15）。
	if err := s.chat.SendAndForget(ctx, rootSessionID, req.Goal); err != nil {
		logger.Ctx(ctx).Error("orch.CreateRun: 触发 Leader 首轮失败", zap.Int64("session", rootSessionID), zap.Error(err))
		return nil, err
	}

	logger.Ctx(ctx).Info("orch.CreateRun: 已创建编排 Run", zap.Int64("run", run.ID), zap.Int64("leader", req.LeaderAgentID))
	return &RunDetail{Run: run, RootTask: root}, nil
}

// marshalAllowedAgentIDs 去重 + 剔 0 后 JSON 化；空切片 → ""（表示不限制）。
func marshalAllowedAgentIDs(ids []int64) string {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return ""
	}
	b, _ := json.Marshal(out)
	return string(b)
}
