package claudecode

import (
	"agentre/internal/pkg/agentruntime"
)

// AutonomousTurns 实现 agentruntime.AutonomousTurnSource:把底层 claudecode.Session
// 的自主续轮(后台任务完成 CLI 自主跑的一轮)桥接成 agentruntime 事件流。
//
// 每个 AutoTurn 复用 drainStream(同 translator / control 协议 / tasks 聚合)。本桥接
// 按 AutoTurn 顺序 **inline** drain —— 自主轮之间不重叠。
//
// **刻意不调 active.setOut(evOut)**:自主轮的事件出口只走 drainStream 显式传入的
// evOut(同步 control_request 经 handleControlRequest(evOut) 仍能到达前端)。a.out 只
// 由 user turn 的 Run goroutine 写(且被 chat_svc 会话锁串行化);自主轮若也抢写
// a.out,则当 user turn 与自主轮在 runtime 层短暂重叠(用户在自主轮进行中又发消息,
// Session FIFO 已防错位但两个 drainStream goroutine 仍可并存)时会 data race。代价:
// 自主轮内若发生异步 tool-permission/ask 应答(emit 走 a.outChan),终态事件落到
// nil/陈旧 channel 被丢弃 → 该卡片不实时回显,靠下一轮 reloadSession 修正(罕见,
// 自主轮多在 acceptEdits 下自动放行)。
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
			var completed *agentruntime.CompletedBackgroundTask
			if at.CompletedTask != nil {
				completed = &agentruntime.CompletedBackgroundTask{
					ToolUseID: at.CompletedTask.ToolUseID,
					TaskID:    at.CompletedTask.TaskID,
					Status:    at.CompletedTask.Status,
					Summary:   at.CompletedTask.Summary,
				}
			}
			// 先把这一轮交给 consumer(它并发 drain evOut),随后 inline 翻译填 evOut。
			// inline(非 goroutine)保证多个自主轮之间顺序处理、不重叠。
			out <- agentruntime.AutonomousTurn{Events: evOut, Result: result, Trigger: at.Trigger, CompletedTask: completed}
			stream := &ccChanStream{ch: at.Events, sidFn: func() string { return at.SessionID }}
			drainStream(stream, evOut, result, a)
			if sid := stream.SessionID(); sid != "" {
				result.ProviderSessionID = sid
			}
			close(evOut)
		}
	}()
	return out
}
