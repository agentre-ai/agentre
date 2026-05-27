package handlers

import (
	"context"
	"testing"

	"github.com/cago-frame/agents/provider"
	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/pkg/agentruntime"
	"agentre/internal/service/chat_svc/turn"
)

type fakeMsgUpdater struct{ calls int }

func (f *fakeMsgUpdater) Update(_ context.Context, _ any) error { f.calls++; return nil }

type fakeUsageWriter struct {
	written *agentruntime.UsageUpdate
	msgID   int64
}

func (f *fakeUsageWriter) WriteUsage(_ any, u *agentruntime.UsageUpdate) { f.written = u }
func (f *fakeUsageWriter) MessageID(_ any) int64                         { return f.msgID }

func TestUsageUpdateHandler(t *testing.T) {
	Convey("UsageUpdate 调 Writer + Updater + emit usage", t, func() {
		acc := turn.New()
		emit := &fakeEmit{}
		wr := &fakeUsageWriter{}
		mu := &fakeMsgUpdater{}
		tc := &turn.TurnContext{AssistantMsg: struct{}{}, MessageUpdater: mu, Stream: "s"}

		err := UsageUpdateHandler{Writer: wr}.Apply(context.Background(),
			agentruntime.UsageUpdate{
				Usage:            &provider.Usage{PromptTokens: 100, CachedTokens: 30},
				TotalInputTokens: 130,
			},
			acc, emit, nil, tc)
		So(err, ShouldBeNil)
		So(wr.written, ShouldNotBeNil)
		So(wr.written.TotalInputTokens, ShouldEqual, 130)
		So(mu.calls, ShouldEqual, 1)

		p := emit.events[0].payload.(map[string]any)
		So(p["kind"], ShouldEqual, "usage")
		usage := p["usage"].(map[string]any)
		So(usage["promptTokens"], ShouldEqual, 100)
		So(usage["totalInputTokens"], ShouldEqual, 130)
	})
}

func TestUsageUpdateHandler_NilUsageNoOp(t *testing.T) {
	Convey("Usage=nil → no-op", t, func() {
		acc := turn.New()
		emit := &fakeEmit{}
		err := UsageUpdateHandler{}.Apply(context.Background(),
			agentruntime.UsageUpdate{Usage: nil}, acc, emit, nil, nil)
		So(err, ShouldBeNil)
		So(emit.events, ShouldHaveLength, 0)
	})
}
