package handlers

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/turn"
)

type fakeCWWriter struct{ tokens int }

func (f *fakeCWWriter) WriteContextWindow(_ any, t int) { f.tokens = t }

func TestContextWindowUpdatedHandler(t *testing.T) {
	Convey("ContextWindowUpdated 写 session.ContextWindow + emit patch", t, func() {
		emit := &fakeEmit{}
		wr := &fakeCWWriter{}
		su := &fakeSessionUpdater{}
		tc := &turn.TurnContext{Session: struct{}{}, SessionUpdater: su, Stream: "s"}

		err := ContextWindowUpdatedHandler{Writer: wr}.Apply(context.Background(),
			agentruntime.ContextWindowUpdated{Tokens: 200000},
			nil, emit, nil, tc)
		So(err, ShouldBeNil)
		So(wr.tokens, ShouldEqual, 200000)
		So(su.calls, ShouldEqual, 1)

		p := emit.events[0].payload.(map[string]any)
		ss := p["sessionStatus"].(map[string]any)
		So(ss["contextWindow"], ShouldEqual, 200000)
	})
}

func TestContextWindowUpdatedHandler_ZeroTokensNoOp(t *testing.T) {
	Convey("Tokens=0 → no-op", t, func() {
		emit := &fakeEmit{}
		err := ContextWindowUpdatedHandler{}.Apply(context.Background(),
			agentruntime.ContextWindowUpdated{Tokens: 0}, nil, emit, nil, nil)
		So(err, ShouldBeNil)
		So(emit.events, ShouldHaveLength, 0)
	})
}
