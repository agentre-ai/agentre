package orch_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// SetEnqueueForTest 仅测试用:替换异步触发,避免 goroutine 与 ctrl.Finish 竞态。
func (s *orchSvc) SetEnqueueForTest(fn func(int64, *orch_entity.Task, string)) {
	s.enqueue = fn
}

// WatchCompletionForTest 仅测试用:暴露私有 watchCompletion 供外部测试包驱动。
func (s *orchSvc) WatchCompletionForTest(ctx context.Context, t *orch_entity.Task) {
	s.watchCompletion(ctx, t)
}

// AllChildrenSettledForTest 仅测试用:暴露私有 allChildrenSettled 供外部测试包驱动边界用例。
func (s *orchSvc) AllChildrenSettledForTest(ctx context.Context, parent *orch_entity.Task) bool {
	return s.allChildrenSettled(ctx, parent)
}

// SetSchedulerCapForTest 仅测试用：覆盖新建调度器的并发上限（0 重置为 NumCPU 默认值）。
func (s *orchSvc) SetSchedulerCapForTest(n int) {
	s.defaultCap = n
}

// ResetSchedulersForTest 仅测试用：清空已缓存的调度器（配合 SetSchedulerCapForTest 重置）。
func (s *orchSvc) ResetSchedulersForTest() {
	s.schedMu.Lock()
	s.schedulers = map[int64]*scheduler{}
	s.schedMu.Unlock()
}

// EnqueueRunForTest 仅测试用：直接调用 enqueueRun（绕过 fireEnqueue / enqueue 钩子）。
func (s *orchSvc) EnqueueRunForTest(runID int64, task *orch_entity.Task, brief string) {
	s.enqueueRun(runID, task, brief)
}

// OnTaskSettledForTest 仅测试用：直接调用 onTaskSettled（手动释放调度槽）。
func (s *orchSvc) OnTaskSettledForTest(runID int64) {
	s.onTaskSettled(runID)
}

// SchedulerPausedForTest 仅测试用：返回某 Run 调度器的 paused 标志（由 PauseRun/ResumeRun 控制）。
func (s *orchSvc) SchedulerPausedForTest(runID int64) bool {
	sc := s.schedulerFor(runID)
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.paused
}
