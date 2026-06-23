package handlers

import (
	"context"
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/turn"
)

type fakeErrorWriter struct{ text string }

func (f *fakeErrorWriter) WriteErrorText(_ any, s string) { f.text = s }

func TestErrorHandler(t *testing.T) {
	Convey("ErrorHandler patch ErrorText + emit error", t, func() {
		emit := &fakeEmit{}
		wr := &fakeErrorWriter{}
		mu := &fakeMsgUpdater{}
		tc := &turn.TurnContext{AssistantMsg: struct{}{}, MessageUpdater: mu, Stream: "s"}

		err := ErrorHandler{Writer: wr}.Apply(context.Background(),
			agentruntime.ErrorEvent{Err: errors.New("boom")},
			nil, emit, nil, tc)
		So(err, ShouldBeNil)
		So(wr.text, ShouldEqual, "boom")
		So(mu.calls, ShouldEqual, 1)

		p := emit.events[0].payload.(map[string]any)
		So(p["kind"], ShouldEqual, "error")
		So(p["error"], ShouldEqual, "boom")
	})
}
