package chat_svc

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/pkg/agentruntime/canonical"
	"agentre/internal/service/chat_svc/blocks"
)

type captureEmitter struct{ events []ChatStreamEvent }

func (c *captureEmitter) Emit(_ context.Context, _ string, payload any) {
	if ev, ok := payload.(ChatStreamEvent); ok {
		c.events = append(c.events, ev)
	}
}

func newTestDispatcherEmitter() (*dispatcherEmitter, *captureEmitter) {
	em := &captureEmitter{}
	svc := &chatSvc{emitter: em}
	return &dispatcherEmitter{svc: svc}, em
}

func TestDispatcherEmitter_Chunk(t *testing.T) {
	Convey("kind=chunk → ChatStreamEvent{Kind:chunk, Delta}", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{"kind": "chunk", "delta": "hi"})
		So(em.events, ShouldHaveLength, 1)
		So(em.events[0].Kind, ShouldEqual, StreamChunk)
		So(em.events[0].Delta, ShouldEqual, "hi")
	})
}

func TestDispatcherEmitter_ToolUse_PassthroughWithoutCanonical(t *testing.T) {
	Convey("kind=tool_use 无 canonical key → ev 透传 toolUseId/toolName/toolInput,Canonical=nil", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind":      "tool_use",
			"toolUseId": "tu-1",
			"toolName":  "Write",
			"toolInput": map[string]any{"file_path": "/tmp/a.txt", "content": "hi"},
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.Kind, ShouldEqual, StreamToolUse)
		So(ev.ToolUseID, ShouldEqual, "tu-1")
		So(ev.ToolName, ShouldEqual, "Write")
		So(ev.Canonical, ShouldBeNil) // 没有 canonical key → 不再用 toolUseToChatBlock 兜底
	})
}

func TestDispatcherEmitter_ToolUse_WithCanonical(t *testing.T) {
	Convey("kind=tool_use 携带 canonical → ev.Canonical 走 view.FromCanonical 转 DTO", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind":      "tool_use",
			"toolUseId": "tu-2",
			"toolName":  "Write",
			"toolInput": map[string]any{"file_path": "/tmp/x", "content": "hi"},
			"canonical": canonical.FileWrite{Path: "/tmp/x", Content: "hi"},
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.Canonical, ShouldNotBeNil)
		So(ev.Canonical.Kind, ShouldEqual, canonical.KindFileWrite)
		So(ev.Canonical.FileWrite, ShouldNotBeNil)
		So(ev.Canonical.FileWrite.Path, ShouldEqual, "/tmp/x")
	})

	Convey("kind=tool_use 无 canonical → ev.Canonical 为 nil(向后兼容)", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind":      "tool_use",
			"toolUseId": "tu-3",
			"toolName":  "Bash",
			"toolInput": map[string]any{"command": "ls"},
		})
		So(em.events, ShouldHaveLength, 1)
		So(em.events[0].Canonical, ShouldBeNil)
	})
}

func TestDispatcherEmitter_AskUserQuestion_FromBlockPtr(t *testing.T) {
	Convey("kind=ask_user_question 带原 block ptr → 投影到 ChatBlockAskUserQuestion", t, func() {
		de, em := newTestDispatcherEmitter()
		blk := &blocks.UserAskBlock{
			RequestID: "r-1",
			Answered:  true,
			Answers:   []blocks.AskAnswerDTO{{QuestionIndex: 0, Labels: []string{"A"}}},
		}
		de.Emit(context.Background(), "s", map[string]any{
			"kind":            "ask_user_question",
			"askUserQuestion": blk,
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.AskUserQuestion, ShouldNotBeNil)
		So(ev.AskUserQuestion.RequestID, ShouldEqual, "r-1")
		So(ev.AskUserQuestion.Answered, ShouldBeTrue)
		So(ev.AskUserQuestion.Answers, ShouldHaveLength, 1)
	})
}

func TestDispatcherEmitter_AskUserQuestion_AttachesCanonicalUserAsk(t *testing.T) {
	Convey("ask_user_question 同时填 ev.Canonical=UserAsk", t, func() {
		de, em := newTestDispatcherEmitter()
		blk := &blocks.UserAskBlock{
			RequestID: "r-A",
			Questions: []blocks.AskQuestionDTO{{Question: "ok?"}},
		}
		de.Emit(context.Background(), "s", map[string]any{
			"kind":            "ask_user_question",
			"askUserQuestion": blk,
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.Canonical, ShouldNotBeNil)
		So(string(ev.Canonical.Kind), ShouldEqual, "user.ask")
		So(ev.Canonical.UserAsk, ShouldNotBeNil)
		So(ev.Canonical.UserAsk.RequestID, ShouldEqual, "r-A")
	})
}

// TestDispatcherEmitter_AskUserQuestion_CanonicalPreservesAnswered 回归
// "提交后 UserAskCard 还显示 WAITING" 的根因 #1: dispatcher_emitter 构造
// canonical.UserAsk 字面量时漏了 Answered 字段,resolved 帧到前端时
// canonical.userAsk.answered 永远 false → UserAskCard 状态徽章翻不到 ANSWERED。
// Questions 也必须保留,否则 markAskUserQuestionAnswered 把 existing canonical
// 整体覆盖成 questions=null,UserAskCard 直接消失。
func TestDispatcherEmitter_AskUserQuestion_CanonicalPreservesAnswered(t *testing.T) {
	Convey("ask_user_question 携带 Answered+Questions+Answers 的 block ptr → canonical.UserAsk 全字段透传", t, func() {
		de, em := newTestDispatcherEmitter()
		blk := &blocks.UserAskBlock{
			RequestID: "r-B",
			Questions: []blocks.AskQuestionDTO{{Question: "ok?"}},
			Answered:  true,
			Answers:   []blocks.AskAnswerDTO{{QuestionIndex: 0, Labels: []string{"Y"}}},
		}
		de.Emit(context.Background(), "s", map[string]any{
			"kind":            "ask_user_question",
			"askUserQuestion": blk,
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.Canonical, ShouldNotBeNil)
		So(ev.Canonical.UserAsk, ShouldNotBeNil)
		So(ev.Canonical.UserAsk.Answered, ShouldBeTrue)
		So(ev.Canonical.UserAsk.Questions, ShouldNotBeNil)
		So(ev.Canonical.UserAsk.Answers, ShouldNotBeNil)
	})
}

// TestDispatcherEmitter_RuntimeStatus 中间 map 形态 → ChatStreamEvent{Kind:runtime_status,
// RuntimeStatus:{Status, Compacting}}。前端 chat-streams-host 据此切 typing indicator。
func TestDispatcherEmitter_RuntimeStatus(t *testing.T) {
	Convey("kind=runtime_status compacting → ev.RuntimeStatus 透传", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind":       "runtime_status",
			"status":     "compacting",
			"compacting": true,
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.Kind, ShouldEqual, StreamRuntimeStatus)
		So(ev.RuntimeStatus, ShouldNotBeNil)
		So(ev.RuntimeStatus.Status, ShouldEqual, "compacting")
		So(ev.RuntimeStatus.Compacting, ShouldBeTrue)
	})

	Convey("kind=runtime_status 其它 status compacting=false 仍透传", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind":       "runtime_status",
			"status":     "requesting",
			"compacting": false,
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.RuntimeStatus.Status, ShouldEqual, "requesting")
		So(ev.RuntimeStatus.Compacting, ShouldBeFalse)
	})
}

func TestDispatcherEmitter_PlanUpdate_AttachesCanonicalPlanUpdate(t *testing.T) {
	Convey("plan_update 携带 canonical → ev.Canonical=PlanUpdate", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind":  "plan_update",
			"delta": "# Plan\n- inspect",
			"canonical": canonical.PlanUpdate{
				Text: "# Plan\n- inspect",
				Steps: []canonical.PlanStep{
					{Step: "inspect", Status: canonical.StepInProgress},
				},
			},
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.Kind, ShouldEqual, StreamPlanUpdate)
		So(ev.Delta, ShouldEqual, "# Plan\n- inspect")
		So(ev.Canonical, ShouldNotBeNil)
		So(string(ev.Canonical.Kind), ShouldEqual, "plan.update")
		So(ev.Canonical.PlanUpdate, ShouldNotBeNil)
		So(ev.Canonical.PlanUpdate.Steps, ShouldHaveLength, 1)
	})
}

func TestDispatcherEmitter_ToolPermission_ExitPlanMode_AttachesPlanApprove(t *testing.T) {
	Convey("tool_permission_request 的 ExitPlanMode 工具填 ev.Canonical=PlanApproveRequest", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind": "tool_permission_request",
			"toolPermission": &blocks.ToolPermissionBlock{
				RequestID: "p-1",
				ToolName:  "ExitPlanMode",
				ToolInput: map[string]any{"plan": "## Plan\n- a\n- b\n"},
			},
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.Canonical, ShouldNotBeNil)
		So(string(ev.Canonical.Kind), ShouldEqual, "plan.approve_request")
		So(ev.Canonical.PlanApprove, ShouldNotBeNil)
		So(ev.Canonical.PlanApprove.RequestID, ShouldEqual, "p-1")
		So(ev.Canonical.PlanApprove.PlanText, ShouldContainSubstring, "## Plan")
	})

	Convey("tool_permission_request 非 ExitPlanMode 工具填 ev.Canonical=ToolPermission", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind": "tool_permission_request",
			"toolPermission": &blocks.ToolPermissionBlock{
				RequestID: "p-2",
				ToolName:  "Bash",
				ToolInput: map[string]any{"command": "rm -rf /"},
			},
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.Canonical, ShouldNotBeNil)
		So(string(ev.Canonical.Kind), ShouldEqual, "tool.permission")
		So(ev.Canonical.ToolPermission, ShouldNotBeNil)
		So(ev.Canonical.ToolPermission.RequestID, ShouldEqual, "p-2")
		So(ev.Canonical.ToolPermission.ToolName, ShouldEqual, "Bash")
	})

	// resolved 阶段 handler 现在带 toolPermission block,dispatcher_emitter 应据此
	// 仍然把 ExitPlanMode 路由到 PlanApproveRequest canonical(含 planText),
	// 否则前端 markToolPermissionResolved 拿到空 tool.permission canonical 会
	// 覆盖掉原本的 PlanApproveCard,显示成空白 ToolPermissionCard。
	Convey("resolved ExitPlanMode 仍走 PlanApproveRequest canonical 并保留 planText", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind":      "tool_permission_request",
			"requestId": "p-3",
			"toolName":  "ExitPlanMode",
			"toolInput": map[string]any{"plan": "## Plan\n- step 1"},
			"resolved":  true,
			"allowed":   true,
			"toolPermission": &blocks.ToolPermissionBlock{
				RequestID: "p-3",
				ToolName:  "ExitPlanMode",
				ToolInput: map[string]any{"plan": "## Plan\n- step 1"},
				Resolved:  true,
				Allowed:   true,
			},
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.Canonical, ShouldNotBeNil)
		So(string(ev.Canonical.Kind), ShouldEqual, "plan.approve_request")
		So(ev.Canonical.PlanApprove, ShouldNotBeNil)
		So(ev.Canonical.PlanApprove.PlanText, ShouldContainSubstring, "## Plan")
		So(ev.Canonical.PlanApprove.Resolved, ShouldBeTrue)
		So(ev.Canonical.PlanApprove.Allowed, ShouldBeTrue)
		So(ev.ToolPermission, ShouldNotBeNil)
		So(ev.ToolPermission.ToolName, ShouldEqual, "ExitPlanMode")
	})
}

func TestDispatcherEmitter_Subagent_AttachesAgentSpawn(t *testing.T) {
	Convey("subagent_started 填 ev.Canonical=AgentSpawn (基础元数据)", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind":      "subagent_started",
			"toolUseId": "task-1",
			"info": map[string]any{
				"taskId":          "task-1",
				"subagentType":    "general-purpose",
				"taskDescription": "find bug",
				"prompt":          "explore",
				"status":          "running",
			},
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.Canonical, ShouldNotBeNil)
		So(string(ev.Canonical.Kind), ShouldEqual, "agent.spawn")
		So(ev.Canonical.AgentSpawn, ShouldNotBeNil)
		So(ev.Canonical.AgentSpawn.TaskID, ShouldEqual, "task-1")
		So(ev.Canonical.AgentSpawn.SubagentType, ShouldEqual, "general-purpose")
	})
}

func TestDispatcherEmitter_Usage(t *testing.T) {
	Convey("kind=usage → ChatStreamUsage 含 totalInputTokens", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind": "usage",
			"usage": map[string]any{
				"promptTokens":     100,
				"totalInputTokens": 130,
			},
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.Kind, ShouldEqual, StreamUsage)
		So(ev.Usage, ShouldNotBeNil)
		So(ev.Usage.PromptTokens, ShouldEqual, 100)
		So(ev.Usage.TotalInputTokens, ShouldEqual, 130)
	})
}

func TestDispatcherEmitter_SessionStatus_ContextWindow(t *testing.T) {
	Convey("kind=session_status 携带 contextWindow patch", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind":          "session_status",
			"sessionStatus": map[string]any{"contextWindow": 200000},
		})
		So(em.events, ShouldHaveLength, 1)
		ev := em.events[0]
		So(ev.SessionStatus, ShouldNotBeNil)
		So(ev.SessionStatus.ContextWindow, ShouldEqual, 200000)
	})
}

func TestDispatcherEmitter_MessageEndDropped(t *testing.T) {
	Convey("kind=message_end (handler 中间) 由 chat_svc 收尾接管, dispatcher emitter 丢弃", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{"kind": "message_end"})
		So(em.events, ShouldHaveLength, 0)
	})
}

func TestDispatcherEmitter_UnknownKindDropped(t *testing.T) {
	Convey("未识别 kind 丢弃(forward-compat)", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{"kind": "future-thing"})
		So(em.events, ShouldHaveLength, 0)
	})
}
