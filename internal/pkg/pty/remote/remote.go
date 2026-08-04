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
var ErrDaemonTimeout = errors.New("agentred did not respond within 5s")

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
	openCtx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()
	var res protocol.TerminalOpenResult
	if err := b.client.Call(openCtx, "terminal.open", protocol.TerminalOpenParams{
		Cwd: spec.Cwd, Shell: spec.Shell, Command: spec.Command, Env: spec.Env, Cols: spec.Cols, Rows: spec.Rows,
	}, &res); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrDaemonTimeout
		}
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
	outcome := pkgpty.ExitInfo{Reason: "connection_lost"}
	defer func() {
		h.exit <- outcome
		close(h.exit)
		close(h.data)
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
						outcome = pkgpty.ExitInfo{Code: ev.Code, Reason: ev.Reason, Msg: ev.Msg}
					}
				default:
				}
				return
			}
			// The daemon base64-encodes each chunk so it survives the JSON hop;
			// decode back to raw bytes. Skip a malformed frame rather than feed
			// the encoded text to xterm.
			decoded, err := base64.StdEncoding.DecodeString(ev.Data)
			if err != nil {
				continue
			}
			select {
			case h.data <- decoded:
			case <-h.done:
				outcome = pkgpty.ExitInfo{Reason: "killed"}
				return
			}
		case ev, ok := <-exitCh:
			if ok {
				outcome = pkgpty.ExitInfo{Code: ev.Code, Reason: ev.Reason, Msg: ev.Msg}
			}
			return
		case <-h.done:
			outcome = pkgpty.ExitInfo{Reason: "killed"}
			return
		}
	}
}

var _ pkgpty.Backend = (*Backend)(nil)
var _ pkgpty.Handle = (*handleImpl)(nil)
