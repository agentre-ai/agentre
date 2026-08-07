package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
		c := NewConn(NewWebSocketFrameConn(ws), reg)
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

type frameConnStub struct {
	readFrames  chan Frame
	writeFrames chan Frame
	done        chan struct{}
	closeOnce   sync.Once
}

func newFrameConnStub() *frameConnStub {
	return &frameConnStub{
		readFrames:  make(chan Frame),
		writeFrames: make(chan Frame, 1),
		done:        make(chan struct{}),
	}
}

func (c *frameConnStub) WriteFrame(f Frame) error {
	select {
	case c.writeFrames <- f:
		return nil
	case <-c.done:
		return io.ErrClosedPipe
	}
}

func (c *frameConnStub) ReadFrame(f *Frame) error {
	select {
	case frame := <-c.readFrames:
		*f = frame
		return nil
	case <-c.done:
		return io.EOF
	}
}

func (c *frameConnStub) Close() error {
	c.closeOnce.Do(func() { close(c.done) })
	return nil
}

func (c *frameConnStub) Done() <-chan struct{} { return c.done }

func TestConn_GivenFrameConn_WhenServing_ThenFramesFlowWithoutWebSocket(t *testing.T) {
	reg := NewRegistry()
	reg.Register("echo", func(_ context.Context, params json.RawMessage) (any, error) {
		return params, nil
	})
	transport := newFrameConnStub()
	conn := NewConn(transport, reg)
	serveDone := make(chan struct{})
	go func() {
		conn.Serve(t.Context())
		close(serveDone)
	}()

	transport.readFrames <- Frame{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "echo",
		Params:  json.RawMessage(`"hello"`),
	}
	select {
	case response := <-transport.writeFrames:
		require.Nil(t, response.Error)
		assert.JSONEq(t, `"hello"`, string(response.Result))
	case <-time.After(time.Second):
		t.Fatal("Conn did not write a response through the frame transport")
	}

	require.NoError(t, conn.Close())
	select {
	case <-serveDone:
	case <-time.After(time.Second):
		t.Fatal("Serve did not exit after the frame transport closed")
	}
}

func TestConn_GivenClosedFrameConn_WhenCallAwaitsResponse_ThenReturnsErrConnClosed(t *testing.T) {
	transport := newFrameConnStub()
	conn := NewConn(transport, NewRegistry())
	errCh := make(chan error, 1)
	go func() { errCh <- conn.Call(context.Background(), "never.responds", nil, nil) }()

	select {
	case <-transport.writeFrames:
	case <-time.After(time.Second):
		t.Fatal("Call did not write its request through the frame transport")
	}
	require.NoError(t, transport.Close())

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, ErrConnClosed)
	case <-time.After(time.Second):
		t.Fatal("Call did not unblock when the frame transport closed")
	}
}

func TestConn_GivenExplicitRegistry_WhenConstructed_ThenKeepsExactRegistry(t *testing.T) {
	reg := NewRegistry()
	c := NewConn(nil, reg)

	require.Same(t, reg, c.Registry())
	c.Registry().Register("direct", func(context.Context, json.RawMessage) (any, error) {
		return "explicit", nil
	})
	got, err := reg.Dispatch(context.Background(), "direct", nil)
	require.NoError(t, err)
	assert.Equal(t, "explicit", got)
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

func TestConn_PeerDisconnectCancelsInFlightRequestContext(t *testing.T) {
	reg := NewRegistry()
	entered := make(chan struct{})
	canceled := make(chan struct{})
	reg.Register("waitForDisconnect", func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	})
	_, url := testServer(t, reg)
	cl := dialClient(t, url)
	require.NoError(t, cl.WriteJSON(Frame{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "waitForDisconnect",
	}))
	<-entered
	require.NoError(t, cl.Close())

	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("request context survived the concrete WebSocket connection")
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

type multiplexerHubStub struct {
	frames chan HubFrame
	sent   chan HubFrame

	mu           sync.Mutex
	connected    bool
	onDial       []func()
	onDisconnect []func(error)
}

func newMultiplexerHubStub() *multiplexerHubStub {
	return &multiplexerHubStub{
		frames: make(chan HubFrame, 16),
		sent:   make(chan HubFrame, 16),
	}
}

func (s *multiplexerHubStub) Send(messageType int, payload []byte) error {
	s.mu.Lock()
	connected := s.connected
	s.mu.Unlock()
	if !connected {
		return ErrHubUnavailable
	}
	s.sent <- HubFrame{MessageType: messageType, Payload: append([]byte(nil), payload...)}
	return nil
}

func (s *multiplexerHubStub) Receive() <-chan HubFrame { return s.frames }

func (s *multiplexerHubStub) AddLifecycleListener(onDial func(), onDisconnect func(error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onDial = append(s.onDial, onDial)
	s.onDisconnect = append(s.onDisconnect, onDisconnect)
}

func (s *multiplexerHubStub) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

func (s *multiplexerHubStub) dial() {
	s.mu.Lock()
	s.connected = true
	hooks := append([]func(){}, s.onDial...)
	s.mu.Unlock()
	for _, hook := range hooks {
		hook()
	}
}

func (s *multiplexerHubStub) disconnect(err error) {
	s.mu.Lock()
	s.connected = false
	hooks := append([]func(error){}, s.onDisconnect...)
	s.mu.Unlock()
	for _, hook := range hooks {
		hook(err)
	}
}

func receiveMultiplexedFrame(t *testing.T, link *multiplexerHubStub) HubFrame {
	t.Helper()
	select {
	case frame := <-link.sent:
		return frame
	case <-time.After(time.Second):
		t.Fatal("multiplexer did not send a relay frame")
		return HubFrame{}
	}
}

func TestMultiplexer_GivenVirtualChannels_WhenFramesFlow_ThenEnvelopePreservesFramesAndDoesNotCrossTalk(t *testing.T) {
	link := newMultiplexerHubStub()
	mux := newMultiplexer(link)
	t.Cleanup(mux.Close)
	link.dial()

	first, err := mux.Open()
	require.NoError(t, err)
	second, err := mux.Open()
	require.NoError(t, err)

	outbound := Frame{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "first", Params: json.RawMessage(`{"value":"one"}`)}
	require.NoError(t, first.WriteFrame(outbound))
	relayFrame := receiveMultiplexedFrame(t, link)
	require.Equal(t, websocket.BinaryMessage, relayFrame.MessageType)
	channelID, rawFrame, err := unmarshalRelayEnvelope(relayFrame.Payload)
	require.NoError(t, err)
	assert.Equal(t, first.ID(), channelID)
	wantRaw, err := json.Marshal(outbound)
	require.NoError(t, err)
	assert.Equal(t, wantRaw, rawFrame, "the envelope must carry the existing JSON-RPC bytes unchanged")

	secondInbound := Frame{JSONRPC: "2.0", Method: "second", Params: json.RawMessage(`{"value":"two"}`)}
	secondRaw, err := json.Marshal(secondInbound)
	require.NoError(t, err)
	link.frames <- HubFrame{MessageType: websocket.BinaryMessage, Payload: marshalRelayEnvelope(second.ID(), secondRaw)}

	var got Frame
	require.NoError(t, second.ReadFrame(&got))
	assert.Equal(t, secondInbound.Method, got.Method)
	assert.JSONEq(t, string(secondInbound.Params), string(got.Params))
	select {
	case unexpected := <-first.inbound:
		t.Fatalf("first virtual channel received frame for second channel: %+v", unexpected)
	default:
	}
}

func TestMultiplexer_GivenInboundChannel_WhenItsFirstEnvelopeArrives_ThenAcceptsAndRoutesIt(t *testing.T) {
	link := newMultiplexerHubStub()
	mux := newMultiplexer(link)
	t.Cleanup(mux.Close)
	link.dial()

	inbound := Frame{JSONRPC: "2.0", Method: "relay.inbound", Params: json.RawMessage(`{"source":"server"}`)}
	link.frames <- HubFrame{MessageType: websocket.BinaryMessage, Payload: marshalRelayEnvelope("server-channel-1", mustJSON(t, inbound))}

	select {
	case channel := <-mux.Accept():
		assert.Equal(t, "server-channel-1", channel.ID())
		var got Frame
		require.NoError(t, channel.ReadFrame(&got))
		assert.Equal(t, inbound.Method, got.Method)
		assert.JSONEq(t, string(inbound.Params), string(got.Params))
	case <-time.After(time.Second):
		t.Fatal("multiplexer did not accept the relay-initiated virtual channel")
	}
}

// 一个中转客户端断开时,server 只能通过「空载荷信封」告诉 daemon 那条虚拟通道没了 ——
// 共享的 relay websocket 还开着,整链路的 onDisconnect 不会触发。收不到这个信号,
// connRegistry 里会留下一个幽灵对端:MCP 隧道(R11)会把工具请求发给一条永远不会
// 回应的通道,Conn.Call 只能干等调用方 ctx(CLI 的 HTTP ctx ~285s)而不是立刻拿到
// 「发起端不在线」的语义错误。
func TestMultiplexer_GivenEmptyEnvelope_WhenItArrivesForAChannel_ThenClosesOnlyThatChannel(t *testing.T) {
	link := newMultiplexerHubStub()
	mux := newMultiplexer(link)
	t.Cleanup(mux.Close)
	link.dial()

	departing, err := mux.Open()
	require.NoError(t, err)
	surviving, err := mux.Open()
	require.NoError(t, err)

	link.frames <- HubFrame{MessageType: websocket.BinaryMessage, Payload: marshalRelayEnvelope(departing.ID(), nil)}

	assertChannelClosed(t, departing.Done())
	assertChannelOpen(t, surviving.Done())
	assert.True(t, link.Connected(), "a single channel close must not drop the shared relay")

	var discarded Frame
	assert.ErrorIs(t, departing.ReadFrame(&discarded), io.EOF)
}

// 通道关闭信号不得凭空造出一条通道 —— 否则一个迟到的 close 会在 Accept() 上
// 冒出一条刚生就死的连接。
func TestMultiplexer_GivenEmptyEnvelope_WhenChannelIsUnknown_ThenAcceptsNothing(t *testing.T) {
	link := newMultiplexerHubStub()
	mux := newMultiplexer(link)
	t.Cleanup(mux.Close)
	link.dial()

	link.frames <- HubFrame{MessageType: websocket.BinaryMessage, Payload: marshalRelayEnvelope("never-seen", nil)}

	select {
	case created := <-mux.Accept():
		t.Fatalf("channel close signal created a virtual channel %q", created.ID())
	case <-time.After(100 * time.Millisecond):
	}
}

func TestMultiplexer_GivenOneClosedChannel_WhenAnotherChannelUsesRelay_ThenRelayAndOtherChannelStayOpen(t *testing.T) {
	link := newMultiplexerHubStub()
	mux := newMultiplexer(link)
	t.Cleanup(mux.Close)
	link.dial()

	closed, err := mux.Open()
	require.NoError(t, err)
	open, err := mux.Open()
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	assertChannelClosed(t, closed.Done())
	link.frames <- HubFrame{MessageType: websocket.BinaryMessage, Payload: marshalRelayEnvelope(closed.ID(), mustJSON(t, Frame{
		JSONRPC: "2.0", Method: "must.not.reopen",
	}))}
	select {
	case recreated := <-mux.Accept():
		t.Fatalf("closed virtual channel was recreated as %q", recreated.ID())
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, open.WriteFrame(Frame{JSONRPC: "2.0", Method: "still.open"}))
	relayFrame := receiveMultiplexedFrame(t, link)
	channelID, _, err := unmarshalRelayEnvelope(relayFrame.Payload)
	require.NoError(t, err)
	assert.Equal(t, open.ID(), channelID)
	assertChannelOpen(t, open.Done())
	assert.True(t, link.Connected(), "closing one channel must not close the shared relay")
}

func TestMultiplexer_GivenHubLinkStops_WhenChannelIsOpen_ThenLifecycleClosesTheChannel(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade relay websocket: %v", err)
			return
		}
		defer func() { _ = ws.Close() }()
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	dialed := make(chan struct{}, 1)
	link := NewHubLink(HubLinkOptions{
		ServerURL:   server.URL,
		AccessToken: "test-token",
		OnDial:      func() { dialed <- struct{}{} },
	})
	mux := NewMultiplexer(link)
	t.Cleanup(mux.Close)
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- link.Run(ctx) }()

	select {
	case <-dialed:
	case <-time.After(time.Second):
		t.Fatal("HubLink did not establish its relay connection")
	}
	channel, err := mux.Open()
	require.NoError(t, err)
	cancel()

	assertChannelClosed(t, channel.Done())
	require.NoError(t, <-runDone)
}

func TestMultiplexer_GivenRelayDisconnect_WhenConnAwaitsResponse_ThenAllChannelsCloseAndCallFailsPromptly(t *testing.T) {
	link := newMultiplexerHubStub()
	mux := newMultiplexer(link)
	t.Cleanup(mux.Close)
	link.dial()

	channel, err := mux.Open()
	require.NoError(t, err)
	other, err := mux.Open()
	require.NoError(t, err)
	conn := NewConn(channel, NewRegistry())
	callDone := make(chan error, 1)
	go func() { callDone <- conn.Call(context.Background(), "waits.for.response", nil, nil) }()
	receiveMultiplexedFrame(t, link)

	link.disconnect(errors.New("relay disconnected"))
	assertChannelClosed(t, channel.Done())
	assertChannelClosed(t, other.Done())
	select {
	case err := <-callDone:
		require.ErrorIs(t, err, ErrConnClosed)
	case <-time.After(time.Second):
		t.Fatal("Conn.Call remained blocked after the relay disconnected")
	}
}

func TestConn_GivenVirtualChannel_WhenCalling_ThenRequestResponseRoundTripNeedsNoTransportAwareness(t *testing.T) {
	link := newMultiplexerHubStub()
	mux := newMultiplexer(link)
	t.Cleanup(mux.Close)
	link.dial()

	channel, err := mux.Open()
	require.NoError(t, err)
	conn := NewConn(channel, NewRegistry())
	serveDone := make(chan struct{})
	go func() {
		conn.Serve(t.Context())
		close(serveDone)
	}()

	var result string
	callDone := make(chan error, 1)
	go func() { callDone <- conn.Call(t.Context(), "relay.ping", map[string]string{"input": "hello"}, &result) }()

	outbound := receiveMultiplexedFrame(t, link)
	channelID, requestBytes, err := unmarshalRelayEnvelope(outbound.Payload)
	require.NoError(t, err)
	assert.Equal(t, channel.ID(), channelID)
	var request Frame
	require.NoError(t, json.Unmarshal(requestBytes, &request))
	assert.Equal(t, "relay.ping", request.Method)
	link.frames <- HubFrame{MessageType: websocket.BinaryMessage, Payload: marshalRelayEnvelope(channel.ID(), mustJSON(t, Frame{
		JSONRPC: "2.0", ID: request.ID, Result: json.RawMessage(`"pong"`),
	}))}

	select {
	case err := <-callDone:
		require.NoError(t, err)
		assert.Equal(t, "pong", result)
	case <-time.After(time.Second):
		t.Fatal("Conn.Call did not receive the virtual-channel response")
	}
	require.NoError(t, conn.Close())
	select {
	case <-serveDone:
	case <-time.After(time.Second):
		t.Fatal("Conn.Serve did not exit when its virtual channel closed")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}

func assertChannelClosed(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("virtual channel did not close")
	}
}

func assertChannelOpen(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
		t.Fatal("virtual channel closed unexpectedly")
	default:
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
