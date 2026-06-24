package orch_svc

import "github.com/agentre-ai/agentre/internal/model/entity/orch_entity"

// SetEnqueueForTest 仅测试用:替换异步触发,避免 goroutine 与 ctrl.Finish 竞态。
func (s *orchSvc) SetEnqueueForTest(fn func(int64, *orch_entity.Task, string)) {
	s.enqueue = fn
}
