package codex

import (
	"context"
	"testing"

	"github.com/cago-frame/agents/provider"
	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/capability"
	pkgcodex "agentre/pkg/codex"
)

// TestCodexCapabilities 钉死 codex runtime 的能力矩阵 + permission mode 元数据。
// 与 claudecode 的关键差异:CapCancelSteer/CapDrainSteer/CapToolPermission=false;
// CapReportContextWindow=true;PermissionModeMeta 仅 default/plan,SwitchableDuringTurn=false。
func TestCodexCapabilities(t *testing.T) {
	Convey("codex Capabilities 矩阵", t, func() {
		r := New()
		caps := r.Capabilities()
		So(caps.Has(capability.CapSteer), ShouldBeTrue)
		So(caps.Has(capability.CapCancelSteer), ShouldBeFalse) // codex fire-and-forget
		So(caps.Has(capability.CapDrainSteer), ShouldBeFalse)  // 无 hook 队列
		So(caps.Has(capability.CapAbort), ShouldBeTrue)
		So(caps.Has(capability.CapSetPermission), ShouldBeTrue)
		So(caps.Has(capability.CapAnswerUserAsk), ShouldBeTrue)
		So(caps.Has(capability.CapToolPermission), ShouldBeFalse) // 无 can_use_tool
		So(caps.Has(capability.CapForkSession), ShouldBeTrue)
		So(caps.Has(capability.CapReportContextWindow), ShouldBeTrue)
		So(caps.Has(capability.CapCompact), ShouldBeTrue)
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

func TestRun_DefaultModelWhenProviderMissing(t *testing.T) {
	Convey("codex runtime 在 CLI 自身登录态下回填默认模型", t, func() {
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
		So(result, ShouldNotBeNil)
		for range events {
		}

		So(result.Model, ShouldEqual, "gpt-5.5")
		So(result.ProviderSessionID, ShouldEqual, "thread-default")
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

type fakeRuntimeSession struct {
	stream cxStream
	sid    string
}

func (s *fakeRuntimeSession) Close(context.Context) error { return nil }
func (s *fakeRuntimeSession) ID() string                  { return s.sid }
func (s *fakeRuntimeSession) Stream(context.Context, string, string) (cxStream, error) {
	return s.stream, nil
}
func (s *fakeRuntimeSession) Compact(context.Context) (cxStream, error)        { return s.stream, nil }
func (s *fakeRuntimeSession) RewindTo(context.Context, string) (string, error) { return s.sid, nil }
func (s *fakeRuntimeSession) ActiveStream() cxSteerStream                      { return nil }
func (s *fakeRuntimeSession) ActiveInterruptor() cxInterruptable               { return nil }

type emptyRuntimeStream struct{}

func (*emptyRuntimeStream) Next() bool            { return false }
func (*emptyRuntimeStream) Event() pkgcodex.Event { return pkgcodex.Event{} }
func (*emptyRuntimeStream) SessionID() string     { return "" }

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
