package chat_svc_test

import (
	"context"
	"testing"
	"time"

	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/pkg/agentruntime/mock_agentruntime"
	"agentre/internal/service/chat_svc"
)

// autoTurnRunner 是同时实现 agentruntime.Runtime + AutonomousTurnSource 的 fake,
// 用来验证 runTurn 的挂载 type-assert(走 builtin Send 路径,比 claudecode 简单)。
type autoTurnRunner struct {
	autoCh chan agentruntime.AutonomousTurn
}

func (*autoTurnRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{
		Set: map[capability.Capability]bool{capability.CapImageInput: true},
		PermissionModeMeta: capability.PermissionModeMeta{
			AllowedModes:         []string{"default", "acceptEdits", "plan", "bypassPermissions"},
			DefaultMode:          "acceptEdits",
			SwitchableDuringTurn: true,
		},
	}
}

func (*autoTurnRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	events := make(chan agentruntime.Event, 1)
	events <- agentruntime.TextDelta{Text: "ok"}
	close(events)
	return events, &agentruntime.RunResult{ProviderSessionID: "builtin-100"}, nil
}

func (r *autoTurnRunner) AutonomousTurns(int64) <-chan agentruntime.AutonomousTurn { return r.autoCh }

// TestDriveAutonomousTurn_PersistsPureAssistantTurn 是 Phase 3 基石:一轮自主续轮
// 落成 **纯 assistant 消息(无 user 行)**,经会话级旁路通知前端 + 实时 stream +
// 收尾翻 idle。
func TestDriveAutonomousTurn_PersistsPureAssistantTurn(t *testing.T) {
	convey.Convey("自主续轮落纯 assistant 轮(无 user 行)", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		sess := &chat_entity.Session{ID: 100, AgentID: 7, AgentStatus: "idle", ProviderSessionID: "sess-abc"}
		be := &agent_backend_entity.AgentBackend{ID: 12, Type: "claudecode"}

		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil).AnyTimes()

		m.dbMock.ExpectBegin()
		m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(5, nil)
		var createdRoles []string
		m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
				createdRoles = append(createdRoles, msg.Role)
				msg.ID = 2001
				return nil
			}).Times(1)
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		m.dbMock.ExpectCommit()
		m.message.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		evs := make(chan agentruntime.Event, 2)
		evs <- agentruntime.TextDelta{Text: "autonomous:listing"}
		close(evs)
		at := agentruntime.AutonomousTurn{
			Events: evs,
			Result: &agentruntime.RunResult{
				ProviderSessionID: "sess-abc",
				Model:             "claude-sonnet-4-6",
				Usage:             &provider.Usage{PromptTokens: 2, CompletionTokens: 2},
			},
			Trigger: "background_task",
		}

		chat_svc.DriveAutonomousTurnForTest(ctx, m.svc, 100, be, at)

		convey.Convey("只建一条 assistant 消息,没有 user 行", func() {
			assert.Equal(t, []string{"assistant"}, createdRoles)
		})

		var (
			startedName    string
			startedStream  string
			startedTrigger string
			startedHasMsg  bool
			sawStarted     bool
			sawDone        bool
			chunk          string
		)
		for _, ev := range m.events {
			p, ok := ev.Payload.(chat_svc.ChatStreamEvent)
			if !ok {
				continue
			}
			switch p.Kind {
			case chat_svc.StreamAutonomousStarted:
				sawStarted = true
				startedName = ev.Name
				startedStream = p.Stream
				startedTrigger = p.Trigger
				startedHasMsg = p.AssistantMessage != nil
			case chat_svc.StreamChunk:
				chunk += p.Delta
			case chat_svc.StreamDone:
				sawDone = true
			}
		}

		convey.Convey("emit 会话级 StreamAutonomousStarted(带 per-turn stream + 新 assistant 行)", func() {
			assert.True(t, sawStarted, "应 emit StreamAutonomousStarted")
			assert.Equal(t, chat_svc.AutonomousStreamName(100), startedName)
			assert.Equal(t, chat_svc.StreamName(100, 2001), startedStream)
			assert.Equal(t, "background_task", startedTrigger)
			assert.True(t, startedHasMsg, "应携带 AssistantMessage 供前端插入")
		})

		convey.Convey("实时 stream chunk + StreamDone", func() {
			assert.Contains(t, chunk, "autonomous:listing")
			assert.True(t, sawDone, "应 emit StreamDone")
		})

		convey.Convey("session 收尾翻 idle", func() {
			assert.Equal(t, "idle", sess.AgentStatus)
		})
	})
}

// TestStartAutonomousWatcher_DedupesAndExitsOnClose 验证 watcher 生命周期:每会话
// 只起一个(去重),底层 AutonomousTurns channel close 后干净退出并清去重位。
func TestStartAutonomousWatcher_DedupesAndExitsOnClose(t *testing.T) {
	convey.Convey("watcher 每会话一个 + channel close 后退出", t, func() {
		m := setupChatTest(t)
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		src := mock_agentruntime.NewMockAutonomousTurnSource(ctrl)
		be := &agent_backend_entity.AgentBackend{ID: 12, Type: "claudecode"}

		ch := make(chan *agentruntime.AutonomousTurn) // 不带值,保持 open
		called := make(chan struct{})
		// 用 <-chan(单向)返回。Times(1) 即验证去重:第二次 start 不再订阅。
		src.EXPECT().AutonomousTurns(int64(100)).
			DoAndReturn(func(int64) <-chan agentruntime.AutonomousTurn {
				out := make(chan agentruntime.AutonomousTurn)
				go func() {
					defer close(out)
					close(called)
					for at := range ch {
						out <- *at
					}
				}()
				return out
			}).Times(1)

		chat_svc.StartAutonomousWatcherForTest(m.svc, 100, be, src)
		<-called // watcher goroutine 已订阅,去重位已占
		assert.True(t, chat_svc.IsAutonomousWatcherActiveForTest(m.svc, 100))

		// 第二次 start:被去重,不再调 AutonomousTurns(Times(1) 验证)。
		chat_svc.StartAutonomousWatcherForTest(m.svc, 100, be, src)

		close(ch) // 让底层 channel close → watcher 退出
		require.Eventually(t, func() bool {
			return !chat_svc.IsAutonomousWatcherActiveForTest(m.svc, 100)
		}, time.Second, 5*time.Millisecond, "watcher 应在 channel close 后退出并清去重位")
	})
}

// TestRunTurn_MountsAutonomousWatcher 验证 runTurn 在 runner 实现 AutonomousTurnSource
// 时(Run 完成、session 已 spawn 后)惰性挂上每会话 watcher。
func TestRunTurn_MountsAutonomousWatcher(t *testing.T) {
	convey.Convey("runTurn 惰性挂 autonomous watcher", t, func() {
		t.Setenv("AGENTRE_DATA_DIR", t.TempDir())
		m := setupChatTest(t)
		ctx := m.ctx

		runner := &autoTurnRunner{autoCh: make(chan agentruntime.AutonomousTurn)}
		t.Cleanup(func() { close(runner.autoCh) }) // 让 watcher 在测试结束后退出,不泄漏
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, runner)
		t.Cleanup(restore)

		sess := &chat_entity.Session{ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE}
		backend := &agent_backend_entity.AgentBackend{ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-11", Status: consts.ACTIVE}
		ag := &agent_entity.Agent{ID: 7, Name: "Builtin", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`}
		prov := &llm_provider_entity.LLMProvider{ID: 11, Type: string(llm_provider_entity.TypeAnthropic), Model: "m", Status: consts.ACTIVE}

		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil)
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(ag, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(backend, nil)
		m.provider.EXPECT().FindByKey(gomock.Any(), "key-11").Return(prov, nil)
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()
		m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()
		m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()
		m.dbMock.ExpectBegin()
		m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
		m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
				if msg.Role == "user" {
					msg.ID = 1000
				} else {
					msg.ID = 1001
				}
				return nil
			}).Times(2)
		m.dbMock.ExpectCommit()

		resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
		require.NoError(t, err)
		chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

		require.Eventually(t, func() bool {
			return chat_svc.IsAutonomousWatcherActiveForTest(m.svc, 100)
		}, time.Second, 5*time.Millisecond, "runTurn 应在 Run 后挂上 watcher")
	})
}
