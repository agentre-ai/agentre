package remote

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/pkg/jsonrpc"
)

// ── rig:App 刚启动、本进程内一轮都没有在跑 ─────────────────────────────────

// restartConn 造一条「App 刚重启后新连上」的连接:session.list 交出该 daemon 上的
// 会话清单,attach / pull / pendingWaiters 按补齐三步应答。
func restartConn(summaries []wire.SessionSummary, journal []wire.JournaledNotification) *fakeConn {
	c := newFakeConn()
	latest := map[int64]int64{}
	lifecycle := map[int64]string{}
	for _, s := range summaries {
		latest[s.SessionID] = s.LatestSeq
		lifecycle[s.SessionID] = s.LifecycleState
	}
	c.script(func(method string, params, result any) error {
		switch method {
		case wire.MethodSessionList:
			*(result.(*wire.SessionListResult)) = wire.SessionListResult{Sessions: summaries}
		case wire.MethodSessionAttach:
			p := params.(wire.SessionAttachParams)
			*(result.(*wire.SessionAttachResult)) = wire.SessionAttachResult{
				SessionID:      p.SessionID,
				LifecycleState: lifecycle[p.SessionID],
				LatestSeq:      latest[p.SessionID],
			}
		case wire.MethodSessionPull:
			p := params.(wire.SessionPullParams)
			out := wire.SessionPullResult{Cursor: p.Cursor}
			for _, n := range journal {
				// 现存最老的一行:真 daemon 报的是 MIN(seq),被回收掉的前缀不在里面。
				if out.OldestSeq == 0 || n.Seq < out.OldestSeq {
					out.OldestSeq = n.Seq
				}
				if n.Seq > p.Cursor {
					out.Notifications = append(out.Notifications, n)
					out.Cursor = n.Seq
				}
			}
			*(result.(*wire.SessionPullResult)) = out
		case wire.MethodSessionPendingWaiters:
			*(result.(*wire.SessionPendingWaitersResult)) = wire.SessionPendingWaitersResult{}
		}
		return nil
	})
	return c
}

// newRestartRuntime 起一个「App 刚启动」的 runtime:没有 Run 过任何一轮,只有一份
// 存活下来的游标。
func newRestartRuntime(t *testing.T, conn *fakeConn, cursorAt int64) (*Runtime, *fakeCursorPort, *connStateRecorder) {
	t.Helper()
	cursor := &fakeCursorPort{}
	cursor.setLoad(func(int64, string) (int64, bool, error) { return cursorAt, true, nil })
	obs := &connStateRecorder{}
	rt := New(conn,
		WithReconnect(ReconnectFunc(func(context.Context) (agentruntime.DaemonClientPort, string, error) {
			return nil, "", ErrReconnectAbandoned
		})),
		WithDaemonFingerprint(rigFingerprint),
		WithSessionCursor(cursor),
		WithConnStateObserver(obs),
		WithReconnectBackoff([]time.Duration{time.Millisecond}),
		WithCursorFlushInterval(0),
	)
	t.Cleanup(func() { _ = rt.Close() })
	return rt, cursor, obs
}

// takeTurn 取下一轮合成/自主轮;没有就让用例失败(而不是永久阻塞)。
func takeTurn(t *testing.T, turns <-chan agentruntime.AutonomousTurn) agentruntime.AutonomousTurn {
	t.Helper()
	select {
	case at, ok := <-turns:
		require.True(t, ok, "会话的轮次流被关掉了")
		return at
	case <-time.After(2 * time.Second):
		t.Fatal("等不到轮次:补齐到的内容没有落点")
		return agentruntime.AutonomousTurn{}
	}
}

// ── F6:App 重启后的补齐 ────────────────────────────────────────────────────

// Given 桌面 App 退出后重开,本进程内一轮都没有在跑,而 daemon 上这条会话在这段时间
// 里又跑完了一整轮,When 按 exec_device_id 重新连上同一台 daemon 并对它跑补齐三步,
// Then 断连期间产生的通知按序重放,并落成一条**不由用户发起**的轮次交给上层持久化。
//
// 这一条就是本轮的头号用户故事:「退出桌面 App 后下次打开看到这段时间发生的全部
// 内容」。在此之前 catchUpAll 只遍历本进程内在飞的轮次,而 App 刚启动时那个集合必然
// 是空的 —— 补齐一条都不会发起,重放上来的内容也没有任何落点。
func TestCatchUpSessions_NoLiveRun_ReplaysIntoSynthesizedTurn(t *testing.T) {
	journal := []wire.JournaledNotification{
		journaledEvent(4, "away-a"),
		journaledEvent(5, "away-b"),
		journaledDone(6, "sonnet"),
	}
	conn := restartConn([]wire.SessionSummary{{
		SessionID:      rigSessionID,
		LifecycleState: wire.SessionLifecycleIdle,
		LatestSeq:      6,
	}}, journal)
	rt, cursor, _ := newRestartRuntime(t, conn, 3)

	turns := rt.AutonomousTurns(rigSessionID)
	_, err := rt.CatchUpSessions(context.Background(), []int64{rigSessionID})
	require.NoError(t, err)

	at := takeTurn(t, turns)
	assert.Equal(t, TriggerCatchUp, at.Trigger,
		"重放上来的这一轮不是用户发起的,上层要据此把它落成纯 assistant 轮")
	assert.Equal(t, []string{"away-a", "away-b"}, drainTexts(t, at.Events, 2*time.Second))
	assert.Equal(t, "sonnet", at.Result.Model, "终态帧的结果要落在这一轮上")

	assert.Equal(t, []string{
		wire.MethodSessionAttach, wire.MethodSessionPull, wire.MethodSessionPendingWaiters,
	}, conn.catchUpOrder(), "补齐三步的顺序是硬的")
	require.Len(t, conn.methodCalls(wire.MethodSessionList), 1,
		"清单是第一步,每次补齐只问一次(不是每条会话问一次)")

	saved, ok := cursor.lastSaved()
	require.True(t, ok)
	assert.Equal(t, int64(6), saved.Seq, "补齐完游标要推到日志末尾,否则下次开 App 再重放一遍")
}

// Given 补齐把一条会话补完,而 daemon 说它已经空闲(此后不会再有推送),When 补齐返回,
// Then 补齐登记与自主轮镜像一并收摊:轮次流关掉(上层 watcher goroutine 随之退出)、
// liveSessionIDs 不再含它、hasLiveRun 归假。
//
// 不收摊的代价按会话数累加,且到进程退出都不还:
//   - tracked 只在 failSession 删,于是 hasLiveRun() 永远为真 —— 此后每次断连都要为
//     早就结束的会话跑完整退避 + 重接管;
//   - autoSessions 条目只由 closeAllAutoSessions 关,于是每条补齐过的会话留下一个
//     常驻 goroutine,还撑大 liveSessionIDs(),让之后每次重连把它们全部重新接管。
func TestCatchUpSessions_FinishedSession_ReleasesTrackingAndWatcher(t *testing.T) {
	journal := []wire.JournaledNotification{
		journaledEvent(4, "away-a"),
		journaledDone(5, "sonnet"),
	}
	conn := restartConn([]wire.SessionSummary{{
		SessionID:      rigSessionID,
		LifecycleState: wire.SessionLifecycleIdle,
		LatestSeq:      5,
	}}, journal)
	rt, _, _ := newRestartRuntime(t, conn, 3)

	turns := rt.AutonomousTurns(rigSessionID)
	live, err := rt.CatchUpSessions(context.Background(), []int64{rigSessionID})
	require.NoError(t, err)
	require.Empty(t, live)

	at := takeTurn(t, turns)
	assert.Equal(t, []string{"away-a"}, drainTexts(t, at.Events, 2*time.Second),
		"收摊不能吃掉补齐到的内容")

	select {
	case _, ok := <-turns:
		assert.False(t, ok, "轮次流必须关掉,上层的 watcher goroutine 才收得了工")
	case <-time.After(2 * time.Second):
		t.Fatal("轮次流没关:每条补齐过的会话都留下一个常驻 goroutine")
	}
	assert.Empty(t, rt.liveSessionIDs(),
		"补完就结束的会话不该再进重连接管的名单")
	assert.False(t, rt.hasLiveRun(),
		"手上一轮都没有了:再断连不值得重连,否则会借回一条谁也不会归还的连接")
}

// Given daemon 说这条会话还在跑,When 补齐跑完,Then 登记与镜像都留着 —— 它接下来的
// 推送要落在这里,断一次线也必须重连接回去。收摊只针对已经结束的那些。
func TestCatchUpSessions_StillRunningSession_KeepsTrackingForReconnect(t *testing.T) {
	conn := restartConn([]wire.SessionSummary{{
		SessionID:      rigSessionID,
		LifecycleState: wire.SessionLifecycleRunning,
		LatestSeq:      3,
	}}, nil)
	rt, _, _ := newRestartRuntime(t, conn, 3)

	turns := rt.AutonomousTurns(rigSessionID)
	live, err := rt.CatchUpSessions(context.Background(), []int64{rigSessionID})
	require.NoError(t, err)
	assert.Equal(t, []int64{rigSessionID}, live)

	select {
	case _, ok := <-turns:
		t.Fatalf("还在跑的会话不能收摊(closed=%v):它接下来的推送就没有落点了", !ok)
	case <-time.After(50 * time.Millisecond):
	}
	assert.Equal(t, []int64{rigSessionID}, rt.liveSessionIDs())
	assert.True(t, rt.hasLiveRun())
}

// Given daemon 交回的会话清单里,一条会话的最新 seq 与本地游标相等(断连期间它什么
// 都没产生),另一条落下了一大段,而本地还认得第三条根本不在这台 daemon 上,
// When 跑补齐,Then 只有真正落下内容的那条才发起 attach + pull。
//
// 清单交回的 SessionSummary 在此之前被整个丢掉。它的用处正是这个:重启后本地可能有
// 几十条老会话记着同一台 daemon,逐条 attach + pull 是几十次白跑的往返,而清单一次
// 就把「谁落下了内容」问清楚了;不在清单里的那条更是连 attach 都不该发 —— 它要么
// 已被 daemon 的重启清扫掉,要么压根属于别的对端。
func TestCatchUpSessions_SessionListDecidesWhoNeedsPulling(t *testing.T) {
	const (
		idleQuiet int64 = 42 // 空闲且已追平:一次往返都不必花
		liveQuiet int64 = 43 // 已追平,但还在跑:必须接管,否则它接下来的通知没有推送目标
		behind    int64 = 44 // 落下了一整段
		unknown   int64 = 45 // 本地还认得,daemon 上已经没有了
	)
	conn := restartConn([]wire.SessionSummary{
		{SessionID: idleQuiet, LifecycleState: wire.SessionLifecycleIdle, LatestSeq: 3},
		{SessionID: liveQuiet, LifecycleState: wire.SessionLifecycleRunning, LatestSeq: 3},
		{SessionID: behind, LifecycleState: wire.SessionLifecycleIdle, LatestSeq: 9},
	}, nil)
	rt, _, _ := newRestartRuntime(t, conn, 3)

	live, err := rt.CatchUpSessions(context.Background(),
		[]int64{idleQuiet, liveQuiet, behind, unknown})
	require.NoError(t, err)
	assert.Equal(t, []int64{liveQuiet}, live,
		"交回的 live 只含 daemon 说还在跑的那条 —— 调用方据它判断本地哪些 running 行是重启遗孤")

	calls := conn.methodCalls(wire.MethodSessionAttach)
	attached := make([]int64, 0, len(calls))
	for _, c := range calls {
		attached = append(attached, c.Params.(wire.SessionAttachParams).SessionID)
	}
	assert.Equal(t, []int64{liveQuiet, behind}, attached,
		"空闲且追平的会话与不在清单里的会话都不该发 attach;还在跑的必须接管推送流")
}

// Given 对面是不认识补齐族 RPC 的老 daemon,When App 启动后发起补齐,
// Then 立刻判定不支持并交出哨兵,不再逐条 attach(R18)。
func TestCatchUpSessions_OldDaemon_ReportsUnsupported(t *testing.T) {
	conn := newFakeConn()
	conn.script(func(method string, _, _ any) error {
		if method == wire.MethodSessionList {
			return &jsonrpc.Error{Code: jsonrpc.ErrMethodNotFound.Code, Message: "Method not found"}
		}
		return nil
	})
	rt, _, _ := newRestartRuntime(t, conn, 3)

	_, err := rt.CatchUpSessions(context.Background(), []int64{rigSessionID})
	require.ErrorIs(t, err, errCatchUpUnsupported)
	assert.Empty(t, conn.methodCalls(wire.MethodSessionAttach),
		"老 daemon 上不该继续发补齐族的其余方法")
}

// Given 一段补齐正在写进合成轮,When 重放到一条 autonomousTurn.started(daemon 在
// 那之后自主起了新的一轮),Then 先把手上这一轮收尾再开新的。
//
// 这是 F2 留下的第二个缺口:handleAutonomousTurnStarted 无条件覆盖 a.cur。旧的那轮
// events 因此永远关不掉 —— 上层 driveAutonomousTurn 卡在 `for ev := range at.Events`
// 上不返回,它那条 assistant 消息永久停在 running,界面上就是一张永远转圈的卡片,
// 旁边又多出一张新卡片。App 重启后「回放一整段历史」变成常态,这一幕随之从边角变成
// 常见路径。
func TestAutonomousTurnStarted_ClosesTheOpenTurnFirst(t *testing.T) {
	conn := newFakeConn()
	rt, _, _ := newRestartRuntime(t, conn, 0)
	turns := rt.AutonomousTurns(rigSessionID)

	// 一条带 seq、却不属于本进程任何在飞轮次的事件 —— 它开出一轮合成轮。
	ev, err := json.Marshal(agentruntime.TextDelta{Text: "replayed"})
	require.NoError(t, err)
	conn.deliver(t, wire.NotifyEvent, wire.EventFrame{SessionID: rigSessionID, Event: ev, Seq: 1})
	first := takeTurn(t, turns)

	conn.deliver(t, wire.NotifyAutonomousTurnStarted, wire.AutonomousTurnStartedFrame{
		SessionID: rigSessionID, Trigger: "background_task", Seq: 2,
	})
	second := takeTurn(t, turns)

	assert.Equal(t, []string{"replayed"}, drainTexts(t, first.Events, 2*time.Second),
		"手上那一轮必须被收尾,否则它的消费方永远不返回")
	assert.Equal(t, "background_task", second.Trigger)
	assert.NotEqual(t, TriggerCatchUp, second.Trigger)
}

// Given 客户端离线的时间超过了 daemon 的整个日志留存窗口,它游标之后的那截尾巴已经被
// 回收(daemon.collectJournal 只保住高水位那一行),When 它回来补齐,
// Then 按 daemon 交回的「现存最老 seq」把游标复位,现存的那段照常重放进转录。
//
// 不复位的后果不是「少几条」而是这条会话就此静默冻住:每一页的第一条都比 游标+1 大,
// 被 dispatchNotification 判成跳号丢弃并触发补洞拉取,补洞又原样拉回同一页 —— 游标
// 永远停在那个已经不存在的位置,此后连实时通知也全被当成跳号,没有错误、没有跳号,
// 会话就是再也不出字(与 8496c291 修的越界冻结同类)。
func TestCatchUpSessions_ReclaimedPrefix_ResetsCursorInsteadOfFreezing(t *testing.T) {
	// 日志曾经是 seq 1..11,留存回收掉了 10 以下的前缀:现存最老的一行是 10。
	journal := []wire.JournaledNotification{
		journaledEvent(10, "survivor"),
		journaledDone(11, "sonnet"),
	}
	conn := restartConn([]wire.SessionSummary{{
		SessionID:      rigSessionID,
		LifecycleState: wire.SessionLifecycleIdle,
		LatestSeq:      11,
	}}, journal)
	// 游标停在 7:那之后的 8、9 已经随留存窗口一起没了。
	rt, cursor, _ := newRestartRuntime(t, conn, 7)

	turns := rt.AutonomousTurns(rigSessionID)
	_, err := rt.CatchUpSessions(context.Background(), []int64{rigSessionID})
	require.NoError(t, err)

	at := takeTurn(t, turns)
	assert.Equal(t, []string{"survivor"}, drainTexts(t, at.Events, 2*time.Second),
		"回收掉的那截拿不回来了,但**现存**的那段必须照常进转录")

	saved, ok := cursor.lastSaved()
	require.True(t, ok)
	assert.Equal(t, int64(11), saved.Seq,
		"游标要推到日志末尾,否则下一条实时通知仍会被判成跳号,会话继续冻着")
}

// Given daemon 进程在桌面端离线期间重启过,把这条会话按 R10 标成了中断态,
// When App 重开后补齐,Then 中断前的历史仍然补回来,但不发 attach(已经没有推送流可
// 接管),补完把会话按**被打断**收尾。
//
// 规格对中断态的要求是「历史可读、不可续跑」:只标终态而不补历史,用户丢的正是
// daemon 挂掉之前那一段——恰恰是最需要看到的那段。终止理由用 ErrRunInterrupted 而不
// 是 ErrDaemonDisconnected:连接明明是通的,说「连不上了」是错的(R15 要求这两种情形
// 由文案区分,而文案的唯一依据就是这个哨兵)。
func TestCatchUpSessions_InterruptedSession_ReadsHistoryThenEndsAsInterrupted(t *testing.T) {
	conn := restartConn([]wire.SessionSummary{{
		SessionID:      rigSessionID,
		LifecycleState: wire.SessionLifecycleInterrupted,
		LatestSeq:      5,
	}}, []wire.JournaledNotification{
		journaledEvent(4, "before-the-crash"),
		journaledEvent(5, "and-then-nothing"),
	})
	rt, _, obs := newRestartRuntime(t, conn, 3)

	turns := rt.AutonomousTurns(rigSessionID)
	_, err := rt.CatchUpSessions(context.Background(), []int64{rigSessionID})
	require.NoError(t, err)

	assert.Empty(t, conn.methodCalls(wire.MethodSessionAttach),
		"中断态会话没有推送流可接管")
	assert.NotEmpty(t, conn.methodCalls(wire.MethodSessionPull), "中断前的历史仍要补回来")

	at := takeTurn(t, turns)
	assert.Equal(t, []string{"before-the-crash", "and-then-nothing"},
		drainTexts(t, at.Events, 2*time.Second))
	require.ErrorIs(t, at.Result.StopErr, ErrRunInterrupted,
		"「被打断」与「连不上了」必须分得开 —— 文案的唯一依据就是这个哨兵")

	states := obs.states(rigSessionID)
	require.NotEmpty(t, states)
	assert.Equal(t, ConnStateLost, states[len(states)-1].State)
}
