package group_svc

import (
	"context"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo/mock_workflow_repo"
)

// TestGroupSystemPrompt_TasksAndWorkflow 锁住 T11 的提示强化:任务三件套用法、
// SOP 注入(仅主持人)、未完成任务快照(主持人全群/成员只看自己相关)、.agentre/handoff 约定。
func TestGroupSystemPrompt_TasksAndWorkflow(t *testing.T) {
	names := map[int64]string{1: "技术主管", 2: "前端工程师", 3: "后端工程师"}
	newSvc := func() *groupSvc {
		s := newGroupSvc(fakeRosterGW{}, nil)
		s.names = func(_ context.Context, id int64) string { return names[id] }
		return s
	}
	g := &group_entity.Group{ID: 5, Title: "支付小队", WorkflowID: 3, Status: consts.ACTIVE}
	members := []*group_entity.GroupMember{
		{ID: 1, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive},
		{ID: 2, AgentID: 2, Role: group_entity.RoleMember, Status: group_entity.MemberActive},
		{ID: 3, AgentID: 3, Role: group_entity.RoleMember, Status: group_entity.MemberActive},
	}

	Convey("主持人视角:动作环 + SOP 注入 + 全群任务快照 + .agentre/handoff 约定", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		wfRepo := mock_workflow_repo.NewMockWorkflowRepo(ctrl)
		workflow_repo.RegisterWorkflow(wfRepo)
		wfRepo.EXPECT().Find(gomock.Any(), int64(3)).Return(&workflow_entity.Workflow{
			ID: 3, Name: "产品开发流程", Content: "# 产品开发流程\n1. PRD → 2. 实现 → 3. 验收",
			Status: consts.ACTIVE,
		}, nil)
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return([]*group_entity.GroupTask{
			{ID: 21, GroupID: 5, TaskNo: 2, Title: "登录页", CreatorMemberID: 1, AssigneeMemberID: 2, Status: group_entity.TaskStatusOpen},
			{ID: 22, GroupID: 5, TaskNo: 9, Title: "旧任务", CreatorMemberID: 1, AssigneeMemberID: 3, Status: group_entity.TaskStatusDone},
			{ID: 23, GroupID: 5, TaskNo: 3, Title: "登录页验收", CreatorMemberID: 1, AssigneeMemberID: 3, Status: group_entity.TaskStatusOpen, ParentTaskNo: 2},
		}, nil)

		suffix := newSvc().buildGroupSystemPrompt(g, members, members[0])
		So(suffix, ShouldContainSubstring, "group_task_create")
		So(suffix, ShouldContainSubstring, "产品开发流程")           // SOP 注入(仅主持人)
		So(suffix, ShouldContainSubstring, "#2")               // 任务快照
		So(suffix, ShouldContainSubstring, "前端工程师")            // 快照 assignee 显示名
		So(suffix, ShouldContainSubstring, "（验证 #2）")          // 验证卡回指渲染
		So(suffix, ShouldNotContainSubstring, "#9")            // done 卡不进快照
		So(suffix, ShouldContainSubstring, ".agentre/handoff") // 交付物约定
	})

	Convey("成员视角:无 SOP + 含交付纪律 + 快照只含与自己相关的卡", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		// 注册无 EXPECT 的 workflow mock:成员路径不许碰 SOP 仓储(触发即 unexpected call)。
		workflow_repo.RegisterWorkflow(mock_workflow_repo.NewMockWorkflowRepo(ctrl))
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return([]*group_entity.GroupTask{
			{ID: 21, GroupID: 5, TaskNo: 2, Title: "登录页", CreatorMemberID: 1, AssigneeMemberID: 2, Status: group_entity.TaskStatusOpen},
			{ID: 23, GroupID: 5, TaskNo: 4, Title: "数据库迁移", CreatorMemberID: 1, AssigneeMemberID: 3, Status: group_entity.TaskStatusOpen},
			{ID: 24, GroupID: 5, TaskNo: 5, Title: "接口联调验证", CreatorMemberID: 2, AssigneeMemberID: 3, Status: group_entity.TaskStatusOpen},
		}, nil)

		suffix := newSvc().buildGroupSystemPrompt(g, members, members[1]) // me = 前端工程师(member 2)
		So(suffix, ShouldContainSubstring, "group_task_complete")
		So(suffix, ShouldContainSubstring, "不要再额外 group_send 重复汇报") // 完成汇报单通道纪律
		So(suffix, ShouldNotContainSubstring, "产品开发流程")             // SOP 仅主持人
		So(suffix, ShouldContainSubstring, "#2")                    // 自己是 assignee
		So(suffix, ShouldContainSubstring, "#5")                    // 自己是 creator
		So(suffix, ShouldNotContainSubstring, "#4")                 // 他人卡不可见
	})
}
