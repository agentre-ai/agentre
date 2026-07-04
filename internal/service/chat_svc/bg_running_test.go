package chat_svc

import (
	"context"
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"

	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
)

func TestBgRunningSet_AddRemoveClearActive(t *testing.T) {
	s := &chatSvc{}
	if s.bgRunningActive(7) {
		t.Fatal("empty session should be inactive")
	}
	if !s.addBgRunning(7, "tu-1", "tu-2") {
		t.Fatal("first add should report changed")
	}
	if !s.bgRunningActive(7) {
		t.Fatal("session should be active after add")
	}
	if s.addBgRunning(7, "tu-1") {
		t.Fatal("re-add existing id should be no-op (idempotent)")
	}
	if !s.removeBgRunning(7, "tu-1") {
		t.Fatal("remove existing should report changed")
	}
	if s.removeBgRunning(7, "tu-1") {
		t.Fatal("remove missing should be no-op")
	}
	if !s.bgRunningActive(7) {
		t.Fatal("still active: tu-2 remains")
	}
	if !s.clearBgRunning(7) {
		t.Fatal("clear non-empty should report changed")
	}
	if s.bgRunningActive(7) {
		t.Fatal("inactive after clear")
	}
	if s.clearBgRunning(7) {
		t.Fatal("clear empty should be no-op")
	}
}

func TestEmitBgRunningStatus_CarriesFlag(t *testing.T) {
	rec := &captureEmitter{}
	s := &chatSvc{emitter: rec}
	s.addBgRunning(9, "tu-x")
	sess := &chat_entity.Session{ID: 9, AgentStatus: "idle"}
	s.emitBgRunningStatus(context.Background(), sess, "stream-9")

	if len(rec.events) != 1 {
		t.Fatalf("want 1 event, got %d", len(rec.events))
	}
	ev := rec.events[0]
	if ev.Kind != StreamSessionStatus || ev.SessionStatus == nil {
		t.Fatalf("want session_status event, got %+v", ev)
	}
	if !ev.SessionStatus.BgRunning {
		t.Fatal("want BgRunning=true")
	}
	if ev.SessionStatus.AgentStatus != "idle" {
		t.Fatalf("want agentStatus idle, got %q", ev.SessionStatus.AgentStatus)
	}
}

func TestSessionLiteFromEntity_CarriesBgRunning(t *testing.T) {
	s := &chatSvc{}
	s.addBgRunning(42, "tu-bg")
	sess := &chat_entity.Session{ID: 42, AgentStatus: "idle"}
	lite := s.sessionLiteFromEntity(sess)
	if !lite.BgRunning {
		t.Fatal("want ChatSessionLite.BgRunning=true after addBgRunning")
	}
}

func TestRunningBgSubagentIDs(t *testing.T) {
	blks := []cagoblocks.ContentBlock{
		// 后台 subagent: Agent tool_use run_in_background=true + running subagent_state
		&cagoblocks.ToolUseBlock{ID: "bg-1", Input: map[string]any{"run_in_background": true}},
		&blocks.SubagentStateBlock{ParentToolCallID: "bg-1", Kind: "local_agent", Status: "running"},
		// 前台 subagent: 无 run_in_background → 不纳入
		&cagoblocks.ToolUseBlock{ID: "fg-1", Input: map[string]any{}},
		&blocks.SubagentStateBlock{ParentToolCallID: "fg-1", Kind: "local_agent", Status: "running"},
		// 后台但已完成 → status 非 running → 不纳入
		&cagoblocks.ToolUseBlock{ID: "done-1", Input: map[string]any{"run_in_background": true}},
		&blocks.SubagentStateBlock{ParentToolCallID: "done-1", Kind: "local_agent", Status: "completed"},
	}
	got := runningBgSubagentIDs(blks)
	if len(got) != 1 || got[0] != "bg-1" {
		t.Fatalf("want [bg-1], got %v", got)
	}
}
