package piagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/cago/pkg/logger"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
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

		Convey("When the service preflights Then fork completes but prompt waits for Start", func() {
			prepared, err := runtime.PrepareRun(context.Background(), agentruntime.RunRequest{
				Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				SessionID:         3,
				ProviderSessionID: "session-old",
				ForkAnchor:        "fork-user",
				Cwd:               t.TempDir(),
				UserText:          "commit first",
			})
			So(err, ShouldBeNil)
			So(proc.commands(), ShouldResemble, []string{"get_state", "fork", "get_state", "get_entries"})

			events, result, err := prepared.Start(context.Background())
			So(err, ShouldBeNil)
			for range events {
			}
			So(result.ProviderSessionID, ShouldEqual, "session-new")
			So(result.UserAnchor, ShouldEqual, "turn-user")
			So(proc.commands(), ShouldResemble, []string{
				"get_state", "fork", "get_state", "get_entries", "prompt", "get_entries", "get_session_stats",
			})
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

func TestRun_LogsPiStreamFailureDiagnostics(t *testing.T) {
	Convey("Given a pi-agent stream that fails after reporting model and usage", t, func() {
		const redactionMarker = "private-payload-marker"
		boom := errors.New("piagent: terminated " + redactionMarker)
		sess := &fakeSession{
			stream: &scriptedStream{events: []pkgpiagent.Event{
				{Kind: pkgpiagent.EventUsage, Model: "gpt-5.5(xhigh)", Usage: provider.Usage{
					PromptTokens:        4017,
					CompletionTokens:    128,
					CachedTokens:        69632,
					CacheCreationTokens: 0,
				}},
				{Kind: pkgpiagent.EventContextWindow, ContextWindow: 1050000},
				{Kind: pkgpiagent.EventError, Err: boom},
			}, err: boom, sid: "pi-session-689", diagnostics: pkgpiagent.StreamDiagnostics{
				FinalErrorEventType:  "agent_end",
				FinalErrorStopReason: "error",
				FinalErrorMessage:    "provider failed " + redactionMarker,
				FinalErrorFrame:      `{"type":"agent_end","messages":[{"content":"` + redactionMarker + `"}]}`,
				StderrTail:           "stderr " + redactionMarker,
			}},
			sid: "pi-session-689",
		}
		restoreFactory := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (sessionHandle, error) {
			return sess, nil
		})
		defer restoreFactory()
		core, logs := observer.New(zapcore.DebugLevel)
		ctx := logger.WithContextLogger(context.Background(), zap.New(core))

		Convey("When the turn drains Then runtime logs enough fields to diagnose future Pi terminated failures", func() {
			events, result, err := New().Run(ctx, agentruntime.RunRequest{
				Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				SessionID: 689,
				AgentID:   8,
				Cwd:       t.TempDir(),
				UserText:  "检查一下pi agent能否支持mcp，实现群聊功能",
			})
			So(err, ShouldBeNil)
			for range events {
			}

			So(result.StopErr, ShouldEqual, boom)
			for _, entry := range logs.All() {
				for _, value := range entry.ContextMap() {
					So(fmt.Sprint(value), ShouldNotContainSubstring, redactionMarker)
				}
			}
			matches := logs.FilterMessage("piagent runtime: turn failed").All()
			So(matches, ShouldHaveLength, 1)
			fields := matches[0].ContextMap()
			So(fields["sessionID"], ShouldEqual, int64(689))
			So(fields["agentID"], ShouldEqual, int64(8))
			So(fields["providerSessionID"], ShouldEqual, "pi-session-689")
			So(fields["model"], ShouldEqual, "gpt-5.5(xhigh)")
			So(fields["contextWindow"], ShouldEqual, int64(1050000))
			So(fields["promptTokens"], ShouldEqual, int64(4017))
			So(fields["completionTokens"], ShouldEqual, int64(128))
			So(fields["cachedTokens"], ShouldEqual, int64(69632))
			So(fields["cacheCreationTokens"], ShouldEqual, int64(0))
			So(fields["totalInputTokens"], ShouldEqual, int64(73649))
			So(fields["errorType"], ShouldEqual, "*errors.errorString")
			_, hasRawError := fields["error"]
			So(hasRawError, ShouldBeFalse)
			diagnostics := logs.FilterMessage("piagent runtime: turn failed diagnostics").All()
			So(diagnostics, ShouldHaveLength, 1)
			diagnosticFields := diagnostics[0].ContextMap()
			So(diagnosticFields["piEventType"], ShouldEqual, "agent_end")
			So(diagnosticFields["piStopReason"], ShouldEqual, "error")
		})
	})
}

type fakeSession struct {
	stream        stream
	sid           string
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
func (s *fakeSession) ActiveInterruptor() interruptable                 { return nil }

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
func (p *runtimeRPCProcess) Stderr() io.Reader { return strings.NewReader("") }
func (p *runtimeRPCProcess) Wait() error       { return <-p.done }
func (p *runtimeRPCProcess) Kill() error       { return p.finish() }
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
