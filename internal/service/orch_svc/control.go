package orch_svc

import (
	"context"

	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/cago-frame/cago/pkg/logger"
)

// setRunStatus 加载 Run、设状态、持久化、推事件（三步原子序列，任一失败即返回错误）。
func (s *orchSvc) setRunStatus(ctx context.Context, runID int64, status, event string) error {
	run, err := s.runs.Find(ctx, runID)
	if err != nil || run == nil {
		return errRunNotActive
	}
	run.Status = status
	if err := s.runs.Update(ctx, run); err != nil {
		return err
	}
	if s.emit != nil {
		s.emit.Emit(ctx, event, map[string]any{"runId": runID})
	}
	return nil
}

// PauseRun 将 Run 标为 paused，并设置调度器暂停门控（kick 不再起新槽）。
func (s *orchSvc) PauseRun(ctx context.Context, runID int64) error {
	if err := s.setRunStatus(ctx, runID, orch_entity.RunPaused, "orch:run:paused"); err != nil {
		return err
	}
	s.setSchedulerPaused(runID, true)
	return nil
}

// ResumeRun 将 Run 标为 running，清除暂停门控，并触发 kick 发射积压任务。
func (s *orchSvc) ResumeRun(ctx context.Context, runID int64) error {
	if err := s.setRunStatus(ctx, runID, orch_entity.RunRunning, "orch:run:resumed"); err != nil {
		return err
	}
	s.setSchedulerPaused(runID, false)
	s.kick(runID)
	return nil
}

// StopRun 将 Run 标为 stopped，级联把活任务标 canceled，并推 orch:run:stopped 事件。
// worktree/CLI 清理由上层负责；本方法只更新状态。
func (s *orchSvc) StopRun(ctx context.Context, runID int64) error {
	run, err := s.runs.Find(ctx, runID)
	if err != nil || run == nil {
		return errRunNotActive
	}
	run.Status = orch_entity.RunStopped
	if err := s.runs.Update(ctx, run); err != nil {
		return err
	}
	// 暂停调度器——阻止新槽启动（in-flight goroutine 继续跑完自行退出）。
	s.setSchedulerPaused(runID, true)

	// 级联取消活任务（非终态）。
	rows, err := s.tasks.ListByRun(ctx, runID)
	if err != nil {
		logger.Ctx(ctx).Warn("orch.StopRun: 取任务列表失败(级联取消可能不完整)", zap.Int64("run", runID), zap.Error(err))
	}
	for _, tk := range rows {
		if tk.IsActive() {
			tk.Status = orch_entity.TaskCanceled
			if err := s.tasks.Update(ctx, tk); err != nil {
				logger.Ctx(ctx).Error("orch_svc.StopRun: cancel task failed",
					zap.Int64("taskID", tk.ID), zap.Error(err))
			}
		}
	}

	if s.emit != nil {
		s.emit.Emit(ctx, "orch:run:stopped", map[string]any{"runId": runID})
	}
	return nil
}

// Speak 向任意会话发言，触发其下一轮（Leader=全局纠偏；worker=局部返工）。
func (s *orchSvc) Speak(ctx context.Context, sessionID int64, message string) error {
	return s.chat.SendAndForget(ctx, sessionID, message)
}
