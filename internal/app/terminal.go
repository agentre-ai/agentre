package app

import "errors"

var errTerminalSvcNotInitialized = errors.New("terminal service not initialized")

// TerminalOpen opens a PTY for the given session. cols and rows set the
// initial terminal dimensions. The frontend should call TerminalClose when
// the panel is dismissed.
func (a *App) TerminalOpen(sessionID int64, cols, rows uint16) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	return a.terminalSvc.Open(a.ctx, sessionID, cols, rows)
}

// TerminalWrite sends input bytes (typically keystrokes) to the running PTY.
func (a *App) TerminalWrite(sessionID int64, data string) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	return a.terminalSvc.Write(a.ctx, sessionID, data)
}

// TerminalResize updates the PTY window dimensions (e.g. after the panel is
// resized by the user).
func (a *App) TerminalResize(sessionID int64, cols, rows uint16) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	return a.terminalSvc.Resize(a.ctx, sessionID, cols, rows)
}

// TerminalClose terminates the PTY process and releases resources.
func (a *App) TerminalClose(sessionID int64) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	return a.terminalSvc.Close(a.ctx, sessionID)
}
