// Package pty defines the abstract Handle/Backend types for the terminal
// integration. Concrete implementations live under local/ and remote/.
package pty

//go:generate mockgen -source=pty.go -destination=../../service/terminal_svc/mocks/mock_handle.go -package=mocks

import "context"

// Spec is the parameters needed to spawn a PTY.
type Spec struct {
	Cwd   string
	Shell string   // empty → backend decides (typically $SHELL else /bin/sh)
	Env   []string // appended to base env; "TERM=xterm-256color" is injected by backend
	Cols  uint16
	Rows  uint16
}

// ExitInfo is delivered exactly once on Handle.Exit() when the PTY dies.
//
// Reason values:
//   - "natural"          shell exited on its own (user typed exit, or process finished)
//   - "killed"           backend sent SIGHUP/SIGKILL via Close()
//   - "connection_lost"  remote: agentred ws went away mid-session
//   - "daemon_shutdown"  remote: agentred is shutting down cleanly
//   - "error"            spawn/io error after Open returned
type ExitInfo struct {
	Code   int
	Reason string
	Msg    string
}

// Handle is the runtime API of one live PTY. Implementations are safe for
// concurrent Write/Resize/Close calls.
//
// Channel contract:
//   - Data() streams stdout chunks and is closed exactly once when no more
//     output will arrive.
//   - Exit() delivers exactly one ExitInfo and is then closed.
//   - The two channels are independent: there is NO ordering guarantee between
//     "Data closed" and "Exit delivered". A consumer MUST drain Data() until it
//     is closed AND read the single Exit() value before treating the PTY as
//     gone — selecting on whichever is ready first and returning will either
//     drop the exit event or truncate trailing output.
//   - Consumers must drain Data() continuously; an implementation may block on
//     the data channel while the PTY is producing output.
type Handle interface {
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
	Close() error
	Data() <-chan []byte
	Exit() <-chan ExitInfo
}

// Backend constructs PTY Handles. Open blocks until the PTY is spawned and
// the reaper goroutine is running. Open returns an error if ctx cancels
// before spawn completes or if the shell/cwd is invalid.
type Backend interface {
	Open(ctx context.Context, spec Spec) (Handle, error)
}
