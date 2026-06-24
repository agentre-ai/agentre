package hook_svc

import (
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
)

func TestComputeNextRun_Cron(t *testing.T) {
	s := &hookSvc{}
	// 每天 0 点；从 now=0(1970-01-01T00:00:00Z UTC) 起下一次应为 86400。
	h := &hook_entity.Hook{ScheduleExpr: "0 0 * * *", Timezone: "UTC"}
	if got := s.computeNextRun(h, 0); got != 86400 {
		t.Fatalf("cron next = %d, want 86400", got)
	}
}

func TestComputeNextRun_BadCronFallback(t *testing.T) {
	s := &hookSvc{}
	h := &hook_entity.Hook{ScheduleExpr: "garbage", Timezone: "UTC"}
	if got := s.computeNextRun(h, 1000); got <= 1000 {
		t.Fatalf("bad cron should fall back to a future time, got %d", got)
	}
}
