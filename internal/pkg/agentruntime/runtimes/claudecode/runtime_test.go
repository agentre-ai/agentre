package claudecode

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/pkg/claudecode"
)

// TestClaudeCodeCapabilities 钉死 claudecode runtime 的能力矩阵 + permission
// mode 元数据。这些值与 chat_svc / 前端 UI gating 的硬编码 switch 一一对应,
// 任何一项偏移都意味着 Plan B 切 dispatcher 后会有 UI/dispatch 错乱。
//
// 历史:旧实现通过"是否实现 Steerer/Aborter/SetPermissionMode/SubmitAnswer"
// 接口隐式声明能力 + 把 mode 白名单硬编码在 chat_svc。Plan A 把这些 facts
// 都收编到 Capabilities() 一个返回值里(spec §5.4)。
func TestClaudeCodeCapabilities(t *testing.T) {
	Convey("claudecode Capabilities 矩阵", t, func() {
		r := New()
		caps := r.Capabilities()
		So(caps.Has(capability.CapSteer), ShouldBeTrue)
		So(caps.Has(capability.CapCancelSteer), ShouldBeTrue)
		So(caps.Has(capability.CapDrainSteer), ShouldBeTrue)
		So(caps.Has(capability.CapAbort), ShouldBeTrue)
		So(caps.Has(capability.CapSetPermission), ShouldBeTrue)
		So(caps.Has(capability.CapAnswerUserAsk), ShouldBeTrue)
		So(caps.Has(capability.CapToolPermission), ShouldBeTrue)
		So(caps.Has(capability.CapForkSession), ShouldBeTrue)
		So(caps.Has(capability.CapReportContextWindow), ShouldBeFalse)
	})

	Convey("claudecode PermissionModeMeta", t, func() {
		caps := New().Capabilities()
		So(caps.PermissionModeMeta.AllowedModes, ShouldResemble, []string{
			"default", "acceptEdits", "plan", "bypassPermissions",
		})
		So(caps.PermissionModeMeta.DefaultMode, ShouldEqual, "acceptEdits")
		So(caps.PermissionModeMeta.SwitchableDuringTurn, ShouldBeTrue)
		So(caps.PermissionModeMeta.Order, ShouldResemble, []string{
			"default", "acceptEdits", "plan", "bypassPermissions",
		})
		// LaunchDefaultMode="":pkg/claudecode args.go 不附 --permission-mode 时
		// 内部兜底 acceptEdits;chat_svc 用此值区分"用户未显式选"vs"explicitly acceptEdits"。
		So(caps.PermissionModeMeta.LaunchDefaultMode, ShouldEqual, "")
	})
}

// fakeCCHandle 是 ccSessionHandle 的最简 stub:Stream 返回一个立即 close 的
// 空事件流;其他控制方法 no-op。仅供 Run() 路径不需要真实 CLI 子进程的单测。
//
// setPermissionModeCalls 记录 SetPermissionMode 被调用时收到的 mode 序列, 单测
// 用来断言 spawn-after 同步逻辑;setPermissionModeErr 让单测构造"CLI 拒绝切 mode"
// 场景验证 Run() 不应把这条错误冒泡。
type fakeCCHandle struct {
	id                     string
	setPermissionModeCalls []string
	setPermissionModeErr   error
}

func (f *fakeCCHandle) ID() string                      { return f.id }
func (f *fakeCCHandle) Close(context.Context) error     { return nil }
func (f *fakeCCHandle) Interrupt(context.Context) error { return nil }
func (f *fakeCCHandle) SetPermissionMode(_ context.Context, mode string) error {
	f.setPermissionModeCalls = append(f.setPermissionModeCalls, mode)
	return f.setPermissionModeErr
}
func (f *fakeCCHandle) RespondToControl(context.Context, string, claudecode.PermissionResult) error {
	return nil
}
func (f *fakeCCHandle) ExitErr() error { return nil }
func (f *fakeCCHandle) Stream(context.Context, string) (ccStream, error) {
	return &fakeCCStream{}, nil
}

type fakeCCStream struct{}

func (s *fakeCCStream) Next() bool              { return false }
func (s *fakeCCStream) Event() claudecode.Event { return claudecode.Event{} }
func (s *fakeCCStream) SessionID() string       { return "" }

// TestRun_NoChatRepoRegistered 回归 daemon 路径下的 nil panic:
// agentred daemon 不 bootstrap cago/chat_repo,Runtime.acquireSession 旧实现
// 直接调 chat_repo.Session().UpdatePermissionModeAtLaunch,在 chat_repo 未
// RegisterSession 的进程里(daemon)会触发 nil pointer dereference。
//
// 修法:runtime 不再直接写 repository,把 resolvedLaunchMode 通过
// RunResult.LaunchPermissionMode 同步回吐,由 chat_svc(只在主进程跑)落库。
func TestRun_NoChatRepoRegistered(t *testing.T) {
	Convey("daemon 路径 (chat_repo 未注册) Run 不应 panic", t, func() {
		restore := SetSessionFactoryForTest(func(ccLaunchSpec) (ccSessionHandle, error) {
			return &fakeCCHandle{id: "fake-sid"}, nil
		})
		defer restore()

		r := New()
		ctx := context.Background()
		events, result, err := r.Run(ctx, agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)},
			SessionID: 42,
			Cwd:       t.TempDir(),
			UserText:  "hello",
		})
		So(err, ShouldBeNil)
		// LaunchPermissionMode 由 acquireSession 在 spawn 路径同步赋值,
		// goroutine 启动后再也不写它 —— 单字段读是 race-free 的。先拍快照再
		// drain 避免 Convey 的 ShouldNotBeNil 走 reflect 扫整个 result 触发
		// 与 drain goroutine 的 race(后者在 0-frame 兜底里写 StopErr)。
		launchMode := result.LaunchPermissionMode

		for range events {
		}
		So(launchMode, ShouldEqual, "")
		// drain 完成后 Close 释放 cache 让 t.Cleanup 不留 fd。
		r.CloseAllSessions(ctx)
	})

	Convey("daemon 路径 backend 有 default permission mode → LaunchPermissionMode 回填", t, func() {
		restore := SetSessionFactoryForTest(func(ccLaunchSpec) (ccSessionHandle, error) {
			return &fakeCCHandle{id: "fake-sid"}, nil
		})
		defer restore()

		r := New()
		ctx := context.Background()
		events, result, err := r.Run(ctx, agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:                  string(agent_backend_entity.TypeClaudeCode),
				DefaultPermissionMode: "bypassPermissions",
			},
			SessionID: 43,
			Cwd:       t.TempDir(),
			UserText:  "hi",
		})
		So(err, ShouldBeNil)
		launchMode := result.LaunchPermissionMode
		for range events {
		}
		So(launchMode, ShouldEqual, "bypassPermissions")
		r.CloseAllSessions(ctx)
	})
}

// TestRun_SpawnAfterSetPermissionMode 锁住「stored mode != launch mode 时
// spawn 后自动校准 CLI」的不变量。「先 plan 后 bypass」工作流靠这条派生:
// backendDefault=bypass 强制 launch=bypass(resolveLaunchMode), 但 stored mode
// (req.PermissionMode) 是用户实际想要的初始 mode(默认 plan)。spawn 完成的瞬间
// 必须发一次 SetPermissionMode 把 CLI 切到 stored, 否则用户看到的 pill 是 Plan
// 但 CLI 还在 bypass 自动执行。
func TestRun_SpawnAfterSetPermissionMode(t *testing.T) {
	Convey("Given backendDefault=bypass + stored=plan, When Run, Then spawn 后 SetPermissionMode(plan) 被调一次", t, func() {
		var captured *fakeCCHandle
		restore := SetSessionFactoryForTest(func(ccLaunchSpec) (ccSessionHandle, error) {
			captured = &fakeCCHandle{id: "fake-sid"}
			return captured, nil
		})
		defer restore()

		r := New()
		ctx := context.Background()
		events, result, err := r.Run(ctx, agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:                  string(agent_backend_entity.TypeClaudeCode),
				DefaultPermissionMode: "bypassPermissions",
			},
			SessionID:      44,
			Cwd:            t.TempDir(),
			UserText:       "hi",
			PermissionMode: "plan",
		})
		So(err, ShouldBeNil)
		// launch 仍然是 bypass(resolveLaunchMode 锁住)。
		launchMode := result.LaunchPermissionMode
		for range events {
		}
		So(launchMode, ShouldEqual, "bypassPermissions")
		So(captured, ShouldNotBeNil)
		So(captured.setPermissionModeCalls, ShouldResemble, []string{"plan"})
		r.CloseAllSessions(ctx)
	})

	Convey("Given stored mode == launch mode, When Run, Then SetPermissionMode 不被调", t, func() {
		var captured *fakeCCHandle
		restore := SetSessionFactoryForTest(func(ccLaunchSpec) (ccSessionHandle, error) {
			captured = &fakeCCHandle{id: "fake-sid"}
			return captured, nil
		})
		defer restore()

		r := New()
		ctx := context.Background()
		// backendDefault=acceptEdits, stored=acceptEdits → launch=acceptEdits, 不需要校准。
		events, _, err := r.Run(ctx, agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:                  string(agent_backend_entity.TypeClaudeCode),
				DefaultPermissionMode: "acceptEdits",
			},
			SessionID:      45,
			Cwd:            t.TempDir(),
			UserText:       "hi",
			PermissionMode: "acceptEdits",
		})
		So(err, ShouldBeNil)
		for range events {
		}
		So(captured, ShouldNotBeNil)
		So(captured.setPermissionModeCalls, ShouldBeEmpty)
		r.CloseAllSessions(ctx)
	})

	Convey("Given SetPermissionMode 失败, Then Run 不应返错(只记 warn)", t, func() {
		var captured *fakeCCHandle
		restore := SetSessionFactoryForTest(func(ccLaunchSpec) (ccSessionHandle, error) {
			captured = &fakeCCHandle{id: "fake-sid", setPermissionModeErr: context.DeadlineExceeded}
			return captured, nil
		})
		defer restore()

		r := New()
		ctx := context.Background()
		events, result, err := r.Run(ctx, agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:                  string(agent_backend_entity.TypeClaudeCode),
				DefaultPermissionMode: "bypassPermissions",
			},
			SessionID:      46,
			Cwd:            t.TempDir(),
			UserText:       "hi",
			PermissionMode: "plan",
		})
		So(err, ShouldBeNil)
		launchMode := result.LaunchPermissionMode
		for range events {
		}
		So(launchMode, ShouldEqual, "bypassPermissions")
		So(captured.setPermissionModeCalls, ShouldResemble, []string{"plan"})
		r.CloseAllSessions(ctx)
	})
}
