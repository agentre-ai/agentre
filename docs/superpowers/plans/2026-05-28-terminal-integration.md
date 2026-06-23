# Session-Scoped Terminal Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-session PTY-backed terminal panel to the session detail page, with backend-dispatched routing between local (`creack/pty`) and remote (`agentred`) backends; UI is a toggle button in the chat topbar that replaces the chat view when active and kills the PTY when closed.

**Architecture:** Single `terminal_svc.Service` exposed via Wails; service inspects `session.Backend.DeviceID` and dispatches to `internal/pkg/pty/local` (creack/pty) or `internal/pkg/pty/remote` (wraps existing agentred ws client). Both backends implement a shared `pty.Handle` interface (`Write` / `Resize` / `Close` / `Data()` / `Exit()`). Agentred daemon exposes mirrored `terminal.*` RPC handlers. Output streams to the frontend via Wails events namespaced `terminal:<sessionID>:data`. Frontend mounts `xterm.js` only while the toggle is ON; unmount = Close.

**Tech Stack:** Go 1.26, `github.com/creack/pty` v1.1.x; React 19, TypeScript, `@xterm/xterm` v5 + `@xterm/addon-fit` + `@xterm/addon-web-links`; Wails v2; test stack `goconvey` + `testify` + `go.uber.org/mock/gomock` + `sqlmock` (per project convention); frontend tests via Vitest.

**Spec:** [docs/superpowers/specs/2026-05-28-terminal-integration-design.md](../specs/2026-05-28-terminal-integration-design.md)

---

## File Structure

### New files (Go)

```
internal/pkg/pty/
├── pty.go                                — Handle interface + ExitInfo + Spec types
├── pty_test.go                           — compile-time interface assertions
├── local/local.go                        — creack/pty Backend + handleImpl
├── local/local_test.go                   — //go:build !windows; real /bin/sh
├── remote/remote.go                      — wraps agentred ws client Backend + handleImpl
└── remote/remote_test.go                 — fake daemon ws server via httptest

internal/service/terminal_svc/
├── service.go                            — Wails-bound Service; Open/Write/Resize/Close + sessionID→Handle map
├── service_test.go                       — goconvey; PTYBackend mock + session repo mock
├── backend.go                            — PTYBackend interface + factory selector
├── backend_test.go                       — selector cases (local vs remote)
├── emitter.go                            — Emitter interface (mirrors chat_svc.Emitter)
└── mocks/                                — `make mock` generates here

internal/daemon/handlers/
├── terminal.go                           — TerminalHandlers; Open/Write/Resize/Close + push goroutines
└── terminal_test.go                      — testify + gomock (matches session_test.go style)

pkg/agentred/protocol/
└── terminal.go                           — TerminalOpenParams/Result, TerminalWriteParams, TerminalResizeParams, TerminalCloseParams, TerminalDataEvent, TerminalExitEvent
```

### New files (Frontend)

```
frontend/src/components/agentre/terminal/
├── terminal-panel.tsx                    — xterm container; lifecycle = mount→Open, unmount→Close
├── terminal-panel.test.tsx
├── use-terminal.ts                       — hook: subscribes to events, exposes terminal state
└── use-terminal.test.ts
```

### Modified files

- `go.mod` — add `github.com/creack/pty`
- `internal/service/chat_svc/cwd.go` — export `ResolveSessionCwd` (rename / wrap private fn so terminal_svc can call it)
- `internal/app/app.go` — register `terminal_svc.Service`, wire Emitter
- `internal/daemon/daemon.go` — register `terminal.*` handlers via `wrapGuarded` / `wrapGuardedNoParams`
- `frontend/package.json` — add `@xterm/xterm`, `@xterm/addon-fit`, `@xterm/addon-web-links`
- `frontend/src/components/agentre/chat-page.tsx` — add terminal toggle button in topbar
- `frontend/src/components/agentre/chat-streams-host.tsx` — when toggle ON, render `<TerminalPanel/>` instead of message stream

### File responsibility boundaries

- **`pty.Handle`** — runtime API of one PTY instance; local & remote both implement it. Service layer only talks Handle.
- **`PTYBackend`** — factory: `Open(ctx, spec) (Handle, error)`. Two impls: `local.Backend`, `remote.Backend`. Service picks impl based on session.
- **`terminal_svc.Service`** — orchestration only: sessionID → Handle map, event emission, lifecycle. No PTY logic.
- **`internal/daemon/handlers/terminal.go`** — agentred-side mirror of `terminal_svc`; uses `local.Backend` directly.
- **Frontend `TerminalPanel`** — pure UI: mounts xterm, owns the toggle's "OPEN" state. Service calls live in `use-terminal.ts`.

---

## Tasks

### Task 1: Add Go dependency on creack/pty

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add dep**

```bash
cd /Users/codfrm/Code/agentre/agentre
go get github.com/creack/pty@v1.1.21
go mod tidy
```

- [ ] **Step 2: Verify**

```bash
grep "creack/pty" go.mod
```
Expected: `github.com/creack/pty v1.1.21` line present.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add github.com/creack/pty for terminal PTY support"
```

---

### Task 2: Add frontend xterm dependencies

**Files:**
- Modify: `frontend/package.json`, `frontend/pnpm-lock.yaml`

- [ ] **Step 1: Add deps**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm add @xterm/xterm@^5.5.0 @xterm/addon-fit@^0.10.0 @xterm/addon-web-links@^0.11.0
```

- [ ] **Step 2: Verify**

```bash
grep -E '@xterm/(xterm|addon-fit|addon-web-links)' package.json
```
Expected: 3 matching lines under `dependencies`.

- [ ] **Step 3: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/package.json frontend/pnpm-lock.yaml
git commit -m "deps(frontend): add @xterm/xterm + fit/web-links addons"
```

---

### Task 3: Shared agentred RPC types for terminal.*

**Files:**
- Create: `pkg/agentred/protocol/terminal.go`
- Create: `pkg/agentred/protocol/terminal_test.go`

- [ ] **Step 1: Write the failing test (JSON roundtrip)**

```go
// pkg/agentred/protocol/terminal_test.go
package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/agentre-ai/agentre/pkg/agentred/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTerminalOpenParams_Roundtrip(t *testing.T) {
	in := protocol.TerminalOpenParams{
		SessionID: 42,
		Cwd:       "/home/me",
		Shell:     "/bin/zsh",
		Env:       []string{"TERM=xterm-256color"},
		Cols:      120,
		Rows:      30,
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out protocol.TerminalOpenParams
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in, out)
}

func TestTerminalExitEvent_ReasonString(t *testing.T) {
	ev := protocol.TerminalExitEvent{TerminalID: "abc", Code: 137, Reason: "killed", Msg: "sighup"}
	b, err := json.Marshal(ev)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"reason":"killed"`)
}
```

- [ ] **Step 2: Run to verify FAIL**

```bash
go test ./pkg/agentred/protocol/...
```
Expected: FAIL (package or symbol not found).

- [ ] **Step 3: Implement**

```go
// pkg/agentred/protocol/terminal.go
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

// TerminalDataEvent is the daemon→client push for stdout chunks.
type TerminalDataEvent struct {
	TerminalID string `json:"terminalId"`
	Data       string `json:"data"`
}

// TerminalExitEvent is the daemon→client push when the PTY exits.
// Reason is one of: "natural" | "killed" | "connection_lost" | "daemon_shutdown" | "error".
type TerminalExitEvent struct {
	TerminalID string `json:"terminalId"`
	Code       int    `json:"code"`
	Reason     string `json:"reason"`
	Msg        string `json:"msg,omitempty"`
}
```

- [ ] **Step 4: Run to verify PASS**

```bash
go test ./pkg/agentred/protocol/...
```
Expected: PASS, 2 tests.

- [ ] **Step 5: Commit**

```bash
git add pkg/agentred/protocol/terminal.go pkg/agentred/protocol/terminal_test.go
git commit -m "feat(protocol): add terminal.* RPC type definitions"
```

---

### Task 4: pty.Handle interface + ExitInfo + Spec types

**Files:**
- Create: `internal/pkg/pty/pty.go`
- Create: `internal/pkg/pty/pty_test.go`

- [ ] **Step 1: Write the failing test (zero-value + reason enum sanity)**

```go
// internal/pkg/pty/pty_test.go
package pty_test

import (
	"testing"

	"github.com/agentre-ai/agentre/internal/pkg/pty"
)

func TestSpec_ZeroValueValid(t *testing.T) {
	s := pty.Spec{}
	if s.Cols != 0 || s.Rows != 0 {
		t.Fatalf("zero Spec must have zero cols/rows")
	}
}

func TestExitInfo_Reason_KnownValues(t *testing.T) {
	for _, r := range []string{"natural", "killed", "connection_lost", "daemon_shutdown", "error"} {
		ei := pty.ExitInfo{Reason: r}
		if ei.Reason == "" {
			t.Fatalf("reason should be set")
		}
	}
}
```

- [ ] **Step 2: Run to verify FAIL**

```bash
go test ./internal/pkg/pty/...
```
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement**

```go
// internal/pkg/pty/pty.go
// Package pty defines the abstract Handle/Backend types for the terminal
// integration. Concrete implementations live under local/ and remote/.
package pty

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
// concurrent Write/Resize/Close calls. Data and Exit channels are closed
// exactly once (after Exit fires), so consumers can range over them.
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
```

- [ ] **Step 4: Run to verify PASS**

```bash
go test ./internal/pkg/pty/...
```
Expected: PASS, 2 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/pty/pty.go internal/pkg/pty/pty_test.go
git commit -m "feat(pty): define Handle / Backend / Spec / ExitInfo interfaces"
```

---

### Task 5: LocalPTY — spawn + echo round-trip

**Files:**
- Create: `internal/pkg/pty/local/local.go`
- Create: `internal/pkg/pty/local/local_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/pkg/pty/local/local_test.go
//go:build !windows

package local_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/pkg/pty/local"

	"github.com/stretchr/testify/require"
)

func TestLocalBackend_OpenEchoRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	be := local.NewBackend()
	h, err := be.Open(ctx, pty.Spec{Cwd: os.TempDir(), Shell: "/bin/sh", Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	_, err = h.Write([]byte("echo hello-pty\n"))
	require.NoError(t, err)

	deadline := time.After(3 * time.Second)
	var buf bytes.Buffer
	for {
		select {
		case chunk, ok := <-h.Data():
			if !ok {
				t.Fatalf("data channel closed before seeing echo output; got: %q", buf.String())
			}
			buf.Write(chunk)
			if bytes.Contains(buf.Bytes(), []byte("hello-pty")) {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for echo output; got: %q", buf.String())
		}
	}
}
```

- [ ] **Step 2: Run to verify FAIL**

```bash
go test ./internal/pkg/pty/local/...
```
Expected: FAIL — package missing.

- [ ] **Step 3: Implement**

```go
// internal/pkg/pty/local/local.go
//go:build !windows

// Package local implements pty.Backend with github.com/creack/pty.
package local

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	creackpty "github.com/creack/pty"

	pkgpty "github.com/agentre-ai/agentre/internal/pkg/pty"
)

// Backend is the local creack/pty implementation of pty.Backend.
type Backend struct{}

func NewBackend() *Backend { return &Backend{} }

const sigkillGrace = 200 * time.Millisecond

// Open spawns a shell under a PTY and starts the reaper + reader goroutines.
func (b *Backend) Open(ctx context.Context, spec pkgpty.Spec) (pkgpty.Handle, error) {
	shell := spec.Shell
	if shell == "" {
		if env := os.Getenv("SHELL"); env != "" {
			shell = env
		} else {
			shell = "/bin/sh"
		}
	}
	cmd := exec.CommandContext(ctx, shell, "-l")
	cmd.Dir = spec.Cwd
	cmd.Env = append(os.Environ(), append(spec.Env, "TERM=xterm-256color")...)

	ws := &creackpty.Winsize{Cols: spec.Cols, Rows: spec.Rows}
	f, err := creackpty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, err
	}
	h := &handleImpl{
		cmd:  cmd,
		file: f,
		data: make(chan []byte, 32),
		exit: make(chan pkgpty.ExitInfo, 1),
		done: make(chan struct{}),
	}
	go h.reader()
	go h.reaper()
	return h, nil
}

type handleImpl struct {
	cmd  *exec.Cmd
	file *os.File

	data chan []byte
	exit chan pkgpty.ExitInfo

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

func (h *handleImpl) Write(p []byte) (int, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return 0, errors.New("pty closed")
	}
	h.mu.Unlock()
	return h.file.Write(p)
}

func (h *handleImpl) Resize(cols, rows uint16) error {
	return creackpty.Setsize(h.file, &creackpty.Winsize{Cols: cols, Rows: rows})
}

func (h *handleImpl) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.mu.Unlock()

	if h.cmd.Process != nil {
		_ = h.cmd.Process.Signal(syscall.SIGHUP)
	}
	select {
	case <-h.done:
	case <-time.After(sigkillGrace):
		if h.cmd.Process != nil {
			_ = h.cmd.Process.Kill()
		}
		<-h.done
	}
	return nil
}

func (h *handleImpl) Data() <-chan []byte          { return h.data }
func (h *handleImpl) Exit() <-chan pkgpty.ExitInfo { return h.exit }

func (h *handleImpl) reader() {
	buf := make([]byte, 8192)
	for {
		n, err := h.file.Read(buf)
		if n > 0 {
			out := make([]byte, n)
			copy(out, buf[:n])
			select {
			case h.data <- out:
			case <-h.done:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (h *handleImpl) reaper() {
	err := h.cmd.Wait()
	close(h.done)
	_ = h.file.Close()

	info := pkgpty.ExitInfo{}
	if err == nil {
		info.Reason = "natural"
	} else {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			info.Code = ee.ExitCode()
			if h.wasKilled() {
				info.Reason = "killed"
			} else {
				info.Reason = "natural"
			}
		} else {
			info.Reason = "error"
			info.Msg = err.Error()
		}
	}
	h.exit <- info
	close(h.exit)
	close(h.data)
}

func (h *handleImpl) wasKilled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

var _ pkgpty.Handle = (*handleImpl)(nil)
var _ pkgpty.Backend = (*Backend)(nil)
```

- [ ] **Step 4: Run to verify PASS**

```bash
go test ./internal/pkg/pty/local/...
```
Expected: PASS within ~5s.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/pty/local/local.go internal/pkg/pty/local/local_test.go
git commit -m "feat(pty/local): spawn shell via creack/pty with reader+reaper"
```

---

### Task 6: LocalPTY — bad cwd returns error

**Files:**
- Modify: `internal/pkg/pty/local/local_test.go` (append test)

- [ ] **Step 1: Write the test**

```go
func TestLocalBackend_OpenBadCwd_Errors(t *testing.T) {
	be := local.NewBackend()
	_, err := be.Open(context.Background(), pty.Spec{
		Cwd:   "/path/that/definitely/does/not/exist/xyzzy",
		Shell: "/bin/sh",
		Cols:  80, Rows: 24,
	})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run to verify PASS**

```bash
go test -run TestLocalBackend_OpenBadCwd_Errors ./internal/pkg/pty/local/...
```
Expected: PASS (current impl surfaces `exec.Command` start error). If it doesn't, the impl is wrong — make it fail loudly.

- [ ] **Step 3: Commit**

```bash
git add internal/pkg/pty/local/local_test.go
git commit -m "test(pty/local): bad cwd surfaces spawn error"
```

---

### Task 7: LocalPTY — Resize delivers SIGWINCH (verified via stty)

**Files:**
- Modify: `internal/pkg/pty/local/local_test.go`

- [ ] **Step 1: Write the test**

```go
func TestLocalBackend_Resize_Reflected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	be := local.NewBackend()
	h, err := be.Open(ctx, pty.Spec{Cwd: os.TempDir(), Shell: "/bin/sh", Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	require.NoError(t, h.Resize(132, 40))
	_, err = h.Write([]byte("stty size\n"))
	require.NoError(t, err)

	deadline := time.After(3 * time.Second)
	var buf bytes.Buffer
	for {
		select {
		case chunk, ok := <-h.Data():
			if !ok {
				t.Fatalf("data closed before stty output; got: %q", buf.String())
			}
			buf.Write(chunk)
			if bytes.Contains(buf.Bytes(), []byte("40 132")) {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for 40 132 in output; got: %q", buf.String())
		}
	}
}
```

- [ ] **Step 2: Run to verify PASS**

```bash
go test -run TestLocalBackend_Resize_Reflected ./internal/pkg/pty/local/...
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/pkg/pty/local/local_test.go
git commit -m "test(pty/local): Resize reflects in stty size"
```

---

### Task 8: LocalPTY — Close emits killed exit

**Files:**
- Modify: `internal/pkg/pty/local/local_test.go`

- [ ] **Step 1: Write the test**

```go
func TestLocalBackend_Close_EmitsKilledExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	be := local.NewBackend()
	h, err := be.Open(ctx, pty.Spec{Cwd: os.TempDir(), Shell: "/bin/sh", Cols: 80, Rows: 24})
	require.NoError(t, err)

	require.NoError(t, h.Close())

	select {
	case info := <-h.Exit():
		require.Equal(t, "killed", info.Reason)
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive exit info within 2s")
	}
}
```

- [ ] **Step 2: Run to verify PASS**

```bash
go test -run TestLocalBackend_Close_EmitsKilledExit ./internal/pkg/pty/local/...
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/pkg/pty/local/local_test.go
git commit -m "test(pty/local): Close emits 'killed' exit info"
```

---

### Task 9: LocalPTY — natural exit (user types `exit`)

**Files:**
- Modify: `internal/pkg/pty/local/local_test.go`

- [ ] **Step 1: Write the test**

```go
func TestLocalBackend_NaturalExit_EmitsNatural(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	be := local.NewBackend()
	h, err := be.Open(ctx, pty.Spec{Cwd: os.TempDir(), Shell: "/bin/sh", Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	_, err = h.Write([]byte("exit 0\n"))
	require.NoError(t, err)

	select {
	case info := <-h.Exit():
		require.Equal(t, "natural", info.Reason)
		require.Equal(t, 0, info.Code)
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive natural exit within 3s")
	}
}
```

- [ ] **Step 2: Run to verify PASS**

```bash
go test -run TestLocalBackend_NaturalExit_EmitsNatural ./internal/pkg/pty/local/...
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/pkg/pty/local/local_test.go
git commit -m "test(pty/local): natural exit emits 'natural' reason"
```

---

### Task 10: Daemon — TerminalHandlers.Open + data/exit pump

**Files:**
- Create: `internal/daemon/handlers/terminal.go`
- Create: `internal/daemon/handlers/terminal_test.go`
- Generated: `internal/daemon/handlers/mock_handlers/mock_terminal.go` (via mockgen, see Step 2)

- [ ] **Step 1: Write the failing test**

```go
// internal/daemon/handlers/terminal_test.go
package handlers_test

import (
	"context"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/daemon/handlers/mock_handlers"
	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type recordingEmitter struct {
	events []recordedEvent
}
type recordedEvent struct {
	Name    string
	Payload any
}

func (e *recordingEmitter) Emit(_ context.Context, name string, payload any) {
	e.events = append(e.events, recordedEvent{name, payload})
}

func TestTerminal_Open_RegistersHandleAndReturnsID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	mh.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mh.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)

	rec := &recordingEmitter{}
	h := handlers.NewTerminalHandlers(mbe, rec)
	res, err := h.Open(context.Background(), protocol.TerminalOpenParams{
		SessionID: 1, Cwd: "/tmp", Cols: 80, Rows: 24,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.TerminalID)
	_ = time.Millisecond // keep import used in later tests
}
```

- [ ] **Step 2: Implement + generate mocks**

Create `internal/daemon/handlers/terminal.go`:

```go
// internal/daemon/handlers/terminal.go
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"
)

//go:generate mockgen -source=terminal.go -destination=mock_handlers/mock_terminal.go -package=mock_handlers

// PTYBackend / PTYHandle are named ports the daemon side speaks to. They
// mirror internal/pkg/pty.{Backend,Handle} but are declared here so
// mockgen can produce local mocks without crossing package boundaries.
type PTYBackend interface {
	Open(ctx context.Context, spec pty.Spec) (PTYHandle, error)
}

type PTYHandle interface {
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
	Close() error
	Data() <-chan []byte
	Exit() <-chan pty.ExitInfo
}

// Emitter is the daemon's push-event sink.
type Emitter interface {
	Emit(ctx context.Context, name string, payload any)
}

type EmitterFunc func(ctx context.Context, name string, payload any)

func (f EmitterFunc) Emit(ctx context.Context, name string, payload any) {
	if f != nil {
		f(ctx, name, payload)
	}
}

const (
	EventNameTerminalData = "terminal.data"
	EventNameTerminalExit = "terminal.exit"
)

var ErrTerminalNotFound = errors.New("terminal not found")

type TerminalHandlers struct {
	be      PTYBackend
	emitter Emitter

	mu        sync.Mutex
	terminals map[string]PTYHandle
}

func NewTerminalHandlers(be PTYBackend, emitter Emitter) *TerminalHandlers {
	return &TerminalHandlers{
		be:        be,
		emitter:   emitter,
		terminals: map[string]PTYHandle{},
	}
}

func (h *TerminalHandlers) Open(ctx context.Context, p protocol.TerminalOpenParams) (protocol.TerminalOpenResult, error) {
	hd, err := h.be.Open(ctx, pty.Spec{
		Cwd: p.Cwd, Shell: p.Shell, Env: p.Env,
		Cols: p.Cols, Rows: p.Rows,
	})
	if err != nil {
		return protocol.TerminalOpenResult{}, err
	}
	id := newTerminalID()
	h.mu.Lock()
	h.terminals[id] = hd
	h.mu.Unlock()
	go h.pump(ctx, id, hd)
	return protocol.TerminalOpenResult{TerminalID: id}, nil
}

func (h *TerminalHandlers) pump(ctx context.Context, id string, hd PTYHandle) {
	for {
		select {
		case data, ok := <-hd.Data():
			if !ok {
				return
			}
			h.emitter.Emit(ctx, EventNameTerminalData, protocol.TerminalDataEvent{
				TerminalID: id, Data: string(data),
			})
		case info, ok := <-hd.Exit():
			h.mu.Lock()
			delete(h.terminals, id)
			h.mu.Unlock()
			if ok {
				h.emitter.Emit(ctx, EventNameTerminalExit, protocol.TerminalExitEvent{
					TerminalID: id, Code: info.Code, Reason: info.Reason, Msg: info.Msg,
				})
			}
			return
		}
	}
}

func newTerminalID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
```

Generate mocks:

```bash
cd /Users/codfrm/Code/agentre/agentre
go generate ./internal/daemon/handlers/...
```

- [ ] **Step 3: Run to verify PASS**

```bash
go test -run TestTerminal_Open_RegistersHandleAndReturnsID ./internal/daemon/handlers/...
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/handlers/terminal.go internal/daemon/handlers/terminal_test.go internal/daemon/handlers/mock_handlers/mock_terminal.go
git commit -m "feat(daemon): terminal.open handler with PTYBackend port and pump"
```

---

### Task 11: TerminalHandlers — Write / Resize / Close + NotFound

**Files:**
- Modify: `internal/daemon/handlers/terminal.go`
- Modify: `internal/daemon/handlers/terminal_test.go`

- [ ] **Step 1: Write the failing tests (append)**

```go
func TestTerminal_Write_DispatchesToHandle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	mh.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mh.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)
	mh.EXPECT().Write([]byte("ls\n")).Return(3, nil)

	h := handlers.NewTerminalHandlers(mbe, &recordingEmitter{})
	res, _ := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})
	_, err := h.Write(context.Background(), protocol.TerminalWriteParams{TerminalID: res.TerminalID, Data: "ls\n"})
	require.NoError(t, err)
}

func TestTerminal_Write_UnknownID_ReturnsNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	h := handlers.NewTerminalHandlers(mbe, &recordingEmitter{})
	_, err := h.Write(context.Background(), protocol.TerminalWriteParams{TerminalID: "nope", Data: "x"})
	require.ErrorIs(t, err, handlers.ErrTerminalNotFound)
}

func TestTerminal_Resize_DispatchesToHandle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	mh.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mh.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)
	mh.EXPECT().Resize(uint16(120), uint16(30)).Return(nil)

	h := handlers.NewTerminalHandlers(mbe, &recordingEmitter{})
	res, _ := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})
	_, err := h.Resize(context.Background(), protocol.TerminalResizeParams{TerminalID: res.TerminalID, Cols: 120, Rows: 30})
	require.NoError(t, err)
}

func TestTerminal_Close_CallsHandleClose(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	mh.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mh.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)
	mh.EXPECT().Close().Return(nil)

	h := handlers.NewTerminalHandlers(mbe, &recordingEmitter{})
	res, _ := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})
	_, err := h.Close(context.Background(), protocol.TerminalCloseParams{TerminalID: res.TerminalID})
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run to verify FAIL**

```bash
go test -run 'TestTerminal_(Write|Resize|Close)' ./internal/daemon/handlers/...
```
Expected: FAIL — Write/Resize/Close not defined.

- [ ] **Step 3: Implement (append to terminal.go)**

```go
type TerminalAck struct{}

func (h *TerminalHandlers) Write(ctx context.Context, p protocol.TerminalWriteParams) (TerminalAck, error) {
	h.mu.Lock()
	hd, ok := h.terminals[p.TerminalID]
	h.mu.Unlock()
	if !ok {
		return TerminalAck{}, ErrTerminalNotFound
	}
	_, err := hd.Write([]byte(p.Data))
	return TerminalAck{}, err
}

func (h *TerminalHandlers) Resize(ctx context.Context, p protocol.TerminalResizeParams) (TerminalAck, error) {
	h.mu.Lock()
	hd, ok := h.terminals[p.TerminalID]
	h.mu.Unlock()
	if !ok {
		return TerminalAck{}, ErrTerminalNotFound
	}
	return TerminalAck{}, hd.Resize(p.Cols, p.Rows)
}

func (h *TerminalHandlers) Close(ctx context.Context, p protocol.TerminalCloseParams) (TerminalAck, error) {
	h.mu.Lock()
	hd, ok := h.terminals[p.TerminalID]
	h.mu.Unlock()
	if !ok {
		return TerminalAck{}, ErrTerminalNotFound
	}
	return TerminalAck{}, hd.Close()
}
```

- [ ] **Step 4: Run to verify PASS**

```bash
go test ./internal/daemon/handlers/... -run 'TestTerminal_'
```
Expected: PASS, all 5 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/handlers/terminal.go internal/daemon/handlers/terminal_test.go
git commit -m "feat(daemon): terminal write/resize/close handlers + NotFound"
```

---

### Task 12: TerminalHandlers — verify data/exit events emitted

**Files:**
- Modify: `internal/daemon/handlers/terminal_test.go`

- [ ] **Step 1: Write the tests**

```go
func TestTerminal_Pump_EmitsDataEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	dataCh := make(chan []byte, 1)
	exitCh := make(chan pty.ExitInfo)
	mh.EXPECT().Data().AnyTimes().Return(dataCh)
	mh.EXPECT().Exit().AnyTimes().Return(exitCh)
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)

	rec := &recordingEmitter{}
	h := handlers.NewTerminalHandlers(mbe, rec)
	res, _ := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})

	dataCh <- []byte("hello")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(rec.events) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	require.Len(t, rec.events, 1)
	assert.Equal(t, handlers.EventNameTerminalData, rec.events[0].Name)
	pay := rec.events[0].Payload.(protocol.TerminalDataEvent)
	assert.Equal(t, res.TerminalID, pay.TerminalID)
	assert.Equal(t, "hello", pay.Data)
}

func TestTerminal_Pump_EmitsExitAndClearsMap(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	dataCh := make(chan []byte)
	exitCh := make(chan pty.ExitInfo, 1)
	mh.EXPECT().Data().AnyTimes().Return(dataCh)
	mh.EXPECT().Exit().AnyTimes().Return(exitCh)
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)

	rec := &recordingEmitter{}
	h := handlers.NewTerminalHandlers(mbe, rec)
	res, _ := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})

	exitCh <- pty.ExitInfo{Code: 0, Reason: "natural"}
	close(exitCh)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(rec.events) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	require.Len(t, rec.events, 1)
	assert.Equal(t, handlers.EventNameTerminalExit, rec.events[0].Name)

	_, err := h.Write(context.Background(), protocol.TerminalWriteParams{TerminalID: res.TerminalID})
	assert.ErrorIs(t, err, handlers.ErrTerminalNotFound)
}
```

- [ ] **Step 2: Run to verify PASS**

```bash
go test -run 'TestTerminal_Pump' ./internal/daemon/handlers/...
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/handlers/terminal_test.go
git commit -m "test(daemon): pump emits data/exit events and clears map"
```

---

### Task 13: Register terminal.* handlers in agentred daemon

**Files:**
- Modify: `internal/daemon/daemon.go` (registration section near line 152, where `session.list` is registered)

- [ ] **Step 1: Read the registration section**

```bash
sed -n '140,180p' /Users/codfrm/Code/agentre/agentre/internal/daemon/daemon.go
```

Confirm `wrapGuarded` helper exists (used for session.get / llm.upsert / etc.).

- [ ] **Step 2: Add the imports**

In the import block at the top of `internal/daemon/daemon.go`, add:

```go
"github.com/agentre-ai/agentre/internal/pkg/pty"
"github.com/agentre-ai/agentre/internal/pkg/pty/local"
```

- [ ] **Step 3: Add registration in the bootstrap method**

In the same method where `d.registry.Register("session.list", ...)` is called, add:

```go
// Terminal: local PTY backend; emitter writes onto the per-ws broadcast
// channel using the same path session.event push uses. If a per-ws emitter
// adapter doesn't already exist, look at how broadcastEvent (or whatever
// it's named in this daemon — search for the existing push-event call)
// is invoked from session handlers and mirror that pattern.
termBackend := localPTYBackendAdapter{be: local.NewBackend()}
termH := handlers.NewTerminalHandlers(termBackend, d.daemonEmitter())
d.registry.Register("terminal.open", wrapGuarded(termH.Open))
d.registry.Register("terminal.write", wrapGuarded(termH.Write))
d.registry.Register("terminal.resize", wrapGuarded(termH.Resize))
d.registry.Register("terminal.close", wrapGuarded(termH.Close))
```

- [ ] **Step 4: Add the adapter (bridges pty.Handle → handlers.PTYHandle)**

Append at the bottom of `internal/daemon/daemon.go` (or create `internal/daemon/terminal_adapter.go` with `package daemon`):

```go
type localPTYBackendAdapter struct {
	be *local.Backend
}

func (a localPTYBackendAdapter) Open(ctx context.Context, spec pty.Spec) (handlers.PTYHandle, error) {
	return a.be.Open(ctx, spec)
}

var _ handlers.PTYBackend = localPTYBackendAdapter{}
```

(`pty.Handle` satisfies `handlers.PTYHandle` structurally — Go interfaces are duck-typed, so the returned `pty.Handle` value is assignable to `handlers.PTYHandle` directly.)

- [ ] **Step 5: Wire daemonEmitter()**

If `d.daemonEmitter()` does **not** already exist, search the file for the existing push mechanism:

```bash
grep -n "broadcast\|pushEvent\|EmitEvent" /Users/codfrm/Code/agentre/agentre/internal/daemon/*.go | head -20
```

Reuse whatever already broadcasts session events. If genuinely missing (the daemon has no push mechanism at all), **STOP and surface to the user** — that's a missing primitive the spec assumed exists, and inventing it here breaks the "no drive-by" rule.

If the helper exists with a different name, write:

```go
func (d *Daemon) daemonEmitter() handlers.Emitter {
	return handlers.EmitterFunc(func(ctx context.Context, name string, payload any) {
		d.broadcastEvent(name, payload) // replace with actual method name
	})
}
```

- [ ] **Step 6: Run build + tests**

```bash
go build ./internal/daemon/...
go test ./internal/daemon/...
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/daemon/daemon.go
# also internal/daemon/terminal_adapter.go if you split it out
git commit -m "feat(daemon): register terminal.* RPC methods backed by local PTY"
```

---

### Task 14: Export chat_svc.ResolveSessionCwd

**Files:**
- Modify: `internal/service/chat_svc/cwd.go` (append exported wrapper)
- Create: `internal/service/chat_svc/cwd_external_test.go`

- [ ] **Step 1: Write the failing test (external package — verifies it's truly exported)**

```go
// internal/service/chat_svc/cwd_external_test.go
package chat_svc_test

import (
	"context"
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSessionCwd_Exported(t *testing.T) {
	t.Cleanup(func() { chat_svc.RegisterCwdResolver(nil) })
	chat_svc.RegisterCwdResolver(func(_ context.Context, _ *chat_entity.Session) (string, error) {
		return "/from/resolver", nil
	})
	cwd, err := chat_svc.ResolveSessionCwd(context.Background(),
		&chat_entity.Session{ID: 1, AgentID: 7},
		&agent_backend_entity.AgentBackend{DeviceID: ""},
	)
	require.NoError(t, err)
	assert.Equal(t, "/from/resolver", cwd)
}
```

- [ ] **Step 2: Run to verify FAIL**

```bash
go test -run TestResolveSessionCwd_Exported ./internal/service/chat_svc/...
```
Expected: FAIL — `chat_svc.ResolveSessionCwd undefined`.

- [ ] **Step 3: Add public wrapper**

In `internal/service/chat_svc/cwd.go`, append:

```go
// ResolveSessionCwd is the public adapter for resolveSessionCwd, exposed so
// terminal_svc (and other future services) can reuse the same project /
// device cwd resolution rules without re-implementing them.
func ResolveSessionCwd(ctx context.Context, sess *chat_entity.Session, be *agent_backend_entity.AgentBackend) (string, error) {
	return resolveSessionCwd(ctx, sess, be)
}
```

- [ ] **Step 4: Run to verify PASS**

```bash
go test ./internal/service/chat_svc/...
```
Expected: PASS for new test and all existing internal tests.

- [ ] **Step 5: Commit**

```bash
git add internal/service/chat_svc/cwd.go internal/service/chat_svc/cwd_external_test.go
git commit -m "feat(chat_svc): export ResolveSessionCwd for cross-service reuse"
```

---

### Task 15: terminal_svc Emitter + event name builders

**Files:**
- Create: `internal/service/terminal_svc/emitter.go`
- Create: `internal/service/terminal_svc/emitter_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/service/terminal_svc/emitter_test.go
package terminal_svc_test

import (
	"context"
	"testing"

	"github.com/agentre-ai/agentre/internal/service/terminal_svc"

	"github.com/stretchr/testify/assert"
)

func TestStreamName_DataAndExit(t *testing.T) {
	assert.Equal(t, "terminal:42:data", terminal_svc.DataEventName(42))
	assert.Equal(t, "terminal:42:exit", terminal_svc.ExitEventName(42))
}

func TestNoopEmitter_DoesNotPanic(t *testing.T) {
	terminal_svc.NoopEmitter{}.Emit(context.Background(), "x", nil)
}

func TestEmitterFunc_DispatchesAndNilSafe(t *testing.T) {
	called := 0
	var f terminal_svc.EmitterFunc = func(_ context.Context, _ string, _ any) { called++ }
	f.Emit(context.Background(), "x", nil)
	assert.Equal(t, 1, called)
	var nilF terminal_svc.EmitterFunc
	nilF.Emit(context.Background(), "x", nil) // must not panic
}
```

- [ ] **Step 2: Run to verify FAIL**

```bash
go test ./internal/service/terminal_svc/...
```
Expected: FAIL — package missing.

- [ ] **Step 3: Implement**

```go
// internal/service/terminal_svc/emitter.go
package terminal_svc

import (
	"context"
	"fmt"
)

type Emitter interface {
	Emit(ctx context.Context, name string, payload any)
}

type EmitterFunc func(ctx context.Context, name string, payload any)

func (f EmitterFunc) Emit(ctx context.Context, name string, payload any) {
	if f != nil {
		f(ctx, name, payload)
	}
}

type NoopEmitter struct{}

func (NoopEmitter) Emit(context.Context, string, any) {}

// DataEventName is the canonical Wails event name for stdout chunks of a
// given sessionID. Frontend subscribes via EventsOn(DataEventName(id)).
func DataEventName(sessionID int64) string {
	return fmt.Sprintf("terminal:%d:data", sessionID)
}

func ExitEventName(sessionID int64) string {
	return fmt.Sprintf("terminal:%d:exit", sessionID)
}
```

- [ ] **Step 4: Run to verify PASS**

```bash
go test ./internal/service/terminal_svc/...
```
Expected: PASS, 3 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/service/terminal_svc/emitter.go internal/service/terminal_svc/emitter_test.go
git commit -m "feat(terminal_svc): Emitter interface + canonical event names"
```

---

### Task 16: PTYBackend interface + selector

**Files:**
- Create: `internal/service/terminal_svc/backend.go`
- Create: `internal/service/terminal_svc/backend_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/service/terminal_svc/backend_test.go
package terminal_svc_test

import (
	"context"
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/service/terminal_svc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBackend struct{ name string }

func (f *fakeBackend) Open(_ context.Context, _ pty.Spec) (pty.Handle, error) {
	return nil, nil
}

func TestBackendSelector_PicksLocal(t *testing.T) {
	be := &agent_backend_entity.AgentBackend{DeviceID: ""}
	sel := terminal_svc.NewBackendSelector(&fakeBackend{name: "local"}, func(string) (terminal_svc.PTYBackend, error) {
		t.Fatal("should not call remote factory for local")
		return nil, nil
	})
	got, err := sel.Pick(be)
	require.NoError(t, err)
	assert.Equal(t, "local", got.(*fakeBackend).name)
}

func TestBackendSelector_PicksRemote(t *testing.T) {
	be := &agent_backend_entity.AgentBackend{DeviceID: "7"}
	sel := terminal_svc.NewBackendSelector(&fakeBackend{name: "local"}, func(devID string) (terminal_svc.PTYBackend, error) {
		assert.Equal(t, "7", devID)
		return &fakeBackend{name: "remote"}, nil
	})
	got, err := sel.Pick(be)
	require.NoError(t, err)
	assert.Equal(t, "remote", got.(*fakeBackend).name)
}

func TestBackendSelector_NilBackend_ReturnsError(t *testing.T) {
	sel := terminal_svc.NewBackendSelector(&fakeBackend{name: "local"}, nil)
	_, err := sel.Pick(nil)
	require.ErrorIs(t, err, terminal_svc.ErrNoBackend)
}
```

- [ ] **Step 2: Run to verify FAIL**

```bash
go test ./internal/service/terminal_svc/...
```
Expected: FAIL — `PTYBackend / NewBackendSelector undefined`.

- [ ] **Step 3: Implement**

```go
// internal/service/terminal_svc/backend.go
package terminal_svc

import (
	"context"
	"errors"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/pty"
)

//go:generate mockgen -source=backend.go -destination=mocks/mock_backend.go -package=mocks

type PTYBackend interface {
	Open(ctx context.Context, spec pty.Spec) (pty.Handle, error)
}

type RemoteBackendFactory func(deviceID string) (PTYBackend, error)

type BackendSelector struct {
	local        PTYBackend
	remoteFactor RemoteBackendFactory
}

func NewBackendSelector(local PTYBackend, remote RemoteBackendFactory) *BackendSelector {
	return &BackendSelector{local: local, remoteFactor: remote}
}

var ErrNoBackend = errors.New("no backend on session agent")

func (s *BackendSelector) Pick(be *agent_backend_entity.AgentBackend) (PTYBackend, error) {
	if be == nil {
		return nil, ErrNoBackend
	}
	if be.IsLocal() {
		return s.local, nil
	}
	return s.remoteFactor(be.DeviceID)
}
```

- [ ] **Step 4: Generate mock for PTYBackend + pty.Handle**

Add a generate directive to `internal/pkg/pty/pty.go` (top of file, after the package comment):

```go
//go:generate mockgen -source=pty.go -destination=../../service/terminal_svc/mocks/mock_handle.go -package=mocks
```

Then run:

```bash
cd /Users/codfrm/Code/agentre/agentre
mkdir -p internal/service/terminal_svc/mocks
go generate ./internal/pkg/pty/...
go generate ./internal/service/terminal_svc/...
```

Expected: `internal/service/terminal_svc/mocks/mock_backend.go` and `mock_handle.go` generated containing `MockPTYBackend`, `MockHandle`, `MockBackend`.

- [ ] **Step 5: Run to verify PASS**

```bash
go test ./internal/service/terminal_svc/...
```
Expected: PASS, all backend tests.

- [ ] **Step 6: Commit**

```bash
git add internal/service/terminal_svc/backend.go internal/service/terminal_svc/backend_test.go internal/service/terminal_svc/mocks/ internal/pkg/pty/pty.go
git commit -m "feat(terminal_svc): PTYBackend interface + local/remote selector"
```

---

### Task 17: terminal_svc.Service — Open (local happy path) + map registration

**Files:**
- Create: `internal/service/terminal_svc/service.go`
- Create: `internal/service/terminal_svc/service_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/service/terminal_svc/service_test.go
package terminal_svc_test

import (
	"context"
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/service/terminal_svc"
	"github.com/agentre-ai/agentre/internal/service/terminal_svc/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type stubSessionLookup struct {
	sess *chat_entity.Session
	be   *agent_backend_entity.AgentBackend
	cwd  string
	err  error
}

func (s stubSessionLookup) Lookup(_ context.Context, _ int64) (*chat_entity.Session, *agent_backend_entity.AgentBackend, string, error) {
	return s.sess, s.be, s.cwd, s.err
}

func TestService_Open_Local_RegistersHandle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBE := mocks.NewMockPTYBackend(ctrl)
	mockH := mocks.NewMockHandle(ctrl)
	mockH.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mockH.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mockBE.EXPECT().Open(gomock.Any(), pty.Spec{Cwd: "/tmp", Cols: 80, Rows: 24}).Return(mockH, nil)

	sel := terminal_svc.NewBackendSelector(mockBE, func(string) (terminal_svc.PTYBackend, error) {
		t.Fatal("should not call remote factory for local")
		return nil, nil
	})
	svc := terminal_svc.NewService(stubSessionLookup{
		sess: &chat_entity.Session{ID: 1},
		be:   &agent_backend_entity.AgentBackend{DeviceID: ""},
		cwd:  "/tmp",
	}, sel, terminal_svc.NoopEmitter{})

	require.NoError(t, svc.Open(context.Background(), 1, 80, 24))

	mockH.EXPECT().Write([]byte("x")).Return(1, nil)
	assert.NoError(t, svc.Write(context.Background(), 1, "x"))
}
```

- [ ] **Step 2: Run to verify FAIL**

```bash
go test ./internal/service/terminal_svc/...
```
Expected: FAIL — `Service / NewService undefined`.

- [ ] **Step 3: Implement**

```go
// internal/service/terminal_svc/service.go
package terminal_svc

import (
	"context"
	"errors"
	"sync"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"
)

// SessionLookup decouples Service from chat_repo so it can be unit tested.
// Production binding lives in app.go and wraps chat_svc.ResolveSessionCwd
// plus chat_repo Find / agent_backend_repo Find.
type SessionLookup interface {
	Lookup(ctx context.Context, sessionID int64) (*chat_entity.Session, *agent_backend_entity.AgentBackend, string, error)
}

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrTerminalClosed  = errors.New("terminal closed")
	ErrTerminalNotOpen = errors.New("terminal not open for this session")
)

type Service struct {
	lookup   SessionLookup
	selector *BackendSelector
	emitter  Emitter

	mu       sync.Mutex
	sessions map[int64]pty.Handle
}

func NewService(lookup SessionLookup, sel *BackendSelector, emitter Emitter) *Service {
	if emitter == nil {
		emitter = NoopEmitter{}
	}
	return &Service{
		lookup:   lookup,
		selector: sel,
		emitter:  emitter,
		sessions: map[int64]pty.Handle{},
	}
}

func (s *Service) Open(ctx context.Context, sessionID int64, cols, rows uint16) error {
	sess, be, cwd, err := s.lookup.Lookup(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return ErrSessionNotFound
	}
	backend, err := s.selector.Pick(be)
	if err != nil {
		return err
	}

	s.mu.Lock()
	old, hasOld := s.sessions[sessionID]
	if hasOld {
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()
	if hasOld {
		_ = old.Close()
	}

	h, err := backend.Open(ctx, pty.Spec{Cwd: cwd, Cols: cols, Rows: rows})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.sessions[sessionID] = h
	s.mu.Unlock()

	go s.pump(ctx, sessionID, h)
	return nil
}

func (s *Service) Write(ctx context.Context, sessionID int64, data string) error {
	h := s.lookupHandle(sessionID)
	if h == nil {
		return ErrTerminalClosed
	}
	_, err := h.Write([]byte(data))
	return err
}

func (s *Service) Resize(ctx context.Context, sessionID int64, cols, rows uint16) error {
	h := s.lookupHandle(sessionID)
	if h == nil {
		return ErrTerminalClosed
	}
	return h.Resize(cols, rows)
}

func (s *Service) Close(ctx context.Context, sessionID int64) error {
	s.mu.Lock()
	h, ok := s.sessions[sessionID]
	if ok {
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()
	if !ok {
		return ErrTerminalNotOpen
	}
	return h.Close()
}

func (s *Service) Shutdown() {
	s.mu.Lock()
	hs := make([]pty.Handle, 0, len(s.sessions))
	for _, h := range s.sessions {
		hs = append(hs, h)
	}
	s.sessions = map[int64]pty.Handle{}
	s.mu.Unlock()
	for _, h := range hs {
		_ = h.Close()
	}
}

func (s *Service) lookupHandle(sessionID int64) pty.Handle {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[sessionID]
}

func (s *Service) pump(ctx context.Context, sessionID int64, h pty.Handle) {
	for {
		select {
		case data, ok := <-h.Data():
			if !ok {
				return
			}
			s.emitter.Emit(ctx, DataEventName(sessionID), map[string]string{"data": string(data)})
		case info, ok := <-h.Exit():
			s.mu.Lock()
			if cur, exists := s.sessions[sessionID]; exists && cur == h {
				delete(s.sessions, sessionID)
			}
			s.mu.Unlock()
			if ok {
				s.emitter.Emit(ctx, ExitEventName(sessionID), protocol.TerminalExitEvent{
					Code: info.Code, Reason: info.Reason, Msg: info.Msg,
				})
			}
			return
		}
	}
}
```

- [ ] **Step 4: Run to verify PASS**

```bash
go test -run TestService_Open_Local_RegistersHandle ./internal/service/terminal_svc/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/terminal_svc/service.go internal/service/terminal_svc/service_test.go
git commit -m "feat(terminal_svc): Service.Open with local backend dispatch + map"
```

---

### Task 18: terminal_svc.Service — Close + Shutdown + error paths

**Files:**
- Modify: `internal/service/terminal_svc/service_test.go`

- [ ] **Step 1: Write the failing tests (append)**

```go
func TestService_Open_SessionNotFound(t *testing.T) {
	sel := terminal_svc.NewBackendSelector(nil, nil)
	svc := terminal_svc.NewService(stubSessionLookup{}, sel, terminal_svc.NoopEmitter{})
	err := svc.Open(context.Background(), 999, 80, 24)
	require.ErrorIs(t, err, terminal_svc.ErrSessionNotFound)
}

func TestService_Write_NoOpenTerminal(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	sel := terminal_svc.NewBackendSelector(mocks.NewMockPTYBackend(ctrl), nil)
	svc := terminal_svc.NewService(stubSessionLookup{
		sess: &chat_entity.Session{ID: 1},
		be:   &agent_backend_entity.AgentBackend{DeviceID: ""},
		cwd:  "/tmp",
	}, sel, terminal_svc.NoopEmitter{})
	err := svc.Write(context.Background(), 1, "x")
	require.ErrorIs(t, err, terminal_svc.ErrTerminalClosed)
}

func TestService_Close_UnknownSession(t *testing.T) {
	sel := terminal_svc.NewBackendSelector(nil, nil)
	svc := terminal_svc.NewService(stubSessionLookup{}, sel, terminal_svc.NoopEmitter{})
	err := svc.Close(context.Background(), 999)
	require.ErrorIs(t, err, terminal_svc.ErrTerminalNotOpen)
}

func TestService_Open_ReOpenClosesPrevious(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockBE := mocks.NewMockPTYBackend(ctrl)
	first := mocks.NewMockHandle(ctrl)
	second := mocks.NewMockHandle(ctrl)
	first.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	first.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	second.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	second.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))

	gomock.InOrder(
		mockBE.EXPECT().Open(gomock.Any(), gomock.Any()).Return(first, nil),
		first.EXPECT().Close().Return(nil),
		mockBE.EXPECT().Open(gomock.Any(), gomock.Any()).Return(second, nil),
	)

	sel := terminal_svc.NewBackendSelector(mockBE, nil)
	svc := terminal_svc.NewService(stubSessionLookup{
		sess: &chat_entity.Session{ID: 1},
		be:   &agent_backend_entity.AgentBackend{DeviceID: ""},
		cwd:  "/tmp",
	}, sel, terminal_svc.NoopEmitter{})

	require.NoError(t, svc.Open(context.Background(), 1, 80, 24))
	require.NoError(t, svc.Open(context.Background(), 1, 80, 24))
}

func TestService_Shutdown_ClosesAll(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockBE := mocks.NewMockPTYBackend(ctrl)
	mh := mocks.NewMockHandle(ctrl)
	mh.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mh.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mockBE.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)
	mh.EXPECT().Close().Return(nil)

	sel := terminal_svc.NewBackendSelector(mockBE, nil)
	svc := terminal_svc.NewService(stubSessionLookup{
		sess: &chat_entity.Session{ID: 1},
		be:   &agent_backend_entity.AgentBackend{DeviceID: ""},
		cwd:  "/tmp",
	}, sel, terminal_svc.NoopEmitter{})

	require.NoError(t, svc.Open(context.Background(), 1, 80, 24))
	svc.Shutdown()
}
```

- [ ] **Step 2: Run to verify PASS**

```bash
go test ./internal/service/terminal_svc/...
```
Expected: PASS for all 5 new tests + the earlier Open test.

- [ ] **Step 3: Commit**

```bash
git add internal/service/terminal_svc/service_test.go
git commit -m "test(terminal_svc): cover Close/Shutdown/reopen/error paths"
```

---

### Task 19: terminal_svc.Service — pump emits data event (verify)

**Files:**
- Modify: `internal/service/terminal_svc/service_test.go`

- [ ] **Step 1: Write the test (append)**

```go
import (
	"sync"
	"time"
	// existing imports retained
)

type recordingEmitter struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	Name    string
	Payload any
}

func (r *recordingEmitter) Emit(_ context.Context, name string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{name, payload})
}

func (r *recordingEmitter) Snapshot() []recordedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedEvent, len(r.events))
	copy(out, r.events)
	return out
}

func TestService_Pump_EmitsDataEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockBE := mocks.NewMockPTYBackend(ctrl)
	mh := mocks.NewMockHandle(ctrl)
	dataCh := make(chan []byte, 1)
	exitCh := make(chan pty.ExitInfo)
	mh.EXPECT().Data().AnyTimes().Return(dataCh)
	mh.EXPECT().Exit().AnyTimes().Return(exitCh)
	mockBE.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)

	rec := &recordingEmitter{}
	sel := terminal_svc.NewBackendSelector(mockBE, nil)
	svc := terminal_svc.NewService(stubSessionLookup{
		sess: &chat_entity.Session{ID: 7},
		be:   &agent_backend_entity.AgentBackend{DeviceID: ""},
		cwd:  "/tmp",
	}, sel, rec)

	require.NoError(t, svc.Open(context.Background(), 7, 80, 24))
	dataCh <- []byte("abc")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(rec.Snapshot()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	evs := rec.Snapshot()
	require.Len(t, evs, 1)
	assert.Equal(t, "terminal:7:data", evs[0].Name)
}
```

- [ ] **Step 2: Run to verify PASS**

```bash
go test -run TestService_Pump_EmitsDataEvent ./internal/service/terminal_svc/...
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/service/terminal_svc/service_test.go
git commit -m "test(terminal_svc): pump emits terminal:<sid>:data event"
```

---

### Task 20: Remote PTY backend — Open + data push round-trip

**Files:**
- Create: `internal/pkg/pty/remote/remote.go`
- Create: `internal/pkg/pty/remote/remote_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/pkg/pty/remote/remote_test.go
package remote_test

import (
	"context"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/pkg/pty/remote"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"

	"github.com/stretchr/testify/require"
)

type fakeClient struct {
	openParams chan protocol.TerminalOpenParams
	dataPush   chan protocol.TerminalDataEvent
	exitPush   chan protocol.TerminalExitEvent
}

func (f *fakeClient) Call(_ context.Context, method string, params any, out any) error {
	switch method {
	case "terminal.open":
		f.openParams <- params.(protocol.TerminalOpenParams)
		*(out.(*protocol.TerminalOpenResult)) = protocol.TerminalOpenResult{TerminalID: "remote-1"}
	}
	return nil
}
func (f *fakeClient) SubscribeData(_ string) <-chan protocol.TerminalDataEvent {
	return f.dataPush
}
func (f *fakeClient) SubscribeExit(_ string) <-chan protocol.TerminalExitEvent {
	return f.exitPush
}

func TestRemoteBackend_Open_RPC_RoundTrip(t *testing.T) {
	fc := &fakeClient{
		openParams: make(chan protocol.TerminalOpenParams, 1),
		dataPush:   make(chan protocol.TerminalDataEvent, 1),
		exitPush:   make(chan protocol.TerminalExitEvent, 1),
	}
	be := remote.NewBackend(fc)

	h, err := be.Open(context.Background(), pty.Spec{Cwd: "/r", Shell: "/bin/sh", Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	op := <-fc.openParams
	require.Equal(t, "/r", op.Cwd)
	require.Equal(t, uint16(80), op.Cols)

	fc.dataPush <- protocol.TerminalDataEvent{TerminalID: "remote-1", Data: "xyz"}

	select {
	case chunk := <-h.Data():
		require.Equal(t, []byte("xyz"), chunk)
	case <-time.After(time.Second):
		t.Fatal("did not receive data within 1s")
	}
}

func TestRemoteBackend_ExitEvent_DeliveredAndChannelsClose(t *testing.T) {
	fc := &fakeClient{
		openParams: make(chan protocol.TerminalOpenParams, 1),
		dataPush:   make(chan protocol.TerminalDataEvent, 1),
		exitPush:   make(chan protocol.TerminalExitEvent, 1),
	}
	be := remote.NewBackend(fc)
	h, err := be.Open(context.Background(), pty.Spec{Cwd: "/r"})
	require.NoError(t, err)
	<-fc.openParams

	fc.exitPush <- protocol.TerminalExitEvent{TerminalID: "remote-1", Code: 0, Reason: "natural"}
	close(fc.exitPush)

	select {
	case info := <-h.Exit():
		require.Equal(t, "natural", info.Reason)
	case <-time.After(time.Second):
		t.Fatal("no exit info")
	}
}

func TestRemoteBackend_ConnectionLost_EmitsConnectionLost(t *testing.T) {
	fc := &fakeClient{
		openParams: make(chan protocol.TerminalOpenParams, 1),
		dataPush:   make(chan protocol.TerminalDataEvent, 1),
		exitPush:   make(chan protocol.TerminalExitEvent), // unbuffered + closed = connection lost
	}
	be := remote.NewBackend(fc)
	h, err := be.Open(context.Background(), pty.Spec{Cwd: "/r"})
	require.NoError(t, err)
	<-fc.openParams

	close(fc.exitPush)

	select {
	case info := <-h.Exit():
		require.Equal(t, "connection_lost", info.Reason)
	case <-time.After(time.Second):
		t.Fatal("no exit info")
	}
}
```

- [ ] **Step 2: Run to verify FAIL**

```bash
go test ./internal/pkg/pty/remote/...
```
Expected: FAIL — package missing.

- [ ] **Step 3: Implement**

```go
// internal/pkg/pty/remote/remote.go
// Package remote implements pty.Backend by relaying ops over an agentred
// JSON-RPC-over-WebSocket client.
package remote

import (
	"context"
	"errors"
	"sync"

	pkgpty "github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"
)

// Client is the minimal subset of the agentred ws client surface needed
// here. In production this is the existing per-device ws client; tests
// stub it.
type Client interface {
	Call(ctx context.Context, method string, params any, out any) error
	SubscribeData(terminalID string) <-chan protocol.TerminalDataEvent
	SubscribeExit(terminalID string) <-chan protocol.TerminalExitEvent
}

type Backend struct {
	client Client
}

func NewBackend(c Client) *Backend { return &Backend{client: c} }

func (b *Backend) Open(ctx context.Context, spec pkgpty.Spec) (pkgpty.Handle, error) {
	var res protocol.TerminalOpenResult
	if err := b.client.Call(ctx, "terminal.open", protocol.TerminalOpenParams{
		Cwd: spec.Cwd, Shell: spec.Shell, Env: spec.Env, Cols: spec.Cols, Rows: spec.Rows,
	}, &res); err != nil {
		return nil, err
	}
	h := &handleImpl{
		client:     b.client,
		terminalID: res.TerminalID,
		data:       make(chan []byte, 32),
		exit:       make(chan pkgpty.ExitInfo, 1),
		done:       make(chan struct{}),
	}
	go h.pump()
	return h, nil
}

type handleImpl struct {
	client     Client
	terminalID string

	data chan []byte
	exit chan pkgpty.ExitInfo

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

func (h *handleImpl) Write(p []byte) (int, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return 0, errors.New("remote pty closed")
	}
	h.mu.Unlock()
	var ack struct{}
	err := h.client.Call(context.Background(), "terminal.write", protocol.TerminalWriteParams{
		TerminalID: h.terminalID, Data: string(p),
	}, &ack)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (h *handleImpl) Resize(cols, rows uint16) error {
	var ack struct{}
	return h.client.Call(context.Background(), "terminal.resize", protocol.TerminalResizeParams{
		TerminalID: h.terminalID, Cols: cols, Rows: rows,
	}, &ack)
}

func (h *handleImpl) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	close(h.done)
	h.mu.Unlock()
	var ack struct{}
	return h.client.Call(context.Background(), "terminal.close", protocol.TerminalCloseParams{
		TerminalID: h.terminalID,
	}, &ack)
}

func (h *handleImpl) Data() <-chan []byte          { return h.data }
func (h *handleImpl) Exit() <-chan pkgpty.ExitInfo { return h.exit }

func (h *handleImpl) pump() {
	dataCh := h.client.SubscribeData(h.terminalID)
	exitCh := h.client.SubscribeExit(h.terminalID)
	for {
		select {
		case ev, ok := <-dataCh:
			if !ok {
				return
			}
			select {
			case h.data <- []byte(ev.Data):
			case <-h.done:
				return
			}
		case ev, ok := <-exitCh:
			if !ok {
				h.exit <- pkgpty.ExitInfo{Reason: "connection_lost"}
			} else {
				h.exit <- pkgpty.ExitInfo{Code: ev.Code, Reason: ev.Reason, Msg: ev.Msg}
			}
			close(h.exit)
			close(h.data)
			return
		case <-h.done:
			return
		}
	}
}

var _ pkgpty.Backend = (*Backend)(nil)
var _ pkgpty.Handle = (*handleImpl)(nil)
```

- [ ] **Step 4: Run to verify PASS**

```bash
go test ./internal/pkg/pty/remote/...
```
Expected: PASS, 3 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/pty/remote/remote.go internal/pkg/pty/remote/remote_test.go
git commit -m "feat(pty/remote): agentred-client-backed Backend with subscribe pump"
```

---

### Task 21: Wire terminal_svc.Service into app.go

**Files:**
- Modify: `internal/app/app.go`
- Create: `internal/app/terminal_wiring.go`

- [ ] **Step 1: Read where chat_svc is wired**

```bash
sed -n '95,140p' /Users/codfrm/Code/agentre/agentre/internal/app/app.go
```

- [ ] **Step 2: Investigate remote client lookup**

Before writing the remote factory, find the existing per-device ws client used by chat dispatch:

```bash
grep -rn "selectRunner\|deviceClient\|remoteClient" /Users/codfrm/Code/agentre/agentre/internal/service/chat_svc/ /Users/codfrm/Code/agentre/agentre/internal/pkg/agentruntime/runtimes/remote/ 2>/dev/null | head -20
```

If the existing client doesn't expose `Call` / `SubscribeData` / `SubscribeExit` exactly, reuse the closest pattern. If it's genuinely a different shape, **add a thin adapter inside `internal/pkg/pty/remote/`** (or a new helper file in the daemon client package) that conforms to the `remote.Client` interface — do **not** rewrite the existing client.

If you can't identify the existing client at all, **STOP and surface to user**. The plan assumed there is one.

- [ ] **Step 3: Create wiring file**

```go
// internal/app/terminal_wiring.go
package app

import (
	"context"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/pkg/pty/local"
	"github.com/agentre-ai/agentre/internal/pkg/pty/remote"
	agentbackendrepo "github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	chatrepo "github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	"github.com/agentre-ai/agentre/internal/service/terminal_svc"
)

type sessionLookupAdapter struct{}

func (sessionLookupAdapter) Lookup(ctx context.Context, sessionID int64) (
	*chat_entity.Session, *agent_backend_entity.AgentBackend, string, error,
) {
	sess, err := chatrepo.Session().Find(ctx, sessionID)
	if err != nil {
		return nil, nil, "", err
	}
	be, _ := agentbackendrepo.AgentBackend().FindByAgent(ctx, sess.AgentID)
	cwd, err := chat_svc.ResolveSessionCwd(ctx, sess, be)
	if err != nil {
		return nil, nil, "", err
	}
	return sess, be, cwd, nil
}

// ptyBackendAdapter bridges pty.Backend → terminal_svc.PTYBackend (same
// method set in different packages; explicit wrapper required by Go's
// nominal typing).
type ptyBackendAdapter struct{ be pty.Backend }

func (a ptyBackendAdapter) Open(ctx context.Context, spec pty.Spec) (pty.Handle, error) {
	return a.be.Open(ctx, spec)
}

func newTerminalService(appCtx context.Context) *terminal_svc.Service {
	localBE := local.NewBackend()
	remoteFactory := func(deviceID string) (terminal_svc.PTYBackend, error) {
		client, err := resolveAgentredClient(deviceID) // see Step 2 above
		if err != nil {
			return nil, err
		}
		return ptyBackendAdapter{be: remote.NewBackend(client)}, nil
	}
	selector := terminal_svc.NewBackendSelector(
		ptyBackendAdapter{be: localBE}, remoteFactory,
	)
	emitter := terminal_svc.EmitterFunc(func(_ context.Context, name string, payload any) {
		wailsruntime.EventsEmit(appCtx, name, payload)
	})
	return terminal_svc.NewService(sessionLookupAdapter{}, selector, emitter)
}
```

`resolveAgentredClient(deviceID)` is the helper identified in Step 2. Write it in this file or wherever the existing client lookup lives. If you wrote an adapter to conform to `remote.Client`, return that.

- [ ] **Step 4: Wire into App**

In `internal/app/app.go`:

- Add import: `"github.com/agentre-ai/agentre/internal/service/terminal_svc"`
- Add field on App struct: `terminalSvc *terminal_svc.Service`
- In `Startup`, after the existing chat_svc wiring (search for `chat_svc.RegisterChat` ~line 122): `a.terminalSvc = newTerminalService(a.ctx)`
- In `Shutdown`: `if a.terminalSvc != nil { a.terminalSvc.Shutdown() }`
- Add `a.terminalSvc` to the Wails options.Bind slice (find the existing Bind block; chat_svc service is the template)

- [ ] **Step 5: Generate Wails bindings**

```bash
cd /Users/codfrm/Code/agentre/agentre
make generate
```

Expected: `frontend/wailsjs/go/terminal_svc/Service.js` and `.d.ts` appear.

- [ ] **Step 6: Run full test**

```bash
make test
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/app/app.go internal/app/terminal_wiring.go frontend/wailsjs/go/terminal_svc/
git commit -m "feat(app): wire terminal_svc.Service into Wails bindings"
```

---

### Task 22: Frontend — chat-terminal-store (Zustand toggle state)

**Files:**
- Create: `frontend/src/stores/chat-terminal-store.ts`
- Create: `frontend/src/stores/chat-terminal-store.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/stores/chat-terminal-store.test.ts
import { describe, it, expect, beforeEach } from 'vitest';
import { useChatTerminalStore } from './chat-terminal-store';

describe('useChatTerminalStore', () => {
  beforeEach(() => {
    useChatTerminalStore.setState({ openSessionID: null });
  });

  it('defaults to null', () => {
    expect(useChatTerminalStore.getState().openSessionID).toBeNull();
  });

  it('toggle(7) opens session 7', () => {
    useChatTerminalStore.getState().toggle(7);
    expect(useChatTerminalStore.getState().openSessionID).toBe(7);
  });

  it('toggle(7) twice closes', () => {
    useChatTerminalStore.getState().toggle(7);
    useChatTerminalStore.getState().toggle(7);
    expect(useChatTerminalStore.getState().openSessionID).toBeNull();
  });

  it('toggle(8) while 7 is open switches to 8', () => {
    useChatTerminalStore.getState().toggle(7);
    useChatTerminalStore.getState().toggle(8);
    expect(useChatTerminalStore.getState().openSessionID).toBe(8);
  });
});
```

- [ ] **Step 2: Run to verify FAIL**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm test -- chat-terminal-store
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```ts
// frontend/src/stores/chat-terminal-store.ts
import { create } from 'zustand';

interface ChatTerminalState {
  openSessionID: number | null;
  toggle: (sessionID: number) => void;
  closeAll: () => void;
}

export const useChatTerminalStore = create<ChatTerminalState>((set, get) => ({
  openSessionID: null,
  toggle: (sessionID) => {
    const current = get().openSessionID;
    set({ openSessionID: current === sessionID ? null : sessionID });
  },
  closeAll: () => set({ openSessionID: null }),
}));
```

- [ ] **Step 4: Run to verify PASS**

```bash
pnpm test -- chat-terminal-store
```
Expected: PASS, 4 tests.

- [ ] **Step 5: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/stores/chat-terminal-store.ts frontend/src/stores/chat-terminal-store.test.ts
git commit -m "feat(frontend): chat-terminal-store with toggle/switch semantics"
```

---

### Task 23: Frontend — use-terminal hook

**Files:**
- Create: `frontend/src/components/agentre/terminal/use-terminal.ts`
- Create: `frontend/src/components/agentre/terminal/use-terminal.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/components/agentre/terminal/use-terminal.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useTerminal } from './use-terminal';

vi.mock('@/wailsjs/go/terminal_svc/Service', () => ({
  Open: vi.fn().mockResolvedValue(undefined),
  Write: vi.fn().mockResolvedValue(undefined),
  Resize: vi.fn().mockResolvedValue(undefined),
  Close: vi.fn().mockResolvedValue(undefined),
}));

const onHandlers: Record<string, (payload: any) => void> = {};
vi.mock('@/wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn((name: string, cb: (payload: any) => void) => {
    onHandlers[name] = cb;
    return () => { delete onHandlers[name]; };
  }),
  EventsOff: vi.fn((name: string) => { delete onHandlers[name]; }),
}));

import * as TerminalSvc from '@/wailsjs/go/terminal_svc/Service';

beforeEach(() => {
  vi.clearAllMocks();
  for (const k of Object.keys(onHandlers)) delete onHandlers[k];
});

describe('useTerminal', () => {
  it('calls Open(sessionID, cols, rows) on mount and subscribes to events', async () => {
    const { result } = renderHook(() => useTerminal({ sessionID: 7, cols: 80, rows: 24 }));
    await act(async () => { await Promise.resolve(); });
    expect(TerminalSvc.Open).toHaveBeenCalledWith(7, 80, 24);
    expect(onHandlers['terminal:7:data']).toBeTypeOf('function');
    expect(onHandlers['terminal:7:exit']).toBeTypeOf('function');
    expect(result.current.state).toBe('open');
  });

  it('exposes incoming data via onData callback', async () => {
    const onData = vi.fn();
    renderHook(() => useTerminal({ sessionID: 7, cols: 80, rows: 24, onData }));
    await act(async () => { await Promise.resolve(); });
    act(() => onHandlers['terminal:7:data']({ data: 'hello' }));
    expect(onData).toHaveBeenCalledWith('hello');
  });

  it('calls Close and EventsOff on unmount', async () => {
    const { unmount } = renderHook(() => useTerminal({ sessionID: 7, cols: 80, rows: 24 }));
    await act(async () => { await Promise.resolve(); });
    unmount();
    expect(TerminalSvc.Close).toHaveBeenCalledWith(7);
    expect(onHandlers['terminal:7:data']).toBeUndefined();
  });

  it('write() proxies to TerminalSvc.Write', async () => {
    const { result } = renderHook(() => useTerminal({ sessionID: 7, cols: 80, rows: 24 }));
    await act(async () => { await Promise.resolve(); });
    await act(async () => { await result.current.write('ls\n'); });
    expect(TerminalSvc.Write).toHaveBeenCalledWith(7, 'ls\n');
  });
});
```

- [ ] **Step 2: Run to verify FAIL**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm test -- use-terminal
```
Expected: FAIL.

- [ ] **Step 3: Implement**

```ts
// frontend/src/components/agentre/terminal/use-terminal.ts
import { useEffect, useState, useCallback } from 'react';
import * as TerminalSvc from '@/wailsjs/go/terminal_svc/Service';
import { EventsOn, EventsOff } from '@/wailsjs/runtime/runtime';

type Reason = 'natural' | 'killed' | 'connection_lost' | 'daemon_shutdown' | 'error';
export type TerminalState = 'opening' | 'open' | 'idle';

export interface UseTerminalArgs {
  sessionID: number;
  cols: number;
  rows: number;
  onData?: (data: string) => void;
  onExit?: (info: { code: number; reason: Reason; msg?: string }) => void;
}

export function useTerminal(args: UseTerminalArgs) {
  const [state, setState] = useState<TerminalState>('opening');

  const dataEvent = `terminal:${args.sessionID}:data`;
  const exitEvent = `terminal:${args.sessionID}:exit`;

  useEffect(() => {
    let cancelled = false;

    EventsOn(dataEvent, (payload: { data: string }) => {
      args.onData?.(payload.data);
    });
    EventsOn(exitEvent, (payload: { code: number; reason: Reason; msg?: string }) => {
      args.onExit?.(payload);
      setState('idle');
      EventsOff(dataEvent);
      EventsOff(exitEvent);
    });

    TerminalSvc.Open(args.sessionID, args.cols, args.rows).then(
      () => {
        if (cancelled) {
          TerminalSvc.Close(args.sessionID);
          return;
        }
        setState('open');
      },
      (err) => {
        if (!cancelled) {
          setState('idle');
          args.onExit?.({ code: -1, reason: 'error', msg: String(err) });
        }
        EventsOff(dataEvent);
        EventsOff(exitEvent);
      },
    );

    return () => {
      cancelled = true;
      EventsOff(dataEvent);
      EventsOff(exitEvent);
      TerminalSvc.Close(args.sessionID).catch(() => {});
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [args.sessionID]);

  const write = useCallback(
    (data: string) => TerminalSvc.Write(args.sessionID, data),
    [args.sessionID],
  );

  const resize = useCallback(
    (cols: number, rows: number) => TerminalSvc.Resize(args.sessionID, cols, rows),
    [args.sessionID],
  );

  return { state, write, resize };
}
```

- [ ] **Step 4: Run to verify PASS**

```bash
pnpm test -- use-terminal
```
Expected: PASS, 4 tests.

- [ ] **Step 5: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/terminal/use-terminal.ts frontend/src/components/agentre/terminal/use-terminal.test.ts
git commit -m "feat(frontend): useTerminal hook with Open/Close lifecycle + events"
```

---

### Task 24: Frontend — TerminalPanel (xterm.js renderer)

**Files:**
- Create: `frontend/src/components/agentre/terminal/terminal-panel.tsx`
- Create: `frontend/src/components/agentre/terminal/terminal-panel.test.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
// frontend/src/components/agentre/terminal/terminal-panel.test.tsx
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { TerminalPanel } from './terminal-panel';

const writeMock = vi.fn();
const onDataMock = vi.fn();
const openMock = vi.fn();
const disposeMock = vi.fn();
vi.mock('@xterm/xterm', () => ({
  Terminal: vi.fn().mockImplementation(() => ({
    open: openMock,
    write: writeMock,
    onData: (cb: (s: string) => void) => { onDataMock.mockImplementation(cb); return { dispose: () => {} }; },
    loadAddon: vi.fn(),
    dispose: disposeMock,
    cols: 80, rows: 24,
  })),
}));
vi.mock('@xterm/addon-fit', () => ({
  FitAddon: vi.fn().mockImplementation(() => ({ fit: vi.fn(), proposeDimensions: () => ({ cols: 80, rows: 24 }) })),
}));
vi.mock('@xterm/addon-web-links', () => ({ WebLinksAddon: vi.fn() }));

const writeProxy = vi.fn();
vi.mock('./use-terminal', () => ({
  useTerminal: vi.fn().mockImplementation((args: any) => ({
    state: 'open',
    write: writeProxy,
    resize: vi.fn(),
  })),
}));
import { useTerminal } from './use-terminal';

describe('TerminalPanel', () => {
  it('mounts xterm, opens hook with sessionID, writes incoming data', () => {
    render(<TerminalPanel sessionID={42} />);
    expect(useTerminal).toHaveBeenCalled();
    const args: any = (useTerminal as any).mock.calls[0][0];
    expect(args.sessionID).toBe(42);
    act(() => args.onData('hello'));
    expect(writeMock).toHaveBeenCalledWith('hello');
  });

  it('proxies xterm onData to hook write()', () => {
    render(<TerminalPanel sessionID={42} />);
    act(() => onDataMock('typed-key'));
    expect(writeProxy).toHaveBeenCalledWith('typed-key');
  });

  it('disposes xterm on unmount', () => {
    const { unmount } = render(<TerminalPanel sessionID={42} />);
    unmount();
    expect(disposeMock).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run to verify FAIL**

```bash
pnpm test -- terminal-panel
```
Expected: FAIL — file missing.

- [ ] **Step 3: Implement**

```tsx
// frontend/src/components/agentre/terminal/terminal-panel.tsx
import { useEffect, useRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';

import { useTerminal } from './use-terminal';

export interface TerminalPanelProps {
  sessionID: number;
}

export function TerminalPanel({ sessionID }: TerminalPanelProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;
    const term = new Terminal({
      fontFamily: "'JetBrains Mono', 'Menlo', 'Monaco', monospace",
      fontSize: 13,
      theme: { background: '#0b1220', foreground: '#e2e8f0' },
      scrollback: 500,
      cursorBlink: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(containerRef.current);
    fit.fit();
    xtermRef.current = term;
    fitRef.current = fit;

    return () => {
      term.dispose();
      xtermRef.current = null;
      fitRef.current = null;
    };
  }, []);

  const { write, resize } = useTerminal({
    sessionID,
    cols: xtermRef.current?.cols ?? 80,
    rows: xtermRef.current?.rows ?? 24,
    onData: (data) => xtermRef.current?.write(data),
    onExit: () => { /* parent toggle store will unmount us */ },
  });

  useEffect(() => {
    const term = xtermRef.current;
    if (!term) return;
    const sub = term.onData((d) => { void write(d); });
    return () => sub.dispose();
  }, [write]);

  useEffect(() => {
    if (!containerRef.current) return;
    const ro = new ResizeObserver(() => {
      const f = fitRef.current; const t = xtermRef.current;
      if (!f || !t) return;
      f.fit();
      void resize(t.cols, t.rows);
    });
    ro.observe(containerRef.current);
    return () => ro.disconnect();
  }, [resize]);

  return (
    <div className="flex-1 min-h-0 bg-[#0b1220]" data-testid="terminal-panel">
      <div ref={containerRef} className="h-full w-full p-2" />
    </div>
  );
}
```

- [ ] **Step 4: Run to verify PASS**

```bash
pnpm test -- terminal-panel
```
Expected: PASS, 3 tests.

- [ ] **Step 5: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/terminal/terminal-panel.tsx frontend/src/components/agentre/terminal/terminal-panel.test.tsx
git commit -m "feat(frontend): TerminalPanel xterm container with hook wiring"
```

---

### Task 25: Frontend — Terminal toggle button in chat topbar

**Files:**
- Modify: `frontend/src/components/agentre/chat-page.tsx`

- [ ] **Step 1: Locate topbar**

```bash
grep -n "topbar\|topBar\|Header\|toolbar" /Users/codfrm/Code/agentre/agentre/frontend/src/components/agentre/chat-page.tsx | head -20
```

Identify the JSX containing existing action buttons (e.g., More menu, session title). The new button slots beside them.

- [ ] **Step 2: Add the button (no test — covered by integration test in next task)**

In `chat-page.tsx`, add the import:

```tsx
import { TerminalSquare } from 'lucide-react';
import { useChatTerminalStore } from '@/stores/chat-terminal-store';
```

Inside the component, near where `currentSessionID` (or whatever the active session id variable is) is available:

```tsx
const openTerminalSessionID = useChatTerminalStore((s) => s.openSessionID);
const toggleTerminal = useChatTerminalStore((s) => s.toggle);
const terminalOn = openTerminalSessionID === currentSessionID;
```

In the topbar JSX, add right next to other action buttons:

```tsx
<button
  type="button"
  title="终端"
  aria-pressed={terminalOn}
  onClick={() => toggleTerminal(currentSessionID)}
  className={
    terminalOn
      ? "rounded-md bg-slate-900 text-white px-2 py-1"
      : "rounded-md border border-slate-300 text-slate-600 hover:bg-slate-100 px-2 py-1"
  }
>
  <TerminalSquare className="h-4 w-4" />
</button>
```

Match the project's button utility classes — search `chat-page.tsx` for existing button styling and copy the convention. The classes above are a sensible fallback if no convention exists.

- [ ] **Step 3: Build + run frontend**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm build
```
Expected: clean build.

- [ ] **Step 4: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/chat-page.tsx
git commit -m "feat(frontend): topbar terminal toggle button bound to store"
```

---

### Task 26: Frontend — Global ⌘` / Ctrl+` shortcut to toggle terminal

Per spec §4 "键盘快捷键 (toggle)": register a global keydown listener on the chat shell. Fires `toggleTerminal(currentSessionID)` on backtick + Cmd (macOS) / Ctrl (Windows/Linux). Must also fire when focus is inside an `<input>` / `<textarea>` / xterm container (it's a toggle, not character input). No-op when there is no current session.

**Files:**
- Modify: `frontend/src/components/agentre/chat-page.tsx`
- Create: `frontend/src/components/agentre/chat-page.test.tsx` (if no existing test file — confirm via `ls frontend/src/components/agentre/chat-page.test.*`)

- [ ] **Step 1: Write the failing test**

Create or append:

```tsx
import { render, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';

const toggleMock = vi.fn();
vi.mock('@/stores/chat-terminal-store', () => ({
  useChatTerminalStore: (selector: any) =>
    selector({ openSessionID: null, toggle: toggleMock }),
}));

// Adapt this import to wherever the chat shell component actually lives.
import { ChatPage } from './chat-page';

describe('chat-page ⌘` shortcut', () => {
  beforeEach(() => toggleMock.mockReset());

  it('toggles terminal for current session on Meta+Backtick', () => {
    render(<ChatPage sessionID={7} /* …minimal valid props… */ />);
    fireEvent.keyDown(window, { key: '`', metaKey: true });
    expect(toggleMock).toHaveBeenCalledWith(7);
  });

  it('toggles on Ctrl+Backtick as well', () => {
    render(<ChatPage sessionID={7} />);
    fireEvent.keyDown(window, { key: '`', ctrlKey: true });
    expect(toggleMock).toHaveBeenCalledWith(7);
  });

  it('does not fire without modifier', () => {
    render(<ChatPage sessionID={7} />);
    fireEvent.keyDown(window, { key: '`' });
    expect(toggleMock).not.toHaveBeenCalled();
  });

  it('no-ops when there is no current session', () => {
    render(<ChatPage sessionID={null as any} />);
    fireEvent.keyDown(window, { key: '`', metaKey: true });
    expect(toggleMock).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run to verify FAIL**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm test -- chat-page
```
Expected: FAIL (handler not registered).

- [ ] **Step 3: Implement the listener**

In `chat-page.tsx`, add (next to where `toggleTerminal` is already pulled from the store in Task 25):

```tsx
import { useEffect } from 'react';

useEffect(() => {
  if (currentSessionID == null) return;
  const handler = (e: KeyboardEvent) => {
    if (e.key !== '`') return;
    if (!(e.metaKey || e.ctrlKey)) return;
    e.preventDefault();
    toggleTerminal(currentSessionID);
  };
  window.addEventListener('keydown', handler);
  return () => window.removeEventListener('keydown', handler);
}, [currentSessionID, toggleTerminal]);
```

Notes for the implementer:
- Use `window` (not `document`) so it survives any internal stopPropagation in nested handlers.
- `preventDefault` avoids any accidental browser default (none for `` ` `` today, but cheap insurance).
- **Do not** add a "skip if target is INPUT/TEXTAREA" guard — per spec the shortcut must fire from anywhere; backtick alone requires `Shift+\`` to type, so swallowing it on Cmd/Ctrl is safe.

- [ ] **Step 4: Run to verify PASS**

```bash
pnpm test -- chat-page
```
Expected: PASS (all 4 cases green).

- [ ] **Step 5: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/chat-page.tsx frontend/src/components/agentre/chat-page.test.tsx
git commit -m "feat(frontend): bind cmd/ctrl+backtick to terminal toggle"
```

---

### Task 27: Frontend — chat-streams-host renders TerminalPanel when toggle is ON

**Files:**
- Modify: `frontend/src/components/agentre/chat-streams-host.tsx` (or wherever the chat body is rendered — confirm via `find frontend/src -name 'chat-streams-host.*'`)
- Modify or create: a co-located test file

- [ ] **Step 1: Locate the chat body render site**

```bash
grep -n "Composer\|messages.map\|chat-body" /Users/codfrm/Code/agentre/agentre/frontend/src/components/agentre/chat-streams-host.tsx /Users/codfrm/Code/agentre/agentre/frontend/src/components/agentre/chat-panel-host.tsx 2>/dev/null
```

- [ ] **Step 2: Write the failing test**

Create or append to the test file co-located with the chat body component:

```tsx
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';

vi.mock('@/stores/chat-terminal-store', () => ({
  useChatTerminalStore: vi.fn(),
}));
import { useChatTerminalStore } from '@/stores/chat-terminal-store';

vi.mock('@/components/agentre/terminal/terminal-panel', () => ({
  TerminalPanel: ({ sessionID }: { sessionID: number }) => (
    <div data-testid="mocked-terminal">terminal-for-{sessionID}</div>
  ),
}));

// ChatStreamsHost is the body-rendering component. Adapt this import to
// the actual filename / export.
import { ChatStreamsHost } from './chat-streams-host';

describe('chat-streams-host terminal swap', () => {
  it('renders TerminalPanel when openSessionID matches current session', () => {
    (useChatTerminalStore as any).mockImplementation((selector: any) =>
      selector({ openSessionID: 5 }),
    );
    render(<ChatStreamsHost sessionID={5} /* …minimal valid props… */ />);
    expect(screen.getByTestId('mocked-terminal')).toHaveTextContent('terminal-for-5');
  });

  it('renders chat content when openSessionID is null', () => {
    (useChatTerminalStore as any).mockImplementation((selector: any) =>
      selector({ openSessionID: null }),
    );
    render(<ChatStreamsHost sessionID={5} />);
    expect(screen.queryByTestId('mocked-terminal')).toBeNull();
  });
});
```

If `ChatStreamsHost` has required props that are hard to mock, look at any existing test file for the chat body component and copy the test boilerplate.

- [ ] **Step 3: Run to verify FAIL**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm test -- chat-streams-host
```
Expected: FAIL.

- [ ] **Step 4: Implement swap**

In `chat-streams-host.tsx` (or whichever file renders the chat message body), add:

```tsx
import { TerminalPanel } from '@/components/agentre/terminal/terminal-panel';
import { useChatTerminalStore } from '@/stores/chat-terminal-store';

// Inside the component:
const openSessionID = useChatTerminalStore((s) => s.openSessionID);
const terminalActive = openSessionID === sessionID;

// Wrap the existing message-stream JSX:
if (terminalActive) {
  return (
    <div className="flex flex-col h-full">
      <TerminalPanel sessionID={sessionID} />
    </div>
  );
}
// …else existing chat body JSX (messages + composer) renders as before
```

- [ ] **Step 5: Run to verify PASS**

```bash
pnpm test -- chat-streams-host
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/chat-streams-host.tsx frontend/src/components/agentre/chat-streams-host.test.tsx
git commit -m "feat(frontend): swap chat body for TerminalPanel when toggle is on"
```

---

### Task 28: Integration smoke test — local PTY through service

**Files:**
- Create: `internal/service/terminal_svc/integration_test.go`

- [ ] **Step 1: Write the test**

```go
// internal/service/terminal_svc/integration_test.go
//go:build !windows

package terminal_svc_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/pkg/pty/local"
	"github.com/agentre-ai/agentre/internal/service/terminal_svc"

	"github.com/stretchr/testify/require"
)

// localBackendBridge mirrors the production app/terminal_wiring.go adapter.
type localBackendBridge struct{ be *local.Backend }

func (b localBackendBridge) Open(ctx context.Context, spec pty.Spec) (pty.Handle, error) {
	return b.be.Open(ctx, spec)
}

type collectingEmitter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *collectingEmitter) Emit(_ context.Context, _ string, payload any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := payload.(map[string]string); ok {
		c.buf.WriteString(m["data"])
	}
}

func (c *collectingEmitter) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buf.Bytes()...)
}

func TestIntegration_LocalPTY_HappyPath(t *testing.T) {
	sel := terminal_svc.NewBackendSelector(
		localBackendBridge{be: local.NewBackend()}, nil,
	)
	emit := &collectingEmitter{}
	svc := terminal_svc.NewService(stubSessionLookup{
		sess: &chat_entity.Session{ID: 1},
		be:   &agent_backend_entity.AgentBackend{DeviceID: ""},
		cwd:  "/tmp",
	}, sel, emit)
	t.Cleanup(svc.Shutdown)

	require.NoError(t, svc.Open(context.Background(), 1, 80, 24))
	require.NoError(t, svc.Write(context.Background(), 1, "echo integ-test\n"))

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("did not see echo output; got: %q", string(emit.Bytes()))
		case <-time.After(50 * time.Millisecond):
			if bytes.Contains(emit.Bytes(), []byte("integ-test")) {
				return
			}
		}
	}
}
```

- [ ] **Step 2: Run to verify PASS**

```bash
cd /Users/codfrm/Code/agentre/agentre
go test -run TestIntegration_LocalPTY_HappyPath ./internal/service/terminal_svc/...
```
Expected: PASS within ~5s.

- [ ] **Step 3: Commit**

```bash
git add internal/service/terminal_svc/integration_test.go
git commit -m "test(terminal_svc): integration smoke covering Open→Write→data event"
```

---

### Task 29: Run full lint + test suite

**Files:** any drift found

- [ ] **Step 1: Run lint**

```bash
cd /Users/codfrm/Code/agentre/agentre
make lint
```
Expected: PASS. If new files trip lint (unused imports etc.), fix only the files this plan touched.

- [ ] **Step 2: Run full test suite**

```bash
make test
```
Expected: PASS for `go test -race ./...` and `pnpm test`.

- [ ] **Step 3: Run a full build**

```bash
make generate
go build ./...
cd frontend && pnpm build
cd ..
```
Expected: clean build, no Wails binding errors.

- [ ] **Step 4: Commit any drift fixes if needed**

```bash
git add -p
git commit -m "chore: lint/build drift fixes after terminal integration"
```

(Skip if there's no drift.)

---

### Task 30: Manual UI verification

This task is non-automated. Do not declare the feature done until these pass.

- [ ] **Step 1: Run dev mode**

```bash
cd /Users/codfrm/Code/agentre/agentre
make dev
```

- [ ] **Step 2: Verify local session terminal**

1. Open a local session (`Backend.DeviceID == ""`).
2. Click the terminal toggle in the chat topbar.
3. Verify chat view replaced by a black terminal showing a shell prompt at the session's cwd.
4. Type `pwd` — verify it matches the session's cwd.
5. Type `ls` — verify output appears.
6. Resize the app window — verify terminal reflows (`stty size` confirms).
7. Click toggle again — verify chat view returns and the shell is killed (run `ps aux | grep -E '/bin/(z?sh)'` in your real terminal to confirm).

- [ ] **Step 3: Verify remote session terminal** (only if you have a paired agentred peer)

1. Open a session whose Agent has a non-empty `DeviceID`.
2. Click the terminal toggle.
3. Run `hostname` — verify it returns the remote box's hostname.
4. Stop the remote daemon — verify the terminal closes / shows an error.

- [ ] **Step 4: Log any discrepancies**

If something doesn't match the spec, write up the discrepancy and surface it. The spec acknowledges these deferred items (no Windows, no scrollback persistence, no background-keep-alive on session switch, no explicit OPENING/CLOSING spinner); anything else discovered here is real feedback.

---

## Self-Review

### Spec coverage

- **Architecture** → Tasks 4, 5–9 (local), 20 (remote), 17 (service), 21 (wiring) ✓
- **Components / Modules** → Tasks 3, 4, 5, 10, 15, 16, 17, 20, 22, 23, 24 cover all listed files ✓
- **Protocol / Wire format** → Task 3 (RPC types), Task 15 (event names), Task 17 (Wails service signatures) ✓
- **UI Behavior — toggle button + replace** → Tasks 22 (store), 25 (button), 26 (swap) ✓
- **UI Behavior — Cmd+C selection-aware copy** → not explicit; xterm's default behavior is close. Add as follow-up if dogfooding reveals UX gap.
- **UI Behavior — OPENING/CLOSING spinner** → simplified to a binary `state: 'opening' | 'open' | 'idle'` field in `useTerminal`; no spinner overlay in MVP. Easy to add later.
- **Error Handling** → Task 6 (bad cwd), 8 (kill), 9 (natural exit), 11 (NotFound), 12 (exit clearing), 17 (re-open closes previous), 18 (Close on unknown / Shutdown) ✓
- **Error Handling — connection_lost banner** → hook surfaces it via `onExit({reason:'connection_lost'})` but UI just unmounts; visual banner not in plan. Add follow-up if needed.
- **Testing Strategy** → Layered across Tasks 4, 5–9, 10–13, 15–20, 23, 24, 26, 27 ✓
- **Backpressure (spec §3 "Wire Format")** → **NOT in this plan as an explicit task.** Current daemon pump emits synchronously; if the consumer/emitter blocks, the PTY reader chain throttles. For MVP this is acceptable (the user accepted "lose old chunks + insert marker" but that requires an explicit buffer on the daemon ws-out side, which depends on the existing daemon emitter shape we couldn't fully inspect). **Flag as follow-up before shipping to anyone running `yes` or `find /` on a remote agentred.**
- **Persistence non-goal** → satisfied implicitly: zero SQLite writes in any task ✓

### Placeholder scan

- No "TBD" / "implement later" found.
- Task 13 (daemon registration) and Task 21 (app wiring) both contain "**STOP and surface to user**" escalation points where the existing codebase shape is uncertain. These are deliberate — the alternative is making up a primitive (`broadcastEvent`, `resolveAgentredClient`) that may not exist. Implementer should investigate and either find / write a thin shim, or escalate.

### Type / name consistency

- `pty.Handle` used uniformly across all Go files ✓
- `terminal_svc.PTYBackend` (factory) vs `pty.Backend` (lower-level factory): same method signature; `ptyBackendAdapter` in Task 21 bridges them — naming chosen to make this explicit ✓
- `DataEventName(sessionID)` / `ExitEventName(sessionID)` used in service (Go) and matched by frontend literal strings `terminal:${sessionID}:data` / `:exit` in `use-terminal.ts` ✓
- `EventNameTerminalData` / `EventNameTerminalExit` in daemon are different (RPC method names, not Wails event names) — that's by design; the daemon push goes through the agentred ws layer to the desktop, which then re-emits to the frontend with the Wails event name ✓
- `TerminalOpenParams` / `TerminalOpenResult` / `TerminalWriteParams` / `TerminalResizeParams` / `TerminalCloseParams` / `TerminalDataEvent` / `TerminalExitEvent` consistent across daemon handler tests, remote backend tests, and protocol package ✓

### Follow-ups identified (post-MVP)

1. **Backpressure on agentred push pipeline** — add explicit channel buffer + "output throttled" marker emission.
2. **OPENING / CLOSING spinner UI** — visual feedback while async Open/Close is in flight.
3. **Selection-aware Cmd+C on macOS** — override xterm default to copy selection (already supported by xterm but needs verification + possibly a hotkey hook).
4. **Connection-lost banner** — distinct visual treatment beyond "panel just unmounts".
5. **Windows support** — `ConPTY` via `creack/pty`; add `//go:build windows` variant and address edge cases.

---

## Notes for Implementers

1. **mockgen**: project uses `go.uber.org/mock/gomock`. Ensure mockgen is installed: `go install go.uber.org/mock/mockgen@latest`. `make mock` runs `go generate ./...`.
2. **Test framework mix**: per existing project convention, daemon handler tests use `testify + gomock` (matches `internal/daemon/handlers/session_test.go`); some service tests use goconvey. This plan uses testify+gomock throughout for consistency. CLAUDE.md mentions both — either is fine.
3. **`resolveAgentredClient(deviceID)` (Task 21)**: must reuse the existing client used by `selectRunner()` in `internal/service/chat_svc/chat.go` (~line 2718). If the existing client doesn't expose `Call` / `SubscribeData` / `SubscribeExit` in exactly the shape `remote.Client` needs, write a thin adapter in `internal/app/terminal_wiring.go` — don't modify the existing client.
4. **`d.daemonEmitter()` (Task 13)**: must reuse the existing daemon push mechanism. If you can't find one, **stop and ask** — don't invent.
5. **Frontend Wails import alias**: tasks use `@/wailsjs/...` — confirm the project's tsconfig `paths` map. If imports use a different alias (`~/wailsjs`, relative paths), substitute throughout.
6. **No drive-by edits**: per CLAUDE.md §3, only touch files listed in each task's `Files` section. Skip linter sweeps, import reordering, unrelated cleanups.
7. **Commit hygiene**: each task ends with a commit. Don't squash — granular history aids `git bisect` if a regression appears.

---

## Plan complete and saved to `docs/superpowers/plans/2026-05-28-terminal-integration.md`.

Two execution options:

**1. Subagent-Driven (recommended)** — Dispatch a fresh subagent per task, review between tasks, fast iteration. Best when you want to monitor progress closely.

**2. Inline Execution** — Execute tasks in this session using `superpowers:executing-plans`, batch with checkpoints. Best when context is fresh and you want to power through.

Which approach?
