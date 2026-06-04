package claudecode

import (
	"context"
	"errors"
	"testing"
	"time"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/agents/provider"
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
		// CapReportContextWindow=true 由 translator EventInit 路径承担:
		// system.init 帧带 model → llmcatalog.Lookup → emit ContextWindowUpdated。
		// Claude Code SDK 自己不报窗口,这里靠 catalog 兜底,语义和 codex 对称。
		So(caps.Has(capability.CapReportContextWindow), ShouldBeTrue)
		// CapImageInput=true:user frame 携带 base64 image content block(CLI
		// stream-json 原生支持)。extractImages 从 RunRequest.UserBlocks 抽 inline
		// 图片,Run 经 handle.Stream 透传。
		So(caps.Has(capability.CapImageInput), ShouldBeTrue)
		// CapAutonomousTurn=true:CLI 后台任务完成自主续轮;必须实现 AutonomousTurnSource。
		So(caps.Has(capability.CapAutonomousTurn), ShouldBeTrue)
		_, ok := agentruntime.Runtime(r).(agentruntime.AutonomousTurnSource)
		So(ok, ShouldBeTrue)
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

// TestRun_ForwardsUserBlockImages 钉死多模态透传:RunRequest.UserBlocks 里的
// inline ImageBlock 被 extractImages 抽出后,经 handle.Stream 透传给底层 CLI
// session(进而进 stream-json user frame 的 base64 image block)。非图片 block
// (TextBlock)不进 images;prompt 仍走 req.UserText。
func TestRun_ForwardsUserBlockImages(t *testing.T) {
	Convey("Run 把 UserBlocks 里的 inline 图片透传给 handle.Stream", t, func() {
		var captured *fakeCCHandle
		restore := SetSessionFactoryForTest(func(ccLaunchSpec) (ccSessionHandle, error) {
			captured = &fakeCCHandle{id: "fake-sid"}
			return captured, nil
		})
		defer restore()

		r := New()
		ctx := context.Background()
		events, _, err := r.Run(ctx, agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)},
			SessionID: 77,
			Cwd:       t.TempDir(),
			UserText:  "describe",
			UserBlocks: []cagoblocks.ContentBlock{
				cagoblocks.TextBlock{Text: "describe"},
				cagoblocks.ImageBlock{MediaType: "image/png", Source: cagoblocks.BlobSource{Inline: []byte{0x01, 0x02}}},
			},
		})
		So(err, ShouldBeNil)
		for range events {
		}
		So(captured.gotPrompt, ShouldEqual, "describe")
		So(captured.gotImages, ShouldResemble, []claudecode.Image{
			{Data: []byte{0x01, 0x02}, MediaType: "image/png"},
		})
		r.CloseAllSessions(ctx)
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
	stream                 ccStream
	// gotPrompt / gotImages 记录最近一次 Stream 收到的入参,Run 透传断言用。
	gotPrompt string
	gotImages []claudecode.Image
	// autoTurns 注入自主续轮(AutonomousTurns 桥接测试用);nil 时方法返回 nil。
	autoTurns <-chan *claudecode.AutoTurn
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
func (f *fakeCCHandle) ExitErr() error                               { return nil }
func (f *fakeCCHandle) AutonomousTurns() <-chan *claudecode.AutoTurn { return f.autoTurns }
func (f *fakeCCHandle) Stream(_ context.Context, prompt string, images []claudecode.Image) (ccStream, error) {
	f.gotPrompt = prompt
	f.gotImages = images
	if f.stream != nil {
		return f.stream, nil
	}
	return &fakeCCStream{}, nil
}

type fakeCCStream struct{}

func (s *fakeCCStream) Next() bool              { return false }
func (s *fakeCCStream) Event() claudecode.Event { return claudecode.Event{} }
func (s *fakeCCStream) SessionID() string       { return "" }

type eventCCStream struct {
	events []claudecode.Event
	idx    int
}

func (s *eventCCStream) Next() bool {
	if s.idx >= len(s.events) {
		return false
	}
	s.idx++
	return true
}

func (s *eventCCStream) Event() claudecode.Event { return s.events[s.idx-1] }
func (s *eventCCStream) SessionID() string       { return "" }

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

// TestAutonomousTurns_BridgesSessionAutoTurn 验证 Runtime.AutonomousTurns 把底层
// Session 自主续轮桥接成 agentruntime.AutonomousTurn,事件经 translate 翻译。
func TestAutonomousTurns_BridgesSessionAutoTurn(t *testing.T) {
	Convey("Runtime.AutonomousTurns 桥接底层 Session 自主续轮", t, func() {
		autoSrc := make(chan *claudecode.AutoTurn, 1)
		restore := SetSessionFactoryForTest(func(ccLaunchSpec) (ccSessionHandle, error) {
			return &fakeCCHandle{
				id:        "fake-sid",
				autoTurns: autoSrc,
				// usage 非空避免 Run 的 0-frame 兜底把 session evict 掉。
				stream: &eventCCStream{events: []claudecode.Event{
					{Kind: claudecode.EventUsage, Usage: provider.Usage{PromptTokens: 1}},
					{Kind: claudecode.EventDone},
				}},
			}, nil
		})
		defer restore()

		r := New()
		ctx := context.Background()
		// 先跑一轮把 session spawn + 缓存。
		events, _, err := r.Run(ctx, agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)},
			SessionID: 77,
			Cwd:       t.TempDir(),
			UserText:  "go",
		})
		So(err, ShouldBeNil)
		for range events { //nolint:revive // drain
		}

		turns := r.AutonomousTurns(77)

		// 注入一轮自主续轮(text + done)。
		atEvents := make(chan claudecode.Event, 3)
		atEvents <- claudecode.Event{Kind: claudecode.EventTextDelta, Text: "autonomous:listing"}
		atEvents <- claudecode.Event{Kind: claudecode.EventDone}
		close(atEvents)
		autoSrc <- &claudecode.AutoTurn{Events: atEvents, SessionID: "fake-sid", Trigger: "background_task"}
		close(autoSrc)

		var at agentruntime.AutonomousTurn
		select {
		case at = <-turns:
		case <-time.After(2 * time.Second):
			t.Fatal("expected a bridged autonomous turn within 2s")
		}
		So(at.Trigger, ShouldEqual, "background_task")

		var text string
		for ev := range at.Events {
			if td, ok := ev.(agentruntime.TextDelta); ok {
				text += td.Text
			}
		}
		So(text, ShouldContainSubstring, "autonomous:listing")
		So(at.Result.ProviderSessionID, ShouldEqual, "fake-sid")

		r.CloseAllSessions(ctx)
	})
}

func TestRun_ErrorFollowedByProgressClearsStopErr(t *testing.T) {
	Convey("claudecode runtime: EventError 后还有进展事件和完成时, StopErr 不应污染成功 turn", t, func() {
		restore := SetSessionFactoryForTest(func(ccLaunchSpec) (ccSessionHandle, error) {
			return &fakeCCHandle{
				id: "fake-sid",
				stream: &eventCCStream{events: []claudecode.Event{
					{Kind: claudecode.EventError, Err: errors.New("temporary upstream hiccup")},
					{Kind: claudecode.EventTextDelta, Text: "recovered"},
					{Kind: claudecode.EventDone},
				}},
			}, nil
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

		var text string
		for ev := range events {
			if td, ok := ev.(agentruntime.TextDelta); ok {
				text += td.Text
			}
		}

		So(text, ShouldEqual, "recovered")
		So(result.StopErr, ShouldBeNil)
		r.CloseAllSessions(ctx)
	})
}

func TestRun_ErrorFollowedOnlyByMetadataKeepsStopErr(t *testing.T) {
	Convey("claudecode runtime: EventError 后只有 metadata 和完成时, StopErr 仍应保留", t, func() {
		restore := SetSessionFactoryForTest(func(ccLaunchSpec) (ccSessionHandle, error) {
			return &fakeCCHandle{
				id: "fake-sid",
				stream: &eventCCStream{events: []claudecode.Event{
					{Kind: claudecode.EventError, Err: errors.New("temporary upstream hiccup")},
					{Kind: claudecode.EventUsage},
					{Kind: claudecode.EventDone},
				}},
			}, nil
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
		for range events {
		}

		So(result.StopErr, ShouldNotBeNil)
		So(result.StopErr.Error(), ShouldContainSubstring, "temporary upstream hiccup")
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
