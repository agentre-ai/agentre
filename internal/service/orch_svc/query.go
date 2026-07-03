package orch_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
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

// ListAllowedAgents 返回调用者所在 Run 的可参与 agent（allowed∪{leader}；集合空/定位不到 Run → 全部）。
func (s *orchSvc) ListAllowedAgents(ctx context.Context, sessionID int64) ([]*agent_entity.Agent, error) {
	all, err := s.agents.List(ctx)
	if err != nil {
		return nil, err
	}
	// 定位不到 Run(会话无任务 / DB 错误)→ 放行全部(fail-open:可参与是团队编成、非安全边界)。
	tk, err := s.tasks.FindBySession(ctx, sessionID)
	if err != nil {
		logger.Ctx(ctx).Warn("orch.ListAllowedAgents: 定位会话任务失败,放行全部", zap.Int64("session", sessionID), zap.Error(err))
		return all, nil
	}
	if tk == nil {
		return all, nil
	}
	run, err := s.runs.Find(ctx, tk.RunID)
	if err != nil {
		logger.Ctx(ctx).Warn("orch.ListAllowedAgents: 取 Run 失败,放行全部", zap.Int64("run", tk.RunID), zap.Error(err))
		return all, nil
	}
	if run == nil {
		return all, nil
	}
	set := run.AllowedSet()
	if len(set) == 0 {
		return all, nil
	}
	out := make([]*agent_entity.Agent, 0, len(all))
	for _, a := range all {
		if set[a.ID] || a.ID == run.LeaderAgentID {
			out = append(out, a)
		}
	}
	return out, nil
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
