package orch_svc

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

var errNotYourDispatch = errors.New("orch: 只能 send 给自己派发的任务")

// Send 往自己派发的某任务会话发后续/返工；同节点再次 running，不新增节点。
func (s *orchSvc) Send(ctx context.Context, callerSessionID, dispatchID int64, message string) error {
	caller, err := s.dispatches.FindBySession(ctx, callerSessionID)
	if err != nil || caller == nil {
		return errRunNotActive
	}
	tk, err := s.dispatches.Find(ctx, dispatchID)
	if err != nil || tk == nil {
		return errAgentNotFound
	}
	if tk.ParentDispatchID != caller.ID {
		return errNotYourDispatch
	}

	tk.Status = orch_entity.DispatchRunning
	if err := s.dispatches.Update(ctx, tk); err != nil {
		return err
	}

	caller.Status = orch_entity.DispatchAwaitingChildren
	if err := s.dispatches.Update(ctx, caller); err != nil {
		logger.Ctx(ctx).Warn("orch.Send: 更新父任务状态失败(非致命)", zap.Int64("task", caller.ID), zap.Error(err))
	}

	// 任务重新进入 running，通知前端刷新 Run 状态。
	s.emitRunUpdated(ctx, tk.RunID)

	s.enqueueRun(tk.RunID, tk, message) // 复用调度器 + watchCompletion，同节点不新增 Dispatch
	return nil
}
