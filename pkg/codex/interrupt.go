package codex

import "context"

// Interrupt 通过 turn/interrupt RPC 让 app server 终止当前 turn。
// 服务端会随后发出 status=interrupted 的 turn/completed 帧，drain 端 Stream.Next
// 看到后正常返 false —— **子进程保留**，调用方可以紧接着发下一条 turn。
//
// codex-cli 0.145.0 TurnInterruptParams is exactly threadId + turnId.
// turnID 已结束 / 不匹配时返 ErrNoActiveTurn（沿用 isNoActiveTurnSteerError 的判定）。
func (s *Stream) Interrupt(ctx context.Context) error {
	if s == nil {
		return ErrNoActiveTurn
	}
	s.mu.RLock()
	threadID := s.sessionID
	turnID := s.turnID
	app := s.app
	s.mu.RUnlock()
	if threadID == "" || turnID == "" || app == nil {
		return ErrNoActiveTurn
	}
	changed, transitionErr := s.beginInterrupt()
	if transitionErr != nil {
		return ErrNoActiveTurn
	}
	if !changed {
		return nil
	}
	s.signalInterrupt()
	_, err := app.Call(ctx, appMethodTurnInterrupt, map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
	})
	if isNoActiveTurnSteerError(err) {
		return ErrNoActiveTurn
	}
	if err != nil {
		return err
	}
	return nil
}
