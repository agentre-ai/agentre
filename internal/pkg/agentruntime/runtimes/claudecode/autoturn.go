package claudecode

import (
	"agentre/internal/pkg/agentruntime"
)

// AutonomousTurns 实现 agentruntime.AutonomousTurnSource:把底层 claudecode.Session
// 的自主续轮(后台任务完成 CLI 自主跑的一轮)桥接成 agentruntime 事件流。
//
// 每个 AutoTurn 复用 drainStream(同 translator / control 协议 / tasks 聚合)。本桥接
// 按 AutoTurn 顺序 **inline** drain —— 自主轮之间不重叠;与 user turn 的 Run 之间则由
// 调用方(chat_svc 会话级 turn 锁)串行化,避免抢写 active.out(见接口约束)。
//
// sessionID 未 spawn / 已 evict → 返回一个立即 close 的 channel。子进程退出时底层
// AutonomousTurns channel close,本 channel 随之 close。
func (r *Runtime) AutonomousTurns(sessionID int64) <-chan agentruntime.AutonomousTurn {
	out := make(chan agentruntime.AutonomousTurn, 4)
	v, ok := r.cache.Get(sessionKey(sessionID))
	if !ok {
		close(out)
		return out
	}
	a, ok := v.(*claudeActive)
	if !ok || a.handle == nil {
		close(out)
		return out
	}
	src := a.handle.AutonomousTurns()
	if src == nil {
		close(out)
		return out
	}
	go func() {
		defer close(out)
		for at := range src {
			evOut := make(chan agentruntime.Event, 32)
			result := &agentruntime.RunResult{ProviderSessionID: at.SessionID}
			// 先把这一轮交给 consumer(它并发 drain evOut),随后 inline 翻译填 evOut。
			// inline(非 goroutine)保证多个自主轮之间不重叠抢 active.out。
			out <- agentruntime.AutonomousTurn{Events: evOut, Result: result, Trigger: at.Trigger}
			a.setOut(evOut)
			stream := &ccChanStream{ch: at.Events, sidFn: func() string { return at.SessionID }}
			drainStream(stream, evOut, result, a)
			a.clearOut()
			if sid := stream.SessionID(); sid != "" {
				result.ProviderSessionID = sid
			}
			close(evOut)
		}
	}()
	return out
}
