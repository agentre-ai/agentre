package peer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/client"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/peer"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// fakePeerServer 起一台内存 WebSocket 对端（会话族 handler 由 register 按每条连接
// 挂上），返回可被 client.Dial 直连的 ws:// URL。传输与生产一致（同一套
// rpc.Subprotocol / websocketFrameConn），只省去中继与账号握手——生产里握手由
// server_svc.DialDesktopRelay 完成，而本测试要验的是 Outbound 的会话族调用与
// runtime.event 订阅，不是中继本身。
func fakePeerServer(t *testing.T, register func(*rpc.Registry)) string {
	t.Helper()
	upgrader := websocket.Upgrader{Subprotocols: []string{rpc.Subprotocol}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		registry := rpc.NewRegistry()
		register(registry)
		conn := rpc.NewConn(rpc.NewWebSocketFrameConn(ws), registry)
		go conn.Serve(context.Background())
	}))
	t.Cleanup(srv.Close)
	// gorilla 客户端只认 ws/wss scheme，把 httptest 的 http:// 换成 ws://。
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dialOutbound(t *testing.T, url string) *peer.Outbound {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, client.Options{URL: url})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return peer.NewOutbound(c, "sha256:peer-desktop")
}

// Given a relay-reachable named desktop is the resolved first target, when this
// desktop dispatches a new conversation to it, then runtime.run carries the
// fresh-session intent plus the account Agent and this machine's project cwd,
// and the peer's real session id comes back for the follow-up attach.
func TestOutbound_GivenFreshSessionRun_WhenDispatchNewConversation_ThenReturnsRemoteSessionID(t *testing.T) {
	url := fakePeerServer(t, func(reg *rpc.Registry) {
		reg.Register(wire.MethodRun, func(_ context.Context, raw json.RawMessage) (any, error) {
			var p wire.RunParams
			require.NoError(t, json.Unmarshal(raw, &p))
			assert.True(t, p.FreshSession, "dispatch must demand a genuinely new session, never resume a stale one")
			assert.Equal(t, "01HXAGENTIDENTITY0000000000", p.AgentSyncID)
			assert.Equal(t, "/Users/wyz/agentre", p.Cwd)
			assert.Equal(t, "帮我看看这个项目", p.UserText)
			assert.Equal(t, "sha256:local-desktop", p.SourceDevice)
			assert.Equal(t, "MacBook Pro", p.SourceDeviceName)
			return wire.RunAck{SessionID: 42}, nil
		})
	})
	ob := dialOutbound(t, url)

	ack, err := ob.RunFresh(context.Background(), wire.RunParams{
		SessionID:        90001,
		AgentSyncID:      "01HXAGENTIDENTITY0000000000",
		Cwd:              "/Users/wyz/agentre",
		Title:            "帮我看看这个项目",
		UserText:         "帮我看看这个项目",
		SourceDevice:     "sha256:local-desktop",
		SourceDeviceName: "MacBook Pro",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(42), ack.SessionID, "the executing machine's real session id must reach the dispatcher")
}

// Given the target desktop owns sessions, when this desktop lists them, then
// every R5 field (title, lifecycle, waiting, updated-at, agent identity) comes
// back without a degraded fallback.
func TestOutbound_GivenRemoteDesktopSessions_WhenList_ThenReturnFullSummaries(t *testing.T) {
	url := fakePeerServer(t, func(reg *rpc.Registry) {
		reg.Register(wire.MethodSessionList, func(_ context.Context, _ json.RawMessage) (any, error) {
			return wire.SessionListResult{
				Sessions: []wire.SessionSummary{{
					SessionID: 7, PeerFingerprint: "sha256:peer-desktop", AgentID: 3,
					Title: "Ship the release", AgentSyncID: "01HXAGENTIDENTITY0000000000",
					BackendType: "claudecode", LifecycleState: wire.SessionLifecycleRunning,
					WaitingForInput: true, LatestSeq: 12, UpdatedAt: 1710000000000,
				}},
				SupportsSessionMetadata: true,
			}, nil
		})
	})
	ob := dialOutbound(t, url)

	result, err := ob.ListSessions(context.Background())
	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	summary := result.Sessions[0]
	assert.Equal(t, int64(7), summary.SessionID)
	assert.Equal(t, "sha256:peer-desktop", summary.PeerFingerprint)
	assert.Equal(t, "Ship the release", summary.Title)
	assert.Equal(t, wire.SessionLifecycleRunning, summary.LifecycleState)
	assert.True(t, summary.WaitingForInput)
	assert.Equal(t, int64(12), summary.LatestSeq)
	assert.Equal(t, int64(1710000000000), summary.UpdatedAt)
	assert.True(t, result.SupportsSessionMetadata, "the target is a desktop, so no round-A degraded fallback")
}

// Given this desktop attaches a remote session, when the executing machine then
// pushes a canonical event, then the subscriber registered via HandleEvent
// receives the live frame on the same connection.
func TestOutbound_GivenAttachedRemoteSession_WhenPeerEmitsEvent_ThenSubscriberReceivesLiveFrame(t *testing.T) {
	events := make(chan wire.EventFrame, 4)
	url := fakePeerServer(t, func(reg *rpc.Registry) {
		reg.Register(wire.MethodSessionAttach, func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p wire.SessionAttachParams
			require.NoError(t, json.Unmarshal(raw, &p))
			assert.Equal(t, int64(7), p.SessionID)
			conn := rpc.ConnFromContext(ctx)
			require.NotNil(t, conn)
			require.NoError(t, conn.Notify(wire.NotifyEvent, wire.EventFrame{
				SessionID: 7, Seq: 13, Event: mustJSON(t, map[string]any{"type": "message", "role": "assistant", "content": "hi"}),
			}))
			return wire.SessionAttachResult{SessionID: 7, LifecycleState: wire.SessionLifecycleRunning, LatestSeq: 12}, nil
		})
	})
	ob := dialOutbound(t, url)
	ob.HandleEvent(func(f wire.EventFrame) error {
		events <- f
		return nil
	})

	att, err := ob.Attach(context.Background(), wire.SessionAttachParams{SessionID: 7})
	require.NoError(t, err)
	assert.Equal(t, int64(12), att.LatestSeq, "attach returns the high-water cursor for the follow-up pull")

	select {
	case frame := <-events:
		assert.Equal(t, int64(7), frame.SessionID)
		assert.Equal(t, int64(13), frame.Seq)
	case <-time.After(2 * time.Second):
		t.Fatal("live event from the attached peer never reached the HandleEvent subscriber")
	}
}

// Given an attached session, when this desktop pulls history after its cursor,
// then the journaled page, new cursor and oldest-seq come back on the same wire
// shape the browser uses.
func TestOutbound_GivenAttachedRemoteSession_WhenPullHistory_ThenReturnJournaledPage(t *testing.T) {
	url := fakePeerServer(t, func(reg *rpc.Registry) {
		reg.Register(wire.MethodSessionPull, func(_ context.Context, raw json.RawMessage) (any, error) {
			var p wire.SessionPullParams
			require.NoError(t, json.Unmarshal(raw, &p))
			assert.Equal(t, int64(7), p.SessionID)
			assert.Equal(t, int64(0), p.Cursor)
			return wire.SessionPullResult{
				Notifications: []wire.JournaledNotification{{
					Seq: 1, Method: wire.NotifyEvent,
					Params: mustJSON(t, wire.EventFrame{SessionID: 7, Seq: 1, Event: mustJSON(t, map[string]any{"type": "message"})}),
				}},
				Cursor: 1, HasMore: false, OldestSeq: 1,
			}, nil
		})
	})
	ob := dialOutbound(t, url)

	page, err := ob.Pull(context.Background(), wire.SessionPullParams{SessionID: 7, Cursor: 0})
	require.NoError(t, err)
	require.Len(t, page.Notifications, 1)
	assert.Equal(t, int64(1), page.Notifications[0].Seq)
	assert.Equal(t, int64(1), page.Cursor)
	assert.Equal(t, int64(1), page.OldestSeq, "desktop transcripts never reclaim the oldest seq")
}

// Given an attached remote session, when this desktop sends a new message, then
// it lands in the peer's existing steer path.
func TestOutbound_GivenAttachedRemoteSession_WhenSteer_ThenMessageLandsOnPeer(t *testing.T) {
	var got wire.SteerParams
	url := fakePeerServer(t, func(reg *rpc.Registry) {
		reg.Register(wire.MethodSteer, func(_ context.Context, raw json.RawMessage) (any, error) {
			require.NoError(t, json.Unmarshal(raw, &got))
			return wire.OK{}, nil
		})
	})
	ob := dialOutbound(t, url)

	require.NoError(t, ob.Steer(context.Background(), wire.SteerParams{SessionID: 7, Text: "接着干"}))
	assert.Equal(t, int64(7), got.SessionID)
	assert.Equal(t, "接着干", got.Text)
}

// Given another endpoint already answered the pending ask, when this desktop
// submits the same answer, then alreadyHandled is surfaced instead of a blind
// success or a transport error.
func TestOutbound_GivenDecisionAlreadyHandled_WhenSubmitAnswer_ThenAlreadyHandledSurfaced(t *testing.T) {
	url := fakePeerServer(t, func(reg *rpc.Registry) {
		reg.Register(wire.MethodSubmitAnswer, func(_ context.Context, _ json.RawMessage) (any, error) {
			return wire.PeerSessionControlResult{AlreadyHandled: true}, nil
		})
		reg.Register(wire.MethodSubmitToolPermission, func(_ context.Context, _ json.RawMessage) (any, error) {
			return wire.PeerSessionControlResult{AlreadyHandled: true}, nil
		})
	})
	ob := dialOutbound(t, url)

	answer, err := ob.SubmitAnswer(context.Background(), wire.SubmitAnswerParams{SessionID: 7, RequestID: "req-1"})
	require.NoError(t, err)
	assert.True(t, answer.AlreadyHandled, "a second answer to the same ask must surface alreadyHandled")

	permission, err := ob.SubmitToolPermission(context.Background(), wire.SubmitToolPermissionParams{SessionID: 7, RequestID: "req-2", Allow: true})
	require.NoError(t, err)
	assert.True(t, permission.AlreadyHandled, "a second decision on the same tool permission must surface alreadyHandled")
}

// Given an older peer that returns the empty control result, when this desktop
// submits a decision, then alreadyHandled stays false — the legacy outcome is
// preserved, not misread as a second-winner race.
func TestOutbound_GivenLegacyPeerEmptyControlResult_WhenSubmitDecision_ThenAlreadyHandledStaysFalse(t *testing.T) {
	url := fakePeerServer(t, func(reg *rpc.Registry) {
		reg.Register(wire.MethodSubmitAnswer, func(_ context.Context, _ json.RawMessage) (any, error) {
			return wire.PeerSessionControlResult{}, nil
		})
	})
	ob := dialOutbound(t, url)

	result, err := ob.SubmitAnswer(context.Background(), wire.SubmitAnswerParams{SessionID: 7, RequestID: "req-1"})
	require.NoError(t, err)
	assert.False(t, result.AlreadyHandled, "an empty legacy result must preserve the original successful outcome")
}
