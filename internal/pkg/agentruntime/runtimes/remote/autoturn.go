package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// autoSession 是某 chat session 的「自主续轮」(AutonomousTurnSource)本地镜像。
// out 是 AutonomousTurns() 返回给 chat_svc watcher 的 channel;cur 是当前在飞的一轮
// (daemon 串行转发,任一时刻至多一轮)。按 sessionID 持久,跨 turn / 子进程 evict 复用,
// conn close 时由 closeAllAutoSessions 统一拆。
type autoSession struct {
	id  int64
	out chan agentruntime.AutonomousTurn

	mu     sync.Mutex
	cur    *autoTurn
	closed bool
}

// autoTurn 一轮自主续轮:events 是事件流(daemon 的 AutonomousTurnEvent 路由进来),
// result 在 Done 帧到达时填好、events close 前可见(与 Run 的 RunResult 契约一致)。
type autoTurn struct {
	events chan agentruntime.Event
	result *agentruntime.RunResult
	// catchUp 这一轮是**补齐合成**的:内容来自 daemon 通知日志的重放,不是 daemon
	// 宣告的自主续轮。两者对上层是同一种东西(一轮没有 user 行的 assistant 轮),
	// 差别只在被别的一轮顶掉时该不该算作「被打断」——补齐轮的内容按重放区间天然
	// 完整,自主续轮被顶掉则是真的没跑完。
	catchUp bool
}

// openTurnLocked 在该会话上开一轮并投给消费方。调用方必须持 a.mu。
//
// 手上已有在飞的一轮时**先收尾它**:少了这一步,a.cur 被无声覆盖,旧那一轮的 events
// 永远关不掉 —— 上层 driveAutonomousTurn 卡在 `for ev := range at.Events` 上不返回,
// 它那条 assistant 消息永久停在 running(界面上一张永远转圈的卡片),旁边又多出一张
// 新卡片。App 重启后「回放一整段历史」是常态,这一幕随之从边角变成常见路径。
//
// a.out 有缓冲且轮次串行(daemon 任一时刻至多一轮、消费方独立 drain),送出几乎不
// 阻塞;缓冲满时短暂阻塞读循环(既定 back-pressure 契约),不与 a.mu 形成锁环。
func (a *autoSession) openTurnLocked(trigger string, catchUp bool, turnToken uint64) *autoTurn {
	if a.closed {
		return nil
	}
	if a.cur != nil {
		// 补齐轮的内容按重放区间天然完整,被下一轮顶掉不是「被打断」;自主续轮的
		// done 没到就被顶掉,才是真的截断,理由必须带出去,否则上层会把一条半截的
		// 回答按「正常跑完」落库。
		var cause error
		if !a.cur.catchUp {
			cause = ErrRunInterrupted
		}
		a.finishCurLocked(cause)
	}
	turn := &autoTurn{
		events:  make(chan agentruntime.Event, 64),
		result:  &agentruntime.RunResult{},
		catchUp: catchUp,
	}
	a.cur = turn
	a.out <- agentruntime.AutonomousTurn{
		Events:    turn.events,
		Result:    turn.result,
		Trigger:   trigger,
		TurnToken: turnToken,
	}
	return turn
}

// finishCurLocked 收尾手上这一轮:写终止理由(已经有理由时不覆盖)、close events、
// 腾出 a.cur。调用方必须持 a.mu。
func (a *autoSession) finishCurLocked(cause error) {
	cur := a.cur
	a.cur = nil
	if cur == nil {
		return
	}
	if cause != nil && cur.result != nil && cur.result.StopErr == nil {
		cur.result.StopErr = cause
	}
	close(cur.events)
}

// AutonomousTurns 实现 agentruntime.AutonomousTurnSource:返回该 session 的自主续轮
// channel。惰性创建 autoSession —— 即便 Started 帧先于本调用到达(理论上不会,自主轮
// 总在 Run 收尾后才发),handleAutonomousTurnStarted 也会把同一个 autoSession 建好,
// 两边拿到同一个 out。
func (r *Runtime) AutonomousTurns(sessionID int64) <-chan agentruntime.AutonomousTurn {
	return r.getOrCreateAutoSession(sessionID).out
}

func (r *Runtime) getOrCreateAutoSession(sid int64) *autoSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.autoSessions[sid]; ok {
		return a
	}
	a := &autoSession{id: sid, out: make(chan agentruntime.AutonomousTurn, 4)}
	r.autoSessions[sid] = a
	return a
}

func (r *Runtime) lookupAutoSession(sid int64) *autoSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.autoSessions[sid]
}

func (r *Runtime) handleAutonomousTurnStarted(ctx context.Context, raw json.RawMessage) (any, error) {
	var frame wire.AutonomousTurnStartedFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		logger.Ctx(ctx).Warn("remote runtime: autonomousTurn.started unmarshal failed", zap.Error(err))
		return nil, nil
	}
	a := r.getOrCreateAutoSession(frame.SessionID)
	logger.Ctx(ctx).Info("remote runtime: autonomous turn started",
		zap.Int64("sid", frame.SessionID), zap.String("trigger", frame.Trigger))
	// 持 a.mu 期间设 cur + 送 a.out —— 与 closeAllAutoSessions 的 close(a.out) 互斥,
	// 杜绝 daemon 断连(watchClose 独立 goroutine)恰在投递新一轮期间关 channel 时的
	// send-on-closed-channel panic。对齐 handleAutonomousTurnEvent 的同款纪律。
	a.mu.Lock()
	defer a.mu.Unlock()
	a.openTurnLocked(frame.Trigger, false, frame.TurnToken)
	return nil, nil
}

// deliverToCatchUpTurn 把一条**没有在飞轮次可收**的事件交给该会话的补齐轮(没有就
// 现开一轮)。返回是否收下了。
//
// 这是补齐重放在「本进程内一轮都没在跑」时的唯一落点:App 重启后既没有 Run 起来的
// 会话、也没有 events channel,而重放的内容必须进得了转录。合成一轮交给上层,是因为
// 上层已经有一条把「没有 user 行的一轮」落成 assistant 消息的成熟路径
// (chat_svc.driveAutonomousTurn),补齐不必再造第二套持久化。
//
// 手上已经有在飞的一轮(自主续轮,或上一段补齐开的)就直接投进去:内容落在用户看得
// 见的那张卡片上,总好过丢掉。
func (r *Runtime) deliverToCatchUpTurn(ctx context.Context, sid int64, ev agentruntime.Event) bool {
	a := r.getOrCreateAutoSession(sid)
	// 送出与 close 都在 a.mu 下,理由同 handleAutonomousTurnEvent。
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	if a.cur == nil {
		logger.Ctx(ctx).Info("remote runtime: opening catch-up turn for replayed content",
			zap.Int64("sid", sid))
		if a.openTurnLocked(TriggerCatchUp, true, 0) == nil {
			return false
		}
	}
	a.cur.events <- ev
	return true
}

// closeCatchUpTurn 收尾补齐轮:重放到一条不属于任何在飞轮次的终态帧,说明这一段
// 补齐所对应的那一轮到此为止,把终态写进它的 RunResult 再 close。
//
// 只收补齐轮:在飞的自主续轮有它自己的 autonomousTurn.done 收尾,拿一条 per-Run 的
// 终态帧把它关掉会让它凭空少半截。
func (r *Runtime) closeCatchUpTurn(ctx context.Context, frame wire.RunResultDoneFrame) {
	a := r.lookupAutoSession(frame.SessionID)
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cur == nil || !a.cur.catchUp {
		return
	}
	a.cur.result.ProviderSessionID = frame.ProviderSessionID
	a.cur.result.UserAnchor = frame.UserAnchor
	a.cur.result.Model = frame.Model
	a.cur.result.ContextWindow = frame.ContextWindow
	a.cur.result.TurnToken = frame.TurnToken
	if frame.Usage != nil {
		a.cur.result.Usage = usageFromWire(frame.Usage)
	}
	a.cur.result.StopErr = stopErrFromFrame(frame)
	logger.Ctx(ctx).Info("remote runtime: catch-up turn finished",
		zap.Int64("sid", frame.SessionID), zap.String("model", frame.Model))
	a.finishCurLocked(nil)
}

func (r *Runtime) handleAutonomousTurnEvent(ctx context.Context, raw json.RawMessage) (any, error) {
	var frame wire.EventFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		logger.Ctx(ctx).Warn("remote runtime: autonomousTurn.event unmarshal failed", zap.Error(err))
		return nil, nil
	}
	a := r.lookupAutoSession(frame.SessionID)
	if a == nil {
		return nil, nil
	}
	ev, err := agentruntime.UnmarshalEvent(frame.Event)
	if err != nil {
		logger.Ctx(ctx).Warn("remote runtime: autonomousTurn UnmarshalEvent failed — dropped",
			zap.Int64("sid", frame.SessionID), zap.Error(err))
		return nil, nil
	}
	// 持 a.mu 期间送 —— 与 closeAllAutoSessions 的 close(cur.events) 互斥,杜绝
	// daemon 断连(watchClose 独立 goroutine)恰在投递期间关 channel 时的
	// send-on-closed-channel panic。对齐 per-Run 的 handleEvent 同款纪律。
	// consumer(driveAutonomousTurn)独立 drain,缓冲满时这里短暂阻塞读循环(本就是
	// 既定 back-pressure 契约),不与 a.mu 形成锁环。
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.cur == nil {
		logger.Ctx(ctx).Warn("remote runtime: autonomousTurn event with no active turn — dropped",
			zap.Int64("sid", frame.SessionID), zap.String("eventType", fmt.Sprintf("%T", ev)))
		return nil, nil
	}
	a.cur.events <- ev
	return nil, nil
}

func (r *Runtime) handleAutonomousTurnDone(ctx context.Context, raw json.RawMessage) (any, error) {
	var frame wire.RunResultDoneFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		logger.Ctx(ctx).Warn("remote runtime: autonomousTurn.done unmarshal failed", zap.Error(err))
		return nil, nil
	}
	a := r.lookupAutoSession(frame.SessionID)
	if a == nil {
		return nil, nil
	}
	a.mu.Lock()
	cur := a.cur
	a.cur = nil
	a.mu.Unlock()
	if cur == nil {
		return nil, nil
	}
	cur.result.ProviderSessionID = frame.ProviderSessionID
	cur.result.Model = frame.Model
	cur.result.ContextWindow = frame.ContextWindow
	cur.result.TurnToken = frame.TurnToken
	if frame.Usage != nil {
		cur.result.Usage = usageFromWire(frame.Usage)
	}
	cur.result.StopErr = stopErrFromFrame(frame)
	logger.Ctx(ctx).Info("remote runtime: autonomous turn done",
		zap.Int64("sid", frame.SessionID), zap.String("model", frame.Model))
	close(cur.events)
	return nil, nil
}

// closeAllAutoSessions 在 conn close(watchClose)时把所有自主续轮镜像拆掉:
// close 每个 out → chat_svc watcher 的 `for range` 退出;在飞的那轮 events 也 close,
// 让 driveAutonomousTurn 收尾。cause 是这一轮的终止理由,见 closeAutoSession。幂等。
func (r *Runtime) closeAllAutoSessions(cause error) {
	r.mu.Lock()
	all := make([]*autoSession, 0, len(r.autoSessions))
	for sid, a := range r.autoSessions {
		all = append(all, a)
		delete(r.autoSessions, sid)
	}
	r.mu.Unlock()
	for _, a := range all {
		closeAutoSession(a, cause)
	}
}

// closeAutoSession 拆掉一个自主续轮镜像。幂等。
//
// cause 是**在飞那一轮**的终止理由(ErrRunInterrupted / ErrDaemonDisconnected),写进
// 它的 RunResult.StopErr —— 与 per-Run 的 closeSessionWithErr 同一条纪律。少了它,
// chat_svc.driveAutonomousTurn 会把一条被截断的助手消息当作正常跑完的轮次落库:
// errorText 空、会话翻 idle,用户看到一条戛然而止却「成功」的回答。
// 终态帧已经到过(handleAutonomousTurnDone 填过 StopErr)时不覆盖。
func closeAutoSession(a *autoSession, cause error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.closed = true
	a.finishCurLocked(cause)
	close(a.out)
}
