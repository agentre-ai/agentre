package app

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/model/entity/group_entity"
	"agentre/internal/service/group_svc"
)

// fakeGroupSvc 嵌入 group_svc.GroupSvc(nil 接口)以满足全部 12 个方法,
// 只覆写本测试需要的几个;未覆写方法在本测试中永不被调用。
type fakeGroupSvc struct {
	group_svc.GroupSvc
	detail    *group_svc.GroupDetail
	sentReq   *group_svc.SendGroupMessageRequest
	pinnedID  int64
	pinnedVal bool
}

func (f *fakeGroupSvc) LoadGroup(_ context.Context, _ int64) (*group_svc.GroupDetail, error) {
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
			Group:   &group_entity.Group{ID: 5, Title: "队", RunStatus: "running", RoundCount: 3, Createtime: 11, Updatetime: 22},
			Members: []*group_entity.GroupMember{{ID: 1, AgentID: 2, BackingSessionID: 9, Role: "coordinator", Status: "active"}},
			Messages: []*group_entity.GroupMessage{{
				ID: 7, Seq: 1, SenderKind: "agent", SenderMemberID: 1,
				RecipientMemberIDs: "[2,3]", ToUser: true, Content: "hi", Createtime: 33,
			}},
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
		So(resp.Members[0].Role, ShouldEqual, "coordinator")
		So(resp.Members[0].AgentID, ShouldEqual, 2)
		So(resp.Members[0].BackingSessionID, ShouldEqual, 9)
		So(resp.Messages[0].RecipientMemberIDs, ShouldResemble, []int64{2, 3})
		So(resp.Messages[0].ToUser, ShouldBeTrue)
		So(resp.Messages[0].Content, ShouldEqual, "hi")
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
