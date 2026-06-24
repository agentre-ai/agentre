package orch_svc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
)

// TestDetectAskCycle_AskOnly: ask 边 700→800, 800→700 形成环；无 dispatch 边。
func TestDetectAskCycle_AskOnly(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	s := &orchSvc{tasks: tasks, pending: map[string]askEnvelope{}, askWaits: map[int64]int64{}}

	// 两个根任务，ParentTaskID=0，Status=running（非 awaiting-children）→ 无 dispatch 边。
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 1, SessionID: 700, ParentTaskID: 0, Status: orch_entity.TaskRunning},
		{ID: 2, SessionID: 800, ParentTaskID: 0, Status: orch_entity.TaskRunning},
	}, nil).AnyTimes()

	s.recordAskWait(700, 800)
	s.recordAskWait(800, 700)

	cycle, found := s.detectAskCycle(context.Background(), 100)
	assert.True(t, found)
	assert.Len(t, cycle, 2)
}

// TestDetectAskCycle_CombinedDispatchAndAsk: dispatch 边 P(500)→C(600) + ask 边 600→500 → 联合环。
func TestDetectAskCycle_CombinedDispatchAndAsk(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	s := &orchSvc{tasks: tasks, pending: map[string]askEnvelope{}, askWaits: map[int64]int64{}}

	// P 处于 awaiting-children 状态，等 dispatch 子任务 C 完成。
	// C 是 dispatch 子任务，正在 running。
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 9, SessionID: 500, ParentTaskID: 0, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskAwaitingChildren},
		{ID: 11, SessionID: 600, ParentTaskID: 9, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskRunning},
	}, nil).AnyTimes()

	// C(600) 发 ask 给 P(500) → 形成环 500→600→500。
	s.recordAskWait(600, 500)

	cycle, found := s.detectAskCycle(context.Background(), 100)
	assert.True(t, found)
	assert.Len(t, cycle, 2)
}

// TestDetectAskCycle_NoCycle: 单向 ask 边 700→800，无回路 → found==false。
func TestDetectAskCycle_NoCycle(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	s := &orchSvc{tasks: tasks, pending: map[string]askEnvelope{}, askWaits: map[int64]int64{}}

	// 无 awaiting-children，无 dispatch 边。
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 1, SessionID: 700, ParentTaskID: 0, Status: orch_entity.TaskRunning},
		{ID: 2, SessionID: 800, ParentTaskID: 0, Status: orch_entity.TaskRunning},
	}, nil).AnyTimes()

	// 单向 ask: 700 问 800，无回头边。
	s.recordAskWait(700, 800)

	cycle, found := s.detectAskCycle(context.Background(), 100)
	assert.False(t, found)
	assert.Nil(t, cycle)
}
