package orch_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// ErrRunNotActive 在 LoadRun 找不到 id 对应的 Run 时返回。
// 导出以便调用方(如 app 绑定层)用 errors.Is 判别。
var ErrRunNotActive = errRunNotActive

// RunLoadResult LoadRun 的返回值:Run 与其全部 Task。
type RunLoadResult struct {
	Run   *orch_entity.OrchestrationRun
	Tasks []*orch_entity.Task
}

// ListRuns 按更新时间倒序返回全部 Run。
func (s *orchSvc) ListRuns(ctx context.Context) ([]*orch_entity.OrchestrationRun, error) {
	return s.runs.List(ctx)
}

// LoadRun 取指定 Run 及其全部 Task;Run 不存在时返回 ErrRunNotActive。
func (s *orchSvc) LoadRun(ctx context.Context, id int64) (*RunLoadResult, error) {
	run, err := s.runs.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, errRunNotActive
	}
	tasks, err := s.tasks.ListByRun(ctx, id)
	if err != nil {
		return nil, err
	}
	return &RunLoadResult{Run: run, Tasks: tasks}, nil
}
