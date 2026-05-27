package handlers_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"

	"agentre/internal/daemon/handlers"
	"agentre/internal/daemon/state"
	"agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

func TestHealthHandlers_Ping(t *testing.T) {
	Convey("Ping returns instanceUUID + serverTimeMs", t, func() {
		h := handlers.NewHealthHandlers("inst-uuid-fixed", state.NewDefault("inst-uuid-fixed"))
		res, err := h.Ping(context.Background())
		So(err, ShouldBeNil)
		So(res.InstanceUUID, ShouldEqual, "inst-uuid-fixed")
		So(res.ServerTimeMs, ShouldBeGreaterThan, int64(0))
	})
}

func TestHealth_Ping_IncludesProviders(t *testing.T) {
	Convey("Ping includes known providers sorted by key", t, func() {
		Convey("two providers — returned sorted by key", func() {
			st := state.NewDefault("test-instance")
			st.Mutate(func(s *state.State) {
				s.LLMProviders["zzz-key"] = state.LLMProviderMeta{Name: "Provider Z", Type: "openai"}
				s.LLMProviders["aaa-key"] = state.LLMProviderMeta{Name: "Provider A", Type: "anthropic"}
			})
			h := handlers.NewHealthHandlers("test-instance", st)
			res, err := h.Ping(context.Background())
			So(err, ShouldBeNil)
			So(res.Providers, ShouldHaveLength, 2)
			assert.Equal(t, wire.ProviderSummary{Key: "aaa-key", Name: "Provider A", Type: "anthropic"}, res.Providers[0])
			assert.Equal(t, wire.ProviderSummary{Key: "zzz-key", Name: "Provider Z", Type: "openai"}, res.Providers[1])
		})

		Convey("zero providers — Providers is nil/empty, no panic", func() {
			st := state.NewDefault("test-instance")
			h := handlers.NewHealthHandlers("test-instance", st)
			res, err := h.Ping(context.Background())
			So(err, ShouldBeNil)
			So(res.Providers, ShouldBeEmpty)
		})
	})
}
