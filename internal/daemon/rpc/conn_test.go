package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testServer(t *testing.T, reg *Registry) (*httptest.Server, string) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		c := NewConn(ws, reg)
		go c.Serve(context.Background())
	}))
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	return srv, wsURL
}

func dialClient(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	c, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		_ = resp.Body.Close()
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestConn_RequestResponse(t *testing.T) {
	reg := NewRegistry()
	reg.Register("ping", func(ctx context.Context, p json.RawMessage) (any, error) {
		return "pong", nil
	})
	_, url := testServer(t, reg)
	c := dialClient(t, url)

	req := Frame{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "ping"}
	require.NoError(t, c.WriteJSON(req))

	var resp Frame
	require.NoError(t, c.ReadJSON(&resp))
	assert.Nil(t, resp.Error)
	assert.JSONEq(t, `"pong"`, string(resp.Result))
}

func TestConn_NotificationNoResponse(t *testing.T) {
	reg := NewRegistry()
	called := make(chan struct{}, 1)
	reg.Register("ping.notify", func(ctx context.Context, p json.RawMessage) (any, error) {
		called <- struct{}{}
		return nil, nil
	})
	_, url := testServer(t, reg)
	c := dialClient(t, url)

	n := Frame{JSONRPC: "2.0", Method: "ping.notify"}
	require.NoError(t, c.WriteJSON(n))

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("handler never called")
	}

	_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	var f Frame
	err := c.ReadJSON(&f)
	assert.Error(t, err, "expected timeout, got frame %+v", f)
}

func TestConn_UnknownMethodReturnsError(t *testing.T) {
	_, url := testServer(t, NewRegistry())
	c := dialClient(t, url)

	require.NoError(t, c.WriteJSON(Frame{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "no.such",
	}))
	var resp Frame
	require.NoError(t, c.ReadJSON(&resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
}

func TestConn_HandlerSeesConnInContext(t *testing.T) {
	reg := NewRegistry()
	got := make(chan *Conn, 1)
	reg.Register("seenConn", func(ctx context.Context, p json.RawMessage) (any, error) {
		got <- ConnFromContext(ctx)
		return "ok", nil
	})
	_, url := testServer(t, reg)
	cl := dialClient(t, url)
	require.NoError(t, cl.WriteJSON(Frame{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "seenConn"}))
	var resp Frame
	require.NoError(t, cl.ReadJSON(&resp))
	select {
	case c := <-got:
		assert.NotNil(t, c, "ConnFromContext must return the *Conn in handler")
	case <-time.After(time.Second):
		t.Fatal("handler never ran")
	}
}

// TestConn_NotificationsAreOrdered 钉死 notification 必须按发送顺序 dispatch。
// 历史 bug:Serve 对每条 notification 都 go func() dispatch,Go 调度器不保证
// goroutine 启动 = 抢锁顺序,导致 runtime.event (TextDelta) 在客户端乱序到达,
// model 输出 "/root/.config/agentre/agents/5" 渲染成 "//.rootconfig/agent/reagents/5"。
//
// 用 sleep-反序-jitter 制造最强不利场景:第一条 handler sleep N ms,最后一条 0 ms。
// 并发 dispatch 下后到的会先完成、append 在前 → 顺序倒挂。串行 dispatch 下顺序
// 与发送严格一致。
func TestConn_NotificationsAreOrdered(t *testing.T) {
	const N = 20
	reg := NewRegistry()
	var (
		mu   sync.Mutex
		seen []int
		done = make(chan struct{})
	)
	reg.Register("seq", func(ctx context.Context, p json.RawMessage) (any, error) {
		var v struct {
			N int `json:"n"`
		}
		_ = json.Unmarshal(p, &v)
		// 反序 jitter:n=0 等最久,n=N-1 不等。并发 dispatch 下 n=N-1 会先 append。
		time.Sleep(time.Duration(N-v.N) * 2 * time.Millisecond)
		mu.Lock()
		seen = append(seen, v.N)
		if len(seen) == N {
			close(done)
		}
		mu.Unlock()
		return nil, nil
	})
	_, url := testServer(t, reg)
	c := dialClient(t, url)

	for i := 0; i < N; i++ {
		params, _ := json.Marshal(map[string]int{"n": i})
		require.NoError(t, c.WriteJSON(Frame{JSONRPC: "2.0", Method: "seq", Params: params}))
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("only %d/%d notifications dispatched", len(seen), N)
	}

	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < N; i++ {
		assert.Equalf(t, i, seen[i], "notification at index %d arrived as %d (jitter exposed reordering: %v)", i, seen[i], seen)
	}
}

func TestConn_AuthStateRoundTrip(t *testing.T) {
	// Verifies the Auth/SetAuth accessor pair for later use by transport_lan.
	// Done without WS to keep the test pure.
	c := &Conn{}
	assert.False(t, c.Auth().Authenticated)
	c.SetAuth(AuthState{Authenticated: true, DeviceFingerprint: "sha256:abc", DeviceName: "mac"})
	a := c.Auth()
	assert.True(t, a.Authenticated)
	assert.Equal(t, "sha256:abc", a.DeviceFingerprint)
	assert.Equal(t, "mac", a.DeviceName)
}
