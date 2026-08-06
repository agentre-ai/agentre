package piagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/cago/pkg/logger"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/piagent/mcpbridge"
	"github.com/agentre-ai/agentre/internal/pkg/cliprocess"
	pkgpiagent "github.com/agentre-ai/agentre/pkg/piagent"
)

func TestPiAgentCapabilities(t *testing.T) {
	Convey("Given pi-agent runtime", t, func() {
		caps := New().Capabilities()

		Convey("When checking supported controls Then it mirrors implemented Pi RPC controls", func() {
			So(caps.Has(capability.CapSteer), ShouldBeTrue)
			So(caps.Has(capability.CapAbort), ShouldBeTrue)
			So(caps.Has(capability.CapImageInput), ShouldBeTrue)
			So(caps.Has(capability.CapCompact), ShouldBeTrue)
			So(caps.Has(capability.CapReportContextWindow), ShouldBeTrue)
			So(caps.Has(capability.CapForkSession), ShouldBeTrue)
			So(caps.Has(capability.CapSetPermission), ShouldBeFalse)
			So(caps.Has(capability.CapCancelSteer), ShouldBeFalse)
			So(caps.Has(capability.CapDrainSteer), ShouldBeFalse)
			So(caps.Has(capability.CapToolPermission), ShouldBeFalse)
			// CapMCPTools=true:pi-agent 经内嵌桥扩展消费 RunRequest.MCPServers。
			So(caps.Has(capability.CapMCPTools), ShouldBeTrue)
		})

		Convey("When comparing optional interfaces Then advertised controls match implementations", func() {
			r := any(New())
			_, steerer := r.(agentruntime.Steerer)
			_, aborter := r.(agentruntime.Aborter)
			_, setter := r.(agentruntime.PermissionModeSetter)
			_, canceler := r.(agentruntime.SteerCanceler)
			_, drainer := r.(agentruntime.SteerDrainer)

			So(steerer, ShouldEqual, caps.Has(capability.CapSteer))
			So(aborter, ShouldEqual, caps.Has(capability.CapAbort))
			So(setter, ShouldEqual, caps.Has(capability.CapSetPermission))
			So(canceler, ShouldEqual, caps.Has(capability.CapCancelSteer))
			So(drainer, ShouldEqual, caps.Has(capability.CapDrainSteer))
		})
	})
}

func TestRun_ForksAndReturnsNativeSessionState(t *testing.T) {
	Convey("Given a resumed Pi session and an exact user-entry fork anchor", t, func() {
		sess := &fakeSession{
			stream: &scriptedStream{anchor: "new-user"},
			sid:    "session-new",
		}
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (sessionHandle, error) {
			return sess, nil
		})
		defer restore()

		Convey("When the runtime runs the prompt Then it forwards the anchor and returns native session state", func() {
			events, result, err := New().Run(context.Background(), agentruntime.RunRequest{
				Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				SessionID:         1,
				ProviderSessionID: "session-old",
				ForkAnchor:        "fork-user",
				Cwd:               t.TempDir(),
				UserText:          "repeat",
			})
			So(err, ShouldBeNil)
			for range events {
			}

			So(sess.gotForkAnchor, ShouldEqual, "fork-user")
			So(result.ProviderSessionID, ShouldEqual, "session-new")
			So(result.UserAnchor, ShouldEqual, "new-user")
		})
	})
}

func TestRun_CanceledAcceptedTurnReturnsSettledUserAnchor(t *testing.T) {
	stream := &acceptedStopStream{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	sess := &fakeSession{
		stream:      stream,
		sid:         "session-accepted",
		interrupter: &acceptedStopInterruptor{stream: stream},
	}
	restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (sessionHandle, error) {
		return sess, nil
	})
	defer restore()
	runtime := New()
	ctx, cancel := context.WithCancel(context.Background())
	events, result, err := runtime.Run(ctx, agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
		SessionID: 2,
		Cwd:       t.TempDir(),
		UserText:  "stop after acceptance",
	})
	require.NoError(t, err)
	<-stream.started

	cancel()
	require.NoError(t, runtime.Abort(context.Background(), 2))
	for range events {
	}

	assert.Equal(t, "pi-user-anchor-after-stop", result.UserAnchor)
	assert.ErrorIs(t, result.StopErr, context.Canceled)
}

func TestPrepareRunWithholdsPromptUntilStart(t *testing.T) {
	Convey("Given a resumed Pi session with a fork anchor", t, func() {
		lines := []string{
			`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-old"}}`,
			`{"id":"session-fork","type":"response","command":"fork","success":true,"data":{"cancelled":false}}`, //nolint:misspell // Pi RPC field uses British spelling.
			`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-new"}}`,
			`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[],"leafId":null}}`,
			`{"type":"response","command":"prompt","success":true}`,
			`{"type":"agent_end","messages":[],"willRetry":false}`,
			`{"type":"agent_settled"}`,
			`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"turn-user","parentId":null,"message":{"role":"user"}}],"leafId":"turn-user"}}`,
			`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		}
		proc := &runtimeRPCProcess{
			stdin:  &cliprocess.LockedBuffer{},
			stdout: strings.NewReader(strings.Join(lines, "\n") + "\n"),
			done:   make(chan error, 1),
		}
		restore := SetSessionFactoryForTest(runtimeRPCSessionFactory(proc))
		defer restore()
		runtime := New()

		Convey("When the service preflights Then the forked ID is available while prompt waits for Start", func() {
			prepared, err := runtime.PrepareRun(context.Background(), agentruntime.RunRequest{
				Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				SessionID:         3,
				ProviderSessionID: "session-old",
				ForkAnchor:        "fork-user",
				Cwd:               t.TempDir(),
				UserText:          "commit first",
			})
			So(err, ShouldBeNil)
			identity, ok := prepared.(PreparedRunIdentity)
			So(ok, ShouldBeTrue)
			So(identity.ProviderSessionID(), ShouldEqual, "session-new")
			So(proc.commands(), ShouldResemble, []string{"get_state", "fork", "get_state", "get_entries"})

			events, result, err := prepared.Start(context.Background())
			So(err, ShouldBeNil)
			for range events {
			}
			So(result.ProviderSessionID, ShouldEqual, identity.ProviderSessionID())
			So(result.UserAnchor, ShouldEqual, "turn-user")
			So(proc.commands(), ShouldResemble, []string{
				"get_state", "fork", "get_state", "get_entries", "prompt", "get_entries", "get_session_stats",
			})
		})
	})
}

func TestPreparedRunCloseBeforeStartSendsNoPrompt(t *testing.T) {
	Convey("Given a prepared Pi fork whose caller abandons ownership", t, func() {
		lines := []string{
			`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-old"}}`,
			`{"id":"session-fork","type":"response","command":"fork","success":true,"data":{"cancelled":false}}`, //nolint:misspell // Pi RPC field uses British spelling.
			`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-new"}}`,
			`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[],"leafId":null}}`,
		}
		proc := &runtimeRPCProcess{
			stdin:  &cliprocess.LockedBuffer{},
			stdout: strings.NewReader(strings.Join(lines, "\n") + "\n"),
			done:   make(chan error, 1),
		}
		restore := SetSessionFactoryForTest(runtimeRPCSessionFactory(proc))
		defer restore()
		prepared, err := New().PrepareRun(context.Background(), agentruntime.RunRequest{
			Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
			SessionID:         4,
			ProviderSessionID: "session-old",
			ForkAnchor:        "fork-user",
			Cwd:               t.TempDir(),
			UserText:          "must not be sent",
		})
		So(err, ShouldBeNil)

		Convey("When Close wins before Start Then cleanup is idempotent and Start stays prompt-free", func() {
			So(prepared.Close(context.Background()), ShouldBeNil)
			So(prepared.Close(context.Background()), ShouldBeNil)
			events, result, startErr := prepared.Start(context.Background())
			So(startErr, ShouldNotBeNil)
			So(events, ShouldBeNil)
			So(result, ShouldBeNil)
			So(proc.commands(), ShouldResemble, []string{"get_state", "fork", "get_state", "get_entries"})
		})
	})
}

func TestRun_PreservesCompletedAnswerWhenUserAnchorMetadataFails(t *testing.T) {
	Convey("Given Pi completes the assistant answer but final user-anchor metadata is unavailable", t, func() {
		proc := newRuntimeRPCProcessWithMetadataFailure()
		restore := SetSessionFactoryForTest(runtimeRPCSessionFactory(proc))
		defer restore()

		Convey("When the runtime drains Then it keeps Done and leaves the user anchor empty", func() {
			events, result, err := New().Run(context.Background(), agentruntime.RunRequest{
				Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				SessionID:         2,
				ProviderSessionID: "session-old",
				Cwd:               t.TempDir(),
				UserText:          "hello",
			})
			So(err, ShouldBeNil)

			var text strings.Builder
			done := false
			failed := false
			for event := range events {
				switch event := event.(type) {
				case agentruntime.TextDelta:
					text.WriteString(event.Text)
				case agentruntime.Done:
					done = true
				case agentruntime.ErrorEvent:
					failed = true
				}
			}

			So(text.String(), ShouldEqual, "completed answer")
			So(done, ShouldBeTrue)
			So(failed, ShouldBeFalse)
			So(result.StopErr, ShouldBeNil)
			So(result.ProviderSessionID, ShouldEqual, "session-old")
			So(result.UserAnchor, ShouldBeEmpty)
			So(proc.commands(), ShouldResemble, []string{
				"get_state", "get_entries", "prompt", "get_entries", "get_session_stats",
			})
		})
	})
}

func TestDefaultModelForBackend(t *testing.T) {
	Convey("Given a pi-agent backend using ~/.pi/agent config", t, func() {
		Convey("When reasoning_effort is set, then Agentre leaves model empty so pi uses user defaultProvider/defaultModel and thinking stays separate", func() {
			model := defaultModelForBackend(&agent_backend_entity.AgentBackend{
				Type:            string(agent_backend_entity.TypePiAgent),
				ReasoningEffort: "high",
			})

			So(model, ShouldEqual, fallbackModelID)
			So(model, ShouldEqual, "")
		})
	})
}

func TestRun_DefaultModelWhenProviderMissing(t *testing.T) {
	Convey("Given pi-agent CLI login runtime", t, func() {
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (sessionHandle, error) {
			return &fakeSession{stream: &emptyStream{}, sid: "pi-session"}, nil
		})
		defer restore()

		Convey("When running without provider Then result has Pi default model and session id", func() {
			events, result, err := New().Run(context.Background(), agentruntime.RunRequest{
				Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				SessionID: 1,
				Cwd:       t.TempDir(),
				UserText:  "hello",
			})
			So(err, ShouldBeNil)
			for range events {
			}
			So(result.Model, ShouldEqual, fallbackModelID)
			So(result.ProviderSessionID, ShouldEqual, "pi-session")
		})
	})
}

func TestRun_MapsMissingNativeSession(t *testing.T) {
	Convey("Given Pi reports that the requested native session no longer exists", t, func() {
		sess := &fakeSession{streamErr: pkgpiagent.ErrSessionNotFound}
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (sessionHandle, error) {
			return sess, nil
		})
		defer restore()

		Convey("When the runtime starts the turn Then it returns the backend-neutral sentinel", func() {
			events, result, err := New().Run(context.Background(), agentruntime.RunRequest{
				Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				SessionID:         1,
				ProviderSessionID: "pi-native-gone",
				ForkAnchor:        "fork-user",
				Cwd:               t.TempDir(),
				UserText:          "hello",
			})

			So(events, ShouldBeNil)
			So(result, ShouldBeNil)
			So(errors.Is(err, agentruntime.ErrSessionNotFound), ShouldBeTrue)
			So(sess.gotForkAnchor, ShouldEqual, "fork-user")
			So(sess.closed, ShouldBeTrue)
		})
	})
}

func TestRun_ClosesSessionAfterDrain(t *testing.T) {
	Convey("Given a pi-agent session", t, func() {
		sess := &fakeSession{stream: &emptyStream{}, sid: "pi-session"}
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (sessionHandle, error) {
			return sess, nil
		})
		defer restore()

		Convey("When Run drains Then the session is closed", func() {
			events, _, err := New().Run(context.Background(), agentruntime.RunRequest{
				Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				SessionID: 1,
				Cwd:       t.TempDir(),
				UserText:  "hello",
			})
			So(err, ShouldBeNil)
			for range events {
			}
			So(sess.closed, ShouldBeTrue)
		})
	})
}

func TestRun_StaleCleanupCannotUnregisterNewerGeneration(t *testing.T) {
	firstCloseStarted := make(chan struct{})
	allowFirstClose := make(chan struct{})
	secondRelease := make(chan struct{})
	releaseSecond := sync.OnceFunc(func() { close(secondRelease) })
	defer releaseSecond()
	secondInterrupted := make(chan struct{}, 1)
	first := &fakeSession{
		stream:       &emptyStream{},
		sid:          "shared-native-session",
		closeStarted: firstCloseStarted,
		allowClose:   allowFirstClose,
	}
	second := &fakeSession{
		stream:      &blockingStream{release: secondRelease},
		sid:         "shared-native-session",
		interrupter: &recordingInterruptor{called: secondInterrupted},
	}
	var (
		factoryMu sync.Mutex
		created   int
	)
	restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (sessionHandle, error) {
		factoryMu.Lock()
		defer factoryMu.Unlock()
		created++
		if created == 1 {
			return first, nil
		}
		return second, nil
	})
	defer restore()
	runtime := New()
	req := agentruntime.RunRequest{
		Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
		SessionID:         91,
		ProviderSessionID: "shared-native-session",
		Cwd:               t.TempDir(),
		UserText:          "ordinary resumed turn",
	}

	firstEvents, _, err := runtime.Run(context.Background(), req)
	require.NoError(t, err)
	<-firstCloseStarted

	secondEvents, _, err := runtime.Run(context.Background(), req)
	require.NoError(t, err)
	close(allowFirstClose)
	for range firstEvents {
	}

	require.NoError(t, runtime.Abort(context.Background(), req.SessionID),
		"generation A's deferred unregister must not remove generation B")
	select {
	case <-secondInterrupted:
	case <-time.After(time.Second):
		t.Fatal("Abort did not reach the newer generation owner")
	}

	releaseSecond()
	for range secondEvents {
	}
}

func TestPreparedRun_StaleCloseCannotRemoveNewerGenerationMCPConfig(t *testing.T) {
	t.Setenv("AGENTRE_DATA_DIR", t.TempDir())
	first := &fakeSession{stream: &emptyStream{}, sid: "shared-native-session"}
	second := &fakeSession{stream: &emptyStream{}, sid: "shared-native-session"}
	created := 0
	restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (sessionHandle, error) {
		created++
		if created == 1 {
			return first, nil
		}
		return second, nil
	})
	defer restore()
	runtime := New()
	req := agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
		SessionID: 92,
		Cwd:       t.TempDir(),
		MCPServers: []agentruntime.MCPServerSpec{{
			Name: "group", URL: "http://127.0.0.1:1/mcp/group/",
		}},
	}

	firstPrepared, err := runtime.PrepareRun(context.Background(), req)
	require.NoError(t, err)
	secondPrepared, err := runtime.PrepareRun(context.Background(), req)
	require.NoError(t, err)
	configPath, err := mcpbridge.RenderConfig(req.MCPServers, req.SessionID)
	require.NoError(t, err)

	require.NoError(t, firstPrepared.Close(context.Background()))
	_, err = os.Stat(configPath)
	require.NoError(t, err, "generation A must not remove generation B's MCP config")

	require.NoError(t, secondPrepared.Close(context.Background()))
	_, err = os.Stat(configPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestRun_ClosesOutputAfterSessionClose(t *testing.T) {
	Convey("Given a pi-agent session whose cleanup is still running", t, func() {
		closeStarted := make(chan struct{})
		allowClose := make(chan struct{})
		sess := &fakeSession{
			stream:       &emptyStream{},
			sid:          "pi-session",
			closeStarted: closeStarted,
			allowClose:   allowClose,
		}
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (sessionHandle, error) {
			return sess, nil
		})
		defer restore()

		Convey("When the stream has drained Then output remains open until session cleanup returns", func() {
			events, _, err := New().Run(context.Background(), agentruntime.RunRequest{
				Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				SessionID: 1,
				Cwd:       t.TempDir(),
				UserText:  "hello",
			})
			So(err, ShouldBeNil)

			<-closeStarted
			for len(events) > 0 {
				<-events
			}
			select {
			case _, open := <-events:
				if !open {
					t.Fatal("runtime closed output before session cleanup returned")
				}
				t.Fatal("runtime emitted an unexpected event while session cleanup was blocked")
			default:
			}

			close(allowClose)
			for range events {
			}
			So(sess.closed, ShouldBeTrue)
		})
	})
}

func TestRun_ForwardsUserBlockImagesToStream(t *testing.T) {
	Convey("Given a pi-agent turn carrying an inline image block", t, func() {
		sess := &fakeSession{stream: &emptyStream{}, sid: "pi-session"}
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (sessionHandle, error) {
			return sess, nil
		})
		defer restore()

		Convey("When Run executes Then the image reaches Pi as a multimodal attachment", func() {
			events, _, err := New().Run(context.Background(), agentruntime.RunRequest{
				Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				SessionID: 1,
				Cwd:       t.TempDir(),
				UserText:  "what is this?",
				UserBlocks: []cagoblocks.ContentBlock{
					cagoblocks.TextBlock{Text: "what is this?"},
					cagoblocks.ImageBlock{MediaType: "image/png", Source: cagoblocks.BlobSource{Inline: []byte{1, 2, 3}}},
				},
			})
			So(err, ShouldBeNil)
			for range events {
			}
			So(sess.gotImages, ShouldHaveLength, 1)
			So(sess.gotImages[0].MimeType, ShouldEqual, "image/png")
			So(string(sess.gotImages[0].Data), ShouldEqual, string([]byte{1, 2, 3}))
		})
	})
}

func TestRun_PiFailuresStayRedactedAtStartupAndDownstream(t *testing.T) {
	secrets := []string{
		"private user prompt: inspect acquisition payroll",
		"PRIVATE_IMAGE_SESSION_BYTES",
		"session-entry-private-history",
		"Authorization: Bearer private-provider-token",
		"stderr private process payload",
		"command failed with private process arguments",
	}
	imageWire := base64.StdEncoding.EncodeToString([]byte(secrets[1]))
	commonStartup := []string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"pi-session-689"}}`,
		`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"user","content":"` + secrets[2] + `"}}],"leafId":"before-leaf"}}`,
	}
	tests := []struct {
		name            string
		lines           []string
		stderr          string
		waitErr         error
		startupError    bool
		wantDiagnostics bool
		wantUsage       bool
		wantErrorType   string
	}{
		{
			name: "RPC failure payload",
			lines: append(append([]string{}, commonStartup...),
				`{"type":"response","command":"prompt","success":false,"error":"`+secrets[0]+` | `+secrets[3]+`","data":{"message":"`+secrets[2]+`"}}`,
			),
			startupError: true,
		},
		{
			name: "terminal event failure payload",
			lines: append(append([]string{}, commonStartup...),
				`{"type":"response","command":"prompt","success":true}`,
				`{"type":"message_end","message":{"role":"assistant","content":"`+secrets[2]+`","model":"gpt-5.5(xhigh)","usage":{"input":4017,"output":128,"cacheRead":69632,"cacheWrite":0}}}`,
				`{"type":"agent_end","messages":[{"role":"assistant","content":"`+secrets[2]+`","stopReason":"error","errorMessage":"`+secrets[0]+` | `+secrets[3]+`"}],"willRetry":false}`,
				`{"type":"agent_settled"}`,
				`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"user"}},{"type":"message","id":"turn-user","parentId":"before-leaf","message":{"role":"user","content":"`+secrets[0]+`","images":[{"data":"`+imageWire+`"}]}}],"leafId":"turn-user"}}`,
				`{"type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"contextWindow":1050000}}}`,
			),
			stderr:          secrets[4],
			wantDiagnostics: true,
			wantUsage:       true,
			wantErrorType:   "*errors.errorString",
		},
		{
			name: "process exit and stderr payload",
			lines: append(append([]string{}, commonStartup...),
				`{"type":"response","command":"prompt","success":true}`,
			),
			stderr:  secrets[4] + " | " + secrets[3],
			waitErr: errors.New(secrets[5]),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc := &runtimeRPCProcess{
				stdin:  &cliprocess.LockedBuffer{},
				stdout: strings.NewReader(strings.Join(tt.lines, "\n") + "\n"),
				stderr: strings.NewReader(tt.stderr),
				done:   make(chan error, 1),
			}
			if tt.waitErr != nil {
				proc.done <- tt.waitErr
			}
			restoreFactory := SetSessionFactoryForTest(runtimeRPCSessionFactory(proc))
			defer restoreFactory()
			core, logs := observer.New(zapcore.DebugLevel)
			ctx := logger.WithContextLogger(context.Background(), zap.New(core))

			events, result, err := New().Run(ctx, agentruntime.RunRequest{
				Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				SessionID:         689,
				AgentID:           8,
				ProviderSessionID: "pi-session-689",
				Cwd:               t.TempDir(),
				UserText:          secrets[0],
				UserBlocks: []cagoblocks.ContentBlock{
					cagoblocks.TextBlock{Text: secrets[0]},
					cagoblocks.ImageBlock{MediaType: "image/png", Source: cagoblocks.BlobSource{Inline: []byte(secrets[1])}},
				},
			})
			if tt.startupError {
				require.Error(t, err)
				assert.Nil(t, events)
				assert.Nil(t, result)
				assert.Equal(t, "piagent rpc prompt failed", err.Error())
				for _, secret := range append(secrets, imageWire) {
					assert.NotContains(t, err.Error(), secret)
				}
			} else {
				require.NoError(t, err)

				var downstreamErr error
				for event := range events {
					if failure, ok := event.(agentruntime.ErrorEvent); ok {
						downstreamErr = failure.Err
					}
				}
				require.Error(t, downstreamErr)
				require.Error(t, result.StopErr)
				assert.Equal(t, downstreamErr.Error(), result.StopErr.Error())
				for _, secret := range append(secrets, imageWire) {
					assert.NotContains(t, downstreamErr.Error(), secret)
					assert.NotContains(t, result.StopErr.Error(), secret)
				}
				if tt.waitErr != nil {
					var exitErr *pkgpiagent.ExitError
					require.ErrorAs(t, result.StopErr, &exitErr)
					assert.Empty(t, exitErr.Stderr)
					for _, secret := range secrets {
						assert.NotContains(t, exitErr.Err.Error(), secret)
					}
				}
			}

			for _, entry := range logs.All() {
				for _, value := range entry.ContextMap() {
					for _, secret := range append(secrets, imageWire) {
						assert.NotContains(t, fmt.Sprint(value), secret)
					}
				}
			}
			matches := logs.FilterMessage("piagent runtime: turn failed").All()
			diagnostics := logs.FilterMessage("piagent runtime: turn failed diagnostics").All()
			if tt.startupError {
				assert.Empty(t, matches, "a rejected prompt must fail before a turn is registered")
				assert.Empty(t, diagnostics)
				return
			}
			require.Len(t, matches, 1)
			fields := matches[0].ContextMap()
			assert.Equal(t, int64(689), fields["sessionID"])
			assert.Equal(t, int64(8), fields["agentID"])
			assert.Equal(t, "pi-session-689", fields["providerSessionID"])
			_, hasRawError := fields["error"]
			assert.False(t, hasRawError)
			if tt.wantErrorType != "" {
				assert.Equal(t, tt.wantErrorType, fields["errorType"])
			}
			if tt.wantUsage {
				assert.Equal(t, "gpt-5.5(xhigh)", fields["model"])
				assert.Equal(t, int64(1050000), fields["contextWindow"])
				assert.Equal(t, int64(4017), fields["promptTokens"])
				assert.Equal(t, int64(128), fields["completionTokens"])
				assert.Equal(t, int64(69632), fields["cachedTokens"])
				assert.Equal(t, int64(0), fields["cacheCreationTokens"])
				assert.Equal(t, int64(73649), fields["totalInputTokens"])
			}
			if tt.wantDiagnostics {
				require.Len(t, diagnostics, 1)
				diagnosticFields := diagnostics[0].ContextMap()
				assert.Equal(t, "agent_end", diagnosticFields["piEventType"])
				assert.Equal(t, "error", diagnosticFields["piStopReason"])
			} else {
				assert.Empty(t, diagnostics)
			}
		})
	}
}

type fakeSession struct {
	stream        stream
	sid           string
	interrupter   interruptable
	gotImages     []pkgpiagent.Image
	gotPrompt     string
	gotForkAnchor string
	streamCall    int
	streamErr     error
	closed        bool
	closeStarted  chan struct{}
	allowClose    <-chan struct{}
}

func (s *fakeSession) Close(context.Context) error {
	if s.closeStarted != nil {
		close(s.closeStarted)
	}
	if s.allowClose != nil {
		<-s.allowClose
	}
	s.closed = true
	return nil
}
func (s *fakeSession) ID() string { return s.sid }
func (s *fakeSession) Stream(_ context.Context, prompt, _ string, images []pkgpiagent.Image) (stream, error) {
	s.streamCall++
	s.gotPrompt = prompt
	s.gotImages = images
	return s.stream, s.streamErr
}
func (s *fakeSession) StreamTurn(ctx context.Context, prompt, mode string, images []pkgpiagent.Image, turn turnSpec) (stream, error) {
	s.gotForkAnchor = turn.forkAnchor
	return s.Stream(ctx, prompt, mode, images)
}
func (s *fakeSession) Compact(context.Context) (stream, error)          { return s.stream, s.streamErr }
func (s *fakeSession) RewindTo(context.Context, string) (string, error) { return s.sid, nil }
func (s *fakeSession) ActiveStream() steerStream                        { return nil }
func (s *fakeSession) ActiveInterruptor() interruptable                 { return s.interrupter }

type recordingInterruptor struct {
	called chan<- struct{}
}

func (i *recordingInterruptor) Interrupt(context.Context) error {
	select {
	case i.called <- struct{}{}:
	default:
	}
	return nil
}

type acceptedStopStream struct {
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	mu        sync.Mutex
	anchor    string
}

func (s *acceptedStopStream) Next() bool {
	s.startOnce.Do(func() { close(s.started) })
	<-s.release
	return false
}
func (*acceptedStopStream) Event() pkgpiagent.Event { return pkgpiagent.Event{} }
func (*acceptedStopStream) SessionID() string       { return "session-accepted" }
func (*acceptedStopStream) Err() error              { return context.Canceled }
func (s *acceptedStopStream) UserAnchor() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.anchor
}
func (s *acceptedStopStream) settle() {
	s.mu.Lock()
	s.anchor = "pi-user-anchor-after-stop"
	s.mu.Unlock()
	s.stopOnce.Do(func() { close(s.release) })
}

type acceptedStopInterruptor struct {
	stream *acceptedStopStream
}

func (i *acceptedStopInterruptor) Interrupt(context.Context) error {
	i.stream.settle()
	return nil
}

type blockingStream struct {
	release <-chan struct{}
}

func (s *blockingStream) Next() bool {
	<-s.release
	return false
}
func (*blockingStream) Event() pkgpiagent.Event { return pkgpiagent.Event{} }
func (*blockingStream) SessionID() string       { return "" }
func (*blockingStream) Err() error              { return nil }

type emptyStream struct{}

func (*emptyStream) Next() bool              { return false }
func (*emptyStream) Event() pkgpiagent.Event { return pkgpiagent.Event{} }
func (*emptyStream) SessionID() string       { return "" }
func (*emptyStream) Err() error              { return nil }

type scriptedStream struct {
	events      []pkgpiagent.Event
	idx         int
	err         error
	sid         string
	anchor      string
	diagnostics pkgpiagent.StreamDiagnostics
}

func (s *scriptedStream) Next() bool {
	if s.idx >= len(s.events) {
		return false
	}
	s.idx++
	return true
}

func (s *scriptedStream) Event() pkgpiagent.Event { return s.events[s.idx-1] }
func (s *scriptedStream) SessionID() string       { return s.sid }
func (s *scriptedStream) Err() error              { return s.err }
func (s *scriptedStream) UserAnchor() string      { return s.anchor }
func (s *scriptedStream) Diagnostics() pkgpiagent.StreamDiagnostics {
	return s.diagnostics
}

type runtimeRPCRunner struct {
	proc cliprocess.Handle
}

func (r *runtimeRPCRunner) Start(context.Context, cliprocess.Options) (cliprocess.Handle, error) {
	return r.proc, nil
}

type runtimeRPCProcess struct {
	stdin  *cliprocess.LockedBuffer
	stdout io.Reader
	stderr io.Reader
	done   chan error
}

func newRuntimeRPCProcessWithMetadataFailure() *runtimeRPCProcess {
	lines := []string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-old"}}`,
		`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"assistant"}}],"leafId":"before-leaf"}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"completed answer"}}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		`{"id":"session-entries-after","type":"response","command":"get_entries","success":false,"error":"metadata unavailable"}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
	}
	return &runtimeRPCProcess{
		stdin:  &cliprocess.LockedBuffer{},
		stdout: strings.NewReader(strings.Join(lines, "\n") + "\n"),
		done:   make(chan error, 1),
	}
}

func runtimeRPCSessionFactory(proc *runtimeRPCProcess) func(agentruntime.RunRequest, map[string]string, string) (sessionHandle, error) {
	return func(req agentruntime.RunRequest, _ map[string]string, _ string) (sessionHandle, error) {
		client := pkgpiagent.New(
			pkgpiagent.WithRPCProcessRunnerForTesting(&runtimeRPCRunner{proc: proc}),
			pkgpiagent.WithSession(req.ProviderSessionID),
		)
		return &clientAdapter{client: client, sid: req.ProviderSessionID}, nil
	}
}

func (p *runtimeRPCProcess) Stdin() io.Writer  { return p.stdin }
func (p *runtimeRPCProcess) Stdout() io.Reader { return p.stdout }
func (p *runtimeRPCProcess) Stderr() io.Reader {
	if p.stderr != nil {
		return p.stderr
	}
	return strings.NewReader("")
}
func (p *runtimeRPCProcess) Wait() error { return <-p.done }
func (p *runtimeRPCProcess) Kill() error { return p.finish() }
func (p *runtimeRPCProcess) Signal(os.Signal) error {
	return p.finish()
}

func (p *runtimeRPCProcess) finish() error {
	select {
	case p.done <- nil:
	default:
	}
	return nil
}

func (p *runtimeRPCProcess) commands() []string {
	frames := p.stdinFrames()
	out := make([]string, 0, len(frames))
	for _, frame := range frames {
		if command, ok := frame["type"].(string); ok {
			out = append(out, command)
		}
	}
	return out
}

func (p *runtimeRPCProcess) stdinFrames() []map[string]any {
	lines := strings.Split(strings.TrimSpace(p.stdin.String()), "\n")
	frames := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var frame map[string]any
		if json.Unmarshal([]byte(line), &frame) == nil {
			frames = append(frames, frame)
		}
	}
	return frames
}
