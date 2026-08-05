package piagent

import (
	"context"
	"errors"
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
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
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
				Cwd:               t.TempDir(),
				UserText:          "hello",
			})

			So(events, ShouldBeNil)
			So(result, ShouldBeNil)
			So(errors.Is(err, agentruntime.ErrSessionNotFound), ShouldBeTrue)
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
		boom := errors.New("piagent: terminated")
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
			}, err: boom, sid: "pi-session-689"},
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
			So(fields["error"], ShouldEqual, "piagent: terminated")
		})
	})
}

func TestRun_ProviderInjectsAPIKeyEnvAndBareModel(t *testing.T) {
	Convey("Given a turn bound to a custom LLM provider", t, func() {
		prov := &llm_provider_entity.LLMProvider{
			ProviderKey: "provabc", APIKey: "tok-super-secret", Model: "deepseek-v3",
			Type: string(llm_provider_entity.TypeAnthropic),
		}
		var gotEnv map[string]string
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, env map[string]string, _ string) (sessionHandle, error) {
			gotEnv = env
			return &fakeSession{stream: &emptyStream{}, sid: "pi-session"}, nil
		})
		defer restore()

		Convey("When running Then the APIKey reaches the factory env and result.Model stays bare", func() {
			events, result, err := New().Run(context.Background(), agentruntime.RunRequest{
				Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				Provider:  prov,
				SessionID: 1,
				Cwd:       t.TempDir(),
				UserText:  "hello",
			})
			So(err, ShouldBeNil)
			for range events {
			}
			So(gotEnv, ShouldNotBeNil)
			So(gotEnv[agentruntime.PiAgentProviderEnvKey(prov.ProviderKey)], ShouldEqual, "tok-super-secret")
			// result.Model 保持裸 req.Provider.Model，不加 agentre-<key>/ 前缀。
			So(result.Model, ShouldEqual, "deepseek-v3")
		})
	})
}

func TestRun_ProviderAPIKeyEmpty_ReturnsConfigErrorWithoutSpawning(t *testing.T) {
	Convey("Given a bound provider with an empty APIKey", t, func() {
		spawned := false
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, _ map[string]string, _ string) (sessionHandle, error) {
			spawned = true
			return &fakeSession{stream: &emptyStream{}, sid: "pi-session"}, nil
		})
		defer restore()

		Convey("When running Then Run returns a config error naming the provider and never spawns", func() {
			_, _, err := New().Run(context.Background(), agentruntime.RunRequest{
				Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				Provider:  &llm_provider_entity.LLMProvider{ProviderKey: "provx", APIKey: "", Model: "m1"},
				SessionID: 1,
				Cwd:       t.TempDir(),
				UserText:  "hello",
			})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "provx")
			So(spawned, ShouldBeFalse)
		})
	})
}

func TestRun_NoProvider_NoEnvInjection(t *testing.T) {
	Convey("Given an unbound pi-agent backend", t, func() {
		var gotEnv map[string]string
		restore := SetSessionFactoryForTest(func(_ agentruntime.RunRequest, env map[string]string, _ string) (sessionHandle, error) {
			gotEnv = env
			return &fakeSession{stream: &emptyStream{}, sid: "pi-session"}, nil
		})
		defer restore()

		Convey("When running Then no provider env key is injected and model stays default", func() {
			events, result, err := New().Run(context.Background(), agentruntime.RunRequest{
				Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), EnvJSON: "{}"},
				SessionID: 1,
				Cwd:       t.TempDir(),
				UserText:  "hello",
			})
			So(err, ShouldBeNil)
			for range events {
			}
			So(gotEnv, ShouldNotBeNil)
			for k := range gotEnv {
				So(strings.HasPrefix(k, "AGENTRE_PI_API_KEY_"), ShouldBeFalse)
			}
			So(result.Model, ShouldEqual, fallbackModelID)
		})
	})
}

func TestProviderRunConfig(t *testing.T) {
	Convey("Given a bound provider and a stubbed extension writer", t, func() {
		restore := SetProviderExtensionWriterForTest(func(string) (string, error) {
			return "/ext/agentre-provider-abc.mjs", nil
		})
		defer restore()

		Convey("When assembling the session config Then model is agentre-<key>/<model> and extension path is returned", func() {
			model, extPath, err := providerRunConfig(&llm_provider_entity.LLMProvider{
				ProviderKey: "provabc", Model: "deepseek-v3", Type: string(llm_provider_entity.TypeOpenAIChat),
			})
			So(err, ShouldBeNil)
			So(model, ShouldEqual, "agentre-provabc/deepseek-v3")
			So(extPath, ShouldEqual, "/ext/agentre-provider-abc.mjs")
		})

		Convey("When the provider is nil Then zero values are returned without error", func() {
			model, extPath, err := providerRunConfig(nil)
			So(err, ShouldBeNil)
			So(model, ShouldEqual, "")
			So(extPath, ShouldEqual, "")
		})

		Convey("When the provider model is empty Then no model or extension is produced", func() {
			model, extPath, err := providerRunConfig(&llm_provider_entity.LLMProvider{
				ProviderKey: "provabc", Type: string(llm_provider_entity.TypeOpenAIChat),
			})
			So(err, ShouldBeNil)
			So(model, ShouldEqual, "")
			So(extPath, ShouldEqual, "")
		})

		Convey("When the provider type is unsupported Then an error is returned instead of silently running unbound", func() {
			_, _, err := providerRunConfig(&llm_provider_entity.LLMProvider{
				ProviderKey: "provabc", Model: "deepseek-v3", Type: "deepseek",
			})
			So(err, ShouldNotBeNil)
		})
	})

	Convey("Given a failing extension writer", t, func() {
		restore := SetProviderExtensionWriterForTest(func(string) (string, error) {
			return "", errors.New("disk full")
		})
		defer restore()

		Convey("When materializing Then the error propagates", func() {
			_, _, err := providerRunConfig(&llm_provider_entity.LLMProvider{
				ProviderKey: "provabc", Model: "deepseek-v3", Type: string(llm_provider_entity.TypeOpenAIChat),
			})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "disk full")
		})
	})
}

type fakeSession struct {
	stream       stream
	sid          string
	gotImages    []pkgpiagent.Image
	gotPrompt    string
	streamCall   int
	streamErr    error
	closed       bool
	closeStarted chan struct{}
	allowClose   <-chan struct{}
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
	events []pkgpiagent.Event
	idx    int
	err    error
	sid    string
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
