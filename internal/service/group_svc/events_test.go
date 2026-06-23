package group_svc_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc/mock_group_svc"
)

// captureEmitter 记录最后一次 "message" 事件的 payload(线程安全, 防调度器 goroutine -race)。
type captureEmitter struct {
	mu  sync.Mutex
	msg any
}

func (c *captureEmitter) Emit(_ context.Context, _ string, payload any) {
	m, ok := payload.(map[string]any)
	if !ok {
		return
	}
	if m["kind"] != "message" {
		return
	}
	c.mu.Lock()
	c.msg = m["message"]
	c.mu.Unlock()
}

func (c *captureEmitter) message() any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.msg
}

// TestPersistMessage_EmitsFrontendIsomorphicPayload 锁住 live 消息事件 JSON 形状:
// 必须与 app.GroupMessageItem 同构 —— lowercase 键 + recipientMemberIDs 为 number 数组。
// 若有人退回裸发 *group_entity.GroupMessage(只有 gorm tag), 本测试会因
// "recipientMemberIDs":[2] 缺失 / 出现首字母大写键 / "[2]" 字符串而失败。
func TestPersistMessage_EmitsFrontendIsomorphicPayload(t *testing.T) {
	Convey("用户发消息 → emit 的 message 事件 JSON 与前端 DTO 同构", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunIdle, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return([]*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive},
			{ID: 2, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive},
		}, nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil).AnyTimes()
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		// launchDelivery → buildGroupSystemPrompt → openTaskSnapshot 读任务卡(本测试无任务)。
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		ch12 := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(12)).Return((<-chan chat_svc.TurnResult)(ch12), func() {}).AnyTimes()
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).Return(&chat_svc.SendResponse{}, nil).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "后端"})
		rec := &captureEmitter{}
		group_svc.SetEmitterForTest(svc, rec)

		err := svc.SendGroupMessage(ctx, &group_svc.SendGroupMessageRequest{GroupID: 5, Text: "麻烦后端看下", RecipientMemberIDs: []int64{2}})
		So(err, ShouldBeNil)

		payload := rec.message()
		So(payload, ShouldNotBeNil)
		b, err := json.Marshal(payload)
		So(err, ShouldBeNil)
		js := string(b)

		// recipientMemberIDs 必须是 number 数组, 不是字符串。
		So(strings.Contains(js, `"recipientMemberIDs":[2]`), ShouldBeTrue)
		// lowercase 键(与 app.GroupMessageItem 同构)。
		So(strings.Contains(js, `"senderKind":`), ShouldBeTrue)
		So(strings.Contains(js, `"toUser":`), ShouldBeTrue)
		So(strings.Contains(js, `"createtime":`), ShouldBeTrue)
		// 绝不能是裸实体(首字母大写键 / "[2]" 字符串 / gorm 内部字段)。
		So(strings.Contains(js, `"RecipientMemberIDs"`), ShouldBeFalse)
		So(strings.Contains(js, `"[2]"`), ShouldBeFalse)
		So(strings.Contains(js, `"GroupID"`), ShouldBeFalse)
	})
}

// captureTaskEmitter 记录最后一次 "task_updated" 事件的 task 载荷(线程安全, 防调度器 goroutine -race)。
type captureTaskEmitter struct {
	mu   sync.Mutex
	task any
}

func (c *captureTaskEmitter) Emit(_ context.Context, _ string, payload any) {
	m, ok := payload.(map[string]any)
	if !ok || m["kind"] != "task_updated" {
		return
	}
	c.mu.Lock()
	c.task = m["task"]
	c.mu.Unlock()
}

func (c *captureTaskEmitter) lastTask() any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.task
}

// TestHandleTaskCreate_EmitsTaskUpdatedIsomorphicPayload 锁住两件事(先例:上方消息事件同构测试):
// ① HandleTaskCreate 确实发出 kind=task_updated 事件;
// ② GroupTaskEvent 的 JSON 形状与 app.GroupTaskItem 同构 —— lowercase 键 11 个字段全钉。
// 若有人退回裸发 *group_entity.GroupTask(只有 gorm tag), 本测试会因首字母大写键 / 多出 "GroupID" 而失败。
func TestHandleTaskCreate_EmitsTaskUpdatedIsomorphicPayload(t *testing.T) {
	Convey("建任务卡 → emit 的 task_updated 事件 JSON 与前端 DTO 同构", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, msgRepo, taskRepo := registerTaskMocks(t, ctrl)

		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE, RunStatus: group_entity.RunIdle}
		host := &group_entity.GroupMember{ID: 1, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
		dev := &group_entity.GroupMember{ID: 2, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(1)).Return(host, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupMember{host, dev}, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		taskRepo.EXPECT().NextTaskNo(gomock.Any(), int64(5)).Return(3, nil)
		taskRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, x *group_entity.GroupTask) error {
				x.ID = 77 // 事件必须携带 DB 落库后的 id
				return nil
			})
		taskRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil).AnyTimes()
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		ch12 := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(12)).Return((<-chan chat_svc.TurnResult)(ch12), func() {}).AnyTimes()
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).Return(&chat_svc.SendResponse{}, nil).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "后端"})
		rec := &captureTaskEmitter{}
		group_svc.SetEmitterForTest(svc, rec)

		created, err := svc.HandleTaskCreate(ctx, 1, "后端", "写测试", "补 e2e", 2)
		So(err, ShouldBeNil)
		So(created, ShouldNotBeNil)

		payload := rec.lastTask()
		So(payload, ShouldNotBeNil) // ① 事件确实发出
		b, err := json.Marshal(payload)
		So(err, ShouldBeNil)
		js := string(b)

		// ② lowercase 键与 app.GroupTaskItem 的 json tag 一一对应(11 个字段全钉)。
		So(strings.Contains(js, `"id":77`), ShouldBeTrue)
		So(strings.Contains(js, `"taskNo":3`), ShouldBeTrue)
		So(strings.Contains(js, `"title":"写测试"`), ShouldBeTrue)
		So(strings.Contains(js, `"brief":"补 e2e"`), ShouldBeTrue)
		So(strings.Contains(js, `"creatorMemberID":1`), ShouldBeTrue)
		So(strings.Contains(js, `"assigneeMemberID":2`), ShouldBeTrue)
		So(strings.Contains(js, `"status":"open"`), ShouldBeTrue)
		So(strings.Contains(js, `"result":""`), ShouldBeTrue)
		So(strings.Contains(js, `"parentTaskNo":2`), ShouldBeTrue)
		So(strings.Contains(js, `"createtime":`), ShouldBeTrue)
		So(strings.Contains(js, `"updatetime":`), ShouldBeTrue)
		// 绝不能是裸实体(首字母大写键 / gorm 才有的 GroupID)。
		So(strings.Contains(js, `"TaskNo"`), ShouldBeFalse)
		So(strings.Contains(js, `"GroupID"`), ShouldBeFalse)
	})
}
