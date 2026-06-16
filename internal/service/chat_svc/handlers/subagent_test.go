package handlers

import (
	"context"
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/pkg/agentruntime"
	"agentre/internal/service/chat_svc/blocks"
	"agentre/internal/service/chat_svc/turn"
)

func TestSubagentLifecycle(t *testing.T) {
	Convey("Started → Progress → Done 累计状态", t, func() {
		acc := turn.New()
		_ = SubagentStartedHandler{}.Apply(context.Background(),
			agentruntime.SubagentStarted{ToolCallID: "task-1"}, acc, nil, nil, nil)
		_ = SubagentProgressHandler{}.Apply(context.Background(),
			agentruntime.SubagentProgress{
				ToolCallID: "task-1",
				Info:       agentruntime.SubagentInfo{ToolUses: 3, LastToolName: "Read", TotalTokens: 1000},
			},
			acc, nil, nil, nil)
		_ = SubagentDoneHandler{}.Apply(context.Background(),
			agentruntime.SubagentDone{
				ToolCallID: "task-1",
				Info:       agentruntime.SubagentInfo{Status: "completed", DurationMs: 1234},
			},
			acc, nil, nil, nil)

		got := acc.Finalize()[0].(*blocks.SubagentStateBlock)
		So(got.Status, ShouldEqual, "completed")
		So(got.DurationMs, ShouldEqual, 1234)
	})
}

func TestSubagentStarted_PersistsKindAndDescription(t *testing.T) {
	Convey("SubagentStarted 落 kind/description + running", t, func() {
		acc := turn.New()
		err := SubagentStartedHandler{}.Apply(context.Background(),
			agentruntime.SubagentStarted{
				ToolCallID: "tu1",
				Info: agentruntime.SubagentInfo{
					Kind:            "local_bash",
					TaskDescription: "sleep 20",
				},
			}, acc, nil, nil, &turn.TurnContext{})
		So(err, ShouldBeNil)

		blks := acc.Finalize()
		So(blks, ShouldHaveLength, 1)
		sb := blks[0].(*blocks.SubagentStateBlock)
		So(sb.Kind, ShouldEqual, "local_bash")
		So(sb.Description, ShouldEqual, "sleep 20")
		So(sb.Status, ShouldEqual, "running")
	})
}

func TestSubagentDone_DefaultStatus(t *testing.T) {
	Convey("SubagentDone info.Status 空时默认 completed", t, func() {
		acc := turn.New()
		_ = SubagentStartedHandler{}.Apply(context.Background(),
			agentruntime.SubagentStarted{ToolCallID: "t-2"}, acc, nil, nil, nil)
		_ = SubagentDoneHandler{}.Apply(context.Background(),
			agentruntime.SubagentDone{ToolCallID: "t-2", Info: agentruntime.SubagentInfo{}},
			acc, nil, nil, nil)
		got := acc.Finalize()[0].(*blocks.SubagentStateBlock)
		So(got.Status, ShouldEqual, "completed")
	})
}

// MarkRunningSubagentsCancelled 是 turn abort 收尾的补救：用户 Stop 后 CLI 被
// interrupt → 不会再来 SubagentDone 事件,running 状态会被原样落 DB,前端 spin
// 不止。这里把 finalBlocks 里所有 *SubagentStateBlock.Status == "running" 的
// 改成 "canceled",已经 completed/failed 的不动。
func TestMarkRunningSubagentsCancelled(t *testing.T) {
	Convey("abort 时将 running 改成 canceled,其它终态不动", t, func() {
		acc := turn.New()
		_ = SubagentStartedHandler{}.Apply(context.Background(),
			agentruntime.SubagentStarted{ToolCallID: "running-1"}, acc, nil, nil, nil)
		_ = SubagentStartedHandler{}.Apply(context.Background(),
			agentruntime.SubagentStarted{ToolCallID: "done-1"}, acc, nil, nil, nil)
		_ = SubagentDoneHandler{}.Apply(context.Background(),
			agentruntime.SubagentDone{
				ToolCallID: "done-1",
				Info:       agentruntime.SubagentInfo{Status: "completed"},
			},
			acc, nil, nil, nil)

		final := acc.Finalize()
		MarkRunningSubagentsCancelled(final)

		var running, done *blocks.SubagentStateBlock
		for _, b := range final {
			sb, ok := b.(*blocks.SubagentStateBlock)
			if !ok {
				continue
			}
			switch sb.ParentToolCallID {
			case "running-1":
				running = sb
			case "done-1":
				done = sb
			}
		}
		So(running, ShouldNotBeNil)
		So(done, ShouldNotBeNil)
		So(running.Status, ShouldEqual, "canceled")
		So(done.Status, ShouldEqual, "completed")
	})

	Convey("空切片 / 无 SubagentStateBlock 不 panic", t, func() {
		MarkRunningSubagentsCancelled(nil)
		MarkRunningSubagentsCancelled([]cagoblocks.ContentBlock{})
		MarkRunningSubagentsCancelled([]cagoblocks.ContentBlock{
			&cagoblocks.TextBlock{Text: "hi"},
		})
	})
}
