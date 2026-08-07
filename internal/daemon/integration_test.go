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
	"sort"
	"strconv"
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
	workspacefswire "github.com/agentre-ai/agentre/internal/pkg/workspacefs/wire"

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

	// workspacefs.* 是独立方法族(spec 设计决策 5),同样套 requireAuth 闭包,
	// 未授权应回 -32001。
	err = c.Call(callCtx, workspacefswire.MethodListDir, workspacefswire.ListDirReq{}, &workspacefswire.ListDirResp{})
	require.Error(t, err, "workspacefs.listDir must be rejected without auth")
	var wfsErr *rpc.Error
	require.True(t, errors.As(err, &wfsErr), "error must be *rpc.Error")
	assert.Equal(t, -32001, wfsErr.Code)
}

// TestIntegration_WorkspaceFsListDir_EndToEnd 验证 workspacefs.listDir 在鉴权
// 后能端到端跑通一跳:配对拿 deviceToken,再用它调用 daemon 真实文件系统上的
// 一个临时目录,断言拿到真实条目而不是错误。未鉴权已由 TestIntegration_UnauthGuard
// 覆盖,这里只覆盖“鉴权后可用”这一半。
//
// 用 bootRigInDir + os.MkdirTemp 短前缀(而不是 startTestDaemon 的 t.TempDir()):
// 长测试名 + t.TempDir() 生成的深路径会超过 macOS 104 字节 unix socket 限制
// (同 TestIntegration_UnauthGuard/bootRemoteRig 的既有取舍)。
func TestIntegration_WorkspaceFsListDir_EndToEnd(t *testing.T) {
	dir, err := os.MkdirTemp("", "ard-wsfs")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	rig := bootRigInDir(t, dir)

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi"), 0o644))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var resp workspacefswire.ListDirResp
	require.NoError(t, rig.cli.Call(ctx, workspacefswire.MethodListDir, workspacefswire.ListDirReq{Root: root}, &resp))
	assert.Equal(t, root, resp.Path)
	names := map[string]workspacefswire.Entry{}
	for _, e := range resp.Entries {
		names[e.Name] = e
	}
	assert.Contains(t, names, "a.txt")
	assert.False(t, names["a.txt"].IsDir)
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
	// token 是配对拿到的 deviceToken:同一台设备再开一条连接时走 auth.connect
	// (真机上的设备监视心跳 / 刷新探测就是这么接的),见 connectSameDevice。
	token string
	// stop 关掉这台 daemon(幂等,t.Cleanup 也会调一次)。daemon 重启用例要在同一个
	// 数据目录上先停再起,所以关停必须是显式可控的。
	stop func()
}

// rigDeviceFingerprint 是 rig 里那台「桌面端」的设备指纹。同一台桌面同时开的多条
// 连接共用它 —— 会话推送因此不能按指纹解析(见 connectSameDevice 的两个回归用例)。
const rigDeviceFingerprint = "sha256:test-device"

func bootRemoteRig(t *testing.T, script []agentruntime.Event) *pairedTestRig {
	t.Helper()
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, &pacedBackendRunner{events: script})
	t.Cleanup(restore)

	// Short prefix avoids exceeding macOS 104-byte unix socket path limit.
	dir, err := os.MkdirTemp("", "ard-rig")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return bootRigInDir(t, dir)
}

// bootRigInDir 在**给定的**数据目录上起一台 daemon 并配对一台桌面端。数据目录是显式
// 参数,是为了 daemon 重启用例:同一个目录上先后起两台 daemon,第二台读到的正是第一台
// 留下的那个库(R10 的启动清扫要扫的就是它)。
func bootRigInDir(t *testing.T, dir string) *pairedTestRig {
	t.Helper()
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
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			dCancel()
			select {
			case <-dErrCh:
			case <-time.After(3 * time.Second):
				t.Log("daemon did not shut down within 3s")
			}
		})
	}
	t.Cleanup(stop)

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
		"deviceFingerprint": rigDeviceFingerprint,
	}, &pairResp))
	require.NotEmpty(t, pairResp.DeviceToken)

	return &pairedTestRig{dir: dir, d: d, cli: cli, runner: remote.New(cli), token: pairResp.DeviceToken, stop: stop}
}

// connectSameDevice 再开一条**同一台设备**的已认证连接(auth.connect,与桌面端的
// 设备监视心跳 remote_device_svc/watcher.go / 刷新探测 refresh.go 走的是同一条路)。
// 一台桌面端同时开 2-3 条这样的连接是常态:连接池那条承载会话,其余只做保活。
func (r *pairedTestRig) connectSameDevice(t *testing.T) *client.Client {
	t.Helper()
	r.d.mu.RLock()
	url := r.d.lan.URL()
	r.d.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	cli, err := client.Dial(ctx, client.Options{URL: url})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	var res map[string]any
	require.NoError(t, cli.Call(ctx, "auth.connect", map[string]any{
		"deviceFingerprint": rigDeviceFingerprint,
		"deviceToken":       r.token,
	}, &res))
	require.Equal(t, true, res["ok"], "second connection of the same device must authenticate")
	return cli
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

// TestIntegration_StrayConnDoesNotStealSessionNotifications 回归:一条完成 WS 升级却
// **从不认证**的连接(LAN 扫描器 / 鉴权失败的客户端 / 掉队的重连)接入时,已配对设备
// 上正在跑的会话必须照常收到推送。
//
// 旧实现在 daemon 上只留一个全局槽,而登记发生在鉴权**之前**(bindConn 是 OnConn 回调,
// auth.pair / auth.connect 要等后续 RPC 才跑),所以野连接一进来就顶掉正主,真设备的
// 指纹从此解析为 nil:会话照常落库、一条推不出去,而客户端既看不到错误也看不到 seq
// 跳号,补齐永不触发,整轮无限期卡住。这里以「野连接接入后事件仍逐条到达客户端」钉死它。
func TestIntegration_StrayConnDoesNotStealSessionNotifications(t *testing.T) {
	rig := bootRemoteRig(t, []agentruntime.Event{
		agentruntime.TextDelta{Text: "hello"},
		agentruntime.TextDelta{Text: " world"},
		agentruntime.Done{},
	})

	rig.d.mu.RLock()
	lanURL := rig.d.lan.URL()
	rig.d.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stray, err := client.Dial(ctx, client.Options{URL: lanURL})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stray.Close() })

	// 先打一发必被拒的业务 RPC:既证明野连接确实没有身份,又确保 daemon 已经 bind 了
	// 它(bindConn 在 Serve 之前同步跑,拿到应答就说明它早已跑完)—— 否则本测会与
	// 升级握手赛跑,偶发地根本没复现出「顶掉」的时序。
	var pong map[string]any
	require.Error(t, stray.Call(ctx, "health.ping", nil, &pong),
		"never-authenticated connection must be rejected by requireAuth")

	events, _ := rig.startRun(t, 700)
	got := drainRuntimeEvents(t, events, 5*time.Second)

	var texts []string
	for _, ev := range got {
		if td, ok := ev.(agentruntime.TextDelta); ok {
			texts = append(texts, td.Text)
		}
	}
	assert.Equal(t, []string{"hello", " world"}, texts,
		"a stray unauthenticated connection must not divert the paired device's session notifications")
}

// gatedBackendRunner 把一轮执行的事件流劈成两段:先发 before,停在 gate 上,gate 关闭
// 后再发 after。用它把「同一台设备的第二条连接接入」精确摆在一轮执行的**中途**,而不是
// 与事件流赛跑 —— 否则用例会偶发地在事件全部送达之后才接入,复现不出被顶掉的时序。
type gatedBackendRunner struct {
	before []agentruntime.Event
	gate   <-chan struct{}
	after  []agentruntime.Event
}

func (*gatedBackendRunner) Capabilities() capability.Capabilities { return capability.Capabilities{} }

func (g *gatedBackendRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event, 1)
	go func() {
		defer close(ch)
		for _, ev := range g.before {
			ch <- ev
			time.Sleep(5 * time.Millisecond)
		}
		<-g.gate
		for _, ev := range g.after {
			ch <- ev
			time.Sleep(5 * time.Millisecond)
		}
	}()
	return ch, &agentruntime.RunResult{}, nil
}

// bootGatedRig 起一个 rig,并把 backend 换成受测试控制的 gatedBackendRunner。
func bootGatedRig(t *testing.T, r *gatedBackendRunner) *pairedTestRig {
	t.Helper()
	rig := bootRemoteRig(t, []agentruntime.Event{agentruntime.Done{}})
	// bootRemoteRig 自己也换过一次 backend;t.Cleanup 是后进先出,这里的还原先跑。
	t.Cleanup(agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, r))
	return rig
}

// approvalBackendRunner 是 gatedBackendRunner 再加一个**审批协议**:它记下真正送达
// backend 的每一次工具审批提交。用它才能把「daemon 报了成功」与「waiter 真被回答了」
// 分开看 —— 这两件事分家正是静默挂死的定义。
type approvalBackendRunner struct {
	before []agentruntime.Event
	gate   <-chan struct{}
	after  []agentruntime.Event

	mu        sync.Mutex
	delivered []string
}

func (*approvalBackendRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{}
}

func (a *approvalBackendRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event, 1)
	go func() {
		defer close(ch)
		for _, ev := range a.before {
			ch <- ev
			time.Sleep(5 * time.Millisecond)
		}
		<-a.gate
		for _, ev := range a.after {
			ch <- ev
			time.Sleep(5 * time.Millisecond)
		}
	}()
	return ch, &agentruntime.RunResult{}, nil
}

func (a *approvalBackendRunner) SubmitToolPermission(_ context.Context, _ int64, requestID string, _, _ bool, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.delivered = append(a.delivered, requestID)
	return nil
}

func (a *approvalBackendRunner) deliveredIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.delivered...)
}

// bootApprovalRig 起一个 rig,并把 backend 换成实现了审批协议的 approvalBackendRunner。
func bootApprovalRig(t *testing.T, r *approvalBackendRunner) *pairedTestRig {
	t.Helper()
	rig := bootRemoteRig(t, []agentruntime.Event{agentruntime.Done{}})
	t.Cleanup(agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, r))
	return rig
}

// noApprovalText 让 keyedApprovalRunner 把这一轮直接跑完(不留待决策);其余 UserText
// 的那一轮登记一个阻塞中的工具审批并停在 gate 上。
const noApprovalText = "no-approval"

// keyedApprovalRunner 是一个**按 backend 自己那把会话键索引待决策**的 fake。真实
// backend 都是这么索引的(claudecode 的 sessionKey(id)、codex 的 r.active[sessionID]),
// 所以「两个对端各自的同号会话会不会撞成同一条」在它身上如实反映生产行为 —— 换成一个
// 无视会话键、对谁都回同一份快照的替身,这条泄漏就测不出来了。
type keyedApprovalRunner struct {
	gate <-chan struct{}

	mu        sync.Mutex
	waiters   map[int64]agentruntime.PendingToolPermission
	delivered map[int64][]string
}

func newKeyedApprovalRunner(gate <-chan struct{}) *keyedApprovalRunner {
	return &keyedApprovalRunner{
		gate:      gate,
		waiters:   map[int64]agentruntime.PendingToolPermission{},
		delivered: map[int64][]string{},
	}
}

func (*keyedApprovalRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{}
}

func (a *keyedApprovalRunner) Run(_ context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event, 1)
	if req.UserText == noApprovalText {
		go func() {
			defer close(ch)
			ch <- agentruntime.Done{}
		}()
		return ch, &agentruntime.RunResult{}, nil
	}
	a.mu.Lock()
	a.waiters[req.SessionID] = agentruntime.PendingToolPermission{
		RequestID: "req-of-the-owner",
		ToolName:  "Bash",
		Input:     json.RawMessage(`{"command":"cat ~/.ssh/id_rsa"}`),
	}
	a.mu.Unlock()
	go func() {
		defer close(ch)
		ch <- agentruntime.TextDelta{Text: "blocked"}
		<-a.gate
		ch <- agentruntime.Done{}
	}()
	return ch, &agentruntime.RunResult{}, nil
}

func (a *keyedApprovalRunner) PendingWaiters(_ context.Context, sid int64) agentruntime.WaiterSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	w, ok := a.waiters[sid]
	if !ok {
		return agentruntime.WaiterSnapshot{}
	}
	return agentruntime.WaiterSnapshot{ToolPermissions: []agentruntime.PendingToolPermission{w}}
}

func (a *keyedApprovalRunner) SubmitToolPermission(_ context.Context, sid int64, requestID string, _, _ bool, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	w, ok := a.waiters[sid]
	if !ok || w.RequestID != requestID {
		return agentruntime.ErrWaiterNotFound
	}
	delete(a.waiters, sid)
	a.delivered[sid] = append(a.delivered[sid], requestID)
	return nil
}

// deliveredIDs 返回真正送达 backend 的全部 requestID(不分会话键)。「daemon 报了成功」
// 与「waiter 真被回答了」是两件事,只有这里看得到后者。
func (a *keyedApprovalRunner) deliveredIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, ids := range a.delivered {
		out = append(out, ids...)
	}
	return out
}

func (a *keyedApprovalRunner) waiterCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.waiters)
}

// bootKeyedApprovalRig 起一个 rig,并把 backend 换成按会话键索引待决策的 fake。
func bootKeyedApprovalRig(t *testing.T, r *keyedApprovalRunner) *pairedTestRig {
	t.Helper()
	rig := bootRemoteRig(t, []agentruntime.Event{agentruntime.Done{}})
	t.Cleanup(agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, r))
	return rig
}

// startRunAs 在给定连接上直接发一次 runtime.run(不经 *remote.Runtime)。第二台配对
// 设备手里只有一条裸连接,而 R16 的隔离恰恰要求它也起得了自己的会话。
func startRunAs(t *testing.T, cli *client.Client, dir string, sid int64, userText string) {
	t.Helper()
	be, err := json.Marshal(agent_backend_entity.AgentBackend{
		Type: string(agent_backend_entity.TypeClaudeCode), ID: 1, Name: "test-backend",
	})
	require.NoError(t, err)
	var ack wire.RunAck
	require.NoError(t, callRig(t, cli, wire.MethodRun, wire.RunParams{
		Backend: be, AgentID: 1, SessionID: sid, Cwd: dir, UserText: userText,
	}, &ack))
	require.Equal(t, sid, ack.SessionID)
}

// awaitLifecycle 等某个对端名下那条会话进入指定生命周期状态。
func awaitLifecycle(t *testing.T, cli *client.Client, sid int64, state string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var list wire.SessionListResult
		if err := callRig(t, cli, wire.MethodSessionList, nil, &list); err != nil {
			return false
		}
		for _, s := range list.Sessions {
			if s.SessionID == sid {
				return s.LifecycleState == state
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond, "会话 %d 没有进入 %s", sid, state)
}

// awaitText 等下一条 TextDelta 并断言文本,超时即失败(会话被推去了别处 / 挂起时就是
// 这个表现:客户端既没有错误也没有事件,只是永远收不到)。
func awaitText(t *testing.T, events <-chan agentruntime.Event, want string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("events channel closed while waiting for %q", want)
			}
			if td, isText := ev.(agentruntime.TextDelta); isText {
				require.Equal(t, want, td.Text)
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q — the session's notifications never reached the connection that started it", want)
		}
	}
}

// TestIntegration_SecondConnOfSameDeviceDoesNotStealSessionNotifications 回归(评审在真
// 环境复现的其一):一台桌面端同时开多条**同指纹**的已认证连接 —— 连接池那条承载会话
// (chat_svc/chat.go 的 remote.New(lease.Client())),设备监视心跳(watcher.go)与刷新探测
// (refresh.go)各占一条。按设备指纹解析推送目标时,后认证的心跳连接会把正在跑的会话的
// 通知整个抢走:daemon 侧推送「成功」,而发起会话的那条连接一条也收不到,没有错误、没有
// seq 跳号,会话永久卡住。
//
// 会话的推送目标必须是**发起该会话的那条连接**,同设备的其它连接接入不改变它。
func TestIntegration_SecondConnOfSameDeviceDoesNotStealSessionNotifications(t *testing.T) {
	gate := make(chan struct{})
	rig := bootGatedRig(t, &gatedBackendRunner{
		before: []agentruntime.Event{agentruntime.TextDelta{Text: "before"}},
		gate:   gate,
		after:  []agentruntime.Event{agentruntime.TextDelta{Text: "after"}, agentruntime.Done{}},
	})

	events, _ := rig.startRun(t, 800)
	awaitText(t, events, "before") // 会话确实在跑,且推送落在发起它的这条连接上

	rig.connectSameDevice(t) // 心跳连接接入并**留着**
	close(gate)

	awaitText(t, events, "after")
	_ = drainRuntimeEvents(t, events, 5*time.Second)
}

// TestIntegration_SameDeviceConnClosingDoesNotSuspendRunningSession 回归(评审在真环境
// 复现的其二):同指纹的第二条连接**关闭**时,它的清理把正在用的那条从表里一并删掉 ——
// 之后会话的通知只落库、推不出去,客户端永久收不到东西。撤销必须按连接身份做,
// 一条与会话无关的连接来去不得影响会话的推送目标。
func TestIntegration_SameDeviceConnClosingDoesNotSuspendRunningSession(t *testing.T) {
	gate := make(chan struct{})
	rig := bootGatedRig(t, &gatedBackendRunner{
		before: []agentruntime.Event{agentruntime.TextDelta{Text: "before"}},
		gate:   gate,
		after:  []agentruntime.Event{agentruntime.TextDelta{Text: "after"}, agentruntime.Done{}},
	})

	events, _ := rig.startRun(t, 801)
	awaitText(t, events, "before")

	second := rig.connectSameDevice(t)
	require.NoError(t, second.Close())
	// 等 daemon 真的处理完这条连接的关闭(bindConn 的 Done 监视是异步的),否则用例会
	// 在「删掉了正主」之前就放行事件,复现不出这个时序。
	require.Eventually(t, func() bool {
		rig.d.conns.mu.Lock()
		defer rig.d.conns.mu.Unlock()
		return len(rig.d.conns.live) == 1
	}, 5*time.Second, 10*time.Millisecond, "daemon must drop the closed connection from its live table")

	close(gate)

	awaitText(t, events, "after")
	_ = drainRuntimeEvents(t, events, 5*time.Second)
}

// TestIntegration_RejectedRuntimeCallDoesNotSeizeSessionOwnership 回归:接管的凭据是
// daemon **受理**了那条 runtime.*,不是「发出过」。认领跑在 handler 之前(runtime.run
// 一返回 fanout 就开始推,记晚了首批事件会丢),但 handler 拒了这一条时必须还原属主 ——
// 否则同指纹的另一条连接随便发一条会被拒的 runtime.*(会话 id 不存在于本 daemon、
// backend 不支持该能力、参数非法……)就能把正在跑的会话的推送整个抢过去:它不消费,
// 发起会话的那条从此一条也收不到,既没有错误也没有 seq 跳号 —— 正是本任务要消灭的那个
// 症状。
//
// 这里那条被拒的调用是「第二条连接对一条它从不拥有的会话发 runtime.abort」:第二条连接
// 的 bindConn 已经把 13 个 runtime.* 重新注册进共享 registry(rpc/registry.go 的 Register
// 是覆盖),所以 abort 派发到的是**它自己**那张空会话表的 RuntimeHandlers,解不出会话 →
// ErrNoActiveTurn。错误码一起钉住,免得哪天派发换了目标、拒绝的理由变了而用例照旧过。
func TestIntegration_RejectedRuntimeCallDoesNotSeizeSessionOwnership(t *testing.T) {
	gate := make(chan struct{})
	rig := bootGatedRig(t, &gatedBackendRunner{
		before: []agentruntime.Event{agentruntime.TextDelta{Text: "before"}},
		gate:   gate,
		after:  []agentruntime.Event{agentruntime.TextDelta{Text: "after"}, agentruntime.Done{}},
	})

	events, _ := rig.startRun(t, 802)
	awaitText(t, events, "before")

	second := rig.connectSameDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var res map[string]any
	abortErr := second.Call(ctx, wire.MethodAbort, map[string]any{"sessionId": 802}, &res)
	require.Error(t, abortErr,
		"第二条连接的 handler 从不拥有 802 —— daemon 必须拒了这一条")
	require.ErrorIs(t, wire.FromJSONRPCError(abortErr), agentruntime.ErrNoActiveTurn,
		"拒绝的理由就是「这个 handler 解不出会话」")

	close(gate)

	awaitText(t, events, "after")
	_ = drainRuntimeEvents(t, events, 5*time.Second)
}

// TestIntegration_SameDeviceHeartbeatDoesNotStealRuntimeHandler 回归:一台桌面端同时握着
// 连接池租约 / 设备监视心跳等多条同指纹连接时,后接入的连接有自己的私有 registry,
// 不能覆盖发起会话那条连接的 RuntimeHandlers。工具审批仍须由原连接真正送到 waiter。
func TestIntegration_SameDeviceHeartbeatDoesNotStealRuntimeHandler(t *testing.T) {
	gate := make(chan struct{})
	be := &approvalBackendRunner{
		before: []agentruntime.Event{agentruntime.TextDelta{Text: "before"}},
		gate:   gate,
		after:  []agentruntime.Event{agentruntime.TextDelta{Text: "after"}, agentruntime.Done{}},
	}
	rig := bootApprovalRig(t, be)

	// 会话跑在一条**带重挂能力**的桌面端 runtime 上(生产上 chat_svc 就这么接:
	// remote.New(lease.Client(), WithReconnect(...)))。连接自始至终是活的,重连端口
	// 只是让 callSession 的重挂重试生效 —— 真被调用就说明用例走错了路。
	rt := remote.New(rig.cli,
		remote.WithDaemonFingerprint(rpc.DaemonFingerprint(rig.d.state.DaemonInstanceUUID)),
		remote.WithReconnect(remote.ReconnectFunc(func(context.Context) (agentruntime.DaemonClientPort, string, error) {
			return nil, "", errors.New("连接一直是活的,这条用例不该触发重连")
		})),
	)
	t.Cleanup(func() { _ = rt.Close() })

	events, _ := rig.startRunOn(t, rt, 803)
	awaitText(t, events, "before") // 会话确实在跑,推送落在发起它的这条连接上

	// 设备监视心跳那条连接接入并留着；它的 bindConn 只改自己的私有 registry。
	rig.connectSameDevice(t)

	// 裸 RPC 仍走发起会话的连接，必须落到原 RuntimeHandlers 并真正回答 waiter。
	var ok wire.OK
	err := callRig(t, rig.cli, wire.MethodSubmitToolPermission,
		wire.SubmitToolPermissionParams{SessionID: 803, RequestID: "p-0", Allow: true}, &ok)
	require.NoError(t, err)
	assert.Equal(t, []string{"p-0"}, be.deliveredIDs(),
		"后接入的同设备连接不得偷走原连接的 runtime handler")

	// 真 *remote.Runtime 也沿用同一连接直接送达，不应触发重挂。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, rt.SubmitToolPermission(ctx, 803, "p-1", true, false, ""))
	assert.Equal(t, []string{"p-0", "p-1"}, be.deliveredIDs(),
		"桌面端报了成功,waiter 就必须真的被回答 —— 这两件事分家就是那个永久挂死")

	close(gate)

	awaitText(t, events, "after")
	_ = drainRuntimeEvents(t, events, 5*time.Second)
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

// TestIntegration_MCPReverseTunnel_NoDispatcher 覆盖 R17 的第三个失败点,也是唯一发生在
// **桌面端那一跳**的:连接仍然活着、隧道成功送达,失败在 desktop 本机重放那一步(未装配
// dispatcher)。它与 _NoTarget / _TargetLostMidCall 必须并存 —— 后两者失败在 daemon 侧,
// 合并任何一个都会让另一段全链路失去覆盖;尤其是「桌面端合成的应答经隧道原样回到 CLI」
// 这条,只有本例走全。
//
// 三个失败点对模型来说是同一件事(这次工具调用够不着发起端),所以答给 CLI 的形状也必须
// 一样:HTTP 200 + JSON-RPC error,而不是桌面端旧的
// `502 mcp proxy: desktop dispatcher unavailable` —— 那是 R17 禁止的裸非 2xx,只是产生在
// 更远的一跳上,daemon 原样透传后模型同样只拿到一句读不懂的基础设施错误。
func TestIntegration_MCPReverseTunnel_NoDispatcher(t *testing.T) {
	// 显式清空 dispatcher(remote 包进程级全局),并在测后保持清空。
	remote.RegisterMCPProxyDispatcher(nil)
	t.Cleanup(func() { remote.RegisterMCPProxyDispatcher(nil) })

	rig := bootRemoteRig(t, []agentruntime.Event{agentruntime.Done{}})
	base := rig.d.gateway.BaseURL()
	require.NotEmpty(t, base)

	httpReq, err := http.NewRequest(http.MethodPost, base+"/mcp/org/",
		strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"org_get"}}`))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)

	// 不是裸 502:HTTP 200 + JSON-RPC error,id 对得上,body 里装的是模型能读懂的话。
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	var rpcResp struct {
		ID    json.RawMessage `json:"id"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(respBody, &rpcResp))
	require.Equal(t, json.RawMessage("7"), rpcResp.ID)
	require.NotNil(t, rpcResp.Error, "must be a JSON-RPC error the LLM can read, not a bare transport failure")
	assert.Contains(t, rpcResp.Error.Message, "org")
	assert.Contains(t, rpcResp.Error.Message, "offline")
	assert.Contains(t, rpcResp.Error.Message, "do not retry")
	assert.NotContains(t, string(respBody), "mcp proxy:", "must not leak the old bare-502 wording")

	// 「不打挂连接」是本例名字里的承诺,直接验:同一条 RPC 连接此后仍可正常应答。
	var list wire.SessionListResult
	require.NoError(t, callRig(t, rig.cli, wire.MethodSessionList, nil, &list),
		"a tunnel-level tool error must not take the RPC connection down with it")
}

// TestIntegration_MCPReverseTunnel_NoTarget 覆盖 R17:发起会话的桌面端彻底断开(daemon
// 活连接表为空,tunnelTarget() 无目标可解)时,daemon 本机 /mcp/ 隧道入口不能把裸 503
// 答回 CLI 子进程的 MCP 客户端——非 2xx 状态码会让 MCP-over-HTTP 客户端把整个应答当传输层
// 故障丢弃,body 里的话模型永远读不到。必须换成 HTTP 200 包一个 JSON-RPC error,是 MCP
// 客户端读作"这次工具调用失败了"的那个形状,措辞点名哪个能力不可用、说明它依赖发起端在线、
// 且不要重试。
//
// 同时钉住 R4/R17 合在一起的效果:会话本身完全不受隧道无目标影响——断连发生在一轮执行
// 中途,隧道报错之后这一轮仍然照常跑完、落到 idle,而不是被这条路径顺手挂起或打断。
//
// 上面的 TestIntegration_MCPReverseTunnel_NoDispatcher 用一条**仍然存活**的连接、清空
// desktop 侧 dispatcher 来触发 502——那是 handleMCPProxy 自己的兜底,走的是隧道成功
// 送达之后 desktop 本机重放失败,和这里"隧道压根没有目标连接"是两个不同的失败点,
// 覆盖不了 R17;反过来本例也覆盖不了它,两个都得留着。
func TestIntegration_MCPReverseTunnel_NoTarget(t *testing.T) {
	gate := make(chan struct{})
	rig := bootGatedRig(t, &gatedBackendRunner{
		before: []agentruntime.Event{agentruntime.TextDelta{Text: "before"}},
		gate:   gate,
		after:  []agentruntime.Event{agentruntime.TextDelta{Text: "after"}, agentruntime.Done{}},
	})

	events, _ := rig.startRun(t, 950)
	awaitText(t, events, "before") // 会话确实在跑

	base := rig.d.gateway.BaseURL()
	require.NotEmpty(t, base)

	// 断开这台设备唯一的一条连接,并等 daemon 真的把它从活连接表里摘掉(bindConn 的
	// Done 监视是异步的)——否则隧道可能还打在一条将死未死的连接上,复现不出"无目标"。
	require.NoError(t, rig.cli.Close())
	require.Eventually(t, func() bool {
		rig.d.conns.mu.Lock()
		defer rig.d.conns.mu.Unlock()
		return len(rig.d.conns.live) == 0
	}, 5*time.Second, 10*time.Millisecond, "daemon must drop the only connection from its live table")

	// daemon 上的 CLI 子进程此刻调一个内置工具:隧道无目标。
	reqBody := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"org_get"}}`
	httpReq, err := http.NewRequest(http.MethodPost, base+"/mcp/org/", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)

	// 不是裸 503:HTTP 200 + JSON-RPC error,id 对得上,body 里装的是模型能读懂的话。
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var rpcResp struct {
		ID    json.RawMessage `json:"id"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(respBody, &rpcResp))
	require.Equal(t, json.RawMessage("7"), rpcResp.ID)
	require.NotNil(t, rpcResp.Error, "must be a JSON-RPC error the LLM can read, not a bare transport failure")
	assert.Contains(t, rpcResp.Error.Message, "org")
	assert.Contains(t, rpcResp.Error.Message, "offline")
	assert.Contains(t, rpcResp.Error.Message, "do not retry")
	assert.NotContains(t, string(respBody), "503", "must not leak the old bare-503 wording")

	// 会话不因为隧道报错而中止:放行剩下的事件,这一轮必须照常跑完。
	close(gate)

	// 此刻用于 startRun 的那条连接已经关了(remote.Runtime 的断连 UX 会给它注
	// ErrDaemonDisconnected 并 close events——这与"daemon 侧会话有没有继续跑"是两回事),
	// 所以改用同设备的新连接把这条会话的日志拉出来,直接验 daemon 侧的事实:这一轮必须
	// 落到 idle,而不是卡在 running / 被打断。
	second := rig.connectSameDevice(t)
	require.Eventually(t, func() bool {
		var list wire.SessionListResult
		require.NoError(t, callRig(t, second, wire.MethodSessionList, nil, &list))
		for _, s := range list.Sessions {
			if s.SessionID == 950 {
				return s.LifecycleState == wire.SessionLifecycleIdle
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond, "session must reach idle after the tunnel error, not be aborted or left running")
}

// TestIntegration_MCPReverseTunnel_TargetLostMidCall 覆盖 R17 的另一个失败点:目标
// 在隧道调用**途中**没了。请求已经经 WS 送到桌面端,桌面端还没答就死了(这里用它在
// dispatcher 里关掉自己的连接来复现进程被杀 / 链路被 RST),daemon 侧等应答的那次
// Call 拿到 rpc.ErrConnClosed。
//
// 它与上面两个用例是第三个失败点,三个都得留着:_NoDispatcher 是隧道成功送达、桌面端
// 本机重放失败(状态码装在 MCPProxyResponse 里原路回流);_NoTarget 是发请求之前就解不
// 出目标;本例是解出了目标、请求也发出去了,回程才断。对模型来说后两者是同一件事
// ——发起端不在线——所以答给 CLI 的形状必须一样:HTTP 200 + JSON-RPC error,而不是真机
// 上复现到的 `502 text/plain: mcp tunnel: rpc: connection closed`。
func TestIntegration_MCPReverseTunnel_TargetLostMidCall(t *testing.T) {
	var (
		rig   *pairedTestRig
		ready = make(chan struct{})
	)
	// 桌面端在收到隧道请求后、答复之前死掉:关掉自己这条连接,应答永远回不去。
	remote.RegisterMCPProxyDispatcher(func(_ context.Context, _ wire.MCPProxyRequest) (wire.MCPProxyResponse, error) {
		<-ready
		_ = rig.cli.Close()
		return wire.MCPProxyResponse{}, errors.New("desktop died mid-call")
	})
	t.Cleanup(func() { remote.RegisterMCPProxyDispatcher(nil) })

	rig = bootRemoteRig(t, []agentruntime.Event{agentruntime.Done{}})
	close(ready)

	base := rig.d.gateway.BaseURL()
	require.NotEmpty(t, base)

	reqBody := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"org_get"}}`
	httpReq, err := http.NewRequest(http.MethodPost, base+"/mcp/org/", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)

	// 不是裸 502:HTTP 200 + JSON-RPC error,id 对得上,body 里装的是模型能读懂的话。
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var rpcResp struct {
		ID    json.RawMessage `json:"id"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(respBody, &rpcResp))
	require.Equal(t, json.RawMessage("7"), rpcResp.ID)
	require.NotNil(t, rpcResp.Error, "must be a JSON-RPC error the LLM can read, not a bare transport failure")
	assert.Contains(t, rpcResp.Error.Message, "org")
	assert.Contains(t, rpcResp.Error.Message, "offline")
	assert.Contains(t, rpcResp.Error.Message, "do not retry")
	assert.NotContains(t, string(respBody), "rpc: connection closed",
		"传输层错误原文不该糊到模型脸上")
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

// ── 断连重连的补齐族(会话清单 / 增量拉取 / 待决策查询 / 显式接管)──────────
//
// 真 daemon + 真 ws + 真 client,先例 TestIntegration_RemoteRuntime_EventRoundTrip。
// 这一族是客户端重连后的第一站,所以每个用例都以「客户端视角能不能靠它把自己接回来」
// 为断言,而不是内部状态。

// callRig 在 rig 的那条已配对连接上发一次 RPC。
func callRig(t *testing.T, cli *client.Client, method string, params, result any) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return cli.Call(ctx, method, params, result)
}

// pairSecondDevice 再配对一台**不同指纹**的设备,并返回它自己的连接。R16 的范围断言
// 需要一个真正的第二对端 —— 同指纹的第二条连接(connectSameDevice)是同一个对端。
func pairSecondDevice(t *testing.T, d *Daemon, fingerprint string) *client.Client {
	t.Helper()
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
		"deviceName":        "other-mac",
		"deviceFingerprint": fingerprint,
	}, &pairResp))
	require.NotEmpty(t, pairResp.DeviceToken)
	return cli
}

// TestIntegration_SessionCatchup_ListAndPullReplayTheWholeTurn 覆盖补齐的主路径:
// 跑完一轮后,客户端能列出这条会话(带生命周期状态与最新 seq)、并按游标把这一轮发出去
// 的每一条通知按 seq 升序**逐条**重放出来 —— 补齐路径与实时路径投递的是同一批
// (method, params),这是 R5 等价性在结构上成立的前提。
//
// 同时钉住三条翻页边界:起始游标 0、每页按 limit 截断且 hasMore 为真、以及起始游标
// 追平最新 seq 后返回空页且**游标不回退**(回退会让客户端把整段日志重放一遍)。
func TestIntegration_SessionCatchup_ListAndPullReplayTheWholeTurn(t *testing.T) {
	rig := bootRemoteRig(t, []agentruntime.Event{
		agentruntime.TextDelta{Text: "hello"},
		agentruntime.TextDelta{Text: " world"},
		agentruntime.Done{},
	})
	events, _ := rig.startRun(t, 900)
	_ = drainRuntimeEvents(t, events, 5*time.Second)

	// 一轮 = 3 条 runtime.event(两条 TextDelta + 一条 Done)+ 1 条 runResultDone。
	const wantTotal = 4

	var list wire.SessionListResult
	require.NoError(t, callRig(t, rig.cli, wire.MethodSessionList, nil, &list))
	require.Len(t, list.Sessions, 1, "跑过一轮的会话必须出现在清单里")
	got := list.Sessions[0]
	assert.Equal(t, int64(900), got.SessionID)
	assert.Equal(t, string(agent_backend_entity.TypeClaudeCode), got.BackendType)
	assert.Equal(t, wire.SessionLifecycleIdle, got.LifecycleState, "轮结束后会话等待下一轮")
	assert.False(t, got.WaitingForInput)
	assert.Equal(t, int64(wantTotal), got.LatestSeq, "最新 seq 取自通知日志的 MAX(seq)")

	// 按 limit=2 翻页拉平,把每一页的 seq / method 串起来。
	var (
		seqs    []int64
		methods []string
		cursor  int64
		pages   int
	)
	for {
		var page wire.SessionPullResult
		require.NoError(t, callRig(t, rig.cli, wire.MethodSessionPull,
			wire.SessionPullParams{SessionID: 900, Cursor: cursor, Limit: 2}, &page))
		pages++
		require.LessOrEqual(t, len(page.Notifications), 2, "单页条数必须被 limit 截断")
		for _, n := range page.Notifications {
			seqs = append(seqs, n.Seq)
			methods = append(methods, n.Method)
			require.NotEmpty(t, n.Params, "日志行必须带上那条通知的 params 原样")
		}
		require.Greater(t, page.Cursor, cursor-1)
		cursor = page.Cursor
		if !page.HasMore {
			break
		}
		require.Less(t, pages, 10, "翻页没有收敛")
	}
	assert.Equal(t, []int64{1, 2, 3, 4}, seqs, "seq 必须从 1 起单调无洞")
	assert.Equal(t, []string{
		wire.NotifyEvent, wire.NotifyEvent, wire.NotifyEvent, wire.NotifyRunResultDone,
	}, methods, "补齐重放的就是那一轮本该发出的通知本身")
	assert.Equal(t, 2, pages, "4 条 / 每页 2 条 = 2 页")
	assert.Equal(t, int64(wantTotal), cursor)

	// 游标已追平最新 seq:空页,游标保持不变。
	var tail wire.SessionPullResult
	require.NoError(t, callRig(t, rig.cli, wire.MethodSessionPull,
		wire.SessionPullParams{SessionID: 900, Cursor: cursor}, &tail))
	assert.Empty(t, tail.Notifications)
	assert.False(t, tail.HasMore)
	assert.Equal(t, cursor, tail.Cursor, "空页不得把游标回退")

	// 起始游标大于最新 seq(客户端游标来自另一台 daemon 实例时会这样)同样是空页。
	var past wire.SessionPullResult
	require.NoError(t, callRig(t, rig.cli, wire.MethodSessionPull,
		wire.SessionPullParams{SessionID: 900, Cursor: 9999}, &past))
	assert.Empty(t, past.Notifications)
	assert.False(t, past.HasMore)
	assert.Equal(t, int64(9999), past.Cursor)

	// 待决策查询:这个 backend 不实现审批协议,必须回空列表而不是报错(R7)。
	var waiters wire.SessionPendingWaitersResult
	require.NoError(t, callRig(t, rig.cli, wire.MethodSessionPendingWaiters,
		wire.SessionPendingWaitersParams{SessionID: 900}, &waiters))
	assert.Empty(t, waiters.ToolPermissions)
	assert.Empty(t, waiters.AskUserQuestions)
}

// TestIntegration_SessionCatchup_AttachRepointsTheLiveStream 覆盖**显式接管**:
// 补齐族不叫 runtime.*,也不走 trackSessionOwner 的隐式认领,所以只做补齐并不会让重连
// 后的新连接成为推送目标 —— 补完仍是挂起态,实时流不恢复。runtime.session.attach 就是
// 那个显式入口:调用它之后,这条会话此后的通知推给发起接管的那条连接。
//
// 这里用同一台设备的第二条连接扮演「重连后的新连接」:接管前事件落在原连接上,接管后
// 的事件必须改落在新连接上。
func TestIntegration_SessionCatchup_AttachRepointsTheLiveStream(t *testing.T) {
	gate := make(chan struct{})
	rig := bootGatedRig(t, &gatedBackendRunner{
		before: []agentruntime.Event{agentruntime.TextDelta{Text: "before"}},
		gate:   gate,
		after:  []agentruntime.Event{agentruntime.TextDelta{Text: "after"}, agentruntime.Done{}},
	})

	events, _ := rig.startRun(t, 901)
	awaitText(t, events, "before") // 推送此刻落在发起会话的那条连接上

	// 「重连后的新连接」:同一台设备,自己订阅 runtime.event。
	second := rig.connectSameDevice(t)
	frames := make(chan wire.EventFrame, 16)
	second.Handle(wire.NotifyEvent, func(_ context.Context, p json.RawMessage) (any, error) {
		var f wire.EventFrame
		if err := json.Unmarshal(p, &f); err == nil {
			select {
			case frames <- f:
			default:
			}
		}
		return nil, nil
	})
	turnDone := make(chan struct{}, 1)
	second.Handle(wire.NotifyRunResultDone, func(_ context.Context, _ json.RawMessage) (any, error) {
		select {
		case turnDone <- struct{}{}:
		default:
		}
		return nil, nil
	})

	var attached wire.SessionAttachResult
	require.NoError(t, callRig(t, second, wire.MethodSessionAttach,
		wire.SessionAttachParams{SessionID: 901}, &attached))
	assert.Equal(t, int64(901), attached.SessionID)
	assert.Equal(t, wire.SessionLifecycleRunning, attached.LifecycleState, "一轮还在跑")
	assert.Positive(t, attached.LatestSeq, "接管要交回此刻的高水位供客户端接着补齐")

	close(gate)

	deadline := time.After(5 * time.Second)
	var sawAfter bool
	for !sawAfter {
		select {
		case f := <-frames:
			if strings.Contains(string(f.Event), "after") {
				assert.Positive(t, f.Seq, "推出去的帧必须带 seq")
				sawAfter = true
			}
		case <-deadline:
			t.Fatal("显式接管后的通知没有推给接管的那条连接 —— 实时流没有恢复")
		}
	}
	// 等这一轮的终态帧再收尾:fanout goroutine 活过用例会让它在 t.Cleanup 删掉数据目录
	// 之后还往库里写(表现为 readonly database),并与下一个用例构造 Daemon 撞上。
	select {
	case <-turnDone:
	case <-time.After(5 * time.Second):
		t.Fatal("接管后没有收到这一轮的终态帧")
	}
}

// TestIntegration_SessionCatchup_AttachRestoresControlOnTheNewConnection 覆盖显式接管
// 的另一半:接管之后,这条连接对该会话的**控制** RPC 也必须重新可用。
//
// RuntimeHandlers 是 per-connection 构造的,重连后拿到的是一张内存会话表为空的新
// handler。不认下会话的话,客户端刚补齐完、正要回答断连期间产生的那条待决策时,
// submitToolPermission 会解不出会话、再被 R8 的幂等折成「成功」—— waiter 没人回答、
// 客户端以为答过了,叠加 R9 的不设过期就是永久挂死。这里用 runtime.abort 当探针:
// 它在同样的 resolveSession 上,但会**如实报错**而不是被折成成功,所以测得出来。
func TestIntegration_SessionCatchup_AttachRestoresControlOnTheNewConnection(t *testing.T) {
	rig := bootRemoteRig(t, []agentruntime.Event{
		agentruntime.TextDelta{Text: "hello"},
		agentruntime.Done{},
	})
	events, _ := rig.startRun(t, 904)
	_ = drainRuntimeEvents(t, events, 5*time.Second)

	second := rig.connectSameDevice(t)

	var ok wire.OK
	require.Error(t,
		callRig(t, second, wire.MethodAbort, wire.AbortParams{SessionID: 904}, &ok),
		"接管之前,新连接的 handler 不认识这条会话")

	var attached wire.SessionAttachResult
	require.NoError(t, callRig(t, second, wire.MethodSessionAttach,
		wire.SessionAttachParams{SessionID: 904}, &attached))

	require.NoError(t,
		callRig(t, second, wire.MethodAbort, wire.AbortParams{SessionID: 904}, &ok),
		"接管之后,控制 RPC 必须解得出会话并真的打到 backend")
}

// TestIntegration_SessionCatchup_ScopedToTheCallersPeer 覆盖 R16:查询与拉取一律限定
// 在调用方自己的对端范围内。第二台**不同指纹**的已配对设备既看不到、也拉不到、更接管
// 不了第一台的会话 —— 本规格阶段 daemon 只认 LAN 配对、没有账号概念,多个配对对端不
// 保证属于同一个人。
func TestIntegration_SessionCatchup_ScopedToTheCallersPeer(t *testing.T) {
	rig := bootRemoteRig(t, []agentruntime.Event{
		agentruntime.TextDelta{Text: "secret"},
		agentruntime.Done{},
	})
	events, _ := rig.startRun(t, 902)
	_ = drainRuntimeEvents(t, events, 5*time.Second)

	// 正主看得见。
	var mine wire.SessionListResult
	require.NoError(t, callRig(t, rig.cli, wire.MethodSessionList, nil, &mine))
	require.Len(t, mine.Sessions, 1)

	other := pairSecondDevice(t, rig.d, "sha256:other-device")

	var theirs wire.SessionListResult
	require.NoError(t, callRig(t, other, wire.MethodSessionList, nil, &theirs))
	assert.Empty(t, theirs.Sessions, "另一个对端不得看见别人的会话")

	var page wire.SessionPullResult
	require.NoError(t, callRig(t, other, wire.MethodSessionPull,
		wire.SessionPullParams{SessionID: 902}, &page))
	assert.Empty(t, page.Notifications, "另一个对端点名拉同一个会话 id 也拉不到内容")

	var waiters wire.SessionPendingWaitersResult
	require.NoError(t, callRig(t, other, wire.MethodSessionPendingWaiters,
		wire.SessionPendingWaitersParams{SessionID: 902}, &waiters))
	assert.Empty(t, waiters.ToolPermissions)
	assert.Empty(t, waiters.AskUserQuestions)

	var attached wire.SessionAttachResult
	err := callRig(t, other, wire.MethodSessionAttach, wire.SessionAttachParams{SessionID: 902}, &attached)
	require.Error(t, err, "接管改的是通知推给谁 —— 跨对端接管等于把别人的事件流引到自己连接上")
	var rpcErr *rpc.Error
	require.True(t, errors.As(err, &rpcErr))
	assert.Equal(t, wire.ErrCodeSessionNotFound, rpcErr.Code)
}

// TestIntegration_SessionCatchup_PendingWaitersNeverCrossPeersOnTheSameSessionID
// 覆盖 R16 里最难的那半:两个对端**各自持有同一个本地会话 id**。
//
// 会话 id 是各客户端本地自增的主键,两台设备的 42 号会话是两条毫不相干的会话。日志与
// 游标已经按 (对端, 会话) 复合键存放,所以 List / Pull 天然隔离;待决策不在库里,它挂在
// backend runtime 的内存里、只按会话 id 索引 —— 于是「按对端限定了行,再拿裸数字去问
// backend」就成了一条跨对端的信息泄漏:
//
//   - 对端 A 只要自己也有一行 42(自己早先跑过就够),就能读到对端 B 那条正在跑的 42
//     号会话的 requestID、工具名与**完整工具入参**;
//   - 还能照着那个 requestID 替 B 提交审批 —— B 的子进程会当成机主本人点的允许。
//
// 所以这里两条断言缺一不可:A 查不到 B 的待决策,且 A 提交了也不会有任何 waiter 被回答。
// 最后一段反过来钉住正主仍然答得了自己的那条 —— 把所有人都挡掉同样能让前两条通过。
func TestIntegration_SessionCatchup_PendingWaitersNeverCrossPeersOnTheSameSessionID(t *testing.T) {
	const sharedSID = 42

	gate := make(chan struct{})
	runner := newKeyedApprovalRunner(gate)
	rig := bootKeyedApprovalRig(t, runner)

	// 另一台**不同指纹**的已配对设备,先跑完自己的 42 号会话 —— 它因此有了一行 42,
	// 但没有任何待决策。这正是泄漏的前提:findOwnSession 查得到行,于是继续去问 backend。
	other := pairSecondDevice(t, rig.d, "sha256:other-device")
	startRunAs(t, other, rig.dir, sharedSID, noApprovalText)
	awaitLifecycle(t, other, sharedSID, wire.SessionLifecycleIdle)

	// 正主的 42 号会话此刻正卡在一条工具审批上。
	events, _ := rig.startRun(t, sharedSID)
	awaitText(t, events, "blocked")
	require.Eventually(t, func() bool { return runner.waiterCount() == 1 },
		5*time.Second, 20*time.Millisecond, "正主那条会话应当卡在审批上")

	var mine wire.SessionPendingWaitersResult
	require.NoError(t, callRig(t, rig.cli, wire.MethodSessionPendingWaiters,
		wire.SessionPendingWaitersParams{SessionID: sharedSID}, &mine))
	require.Len(t, mine.ToolPermissions, 1, "正主必须查得到自己那条待决策")

	// ① 查询:另一个对端拿不到别人的 requestID / 工具名 / 工具入参。
	var theirs wire.SessionPendingWaitersResult
	require.NoError(t, callRig(t, other, wire.MethodSessionPendingWaiters,
		wire.SessionPendingWaitersParams{SessionID: sharedSID}, &theirs))
	assert.Empty(t, theirs.ToolPermissions,
		"另一个对端同号会话的待决策查询泄漏了别人的审批载荷")
	assert.Empty(t, theirs.AskUserQuestions)

	// ② 提交:另一个对端替不了别人答。daemon 侧按 R8 一律回成功(重连的客户端分不清
	// 自己上一次提交到没到),所以判据只能是「backend 那边有没有 waiter 真被回答」。
	var ok wire.OK
	require.NoError(t, callRig(t, other, wire.MethodSubmitToolPermission,
		wire.SubmitToolPermissionParams{
			SessionID: sharedSID, RequestID: "req-of-the-owner", Allow: true,
		}, &ok))
	assert.Empty(t, runner.deliveredIDs(),
		"另一个对端替正主提交了审批 —— 正主的子进程会把它当成机主本人点的允许")

	// ③ 正主自己仍然答得了:隔离不是把所有人都挡掉。
	require.NoError(t, callRig(t, rig.cli, wire.MethodSubmitToolPermission,
		wire.SubmitToolPermissionParams{
			SessionID: sharedSID, RequestID: "req-of-the-owner", Allow: true,
		}, &ok))
	assert.Equal(t, []string{"req-of-the-owner"}, runner.deliveredIDs(),
		"正主对自己那条会话的提交必须真的送达 backend")

	close(gate)
	_ = drainRuntimeEvents(t, events, 5*time.Second)
}

// TestIntegration_SessionCatchup_DaemonRestartMarksSessionsInterrupted 覆盖 R10:
// daemon 启动时把库里全部非终态会话标记为已中断。中断态会话的**历史仍可读**(客户端
// 靠它把断连前的转录补完),但接不回实时流 —— 那一轮的子进程随上一个 daemon 进程消亡了,
// 接管它等于让客户端对着一条永远不会再产出任何东西的会话无限期等下去。
func TestIntegration_SessionCatchup_DaemonRestartMarksSessionsInterrupted(t *testing.T) {
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode,
		&pacedBackendRunner{events: []agentruntime.Event{
			agentruntime.TextDelta{Text: "hello"},
			agentruntime.Done{},
		}})
	t.Cleanup(restore)

	dir, err := os.MkdirTemp("", "ard-restart")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	// 第一台 daemon:跑一轮,留下一条非终态(idle)会话与它的通知日志。
	first := bootRigInDir(t, dir)
	events, _ := first.startRun(t, 903)
	_ = drainRuntimeEvents(t, events, 5*time.Second)

	var before wire.SessionListResult
	require.NoError(t, callRig(t, first.cli, wire.MethodSessionList, nil, &before))
	require.Len(t, before.Sessions, 1)
	require.Equal(t, wire.SessionLifecycleIdle, before.Sessions[0].LifecycleState)
	wantSeq := before.Sessions[0].LatestSeq
	require.Positive(t, wantSeq)

	first.stop()

	// 第二台 daemon:同一个数据目录 = 同一个库。
	second := bootRigInDir(t, dir)

	var after wire.SessionListResult
	require.NoError(t, callRig(t, second.cli, wire.MethodSessionList, nil, &after))
	require.Len(t, after.Sessions, 1, "重启不该让会话从清单里消失 —— 它的历史还在")
	assert.Equal(t, wire.SessionLifecycleInterrupted, after.Sessions[0].LifecycleState)
	assert.False(t, after.Sessions[0].WaitingForInput, "等待输入是实时叠加,重启后无人可答")
	assert.Equal(t, wantSeq, after.Sessions[0].LatestSeq, "日志与 seq 都不因重启而变")

	// 历史可读。
	var page wire.SessionPullResult
	require.NoError(t, callRig(t, second.cli, wire.MethodSessionPull,
		wire.SessionPullParams{SessionID: 903}, &page))
	assert.Len(t, page.Notifications, int(wantSeq), "中断态会话的历史必须照样拉得出来")

	// 不可续跑。
	var attached wire.SessionAttachResult
	err = callRig(t, second.cli, wire.MethodSessionAttach, wire.SessionAttachParams{SessionID: 903}, &attached)
	require.Error(t, err, "中断态会话不可续跑")
	var rpcErr *rpc.Error
	require.True(t, errors.As(err, &rpcErr))
	assert.Equal(t, wire.ErrCodeNoActiveTurn, rpcErr.Code)
}

// ── 断连补齐:硬不变量 ──────────────────────────────────────────────────────

// recordedNotify 是客户端**实际拿到**的一条通知:方法、它在 daemon 通知日志里的
// 序号、以及剥掉 seq 之后的规范化载荷。实时推送与补齐拉取都归一到这个形状,
// 两条路径因此可以逐条比对。
type recordedNotify struct {
	Method  string
	Seq     int64
	Payload string
}

// notifyRecorder 收集一次运行里客户端拿到的全部通知。
type notifyRecorder struct {
	mu  sync.Mutex
	got []recordedNotify
}

// recordedNotifyMethods 是 daemon → client 的五类通知。
var recordedNotifyMethods = map[string]struct{}{
	wire.NotifyEvent:                 {},
	wire.NotifyRunResultDone:         {},
	wire.NotifyAutonomousTurnStarted: {},
	wire.NotifyAutonomousTurnEvent:   {},
	wire.NotifyAutonomousTurnDone:    {},
}

func (r *notifyRecorder) observeLive(t *testing.T, method string, raw json.RawMessage) {
	seq, payload := splitSeq(t, raw)
	r.add(recordedNotify{Method: method, Seq: seq, Payload: payload})
}

func (r *notifyRecorder) observePulled(t *testing.T, ns []wire.JournaledNotification) {
	for _, n := range ns {
		_, payload := splitSeq(t, n.Params)
		r.add(recordedNotify{Method: n.Method, Seq: n.Seq, Payload: payload})
	}
}

func (r *notifyRecorder) add(n recordedNotify) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, n)
}

// ordered 按 seq 升序去重后交出这次运行客户端**获得**的通知序列。去重是必要的:
// 补齐期间 daemon 可能把同一条既推过来又在 pull 里带出来,R6 要求客户端丢弃后者;
// 「同一条不被投递两次」由事件流的逐条相等去证(见用例末尾)。
func (r *notifyRecorder) ordered() []recordedNotify {
	r.mu.Lock()
	defer r.mu.Unlock()
	bySeq := map[int64]recordedNotify{}
	seqs := make([]int64, 0, len(r.got))
	for _, n := range r.got {
		if n.Seq == 0 {
			continue
		}
		if _, dup := bySeq[n.Seq]; dup {
			continue
		}
		bySeq[n.Seq] = n
		seqs = append(seqs, n.Seq)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	out := make([]recordedNotify, 0, len(seqs))
	for _, s := range seqs {
		out = append(out, bySeq[s])
	}
	return out
}

// splitSeq 把一帧拆成 (seq, 剥掉 seq 的规范化载荷)。实时帧带 seq、日志载荷不带,
// 归一后才能逐条比对同一条通知的字节。
func splitSeq(t *testing.T, raw json.RawMessage) (int64, string) {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	var seq int64
	if v, ok := m["seq"].(float64); ok {
		seq = int64(v)
	}
	delete(m, "seq")
	canonicalJSON, err := json.Marshal(m)
	require.NoError(t, err)
	return seq, string(canonicalJSON)
}

// recordingClient 在客户端这一侧记账:它包住真 *client.Client,拦下五类通知的
// handler(实时路径)与 runtime.session.pull 的应答(补齐路径)。记账点在
// *remote.Runtime 之外,所以它记的就是「客户端拿到了什么」,不掺实现细节。
type recordingClient struct {
	agentruntime.DaemonClientPort
	t   *testing.T
	rec *notifyRecorder
}

func (c *recordingClient) Handle(method string, fn func(context.Context, json.RawMessage) (any, error)) {
	if _, ok := recordedNotifyMethods[method]; ok {
		inner := fn
		fn = func(ctx context.Context, raw json.RawMessage) (any, error) {
			c.rec.observeLive(c.t, method, raw)
			return inner(ctx, raw)
		}
	}
	c.DaemonClientPort.Handle(method, fn)
}

func (c *recordingClient) Call(ctx context.Context, method string, params, result any) error {
	err := c.DaemonClientPort.Call(ctx, method, params, result)
	if err == nil && method == wire.MethodSessionPull {
		if res, ok := result.(*wire.SessionPullResult); ok {
			c.rec.observePulled(c.t, res.Notifications)
		}
	}
	return err
}

// memCursor 是桌面端游标端口的内存替身:daemon 包里没有 chat_sessions,
// 而本用例要验的是「按游标补齐」的行为,不是它存在哪一列。
type memCursor struct {
	mu   sync.Mutex
	fp   string
	seqs map[int64]int64
}

func newMemCursor(fp string) *memCursor {
	return &memCursor{fp: fp, seqs: map[int64]int64{}}
}

func (m *memCursor) LoadCursor(_ context.Context, sessionID int64, fp string) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fp != m.fp {
		// R12:实例标识对不上就判游标失效。
		return 0, false, nil
	}
	return m.seqs[sessionID], true, nil
}

func (m *memCursor) SaveCursor(_ context.Context, sessionID int64, fp string, seq int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fp != m.fp {
		return nil
	}
	if seq > m.seqs[sessionID] {
		m.seqs[sessionID] = seq
	}
	return nil
}

// durableRunner 在 rig 上造一个**带重连能力**的 *remote.Runtime:记账客户端 +
// 重连端口(重新 auth.connect 一条同设备连接,与真桌面端连接池重拨走的是同一条路)。
func (r *pairedTestRig) durableRunner(t *testing.T, rec *notifyRecorder, gate <-chan struct{}, states *connStateLog) *remote.Runtime {
	t.Helper()
	fp := rpc.DaemonFingerprint(r.d.state.DaemonInstanceUUID)
	rt := remote.New(
		&recordingClient{DaemonClientPort: r.cli, t: t, rec: rec},
		remote.WithDaemonFingerprint(fp),
		remote.WithSessionCursor(newMemCursor(fp)),
		remote.WithCursorFlushInterval(0),
		remote.WithConnStateObserver(states),
		remote.WithReconnectBackoff([]time.Duration{
			10 * time.Millisecond, 50 * time.Millisecond, 200 * time.Millisecond, time.Second,
		}),
		remote.WithReconnect(remote.ReconnectFunc(func(context.Context) (agentruntime.DaemonClientPort, string, error) {
			// gate 让用例决定「什么时候才允许重连」。用它把重连推到 daemon 把整轮
			// 都落完库之后,补齐就必然经 runtime.session.pull 拿回来 —— 否则重连
			// 恰好赶在事件产生之前,一切照旧走实时推送,这个用例就什么也没验到。
			if gate != nil {
				<-gate
			}
			return &recordingClient{DaemonClientPort: r.connectSameDevice(t), t: t, rec: rec}, fp, nil
		})),
	)
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

func (r *pairedTestRig) startRunOn(t *testing.T, rt *remote.Runtime, sid int64) (<-chan agentruntime.Event, *agentruntime.RunResult) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	events, result, err := rt.Run(ctx, agentruntime.RunRequest{
		Backend: &agent_backend_entity.AgentBackend{
			Type: string(agent_backend_entity.TypeClaudeCode), ID: 1, Name: "test-backend",
		},
		AgentID: 1, SessionID: sid, Cwd: r.dir, UserText: "hi",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	return events, result
}

// scriptPhase 一个阶段:先等 gate(nil 不等),再把 events 逐条送进事件流。
type scriptPhase struct {
	gate   <-chan struct{}
	events []agentruntime.Event
}

// phasedBackendRunner 让用例精确控制「哪些事件在断连期间产生、哪些在补齐之后产生」。
type phasedBackendRunner struct {
	phases []scriptPhase
}

func (*phasedBackendRunner) Capabilities() capability.Capabilities { return capability.Capabilities{} }

func (p *phasedBackendRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event, 1)
	go func() {
		defer close(ch)
		for _, ph := range p.phases {
			if ph.gate != nil {
				<-ph.gate
			}
			for _, ev := range ph.events {
				ch <- ev
				time.Sleep(5 * time.Millisecond) // 让 fanout 先把这一帧冲出去
			}
		}
	}()
	return ch, &agentruntime.RunResult{}, nil
}

// bootPhasedRig 起一个 rig 并把 backend 换成阶段脚本。
func bootPhasedRig(t *testing.T, r *phasedBackendRunner) *pairedTestRig {
	t.Helper()
	rig := bootRemoteRig(t, []agentruntime.Event{agentruntime.Done{}})
	// bootRemoteRig 自己也换过一次 backend;t.Cleanup 是后进先出,这里的还原先跑。
	t.Cleanup(agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, r))
	return rig
}

// awaitJournalDepth 等 daemon 的通知日志攒够 want 条。读的是 daemon 自己的库,
// 与补齐 RPC 同源。
func awaitJournalDepth(t *testing.T, r *pairedTestRig, sessionID, want int64) {
	t.Helper()
	reader := journalReader{db: r.d.db}
	require.Eventually(t, func() bool {
		latest, err := reader.LatestSeq(context.Background(), rigDeviceFingerprint,
			strconv.FormatInt(sessionID, 10))
		return err == nil && latest >= want
	}, 10*time.Second, 10*time.Millisecond, "daemon 应在断连期间照常落库")
}

// awaitTextCollecting 等下一条指定文本的 TextDelta,并把这期间收下的事件原样交回
// (它们已经离开 channel,不收回来后面的逐条比对就会凭空少几条)。
func awaitTextCollecting(t *testing.T, events <-chan agentruntime.Event, want string) []agentruntime.Event {
	t.Helper()
	var got []agentruntime.Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("events channel closed while waiting for %q", want)
			}
			got = append(got, ev)
			if td, isText := ev.(agentruntime.TextDelta); isText && td.Text == want {
				return got
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}

// connStateLog 收集会话级连接态。用例靠它知道「补齐已经落定、回到实时了」,
// 也顺带证明这条信号真的传得到上层(前端那一步要拿它渲染断连指示器)。
type connStateLog struct {
	mu  sync.Mutex
	got map[int64][]remote.ConnState
}

func newConnStateLog() *connStateLog { return &connStateLog{got: map[int64][]remote.ConnState{}} }

func (c *connStateLog) OnSessionConnState(sessionID int64, st remote.SessionConnState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got[sessionID] = append(c.got[sessionID], st.State)
}

func (c *connStateLog) sawAfterReconnect(sessionID int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := c.got[sessionID]
	for i := 1; i < len(seen); i++ {
		if seen[i-1] == remote.ConnStateReconnecting && seen[i] == remote.ConnStateConnected {
			return true
		}
	}
	return false
}

// TestIntegration_ReconnectCatchUp_MatchesUninterruptedRun 是本规格的**硬不变量**:
// 同一次会话执行,「中途掐断连接、隔一会儿重连补齐」最终拿到的通知序列,必须与
// 「全程不断连」逐条相等 —— method 相同、载荷字节相同、顺序相同、无重复、无遗漏。
//
// 真 daemon + 真 WebSocket + 真 *remote.Runtime。两次运行跑同一段三阶段脚本,唯一
// 区别是第二次在第一阶段之后 Close 掉那条 ws,并把重连推迟到 daemon 把第二阶段全部
// 落完库之后 —— 断连期间那几条因此只可能从 runtime.session.pull 回来。第三阶段在
// 补齐落定**之后**才产生,用来验证会话确实回到了实时推送,而不是补完就哑了。
//
// 记账点在 *remote.Runtime 之外的客户端侧:实时路径记 handler 收到的帧,补齐路径记
// runtime.session.pull 的应答,两边都剥掉 seq 后规范化,因此可以逐条比对。
func TestIntegration_ReconnectCatchUp_MatchesUninterruptedRun(t *testing.T) {
	const sid int64 = 100
	phase1 := []agentruntime.Event{agentruntime.TextDelta{Text: "one"}, agentruntime.TextDelta{Text: "two"}}
	phase2 := []agentruntime.Event{agentruntime.TextDelta{Text: "three"}, agentruntime.TextDelta{Text: "four"}}
	phase3 := []agentruntime.Event{agentruntime.TextDelta{Text: "five"}, agentruntime.Done{}}

	var baseline []recordedNotify
	var baselineEvents []agentruntime.Event
	var baselineResult agentruntime.RunResult

	t.Run("uninterrupted", func(t *testing.T) {
		g1, g2 := make(chan struct{}), make(chan struct{})
		close(g1)
		close(g2)
		rig := bootPhasedRig(t, &phasedBackendRunner{phases: []scriptPhase{
			{events: phase1}, {gate: g1, events: phase2}, {gate: g2, events: phase3},
		}})
		rec := &notifyRecorder{}
		rt := rig.durableRunner(t, rec, nil, newConnStateLog())
		events, result := rig.startRunOn(t, rt, sid)

		baselineEvents = drainRuntimeEvents(t, events, 10*time.Second)
		baselineResult = *result
		baseline = rec.ordered()
		require.NotEmpty(t, baseline, "全程不断连也该收到通知")
	})

	t.Run("disconnect_then_catch_up", func(t *testing.T) {
		disconnected := make(chan struct{})
		caughtUp := make(chan struct{})
		rig := bootPhasedRig(t, &phasedBackendRunner{phases: []scriptPhase{
			{events: phase1}, {gate: disconnected, events: phase2}, {gate: caughtUp, events: phase3},
		}})
		rec := &notifyRecorder{}
		states := newConnStateLog()
		reconnectGate := make(chan struct{})
		rt := rig.durableRunner(t, rec, reconnectGate, states)
		events, result := rig.startRunOn(t, rt, sid)

		// 掐断:第一阶段已经实时到达,第二阶段在断连期间产生。等到的那几条要收回
		// 序列里,它们同样是这次运行交付出去的。
		got := awaitTextCollecting(t, events, "one")
		require.NoError(t, rig.cli.Close())
		close(disconnected)

		// 等 daemon 把第二阶段落完库(此刻推送无人接收),再放行重连。
		awaitJournalDepth(t, rig, sid, int64(len(phase1)+len(phase2)))
		close(reconnectGate)

		// 补齐落定 → 会话回到实时 → 第三阶段才开始产生。
		require.Eventually(t, func() bool { return states.sawAfterReconnect(sid) },
			10*time.Second, 10*time.Millisecond, "补齐后应回到 connected")
		close(caughtUp)

		got = append(got, drainRuntimeEvents(t, events, 15*time.Second)...)

		// (a) 客户端**获得**的通知序列与不断连时逐条相等:方法、seq、载荷字节。
		assert.Equal(t, baseline, rec.ordered(),
			"补齐后拿到的通知序列必须与全程不断连时逐条相等")
		// (b) 客户端**投递**出去的事件流也逐条相等 —— 这一条堵死重复投递:
		// (a) 按 seq 去重,只有 (b) 能证明同一条没有被消费两次。
		assert.Equal(t, baselineEvents, got, "补齐后交付的事件流不得多一条、少一条或换序")
		assert.Equal(t, baselineResult.StopErr, result.StopErr, "断连不得把终态污染成失败")
	})
}
