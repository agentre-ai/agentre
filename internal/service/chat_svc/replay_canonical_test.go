package chat_svc

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/canonical"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
)

// 三个 entity→wire 转换 helper 在 Plan C 接管 replay 路径,落 cb.Canonical 让前端
// CanonicalToolRouter 与 live emit(dispatcher_emitter)走同一份卡片渲染逻辑。
func TestReplay_AskUserQuestionBlockSetsCanonicalUserAsk(t *testing.T) {
	Convey("askUserQuestionBlockToChatBlock 落 Canonical=UserAsk", t, func() {
		cb := askUserQuestionBlockToChatBlock(blocks.UserAskBlock{
			RequestID: "r-1",
			Questions: []blocks.AskQuestionDTO{{Question: "ok?"}},
			Answered:  true,
		})
		So(cb.Canonical, ShouldNotBeNil)
		So(string(cb.Canonical.Kind), ShouldEqual, "user.ask")
		So(cb.Canonical.UserAsk, ShouldNotBeNil)
		So(cb.Canonical.UserAsk.RequestID, ShouldEqual, "r-1")
		// 兼容 sidecar 同时存在(Plan C 收尾才删)。
		So(cb.AskUserQuestion, ShouldNotBeNil)
	})
}

// TestReplay_AskUserQuestionBlock_PreservesAnswered 回归 "重新进入会话后
// UserAskCard 又显示 WAITING" 的根因 #3: replay 路径同样在 canonical.UserAsk
// 字面量里漏了 Answered 字段(跟 dispatcher_emitter 同形态),即使 DB 落的是
// Answered=true,LoadChatSession → toChatMessage 投影出来的 canonical.userAsk.answered
// 永远是零值 false → 前端 StatusPill 永远翻回 "WAITING · 等待回复"。
func TestReplay_AskUserQuestionBlock_PreservesAnswered(t *testing.T) {
	Convey("已答状态在 replay 路径必须保留", t, func() {
		cb := askUserQuestionBlockToChatBlock(blocks.UserAskBlock{
			RequestID: "r-answered",
			Questions: []blocks.AskQuestionDTO{{Question: "ok?"}},
			Answered:  true,
			Answers:   []blocks.AskAnswerDTO{{QuestionIndex: 0, Labels: []string{"Y"}}},
		})
		So(cb.Canonical, ShouldNotBeNil)
		So(cb.Canonical.UserAsk, ShouldNotBeNil)
		So(cb.Canonical.UserAsk.Answered, ShouldBeTrue)
		So(cb.Canonical.UserAsk.Answers, ShouldHaveLength, 1)
	})

	Convey("跳过状态在 replay 路径必须保留", t, func() {
		cb := askUserQuestionBlockToChatBlock(blocks.UserAskBlock{
			RequestID: "r-skipped",
			Questions: []blocks.AskQuestionDTO{{Question: "ok?"}},
			Skipped:   true,
		})
		So(cb.Canonical.UserAsk.Skipped, ShouldBeTrue)
		So(cb.Canonical.UserAsk.Answered, ShouldBeFalse)
	})
}

func TestReplay_ToolPermissionExitPlanModeSetsCanonicalPlanApprove(t *testing.T) {
	Convey("toolPermissionBlockToChatBlock 对 ExitPlanMode 落 Canonical=PlanApproveRequest", t, func() {
		cb := toolPermissionBlockToChatBlock(blocks.ToolPermissionBlock{
			RequestID: "p-1",
			ToolName:  "ExitPlanMode",
			ToolInput: map[string]any{"plan": "## Plan\n- A\n"},
		})
		So(cb.Canonical, ShouldNotBeNil)
		So(string(cb.Canonical.Kind), ShouldEqual, "plan.approve_request")
		So(cb.Canonical.PlanApprove, ShouldNotBeNil)
		So(cb.Canonical.PlanApprove.PlanText, ShouldContainSubstring, "## Plan")
	})

	Convey("非 ExitPlanMode 落 Canonical=ToolPermission", t, func() {
		cb := toolPermissionBlockToChatBlock(blocks.ToolPermissionBlock{
			RequestID:   "p-2",
			ToolName:    "Bash",
			ToolInput:   map[string]any{"command": "rm -rf /"},
			Resolved:    true,
			Allowed:     true,
			AlwaysAllow: false,
		})
		So(cb.Canonical, ShouldNotBeNil)
		So(string(cb.Canonical.Kind), ShouldEqual, "tool.permission")
		So(cb.Canonical.ToolPermission, ShouldNotBeNil)
		So(cb.Canonical.ToolPermission.RequestID, ShouldEqual, "p-2")
		So(cb.Canonical.ToolPermission.ToolName, ShouldEqual, "Bash")
		So(cb.Canonical.ToolPermission.Resolved, ShouldBeTrue)
		So(cb.Canonical.ToolPermission.Allowed, ShouldBeTrue)
	})
}

func TestReplay_PlanBlockSetsCanonicalPlanUpdate(t *testing.T) {
	Convey("planBlockToChatBlock 落 Canonical=PlanUpdate", t, func() {
		cb := planBlockToChatBlock(PlanBlock{
			Steps: []PlanStepDTO{{Step: "first", Status: "completed"}, {Step: "second", Status: "inProgress"}},
		})
		So(cb.Canonical, ShouldNotBeNil)
		So(string(cb.Canonical.Kind), ShouldEqual, "plan.update")
		So(cb.Canonical.PlanUpdate, ShouldNotBeNil)
		So(cb.Canonical.PlanUpdate.Steps, ShouldHaveLength, 2)
		So(cb.Canonical.PlanUpdate.Steps[0].Step, ShouldEqual, "first")
		So(string(cb.Canonical.PlanUpdate.Steps[0].Status), ShouldEqual, "completed")
	})

	Convey("PlanBlock actions 回放到 Canonical=PlanUpdate", t, func() {
		cb := planBlockToChatBlock(PlanBlock{
			Text: "## Plan",
			Actions: []canonical.PlanAction{
				{ID: "plan.execute", Kind: canonical.PlanActionApprove},
				{ID: "plan.refine", Kind: canonical.PlanActionRefine, RequiresFeedback: true},
			},
		})
		So(cb.Canonical, ShouldNotBeNil)
		So(cb.Canonical.PlanUpdate, ShouldNotBeNil)
		So(cb.Canonical.PlanUpdate.Actions, ShouldHaveLength, 2)
		So(cb.Canonical.PlanUpdate.Actions[0].ID, ShouldEqual, "plan.execute")
	})
}
