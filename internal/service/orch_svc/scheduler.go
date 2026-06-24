package orch_svc

import (
	"context"
	"runtime"
	"sync"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// queued 是等待被调度器发射的一个子任务。
type queued struct {
	task  *orch_entity.Task
	brief string
}

// scheduler 是单个 Run 的并发调度器：pending FIFO + inflight 计数 + 并发上限。
type scheduler struct {
	mu       sync.Mutex
	pending  []queued
	inflight int
	cap      int
	paused   bool // pause 门控：true 时 kick 不起新槽（in-flight goroutine 继续跑完）
}

// schedulerFor 懒建并返回某 Run 的调度器（s.schedMu 保护 s.schedulers）。
// 并发上限：若 s.defaultCap > 0（测试覆盖），直接用它；否则 min(16, runtime.NumCPU())，floor 1。
func (s *orchSvc) schedulerFor(runID int64) *scheduler {
	s.schedMu.Lock()
	defer s.schedMu.Unlock()
	sc := s.schedulers[runID]
	if sc == nil {
		c := s.defaultCap
		if c <= 0 {
			c = max(1, min(16, runtime.NumCPU()))
		}
		sc = &scheduler{cap: c}
		s.schedulers[runID] = sc
	}
	return sc
}

// enqueueRun 将子任务加入该 Run 的调度队列，然后 kick 尝试发射。
func (s *orchSvc) enqueueRun(runID int64, task *orch_entity.Task, brief string) {
	sc := s.schedulerFor(runID)
	sc.mu.Lock()
	sc.pending = append(sc.pending, queued{task: task, brief: brief})
	sc.mu.Unlock()
	s.kick(runID)
}

// setSchedulerPaused 设置/清除某 Run 调度器的 paused 门控标志。
func (s *orchSvc) setSchedulerPaused(runID int64, paused bool) {
	sc := s.schedulerFor(runID)
	sc.mu.Lock()
	sc.paused = paused
	sc.mu.Unlock()
}

// kick 在并发上限内尽可能将 pending 任务移入 inflight，并在锁外为每个任务起一个 goroutine。
func (s *orchSvc) kick(runID int64) {
	sc := s.schedulerFor(runID)
	sc.mu.Lock()
	if sc.paused {
		sc.mu.Unlock()
		return // 暂停中：不起新槽，in-flight goroutine 继续跑完
	}
	var launch []queued
	for sc.inflight < sc.cap && len(sc.pending) > 0 {
		q := sc.pending[0]
		sc.pending = sc.pending[1:]
		sc.inflight++
		launch = append(launch, q)
	}
	sc.mu.Unlock()

	// goroutine 在锁外启动，避免 SendAndForget 阻塞其他任务。
	for _, q := range launch {
		go func(q queued) {
			ctx := context.Background()
			if err := s.chat.SendAndForget(ctx, q.task.SessionID, q.brief); err != nil {
				s.onTaskSettled(runID)
				return
			}
			s.watchCompletion(ctx, q.task)
		}(q)
	}
}

// onTaskSettled 释放调度并发槽，然后 kick 尝试发射下一个 pending 任务。
// 由 watchCompletion（子任务完成/出错）在锁外调用。
func (s *orchSvc) onTaskSettled(runID int64) {
	sc := s.schedulerFor(runID)
	sc.mu.Lock()
	if sc.inflight > 0 {
		sc.inflight--
	}
	sc.mu.Unlock()
	s.kick(runID)
}
