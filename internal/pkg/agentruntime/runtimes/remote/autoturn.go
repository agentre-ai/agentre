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
	turn := &autoTurn{
		events: make(chan agentruntime.Event, 64),
		result: &agentruntime.RunResult{},
	}
	logger.Ctx(ctx).Info("remote runtime: autonomous turn started",
		zap.Int64("sid", frame.SessionID), zap.String("trigger", frame.Trigger))
	// 持 a.mu 期间设 cur + 送 a.out —— 与 closeAllAutoSessions 的 close(a.out) 互斥,
	// 杜绝 daemon 断连(watchClose 独立 goroutine)恰在投递新一轮期间关 channel 时的
	// send-on-closed-channel panic。对齐 handleAutonomousTurnEvent 的同款纪律。
	// a.out 有缓冲且自主轮串行(daemon 任一时刻至多一轮、消费方 driveAutonomousTurn
	// 独立 drain),送出几乎不阻塞;缓冲满时短暂阻塞读循环(既定 back-pressure 契约),
	// 不与 a.mu 形成锁环。
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, nil
	}
	a.cur = turn
	a.out <- agentruntime.AutonomousTurn{
		Events:  turn.events,
		Result:  turn.result,
		Trigger: frame.Trigger,
	}
	return nil, nil
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
// 让 driveAutonomousTurn 收尾。幂等。
func (r *Runtime) closeAllAutoSessions() {
	r.mu.Lock()
	all := make([]*autoSession, 0, len(r.autoSessions))
	for sid, a := range r.autoSessions {
		all = append(all, a)
		delete(r.autoSessions, sid)
	}
	r.mu.Unlock()
	for _, a := range all {
		a.mu.Lock()
		if a.closed {
			a.mu.Unlock()
			continue
		}
		a.closed = true
		if a.cur != nil {
			close(a.cur.events)
			a.cur = nil
		}
		close(a.out)
		a.mu.Unlock()
	}
}
