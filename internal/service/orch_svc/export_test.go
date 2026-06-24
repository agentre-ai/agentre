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
