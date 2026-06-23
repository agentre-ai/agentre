package claudecode

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/pkg/claudecode"
)

// TestHandleControlRequest_BypassExitPlanMode 回归「bypass 快照吞掉计划审批」:
// active.permissionMode 还停留在 bypassPermissions(快照失同步,或会话真在 bypass)
// 时,ExitPlanMode 的 control_request 被短路自动 allow,前端永远看不到计划审批卡,
// 然后 CLI 自切 mode(实测 acceptEdits)—— 全程没有用户审批。
//
// 约定:计划审批是流程门禁不是工具权限,bypassPermissions 短路对 ExitPlanMode
// 永远不生效 —— 必须注册 permWaiter 并 emit ToolPermissionRequest 走审批卡。
func TestHandleControlRequest_BypassExitPlanMode(t *testing.T) {
	Convey("Given 快照=bypassPermissions, When ExitPlanMode control_request 到达, Then 不自动放行而是发起审批", t, func() {
		active := &claudeActive{handle: &fakeCCHandle{}, permissionMode: "bypassPermissions"}
		out := make(chan agentruntime.Event, 1)

		handleControlRequest(&claudecode.ControlRequestEvent{
			RequestID: "req-plan-1",
			ToolName:  "ExitPlanMode",
			Input:     json.RawMessage(`{"plan":"# Plan\n1. do it"}`),
		}, active, out)

		So(active.takePermWaiter("req-plan-1"), ShouldNotBeNil)
		var got agentruntime.Event
		select {
		case got = <-out:
		default:
		}
		perm, ok := got.(agentruntime.ToolPermissionRequest)
		So(ok, ShouldBeTrue)
		So(perm.RequestID, ShouldEqual, "req-plan-1")
		So(perm.ToolName, ShouldEqual, "ExitPlanMode")
	})

	Convey("Given 快照=bypassPermissions, When 普通工具 control_request 到达, Then 仍走短路自动 allow", t, func() {
		responded := make(chan claudecode.PermissionResult, 1)
		active := &claudeActive{handle: &fakeCCHandle{respondedResults: responded}, permissionMode: "bypassPermissions"}
		out := make(chan agentruntime.Event, 1)

		handleControlRequest(&claudecode.ControlRequestEvent{
			RequestID: "req-bash-1",
			ToolName:  "Bash",
			Input:     json.RawMessage(`{"command":"ls"}`),
		}, active, out)

		var res claudecode.PermissionResult
		select {
		case res = <-responded:
		case <-time.After(2 * time.Second):
			t.Fatal("auto-allow RespondToControl not called within 2s")
		}
		So(res.Behavior, ShouldEqual, "allow")
		So(active.takePermWaiter("req-bash-1"), ShouldBeNil)
		So(len(out), ShouldEqual, 0)
	})
}

// TestSetPermissionMode_SyncsActiveSnapshot 回归「快照失同步」根因:
// Runtime.SetPermissionMode 把切换发给 CLI 后不更新 active.permissionMode,
// bypass 会话轮间切到 plan 后快照仍是 bypassPermissions(CLI 的空闲 status 回显帧
// 被 demux reader 丢弃,复用进程的下一轮也不重发 mode),于是 ExitPlanMode /
// 工具审批照旧被 bypass 短路吞掉。
func TestSetPermissionMode_SyncsActiveSnapshot(t *testing.T) {
	Convey("Given 缓存的 claudeActive 快照=bypassPermissions, When SetPermissionMode(plan) 成功, Then 快照同步为 plan", t, func() {
		r := New()
		a := &claudeActive{handle: &fakeCCHandle{}, permissionMode: "bypassPermissions"}
		r.cache.Put(sessionKey(7), a)

		err := r.SetPermissionMode(context.Background(), 7, "plan")

		So(err, ShouldBeNil)
		So(a.permissionModeSnapshot(), ShouldEqual, "plan")
	})

	Convey("Given CLI 拒绝切 mode, When SetPermissionMode 失败, Then 快照保持原值", t, func() {
		r := New()
		a := &claudeActive{
			handle:         &fakeCCHandle{setPermissionModeErr: context.DeadlineExceeded},
			permissionMode: "bypassPermissions",
		}
		r.cache.Put(sessionKey(8), a)

		err := r.SetPermissionMode(context.Background(), 8, "plan")

		So(err, ShouldNotBeNil)
		So(a.permissionModeSnapshot(), ShouldEqual, "bypassPermissions")
	})
}
