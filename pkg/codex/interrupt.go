package codex

import "context"

// Interrupt 通过 turn/interrupt RPC 让 app server 终止当前 turn。
// 服务端会随后发出 status=interrupted 的 turn/completed 帧，drain 端 Stream.Next
// 看到后正常返 false —— **子进程保留**，调用方可以紧接着发下一条 turn。
//
// 当前 app-server 要求 threadId / turnId；保留 expectedTurnId 兼容旧版协议。
// turnID 已结束 / 不匹配时返 ErrNoActiveTurn（沿用 isNoActiveTurnSteerError 的判定）。
func (s *Stream) Interrupt(ctx context.Context) error {
	s.mu.RLock()
	threadID := s.sessionID
	turnID := s.turnID
	app := s.app
	s.mu.RUnlock()
	if threadID == "" || turnID == "" || app == nil {
		return ErrNoActiveTurn
	}
	_, err := app.Call(ctx, appMethodTurnInterrupt, map[string]any{
		"threadId":       threadID,
		"turnId":         turnID,
		"expectedTurnId": turnID,
	})
	if isNoActiveTurnSteerError(err) {
		return ErrNoActiveTurn
	}
	return err
}
