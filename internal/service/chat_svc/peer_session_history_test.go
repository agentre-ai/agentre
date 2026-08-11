package chat_svc

import (
	"context"
	"encoding/json"
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// Given persisted desktop transcript blocks, when a peer snapshot is made,
// then its canonical events retain readable semantics and unknown blocks remain
// opaque raw objects rather than being discarded or decoded as sealed events.
func TestSynthesizePeerHistory_GivenPersistedBlocks_ThenMapsReadableEventsAndPreservesUnknownRawBlock(t *testing.T) {
	messages := []*chat_entity.Message{
		{SessionID: 41, Role: "user", Seq: 1, BlocksJSON: `[{"type":"text","data":{"text":"ship it"}}]`},
		{SessionID: 41, Role: "assistant", Seq: 2, BlocksJSON: `[{"type":"thinking","data":{"text":"checking"}},{"type":"tool_use","data":{"id":"tool-1","name":"Read","input":{"path":"README.md"}}},{"type":"tool_result","data":{"tool_use_id":"tool-1","content":[{"type":"text","data":{"text":"ok"}}]}},{"type":"future_block","data":{"nested":{"keep":true}}}]`, ErrorText: "provider stopped"},
	}

	events, err := synthesizePeerHistory(41, messages)
	require.NoError(t, err)
	require.Len(t, events, 7)

	assertPeerEventKind(t, events[0], agentruntime.EventUserMessage)
	assertPeerEventKind(t, events[1], agentruntime.EventThinkingDelta)
	assertPeerEventKind(t, events[2], agentruntime.EventToolUseStart)
	assertPeerEventKind(t, events[3], agentruntime.EventToolResult)

	var unknown map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(events[4].Event, &unknown))
	assert.Equal(t, json.RawMessage(`"unrecognized_block"`), unknown["kind"])
	var raw cagoblocks.StoredBlock
	require.NoError(t, json.Unmarshal(unknown["block"], &raw))
	assert.Equal(t, cagoblocks.StoredBlock{Type: "future_block", Data: json.RawMessage(`{"nested":{"keep":true}}`)}, raw,
		"the fallback must retain the original StoredBlock envelope exactly")
	assertPeerEventKind(t, events[5], agentruntime.EventError)
	assertPeerEventKind(t, events[6], agentruntime.EventDone)
}

// Given an attached peer and a frozen history, when live canonical events
// arrive before the peer has pulled the snapshot high-water mark, then pull
// emits deterministic 1..H history first and releases the buffered live frame
// only after the cursor reaches H.
// Given persisted final control-card state, when the desktop synthesizes a
// history, then it reconstructs both the card creation and its final update so
// the existing reducer can reach the stored readable state.
func TestSynthesizePeerHistory_GivenFinalControlAndSnapshotBlocks_ThenEmitsReducerCompleteEvents(t *testing.T) {
	messages := []*chat_entity.Message{{
		SessionID: 41, Role: "assistant", Seq: 1, PromptTokens: 10, TotalInputTokens: 10,
		BlocksJSON: `[` +
			`{"type":"user_ask","data":{"request_id":"ask-1","tool_call_id":"tool-1","questions":[{"question":"continue?","options":[]}],"answered":true,"answers":[{"questionIndex":0,"labels":["yes"]}]}},` +
			`{"type":"tool_permission","data":{"request_id":"permission-1","tool_call_id":"tool-2","tool_name":"Bash","tool_input":{"command":"pwd"},"resolved":true,"allowed":true}},` +
			`{"type":"permission_mode_change","data":{"to":"plan"}},` +
			`{"type":"subagent_state","data":{"parent_tool_call_id":"agent-1","status":"completed","total_tokens":7,"model":"claude"}},` +
			`{"type":"plan","data":{"steps":[{"step":"inspect","status":"completed"}],"text":"# Plan"}}` +
			`]`,
	}}

	events, err := synthesizePeerHistory(41, messages)
	require.NoError(t, err)
	kinds := make([]agentruntime.EventKind, 0, len(events))
	for _, event := range events {
		var head struct {
			Kind agentruntime.EventKind `json:"kind"`
		}
		require.NoError(t, json.Unmarshal(event.Event, &head))
		kinds = append(kinds, head.Kind)
	}
	assert.Equal(t, []agentruntime.EventKind{
		agentruntime.EventAskUserQuestion,
		agentruntime.EventAskUserQuestionAnswered,
		agentruntime.EventToolPermissionRequest,
		agentruntime.EventToolPermissionResolved,
		agentruntime.EventPermissionModeChanged,
		agentruntime.EventSubagentDone,
		agentruntime.EventSubagentModel,
		agentruntime.EventPlanUpdated,
		agentruntime.EventUsage,
		agentruntime.EventDone,
	}, kinds)
}

// Given an attached peer and a frozen history, when live canonical events
// arrive before the peer has pulled the snapshot high-water mark, then pull
// emits deterministic 1..H history first and releases the buffered live frame
// only after the cursor reaches H.
func TestPeerSessionPull_GivenSnapshotAndEarlyLiveEvent_ThenKeepsOneOrderedSeqUniverse(t *testing.T) {
	deps := setupPeerSessionTest(t)
	ctx := context.Background()
	messages := []*chat_entity.Message{
		{SessionID: 41, Role: "user", Seq: 1, BlocksJSON: `[{"type":"text","data":{"text":"hello"}}]`},
		{SessionID: 41, Role: "assistant", Seq: 2, BlocksJSON: `[{"type":"text","data":{"text":"world"}}]`},
	}
	deps.session.EXPECT().Find(ctx, int64(41)).Return(&chat_entity.Session{ID: 41, AgentID: 7, AgentStatus: "idle"}, nil)
	deps.agent.EXPECT().Find(ctx, int64(7)).Return(agentForPeerSession(), nil)
	deps.backend.EXPECT().Find(ctx, int64(11)).Return(nil, nil)
	deps.message.EXPECT().List(ctx, int64(41)).Return(messages, nil)

	subscriber := newRecordingPeerSubscriber()
	attached, err := deps.svc.AttachPeerSession(ctx, wire.SessionAttachParams{SessionID: 41}, subscriber)
	require.NoError(t, err)
	assert.Equal(t, int64(3), attached.LatestSeq, "history must be frozen as user_message, text, done")

	deps.svc.publishPeerEvent(41, agentruntime.TextDelta{Text: "live"})
	assert.Empty(t, subscriber.notifications(), "live output must wait behind the frozen snapshot")

	first, err := deps.svc.PullPeerSession(ctx, wire.SessionPullParams{SessionID: 41, Cursor: 0, Limit: 2}, subscriber)
	require.NoError(t, err)
	assert.Equal(t, int64(1), first.OldestSeq)
	assert.Equal(t, int64(2), first.Cursor)
	assert.True(t, first.HasMore)
	assertPeerNotificationSeqs(t, first.Notifications, 1, 2)
	assert.Empty(t, subscriber.notifications(), "cursor below H must retain live buffering")

	second, err := deps.svc.PullPeerSession(ctx, wire.SessionPullParams{SessionID: 41, Cursor: first.Cursor, Limit: 2}, subscriber)
	require.NoError(t, err)
	assert.Equal(t, int64(3), second.Cursor)
	assert.False(t, second.HasMore)
	assertPeerNotificationSeqs(t, second.Notifications, 3)
	live := subscriber.notifications()
	require.Len(t, live, 1)
	assert.Equal(t, wire.NotifyEvent, live[0].method)
	assert.Equal(t, int64(4), eventFrameSeq(t, live[0].params))
}

// Given a reconnecting peer while this desktop is still serving a session,
// when canonical live output was published after the initial snapshot, then a
// fresh attach sees the same monotonic sequence universe instead of a gap.
func TestAttachPeerSession_GivenReconnectAfterLiveEvent_ThenRetainsLiveSeqForDedup(t *testing.T) {
	deps := setupPeerSessionTest(t)
	ctx := context.Background()
	messages := []*chat_entity.Message{{SessionID: 41, Role: "assistant", Seq: 1, BlocksJSON: `[{"type":"text","data":{"text":"history"}}]`}}
	deps.session.EXPECT().Find(ctx, int64(41)).Return(&chat_entity.Session{ID: 41, AgentID: 7, AgentStatus: "idle"}, nil).Times(2)
	deps.agent.EXPECT().Find(ctx, int64(7)).Return(agentForPeerSession(), nil).Times(2)
	deps.backend.EXPECT().Find(ctx, int64(11)).Return(nil, nil).Times(2)
	deps.message.EXPECT().List(ctx, int64(41)).Return(messages, nil)

	first := newRecordingPeerSubscriber()
	attached, err := deps.svc.AttachPeerSession(ctx, wire.SessionAttachParams{SessionID: 41}, first)
	require.NoError(t, err)
	assert.Equal(t, int64(2), attached.LatestSeq)
	deps.svc.publishPeerEvent(41, agentruntime.TextDelta{Text: "live"})

	second := newRecordingPeerSubscriber()
	reconnected, err := deps.svc.AttachPeerSession(ctx, wire.SessionAttachParams{SessionID: 41}, second)
	require.NoError(t, err)
	assert.Equal(t, int64(3), reconnected.LatestSeq)
	page, err := deps.svc.PullPeerSession(ctx, wire.SessionPullParams{SessionID: 41, Limit: 3}, second)
	require.NoError(t, err)
	assertPeerNotificationSeqs(t, page.Notifications, 1, 2, 3)
}

// Given a desktop session with no stored messages, when an attached peer pulls
// its transcript, then the non-reclaiming desktop contract reports OldestSeq=0.
func TestPeerSessionPull_GivenEmptyHistory_ThenReportsZeroOldestSeq(t *testing.T) {
	deps := setupPeerSessionTest(t)
	ctx := context.Background()
	deps.session.EXPECT().Find(ctx, int64(41)).Return(&chat_entity.Session{ID: 41, AgentID: 7, AgentStatus: "idle"}, nil)
	deps.agent.EXPECT().Find(ctx, int64(7)).Return(agentForPeerSession(), nil)
	deps.backend.EXPECT().Find(ctx, int64(11)).Return(nil, nil)
	deps.message.EXPECT().List(ctx, int64(41)).Return(nil, nil)

	subscriber := newRecordingPeerSubscriber()
	_, err := deps.svc.AttachPeerSession(ctx, wire.SessionAttachParams{SessionID: 41}, subscriber)
	require.NoError(t, err)
	page, err := deps.svc.PullPeerSession(ctx, wire.SessionPullParams{SessionID: 41}, subscriber)
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.OldestSeq)
	assert.Empty(t, page.Notifications)
}

type peerNotification struct {
	method string
	params any
}

type peerRecordingSubscriber struct {
	done    chan struct{}
	records []peerNotification
}

func newRecordingPeerSubscriber() *peerRecordingSubscriber {
	return &peerRecordingSubscriber{done: make(chan struct{})}
}

func (s *peerRecordingSubscriber) Notify(method string, params any) error {
	s.records = append(s.records, peerNotification{method: method, params: params})
	return nil
}

func (s *peerRecordingSubscriber) Done() <-chan struct{} { return s.done }
func (s *peerRecordingSubscriber) notifications() []peerNotification {
	return append([]peerNotification(nil), s.records...)
}

func agentForPeerSession() *agent_entity.Agent {
	return &agent_entity.Agent{ID: 7, AgentBackendID: 11}
}

func assertPeerEventKind(t *testing.T, frame wire.EventFrame, kind agentruntime.EventKind) {
	t.Helper()
	var head struct {
		Kind agentruntime.EventKind `json:"kind"`
	}
	require.NoError(t, json.Unmarshal(frame.Event, &head))
	assert.Equal(t, kind, head.Kind)
}

func assertPeerNotificationSeqs(t *testing.T, notifications []wire.JournaledNotification, want ...int64) {
	t.Helper()
	require.Len(t, notifications, len(want))
	for i, seq := range want {
		assert.Equal(t, seq, notifications[i].Seq)
		assert.Equal(t, wire.NotifyEvent, notifications[i].Method)
	}
}

func eventFrameSeq(t *testing.T, params any) int64 {
	t.Helper()
	frame, ok := params.(wire.EventFrame)
	require.True(t, ok)
	return frame.Seq
}
