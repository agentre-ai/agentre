package piagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/canonical"
	pkgpi "github.com/agentre-ai/agentre/pkg/piagent"
)

func TestSelectSubagentCandidate_NameGateRunsBeforeClassificationAndTrackerCreation(t *testing.T) {
	classifyCalls := 0
	trackerCalls := 0
	selector := subagentSelector{
		classify: func([]byte) (subagentInvocation, bool) {
			classifyCalls++
			return subagentInvocation{
				Mode: subagentModeSingle,
				Runs: []invocationRun{{ID: "run-0", Agent: "worker", Task: "inspect"}},
			}, true
		},
		newTracker: func(outerID string, inv subagentInvocation) *subagentTracker {
			trackerCalls++
			return newSubagentTracker(outerID, inv)
		},
	}

	// Given a non-subagent name with otherwise valid input, when selection runs,
	// then neither the invocation classifier nor tracker/decoder boundary is called.
	tracker, spawn := selector.selectCandidate("delegate_task", "outer", []byte(`{"agent":"worker","task":"inspect"}`))
	assert.Nil(t, tracker)
	assert.Nil(t, spawn)
	assert.Zero(t, classifyCalls)
	assert.Zero(t, trackerCalls)

	// Given a mixed-case namespaced subagent name, when selection runs, then the
	// same candidate reaches both boundaries exactly once.
	tracker, spawn = selector.selectCandidate("Vendor__SubAgent", "outer", []byte(`{"agent":"worker","task":"inspect"}`))
	require.NotNil(t, tracker)
	require.NotNil(t, spawn)
	assert.Equal(t, 1, classifyCalls)
	assert.Equal(t, 1, trackerCalls)

	_, globallyRecognized := canonical.FromToolUse("Vendor__SubAgent", map[string]any{"agent": "worker", "task": "inspect"})
	assert.False(t, globallyRecognized, "fuzzy Pi recognition must not broaden global canonical matching")
}

func TestClassifySubagentInvocation_SingleAndFlatContracts(t *testing.T) {
	t.Run("official single accepts inactive arrays and blank optional strings", func(t *testing.T) {
		inv, ok := classifySubagentInvocation([]byte(`{
			"agent":" worker ","task":" inspect ","tasks":[],"chain":[],
			"model":"   ","thinking":" ","cwd":""
		}`))
		require.True(t, ok)
		assert.Equal(t, subagentModeSingle, inv.Mode)
		assert.Equal(t, envelopePending, inv.Envelope)
		require.Len(t, inv.Runs, 1)
		assert.Equal(t, "worker", inv.Runs[0].Agent)
		assert.Equal(t, "inspect", inv.Runs[0].Task)
		assert.Empty(t, inv.Runs[0].RequestedModel)
	})

	t.Run("profile flat locks immediately and keeps profile separate", func(t *testing.T) {
		inv, ok := classifySubagentInvocation([]byte(`{"task":"audit","profile":"read-only","model":"gpt-requested"}`))
		require.True(t, ok)
		assert.Equal(t, subagentModeSingle, inv.Mode)
		assert.Equal(t, envelopeFlat, inv.Envelope)
		require.Len(t, inv.Runs, 1)
		assert.Equal(t, "read-only", inv.Runs[0].Profile)
		assert.Empty(t, inv.Runs[0].Agent)
	})

	t.Run("official grouped modes classify without entering the single runtime slice", func(t *testing.T) {
		parallel, ok := classifySubagentInvocation([]byte(`{"tasks":[{"agent":"a","task":"one"}]}`))
		require.True(t, ok)
		assert.Equal(t, subagentModeParallel, parallel.Mode)
		assert.Equal(t, envelopeOfficial, parallel.Envelope)

		chain, ok := classifySubagentInvocation([]byte(`{"chain":[{"agent":"a","task":"one"},{"agent":"b","task":"two"}]}`))
		require.True(t, ok)
		assert.Equal(t, subagentModeChain, chain.Mode)
		require.Len(t, chain.Runs, 2)
	})

	for name, input := range map[string]string{
		"malformed json":        `{`,
		"non object":            `[]`,
		"control only":          `{"task_id":"x","action":"stop"}`,
		"known poison":          `{"agent":"worker","task":"inspect","agentScope":42}`,
		"ambiguous official":    `{"agent":"worker","task":"inspect","tasks":[{"agent":"other","task":"other"}]}`,
		"oversized parallel":    `{"tasks":[{"agent":"a","task":"1"},{"agent":"a","task":"2"},{"agent":"a","task":"3"},{"agent":"a","task":"4"},{"agent":"a","task":"5"},{"agent":"a","task":"6"},{"agent":"a","task":"7"},{"agent":"a","task":"8"},{"agent":"a","task":"9"}]}`,
		"task without identity": `{"task":"inspect"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := classifySubagentInvocation([]byte(input))
			assert.False(t, ok)
		})
	}
}

func TestSubagentTracker_PendingEnvelopeLocksOnceAndStreamsOfficialStepsExactlyOnce(t *testing.T) {
	inv, ok := classifySubagentInvocation([]byte(`{"agent":"worker","task":"inspect","tasks":[],"chain":[],"model":"requested"}`))
	require.True(t, ok)
	tracker := newSubagentTracker("outer", inv)

	ambiguous := []byte(`{"details":{"results":[],"messages":[]}}`)
	events, changed := tracker.consumeUpdate(ambiguous)
	assert.Empty(t, events)
	assert.False(t, changed)
	assert.Equal(t, envelopePending, tracker.envelope)

	mismatched := []byte(`{"details":{"mode":"parallel","results":[]}}`)
	events, changed = tracker.consumeUpdate(mismatched)
	assert.Empty(t, events)
	assert.False(t, changed)
	assert.Equal(t, envelopePending, tracker.envelope)

	poisonedDual := []byte(`{"details":{"results":"bad","messages":[]}}`)
	events, changed = tracker.consumeUpdate(poisonedDual)
	assert.Empty(t, events)
	assert.False(t, changed)
	assert.Equal(t, envelopePending, tracker.envelope)

	callSnapshot := []byte(`{"details":{"mode":"single","results":[{"messages":[
		{"role":"assistant","model":"observed-model","content":[{"type":"toolCall","id":"inner","name":"read","arguments":{"path":"a.go"}}],"stopReason":"toolUse"}
	]}]}}`)
	events, changed = tracker.consumeUpdate(callSnapshot)
	require.Len(t, events, 1)
	call := events[0].(agentruntime.ToolCall)
	assert.Equal(t, "outer", call.ParentToolCallID)
	assert.Equal(t, "run-0", call.SubagentRunID)
	assert.NotEqual(t, "inner", call.ID)
	assert.True(t, changed)
	assert.Equal(t, envelopeOfficial, tracker.envelope)
	assert.Equal(t, "observed-model", tracker.info().Runs[0].Model)
	assert.Equal(t, 1, tracker.info().Runs[0].ToolUses)
	assert.Equal(t, "running", tracker.info().Runs[0].Status)

	resultSnapshot := []byte(`{"details":{"results":[{"messages":[
		{"role":"assistant","model":"observed-model","content":[{"type":"toolCall","id":"inner","name":"read","arguments":{"path":"a.go"}}],"stopReason":"toolUse"},
		{"role":"toolResult","toolCallId":"inner","content":[{"type":"text","text":"ok"}],"isError":false}
	]}]}}`)
	events, _ = tracker.consumeUpdate(resultSnapshot)
	require.Len(t, events, 1)
	result := events[0].(agentruntime.ToolResult)
	assert.Equal(t, call.ID, result.ToolCallID)
	assert.Equal(t, "run-0", result.SubagentRunID)
	assert.Equal(t, "ok", result.Content)

	events, changed = tracker.consumeUpdate(resultSnapshot)
	assert.Empty(t, events)
	assert.False(t, changed)

	strayFlat := []byte(`{"details":{"messages":[{"role":"assistant","content":[{"type":"toolCall","id":"other","name":"bash","arguments":{}}]}]}}`)
	events, changed = tracker.consumeUpdate(strayFlat)
	assert.Empty(t, events)
	assert.False(t, changed)
	assert.Equal(t, envelopeOfficial, tracker.envelope)
}

func TestSubagentTracker_ResultBeforeCallAndFinalRecovery(t *testing.T) {
	inv, ok := classifySubagentInvocation([]byte(`{"task":"inspect","profile":"read-only"}`))
	require.True(t, ok)
	tracker := newSubagentTracker("outer", inv)

	orphan := []byte(`{"details":{"messages":[{"role":"toolResult","toolCallId":"late","content":[]} ]}}`)
	events, _ := tracker.consumeUpdate(orphan)
	assert.Empty(t, events)

	finalDetails := []byte(`{"messages":[
		{"role":"toolResult","toolCallId":"late","content":[]},
		{"role":"assistant","model":"actual","content":[{"type":"toolCall","id":"late","name":"bash","arguments":{"command":"pwd"}}],"stopReason":"toolUse"},
		{"role":"assistant","model":"actual","content":[{"type":"text","text":"finished"}],"stopReason":"stop"}
	],"exitCode":0}`)
	events, changed := tracker.consumeFinal(finalDetails, false, "outer text")
	require.Len(t, events, 2)
	call := events[0].(agentruntime.ToolCall)
	result := events[1].(agentruntime.ToolResult)
	assert.Equal(t, call.ID, result.ToolCallID)
	assert.Empty(t, result.Content)
	assert.True(t, changed)
	info := tracker.info()
	assert.Equal(t, "actual", info.Runs[0].Model)
	assert.Equal(t, "finished", info.Runs[0].Summary)
	assert.Equal(t, "completed", info.Runs[0].Status)
}

func TestSubagentTracker_FinalStatusFallbacksAndProjectConfirmationCancellation(t *testing.T) {
	t.Run("usable flat envelope without terminal evidence becomes unknown on outer success", func(t *testing.T) {
		inv, ok := classifySubagentInvocation([]byte(`{"task":"inspect","profile":"read-only"}`))
		require.True(t, ok)
		tracker := newSubagentTracker("outer", inv)
		_, _ = tracker.consumeFinal([]byte(`{"messages":[{"role":"assistant","content":[{"type":"text","text":"partial"}]}]}`), false, "outer")
		assert.Equal(t, "unknown", tracker.info().Status)
		assert.Equal(t, "unknown", tracker.info().Runs[0].Status)
	})

	t.Run("missing final envelope follows the sole outer result", func(t *testing.T) {
		inv, ok := classifySubagentInvocation([]byte(`{"agent":"worker","task":"inspect"}`))
		require.True(t, ok)
		tracker := newSubagentTracker("outer", inv)
		_, _ = tracker.consumeFinal(nil, false, "outer")
		assert.Equal(t, "completed", tracker.info().Runs[0].Status)

		tracker = newSubagentTracker("outer", inv)
		_, _ = tracker.consumeFinal(nil, true, "outer")
		assert.Equal(t, "failed", tracker.info().Runs[0].Status)
	})

	t.Run("official aborted fails while flat aborted cancels", func(t *testing.T) {
		official, ok := classifySubagentInvocation([]byte(`{"agent":"worker","task":"inspect"}`))
		require.True(t, ok)
		officialTracker := newSubagentTracker("outer-official", official)
		_, _ = officialTracker.consumeFinal([]byte(`{"mode":"single","results":[{"messages":[{"role":"assistant","content":[],"stopReason":"aborted"}]}]}`), false, "outer")
		assert.Equal(t, "failed", officialTracker.info().Runs[0].Status)

		flat, ok := classifySubagentInvocation([]byte(`{"task":"inspect","profile":"read-only"}`))
		require.True(t, ok)
		flatTracker := newSubagentTracker("outer-flat", flat)
		_, _ = flatTracker.consumeFinal([]byte(`{"messages":[{"role":"assistant","content":[],"stopReason":"aborted"}]}`), true, "outer")
		assert.Equal(t, "canceled", flatTracker.info().Runs[0].Status)
	})

	t.Run("declined project agents with empty official results cancel", func(t *testing.T) {
		inv, ok := classifySubagentInvocation([]byte(`{"agent":"project-worker","task":"inspect","agentScope":"both"}`))
		require.True(t, ok)
		tracker := newSubagentTracker("outer", inv)
		_, _ = tracker.consumeFinal([]byte(`{"mode":"single","results":[]}`), false, "Canceled by client")
		assert.Equal(t, "canceled", tracker.info().Status)
		assert.Equal(t, "canceled", tracker.info().Runs[0].Status)
	})
}

func TestDrainStream_SubagentOrderingAndUnsupportedFallback(t *testing.T) {
	t.Run("supported final snapshot recovers children before done and outer result", func(t *testing.T) {
		stream := &scriptedStream{events: []pkgpi.Event{
			{Kind: pkgpi.EventPreToolUse, Tool: pkgpi.ToolEvent{ID: "outer", Name: "Vendor__SubAgent", Input: []byte(`{"task":"inspect","profile":"read-only","model":"requested"}`)}},
			{Kind: pkgpi.EventPostToolUse, Tool: pkgpi.ToolEvent{ID: "outer", Name: "Vendor__SubAgent", Content: "outer result", Details: []byte(`{"messages":[
				{"role":"assistant","model":"actual","content":[{"type":"toolCall","id":"inner","name":"read","arguments":{"path":"a.go"}}],"stopReason":"toolUse"},
				{"role":"toolResult","toolCallId":"inner","content":[{"type":"text","text":"ok"}]},
				{"role":"assistant","model":"actual","content":[{"type":"text","text":"done"}],"stopReason":"stop"}
			],"exitCode":0}`)}},
		}}
		got := drainForTest(t, stream)
		require.Len(t, got, 8)
		_, outerCall := got[0].(agentruntime.ToolCall)
		_, started := got[1].(agentruntime.SubagentStarted)
		_, childCall := got[2].(agentruntime.ToolCall)
		_, childResult := got[3].(agentruntime.ToolResult)
		_, progress := got[4].(agentruntime.SubagentProgress)
		_, done := got[5].(agentruntime.SubagentDone)
		outerResult, result := got[6].(agentruntime.ToolResult)
		_, turnDone := got[7].(agentruntime.Done)
		assert.True(t, outerCall && started && childCall && childResult && progress && done && result && turnDone)
		assert.Equal(t, "outer result", outerResult.Content)
	})

	t.Run("nonmatching name stays ordinary even with valid details", func(t *testing.T) {
		stream := &scriptedStream{events: []pkgpi.Event{
			{Kind: pkgpi.EventPreToolUse, Tool: pkgpi.ToolEvent{ID: "outer", Name: "delegate_task", Input: []byte(`{"task":"inspect","profile":"read-only"}`)}},
			{Kind: pkgpi.EventToolUseUpdate, Tool: pkgpi.ToolEvent{ID: "outer", PartialResult: []byte(`{"details":{"messages":[]}}`)}},
			{Kind: pkgpi.EventPostToolUse, Tool: pkgpi.ToolEvent{ID: "outer", Content: "raw", Details: []byte(`{"messages":[]}`), IsError: true}},
		}}
		got := drainForTest(t, stream)
		require.Len(t, got, 3)
		call := got[0].(agentruntime.ToolCall)
		assert.Nil(t, call.Canonical)
		result := got[1].(agentruntime.ToolResult)
		assert.Equal(t, "raw", result.Content)
		assert.True(t, result.IsError)
		assert.IsType(t, agentruntime.Done{}, got[2])
	})
}

func drainForTest(t *testing.T, stream *scriptedStream) []agentruntime.Event {
	t.Helper()
	out := make(chan agentruntime.Event, 32)
	result := &agentruntime.RunResult{}
	drainStream(context.Background(), agentruntime.RunRequest{}, "", stream, out, result, nil)
	close(out)
	var got []agentruntime.Event
	for event := range out {
		got = append(got, event)
	}
	return got
}
