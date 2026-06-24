package orch_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// Dispatch 把子任务异步派给某 agent：建子会话 + Task，触发其首轮，立即返回 taskID。
// 子任务完成由 watchCompletion(Task 8) 回报派发者。
func (s *orchSvc) Dispatch(ctx context.Context, parentSessionID int64, agentName, brief string, isolate bool) (int64, error) {
	parent, err := s.tasks.FindBySession(ctx, parentSessionID)
	if err != nil {
		return 0, err
	}
	if parent == nil {
		return 0, errRunNotActive
	}

	target, err := s.agents.FindByName(ctx, agentName)
	if err != nil {
		return 0, err
	}
	if target == nil {
		return 0, errAgentNotFound
	}

	n, err := s.tasks.CountByRunAgent(ctx, parent.RunID, target.ID)
	if err != nil {
		return 0, err
	}

	childSession, err := s.chat.EnsureOrchSession(ctx, EnsureOrchSessionInput{
		AgentID:         target.ID,
		ParentSessionID: parentSessionID,
		RunID:           parent.RunID,
		Isolate:         isolate,
	})
	if err != nil {
		return 0, err
	}

	child := &orch_entity.Task{
		RunID:        parent.RunID,
		AgentID:      target.ID,
		SessionID:    childSession,
		ParentTaskID: parent.ID,
		Kind:         orch_entity.TaskKindDispatch,
		Status:       orch_entity.TaskRunning,
		Brief:        brief,
		CallSeq:      int(n) + 1,
	}
	if err := s.tasks.Create(ctx, child); err != nil {
		return 0, err
	}

	// 派发者进入「等子任务」（Task 8 在子回报时改回 running）。
	parent.Status = orch_entity.TaskAwaitingChildren
	if err := s.tasks.Update(ctx, parent); err != nil {
		logger.Ctx(ctx).Warn("orch.Dispatch: 更新父任务状态失败(非致命,子任务照常跑)", zap.Int64("task", parent.ID), zap.Error(err))
	}

	// 经调度器并发执行（Task 9 接管）；本步先直接触发首轮 + 挂完成监听。
	s.fireEnqueue(parent.RunID, child, brief)

	logger.Ctx(ctx).Info("orch.Dispatch: 已派发子任务",
		zap.Int64("run", parent.RunID), zap.Int64("task", child.ID),
		zap.Int64("agent", target.ID), zap.Int("callSeq", child.CallSeq))
	return child.ID, nil
}

// fireEnqueue 路由到真实 enqueueRun 或测试注入的 no-op 钩子。
func (s *orchSvc) fireEnqueue(runID int64, task *orch_entity.Task, brief string) {
	if s.enqueue != nil {
		s.enqueue(runID, task, brief)
		return
	}
	s.enqueueRun(runID, task, brief)
}
