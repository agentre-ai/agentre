package handlers_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/daemon/state"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// stubDBStat 是一份固定的库落盘事实,替代真库让本包的单测不碰磁盘。
type stubDBStat struct{ stat handlers.DBStat }

func (s stubDBStat) DBStat() handlers.DBStat { return s.stat }

// TestHealth_Ping_ReportsDatabasePathAndSize 覆盖规格「磁盘增长」那一句在 RPC 侧的一半:
// 探活应答里要带上 daemon 库文件的路径与体量,用户(与桌面端)据此判断何时该清理。
// 没有注入库统计口时不编造零值 —— 那会在界面上显示成「库是空的」。
func TestHealth_Ping_ReportsDatabasePathAndSize(t *testing.T) {
	Convey("Ping 交出库文件路径与体量", t, func() {
		h := handlers.NewHealthHandlers("inst", state.NewDefault("inst"),
			stubDBStat{stat: handlers.DBStat{Path: "/var/agentred/agentred.db", SizeBytes: 4096}})
		res, err := h.Ping(context.Background())
		So(err, ShouldBeNil)
		So(res.DBPath, ShouldEqual, "/var/agentred/agentred.db")
		So(res.DBSizeBytes, ShouldEqual, int64(4096))

		Convey("没有库统计口时两个字段留空", func() {
			bare := handlers.NewHealthHandlers("inst", state.NewDefault("inst"), nil)
			bareRes, bareErr := bare.Ping(context.Background())
			So(bareErr, ShouldBeNil)
			So(bareRes.DBPath, ShouldBeEmpty)
			So(bareRes.DBSizeBytes, ShouldEqual, int64(0))
		})
	})
}

func TestHealthHandlers_Ping(t *testing.T) {
	Convey("Ping returns instanceUUID + serverTimeMs", t, func() {
		h := handlers.NewHealthHandlers("inst-uuid-fixed", state.NewDefault("inst-uuid-fixed"), nil)
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
			h := handlers.NewHealthHandlers("test-instance", st, nil)
			res, err := h.Ping(context.Background())
			So(err, ShouldBeNil)
			So(res.Providers, ShouldHaveLength, 2)
			assert.Equal(t, wire.ProviderSummary{Key: "aaa-key", Name: "Provider A", Type: "anthropic"}, res.Providers[0])
			assert.Equal(t, wire.ProviderSummary{Key: "zzz-key", Name: "Provider Z", Type: "openai"}, res.Providers[1])
		})

		Convey("zero providers — Providers is nil/empty, no panic", func() {
			st := state.NewDefault("test-instance")
			h := handlers.NewHealthHandlers("test-instance", st, nil)
			res, err := h.Ping(context.Background())
			So(err, ShouldBeNil)
			So(res.Providers, ShouldBeEmpty)
		})
	})
}
