package protocol

// TerminalOpenParams is the terminal.open RPC request.
type TerminalOpenParams struct {
	SessionID int64    `json:"sessionId"`
	Cwd       string   `json:"cwd"`
	Shell     string   `json:"shell,omitempty"`
	Env       []string `json:"env,omitempty"`
	Cols      uint16   `json:"cols"`
	Rows      uint16   `json:"rows"`
}

// TerminalOpenResult returns the daemon-side PTY id which the desktop
// uses opaquely for subsequent write/resize/close calls.
type TerminalOpenResult struct {
	TerminalID string `json:"terminalId"`
}

type TerminalWriteParams struct {
	TerminalID string `json:"terminalId"`
	Data       string `json:"data"`
}

type TerminalResizeParams struct {
	TerminalID string `json:"terminalId"`
	Cols       uint16 `json:"cols"`
	Rows       uint16 `json:"rows"`
}

type TerminalCloseParams struct {
	TerminalID string `json:"terminalId"`
}

// TerminalDataEvent is the daemon→client push for stdout chunks. Data is
// base64-encoded raw PTY bytes — not a UTF-8 string — so multibyte sequences
// split across PTY reads survive the JSON hop instead of being mangled to U+FFFD.
type TerminalDataEvent struct {
	TerminalID string `json:"terminalId"`
	Data       string `json:"data"`
}

// TerminalExitEvent — Reason is one of:
// "natural" | "killed" | "connection_lost" | "daemon_shutdown" | "error"
type TerminalExitEvent struct {
	TerminalID string `json:"terminalId"`
	Code       int    `json:"code"`
	Reason     string `json:"reason"`
	Msg        string `json:"msg,omitempty"`
}
