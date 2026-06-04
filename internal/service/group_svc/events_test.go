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

	"agentre/internal/model/entity/group_entity"
	"agentre/internal/repository/group_repo"
	"agentre/internal/repository/group_repo/mock_group_repo"
	"agentre/internal/service/chat_svc"
	"agentre/internal/service/group_svc"
	"agentre/internal/service/group_svc/mock_group_svc"
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
			{ID: 1, GroupID: 5, AgentID: 1, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive},
			{ID: 2, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive},
		}, nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil).AnyTimes()
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
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
