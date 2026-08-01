package codex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cago-frame/agents/provider"
	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	pkgcodex "github.com/agentre-ai/agentre/pkg/codex"
)

// TestCodexCapabilities 钉死 codex runtime 的能力矩阵 + permission mode 元数据。
// 与 claudecode 的关键差异:CapCancelSteer/CapDrainSteer=false;
// CapReportContextWindow=true;PermissionModeMeta 仅 default/plan,SwitchableDuringTurn=false。
func TestCodexCapabilities(t *testing.T) {
	Convey("codex Capabilities 矩阵", t, func() {
		r := New()
		caps := r.Capabilities()
		So(caps.Has(capability.CapSteer), ShouldBeTrue)
		So(caps.Has(capability.CapCancelSteer), ShouldBeFalse) // codex fire-and-forget
		So(caps.Has(capability.CapDrainSteer), ShouldBeFalse)  // 无 hook 队列
		So(caps.Has(capability.CapAbort), ShouldBeTrue)
		So(caps.Has(capability.CapImageInput), ShouldBeTrue)
		So(caps.Has(capability.CapSetPermission), ShouldBeTrue)
		So(caps.Has(capability.CapAnswerUserAsk), ShouldBeTrue)
		So(caps.Has(capability.CapToolPermission), ShouldBeTrue)
		So(caps.Has(capability.CapForkSession), ShouldBeTrue)
		So(caps.Has(capability.CapReportContextWindow), ShouldBeTrue)
		So(caps.Has(capability.CapCompact), ShouldBeTrue)
		So(caps.Has(capability.CapMCPTools), ShouldBeTrue)
		So(caps.Has(capability.CapSkills), ShouldBeTrue)
	})

	Convey("codex PermissionModeMeta", t, func() {
		caps := New().Capabilities()
		So(caps.PermissionModeMeta.AllowedModes, ShouldResemble, []string{"default", "plan"})
		So(caps.PermissionModeMeta.DefaultMode, ShouldEqual, "default")
		So(caps.PermissionModeMeta.SwitchableDuringTurn, ShouldBeFalse)
		So(caps.PermissionModeMeta.Order, ShouldResemble, []string{"default", "plan"})
		// LaunchDefaultMode="default":codex 协议每次 launch 必须显式 mode。
		So(caps.PermissionModeMeta.LaunchDefaultMode, ShouldEqual, "default")
	})
}

func TestSubmitToolPermission(t *testing.T) {
	Convey("Given Codex approval request is active, when user allows for session, then approval is submitted and resolved event is emitted", t, func() {
		stream := newApprovalRuntimeStream(pkgcodex.Event{
			Kind: pkgcodex.EventApprovalRequest,
			Approval: &pkgcodex.ApprovalRequestEvent{
				RequestID: "approval-1",
				ItemID:    "item-command",
				ToolName:  "Bash",
				Input:     []byte(`{"command":"rm -rf build"}`),
			},
		})
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			return &fakeRuntimeSession{stream: stream, sid: "thread-approval"}, nil
		})
		defer restore()

		r := New()
		events, _, err := r.Run(context.Background(), agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeCodex), EnvJSON: "{}"},
			SessionID: 42,
			Cwd:       t.TempDir(),
			UserText:  "run it",
		})
		So(err, ShouldBeNil)

		ev := <-events
		req, ok := ev.(agentruntime.ToolPermissionRequest)
		So(ok, ShouldBeTrue)
		So(req.RequestID, ShouldEqual, "approval-1")

		err = r.SubmitToolPermission(context.Background(), 42, "approval-1", true, true, "")
		So(err, ShouldBeNil)
		So(stream.submittedRequestID, ShouldEqual, "approval-1")
		So(stream.submittedAllow, ShouldBeTrue)
		So(stream.submittedAlways, ShouldBeTrue)

		resolved := <-events
		res, ok := resolved.(agentruntime.ToolPermissionResolved)
		So(ok, ShouldBeTrue)
		So(res.RequestID, ShouldEqual, "approval-1")
		So(res.Allowed, ShouldBeTrue)
		So(res.AlwaysAllow, ShouldBeTrue)

		stream.finish()
		for range events {
		}
	})

	Convey("Given no active Codex approval request, when user answers, then no active turn is returned", t, func() {
		err := New().SubmitToolPermission(context.Background(), 42, "missing", true, false, "")
		So(err, ShouldEqual, agentruntime.ErrNoActiveTurn)
	})
}

func TestRun_DefaultModelWhenProviderMissing(t *testing.T) {
	Convey("codex runtime 在 CLI 自身登录态下回填 app-server resolved model", t, func() {
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			return &fakeRuntimeSession{stream: &emptyRuntimeStream{}, sid: "thread-default", model: "gpt-5.6-sol"}, nil
		})
		defer restore()

		events, result, err := New().Run(context.Background(), agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			SessionID: 1,
			Cwd:       t.TempDir(),
			UserText:  "hello",
		})
		So(err, ShouldBeNil)
		So(result, ShouldNotBeNil)
		for range events {
		}

		So(result.Model, ShouldEqual, "gpt-5.6-sol")
		So(result.ProviderSessionID, ShouldEqual, "thread-default")
	})
}

func TestRun_ModelResolution(t *testing.T) {
	Convey("codex runtime model resolution", t, func() {
		Convey("Given app-server does not report model, when provider is missing, then fallback default is used", func() {
			restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
				return &fakeRuntimeSession{stream: &emptyRuntimeStream{}, sid: "thread-default"}, nil
			})
			defer restore()

			events, result, err := New().Run(context.Background(), agentruntime.RunRequest{
				Backend: &agent_backend_entity.AgentBackend{
					Type:    string(agent_backend_entity.TypeCodex),
					EnvJSON: "{}",
				},
				SessionID: 1,
				Cwd:       t.TempDir(),
				UserText:  "hello",
			})
			So(err, ShouldBeNil)
			for range events {
			}

			So(result.Model, ShouldEqual, "gpt-5.5")
		})

		Convey("Given provider model is configured, when app-server reports a different model, then provider model wins", func() {
			restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
				return &fakeRuntimeSession{stream: &emptyRuntimeStream{}, sid: "thread-provider", model: "gpt-5.6-sol"}, nil
			})
			defer restore()

			events, result, err := New().Run(context.Background(), agentruntime.RunRequest{
				Backend: &agent_backend_entity.AgentBackend{
					Type:    string(agent_backend_entity.TypeCodex),
					EnvJSON: "{}",
				},
				Provider:  &llm_provider_entity.LLMProvider{Model: "gpt-5.4"},
				SessionID: 1,
				Cwd:       t.TempDir(),
				UserText:  "hello",
			})
			So(err, ShouldBeNil)
			for range events {
			}

			So(result.Model, ShouldEqual, "gpt-5.4")
		})
	})
}

func TestSetGoal_CreatesProviderThreadBeforeFirstTurn(t *testing.T) {
	Convey("Given a Codex chat session has no provider thread yet, when setting a goal, then runtime starts a session and returns the created thread id", t, func() {
		fake := &fakeRuntimeSession{}
		restore := SetSessionFactoryForTest(func(req agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			So(req.ProviderSessionID, ShouldEqual, "")
			So(req.SessionID, ShouldEqual, int64(42))
			return fake, nil
		})
		defer restore()

		objective := "ship before first turn"
		status := "active"
		goal, err := New().SetGoal(context.Background(), agentruntime.GoalRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			AgentID:   7,
			SessionID: 42,
			Cwd:       t.TempDir(),
			Objective: &objective,
			Status:    &status,
		})

		So(err, ShouldBeNil)
		So(goal, ShouldNotBeNil)
		So(goal.ThreadID, ShouldEqual, "thread-created-for-goal")
		So(goal.Objective, ShouldEqual, "ship before first turn")
		So(fake.setGoalReq.Objective, ShouldNotBeNil)
		So(*fake.setGoalReq.Objective, ShouldEqual, "ship before first turn")
	})
}

func TestSetGoal_ReleasesOneShotSessionToIdle(t *testing.T) {
	Convey("Given /goal only performs a one-shot Codex RPC, when SetGoal returns, then the cached CLI session is idle for the next turn", t, func() {
		pool := agentruntime.NewCLISessionPool(8)
		fake := &fakeRuntimeSession{}
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			return fake, nil
		})
		defer restore()

		objective := "ship without starting a turn"
		status := "active"
		goal, err := NewWithPool(pool).SetGoal(context.Background(), agentruntime.GoalRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			AgentID:   7,
			SessionID: 42,
			Cwd:       t.TempDir(),
			Objective: &objective,
			Status:    &status,
		})

		So(err, ShouldBeNil)
		So(goal, ShouldNotBeNil)
		So(pool.Len(), ShouldEqual, 1)
		So(pool.IdleLen(), ShouldEqual, 1)
	})
}

func TestSetGoal_KeepsActiveTurnSessionActive(t *testing.T) {
	Convey("Given a Codex turn is active, when SetGoal runs against the same session, then the cached CLI session is not marked idle", t, func() {
		pool := agentruntime.NewCLISessionPool(8)
		stream := newBlockingRuntimeStream()
		fake := &fakeRuntimeSession{stream: stream, sid: "thread-active"}
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			return fake, nil
		})
		defer restore()

		r := NewWithPool(pool)
		events, _, err := r.Run(context.Background(), agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			SessionID: 42,
			Cwd:       t.TempDir(),
			UserText:  "run",
		})
		So(err, ShouldBeNil)
		defer func() {
			stream.finish()
			for range events {
			}
		}()

		objective := "update while active"
		status := "active"
		goal, err := r.SetGoal(context.Background(), agentruntime.GoalRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			AgentID:           7,
			SessionID:         42,
			ProviderSessionID: "thread-active",
			Cwd:               t.TempDir(),
			Objective:         &objective,
			Status:            &status,
		})

		So(err, ShouldBeNil)
		So(goal, ShouldNotBeNil)
		So(pool.Len(), ShouldEqual, 1)
		So(pool.IdleLen(), ShouldEqual, 0)
	})
}

func TestRun_ReusesCachedSessionAcrossTurns(t *testing.T) {
	Convey("Given a Codex chat session is idle after one turn, when Run is called again, then the cached CLI session is reused", t, func() {
		pool := agentruntime.NewCLISessionPool(8)
		cached := &countingRuntimeSession{
			sid: "thread-cached",
			streams: []cxStream{
				&emptyRuntimeStream{},
				&emptyRuntimeStream{},
			},
		}
		factoryCalls := 0
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			factoryCalls++
			return cached, nil
		})
		defer restore()

		r := NewWithPool(pool)
		req := agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			SessionID: 77,
			Cwd:       t.TempDir(),
		}

		events, _, err := r.Run(context.Background(), req)
		So(err, ShouldBeNil)
		for range events {
		}

		req.UserText = "again"
		events, _, err = r.Run(context.Background(), req)
		So(err, ShouldBeNil)
		for range events {
		}

		So(factoryCalls, ShouldEqual, 1)
		So(cached.streamCalls, ShouldEqual, 2)
		So(cached.closed, ShouldBeFalse)
		So(pool.Len(), ShouldEqual, 1)
		So(pool.IdleLen(), ShouldEqual, 1)
	})
}

func TestRun_MCPServersBypassCachedSession(t *testing.T) {
	Convey("Given a Codex chat session has an idle app-server without MCP, when a group turn injects MCPServers, then runtime starts a fresh app-server with MCP config", t, func() {
		pool := agentruntime.NewCLISessionPool(8)
		first := &countingRuntimeSession{
			sid:     "thread-cached",
			streams: []cxStream{&emptyRuntimeStream{}},
		}
		second := &countingRuntimeSession{
			sid:      "thread-cached",
			streams:  []cxStream{&emptyRuntimeStream{}},
			closedCh: make(chan struct{}),
		}
		factoryCalls := 0
		var secondReq agentruntime.RunRequest
		restore := SetSessionFactoryForTest(func(req agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			factoryCalls++
			if factoryCalls == 1 {
				return first, nil
			}
			secondReq = req
			return second, nil
		})
		defer restore()

		r := NewWithPool(pool)
		req := agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			SessionID: 77,
			Cwd:       t.TempDir(),
		}

		events, _, err := r.Run(context.Background(), req)
		So(err, ShouldBeNil)
		for range events {
		}

		req.MCPServers = []agentruntime.MCPServerSpec{{
			Name:  "group",
			URL:   "http://127.0.0.1:9000/mcp/group/",
			Tools: []string{"group_send"},
		}}
		events, _, err = r.Run(context.Background(), req)
		So(err, ShouldBeNil)
		for range events {
		}

		So(factoryCalls, ShouldEqual, 2)
		So(first.streamCalls, ShouldEqual, 1)
		So(second.streamCalls, ShouldEqual, 1)
		So(secondReq.MCPServers, ShouldHaveLength, 1)
		So(pool.Len(), ShouldEqual, 1)
		second.waitClosed(t)
		So(second.closed, ShouldBeTrue)
	})
}

func TestRun_EnabledPluginsBypassCachedSession(t *testing.T) {
	Convey("Given a Codex chat session has an idle app-server without plugin overrides, when a turn injects EnabledPlugins, then runtime starts a fresh app-server with those overrides", t, func() {
		pool := agentruntime.NewCLISessionPool(8)
		first := &countingRuntimeSession{
			sid:     "thread-cached",
			streams: []cxStream{&emptyRuntimeStream{}, &emptyRuntimeStream{}},
		}
		second := &countingRuntimeSession{
			sid:      "thread-cached",
			streams:  []cxStream{&emptyRuntimeStream{}},
			closedCh: make(chan struct{}),
		}
		factoryCalls := 0
		var secondReq agentruntime.RunRequest
		restore := SetSessionFactoryForTest(func(req agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			factoryCalls++
			if factoryCalls == 1 {
				return first, nil
			}
			secondReq = req
			return second, nil
		})
		defer restore()

		r := NewWithPool(pool)
		req := agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			SessionID: 77,
			Cwd:       t.TempDir(),
		}

		events, _, err := r.Run(context.Background(), req)
		So(err, ShouldBeNil)
		for range events {
		}

		req.EnabledPlugins = map[string]bool{
			"browser@openai-bundled":           true,
			"superpowers@openai-curated":       false,
			"documents@openai-primary-runtime": true,
		}
		events, _, err = r.Run(context.Background(), req)
		So(err, ShouldBeNil)
		for range events {
		}

		So(factoryCalls, ShouldEqual, 2)
		So(first.streamCalls, ShouldEqual, 1)
		So(second.streamCalls, ShouldEqual, 1)
		So(secondReq.EnabledPlugins, ShouldHaveLength, 3)
		So(pool.Len(), ShouldEqual, 1)
		second.waitClosed(t)
		So(second.closed, ShouldBeTrue)
	})
}

func TestCloseSession_RemovesCachedCodexSession(t *testing.T) {
	Convey("Given a cached idle Codex CLI session, when CloseSession is called, then the session is closed and evicted", t, func() {
		pool := agentruntime.NewCLISessionPool(8)
		cached := &countingRuntimeSession{
			sid:      "thread-close",
			streams:  []cxStream{&emptyRuntimeStream{}},
			closedCh: make(chan struct{}),
		}
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			return cached, nil
		})
		defer restore()

		r := NewWithPool(pool)
		events, _, err := r.Run(context.Background(), agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeCodex), EnvJSON: "{}"},
			SessionID: 88,
			Cwd:       t.TempDir(),
		})
		So(err, ShouldBeNil)
		for range events {
		}
		So(pool.Len(), ShouldEqual, 1)

		r.CloseSession(context.Background(), 88)

		cached.waitClosed(t)
		So(cached.closed, ShouldBeTrue)
		So(pool.Len(), ShouldEqual, 0)
	})
}

func TestRun_EmitsContextWindowUpdateFromTokenUsage(t *testing.T) {
	Convey("codex runtime 在 token usage 帧上报 modelContextWindow 时实时 emit ContextWindowUpdated", t, func() {
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			return &fakeRuntimeSession{stream: &eventRuntimeStream{
				events: []pkgcodex.Event{
					{
						Kind:          pkgcodex.EventUsage,
						ContextWindow: 258400,
						Usage: provider.Usage{
							PromptTokens:     100,
							CompletionTokens: 20,
						},
					},
					{
						Kind:          pkgcodex.EventUsage,
						ContextWindow: 258400,
						Usage: provider.Usage{
							PromptTokens:     120,
							CompletionTokens: 30,
						},
					},
				},
			}, sid: "thread-cw"}, nil
		})
		defer restore()

		events, result, err := New().Run(context.Background(), agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			SessionID: 1,
			Cwd:       t.TempDir(),
			UserText:  "hello",
		})
		So(err, ShouldBeNil)

		var contextWindows []agentruntime.ContextWindowUpdated
		var usages []agentruntime.UsageUpdate
		for ev := range events {
			switch e := ev.(type) {
			case agentruntime.ContextWindowUpdated:
				contextWindows = append(contextWindows, e)
			case agentruntime.UsageUpdate:
				usages = append(usages, e)
			}
		}

		So(contextWindows, ShouldHaveLength, 1)
		So(contextWindows[0].Tokens, ShouldEqual, 258400)
		So(usages, ShouldHaveLength, 2)
		So(result.ContextWindow, ShouldEqual, 258400)
	})
}

func TestRun_ErrorFollowedByProgressClearsStopErr(t *testing.T) {
	Convey("codex runtime: EventError 后还有进展事件和完成时, StopErr 不应污染成功 turn", t, func() {
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			return &fakeRuntimeSession{stream: &eventRuntimeStream{
				events: []pkgcodex.Event{
					{Kind: pkgcodex.EventError, Err: errors.New("temporary upstream hiccup")},
					{Kind: pkgcodex.EventTextDelta, Text: "recovered"},
					{Kind: pkgcodex.EventDone},
				},
			}, sid: "thread-recovered"}, nil
		})
		defer restore()

		events, result, err := New().Run(context.Background(), agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			SessionID: 1,
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
	})
}

func TestRun_ErrorFollowedOnlyByMetadataKeepsStopErr(t *testing.T) {
	Convey("codex runtime: EventError 后只有 metadata 和完成时, StopErr 仍应保留", t, func() {
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			return &fakeRuntimeSession{stream: &eventRuntimeStream{
				events: []pkgcodex.Event{
					{Kind: pkgcodex.EventError, Err: errors.New("temporary upstream hiccup")},
					{Kind: pkgcodex.EventUsage},
					{Kind: pkgcodex.EventDone},
				},
			}, sid: "thread-failed"}, nil
		})
		defer restore()

		events, result, err := New().Run(context.Background(), agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			SessionID: 1,
			Cwd:       t.TempDir(),
			UserText:  "hello",
		})
		So(err, ShouldBeNil)
		for range events {
		}

		So(result.StopErr, ShouldNotBeNil)
		So(result.StopErr.Error(), ShouldContainSubstring, "temporary upstream hiccup")
	})
}

func TestRun_DuplicateSessionTurnDoesNotReplaceActiveOwner(t *testing.T) {
	Convey("Given a Codex turn owns a chat session, when another Run starts for the same session, then it is rejected without replacing the first owner", t, func() {
		pool := agentruntime.NewCLISessionPool(8)
		stream := newBlockingRuntimeStream()
		fake := &fakeRuntimeSession{stream: stream, sid: "thread-owner"}
		factoryCalls := 0
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			factoryCalls++
			return fake, nil
		})
		defer restore()

		r := NewWithPool(pool)
		req := agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeCodex), EnvJSON: "{}"},
			SessionID: 707,
			Cwd:       t.TempDir(),
			UserText:  "first",
		}
		firstEvents, _, err := r.Run(context.Background(), req)
		So(err, ShouldBeNil)

		req.UserText = "must not overlap"
		secondEvents, _, secondErr := r.Run(context.Background(), req)

		So(secondErr, ShouldNotBeNil)
		So(secondErr.Error(), ShouldContainSubstring, "active turn")
		So(secondEvents, ShouldBeNil)
		So(factoryCalls, ShouldEqual, 1)

		stream.finish()
		for range firstEvents {
		}
	})
}

func TestRuntimeUnregister_StaleOwnerCannotDeleteReplacement(t *testing.T) {
	Convey("Given a replacement owner is installed, when an old turn defers unregister, then only the expected owner can be removed", t, func() {
		r := New()
		oldOwner := &codexActive{}
		newOwner := &codexActive{}
		r.active[11] = newOwner

		r.unregister(11, oldOwner)

		So(r.active[11], ShouldEqual, newOwner)
		r.unregister(11, newOwner)
		_, exists := r.active[11]
		So(exists, ShouldBeFalse)
	})
}

func TestRun_ControlCallsAreRaceFreeWhileActiveOwnerInitializes(t *testing.T) {
	// Given Run has claimed the session but the app-server has not returned its
	// stream yet, control calls may concurrently observe that provisional owner.
	stream := &interruptibleBlockingRuntimeStream{blockingRuntimeStream: newBlockingRuntimeStream()}
	sess := &blockingStartRuntimeSession{
		fakeRuntimeSession: &fakeRuntimeSession{stream: stream, sid: "thread-initializing"},
		entered:            make(chan struct{}),
		release:            make(chan struct{}),
	}
	restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
		return sess, nil
	})
	defer restore()

	r := New()
	type runResult struct {
		events <-chan agentruntime.Event
		err    error
	}
	runDone := make(chan runResult, 1)
	go func() {
		events, _, err := r.Run(context.Background(), agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeCodex), EnvJSON: "{}"},
			SessionID: 707,
			Cwd:       t.TempDir(),
			UserText:  "initialize",
		})
		runDone <- runResult{events: events, err: err}
	}()

	<-sess.entered
	stopControls := make(chan struct{})
	controlsDone := make(chan struct{})
	go func() {
		defer close(controlsDone)
		for {
			select {
			case <-stopControls:
				return
			default:
				_ = r.Abort(context.Background(), 707)
			}
		}
	}()
	close(sess.release)
	result := <-runDone
	close(stopControls)
	<-controlsDone
	if result.err != nil {
		t.Fatalf("Run failed after initialization: %v", result.err)
	}
	stream.finish()
	for range result.events {
	}
}

func TestRun_FailedCachedSessionIsEvictedBeforeNextTurn(t *testing.T) {
	Convey("Given a cached Codex process fails to start a turn, when the next turn runs, then a fresh session is created", t, func() {
		pool := agentruntime.NewCLISessionPool(8)
		bad := &startFailRuntimeSession{sid: "thread-dead", err: pkgcodex.ErrProcessDead}
		good := &countingRuntimeSession{sid: "thread-restarted", streams: []cxStream{&emptyRuntimeStream{}}}
		factoryCalls := 0
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			factoryCalls++
			if factoryCalls == 1 {
				return bad, nil
			}
			return good, nil
		})
		defer restore()

		r := NewWithPool(pool)
		req := agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeCodex), EnvJSON: "{}"},
			SessionID: 808,
			Cwd:       t.TempDir(),
		}
		_, _, firstErr := r.Run(context.Background(), req)
		So(firstErr, ShouldNotBeNil)

		events, _, secondErr := r.Run(context.Background(), req)
		So(secondErr, ShouldBeNil)
		for range events {
		}
		So(factoryCalls, ShouldEqual, 2)
		So(good.streamCalls, ShouldEqual, 1)
	})
}

func TestResolvedEventDelivery_DoesNotDropWhenOutputBufferIsFull(t *testing.T) {
	Convey("Given the runtime event buffer is full, when a request resolves, then the state event waits for delivery instead of being silently dropped", t, func() {
		out := make(chan agentruntime.Event, 1)
		out <- agentruntime.TextDelta{Text: "occupy"}
		active := &codexActive{}
		active.setOut(out)
		delivered := make(chan struct{})

		go func() {
			_ = emitUserAskResolved(active, "request-1", true, nil)
			close(delivered)
		}()

		select {
		case <-delivered:
			So("resolved event returned before there was capacity", ShouldBeBlank)
		case <-time.After(30 * time.Millisecond):
		}
		<-out
		select {
		case <-delivered:
		case <-time.After(time.Second):
			So("resolved event was never delivered", ShouldBeBlank)
		}
		resolved := <-out
		_, ok := resolved.(agentruntime.UserAskResolved)
		So(ok, ShouldBeTrue)
	})
}

func TestSubmitResolution_DoesNotAnswerAfterRuntimeOutputClosed(t *testing.T) {
	Convey("Given the turn output is already closed, when user input is submitted, then backend response and waiter state stay untouched", t, func() {
		backend := &recordingUserInputStream{}
		active := &codexActive{userInput: backend}
		active.registerAskWaiter("input-closed", []agentruntime.AskQuestion{{ID: "q1", Question: "Continue?"}})
		r := New()
		r.active[911] = active

		err := r.SubmitAnswer(context.Background(), 911, "input-closed", nil, nil, true)

		So(err, ShouldEqual, agentruntime.ErrNoActiveTurn)
		So(backend.called, ShouldBeFalse)
		So(active.askWaiter("input-closed"), ShouldNotBeNil)
	})

	Convey("Given the turn output is already closed, when approval is submitted, then backend response and waiter state stay untouched", t, func() {
		backend := newApprovalRuntimeStream(pkgcodex.Event{})
		active := &codexActive{approval: backend}
		active.registerPermWaiter("approval-closed")
		r := New()
		r.active[912] = active

		err := r.SubmitToolPermission(context.Background(), 912, "approval-closed", true, false, "")

		So(err, ShouldEqual, agentruntime.ErrNoActiveTurn)
		So(backend.submittedRequestID, ShouldBeBlank)
		So(active.hasPermWaiter("approval-closed"), ShouldBeTrue)
	})
}

func TestRun_ServerResolvedRequestsConvergeRuntimeWaiters(t *testing.T) {
	Convey("Given request_user_input is pending, when app-server resolves it externally, then runtime emits a skipped resolution and releases waiting state", t, func() {
		pool := agentruntime.NewCLISessionPool(8)
		defer pool.RemoveAll()
		stream := newBlockingEventRuntimeStream(
			pkgcodex.Event{
				Kind: pkgcodex.EventRequestUserInput,
				RequestUserInput: &pkgcodex.RequestUserInputEvent{
					RequestID: "input-auto-resolved",
					ItemID:    "question-item",
					Questions: []pkgcodex.RequestUserInputQuestion{{
						ID:       "question-1",
						Header:   "Choice",
						Question: "Continue?",
					}},
				},
			},
			pkgcodex.Event{
				Kind: pkgcodex.EventRequestResolved,
				RequestResolved: &pkgcodex.RequestResolvedEvent{
					RequestID: "input-auto-resolved",
					Kind:      pkgcodex.RequestKindUserInput,
				},
			},
		)
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			return &fakeRuntimeSession{stream: stream, sid: "thread-input-resolved"}, nil
		})
		defer restore()

		r := NewWithPool(pool)
		events, _, err := r.Run(context.Background(), agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeCodex), EnvJSON: "{}"},
			SessionID: 901,
			Cwd:       t.TempDir(),
			UserText:  "ask",
		})
		So(err, ShouldBeNil)

		request, ok := (<-events).(agentruntime.UserAskRequest)
		So(ok, ShouldBeTrue)
		So(request.RequestID, ShouldEqual, "input-auto-resolved")
		resolved, ok := (<-events).(agentruntime.UserAskResolved)
		So(ok, ShouldBeTrue)
		So(resolved.RequestID, ShouldEqual, "input-auto-resolved")
		So(resolved.Skipped, ShouldBeTrue)

		r.mu.Lock()
		active := r.active[901]
		r.mu.Unlock()
		if active == nil {
			t.Fatal("runtime removed the active turn before the request resolution was observed")
		}
		So(active.askWaiter("input-auto-resolved"), ShouldBeNil)
		stream.finish()
		for range events {
		}
		So(pool.IdleLen(), ShouldEqual, 1)
	})

	Convey("Given approval is pending, when app-server resolves it externally, then runtime emits a neutral denial and releases waiting state", t, func() {
		pool := agentruntime.NewCLISessionPool(8)
		defer pool.RemoveAll()
		stream := newBlockingEventRuntimeStream(
			pkgcodex.Event{
				Kind: pkgcodex.EventApprovalRequest,
				Approval: &pkgcodex.ApprovalRequestEvent{
					RequestID: "approval-auto-resolved",
					ItemID:    "command-item",
					ToolName:  "Bash",
					Input:     []byte(`{"command":"pwd"}`),
				},
			},
			pkgcodex.Event{
				Kind: pkgcodex.EventRequestResolved,
				RequestResolved: &pkgcodex.RequestResolvedEvent{
					RequestID: "approval-auto-resolved",
					Kind:      pkgcodex.RequestKindApproval,
				},
			},
		)
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (cxSessionHandle, error) {
			return &fakeRuntimeSession{stream: stream, sid: "thread-approval-resolved"}, nil
		})
		defer restore()

		r := NewWithPool(pool)
		events, _, err := r.Run(context.Background(), agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeCodex), EnvJSON: "{}"},
			SessionID: 902,
			Cwd:       t.TempDir(),
			UserText:  "run",
		})
		So(err, ShouldBeNil)

		request, ok := (<-events).(agentruntime.ToolPermissionRequest)
		So(ok, ShouldBeTrue)
		So(request.RequestID, ShouldEqual, "approval-auto-resolved")
		resolved, ok := (<-events).(agentruntime.ToolPermissionResolved)
		So(ok, ShouldBeTrue)
		So(resolved.RequestID, ShouldEqual, "approval-auto-resolved")
		So(resolved.Allowed, ShouldBeFalse)
		So(resolved.AlwaysAllow, ShouldBeFalse)
		So(resolved.DenyReason, ShouldEqual, "approval request resolved by Codex app-server without a decision")

		r.mu.Lock()
		active := r.active[902]
		r.mu.Unlock()
		if active == nil {
			t.Fatal("runtime removed the active turn before the approval resolution was observed")
		}
		So(active.hasPermWaiter("approval-auto-resolved"), ShouldBeFalse)
		stream.finish()
		for range events {
		}
		So(pool.IdleLen(), ShouldEqual, 1)
	})
}

type fakeRuntimeSession struct {
	stream cxStream
	sid    string
	model  string

	setGoalReq pkgcodex.GoalUpdate
}

type blockingStartRuntimeSession struct {
	*fakeRuntimeSession
	entered chan struct{}
	release chan struct{}
}

func (s *blockingStartRuntimeSession) Stream(context.Context, string, string) (cxStream, error) {
	close(s.entered)
	<-s.release
	return s.stream, nil
}

type startFailRuntimeSession struct {
	sid string
	err error
}

func (s *startFailRuntimeSession) Close(context.Context) error { return nil }
func (s *startFailRuntimeSession) ID() string                  { return s.sid }
func (*startFailRuntimeSession) Model() string                 { return "" }
func (s *startFailRuntimeSession) Stream(context.Context, string, string) (cxStream, error) {
	return nil, s.err
}
func (s *startFailRuntimeSession) StreamInput(context.Context, []pkgcodex.UserInput, string) (cxStream, error) {
	return nil, s.err
}
func (s *startFailRuntimeSession) Compact(context.Context) (cxStream, error) {
	return nil, s.err
}
func (*startFailRuntimeSession) GetGoal(context.Context) (*pkgcodex.Goal, error) { return nil, nil }
func (*startFailRuntimeSession) SetGoal(context.Context, pkgcodex.GoalUpdate) (*pkgcodex.Goal, error) {
	return nil, nil
}
func (*startFailRuntimeSession) ClearGoal(context.Context) (bool, error) { return true, nil }
func (s *startFailRuntimeSession) RewindTo(context.Context, string) (string, error) {
	return s.sid, nil
}
func (*startFailRuntimeSession) ActiveStream() cxSteerStream        { return nil }
func (*startFailRuntimeSession) ActiveInterruptor() cxInterruptable { return nil }

func (s *fakeRuntimeSession) Close(context.Context) error { return nil }
func (s *fakeRuntimeSession) ID() string                  { return s.sid }
func (s *fakeRuntimeSession) Model() string               { return s.model }
func (s *fakeRuntimeSession) Stream(context.Context, string, string) (cxStream, error) {
	return s.stream, nil
}
func (s *fakeRuntimeSession) StreamInput(context.Context, []pkgcodex.UserInput, string) (cxStream, error) {
	return s.stream, nil
}
func (s *fakeRuntimeSession) Compact(context.Context) (cxStream, error)       { return s.stream, nil }
func (s *fakeRuntimeSession) GetGoal(context.Context) (*pkgcodex.Goal, error) { return nil, nil }
func (s *fakeRuntimeSession) SetGoal(_ context.Context, req pkgcodex.GoalUpdate) (*pkgcodex.Goal, error) {
	s.setGoalReq = req
	if s.sid == "" {
		s.sid = "thread-created-for-goal"
	}
	objective := ""
	if req.Objective != nil {
		objective = *req.Objective
	}
	status := pkgcodex.GoalStatus("")
	if req.Status != nil {
		status = *req.Status
	}
	return &pkgcodex.Goal{ThreadID: s.sid, Objective: objective, Status: status}, nil
}
func (s *fakeRuntimeSession) ClearGoal(context.Context) (bool, error)          { return true, nil }
func (s *fakeRuntimeSession) RewindTo(context.Context, string) (string, error) { return s.sid, nil }
func (s *fakeRuntimeSession) ActiveStream() cxSteerStream                      { return nil }
func (s *fakeRuntimeSession) ActiveInterruptor() cxInterruptable               { return nil }

type countingRuntimeSession struct {
	streams     []cxStream
	sid         string
	streamCalls int
	closed      bool
	closedCh    chan struct{}
}

func (s *countingRuntimeSession) Close(context.Context) error {
	if !s.closed {
		s.closed = true
		if s.closedCh != nil {
			close(s.closedCh)
		}
	}
	return nil
}
func (s *countingRuntimeSession) ID() string { return s.sid }
func (s *countingRuntimeSession) Model() string {
	return ""
}
func (s *countingRuntimeSession) Stream(context.Context, string, string) (cxStream, error) {
	stream := s.streams[s.streamCalls]
	s.streamCalls++
	return stream, nil
}
func (s *countingRuntimeSession) StreamInput(context.Context, []pkgcodex.UserInput, string) (cxStream, error) {
	return s.Stream(context.Background(), "", "")
}
func (s *countingRuntimeSession) Compact(context.Context) (cxStream, error) {
	return s.Stream(context.Background(), "", "")
}
func (s *countingRuntimeSession) GetGoal(context.Context) (*pkgcodex.Goal, error) { return nil, nil }
func (s *countingRuntimeSession) SetGoal(context.Context, pkgcodex.GoalUpdate) (*pkgcodex.Goal, error) {
	return nil, nil
}
func (s *countingRuntimeSession) ClearGoal(context.Context) (bool, error)          { return true, nil }
func (s *countingRuntimeSession) RewindTo(context.Context, string) (string, error) { return s.sid, nil }
func (s *countingRuntimeSession) ActiveStream() cxSteerStream                      { return nil }
func (s *countingRuntimeSession) ActiveInterruptor() cxInterruptable               { return nil }

func (s *countingRuntimeSession) waitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-s.closedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("cached codex session was not closed")
	}
}

type emptyRuntimeStream struct{}

func (*emptyRuntimeStream) Next() bool            { return false }
func (*emptyRuntimeStream) Event() pkgcodex.Event { return pkgcodex.Event{} }
func (*emptyRuntimeStream) SessionID() string     { return "" }

type blockingRuntimeStream struct {
	done chan struct{}
}

func newBlockingRuntimeStream() *blockingRuntimeStream {
	return &blockingRuntimeStream{done: make(chan struct{})}
}

func (s *blockingRuntimeStream) Next() bool          { <-s.done; return false }
func (*blockingRuntimeStream) Event() pkgcodex.Event { return pkgcodex.Event{} }
func (*blockingRuntimeStream) SessionID() string     { return "" }
func (s *blockingRuntimeStream) finish()             { close(s.done) }

type interruptibleBlockingRuntimeStream struct {
	*blockingRuntimeStream
}

func (*interruptibleBlockingRuntimeStream) Interrupt(context.Context) error { return nil }

type eventRuntimeStream struct {
	events []pkgcodex.Event
	idx    int
}

func (s *eventRuntimeStream) Next() bool {
	if s.idx >= len(s.events) {
		return false
	}
	s.idx++
	return true
}

func (s *eventRuntimeStream) Event() pkgcodex.Event { return s.events[s.idx-1] }
func (s *eventRuntimeStream) SessionID() string     { return "" }

type blockingEventRuntimeStream struct {
	events []pkgcodex.Event
	idx    int
	done   chan struct{}
}

func newBlockingEventRuntimeStream(events ...pkgcodex.Event) *blockingEventRuntimeStream {
	return &blockingEventRuntimeStream{events: events, done: make(chan struct{})}
}

func (s *blockingEventRuntimeStream) Next() bool {
	if s.idx < len(s.events) {
		s.idx++
		return true
	}
	<-s.done
	return false
}

func (s *blockingEventRuntimeStream) Event() pkgcodex.Event { return s.events[s.idx-1] }
func (s *blockingEventRuntimeStream) SessionID() string     { return "" }
func (s *blockingEventRuntimeStream) finish()               { close(s.done) }

type approvalRuntimeStream struct {
	event pkgcodex.Event
	done  chan struct{}
	used  bool

	submittedRequestID string
	submittedAllow     bool
	submittedAlways    bool
}

func newApprovalRuntimeStream(ev pkgcodex.Event) *approvalRuntimeStream {
	return &approvalRuntimeStream{event: ev, done: make(chan struct{})}
}

func (s *approvalRuntimeStream) Next() bool {
	if !s.used {
		s.used = true
		return true
	}
	<-s.done
	return false
}

func (s *approvalRuntimeStream) Event() pkgcodex.Event { return s.event }
func (s *approvalRuntimeStream) SessionID() string     { return "" }

func (s *approvalRuntimeStream) SubmitApproval(_ context.Context, requestID string, allow, alwaysAllowSession bool) error {
	s.submittedRequestID = requestID
	s.submittedAllow = allow
	s.submittedAlways = alwaysAllowSession
	return nil
}

func (s *approvalRuntimeStream) finish() { close(s.done) }

type recordingUserInputStream struct {
	called bool
}

func (s *recordingUserInputStream) SubmitUserInput(context.Context, string, map[string][]string) error {
	s.called = true
	return nil
}
