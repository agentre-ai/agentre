// Package remote implements pty.Backend by relaying ops over an agentred
// JSON-RPC-over-WebSocket client.
package remote

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/agentre-ai/agentre/internal/pkg/jsonrpc"
	pkgpty "github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"
)

const (
	openTimeout        = 5 * time.Second
	openCleanupTimeout = time.Second
)

// ErrDaemonTimeout is returned by Backend.Open when agentred does not respond
// within openTimeout.
var (
	ErrDaemonTimeout      error = daemonTimeoutError{}
	ErrTerminalIDMismatch       = errors.New("agentred returned a mismatched terminal id")
	errOpenTimeout              = errors.New("remote terminal open timeout")
)

type daemonTimeoutError struct{}

func (daemonTimeoutError) Error() string   { return "agentred did not respond within 5s" }
func (daemonTimeoutError) Timeout() bool   { return true }
func (daemonTimeoutError) Temporary() bool { return true }

// Subscription is one atomic terminal event generation. Data and Exit always
// come from the same registration and remain the exact references consumed by
// the handle pump.
type Subscription struct {
	Data <-chan protocol.TerminalDataEvent
	Exit <-chan protocol.TerminalExitEvent
}

// Client is the minimal subset of the agentred ws client surface needed here.
// Abort is the safety fallback when pending-open cleanup cannot be acknowledged.
type Client interface {
	Call(ctx context.Context, method string, params any, out any) error
	Subscribe(terminalID string) Subscription
	Unsubscribe(terminalID string, subscription Subscription)
	Abort()
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
	terminalID, err := terminalIDForOpen(spec.TerminalID)
	if err != nil {
		release()
		return nil, err
	}

	// Register both event channels under the stable desktop identity before the
	// request can make agentred spawn or emit anything.
	subscription := b.client.Subscribe(terminalID)
	settleFailure := func() {
		b.client.Unsubscribe(terminalID, subscription)
		release()
	}

	openCtx, cancel := context.WithTimeoutCause(ctx, openTimeout, errOpenTimeout)
	defer cancel()
	var res protocol.TerminalOpenResult
	err = b.client.Call(openCtx, "terminal.open", protocol.TerminalOpenParams{
		TerminalID: terminalID,
		Cwd:        spec.Cwd,
		Shell:      spec.Shell,
		Command:    spec.Command,
		Env:        spec.Env,
		Cols:       spec.Cols,
		Rows:       spec.Rows,
	}, &res)
	if err != nil {
		if returned, interrupted := interruptedOpenError(ctx, openCtx, err); interrupted {
			b.cancelPendingOpen(ctx, terminalID)
			settleFailure()
			return nil, returned
		}
		// A generic terminal.open RPC error is an authoritative rejection: no
		// pending PTY needs an extra cancellation request.
		settleFailure()
		return nil, err
	}
	if res.TerminalID != terminalID {
		b.cleanupMismatchedOpen(ctx, res.TerminalID)
		settleFailure()
		return nil, fmt.Errorf("%w: expected %q, got %q", ErrTerminalIDMismatch, terminalID, res.TerminalID)
	}

	h := &handleImpl{
		client:       b.client,
		terminalID:   terminalID,
		subscription: subscription,
		data:         make(chan []byte, 32),
		exit:         make(chan pkgpty.ExitInfo, 1),
		done:         make(chan struct{}),
		release:      release,
	}
	go h.pump()
	return h, nil
}

func terminalIDForOpen(supplied string) (string, error) {
	if supplied != "" {
		return supplied, nil
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate remote terminal id: %w", err)
	}
	return hex.EncodeToString(id[:]), nil
}

func interruptedOpenError(
	ctx context.Context,
	openCtx context.Context,
	callErr error,
) (error, bool) {
	// A JSON-RPC error frame is an authoritative terminal.open rejection even
	// if caller cancellation raced its arrival. Transport/context failures have
	// no such authority and must preserve the desktop interruption outcome.
	var rpcErr *jsonrpc.Error
	if errors.As(callErr, &rpcErr) {
		return nil, false
	}
	if errors.Is(context.Cause(openCtx), errOpenTimeout) {
		return ErrDaemonTimeout, true
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr, true
	}
	if openCtx.Err() != nil &&
		(errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded)) {
		return callErr, true
	}
	return nil, false
}

func (b *Backend) cancelPendingOpen(ctx context.Context, terminalID string) {
	b.cleanupTerminal(ctx, protocol.TerminalCloseParams{
		TerminalID:        terminalID,
		CancelPendingOpen: true,
	})
}

func (b *Backend) cleanupMismatchedOpen(ctx context.Context, terminalID string) {
	if terminalID == "" {
		b.client.Abort()
		return
	}
	b.cleanupTerminal(ctx, protocol.TerminalCloseParams{TerminalID: terminalID})
}

func (b *Backend) cleanupTerminal(ctx context.Context, params protocol.TerminalCloseParams) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openCleanupTimeout)
	defer cancel()
	var ack struct{}
	if err := b.client.Call(cleanupCtx, "terminal.close", params, &ack); err != nil {
		// A failed acknowledgement leaves connection ownership suspect. Abort it
		// synchronously so agentred's connection-scoped CloseAll owns cleanup.
		b.client.Abort()
	}
}

func onceRelease(release func()) func() {
	if release == nil {
		return func() {}
	}
	return sync.OnceFunc(release)
}

type handleImpl struct {
	client       Client
	terminalID   string
	subscription Subscription

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
	dataCh := h.subscription.Data
	exitCh := h.subscription.Exit
	outcome := pkgpty.ExitInfo{Reason: "connection_lost"}
	defer func() {
		h.exit <- outcome
		close(h.exit)
		close(h.data)
		h.client.Unsubscribe(h.terminalID, h.subscription)
		h.release()
	}()

	for {
		select {
		case ev, ok := <-dataCh:
			if !ok {
				// ClientAdapter queues an authoritative exit before closing data.
				// Prefer that buffered outcome; otherwise the subscription ended
				// because the connection disappeared without terminal.exit.
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
			if !h.forwardData(ev) {
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

func (h *handleImpl) forwardData(ev protocol.TerminalDataEvent) bool {
	// The daemon base64-encodes each chunk so it survives the JSON hop;
	// decode back to raw bytes. Skip a malformed frame rather than feed
	// the encoded text to xterm.
	decoded, err := base64.StdEncoding.DecodeString(ev.Data)
	if err != nil {
		return true
	}
	select {
	case h.data <- decoded:
		return true
	case <-h.done:
		return false
	}
}

func (h *handleImpl) drainDataAfterExit(dataCh <-chan protocol.TerminalDataEvent) {
	// A daemon exit is authoritative only after every earlier accepted frame
	// has either reached the consumer or a confirmed local Close has won.
	for ev := range dataCh {
		if !h.forwardData(ev) {
			return
		}
	}
}

var _ pkgpty.Backend = (*Backend)(nil)
var _ pkgpty.Handle = (*handleImpl)(nil)
