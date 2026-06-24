package orch_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// Finish 收口当前任务；若为 Run 根任务则整个 Run done。
func (s *orchSvc) Finish(ctx context.Context, sessionID int64, summary string) error {
	tk, err := s.tasks.FindBySession(ctx, sessionID)
	if err != nil || tk == nil {
		return errRunNotActive
	}
	tk.Status = orch_entity.TaskDone
	tk.Result = summary
	if err := s.tasks.Update(ctx, tk); err != nil {
		return err
	}
	run, err := s.runs.Find(ctx, tk.RunID)
	if err != nil {
		return err
	}
	if run != nil && run.RootTaskID == tk.ID {
		// ROOT: 整个 Run 收口。
		run.Status = orch_entity.RunDone
		if err := s.runs.Update(ctx, run); err != nil {
			return err
		}
		if s.emit != nil {
			s.emit.Emit(ctx, "orch:run:done", map[string]any{"runId": run.ID})
		}
		logger.Ctx(ctx).Info("orch.Finish: Run 收口完成", zap.Int64("run", run.ID))
		return nil
	}
	// 非根:只「记录」显式小结。回报父 + 释放调度槽由 watcher(watchCompletion)
	// 统一负责;此处若同步再做一遍会让 Leader 收到两份回报(两次多余续轮)、
	// inflight 被减两次(并发额度记账错乱)。tk.Status=done + Result 已在上方写库,
	// watcher 的 idle 分支会优先读到该 Result 作为回报正文。
	// 任务已记录完成态，提前通知前端刷新 Run 状态（watcher 后续也会 emit，过发无害）。
	s.emitRunUpdated(ctx, tk.RunID)
	return nil
}
