package handlers

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/pkg/agentruntime"
	"agentre/internal/service/chat_svc/blocks"
	"agentre/internal/service/chat_svc/turn"
)

func TestUserAskRequestHandler_AddsBlock(t *testing.T) {
	Convey("UserAskRequest handler 入 acc + emit ask_user_question", t, func() {
		acc := turn.New()
		emit := &fakeEmit{}
		err := UserAskRequestHandler{}.Apply(context.Background(),
			agentruntime.UserAskRequest{
				RequestID:  "r-1",
				ToolCallID: "tu-x",
				Questions: []agentruntime.AskQuestion{
					{ID: "q1", Question: "ok?", Options: []agentruntime.AskOption{{Label: "Y"}}},
				},
			},
			acc, emit, nil, nil)
		So(err, ShouldBeNil)

		final := acc.Finalize()
		So(final, ShouldHaveLength, 1)
		got, ok := final[0].(*blocks.UserAskBlock)
		So(ok, ShouldBeTrue)
		So(got.RequestID, ShouldEqual, "r-1")
		So(got.ToolCallID, ShouldEqual, "tu-x")
		So(got.Questions, ShouldHaveLength, 1)

		p := emit.events[0].payload.(map[string]any)
		So(p["kind"], ShouldEqual, "ask_user_question")
		So(p["requestId"], ShouldEqual, "r-1")
	})
}

func TestUserAskResolvedHandler_MutatesAnswered(t *testing.T) {
	Convey("Resolved 通过 Mutate 改 Answered/Answers", t, func() {
		acc := turn.New()
		emit := &fakeEmit{}
		_ = UserAskRequestHandler{}.Apply(context.Background(),
			agentruntime.UserAskRequest{RequestID: "r-1"}, acc, emit, nil, nil)

		err := UserAskResolvedHandler{}.Apply(context.Background(),
			agentruntime.UserAskResolved{
				RequestID: "r-1",
				Answers:   []agentruntime.AskAnswer{{QuestionIndex: 0, Labels: []string{"Y"}}},
			},
			acc, emit, nil, nil)
		So(err, ShouldBeNil)

		final := acc.Finalize()
		got := final[0].(*blocks.UserAskBlock)
		So(got.Answered, ShouldBeTrue)
		So(got.Skipped, ShouldBeFalse)
		So(got.Answers, ShouldHaveLength, 1)
	})
}

func TestUserAskResolvedHandler_SkippedFlow(t *testing.T) {
	Convey("Skipped=true → Answered=false + Skipped=true", t, func() {
		acc := turn.New()
		_ = UserAskRequestHandler{}.Apply(context.Background(),
			agentruntime.UserAskRequest{RequestID: "r-2"}, acc, nil, nil, nil)

		err := UserAskResolvedHandler{}.Apply(context.Background(),
			agentruntime.UserAskResolved{RequestID: "r-2", Skipped: true},
			acc, nil, nil, nil)
		So(err, ShouldBeNil)

		got := acc.Finalize()[0].(*blocks.UserAskBlock)
		So(got.Answered, ShouldBeFalse)
		So(got.Skipped, ShouldBeTrue)
	})
}

// TestUserAskResolvedHandler_EmitsBlockPointer 回归 "提交后还显示等待回复" 的根因 #2:
// resolved handler 必须把 Mutate 完的 block 指针通过 "askUserQuestion" key 一起回灌,
// 跟 request handler 对称。否则 dispatcher_emitter 的 askUserQuestionFromMap 走 fallback,
// 拿不到 Questions/Answers,新 canonical 把前端 existing canonical 整体覆盖成 questions=null。
func TestUserAskResolvedHandler_EmitsBlockPointer(t *testing.T) {
	Convey("Resolved emit payload 带 askUserQuestion=block 指针,Answered/Questions 全状态可传递", t, func() {
		acc := turn.New()
		emit := &fakeEmit{}
		_ = UserAskRequestHandler{}.Apply(context.Background(),
			agentruntime.UserAskRequest{
				RequestID: "r-3",
				Questions: []agentruntime.AskQuestion{
					{ID: "q1", Question: "ok?", Options: []agentruntime.AskOption{{Label: "Y"}}},
				},
			}, acc, emit, nil, nil)

		emit.events = nil // 只关心 resolved 那帧
		err := UserAskResolvedHandler{}.Apply(context.Background(),
			agentruntime.UserAskResolved{
				RequestID: "r-3",
				Answers:   []agentruntime.AskAnswer{{QuestionIndex: 0, Labels: []string{"Y"}}},
			}, acc, emit, nil, nil)
		So(err, ShouldBeNil)
		So(emit.events, ShouldHaveLength, 1)

		p := emit.events[0].payload.(map[string]any)
		blk, ok := p["askUserQuestion"].(*blocks.UserAskBlock)
		So(ok, ShouldBeTrue)
		So(blk, ShouldNotBeNil)
		So(blk.RequestID, ShouldEqual, "r-3")
		So(blk.Answered, ShouldBeTrue)
		So(blk.Questions, ShouldHaveLength, 1)
		So(blk.Answers, ShouldHaveLength, 1)
	})
}
