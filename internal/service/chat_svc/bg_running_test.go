package chat_svc

import (
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"

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
