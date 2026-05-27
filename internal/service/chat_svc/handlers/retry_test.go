package handlers

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/pkg/agentruntime"
	"agentre/internal/service/chat_svc/turn"
)

func TestRetryHandler(t *testing.T) {
	Convey("RetryHandler 只 emit, 不动 acc", t, func() {
		acc := turn.New()
		emit := &fakeEmit{}
		err := RetryHandler{}.Apply(context.Background(),
			agentruntime.Retry{Message: "rate limit", Attempt: 2, Max: 5},
			acc, emit, nil, nil)
		So(err, ShouldBeNil)
		So(acc.Empty(), ShouldBeTrue)

		p := emit.events[0].payload.(map[string]any)
		So(p["kind"], ShouldEqual, "retry")
		So(p["retryAttempt"], ShouldEqual, 2)
		So(p["retryMessage"], ShouldEqual, "rate limit")
	})
}
