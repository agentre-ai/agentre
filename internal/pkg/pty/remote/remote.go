// Package remote implements pty.Backend by relaying ops over an agentred
// JSON-RPC-over-WebSocket client.
package remote

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	pkgpty "github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"
)

const openTimeout = 5 * time.Second

// ErrDaemonTimeout is returned by Backend.Open when agentred does not respond
// within openTimeout.
var (
	ErrDaemonTimeout error = daemonTimeoutError{}
	errOpenTimeout         = errors.New("remote terminal open timeout")
)

type daemonTimeoutError struct{}

func (daemonTimeoutError) Error() string   { return "agentred did not respond within 5s" }
func (daemonTimeoutError) Timeout() bool   { return true }
func (daemonTimeoutError) Temporary() bool { return true }

// Client is the minimal subset of the agentred ws client surface needed
// here. In production this is the existing per-device ws client; tests
// stub it.
type Client interface {
	Call(ctx context.Context, method string, params any, out any) error
	SubscribeData(terminalID string) <-chan protocol.TerminalDataEvent
	SubscribeExit(terminalID string) <-chan protocol.TerminalExitEvent
}

type Backend struct {
	client  Client
	release func()
}

func NewBackend(c Client) *Backend { return &Backend{client: c} }

// NewBackendWithLease binds one successful daemon-client borrow to one Open.
// Open failure releases immediately; a successful handle releases when its
// terminal outcome is settled. The release function is guarded exactly once.
func NewBackendWithLease(c Client, release func()) *Backend {
	return &Backend{client: c, release: release}
}

func (b *Backend) Open(ctx context.Context, spec pkgpty.Spec) (pkgpty.Handle, error) {
	release := onceRelease(b.release)
	openCtx, cancel := context.WithTimeoutCause(ctx, openTimeout, errOpenTimeout)
	defer cancel()
	var res protocol.TerminalOpenResult
	if err := b.client.Call(openCtx, "terminal.open", protocol.TerminalOpenParams{
		Cwd: spec.Cwd, Shell: spec.Shell, Command: spec.Command, Env: spec.Env, Cols: spec.Cols, Rows: spec.Rows,
	}, &res); err != nil {
		release()
		if errors.Is(context.Cause(openCtx), errOpenTimeout) {
			return nil, ErrDaemonTimeout
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	h := &handleImpl{
		client:     b.client,
		terminalID: res.TerminalID,
		data:       make(chan []byte, 32),
		exit:       make(chan pkgpty.ExitInfo, 1),
		done:       make(chan struct{}),
		release:    release,
	}
	go h.pump()
	return h, nil
}

func onceRelease(release func()) func() {
	if release == nil {
		return func() {}
	}
	return sync.OnceFunc(release)
}

type handleImpl struct {
	client     Client
	terminalID string

	data chan []byte
	exit chan pkgpty.ExitInfo

	mu        sync.Mutex
	closed    bool
	closeCall *closeCall
	done      chan struct{}
	release   func()
}

type closeCall struct {
	done chan struct{}
	err  error
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
	if call := h.closeCall; call != nil {
		h.mu.Unlock()
		<-call.done
		return call.err
	}
	call := &closeCall{done: make(chan struct{})}
	h.closeCall = call
	h.mu.Unlock()

	var ack struct{}
	err := h.client.Call(context.Background(), "terminal.close", protocol.TerminalCloseParams{
		TerminalID: h.terminalID,
	}, &ack)

	h.mu.Lock()
	settled := h.closed
	if settled {
		// A daemon exit that arrived while the RPC was in flight is already
		// authoritative, so closing is satisfied even if terminal.close raced
		// it and reported an error.
		err = nil
	} else if err == nil {
		h.closed = true
		close(h.done)
		settled = true
	}
	call.err = err
	h.closeCall = nil
	h.mu.Unlock()
	if settled {
		h.release()
	}
	close(call.done)
	return err
}

func (h *handleImpl) Data() <-chan []byte          { return h.data }
func (h *handleImpl) Exit() <-chan pkgpty.ExitInfo { return h.exit }

func (h *handleImpl) pump() {
	dataCh := h.client.SubscribeData(h.terminalID)
	exitCh := h.client.SubscribeExit(h.terminalID)
	outcome := pkgpty.ExitInfo{Reason: "connection_lost"}
	defer func() {
		h.exit <- outcome
		close(h.exit)
		close(h.data)
		h.release()
	}()

	for {
		select {
		case ev, ok := <-dataCh:
			if !ok {
				// ClientAdapter closes the exit subscription before data after
				// delivering a daemon exit. Prefer that authoritative outcome;
				// otherwise the data subscription disappeared with no exit.
				select {
				case ev, exitOK := <-exitCh:
					if exitOK {
						outcome = exitInfo(ev)
					}
				default:
				}
				if !h.claimDaemonExit() {
					outcome = pkgpty.ExitInfo{Reason: "killed"}
				}
				return
			}
			if !h.forwardData(ev, false) {
				outcome = pkgpty.ExitInfo{Reason: "killed"}
				return
			}
		case ev, ok := <-exitCh:
			if ok {
				outcome = exitInfo(ev)
			}
			if !h.claimDaemonExit() {
				outcome = pkgpty.ExitInfo{Reason: "killed"}
				return
			}
			if ok {
				h.drainDataAfterExit(dataCh)
			}
			return
		case <-h.done:
			outcome = pkgpty.ExitInfo{Reason: "killed"}
			return
		}
	}
}

func (h *handleImpl) claimDaemonExit() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.closed = true
	return true
}

func exitInfo(ev protocol.TerminalExitEvent) pkgpty.ExitInfo {
	return pkgpty.ExitInfo{Code: ev.Code, Reason: ev.Reason, Msg: ev.Msg}
}

func (h *handleImpl) forwardData(ev protocol.TerminalDataEvent, prioritizeOutput bool) bool {
	// The daemon base64-encodes each chunk so it survives the JSON hop;
	// decode back to raw bytes. Skip a malformed frame rather than feed
	// the encoded text to xterm.
	decoded, err := base64.StdEncoding.DecodeString(ev.Data)
	if err != nil {
		return true
	}
	if prioritizeOutput {
		select {
		case h.data <- decoded:
			return true
		default:
		}
	}
	select {
	case h.data <- decoded:
		return true
	case <-h.done:
		return false
	}
}

func (h *handleImpl) drainDataAfterExit(dataCh <-chan protocol.TerminalDataEvent) {
	// ClientAdapter closes dataCh immediately after queueing the daemon exit.
	// Range therefore drains the already-buffered final frames in FIFO order.
	for ev := range dataCh {
		if !h.forwardData(ev, true) {
			return
		}
	}
}

var _ pkgpty.Backend = (*Backend)(nil)
var _ pkgpty.Handle = (*handleImpl)(nil)
