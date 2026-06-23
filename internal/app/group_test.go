package app

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/service/group_svc"
)

// fakeGroupSvc 嵌入 group_svc.GroupSvc(nil 接口)以满足全部 12 个方法,
// 只覆写本测试需要的几个;未覆写方法在本测试中永不被调用。
type fakeGroupSvc struct {
	group_svc.GroupSvc
	detail     *group_svc.GroupDetail
	createdReq *group_svc.CreateGroupRequest
	sentReq    *group_svc.SendGroupMessageRequest
	pinnedID   int64
	pinnedVal  bool
}

func (f *fakeGroupSvc) LoadGroup(_ context.Context, _ int64) (*group_svc.GroupDetail, error) {
	return f.detail, nil
}

func (f *fakeGroupSvc) CreateGroup(_ context.Context, req *group_svc.CreateGroupRequest) (*group_svc.GroupDetail, error) {
	f.createdReq = req
	return f.detail, nil
}

func (f *fakeGroupSvc) SendGroupMessage(_ context.Context, req *group_svc.SendGroupMessageRequest) error {
	f.sentReq = req
	return nil
}

func (f *fakeGroupSvc) SetGroupPinned(_ context.Context, id int64, pinned bool) error {
	f.pinnedID, f.pinnedVal = id, pinned
	return nil
}

func TestApp_GroupLoad_MapsDetail(t *testing.T) {
	Convey("GroupLoad 应把 svc detail 映射为 DTO", t, func() {
		prev := group_svc.Default()
		defer group_svc.SetDefault(prev)
		group_svc.SetDefault(&fakeGroupSvc{detail: &group_svc.GroupDetail{
			Group:   &group_entity.Group{ID: 5, Title: "队", RunStatus: "running", RoundCount: 3, ProjectID: 42, Createtime: 11, Updatetime: 22},
			Members: []*group_entity.GroupMember{{ID: 1, AgentID: 2, BackingSessionID: 9, Role: "host", Status: "active"}},
			Messages: []*group_entity.GroupMessage{{
				ID: 7, Seq: 1, SenderKind: "agent", SenderMemberID: 1,
				RecipientMemberIDs: "[2,3]", ToUser: true, Content: "hi", Createtime: 33,
			}},
			MemberRunStates: map[int64]string{1: "running"},
		}})
		a := &App{ctx: context.Background()}
		resp, err := a.GroupLoad(5)
		So(err, ShouldBeNil)
		So(resp.Group.ID, ShouldEqual, 5)
		So(resp.Group.Title, ShouldEqual, "队")
		So(resp.Group.RunStatus, ShouldEqual, "running")
		So(resp.Group.RoundCount, ShouldEqual, 3)
		So(resp.Group.Createtime, ShouldEqual, 11)
		So(resp.Group.Updatetime, ShouldEqual, 22)
		So(resp.Group.ProjectID, ShouldEqual, 42)
		So(resp.Members[0].Role, ShouldEqual, "host")
		So(resp.Members[0].AgentID, ShouldEqual, 2)
		So(resp.Members[0].BackingSessionID, ShouldEqual, 9)
		So(resp.Members[0].RunState, ShouldEqual, "running")
		So(resp.Messages[0].RecipientMemberIDs, ShouldResemble, []int64{2, 3})
		So(resp.Messages[0].ToUser, ShouldBeTrue)
		So(resp.Messages[0].Content, ShouldEqual, "hi")
	})
}

func TestApp_GroupCreate_PassesWorkflowID(t *testing.T) {
	Convey("GroupCreate 应把 WorkflowID 透传到 svc", t, func() {
		prev := group_svc.Default()
		defer group_svc.SetDefault(prev)
		fake := &fakeGroupSvc{detail: &group_svc.GroupDetail{Group: &group_entity.Group{ID: 5}}}
		group_svc.SetDefault(fake)
		a := &App{ctx: context.Background()}
		_, err := a.GroupCreate(&GroupCreateRequest{Title: "队", HostAgentID: 1, WorkflowID: 3})
		So(err, ShouldBeNil)
		So(fake.createdReq, ShouldNotBeNil)
		So(fake.createdReq.WorkflowID, ShouldEqual, 3)
	})
}

func TestApp_GroupLoad_MapsTasksAndTaskFields(t *testing.T) {
	Convey("GroupLoad 应映射 Tasks 与消息的 TaskID/TaskEvent", t, func() {
		prev := group_svc.Default()
		defer group_svc.SetDefault(prev)
		group_svc.SetDefault(&fakeGroupSvc{detail: &group_svc.GroupDetail{
			Group:    &group_entity.Group{ID: 5},
			Messages: []*group_entity.GroupMessage{{ID: 7, TaskID: 77, TaskEvent: "created"}},
			Tasks: []*group_entity.GroupTask{{
				ID: 77, TaskNo: 1, Title: "写测试", Brief: "补 e2e", CreatorMemberID: 1, AssigneeMemberID: 2,
				Status: group_entity.TaskStatusDone, Result: "改了 X,自测过", ParentTaskNo: 2, Createtime: 11, Updatetime: 22,
			}},
		}})
		a := &App{ctx: context.Background()}
		resp, err := a.GroupLoad(5)
		So(err, ShouldBeNil)
		So(resp.Tasks, ShouldHaveLength, 1)
		So(resp.Tasks[0].ID, ShouldEqual, 77)
		So(resp.Tasks[0].TaskNo, ShouldEqual, 1)
		So(resp.Tasks[0].Title, ShouldEqual, "写测试")
		So(resp.Tasks[0].Brief, ShouldEqual, "补 e2e")
		So(resp.Tasks[0].CreatorMemberID, ShouldEqual, 1)
		So(resp.Tasks[0].AssigneeMemberID, ShouldEqual, 2)
		So(resp.Tasks[0].Status, ShouldEqual, group_entity.TaskStatusDone)
		So(resp.Tasks[0].Result, ShouldEqual, "改了 X,自测过")
		So(resp.Tasks[0].ParentTaskNo, ShouldEqual, 2)
		So(resp.Tasks[0].Createtime, ShouldEqual, 11)
		So(resp.Tasks[0].Updatetime, ShouldEqual, 22)
		So(resp.Messages[0].TaskID, ShouldEqual, 77)
		So(resp.Messages[0].TaskEvent, ShouldEqual, "created")
	})
}

func TestApp_GroupSend_PassesThrough(t *testing.T) {
	Convey("GroupSend 应原样透传字段到 svc", t, func() {
		prev := group_svc.Default()
		defer group_svc.SetDefault(prev)
		fake := &fakeGroupSvc{}
		group_svc.SetDefault(fake)
		a := &App{ctx: context.Background()}
		err := a.GroupSend(&GroupSendRequest{GroupID: 5, Text: "hi", RecipientMemberIDs: []int64{2}, ToUser: true})
		So(err, ShouldBeNil)
		So(fake.sentReq, ShouldNotBeNil)
		So(fake.sentReq.GroupID, ShouldEqual, 5)
		So(fake.sentReq.Text, ShouldEqual, "hi")
		So(fake.sentReq.RecipientMemberIDs, ShouldResemble, []int64{2})
		So(fake.sentReq.ToUser, ShouldBeTrue)
	})
}

func TestApp_GroupSetPinned_PassesThrough(t *testing.T) {
	Convey("GroupSetPinned 应透传到 svc", t, func() {
		prev := group_svc.Default()
		defer group_svc.SetDefault(prev)
		fake := &fakeGroupSvc{}
		group_svc.SetDefault(fake)
		a := &App{ctx: context.Background()}
		So(a.GroupSetPinned(5, true), ShouldBeNil)
		So(fake.pinnedID, ShouldEqual, 5)
		So(fake.pinnedVal, ShouldBeTrue)
	})
}

func TestApp_GroupItem_CarriesPinned(t *testing.T) {
	Convey("toGroupItem 应带出 Pinned", t, func() {
		So(toGroupItem(&group_entity.Group{ID: 5, Pinned: true}).Pinned, ShouldBeTrue)
		So(toGroupItem(&group_entity.Group{ID: 6}).Pinned, ShouldBeFalse)
	})
}

func TestApp_GroupItem_CarriesProjectID(t *testing.T) {
	Convey("toGroupItem 应带出 ProjectID(供前端 roster 解析项目名 + 点击跳转)", t, func() {
		So(toGroupItem(&group_entity.Group{ID: 5, ProjectID: 7}).ProjectID, ShouldEqual, 7)
		So(toGroupItem(&group_entity.Group{ID: 6}).ProjectID, ShouldEqual, 0)
	})
}
