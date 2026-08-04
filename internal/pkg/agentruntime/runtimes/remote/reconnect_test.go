package remote

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/pkg/jsonrpc"
)

// ── fakes ───────────────────────────────────────────────────────────────────

// fakeConn 是一条可脚本化的 daemon 连接:Handle 记下 handler(测试用来同步投递
// server-push),Call 交给 onCall 脚本,Close 触发断连。gomock 在这里不好用 ——
// 重连流程是「同一个方法在不同连接代上答不同的东西」,脚本比期望链清楚得多。
type fakeConn struct {
	mu       sync.Mutex
	handlers map[string]func(context.Context, json.RawMessage) (any, error)
	calls    []fakeCall
	onCall   func(method string, params, result any) error

	closed    chan struct{}
	closeOnce sync.Once
}

type fakeCall struct {
	Method string
	Params any
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		handlers: map[string]func(context.Context, json.RawMessage) (any, error){},
		closed:   make(chan struct{}),
	}
}

func (f *fakeConn) Handle(method string, fn func(context.Context, json.RawMessage) (any, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[method] = fn
}

func (f *fakeConn) Call(_ context.Context, method string, params, result any) error {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{Method: method, Params: params})
	script := f.onCall
	f.mu.Unlock()
	if script == nil {
		return nil
	}
	return script(method, params, result)
}

func (f *fakeConn) Notify(string, any) error { return nil }
func (f *fakeConn) Closed() <-chan struct{}  { return f.closed }
func (f *fakeConn) Close() error             { f.closeOnce.Do(func() { close(f.closed) }); return nil }
func (f *fakeConn) script(fn func(method string, params, result any) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onCall = fn
}

// deliver 同步投递一条 server-push 通知,走的正是 Runtime 注册进来的那个 handler。
func (f *fakeConn) deliver(t *testing.T, method string, payload any) {
	t.Helper()
	f.mu.Lock()
	fn, ok := f.handlers[method]
	f.mu.Unlock()
	require.True(t, ok, "no handler registered for %s", method)
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	_, err = fn(context.Background(), raw)
	require.NoError(t, err)
}

// dispatchRaw 从**另一个 goroutine** 投递一条 server-push。刻意不碰 *testing.T:
// require/assert 的 FailNow 只能在测试 goroutine 里调。
func (f *fakeConn) dispatchRaw(method string, raw json.RawMessage) {
	f.mu.Lock()
	fn := f.handlers[method]
	f.mu.Unlock()
	if fn == nil {
		return
	}
	_, _ = fn(context.Background(), raw)
}

func (f *fakeConn) methodCalls(method string) []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeCall
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// catchUpOrder 交出补齐三步的实际发生顺序。
func (f *fakeConn) catchUpOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		switch c.Method {
		case wire.MethodSessionAttach, wire.MethodSessionPull, wire.MethodSessionPendingWaiters:
			out = append(out, c.Method)
		}
	}
	return out
}

// fakeCursorPort 是 agentruntime.SessionCursorPort 的测试替身。
type fakeCursorPort struct {
	mu    sync.Mutex
	load  func(sessionID int64, fp string) (int64, bool, error)
	saved []savedCursor
}

type savedCursor struct {
	SessionID   int64
	Fingerprint string
	Seq         int64
}

func (f *fakeCursorPort) setLoad(fn func(sessionID int64, fp string) (int64, bool, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load = fn
}

func (f *fakeCursorPort) LoadCursor(_ context.Context, sessionID int64, fp string) (int64, bool, error) {
	f.mu.Lock()
	fn := f.load
	f.mu.Unlock()
	if fn == nil {
		return 0, true, nil
	}
	return fn(sessionID, fp)
}

func (f *fakeCursorPort) SaveCursor(_ context.Context, sessionID int64, fp string, seq int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, savedCursor{SessionID: sessionID, Fingerprint: fp, Seq: seq})
	return nil
}

// savedSeqs 交出落库过的每一个 seq(按发生顺序)。
func (f *fakeCursorPort) savedSeqs() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int64, 0, len(f.saved))
	for _, s := range f.saved {
		out = append(out, s.Seq)
	}
	return out
}

func (f *fakeCursorPort) lastSaved() (savedCursor, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.saved) == 0 {
		return savedCursor{}, false
	}
	return f.saved[len(f.saved)-1], true
}

// connStateRecorder 收集会话级连接态变化。
type connStateRecorder struct {
	mu  sync.Mutex
	got []recordedConnState
}

type recordedConnState struct {
	SessionID int64
	State     SessionConnState
}

func (c *connStateRecorder) OnSessionConnState(sessionID int64, st SessionConnState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, recordedConnState{SessionID: sessionID, State: st})
}

func (c *connStateRecorder) states(sessionID int64) []SessionConnState {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []SessionConnState
	for _, g := range c.got {
		if g.SessionID == sessionID {
			out = append(out, g.State)
		}
	}
	return out
}

// ── rig ─────────────────────────────────────────────────────────────────────

// reconnectRig 起一个带重连能力的 *Runtime:第一条连接 conn1 已在跑一轮会话 sid,
// 断连后 reconnect 交出 next() 给的下一条连接。
type reconnectRig struct {
	rt       *Runtime
	conn1    *fakeConn
	cursor   *fakeCursorPort
	observer *connStateRecorder
	events   <-chan agentruntime.Event
	result   *agentruntime.RunResult

	mu       sync.Mutex
	nextConn []agentruntime.DaemonClientPort
	nextFP   []string
	nextErr  []error
	attempts int
}

const rigFingerprint = "sha256:daemon-a"

const rigSessionID int64 = 42

// newReconnectRig 构造 rig 并跑起一轮会话。supported 决定能力探测(runtime.session.list)
// 是回正常结果还是回 method-not-found。
func newReconnectRig(t *testing.T, supported bool) *reconnectRig {
	t.Helper()
	return newReconnectRigWithBackoff(t, supported, []time.Duration{time.Millisecond, time.Millisecond})
}

// newReconnectRigWithBackoff 同上,但由调用方决定退避节奏(用来把会话稳定停在
// 某一档上供断言)。
func newReconnectRigWithBackoff(t *testing.T, supported bool, backoff []time.Duration) *reconnectRig {
	t.Helper()
	rig := &reconnectRig{
		conn1:    newFakeConn(),
		cursor:   &fakeCursorPort{},
		observer: &connStateRecorder{},
	}
	rig.conn1.script(func(method string, _, result any) error {
		switch method {
		case wire.MethodSessionList:
			if !supported {
				return &jsonrpc.Error{Code: jsonrpc.ErrMethodNotFound.Code, Message: "Method not found"}
			}
			return nil
		case wire.MethodRun:
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rigSessionID}
			return nil
		}
		return nil
	})
	rig.rt = New(rig.conn1,
		WithReconnect(ReconnectFunc(rig.reconnect)),
		WithDaemonFingerprint(rigFingerprint),
		WithSessionCursor(rig.cursor),
		WithConnStateObserver(rig.observer),
		WithReconnectBackoff(backoff),
		WithCursorFlushInterval(0),
	)
	t.Cleanup(func() { _ = rig.rt.Close() })

	events, result, err := rig.rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode), ID: 1, Name: "x"},
		SessionID: rigSessionID,
		UserText:  "hi",
	})
	require.NoError(t, err)
	rig.events, rig.result = events, result
	return rig
}

// queue 排入下一次重连的结果。
func (r *reconnectRig) queue(c agentruntime.DaemonClientPort, fp string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextConn = append(r.nextConn, c)
	r.nextFP = append(r.nextFP, fp)
	r.nextErr = append(r.nextErr, err)
}

func (r *reconnectRig) reconnect(context.Context) (agentruntime.DaemonClientPort, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts++
	if len(r.nextConn) == 0 {
		return nil, "", assertErr("no more connections queued")
	}
	c, fp, err := r.nextConn[0], r.nextFP[0], r.nextErr[0]
	r.nextConn, r.nextFP, r.nextErr = r.nextConn[1:], r.nextFP[1:], r.nextErr[1:]
	return c, fp, err
}

func (r *reconnectRig) reconnectAttempts() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attempts
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

// catchUpConn 造一条「重连后」的连接:attach 报 latestSeq,pull 交出 journaled,
// pendingWaiters 交出 waiters。
func catchUpConn(latestSeq int64, journaled []wire.JournaledNotification, waiters wire.SessionPendingWaitersResult) *fakeConn {
	c := newFakeConn()
	c.script(func(method string, params, result any) error {
		switch method {
		case wire.MethodSessionAttach:
			*(result.(*wire.SessionAttachResult)) = wire.SessionAttachResult{
				SessionID:      rigSessionID,
				LifecycleState: wire.SessionLifecycleRunning,
				LatestSeq:      latestSeq,
			}
		case wire.MethodSessionPull:
			p := params.(wire.SessionPullParams)
			out := wire.SessionPullResult{Cursor: p.Cursor}
			for _, n := range journaled {
				if n.Seq > p.Cursor {
					out.Notifications = append(out.Notifications, n)
					out.Cursor = n.Seq
				}
			}
			*(result.(*wire.SessionPullResult)) = out
		case wire.MethodSessionPendingWaiters:
			*(result.(*wire.SessionPendingWaitersResult)) = waiters
		}
		return nil
	})
	return c
}

func journaledEvent(seq int64, text string) wire.JournaledNotification {
	ev, _ := json.Marshal(agentruntime.TextDelta{Text: text})
	// 日志里的 payload **不含 seq** —— seq 是日志行自己的列。
	params, _ := json.Marshal(wire.EventFrame{SessionID: rigSessionID, Event: ev})
	return wire.JournaledNotification{Seq: seq, Method: wire.NotifyEvent, Params: params}
}

func journaledDone(seq int64, model string) wire.JournaledNotification {
	params, _ := json.Marshal(wire.RunResultDoneFrame{SessionID: rigSessionID, Model: model})
	return wire.JournaledNotification{Seq: seq, Method: wire.NotifyRunResultDone, Params: params}
}

// journaledAutoEvent / journaledAutoDone 是自主续轮那两类通知的日志行。
func journaledAutoEvent(seq int64, text string) wire.JournaledNotification {
	ev, _ := json.Marshal(agentruntime.TextDelta{Text: text})
	params, _ := json.Marshal(wire.EventFrame{SessionID: rigSessionID, Event: ev})
	return wire.JournaledNotification{Seq: seq, Method: wire.NotifyAutonomousTurnEvent, Params: params}
}

func journaledAutoDone(seq int64, model string) wire.JournaledNotification {
	params, _ := json.Marshal(wire.RunResultDoneFrame{SessionID: rigSessionID, Model: model})
	return wire.JournaledNotification{Seq: seq, Method: wire.NotifyAutonomousTurnDone, Params: params}
}

// drainTexts 读到 channel 关闭为止,收集 TextDelta 文本。
func drainTexts(t *testing.T, ch <-chan agentruntime.Event, deadline time.Duration) []string {
	t.Helper()
	var out []string
	timeout := time.After(deadline)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			if td, isText := ev.(agentruntime.TextDelta); isText {
				out = append(out, td.Text)
			}
		case <-timeout:
			t.Fatalf("timed out waiting for events channel close; got %v", out)
		}
	}
}

// ── R4: 断连不终止会话 ───────────────────────────────────────────────────────

// Given 一条在跑的远端会话与一台支持补齐的 daemon,When 连接断开,Then 会话不被
// 注入 ErrDaemonDisconnected、events channel 不关闭,连接态转入 reconnecting。
func TestDisconnect_DoesNotEndSession_EntersReconnecting(t *testing.T) {
	// 第一档 1ms 后失败,第二档要等很久 —— 会话因此稳定停在 reconnecting 态供断言。
	rig := newReconnectRigWithBackoff(t, true, []time.Duration{time.Millisecond, time.Hour})
	rig.queue(nil, "", assertErr("dial refused"))

	_ = rig.conn1.Close()

	require.Eventually(t, func() bool {
		return rig.reconnectAttempts() >= 1
	}, time.Second, time.Millisecond, "watchClose 应发起重连")

	assert.Nil(t, rig.result.StopErr, "断连不得注入 StopErr")
	select {
	case _, ok := <-rig.events:
		require.True(t, ok, "断连不得 close events channel")
		t.Fatal("断连不应产生任何事件")
	default:
	}
	assert.Equal(t, ConnStateReconnecting, rig.rt.ConnState(rigSessionID))
	require.NotEmpty(t, rig.observer.states(rigSessionID))
	assert.Equal(t, ConnStateReconnecting, rig.observer.states(rigSessionID)[0].State)
}

// ── 补齐三步 + 重放喂回同一套 handler ────────────────────────────────────────

// Given 断连期间 daemon 又落了两条通知,When 重连补齐,Then 它们经**实时同一套
// handler** 交付给会话,游标推进到最新 seq,补齐三步按 attach → pull →
// pendingWaiters 的顺序发出。
func TestReconnect_CatchUpReplaysJournaledNotifications(t *testing.T) {
	rig := newReconnectRig(t, true)
	rig.cursor.setLoad(func(int64, string) (int64, bool, error) { return 3, true, nil })
	conn2 := catchUpConn(5, []wire.JournaledNotification{
		journaledEvent(4, "caught-up"),
		journaledDone(5, "sonnet"),
	}, wire.SessionPendingWaitersResult{})
	rig.queue(conn2, rigFingerprint, nil)

	_ = rig.conn1.Close()

	texts := drainTexts(t, rig.events, 3*time.Second)
	assert.Equal(t, []string{"caught-up"}, texts, "补齐的通知必须经实时 handler 交付")
	assert.Equal(t, "sonnet", rig.result.Model, "终态帧同样走补齐")

	// 三步顺序:先 attach 认领实时流与控制面,再 pull,最后 pendingWaiters。
	order := conn2.catchUpOrder()
	require.GreaterOrEqual(t, len(order), 3)
	assert.Equal(t, wire.MethodSessionAttach, order[0])
	assert.Equal(t, wire.MethodSessionPull, order[1])
	assert.Equal(t, wire.MethodSessionPendingWaiters, order[len(order)-1])

	last, ok := rig.cursor.lastSaved()
	require.True(t, ok, "补齐后必须落游标")
	assert.Equal(t, int64(5), last.Seq)
	assert.Equal(t, rigFingerprint, last.Fingerprint)
}

// Given 补齐正在翻页,When 一条更靠后的实时帧在同一时刻从新连接到达(attach 之后
// daemon 立刻就把新通知推给这条连接),Then 交付顺序仍严格按 seq 递增、同一条不交付
// 两次 —— 「补齐的尾巴」与「新到的实时帧」交接是硬不变量最容易破的那一处,而集成
// 用例把第三阶段闸在补齐落定之后,恰恰绕开了它。
func TestCatchUp_LiveFrameDuringReplay_KeepsOrderAndDoesNotDuplicate(t *testing.T) {
	rig := newReconnectRig(t, true)
	rig.cursor.setLoad(func(int64, string) (int64, bool, error) { return 0, true, nil })

	journal := []wire.JournaledNotification{
		journaledEvent(1, "one"), journaledEvent(2, "two"),
		journaledEvent(3, "three"), journaledEvent(4, "four"),
		journaledDone(5, "sonnet"),
	}
	conn2 := newFakeConn()
	var liveOnce sync.Once
	conn2.script(func(method string, params, result any) error {
		switch method {
		case wire.MethodSessionAttach:
			*(result.(*wire.SessionAttachResult)) = wire.SessionAttachResult{
				SessionID: rigSessionID, LifecycleState: wire.SessionLifecycleRunning, LatestSeq: 5,
			}
		case wire.MethodSessionPull:
			p := params.(wire.SessionPullParams)
			// 第一页还在飞的时候,daemon 把 seq=4 推到这条新连接上。它比补齐进度
			// 靠前,必须被闸门挡下,而不是插到 2/3 前面 —— 也不得在补齐拉到它时
			// 再交付一次。
			liveOnce.Do(func() {
				done := make(chan struct{})
				go func() {
					defer close(done)
					ev, _ := json.Marshal(agentruntime.TextDelta{Text: "four"})
					raw, _ := json.Marshal(wire.EventFrame{SessionID: rigSessionID, Event: ev, Seq: 4})
					conn2.dispatchRaw(wire.NotifyEvent, raw)
				}()
				<-done
			})
			out := wire.SessionPullResult{Cursor: p.Cursor}
			for _, n := range journal {
				if n.Seq > p.Cursor && len(out.Notifications) < 2 {
					out.Notifications = append(out.Notifications, n)
					out.Cursor = n.Seq
				}
			}
			out.HasMore = out.Cursor < 5
			*(result.(*wire.SessionPullResult)) = out
		}
		return nil
	})
	rig.queue(conn2, rigFingerprint, nil)

	_ = rig.conn1.Close()

	texts := drainTexts(t, rig.events, 5*time.Second)
	assert.Equal(t, []string{"one", "two", "three", "four"}, texts,
		"交接期间交付必须严格按 seq 递增,且同一条不得交付两次")
	assert.Equal(t, "sonnet", rig.result.Model)
}

// Given 日志里夹着一条本客户端还不认识的通知(新版 daemon 加了第六类通知),
// When 重连补齐,Then 它后面的已知通知照常按序交付。
//
// 认不得的那条如果只是「跳过」而不占掉它那一格游标,下一条已知通知就会被闸门判成
// 跳号丢弃,补洞拉取又会把同一条认不得的通知再拉回来 —— 每条后续通知都触发一次
// 拉取且一条也交付不出去,会话卡死在「生成中」直到超时。
func TestReplay_UnknownNotificationMethod_DoesNotStallCatchUp(t *testing.T) {
	rig := newReconnectRig(t, true)
	rig.cursor.setLoad(func(int64, string) (int64, bool, error) { return 3, true, nil })
	unknown := wire.JournaledNotification{
		Seq:    4,
		Method: "runtime.somethingTheClientDoesNotKnow",
		Params: json.RawMessage(`{"sessionId":42}`),
	}
	conn2 := catchUpConn(6, []wire.JournaledNotification{
		unknown,
		journaledEvent(5, "after-hole"),
		journaledDone(6, "sonnet"),
	}, wire.SessionPendingWaitersResult{})
	rig.queue(conn2, rigFingerprint, nil)

	_ = rig.conn1.Close()

	texts := drainTexts(t, rig.events, 3*time.Second)
	assert.Equal(t, []string{"after-hole"}, texts,
		"认不得的一条不得把它后面的已知通知一起吞掉")
	assert.Equal(t, "sonnet", rig.result.Model, "终态帧同样要补齐得到")
}

// ── R6: 跳号补洞 / 重复丢弃 ──────────────────────────────────────────────────

// Given 本地游标为 3,When 实时帧带着 seq=6 到达(跳号),Then 该帧不被消费,
// 客户端改从游标发起增量拉取,拉平后按序交付 4/5/6。
func TestLiveFrame_SeqGap_TriggersPull(t *testing.T) {
	rig := newReconnectRig(t, true)
	rig.cursor.setLoad(func(int64, string) (int64, bool, error) { return 3, true, nil })
	rig.conn1.script(func(method string, params, result any) error {
		switch method {
		case wire.MethodSessionPull:
			p := params.(wire.SessionPullParams)
			out := wire.SessionPullResult{Cursor: p.Cursor}
			for _, n := range []wire.JournaledNotification{
				journaledEvent(4, "a"), journaledEvent(5, "b"), journaledEvent(6, "c"),
			} {
				if n.Seq > p.Cursor {
					out.Notifications = append(out.Notifications, n)
					out.Cursor = n.Seq
				}
			}
			*(result.(*wire.SessionPullResult)) = out
		}
		return nil
	})

	ev, err := json.Marshal(agentruntime.TextDelta{Text: "gap-frame"})
	require.NoError(t, err)
	rig.conn1.deliver(t, wire.NotifyEvent, wire.EventFrame{SessionID: rigSessionID, Event: ev, Seq: 6})

	var texts []string
	require.Eventually(t, func() bool {
		for {
			select {
			case e := <-rig.events:
				if td, ok := e.(agentruntime.TextDelta); ok {
					texts = append(texts, td.Text)
				}
			default:
				return len(texts) >= 3
			}
		}
	}, 3*time.Second, 5*time.Millisecond)

	assert.Equal(t, []string{"a", "b", "c"}, texts,
		"跳号帧本身不消费,补洞拉回来的才是交付内容")
	assert.NotEmpty(t, rig.conn1.methodCalls(wire.MethodSessionPull), "跳号必须触发增量拉取")
}

// Given 本地游标为 5,When 实时帧带着 seq=5(重复投递)到达,Then 丢弃。
func TestLiveFrame_SeqNotNewerThanCursor_Discarded(t *testing.T) {
	rig := newReconnectRig(t, true)
	rig.cursor.setLoad(func(int64, string) (int64, bool, error) { return 5, true, nil })

	ev, err := json.Marshal(agentruntime.TextDelta{Text: "dup"})
	require.NoError(t, err)
	rig.conn1.deliver(t, wire.NotifyEvent, wire.EventFrame{SessionID: rigSessionID, Event: ev, Seq: 5})

	select {
	case got := <-rig.events:
		t.Fatalf("不大于游标的 seq 必须丢弃,却交付了 %#v", got)
	case <-time.After(100 * time.Millisecond):
	}
	assert.Empty(t, rig.conn1.methodCalls(wire.MethodSessionPull), "重复帧不该触发补洞")
}

// ── 回放到的旧轮次终态帧不得终结当前这一轮 ───────────────────────────────────

// Given 客户端关掉又重开(游标停在上一轮中段,而那之后 daemon 上又跑完了几轮),
// When 新一轮的第一条实时帧触发补洞、回放区间里夹着**已结束轮次**的 runResultDone,
// Then 它既不关闭新这一轮的 events、也不拿旧结果覆盖它的 RunResult,其后的帧照常
// 交付;只有新这一轮自己的终态帧才收尾。
//
// 破法是用户可见的:关掉 App、过一阵重开、发一条新消息 —— 这条消息瞬间「结束」并
// 带着上一轮的答案,其后的实时帧全部走「未知会话」被丢弃。
//
// 区间里刻意放三条已结束轮次的终态帧:回放一整段历史是常态而非边角,守卫必须对整段
// 成立,而不是只挡住紧挨着游标的那一条。
func TestGapFill_ReplayedDoneOfEndedTurn_DoesNotEndCurrentTurn(t *testing.T) {
	// 游标停在 3;4..9 是客户端离线期间落库的三轮尾巴,9 是开新一轮那一刻的高水位;
	// 10/11 才是新这一轮自己的通知。
	journal := []wire.JournaledNotification{
		journaledEvent(4, "old-a"), journaledDone(5, "old-1"),
		journaledEvent(6, "old-b"), journaledDone(7, "old-2"),
		journaledEvent(8, "old-c"), journaledDone(9, "old-3"),
		journaledEvent(10, "new-a"), journaledEvent(11, "new-b"),
	}
	conn := newFakeConn()
	conn.script(func(method string, params, result any) error {
		switch method {
		case wire.MethodSessionList:
			*(result.(*wire.SessionListResult)) = wire.SessionListResult{
				Sessions: []wire.SessionSummary{{
					SessionID:      rigSessionID,
					LifecycleState: wire.SessionLifecycleIdle,
					LatestSeq:      9,
				}},
			}
		case wire.MethodRun:
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rigSessionID}
		case wire.MethodSessionPull:
			p := params.(wire.SessionPullParams)
			out := wire.SessionPullResult{Cursor: p.Cursor}
			for _, n := range journal {
				if n.Seq > p.Cursor {
					out.Notifications = append(out.Notifications, n)
					out.Cursor = n.Seq
				}
			}
			*(result.(*wire.SessionPullResult)) = out
		}
		return nil
	})
	cursor := &fakeCursorPort{}
	cursor.setLoad(func(int64, string) (int64, bool, error) { return 3, true, nil })
	rt := New(conn,
		WithReconnect(ReconnectFunc(func(context.Context) (agentruntime.DaemonClientPort, string, error) {
			return nil, "", ErrReconnectAbandoned
		})),
		WithDaemonFingerprint(rigFingerprint),
		WithSessionCursor(cursor),
		WithReconnectBackoff([]time.Duration{time.Millisecond}),
		WithCursorFlushInterval(0),
	)
	t.Cleanup(func() { _ = rt.Close() })

	events, result, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode), ID: 1, Name: "x"},
		SessionID: rigSessionID,
		UserText:  "新一轮",
	})
	require.NoError(t, err)

	// 新这一轮的第一条实时帧:对着停在 3 的游标是跳号,整段区间因此被重放回来。
	ev, err := json.Marshal(agentruntime.TextDelta{Text: "new-a"})
	require.NoError(t, err)
	conn.deliver(t, wire.NotifyEvent, wire.EventFrame{SessionID: rigSessionID, Event: ev, Seq: 10})

	var texts []string
	closedEarly := false
	require.Eventually(t, func() bool {
		for {
			select {
			case e, ok := <-events:
				if !ok {
					closedEarly = true
					return true
				}
				if td, isText := e.(agentruntime.TextDelta); isText {
					texts = append(texts, td.Text)
				}
			default:
				return len(texts) >= 5
			}
		}
	}, 3*time.Second, 5*time.Millisecond)

	assert.False(t, closedEarly, "已结束轮次的终态帧不得关掉新这一轮的 events")
	assert.Equal(t, []string{"old-a", "old-b", "old-c", "new-a", "new-b"}, texts,
		"旧轮次的终态帧不得截断回放:它后面的帧照常按序交付")
	assert.Empty(t, result.Model, "旧轮次的结果不得覆盖新这一轮的 RunResult")
	assert.True(t, rt.hasSession(rigSessionID),
		"新这一轮必须还在会话表里,否则其后的实时帧会被当作未知会话丢弃")

	// 只有新这一轮自己的终态帧才收尾。
	conn.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID: rigSessionID, Model: "new-turn", Seq: 12,
	})
	select {
	case _, ok := <-events:
		assert.False(t, ok, "本轮自己的终态帧到达后 events 必须关闭")
	case <-time.After(time.Second):
		t.Fatal("本轮自己的终态帧没有收尾 events")
	}
	assert.Equal(t, "new-turn", result.Model, "收尾的必须是本轮自己的结果")
}

// ── 游标落库防抖:轮末那一条不能压在里面 ─────────────────────────────────────

// Given 游标落库开了防抖,When 轮末终态帧到达,Then 游标当场落库,不等防抖窗口。
//
// 压在防抖里的后果:用户拿到答案随手关窗(2 秒内进程就没了),库里的游标停在这一轮
// 的中段。下一轮换了新的 *remote.Runtime,第一条实时帧对着旧游标判成跳号,从旧游标
// 整段补齐 —— 上一轮的尾巴被重放进新的一轮,硬不变量的「无重复」当场破掉。
func TestTerminalNotification_FlushesCursorImmediately(t *testing.T) {
	rig := newReconnectRigWithBackoff(t, true, []time.Duration{time.Hour})
	// 防抖窗口设成一小时:任何落库都只可能来自轮末的主动 flush。
	rig.rt.cursorFlush = time.Hour

	ev, err := json.Marshal(agentruntime.TextDelta{Text: "x"})
	require.NoError(t, err)
	rig.conn1.deliver(t, wire.NotifyEvent, wire.EventFrame{SessionID: rigSessionID, Event: ev, Seq: 1})
	_, ok := rig.cursor.lastSaved()
	require.False(t, ok, "轮中的每一条不该各写一次库 —— 那正是防抖要省掉的")

	rig.conn1.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{SessionID: rigSessionID, Seq: 2})

	last, ok := rig.cursor.lastSaved()
	require.True(t, ok, "轮末必须把攒下的游标落库")
	assert.Equal(t, int64(2), last.Seq)
	assert.Equal(t, rigFingerprint, last.Fingerprint)
}

// ── R12: 实例标识不匹配 → 游标失效 ──────────────────────────────────────────

// Given 会话记录的 daemon 实例标识与重连上的这台不一致,When 重连,Then 不发起
// 增量拉取,会话按已中断处理(注入 ErrDaemonDisconnected 并关闭 events)。
func TestReconnect_DaemonIdentityMismatch_InvalidatesCursor(t *testing.T) {
	rig := newReconnectRig(t, true)
	rig.cursor.setLoad(func(_ int64, fp string) (int64, bool, error) {
		assert.Equal(t, "sha256:daemon-b", fp, "校验必须用重连后观察到的指纹")
		return 0, false, nil
	})
	conn2 := catchUpConn(9, []wire.JournaledNotification{journaledEvent(4, "should-not-appear")},
		wire.SessionPendingWaitersResult{})
	rig.queue(conn2, "sha256:daemon-b", nil)

	_ = rig.conn1.Close()

	texts := drainTexts(t, rig.events, 3*time.Second)
	assert.Empty(t, texts, "游标失效时不得重放任何通知")
	assert.ErrorIs(t, rig.result.StopErr, ErrDaemonDisconnected)
	assert.Empty(t, conn2.methodCalls(wire.MethodSessionPull), "游标失效时不得增量拉取")
	assert.Equal(t, ConnStateLost, rig.rt.ConnState(rigSessionID))
}

// Given 重连时游标端口读失败(库正忙、事务冲突之类),When 补齐,Then 不把它当成
// 「游标失效」收尾会话,而是按退避表再试一次 —— 读不出来不等于对不上。
//
// R12 说的失效只有一种:daemon 实例标识不匹配。把一次瞬时读错误也算进去,等于让一次
// sqlite busy 就杀掉一条远端还在跑的会话,正是 R4 要消灭的行为。
func TestReconnect_CursorLoadError_RetriesInsteadOfEndingSession(t *testing.T) {
	rig := newReconnectRig(t, true)
	var loads int32
	rig.cursor.setLoad(func(int64, string) (int64, bool, error) {
		if atomic.AddInt32(&loads, 1) == 1 {
			return 0, false, assertErr("database is locked")
		}
		return 3, true, nil
	})
	conn2 := catchUpConn(5, nil, wire.SessionPendingWaitersResult{})
	conn3 := catchUpConn(5, []wire.JournaledNotification{
		journaledEvent(4, "recovered"),
		journaledDone(5, "sonnet"),
	}, wire.SessionPendingWaitersResult{})
	rig.queue(conn2, rigFingerprint, nil)
	rig.queue(conn3, rigFingerprint, nil)

	_ = rig.conn1.Close()

	texts := drainTexts(t, rig.events, 3*time.Second)
	assert.Equal(t, []string{"recovered"}, texts, "读游标失败只该让这一次补齐重来")
	assert.Empty(t, conn2.methodCalls(wire.MethodSessionPull), "游标没读出来之前不得拉取")
	assert.Nil(t, rig.result.StopErr, "瞬时读错误不得把会话按已中断收尾")
}

// ── 游标越界:daemon 的高水位低于本地游标 ────────────────────────────────────

// Given daemon 的通知日志退到了本地游标之下(agentred.db 被恢复 / 截断,而 state.json
// 连同 TOFU 指纹还在,R12 的指纹校验因此照常放行),When 重连接管,Then 客户端拿接管
// 交回的高水位认出游标越界,作废它(连同库里那份)并从头补齐,其后的实时帧照常交付。
//
// 不作废的后果不是少几条:此后每一条实时帧都满足 seq <= 游标,在 dispatchNotification
// 的第一条规则里被静默丢弃 —— 会话没有跳号、没有错误、Debug 以上没有任何日志地冻住。
func TestReconnect_CursorAboveDaemonHighWater_InvalidatesCursorAndCatchesUpFromScratch(t *testing.T) {
	rig := newReconnectRig(t, true)
	// 本地游标停在 7,而 daemon 恢复出来的日志只到 3。
	rig.cursor.setLoad(func(int64, string) (int64, bool, error) { return 7, true, nil })
	conn2 := catchUpConn(3, []wire.JournaledNotification{
		journaledEvent(1, "restored-1"),
		journaledEvent(2, "restored-2"),
		journaledEvent(3, "restored-3"),
	}, wire.SessionPendingWaitersResult{})
	rig.queue(conn2, rigFingerprint, nil)

	_ = rig.conn1.Close()

	// 等补齐三步跑完(pendingWaiters 是最后一步)。不能等 ConnState:会话此刻还没有
	// 补齐状态条目,ConnState 会先答一个「已连接」。
	require.Eventually(t, func() bool {
		return len(conn2.methodCalls(wire.MethodSessionPendingWaiters)) > 0
	}, 3*time.Second, 5*time.Millisecond, "补齐应完成")
	assert.Equal(t, ConnStateConnected, rig.rt.ConnState(rigSessionID))

	// 补齐落定之后 daemon 推来的下一条实时帧:高水位 3 之后就是 seq=4。
	ev, err := json.Marshal(agentruntime.TextDelta{Text: "live-after-restore"})
	require.NoError(t, err)
	conn2.deliver(t, wire.NotifyEvent, wire.EventFrame{SessionID: rigSessionID, Event: ev, Seq: 4})

	var texts []string
	require.Eventually(t, func() bool {
		for {
			select {
			case e := <-rig.events:
				if td, ok := e.(agentruntime.TextDelta); ok {
					texts = append(texts, td.Text)
				}
			default:
				return len(texts) >= 4
			}
		}
	}, 3*time.Second, 5*time.Millisecond, "越界游标必须被作废,否则每条帧都被静默丢掉")

	assert.Equal(t, []string{"restored-1", "restored-2", "restored-3", "live-after-restore"}, texts,
		"游标越界时从头补齐,其后的实时帧照常按序交付")
	assert.Contains(t, rig.cursor.savedSeqs(), int64(0),
		"库里那份越界的游标也要一并作废,否则下次进程启动又从它开始丢帧")
}

// Given daemon 交回的高水位正好等于本地游标(补齐已经拉平,没有任何回退),When 重连
// 接管,Then 游标原样保留 —— 越界守卫只在**严格大于**时开火,不能把正常的「已拉平」
// 当成越界,那会把整段转录重放一遍(硬不变量的「无重复」当场破掉)。
func TestReconnect_CursorEqualsDaemonHighWater_KeepsCursor(t *testing.T) {
	rig := newReconnectRig(t, true)
	rig.cursor.setLoad(func(int64, string) (int64, bool, error) { return 3, true, nil })
	conn2 := catchUpConn(3, []wire.JournaledNotification{
		journaledEvent(1, "old-1"),
		journaledEvent(2, "old-2"),
		journaledEvent(3, "old-3"),
	}, wire.SessionPendingWaitersResult{})
	rig.queue(conn2, rigFingerprint, nil)

	_ = rig.conn1.Close()

	require.Eventually(t, func() bool {
		return len(conn2.methodCalls(wire.MethodSessionPendingWaiters)) > 0
	}, 3*time.Second, 5*time.Millisecond, "补齐应完成")

	select {
	case got := <-rig.events:
		t.Fatalf("游标与高水位持平时不得重放任何通知,却交付了 %#v", got)
	case <-time.After(100 * time.Millisecond):
	}
	assert.NotContains(t, rig.cursor.savedSeqs(), int64(0), "持平不是越界,不得作废游标")
}

// ── R18: 老 daemon 回落 ─────────────────────────────────────────────────────

// Given 一台不认识补齐族 RPC 的老 daemon,When 连接断开,Then 立即回落到今天的
// 「断连即终止」:注入 ErrDaemonDisconnected 并 close events,不发起任何重连。
func TestDisconnect_DaemonWithoutCatchUpRPCs_FallsBackToEndingTurn(t *testing.T) {
	rig := newReconnectRig(t, false)

	_ = rig.conn1.Close()

	texts := drainTexts(t, rig.events, 3*time.Second)
	assert.Empty(t, texts)
	assert.ErrorIs(t, rig.result.StopErr, ErrDaemonDisconnected)
	assert.Equal(t, 0, rig.reconnectAttempts(), "老 daemon 不该触发重连")
}

// Given 重连成功但这台 daemon 不认识 runtime.session.attach,When 补齐,
// Then 同样回落到断连即终止。
func TestReconnect_AttachMethodNotFound_FallsBackToEndingTurn(t *testing.T) {
	rig := newReconnectRig(t, true)
	conn2 := newFakeConn()
	conn2.script(func(method string, _, _ any) error {
		if method == wire.MethodSessionAttach {
			return &jsonrpc.Error{Code: jsonrpc.ErrMethodNotFound.Code, Message: "Method not found"}
		}
		return nil
	})
	rig.queue(conn2, rigFingerprint, nil)

	_ = rig.conn1.Close()

	_ = drainTexts(t, rig.events, 3*time.Second)
	assert.ErrorIs(t, rig.result.StopErr, ErrDaemonDisconnected)
}

// ── 重连超上限 ──────────────────────────────────────────────────────────────

// Given 重连一直失败,When 退避重试次数用尽,Then 才注入 ErrDaemonDisconnected。
func TestReconnect_AttemptsExhausted_InjectsDisconnected(t *testing.T) {
	rig := newReconnectRig(t, true)
	rig.queue(nil, "", assertErr("dial refused"))
	rig.queue(nil, "", assertErr("dial refused"))

	_ = rig.conn1.Close()

	_ = drainTexts(t, rig.events, 3*time.Second)
	assert.ErrorIs(t, rig.result.StopErr, ErrDaemonDisconnected)
	assert.Equal(t, 2, rig.reconnectAttempts(), "退避表有几档就试几次")
	assert.Equal(t, ConnStateLost, rig.rt.ConnState(rigSessionID))
}

// ── 接管可反复发起 ──────────────────────────────────────────────────────────

// Given 补齐后另一条同指纹连接把 daemon 侧的接管悄悄还原了,When 客户端提交一个
// 待决策,Then 客户端重新 attach 并重试,而不是把 ErrNoActiveTurn 交给上层。
func TestControlCall_NoActiveTurn_ReAttachesAndRetries(t *testing.T) {
	rig := newReconnectRig(t, true)
	var submits int
	rig.conn1.script(func(method string, _, _ any) error {
		if method == wire.MethodSubmitToolPermission {
			submits++
			if submits == 1 {
				return &jsonrpc.Error{Code: wire.ErrCodeNoActiveTurn, Message: "no active turn"}
			}
		}
		return nil
	})

	err := rig.rt.SubmitToolPermission(context.Background(), rigSessionID, "req-1", true, false, "")
	require.NoError(t, err, "接管被还原后应重新 attach 并重试,而不是把错误上抛")
	assert.Equal(t, 2, submits)
	assert.NotEmpty(t, rig.conn1.methodCalls(wire.MethodSessionAttach), "重试前必须重新接管")
}

// ── guard ───────────────────────────────────────────────────────────────────

// 补齐重放要把日志里不含 seq 的载荷解成帧、盖上 seq 再喂回 handler。少一个映射,
// 那一类通知就会在补齐时整条丢掉(解出 seq=0 → 不大于游标 → 丢弃),而实时路径
// 一切正常 —— 只有断连过才看得出来。所以两张表必须同集合。
func TestSeqStampersCoverEveryNotifyHandler(t *testing.T) {
	for method := range notifyHandlers {
		assert.Contains(t, seqStampers, method, "%s 缺少 method→帧 映射", method)
	}
	for method := range seqStampers {
		assert.Contains(t, notifyHandlers, method, "%s 有映射却没有 handler", method)
	}
}

// 盖 seq 必须保留载荷的其余字节:补齐与实时投递的必须是同一份内容。
func TestStampSeq_PreservesPayloadAndSetsSeq(t *testing.T) {
	ev, err := json.Marshal(agentruntime.TextDelta{Text: "x"})
	require.NoError(t, err)
	params, err := json.Marshal(wire.EventFrame{SessionID: 7, Event: ev})
	require.NoError(t, err)

	raw, err := stampSeq(wire.NotifyEvent, params, 11)
	require.NoError(t, err)

	var got wire.EventFrame
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, int64(7), got.SessionID)
	assert.Equal(t, int64(11), got.Seq)
	assert.JSONEq(t, string(ev), string(got.Event))
}

// Given 这一轮已经跑完(没有在飞会话),When 池化连接被 idle 回收,Then 不重连 ——
// 重连会再借一条谁也不会归还的连接,把套接字永久钉住,连接池的 idle 回收就此失效。
func TestDisconnect_NoLiveRun_DoesNotReconnect(t *testing.T) {
	rig := newReconnectRig(t, true)
	// 轮末终态帧:会话正常收尾,本地不再有在飞会话。
	rig.conn1.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{SessionID: rigSessionID, Seq: 1})
	_ = drainTexts(t, rig.events, time.Second)

	_ = rig.conn1.Close()

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, rig.reconnectAttempts(), "没有在飞会话就不该重连")
}

// Given 一轮 Run 已经收尾、而 CLI 发起的自主续轮(后台任务续轮)正在流,When 连接断开,
// Then 同样走退避重连与补齐:自主轮的 events 不在中途关闭,断连期间 daemon 已经落库的
// 尾巴照常补回来。
//
// 破法是用户可见的:events 被中途 close 而 result.StopErr 仍是 nil,chat_svc.
// driveAutonomousTurn 于是把一条**被截断的**助手消息当作正常完成的轮次持久化,
// 远端已经产出并落库的尾巴永远不会被回放。
func TestDisconnect_AutonomousTurnInFlight_ReconnectsInsteadOfTruncating(t *testing.T) {
	rig := newReconnectRig(t, true)
	turns := rig.rt.AutonomousTurns(rigSessionID)

	// 这一轮 Run 正常收尾 —— r.sessions 就此清空,只剩自主轮还在跑。
	rig.conn1.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{SessionID: rigSessionID, Seq: 1})
	_ = drainTexts(t, rig.events, time.Second)

	rig.conn1.deliver(t, wire.NotifyAutonomousTurnStarted, wire.AutonomousTurnStartedFrame{
		SessionID: rigSessionID, Trigger: "background_task", Seq: 2,
	})
	var turn agentruntime.AutonomousTurn
	select {
	case turn = <-turns:
	case <-time.After(time.Second):
		t.Fatal("自主轮没有交付给 watcher")
	}
	ev, err := json.Marshal(agentruntime.TextDelta{Text: "partial"})
	require.NoError(t, err)
	rig.conn1.deliver(t, wire.NotifyAutonomousTurnEvent, wire.EventFrame{
		SessionID: rigSessionID, Event: ev, Seq: 3,
	})

	conn2 := catchUpConn(5, []wire.JournaledNotification{
		journaledAutoEvent(4, "tail"),
		journaledAutoDone(5, "sonnet"),
	}, wire.SessionPendingWaitersResult{})
	rig.queue(conn2, rigFingerprint, nil)

	_ = rig.conn1.Close()

	texts := drainTexts(t, turn.Events, 3*time.Second)
	assert.Equal(t, []string{"partial", "tail"}, texts, "自主轮的尾巴要补回来,而不是被中途截断")
	assert.Equal(t, "sonnet", turn.Result.Model, "补齐到的终态帧要落进这一轮的 RunResult")
	assert.Positive(t, rig.reconnectAttempts(), "只有自主轮在跑时断连,同样要重连")
}

// Given 会话订阅过自主续轮、而此刻没有任何一轮在跑(上一轮已收到 done),When 池化连接
// 被 idle 回收,Then 不重连。
//
// autoSessions 的条目在订阅时创建、直到 conn close 才删,天真地按「有没有条目」设闸会让
// 运行时在空闲之后永远重连下去,每次都再借一条谁也不会归还的连接 —— 闸必须看的是那条
// 自主会话上有没有**在飞的一轮**。
func TestDisconnect_AutonomousTurnFinished_DoesNotReconnect(t *testing.T) {
	rig := newReconnectRig(t, true)
	turns := rig.rt.AutonomousTurns(rigSessionID)

	rig.conn1.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{SessionID: rigSessionID, Seq: 1})
	_ = drainTexts(t, rig.events, time.Second)
	rig.conn1.deliver(t, wire.NotifyAutonomousTurnStarted, wire.AutonomousTurnStartedFrame{
		SessionID: rigSessionID, Trigger: "background_task", Seq: 2,
	})
	turn := <-turns
	rig.conn1.deliver(t, wire.NotifyAutonomousTurnDone, wire.RunResultDoneFrame{
		SessionID: rigSessionID, Model: "sonnet", Seq: 3,
	})
	_ = drainTexts(t, turn.Events, time.Second)

	_ = rig.conn1.Close()

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, rig.reconnectAttempts(), "订阅本身不算在飞的一轮")
}

// Given 用户在重连期间解除了配对,When 重连端口宣告放弃,Then 立刻注入
// ErrDaemonDisconnected,而不是把会话在退避表上再吊几十秒 —— 凭据已经撤销,
// 重试多少次都是同一个结果。
func TestReconnect_PortAbandons_InjectsDisconnectedImmediately(t *testing.T) {
	rig := newReconnectRigWithBackoff(t, true, []time.Duration{time.Millisecond, time.Hour})
	rig.queue(nil, "", ErrReconnectAbandoned)

	_ = rig.conn1.Close()

	_ = drainTexts(t, rig.events, 3*time.Second)
	assert.ErrorIs(t, rig.result.StopErr, ErrDaemonDisconnected)
	assert.Equal(t, 1, rig.reconnectAttempts(), "宣告放弃后不该再退避重试")
}
