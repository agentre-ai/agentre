package orch_svc_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
)

// setupTodoDeps 注入 dispatches(拿 RunID)+ todos(清单仓储)两个 mock。
func setupTodoDeps(t *testing.T, ctrl *gomock.Controller) (*mock_orch_repo.MockDispatchRepo, *mock_orch_repo.MockTaskRepo) {
	t.Helper()
	dispatches := mock_orch_repo.NewMockDispatchRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, nil, dispatches, nil, nil)
	orch_svc.Default().RegisterTodoRepo(tasks)
	return dispatches, tasks
}

func TestTaskAdd_SeqAndCreatedByAndPending(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	dispatches, tasks := setupTodoDeps(t, ctrl)

	dispatches.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().MaxSeq(gomock.Any(), int64(100)).Return(3, nil)
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.RunID, ShouldEqual, int64(100))
		So(tk.Seq, ShouldEqual, 4)
		So(tk.Text, ShouldEqual, "写测试")
		So(tk.Status, ShouldEqual, orch_entity.TaskStatusPending)
		So(tk.CreatedByAgentID, ShouldEqual, int64(7))
		tk.ID = 42
		return nil
	})

	Convey("TaskAdd 分配 seq=MaxSeq+1、created_by=调用者、status=pending、返回新 id", t, func() {
		id, err := orch_svc.Default().TaskAdd(context.Background(), 500, 7, "写测试")
		So(err, ShouldBeNil)
		So(id, ShouldEqual, int64(42))
	})
}

func TestTaskUpdate_WritesValidStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	dispatches, tasks := setupTodoDeps(t, ctrl)

	dispatches.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(42)).Return(&orch_entity.Task{ID: 42, RunID: 100, Status: orch_entity.TaskStatusPending}, nil)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.Status, ShouldEqual, orch_entity.TaskStatusInProgress)
		return nil
	})

	Convey("TaskUpdate 合法 status 写回", t, func() {
		err := orch_svc.Default().TaskUpdate(context.Background(), 500, 7, 42, orch_entity.TaskStatusInProgress, false)
		So(err, ShouldBeNil)
	})
}

func TestTaskUpdate_RejectsInvalidStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	dispatches, tasks := setupTodoDeps(t, ctrl)

	dispatches.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(42)).Return(&orch_entity.Task{ID: 42, RunID: 100, Status: orch_entity.TaskStatusPending}, nil)
	// Update 不应被调用：非法 status 应在写回前拒绝。

	Convey("TaskUpdate 非法 status 返回 errInvalidTaskStatus", t, func() {
		err := orch_svc.Default().TaskUpdate(context.Background(), 500, 7, 42, "bogus", false)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "invalid task status")
	})
}

func TestTaskUpdate_ClaimSetsAssignee(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	dispatches, tasks := setupTodoDeps(t, ctrl)

	dispatches.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(42)).Return(&orch_entity.Task{ID: 42, RunID: 100, Status: orch_entity.TaskStatusPending}, nil)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.AssigneeAgentID, ShouldEqual, int64(7))
		return nil
	})

	Convey("TaskUpdate claim=true 把 assignee 设为调用者 agentID", t, func() {
		err := orch_svc.Default().TaskUpdate(context.Background(), 500, 7, 42, "", true)
		So(err, ShouldBeNil)
	})
}

func TestTaskUpdate_RejectsForeignRunTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	dispatches, tasks := setupTodoDeps(t, ctrl)

	// 调用者所在 Run=100，但目标 task 属于 Run=200。
	dispatches.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(42)).Return(&orch_entity.Task{ID: 42, RunID: 200, Status: orch_entity.TaskStatusPending}, nil)

	Convey("TaskUpdate 越 Run 返回 errForeignTask", t, func() {
		err := orch_svc.Default().TaskUpdate(context.Background(), 500, 7, 42, "", true)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "not in this run")
	})
}

func TestTaskList_RendersJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	dispatches, tasks := setupTodoDeps(t, ctrl)

	dispatches.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 42, RunID: 100, Seq: 1, Text: "写测试", Status: orch_entity.TaskStatusPending, AssigneeAgentID: 7},
	}, nil)

	Convey("TaskList 渲染 JSON 含 id/seq/text/status/assignee", t, func() {
		out, err := orch_svc.Default().TaskList(context.Background(), 500)
		So(err, ShouldBeNil)
		So(out, ShouldContainSubstring, `"task_id":42`)
		So(out, ShouldContainSubstring, `"seq":1`)
		So(out, ShouldContainSubstring, `"text":"写测试"`)
		So(out, ShouldContainSubstring, `"status":"pending"`)
		So(out, ShouldContainSubstring, `"assignee_agent_id":7`)
	})
}
