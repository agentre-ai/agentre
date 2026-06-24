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
	// 非根：把小结当回报上抛父（与完成回报同路）。
	s.reportToParent(ctx, tk.ParentTaskID, tk, summary)
	s.onTaskSettled(tk.RunID)
	return nil
}
