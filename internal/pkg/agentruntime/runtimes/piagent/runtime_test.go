package piagent

import (
	"context"
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/capability"
	pkgpiagent "agentre/pkg/piagent"
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

type fakeSession struct {
	stream     stream
	sid        string
	gotImages  []pkgpiagent.Image
	gotPrompt  string
	streamCall int
	closed     bool
}

func (s *fakeSession) Close(context.Context) error { s.closed = true; return nil }
func (s *fakeSession) ID() string                  { return s.sid }
func (s *fakeSession) Stream(_ context.Context, prompt, _ string, images []pkgpiagent.Image) (stream, error) {
	s.streamCall++
	s.gotPrompt = prompt
	s.gotImages = images
	return s.stream, nil
}
func (s *fakeSession) Compact(context.Context) (stream, error)          { return s.stream, nil }
func (s *fakeSession) RewindTo(context.Context, string) (string, error) { return s.sid, nil }
func (s *fakeSession) ActiveStream() steerStream                        { return nil }
func (s *fakeSession) ActiveInterruptor() interruptable                 { return nil }

type emptyStream struct{}

func (*emptyStream) Next() bool              { return false }
func (*emptyStream) Event() pkgpiagent.Event { return pkgpiagent.Event{} }
func (*emptyStream) SessionID() string       { return "" }
func (*emptyStream) Err() error              { return nil }
