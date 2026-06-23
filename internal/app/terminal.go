package app

import (
	"errors"

	"github.com/agentre-ai/agentre/internal/service/project_svc"
)

var errTerminalSvcNotInitialized = errors.New("terminal service not initialized")

// TerminalOpen opens a PTY for the given project/device combination. cols and
// rows set the initial terminal dimensions. The frontend should call
// TerminalClose when the panel is dismissed.
func (a *App) TerminalOpen(terminalID string, projectID int64, deviceID string, cols, rows uint16) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	cwd, err := project_svc.Default().ResolveProjectCwd(a.ctx, projectID, deviceID)
	if err != nil {
		return err
	}
	return a.terminalSvc.Open(a.ctx, terminalID, deviceID, cwd, cols, rows)
}

// TerminalWrite sends input bytes (typically keystrokes) to the running PTY.
func (a *App) TerminalWrite(terminalID string, data string) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	return a.terminalSvc.Write(a.ctx, terminalID, data)
}

// TerminalResize updates the PTY window dimensions (e.g. after the panel is
// resized by the user).
func (a *App) TerminalResize(terminalID string, cols, rows uint16) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	return a.terminalSvc.Resize(a.ctx, terminalID, cols, rows)
}

// TerminalClose terminates the PTY process and releases resources.
func (a *App) TerminalClose(terminalID string) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	return a.terminalSvc.Close(a.ctx, terminalID)
}
