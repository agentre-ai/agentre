package orch_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// CancelTask 软取消 + 尽力硬打断目标任务及其全部 dispatch 子孙(限调用者同 Run)。
// 返回被标记取消的活任务数。watcher 侧的让位守卫保证被取消任务不会被误翻/误回报。
func (s *orchSvc) CancelTask(ctx context.Context, sessionID, taskID int64) (int, error) {
	caller, err := s.dispatches.FindBySession(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if caller == nil {
		return 0, errRunNotActive
	}
	target, err := s.dispatches.Find(ctx, taskID)
	if err != nil {
		return 0, err
	}
	if target == nil || target.RunID != caller.RunID {
		return 0, errForeignDispatch
	}
	rows, err := s.dispatches.ListByRun(ctx, caller.RunID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, tk := range collectSubtree(rows, taskID) {
		if !tk.IsActive() {
			continue
		}
		tk.Status = orch_entity.DispatchCanceled
		if uerr := s.dispatches.Update(ctx, tk); uerr != nil {
			logger.Ctx(ctx).Error("orch.CancelTask: 标记取消失败", zap.Int64("task", tk.ID), zap.Error(uerr))
			continue
		}
		n++
		// 尽力硬打断在跑的一轮(无活跃 turn → 适配器吞成功)。
		if aerr := s.chat.AbortTurn(ctx, tk.SessionID); aerr != nil {
			logger.Ctx(ctx).Warn("orch.CancelTask: 硬打断失败(软取消已生效)", zap.Int64("task", tk.ID), zap.Error(aerr))
		}
	}
	s.emitRunUpdated(ctx, caller.RunID)
	return n, nil
}

// collectSubtree 返回 rootID 及其全部 dispatch 子孙任务(BFS,防环)。
func collectSubtree(rows []*orch_entity.Dispatch, rootID int64) []*orch_entity.Dispatch {
	byID := map[int64]*orch_entity.Dispatch{}
	byParent := map[int64][]*orch_entity.Dispatch{}
	for _, t := range rows {
		byID[t.ID] = t
		byParent[t.ParentDispatchID] = append(byParent[t.ParentDispatchID], t)
	}
	var out []*orch_entity.Dispatch
	seen := map[int64]bool{}
	queue := []int64{rootID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		if t := byID[id]; t != nil {
			out = append(out, t)
		}
		for _, c := range byParent[id] {
			if c.Kind == orch_entity.DispatchKindDispatch {
				queue = append(queue, c.ID)
			}
		}
	}
	return out
}
