package app

import (
	"context"
	"errors"
)

var errTerminalSvcNotInitialized = errors.New("terminal service not initialized")

// TerminalOpen opens a PTY for the given session. cols and rows set the
// initial terminal dimensions. The frontend should call TerminalClose when
// the panel is dismissed.
func (a *App) TerminalOpen(ctx context.Context, sessionID int64, cols, rows uint16) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	return a.terminalSvc.Open(ctx, sessionID, cols, rows)
}

// TerminalWrite sends input bytes (typically keystrokes) to the running PTY.
func (a *App) TerminalWrite(ctx context.Context, sessionID int64, data string) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	return a.terminalSvc.Write(ctx, sessionID, data)
}

// TerminalResize updates the PTY window dimensions (e.g. after the panel is
// resized by the user).
func (a *App) TerminalResize(ctx context.Context, sessionID int64, cols, rows uint16) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	return a.terminalSvc.Resize(ctx, sessionID, cols, rows)
}

// TerminalClose terminates the PTY process and releases resources.
func (a *App) TerminalClose(ctx context.Context, sessionID int64) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	return a.terminalSvc.Close(ctx, sessionID)
}
