package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/client"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	piagentrt "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/piagent"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

func TestDaemon_BootShutdown(t *testing.T) {
	dir := t.TempDir()
	d, err := New(Options{
		DataDir: dir,
		LANHost: "127.0.0.1",
		LANPort: 0,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	require.Eventually(t, func() bool {
		d.mu.RLock()
		lan := d.lan
		d.mu.RUnlock()
		return lan != nil && lan.Addr() != ""
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestDaemon_IPCStatus(t *testing.T) {
	dir := t.TempDir()
	d, err := New(Options{
		DataDir: dir,
		LANHost: "127.0.0.1",
		LANPort: 0,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	require.Eventually(t, func() bool {
		_, err := os.Stat(d.SocketPath())
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)

	tr := &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
		return net.Dial("unix", d.SocketPath())
	}}
	c := &http.Client{Transport: tr}

	resp, err := c.Get("http://daemon/local/status")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var v map[string]any
	require.NoError(t, json.Unmarshal(body, &v))
	assert.NotEmpty(t, v["daemonUUID"])
	assert.NotContains(t, v, "keyStorage")

	resp2, err := c.Get("http://daemon/local/pair")
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	body2, _ := io.ReadAll(resp2.Body)
	var pp map[string]any
	require.NoError(t, json.Unmarshal(body2, &pp))
	code, _ := pp["code"].(string)
	assert.Len(t, code, 6)
}

// TestRecoverHandlerPanic 验证 RPC handler panic 被吃掉,翻成
// rpc.Error{ErrInternal} 让 daemon 进程不挂、客户端收到结构化错误,而不是
// 看到 SIGSEGV 整个 agentred 进程死。回归 claudecode runtime nil deref 把整
// 个 daemon 打挂 / 前端无任何提示 / 会话永远卡在「生成中」的旧 bug。
//
// 直接走 recoverHandlerPanic 而不是 wrapGuarded 是因为后者会先撞 requireAuth
// (需要真 *rpc.Conn 注入),与本测想覆盖的 panic-recovery 边界正交。
func TestRecoverHandlerPanic(t *testing.T) {
	t.Run("panic 翻成 daemon handler panic 错误", func(t *testing.T) {
		var err error
		func() {
			defer recoverHandlerPanic(&err)
			panic("boom")
		}()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "daemon handler panic")
		assert.Contains(t, err.Error(), "boom")
	})

	t.Run("nil pointer deref panic 同样被回收(回归原始 SIGSEGV 场景)", func(t *testing.T) {
		var err error
		func() {
			defer recoverHandlerPanic(&err)
			var p *int
			_ = *p
		}()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "daemon handler panic")
	})

	t.Run("无 panic 时 err 保持 nil", func(t *testing.T) {
		var err error
		func() { defer recoverHandlerPanic(&err) }()
		assert.NoError(t, err)
	})
}

type daemonPreparedPiRT struct {
	mu       sync.Mutex
	prepared []*daemonPreparedPiRun
	next     int
}

func newDaemonPreparedPiRT(ids ...string) *daemonPreparedPiRT {
	r := &daemonPreparedPiRT{}
	for _, id := range ids {
		r.prepared = append(r.prepared, &daemonPreparedPiRun{
			providerSessionID: id,
			events:            make(chan agentruntime.Event),
			closed:            make(chan struct{}),
		})
	}
	return r
}

func (*daemonPreparedPiRT) Capabilities() capability.Capabilities {
	return capability.Capabilities{Set: map[capability.Capability]bool{
		capability.CapAbort:       true,
		capability.CapForkSession: true,
	}}
}

func (*daemonPreparedPiRT) Run(context.Context, agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	return nil, nil, errors.New("daemon prepared Pi runtime must use PrepareRun")
}

func (r *daemonPreparedPiRT) PrepareRun(context.Context, agentruntime.RunRequest) (piagentrt.PreparedRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.next >= len(r.prepared) {
		return nil, errors.New("unexpected daemon Pi preparation")
	}
	prepared := r.prepared[r.next]
	r.next++
	return prepared, nil
}

type daemonPreparedPiRun struct {
	providerSessionID string
	events            chan agentruntime.Event
	closed            chan struct{}
	closeOnce         sync.Once
}

func (p *daemonPreparedPiRun) ProviderSessionID() string { return p.providerSessionID }

func (p *daemonPreparedPiRun) Start(context.Context) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	return p.events, &agentruntime.RunResult{ProviderSessionID: p.providerSessionID}, nil
}

func (p *daemonPreparedPiRun) Close(context.Context) error {
	p.closeOnce.Do(func() {
		close(p.closed)
		close(p.events)
	})
	return nil
}

func TestDaemon_ConnectionCleanupIsolatesReconnectWithSameSession(t *testing.T) {
	runtime := newDaemonPreparedPiRT("shared-native-session", "shared-native-session")
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypePiAgent, runtime)
	t.Cleanup(restore)
	d, stop := startTaskDaemon(t)
	defer stop()

	first, pair := pairDaemonClient(t, d, "sha256:cleanup-first")
	second := connectDaemonClient(t, d, "sha256:cleanup-first", pair)
	params := daemonPiRunParams(t, 501, "generation-first")
	prepareDaemonPi(t, first, params)

	require.NoError(t, first.Close())
	select {
	case <-runtime.prepared[0].closed:
	case <-time.After(2 * time.Second):
		t.Fatal("first connection close did not close its real PreparedRun resource")
	}

	params.PermissionMode = "generation-second"
	prepareDaemonPi(t, second, params)
	select {
	case <-runtime.prepared[1].closed:
		t.Fatal("old connection cleanup closed the reconnect generation with the same session identities")
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, second.Close())
	select {
	case <-runtime.prepared[1].closed:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect generation was not closed by its own connection")
	}
}

func TestDaemon_ShutdownClosesRunningPiGenerationBeforeReturning(t *testing.T) {
	runtime := newDaemonPreparedPiRT("shutdown-native-session")
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypePiAgent, runtime)
	t.Cleanup(restore)
	dir, err := os.MkdirTemp("", "ard-cleanup")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d, err := New(Options{DataDir: dir, LANHost: "127.0.0.1", LANPort: 0})
	require.NoError(t, err)
	daemonCtx, cancelDaemon := context.WithCancel(context.Background())
	errC := make(chan error, 1)
	go func() { errC <- d.Run(daemonCtx) }()
	require.Eventually(t, func() bool {
		d.mu.RLock()
		defer d.mu.RUnlock()
		return d.lan != nil && d.lan.Addr() != ""
	}, 2*time.Second, 10*time.Millisecond)

	conn, _ := pairDaemonClient(t, d, "sha256:shutdown")
	params := daemonPiRunParams(t, 502, "generation-shutdown")
	ack := prepareDaemonPi(t, conn, params)
	params.ProviderSessionID = ack.ProviderSessionID
	callCtx, cancelCall := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCall()
	var startAck wire.RunAck
	require.NoError(t, conn.Call(callCtx, wire.MethodRun, params, &startAck))

	cancelDaemon()
	select {
	case <-runtime.prepared[0].closed:
	case <-time.After(2 * time.Second):
		t.Fatal("daemon shutdown returned without closing the running Pi generation")
	}
	select {
	case runErr := <-errC:
		assert.NoError(t, runErr)
	case <-time.After(3 * time.Second):
		t.Fatal("daemon Run did not wait boundedly for connection runtime cleanup")
	}
}

func startTaskDaemon(t *testing.T) (*Daemon, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "ard-cleanup")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d, err := New(Options{DataDir: dir, LANHost: "127.0.0.1", LANPort: 0})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	errC := make(chan error, 1)
	go func() { errC <- d.Run(ctx) }()
	require.Eventually(t, func() bool {
		d.mu.RLock()
		defer d.mu.RUnlock()
		return d.lan != nil && d.lan.Addr() != ""
	}, 2*time.Second, 10*time.Millisecond)
	return d, func() {
		cancel()
		select {
		case <-errC:
		case <-time.After(3 * time.Second):
			t.Log("daemon did not shut down within 3s")
		}
	}
}

type daemonPairResult struct {
	DeviceToken       string `json:"deviceToken"`
	DaemonFingerprint string `json:"daemonFingerprint"`
}

func pairDaemonClient(t *testing.T, d *Daemon, fingerprint string) (*client.Client, daemonPairResult) {
	t.Helper()
	pairBody := readLocalPair(t, d)
	code, _ := pairBody["code"].(string)
	require.Len(t, code, 6)
	d.mu.RLock()
	url := d.lan.URL()
	d.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := client.Dial(ctx, client.Options{URL: url})
	require.NoError(t, err)
	var result daemonPairResult
	require.NoError(t, conn.Call(ctx, "auth.pair", rpc.PairParams{
		Code: code, DeviceName: "test-desktop", DeviceFingerprint: fingerprint,
	}, &result))
	require.NotEmpty(t, result.DeviceToken)
	return conn, result
}

func connectDaemonClient(t *testing.T, d *Daemon, fingerprint string, pair daemonPairResult) *client.Client {
	t.Helper()
	d.mu.RLock()
	url := d.lan.URL()
	d.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := client.Dial(ctx, client.Options{URL: url})
	require.NoError(t, err)
	var result rpc.ConnectResult
	require.NoError(t, conn.Call(ctx, "auth.connect", rpc.ConnectParams{
		DeviceFingerprint:         fingerprint,
		DeviceToken:               pair.DeviceToken,
		ExpectedDaemonFingerprint: pair.DaemonFingerprint,
	}, &result))
	return conn
}

func daemonPiRunParams(t *testing.T, sessionID int64, generation string) wire.RunParams {
	t.Helper()
	backend, err := json.Marshal(agent_backend_entity.AgentBackend{
		Type: string(agent_backend_entity.TypePiAgent), Name: "pi",
	})
	require.NoError(t, err)
	return wire.RunParams{
		Backend: json.RawMessage(backend), SessionID: sessionID, PermissionMode: generation, Cwd: t.TempDir(), UserText: "hello",
	}
}

func prepareDaemonPi(t *testing.T, conn *client.Client, params wire.RunParams) wire.RunAck {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var registration wire.RunAck
	require.NoError(t, conn.Call(ctx, wire.MethodRun, params, &registration))
	var prepared wire.RunAck
	require.NoError(t, conn.Call(ctx, wire.MethodRun, params, &prepared))
	require.NotEmpty(t, prepared.ProviderSessionID)
	return prepared
}
