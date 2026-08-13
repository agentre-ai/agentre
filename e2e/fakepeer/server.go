// Package fakepeer implements the runner-owned loopback agentred protocol boundary.
package fakepeer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/pkg/jsonrpc"
)

// Fault controls the next runtime.run behavior through the loopback-only control API.
type Fault string

const (
	FaultNone       Fault = ""
	FaultDisconnect Fault = "disconnect"
	FaultProtocol   Fault = "protocol"
)

// Options contains only generated E2E identity and loopback configuration.
type Options struct {
	DeviceFingerprint string
	DeviceToken       string
	InstanceUUID      string
	ControlToken      string
}

// RecordedRequest is safe to expose to Playwright and failure artifacts.
type RecordedRequest struct {
	ConnectionID int64  `json:"connectionId"`
	Method       string `json:"method"`
	Params       any    `json:"params,omitempty"`
}

// Snapshot is a defensive, sanitized copy of the fake's observable protocol history.
type Snapshot struct {
	Requests []RecordedRequest `json:"requests"`
}

type session struct {
	id                int64
	providerSessionID string
	lifecycle         string
	latestSeq         int64
	notifications     []wire.JournaledNotification
}

// Server is a loopback WebSocket/JSON-RPC peer plus an authenticated control endpoint.
type Server struct {
	opts Options

	listener net.Listener
	http     *http.Server

	mu          sync.Mutex
	connections map[int64]*rpc.Conn
	requests    []RecordedRequest
	sessions    map[int64]*session
	nextFault   Fault
	closed      bool
	nextConnID  atomic.Int64
}

// Start binds a dynamic loopback port and serves /rpc plus /__control/*.
func Start(ctx context.Context, opts Options) (*Server, error) {
	if strings.TrimSpace(opts.DeviceFingerprint) == "" || strings.TrimSpace(opts.DeviceToken) == "" ||
		strings.TrimSpace(opts.InstanceUUID) == "" || strings.TrimSpace(opts.ControlToken) == "" {
		return nil, errors.New("fakepeer: generated identity and control token are required")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{
		opts:        opts,
		listener:    listener,
		connections: map[int64]*rpc.Conn{},
		sessions:    map[int64]*session{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", s.handleWebSocket)
	mux.HandleFunc("/__control/fault", s.handleFault)
	mux.HandleFunc("/__control/snapshot", s.handleSnapshot)
	s.http = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	go func() {
		_ = s.http.Serve(listener)
	}()
	return s, nil
}

// URL returns the exact agentred WebSocket endpoint.
func (s *Server) URL() string { return "ws://" + s.listener.Addr().String() + "/rpc" }

// ControlURL returns the loopback HTTP control root.
func (s *Server) ControlURL() string { return "http://" + s.listener.Addr().String() + "/__control" }

// DaemonFingerprint is the TOFU identity the desktop must seed.
func (s *Server) DaemonFingerprint() string { return rpc.DaemonFingerprint(s.opts.InstanceUUID) }

// SetNextRunFault configures one deterministic boundary failure.
func (s *Server) SetNextRunFault(fault Fault) {
	s.mu.Lock()
	s.nextFault = fault
	s.mu.Unlock()
}

// Snapshot returns sanitized requests only; credential values are replaced at record time.
func (s *Server) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RecordedRequest, len(s.requests))
	copy(out, s.requests)
	return Snapshot{Requests: out}
}

// Close stops listener and every upgraded socket. It is idempotent.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	connections := make([]*rpc.Conn, 0, len(s.connections))
	for _, conn := range s.connections {
		connections = append(connections, conn)
	}
	s.connections = map[int64]*rpc.Conn{}
	s.mu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.http.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	requested := websocket.Subprotocols(r)
	if len(requested) != 1 || requested[0] != rpc.Subprotocol {
		http.Error(w, "required WebSocket subprotocol is missing", http.StatusBadRequest)
		return
	}
	upgrader := websocket.Upgrader{
		Subprotocols: []string{rpc.Subprotocol},
		CheckOrigin:  func(*http.Request) bool { return true },
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	id := s.nextConnID.Add(1)
	registry := rpc.NewRegistry()
	conn := rpc.NewConn(rpc.NewWebSocketFrameConn(ws), registry)
	s.register(conn, registry, id)
	s.mu.Lock()
	s.connections[id] = conn
	s.mu.Unlock()
	go func() {
		conn.Serve(context.Background())
		s.mu.Lock()
		delete(s.connections, id)
		s.mu.Unlock()
	}()
}

func (s *Server) register(conn *rpc.Conn, registry *rpc.Registry, connectionID int64) {
	registry.Register("auth.connect", func(ctx context.Context, raw json.RawMessage) (any, error) {
		s.record(connectionID, "auth.connect", raw)
		var params rpc.ConnectParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, jsonrpc.ErrInvalidParams
		}
		if params.DeviceFingerprint != s.opts.DeviceFingerprint || params.DeviceToken != s.opts.DeviceToken ||
			params.ExpectedDaemonFingerprint != s.DaemonFingerprint() {
			return nil, jsonrpc.ErrUnauthorized
		}
		conn.SetAuth(rpc.AuthState{Authenticated: true, DeviceFingerprint: params.DeviceFingerprint})
		return rpc.ConnectResult{OK: true, InstanceUUID: s.opts.InstanceUUID}, nil
	})
	register := func(method string, handler rpc.HandlerFunc) {
		registry.Register(method, func(ctx context.Context, raw json.RawMessage) (any, error) {
			s.record(connectionID, method, raw)
			if !conn.Auth().Authenticated {
				return nil, jsonrpc.ErrUnauthorized
			}
			return handler(ctx, raw)
		})
	}
	register("health.ping", func(context.Context, json.RawMessage) (any, error) {
		return map[string]any{
			"instanceUUID": s.opts.InstanceUUID,
			"serverTimeMs": time.Now().UnixMilli(),
			"capabilities": []string{wire.CapLLMModelTargetV1},
		}, nil
	})
	register(wire.MethodCapabilities, func(_ context.Context, raw json.RawMessage) (any, error) {
		var params wire.CapabilitiesParams
		if err := json.Unmarshal(raw, &params); err != nil || params.BackendType == "" {
			return nil, jsonrpc.ErrInvalidParams
		}
		return wire.CapabilitiesResult{Capabilities: capability.Capabilities{
			Set: map[capability.Capability]bool{capability.CapAbort: true},
		}}, nil
	})
	register(wire.MethodSessionList, func(context.Context, json.RawMessage) (any, error) {
		return s.sessionList(), nil
	})
	register(wire.MethodSessionAttach, func(_ context.Context, raw json.RawMessage) (any, error) {
		var params wire.SessionAttachParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, jsonrpc.ErrInvalidParams
		}
		return s.attach(params.SessionID)
	})
	register(wire.MethodSessionPull, func(_ context.Context, raw json.RawMessage) (any, error) {
		var params wire.SessionPullParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, jsonrpc.ErrInvalidParams
		}
		return s.pull(params), nil
	})
	register(wire.MethodSessionPendingWaiters, func(context.Context, json.RawMessage) (any, error) {
		return wire.SessionPendingWaitersResult{}, nil
	})
	register(wire.MethodRun, func(_ context.Context, raw json.RawMessage) (any, error) {
		var params wire.RunParams
		if err := json.Unmarshal(raw, &params); err != nil || params.SessionID <= 0 {
			return nil, jsonrpc.ErrInvalidParams
		}
		fault := s.beginRun(params.SessionID)
		providerSessionID := fmt.Sprintf("e2e-remote-session-%d", params.SessionID)
		go s.streamRun(conn, params, providerSessionID, fault)
		return wire.RunAck{SessionID: params.SessionID, ProviderSessionID: providerSessionID}, nil
	})
}

func (s *Server) beginRun(sessionID int64) Fault {
	s.mu.Lock()
	defer s.mu.Unlock()
	fault := s.nextFault
	s.nextFault = FaultNone
	s.sessions[sessionID] = &session{
		id:                sessionID,
		providerSessionID: fmt.Sprintf("e2e-remote-session-%d", sessionID),
		lifecycle:         wire.SessionLifecycleRunning,
	}
	return fault
}

func (s *Server) streamRun(conn *rpc.Conn, params wire.RunParams, providerSessionID string, fault Fault) {
	// Ensure runtime.run's response is written before the first notification.
	time.Sleep(20 * time.Millisecond)
	chunks := []string{"remote-peer-", "reply: ", params.UserText}
	if fault == FaultDisconnect {
		chunks = []string{"remote-peer-partial: " + params.UserText}
	}
	for _, chunk := range chunks {
		eventRaw, err := json.Marshal(agentruntime.TextDelta{Text: chunk})
		if err != nil {
			return
		}
		frame := wire.EventFrame{SessionID: params.SessionID, Event: eventRaw}
		s.appendNotification(params.SessionID, wire.NotifyEvent, &frame)
		if err := conn.Notify(wire.NotifyEvent, frame); err != nil {
			return
		}
		if fault == FaultDisconnect {
			s.setLifecycle(params.SessionID, wire.SessionLifecycleInterrupted)
			_ = conn.Close()
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	if fault == FaultProtocol {
		malformed := wire.EventFrame{SessionID: params.SessionID, Event: json.RawMessage(`{"kind":"e2e_unknown"}`)}
		s.appendNotification(params.SessionID, wire.NotifyEvent, &malformed)
		_ = conn.Notify(wire.NotifyEvent, malformed)
		terminal := wire.RunResultDoneFrame{
			SessionID: params.SessionID, ProviderSessionID: providerSessionID,
			StopErrMsg: "e2e remote protocol failure",
		}
		s.appendNotification(params.SessionID, wire.NotifyRunResultDone, &terminal)
		s.setLifecycle(params.SessionID, wire.SessionLifecycleIdle)
		_ = conn.Notify(wire.NotifyRunResultDone, terminal)
		return
	}
	doneRaw, err := json.Marshal(agentruntime.Done{})
	if err != nil {
		return
	}
	doneEvent := wire.EventFrame{SessionID: params.SessionID, Event: doneRaw}
	s.appendNotification(params.SessionID, wire.NotifyEvent, &doneEvent)
	if err := conn.Notify(wire.NotifyEvent, doneEvent); err != nil {
		return
	}
	terminal := wire.RunResultDoneFrame{
		SessionID: params.SessionID, ProviderSessionID: providerSessionID,
		Model: "remote-peer-model",
	}
	s.appendNotification(params.SessionID, wire.NotifyRunResultDone, &terminal)
	s.setLifecycle(params.SessionID, wire.SessionLifecycleIdle)
	_ = conn.Notify(wire.NotifyRunResultDone, terminal)
}

func (s *Server) appendNotification(sessionID int64, method string, frame interface{ SetSeq(int64) }) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[sessionID]
	if sess == nil {
		return
	}
	sess.latestSeq++
	frame.SetSeq(sess.latestSeq)
	raw, err := json.Marshal(frame)
	if err != nil {
		return
	}
	var params map[string]any
	if json.Unmarshal(raw, &params) == nil {
		delete(params, "seq")
		raw, _ = json.Marshal(params)
	}
	sess.notifications = append(sess.notifications, wire.JournaledNotification{
		Seq: sess.latestSeq, Method: method, Params: raw,
	})
}

func (s *Server) setLifecycle(sessionID int64, lifecycle string) {
	s.mu.Lock()
	if sess := s.sessions[sessionID]; sess != nil {
		sess.lifecycle = lifecycle
	}
	s.mu.Unlock()
}

func (s *Server) sessionList() wire.SessionListResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]int64, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result := wire.SessionListResult{SupportsSessionMetadata: true}
	for _, id := range ids {
		sess := s.sessions[id]
		result.Sessions = append(result.Sessions, wire.SessionSummary{
			SessionID: sess.id, ProviderSessionID: sess.providerSessionID,
			BackendType: "claudecode", LifecycleState: sess.lifecycle, LatestSeq: sess.latestSeq,
		})
	}
	return result
}

func (s *Server) attach(sessionID int64) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[sessionID]
	if sess == nil {
		return nil, &jsonrpc.Error{Code: wire.ErrCodeSessionNotFound, Message: agentruntime.ErrSessionNotFound.Error()}
	}
	if sess.lifecycle == wire.SessionLifecycleInterrupted {
		return nil, &jsonrpc.Error{Code: wire.ErrCodeNoActiveTurn, Message: agentruntime.ErrNoActiveTurn.Error()}
	}
	return wire.SessionAttachResult{
		SessionID: sessionID, BackendType: "claudecode",
		LifecycleState: sess.lifecycle, LatestSeq: sess.latestSeq,
	}, nil
}

func (s *Server) pull(params wire.SessionPullParams) wire.SessionPullResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[params.SessionID]
	if sess == nil {
		return wire.SessionPullResult{Cursor: params.Cursor}
	}
	limit := params.Limit
	if limit <= 0 {
		limit = wire.DefaultSessionPullLimit
	}
	if limit > wire.MaxSessionPullLimit {
		limit = wire.MaxSessionPullLimit
	}
	result := wire.SessionPullResult{Cursor: params.Cursor}
	if len(sess.notifications) > 0 {
		result.OldestSeq = sess.notifications[0].Seq
	}
	for _, notification := range sess.notifications {
		if notification.Seq <= params.Cursor {
			continue
		}
		if len(result.Notifications) >= limit {
			result.HasMore = true
			break
		}
		result.Notifications = append(result.Notifications, notification)
		result.Cursor = notification.Seq
	}
	return result
}

func (s *Server) record(connectionID int64, method string, raw json.RawMessage) {
	var params any
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &params); err != nil {
			params = map[string]any{"invalidJSON": true, "bytes": len(raw)}
		}
	}
	s.mu.Lock()
	s.requests = append(s.requests, RecordedRequest{
		ConnectionID: connectionID, Method: method, Params: sanitize(params),
	})
	s.mu.Unlock()
}

func sanitize(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "credential") ||
				strings.Contains(lower, "secret") || strings.Contains(lower, "authorization") ||
				strings.Contains(lower, "fingerprint") {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = sanitize(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = sanitize(item)
		}
		return out
	default:
		return value
	}
}

func (s *Server) authorizeControl(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+s.opts.ControlToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *Server) handleFault(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeControl(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Fault Fault `json:"fault"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || (body.Fault != FaultDisconnect && body.Fault != FaultProtocol) {
		http.Error(w, "invalid fault", http.StatusBadRequest)
		return
	}
	s.SetNextRunFault(body.Fault)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeControl(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.Snapshot())
}
