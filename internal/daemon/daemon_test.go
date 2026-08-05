package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/daemon/client"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestDaemon_TwoConnectionsKeepTerminalHandlersIsolated(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "agentred-isolation-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	d, err := New(Options{DataDir: dataDir, LANHost: "127.0.0.1", LANPort: 0})
	require.NoError(t, err)
	daemonCtx, stopDaemon := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(daemonCtx) }()
	defer func() {
		stopDaemon()
		select {
		case runErr := <-errCh:
			assert.NoError(t, runErr)
		case <-time.After(3 * time.Second):
			t.Error("daemon did not stop within 3s")
		}
	}()
	require.Eventually(t, func() bool {
		d.mu.RLock()
		ready := d.lan != nil && d.lan.Addr() != ""
		d.mu.RUnlock()
		return ready
	}, 2*time.Second, 10*time.Millisecond)

	pairBody := readLocalPair(t, d)
	pairCode, _ := pairBody["code"].(string)
	require.Len(t, pairCode, 6)

	d.mu.RLock()
	lanURL := d.lan.URL()
	d.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientA, err := client.Dial(ctx, client.Options{URL: lanURL})
	require.NoError(t, err)
	defer func() { _ = clientA.Close() }()
	var pairResp rpc.PairResult
	require.NoError(t, clientA.Call(ctx, "auth.pair", rpc.PairParams{
		Code:              pairCode,
		DeviceName:        "connection-a",
		DeviceFingerprint: "sha256:shared-device",
	}, &pairResp))

	clientB, err := client.Dial(ctx, client.Options{URL: lanURL})
	require.NoError(t, err)
	defer func() { _ = clientB.Close() }()
	require.NoError(t, clientB.Call(ctx, "auth.connect", rpc.ConnectParams{
		DeviceFingerprint:         "sha256:shared-device",
		DeviceToken:               pairResp.DeviceToken,
		ExpectedDaemonFingerprint: pairResp.DaemonFingerprint,
	}, nil))

	require.NoError(t, clientA.Call(ctx, "health.ping", nil, nil))
	require.NoError(t, clientB.Call(ctx, "health.ping", nil, nil))

	// Filling A's bounded pending-close tombstones exercises A's
	// TerminalHandlers without opening a real platform PTY. B must retain a
	// fresh handler set even after A reaches its own capacity and disconnects.
	var capacityErr error
	for i := 0; i < 1024 && capacityErr == nil; i++ {
		capacityErr = clientA.Call(ctx, "terminal.close", map[string]any{
			"terminalId":        fmt.Sprintf("connection-a-%d", i),
			"cancelPendingOpen": true,
		}, nil)
	}
	require.Error(t, capacityErr)
	require.Contains(t, capacityErr.Error(), "capacity")
	require.NoError(t, clientA.Close())
	require.NoError(t, clientB.Call(ctx, "terminal.close", map[string]any{
		"terminalId":        "connection-b-after-a-close",
		"cancelPendingOpen": true,
	}, nil))

	_, err = d.registry.Dispatch(context.Background(), "terminal.close", nil)
	require.ErrorIs(t, err, rpc.ErrMethodNotFound, "bindConn must not mutate the bootstrap registry")
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
