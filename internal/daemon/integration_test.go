package daemon

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/daemon/client"
	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/canonical"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
	"github.com/agentre-ai/agentre/internal/pkg/ccoauth"
	remotefswire "github.com/agentre-ai/agentre/internal/pkg/remotefs/wire"

	"github.com/cago-frame/agents/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBackendRunner is an in-test Runtime the integration test swaps in for
// TypeClaudeCode via agentruntime.SwapRuntimeForTest. It emits a scripted
// sequence of NEW Event values and supports Steer/Abort.
type fakeBackendRunner struct {
	mu      sync.Mutex
	steered []string
	aborted []int64
	// scripted events emitted on each Run; replaced via setEvents.
	events []agentruntime.Event
}

func (*fakeBackendRunner) Capabilities() capability.Capabilities { return capability.Capabilities{} }

func (f *fakeBackendRunner) setEvents(evs []agentruntime.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = evs
}

func (f *fakeBackendRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	f.mu.Lock()
	evs := append([]agentruntime.Event(nil), f.events...)
	f.mu.Unlock()
	ch := make(chan agentruntime.Event, len(evs)+1)
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, &agentruntime.RunResult{}, nil
}

func (f *fakeBackendRunner) Steer(_ context.Context, _ int64, _ string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steered = append(f.steered, text)
	return nil
}

func (f *fakeBackendRunner) Abort(_ context.Context, sessionID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aborted = append(f.aborted, sessionID)
	return nil
}

// startTestDaemon spins a daemon on ephemeral port, returns it + cancel.
func startTestDaemon(t *testing.T) (*Daemon, func()) {
	t.Helper()
	dir := t.TempDir()
	d, err := New(Options{
		DataDir: dir,
		LANHost: "127.0.0.1",
		LANPort: 0,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	require.Eventually(t, func() bool {
		d.mu.RLock()
		ready := d.lan != nil && d.lan.Addr() != ""
		d.mu.RUnlock()
		return ready
	}, 2*time.Second, 10*time.Millisecond)
	return d, func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(3 * time.Second):
			t.Log("daemon did not shut down within 3s")
		}
	}
}

// readLocalPair dials the daemon's unix socket and calls /local/pair.
func readLocalPair(t *testing.T, d *Daemon) map[string]any {
	t.Helper()
	tr := &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
		return net.Dial("unix", d.SocketPath())
	}}
	c := &http.Client{Transport: tr}
	resp, err := c.Get("http://daemon/local/pair")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var v map[string]any
	require.NoError(t, json.Unmarshal(body, &v))
	return v
}

// TestIntegration_FullFlow exercises the protocol end-to-end at the raw
// JSON-RPC layer (no *remote.Runtime wrapper): runtime.run + runtime.event +
// runtime.runResultDone, plus llm.upsert / list, plus auth.pair handshake.
// Asserts that text_delta events round-trip and the terminal RunResult frame
// arrives after channel close.
func TestIntegration_FullFlow(t *testing.T) {
	// 1. Swap a fake backend runner for TypeClaudeCode.
	fake := &fakeBackendRunner{}
	fake.setEvents([]agentruntime.Event{
		agentruntime.TextDelta{Text: "hello"},
		agentruntime.TextDelta{Text: " world"},
		agentruntime.Done{},
	})
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, fake)
	t.Cleanup(restore)

	// 2. Boot the daemon.
	d, stop := startTestDaemon(t)
	defer stop()

	// 3. Get a pairing code via the unix socket.
	pairBody := readLocalPair(t, d)
	code, _ := pairBody["code"].(string)
	require.Len(t, code, 6)

	// 4. WS dial + auth.pair.
	d.mu.RLock()
	url := d.lan.URL()
	d.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, client.Options{URL: url})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	var pairResp struct {
		DeviceToken       string `json:"deviceToken"`
		DaemonFingerprint string `json:"daemonFingerprint"`
		InstanceUUID      string `json:"instanceUUID"`
	}
	require.NoError(t, c.Call(ctx, "auth.pair", map[string]any{
		"code":              code,
		"deviceName":        "test-mac",
		"deviceFingerprint": "sha256:test-device",
	}, &pairResp))
	assert.NotEmpty(t, pairResp.DeviceToken)
	assert.NotEmpty(t, pairResp.DaemonFingerprint)

	// 5. llm.upsert + llm.list round trip.
	require.NoError(t, c.Call(ctx, "llm.upsert", map[string]any{
		"providerKey": "4f8c1d2e-3b5a-4c6d-8e9f-1a2b3c4d5e6f",
		"name":        "anth",
		"type":        "anthropic",
		"baseURL":     "https://api.anthropic.com",
		"apiKey":      "sk-test",
		"updatedAt":   time.Now().UnixMilli(),
	}, nil))

	var listResp struct {
		Providers []struct {
			ProviderKey string `json:"providerKey"`
		} `json:"providers"`
	}
	require.NoError(t, c.Call(ctx, "llm.list", nil, &listResp))
	assert.Len(t, listResp.Providers, 1)

	// 6. Register runtime.event + runtime.runResultDone handlers BEFORE
	// runtime.run so we don't lose frames.
	events := make(chan wire.EventFrame, 16)
	c.Handle(wire.NotifyEvent, func(_ context.Context, p json.RawMessage) (any, error) {
		var f wire.EventFrame
		_ = json.Unmarshal(p, &f)
		events <- f
		return nil, nil
	})
	done := make(chan wire.RunResultDoneFrame, 1)
	c.Handle(wire.NotifyRunResultDone, func(_ context.Context, p json.RawMessage) (any, error) {
		var f wire.RunResultDoneFrame
		_ = json.Unmarshal(p, &f)
		done <- f
		return nil, nil
	})

	// 7. runtime.run with claudecode backend.
	backendJSON, _ := json.Marshal(map[string]any{
		"type": "claudecode",
		"id":   1,
		"name": "test-backend",
	})
	var ack wire.RunAck
	require.NoError(t, c.Call(ctx, wire.MethodRun, wire.RunParams{
		Backend:   json.RawMessage(backendJSON),
		SessionID: 42,
		Cwd:       t.TempDir(),
		UserText:  "hi",
	}, &ack))
	assert.Equal(t, int64(42), ack.SessionID)

	// 8. Drain at least one text_delta frame.
	got := drainEventFrames(t, events, 3*time.Second, 1)
	var sawText bool
	for _, f := range got {
		assert.Equal(t, int64(42), f.SessionID)
		ev, err := agentruntime.UnmarshalEvent(f.Event)
		require.NoError(t, err)
		if _, ok := ev.(agentruntime.TextDelta); ok {
			sawText = true
		}
	}
	assert.True(t, sawText, "expected at least one text_delta frame; got %d", len(got))

	// 9. runResultDone fires after the fake's channel closes.
	select {
	case f := <-done:
		assert.Equal(t, int64(42), f.SessionID)
		assert.Empty(t, f.StopErrMsg)
	case <-time.After(2 * time.Second):
		t.Fatal("runResultDone not received")
	}
}

// drainEventFrames collects at least minCount EventFrames (or until deadline),
// then non-blocking drains anything else already queued.
func drainEventFrames(t *testing.T, ch <-chan wire.EventFrame, deadline time.Duration, minCount int) []wire.EventFrame {
	t.Helper()
	out := []wire.EventFrame{}
	timeout := time.After(deadline)
	for len(out) < minCount {
		select {
		case ev := <-ch:
			out = append(out, ev)
		case <-timeout:
			return out
		}
	}
	for {
		select {
		case ev := <-ch:
			out = append(out, ev)
		default:
			return out
		}
	}
}

func TestIntegration_TLS_AllModes(t *testing.T) {
	certPath, keyPath, certPEM := writeSelfSignedPair(t, "127.0.0.1")

	// Swap a fake backend runner so we don't need a real provider/CLI.
	fake := &fakeBackendRunner{}
	fake.setEvents([]agentruntime.Event{agentruntime.Done{}})
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, fake)
	t.Cleanup(restore)

	// Use os.MkdirTemp with a short prefix in /tmp to avoid exceeding the
	// 104-byte macOS unix-socket path limit when t.TempDir() generates a
	// long path from the test name.
	dir, err := os.MkdirTemp("", "ard-tls")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d, err := New(Options{
		DataDir:     dir,
		LANHost:     "127.0.0.1",
		LANPort:     0,
		TLSCertFile: certPath,
		TLSKeyFile:  keyPath,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	require.Eventually(t, func() bool {
		d.mu.RLock()
		ready := d.lan != nil && d.lan.Addr() != ""
		d.mu.RUnlock()
		return ready
	}, 2*time.Second, 10*time.Millisecond)

	d.mu.RLock()
	wssURL := d.lan.URL()
	d.mu.RUnlock()
	require.True(t, strings.HasPrefix(wssURL, "wss://"), "expected wss URL, got %q", wssURL)

	cases := []struct {
		mode    client.TLSMode
		certArg string
		wantOK  bool
	}{
		{client.TLSPinCert, certPEM, true},
		{client.TLSCABundle, certPEM, true},
		{client.TLSSkipVerify, "", true},
		{client.TLSDefault, "", false}, // OS trust store does not have this self-signed cert.
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			cfg, err := client.BuildTLSConfig(tc.mode, tc.certArg)
			require.NoError(t, err)
			dialCtx, dialCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer dialCancel()
			c, err := client.Dial(dialCtx, client.Options{
				URL: wssURL, TLSConfig: cfg,
			})
			if tc.wantOK {
				require.NoError(t, err, "TLS mode %q must dial successfully", tc.mode)
				_ = c.Close()
			} else {
				assert.Error(t, err, "TLS mode %q must reject untrusted self-signed cert", tc.mode)
			}
		})
	}
}

func TestIntegration_UnauthGuard(t *testing.T) {
	fake := &fakeBackendRunner{}
	fake.setEvents([]agentruntime.Event{agentruntime.Done{}})
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, fake)
	t.Cleanup(restore)

	// Use os.MkdirTemp with a short prefix in /tmp to avoid exceeding the
	// 104-byte macOS unix-socket path limit when t.TempDir() generates a
	// long path from the test name.
	dir, err := os.MkdirTemp("", "ard-unauth")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	d, err := New(Options{
		DataDir: dir,
		LANHost: "127.0.0.1",
		LANPort: 0,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	require.Eventually(t, func() bool {
		d.mu.RLock()
		ready := d.lan != nil && d.lan.Addr() != ""
		d.mu.RUnlock()
		return ready
	}, 2*time.Second, 10*time.Millisecond)
	defer func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(3 * time.Second):
			t.Log("daemon did not shut down within 3s")
		}
	}()

	d.mu.RLock()
	url := d.lan.URL()
	d.mu.RUnlock()

	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()
	c, err := client.Dial(callCtx, client.Options{URL: url})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	// Skip auth.pair / auth.connect entirely. Any business method must return -32001.
	err = c.Call(callCtx, "llm.list", nil, &struct{}{})
	require.Error(t, err, "llm.list must be rejected without auth")
	var rpcErr *rpc.Error
	require.True(t, errors.As(err, &rpcErr), "error must be *rpc.Error")
	assert.Equal(t, -32001, rpcErr.Code)

	// remotefs.* 也走 requireAuth 闭包,未授权同样应回 -32001。
	err = c.Call(callCtx, remotefswire.MethodListDir, remotefswire.ListDirReq{}, &remotefswire.ListDirResp{})
	require.Error(t, err, "remotefs.listDir must be rejected without auth")
	var rfsErr *rpc.Error
	require.True(t, errors.As(err, &rfsErr), "error must be *rpc.Error")
	assert.Equal(t, -32001, rfsErr.Code)
}

// pacedBackendRunner emits events one at a time with a small inter-event gap so
// the daemon's WS fanout sends notifications sequentially — preventing the
// concurrent goroutine dispatch in Conn.Serve from allowing Done to race ahead
// of the last TextDelta on the RemoteRunner side.
type pacedBackendRunner struct {
	events []agentruntime.Event
}

func (*pacedBackendRunner) Capabilities() capability.Capabilities { return capability.Capabilities{} }

func (p *pacedBackendRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event, 1)
	go func() {
		defer close(ch)
		for _, ev := range p.events {
			ch <- ev
			time.Sleep(5 * time.Millisecond) // let fanout flush the WS frame before the next
		}
	}()
	return ch, &agentruntime.RunResult{}, nil
}

func (p *pacedBackendRunner) Steer(_ context.Context, _ int64, _ string, _ string) error { return nil }
func (p *pacedBackendRunner) Abort(_ context.Context, _ int64) error                     { return nil }

// pairedTestRig boots a daemon, pairs a WS client, and constructs a *remote.Runtime
// proxy on top so subtests can drive backend Events end-to-end through the full
// WS path. The script must end with agentruntime.Done so the daemon closes the
// fanout channel and emits runtime.runResultDone.
type pairedTestRig struct {
	dir    string
	d      *Daemon
	cli    *client.Client
	runner *remote.Runtime
}

func bootRemoteRig(t *testing.T, script []agentruntime.Event) *pairedTestRig {
	t.Helper()
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, &pacedBackendRunner{events: script})
	t.Cleanup(restore)

	// Short prefix avoids exceeding macOS 104-byte unix socket path limit.
	dir, err := os.MkdirTemp("", "ard-rig")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d, err := New(Options{
		DataDir: dir, LANHost: "127.0.0.1", LANPort: 0,
	})
	require.NoError(t, err)
	dCtx, dCancel := context.WithCancel(context.Background())
	dErrCh := make(chan error, 1)
	go func() { dErrCh <- d.Run(dCtx) }()
	require.Eventually(t, func() bool {
		d.mu.RLock()
		ready := d.lan != nil && d.lan.Addr() != ""
		d.mu.RUnlock()
		return ready
	}, 2*time.Second, 10*time.Millisecond)
	t.Cleanup(func() {
		dCancel()
		select {
		case <-dErrCh:
		case <-time.After(3 * time.Second):
			t.Log("daemon did not shut down within 3s")
		}
	})

	pairBody := readLocalPair(t, d)
	code, _ := pairBody["code"].(string)
	require.Len(t, code, 6)

	d.mu.RLock()
	url := d.lan.URL()
	d.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	cli, err := client.Dial(ctx, client.Options{URL: url})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })

	var pairResp struct {
		DeviceToken string `json:"deviceToken"`
	}
	require.NoError(t, cli.Call(ctx, "auth.pair", map[string]any{
		"code":              code,
		"deviceName":        "test-mac",
		"deviceFingerprint": "sha256:test-device",
	}, &pairResp))
	require.NotEmpty(t, pairResp.DeviceToken)

	return &pairedTestRig{dir: dir, d: d, cli: cli, runner: remote.New(cli)}
}

func (r *pairedTestRig) startRun(t *testing.T, sid int64) (<-chan agentruntime.Event, *agentruntime.RunResult) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	events, result, err := r.runner.Run(ctx, agentruntime.RunRequest{
		Backend: &agent_backend_entity.AgentBackend{
			Type: string(agent_backend_entity.TypeClaudeCode), ID: 1, Name: "test-backend",
		},
		AgentID: 1, SessionID: sid, Cwd: r.dir, UserText: "hi",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	return events, result
}

// drainRuntimeEvents collects every Event delivered to the *remote.Runtime
// caller until the channel closes (channel close marks turn-end).
func drainRuntimeEvents(t *testing.T, events <-chan agentruntime.Event, deadline time.Duration) []agentruntime.Event {
	t.Helper()
	var got []agentruntime.Event
	timeout := time.After(deadline)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-timeout:
			t.Fatalf("timed out waiting for channel close; got %d events", len(got))
		}
	}
}

// TestIntegration_RemoteRuntime_EventRoundTrip wires real daemon + real WS +
// real *remote.Runtime together and pumps every interesting Event Kind through
// the full path to prove the protocol round-trips losslessly. Coverage-critical
// Kinds (plan_updated / usage_update / subagent_lifecycle) each get their own
// subtest; the happy-path TextDelta scenario is the parent test.
func TestIntegration_RemoteRuntime_EventRoundTrip(t *testing.T) {
	t.Run("text_delta_sequence", func(t *testing.T) {
		rig := bootRemoteRig(t, []agentruntime.Event{
			agentruntime.TextDelta{Text: "hello"},
			agentruntime.TextDelta{Text: " from"},
			agentruntime.TextDelta{Text: " remote"},
			agentruntime.Done{},
		})
		events, result := rig.startRun(t, 100)
		got := drainRuntimeEvents(t, events, 5*time.Second)

		var texts []string
		for _, ev := range got {
			if td, ok := ev.(agentruntime.TextDelta); ok {
				texts = append(texts, td.Text)
			}
		}
		assert.Equal(t, []string{"hello", " from", " remote"}, texts)
		assert.NoError(t, result.StopErr)
	})

	t.Run("plan_updated_roundtrip", func(t *testing.T) {
		plan := canonical.PlanUpdate{
			Text: "## Plan\n- step 1\n- step 2",
			Steps: []canonical.PlanStep{
				{ID: "s1", Step: "do a", Status: canonical.StepCompleted},
				{ID: "s2", Step: "do b", Status: canonical.StepInProgress},
			},
		}
		rig := bootRemoteRig(t, []agentruntime.Event{
			agentruntime.PlanUpdated{Plan: plan},
			agentruntime.Done{},
		})
		events, _ := rig.startRun(t, 200)
		got := drainRuntimeEvents(t, events, 5*time.Second)

		var seen agentruntime.PlanUpdated
		var found bool
		for _, ev := range got {
			if pu, ok := ev.(agentruntime.PlanUpdated); ok {
				seen, found = pu, true
			}
		}
		require.True(t, found, "plan_updated event must round-trip; got %d events", len(got))
		assert.Equal(t, plan, seen.Plan)
	})

	t.Run("usage_update_ordering", func(t *testing.T) {
		usages := []*provider.Usage{
			{PromptTokens: 100, TotalTokens: 100},
			{PromptTokens: 200, TotalTokens: 200},
			{PromptTokens: 300, TotalTokens: 300},
		}
		rig := bootRemoteRig(t, []agentruntime.Event{
			agentruntime.UsageUpdate{Usage: usages[0], TotalInputTokens: 100},
			agentruntime.UsageUpdate{Usage: usages[1], TotalInputTokens: 200},
			agentruntime.UsageUpdate{Usage: usages[2], TotalInputTokens: 300},
			agentruntime.Done{},
		})
		events, _ := rig.startRun(t, 300)
		got := drainRuntimeEvents(t, events, 5*time.Second)

		var totals []int
		for _, ev := range got {
			if uu, ok := ev.(agentruntime.UsageUpdate); ok {
				totals = append(totals, uu.TotalInputTokens)
			}
		}
		assert.Equal(t, []int{100, 200, 300}, totals)
	})

	t.Run("subagent_lifecycle", func(t *testing.T) {
		rig := bootRemoteRig(t, []agentruntime.Event{
			agentruntime.SubagentStarted{
				ToolCallID: "tu_task",
				Info:       agentruntime.SubagentInfo{TaskID: "t1", SubagentType: "researcher", Status: "running"},
			},
			agentruntime.SubagentProgress{
				ToolCallID: "tu_task",
				Info:       agentruntime.SubagentInfo{TaskID: "t1", LastToolName: "Read", ToolUses: 1, Status: "running"},
			},
			agentruntime.SubagentDone{
				ToolCallID: "tu_task",
				Info:       agentruntime.SubagentInfo{TaskID: "t1", ToolUses: 3, TotalTokens: 1234, DurationMs: 4000, Status: "completed"},
			},
			agentruntime.Done{},
		})
		events, _ := rig.startRun(t, 400)
		got := drainRuntimeEvents(t, events, 5*time.Second)

		var kinds []string
		for _, ev := range got {
			switch ev.(type) {
			case agentruntime.SubagentStarted:
				kinds = append(kinds, "started")
			case agentruntime.SubagentProgress:
				kinds = append(kinds, "progress")
			case agentruntime.SubagentDone:
				kinds = append(kinds, "done")
			}
		}
		assert.Equal(t, []string{"started", "progress", "done"}, kinds,
			"subagent lifecycle must arrive in emit order")
	})
}

// TestIntegration_ErrorCodeRehydration drives a control RPC against a backend
// that does NOT implement the corresponding sub-interface, asserting that the
// daemon returns ErrUnsupported, the wire layer maps it to JSON-RPC error
// code -32012, and the *remote.Runtime client rehydrates it back to the
// sentinel for errors.Is. pacedBackendRunner implements Steerer + Aborter but
// NOT PermissionModeSetter, so SetPermissionMode is the test vehicle.
func TestIntegration_ErrorCodeRehydration(t *testing.T) {
	rig := bootRemoteRig(t, []agentruntime.Event{
		// Hold the turn open long enough for the SetPermissionMode RPC to be
		// dispatched against a still-live session.
		agentruntime.TextDelta{Text: "warming"},
		agentruntime.TextDelta{Text: " up"},
		agentruntime.Done{},
	})
	events, _ := rig.startRun(t, 500)

	// SetPermissionMode racing with channel-close on a no-PM-setter backend
	// must return ErrUnsupported via the wire sentinel rehydrate path.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rig.runner.SetPermissionMode(ctx, 500, "plan")
	require.Error(t, err)
	assert.ErrorIs(t, err, agentruntime.ErrUnsupported,
		"daemon ErrUnsupported must round-trip via wire code -32012 to client errors.Is")

	// Drain to keep harness clean.
	_ = drainRuntimeEvents(t, events, 5*time.Second)
}

// TestIntegration_MCPReverseTunnel 端到端验证内置工具 MCP 反向隧道(org/subagent/group/
// workflow 在远端 agentred 执行时可用):真 daemon + 真 WS + 真 *remote.Runtime。daemon 本机
// gateway 的 /mcp/ 隧道入口收到 CLI 子进程(此处用裸 HTTP 模拟)的请求后,经 WS
// MethodMCPProxy 反向请求回 desktop,desktop 用注入的 dispatcher 重放到本机真 gateway(此处
// 用 httptest 充当真 /mcp/* handler 的替身),应答原路返回。断言 path / 鉴权头 / body 全程
// 保真,且响应正确回流——这是 06023bb 反向隧道唯一被全链路覆盖的路径(其余各 seam 是单测)。
func TestIntegration_MCPReverseTunnel(t *testing.T) {
	// desktop 侧:httptest 充当 desktop 本机真 gateway(真 /mcp/org/ handler 的替身),
	// 记录隧道送达的请求,回一个 JSON-RPC 应答。
	var (
		gotPath, gotMethod, gotAuth string
		gotBody                     []byte
	)
	desktopGW := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotAuth = r.URL.Path, r.Method, r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer desktopGW.Close()

	// desktop 侧装配 dispatcher(就是 bootstrap.Init 里那条),把隧道请求重放到本机真
	// gateway。进程级全局,测后清空。
	remote.RegisterMCPProxyDispatcher(remote.NewLocalGatewayDispatcher(
		func() string { return desktopGW.URL }, desktopGW.Client()))
	t.Cleanup(func() { remote.RegisterMCPProxyDispatcher(nil) })

	// 真 daemon + 真 WS 客户端;remote.New(cli) 已在 rig 内注册 MethodMCPProxy 反向 handler。
	// script 仅 Done:本测不跑 runtime.run,只验隧道(此刻 daemon 已记下活跃 notifier)。
	rig := bootRemoteRig(t, []agentruntime.Event{agentruntime.Done{}})

	// daemon 本机 gateway 的隧道入口(真机上 CLI 子进程被改写后打的就是这个 base)。
	base := rig.d.gateway.BaseURL()
	require.NotEmpty(t, base, "daemon gateway must be running for the /mcp/ tunnel entry")

	// 模拟 daemon 上的 CLI 子进程:POST /mcp/org/,带 desktop 轮起手时签的 token。
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	httpReq, err := http.NewRequest(http.MethodPost, base+"/mcp/org/", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Authorization", "Bearer desktop-signed-tok")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)

	// 1) desktop 应答原路回到「CLI」:状态码 / Content-Type / body 都还原。
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	require.Equal(t, `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`, string(respBody))

	// 2) 请求经 WS 隧道送达 desktop gateway 时 path / method / 鉴权头 / body 全程保真。
	require.Equal(t, "/mcp/org/", gotPath)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "Bearer desktop-signed-tok", gotAuth)
	require.Equal(t, reqBody, string(gotBody))
}

// TestIntegration_MCPReverseTunnel_NoDispatcher 验证 desktop 侧未装配 dispatcher 时,隧道
// 不会打挂 RPC 连接,而是把 502 以 HTTP 应答回给 CLI(handleMCPProxy 的兜底)。
func TestIntegration_MCPReverseTunnel_NoDispatcher(t *testing.T) {
	// 显式清空 dispatcher(remote 包进程级全局),并在测后保持清空。
	remote.RegisterMCPProxyDispatcher(nil)
	t.Cleanup(func() { remote.RegisterMCPProxyDispatcher(nil) })

	rig := bootRemoteRig(t, []agentruntime.Event{agentruntime.Done{}})
	base := rig.d.gateway.BaseURL()
	require.NotEmpty(t, base)

	httpReq, err := http.NewRequest(http.MethodPost, base+"/mcp/org/",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// 未装配 dispatcher → desktop handleMCPProxy 回 502(而非让反向 RPC 失败打挂连接)。
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
}

// rigSkillDisc 替身发现器,供 skills.list 集成测试在 daemon 侧换上,免依赖真实 claude。
type rigSkillDisc struct{ packs []agentskill.SkillPack }

func (d rigSkillDisc) Discover(_ context.Context, _ agentskill.DiscoverQuery) ([]agentskill.SkillPack, error) {
	return d.packs, nil
}

// TestIntegration_SkillsList 端到端验证 skills.list:真 daemon + 真 WS + paired client。
// daemon 侧换上替身发现器(daemon 与测试同进程,共享 agentskill 全局注册表),paired
// client 调 skills.list,断言 daemon 本机已装技能包经 RPC 原样回传 —— 远端 agent 配
// per-agent 技能时,desktop 据此展 daemon 真实可用集(而非 desktop 本地的)。
func TestIntegration_SkillsList(t *testing.T) {
	want := []agentskill.SkillPack{
		{ID: "superpowers@claude-plugins-official", Name: "superpowers", Installed: true, Source: agentskill.SourceInstalled, GloballyEnabled: true},
		{ID: "opsctl@opskat", Name: "opsctl", Installed: true, Source: agentskill.SourceInstalled},
	}
	restore := agentskill.SwapDiscovererForTest(agent_backend_entity.TypeClaudeCode, rigSkillDisc{packs: want})
	t.Cleanup(restore)
	// daemon 本机 CLI 路径解析换成桩,免依赖宿主 PATH(替身发现器不消费它,这里仅求确定性)。
	handlers.SetResolveCLIPathFunc(func(string) (string, bool, error) { return "/daemon/bin/claude", true, nil })
	t.Cleanup(handlers.ResetResolveCLIPathFunc)

	rig := bootRemoteRig(t, []agentruntime.Event{agentruntime.Done{}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var resp handlers.SkillsListResult
	require.NoError(t, rig.cli.Call(ctx, "skills.list",
		handlers.SkillsListParams{BackendType: "claudecode"}, &resp))

	require.Len(t, resp.Packs, 2)
	require.Equal(t, "superpowers@claude-plugins-official", resp.Packs[0].ID)
	require.True(t, resp.Packs[0].GloballyEnabled)
	require.Equal(t, "opsctl@opskat", resp.Packs[1].ID)
}

func TestIntegration_HealthPing(t *testing.T) {
	d, stop := startTestDaemon(t)
	defer stop()

	pairBody := readLocalPair(t, d)
	code, _ := pairBody["code"].(string)
	require.Len(t, code, 6)

	d.mu.RLock()
	serverURL := d.lan.URL()
	d.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 已鉴权的客户端：auth.pair 后调 health.ping。
	c, err := client.Dial(ctx, client.Options{URL: serverURL})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	var pairResp struct {
		DeviceToken string `json:"deviceToken"`
	}
	require.NoError(t, c.Call(ctx, "auth.pair", map[string]any{
		"code":              code,
		"deviceName":        "test-mac",
		"deviceFingerprint": "sha256:test-health",
	}, &pairResp))
	require.NotEmpty(t, pairResp.DeviceToken)

	// health.ping returns instanceUUID + serverTimeMs。
	var pingRes struct {
		InstanceUUID string `json:"instanceUUID"`
		ServerTimeMs int64  `json:"serverTimeMs"`
	}
	err = c.Call(ctx, "health.ping", nil, &pingRes)
	require.NoError(t, err)
	assert.NotEmpty(t, pingRes.InstanceUUID)
	assert.Greater(t, pingRes.ServerTimeMs, int64(0))

	// health.ping requires auth: 未鉴权的裸连接必须返回 -32001。
	raw, err := client.Dial(ctx, client.Options{URL: serverURL})
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()

	var anyRes any
	err = raw.Call(ctx, "health.ping", nil, &anyRes)
	require.Error(t, err, "health.ping must be rejected without auth")
	var rpcErr *rpc.Error
	require.True(t, errors.As(err, &rpcErr), "error must be *rpc.Error")
	assert.Equal(t, -32001, rpcErr.Code)
}

// TestIntegration_CCUsage 验证 claudecode.usage RPC 注册成功、走过 auth 鉴权、
// 并把 CCUsageFetcher 注入的结果正确序列化回客户端。
// (test 名故意保持短:macOS 单元 socket 路径上限 104 字节,t.TempDir 已经吃掉很多)
func TestIntegration_CCUsage(t *testing.T) {
	stub := func(_ context.Context) (*ccoauth.RateLimits, error) {
		return &ccoauth.RateLimits{FiveHourPercent: 73, WeeklyPercent: 25}, nil
	}
	dir := t.TempDir()
	d, err := New(Options{
		DataDir:        dir,
		LANHost:        "127.0.0.1",
		LANPort:        0,
		CCUsageFetcher: stub,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(3 * time.Second):
		}
	}()
	require.Eventually(t, func() bool {
		select {
		case e := <-errCh:
			t.Logf("daemon Run exited early: %v", e)
			errCh <- e
			return false
		default:
		}
		d.mu.RLock()
		ready := d.lan != nil && d.lan.Addr() != ""
		d.mu.RUnlock()
		return ready
	}, 2*time.Second, 10*time.Millisecond)

	pairBody := readLocalPair(t, d)
	code, _ := pairBody["code"].(string)
	require.Len(t, code, 6)

	d.mu.RLock()
	serverURL := d.lan.URL()
	d.mu.RUnlock()

	callCtx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ccancel()

	c, err := client.Dial(callCtx, client.Options{URL: serverURL})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	var pairResp struct {
		DeviceToken string `json:"deviceToken"`
	}
	require.NoError(t, c.Call(callCtx, "auth.pair", map[string]any{
		"code":              code,
		"deviceName":        "test-cc",
		"deviceFingerprint": "sha256:test-ccusage",
	}, &pairResp))

	var got handlers.CCUsageResult
	err = c.Call(callCtx, "claudecode.usage", nil, &got)
	require.NoError(t, err)
	assert.Equal(t, "ok", got.Reason)
	require.NotNil(t, got.Data)
	assert.Equal(t, float64(73), got.Data.FiveHourPercent)
	assert.Equal(t, float64(25), got.Data.WeeklyPercent)

	// 鉴权门禁:裸连接(未 auth.pair)必须被拒,统一 -32001。
	raw, err := client.Dial(callCtx, client.Options{URL: serverURL})
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()
	var any2 any
	err = raw.Call(callCtx, "claudecode.usage", nil, &any2)
	require.Error(t, err)
	var rpcErr *rpc.Error
	require.True(t, errors.As(err, &rpcErr))
	assert.Equal(t, -32001, rpcErr.Code)
}

// writeSelfSignedPair generates a self-signed ECDSA cert/key in t.TempDir
// and returns their paths. Duplicated from rpc/transport_lan_test.go to
// keep test imports trivial; for MVP this 30-line duplication is fine.
func writeSelfSignedPair(t *testing.T, host string) (certPath, keyPath, certPEM string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP(host)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)

	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	certBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NotNil(t, certBytes)
	require.NoError(t, os.WriteFile(certPath, certBytes, 0o600))

	kb, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	require.NotNil(t, keyBytes)
	require.NoError(t, os.WriteFile(keyPath, keyBytes, 0o600))
	certPEM = string(certBytes)
	return
}

func TestIntegration_CLIResolvePath(t *testing.T) {
	// 注入 fake resolve fn 让远端不依赖宿主 PATH 状态。
	handlers.SetResolveCLIPathFunc(func(backendType string) (string, bool, error) {
		require.Equal(t, "claudecode", backendType)
		return "/fake/remote/bin/claude", true, nil
	})
	t.Cleanup(handlers.ResetResolveCLIPathFunc)

	// 用 os.MkdirTemp 短前缀,避免 t.TempDir() 长测试名超过 macOS 104 字节 unix socket 限制。
	dir, err := os.MkdirTemp("", "ard-cli")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d, err := New(Options{
		DataDir: dir,
		LANHost: "127.0.0.1",
		LANPort: 0,
	})
	require.NoError(t, err)
	dCtx, dCancel := context.WithCancel(context.Background())
	dErrCh := make(chan error, 1)
	go func() { dErrCh <- d.Run(dCtx) }()
	require.Eventually(t, func() bool {
		d.mu.RLock()
		ready := d.lan != nil && d.lan.Addr() != ""
		d.mu.RUnlock()
		return ready
	}, 2*time.Second, 10*time.Millisecond)
	t.Cleanup(func() {
		dCancel()
		select {
		case <-dErrCh:
		case <-time.After(3 * time.Second):
			t.Log("daemon did not shut down within 3s")
		}
	})

	pairBody := readLocalPair(t, d)
	code, _ := pairBody["code"].(string)
	require.Len(t, code, 6)

	d.mu.RLock()
	url := d.lan.URL()
	d.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, client.Options{URL: url})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	var pairResp struct {
		DeviceToken string `json:"deviceToken"`
	}
	require.NoError(t, c.Call(ctx, "auth.pair", map[string]any{
		"code":              code,
		"deviceName":        "test-mac",
		"deviceFingerprint": "sha256:test-device",
	}, &pairResp))
	require.NotEmpty(t, pairResp.DeviceToken)

	var resp handlers.CLIResolvePathResult
	err = c.Call(ctx, "cli.resolvePath", handlers.CLIResolvePathParams{Type: "claudecode"}, &resp)
	require.NoError(t, err)
	assert.Equal(t, "/fake/remote/bin/claude", resp.Path)
	assert.True(t, resp.Found)
}
