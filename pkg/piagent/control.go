package piagent

import "context"

func (s *Stream) Interrupt(ctx context.Context) error {
	return s.send(ctx, map[string]any{"type": "abort"})
}

func (s *Stream) Steer(ctx context.Context, text string) error {
	return s.send(ctx, map[string]any{"type": "steer", "message": text})
}
