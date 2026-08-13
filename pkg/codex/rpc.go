package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type rpcError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	// Data is intentionally omitted: upstream errors may echo request headers,
	// tool arguments, or configuration values. The stable code and redacted
	// message are sufficient to correlate the failure without leaking secrets.
	return fmt.Sprintf("codex app-server: rpc error %d: %s", e.Code, sanitizeDiagnostic(e.Message))
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcResponse struct {
	Result json.RawMessage
	Err    error
}

type appInboundKind int

const (
	appInboundNotification appInboundKind = iota + 1
	appInboundRequest
)

type appInbound struct {
	Kind   appInboundKind
	ID     json.RawMessage
	Method string
	Params json.RawMessage
}

type appClient struct {
	proc processHandle

	writeMu sync.Mutex
	nextID  atomic.Int64

	pendingMu sync.Mutex
	pending   map[string]chan rpcResponse

	incoming chan appInbound
	queueMu  sync.Mutex
	queue    []appInbound
	queueSig chan struct{}
	queueEnd bool
	done     chan struct{}
	doneMu   sync.Mutex
	doneErr  error
	stop     chan struct{}
	stopOnce sync.Once
	stderr   *lockedBuffer
	// rawSink 若非 nil,readLoop 每读到一行原始 stdout 就同步回调一次。见 Client.rawSink。
	rawSink func([]byte)
}

func newAppClient(ctx context.Context, runner appServerRunner, opts procOptions, rawSink func([]byte)) (*appClient, error) {
	proc, err := runner.Start(ctx, opts)
	if err != nil {
		return nil, err
	}
	c := &appClient{
		proc:     proc,
		pending:  map[string]chan rpcResponse{},
		incoming: make(chan appInbound),
		queueSig: make(chan struct{}, 1),
		done:     make(chan struct{}),
		stop:     make(chan struct{}),
		stderr:   &lockedBuffer{},
		rawSink:  rawSink,
	}
	go func() { _, _ = io.Copy(c.stderr, proc.Stderr()) }()
	go c.dispatchIncoming()
	go c.readLoop()
	return c, nil
}

func (c *appClient) Incoming() <-chan appInbound { return c.incoming }
func (c *appClient) Done() <-chan struct{}       { return c.done }

func (c *appClient) Err() error {
	c.doneMu.Lock()
	defer c.doneMu.Unlock()
	return c.doneErr
}

func (c *appClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	key := strconv.FormatInt(id, 10)
	ch := make(chan rpcResponse, 1)

	c.pendingMu.Lock()
	c.pending[key] = ch
	c.pendingMu.Unlock()

	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if err := c.writeJSON(msg); err != nil {
		c.deletePending(key)
		return nil, err
	}

	select {
	case res := <-ch:
		return res.Result, res.Err
	case <-ctx.Done():
		c.deletePending(key)
		return nil, ctx.Err()
	case <-c.done:
		c.deletePending(key)
		// readLoop 只在把读到的响应全部 route 完之后才 close(done)：所以一旦
		// done 关闭，任何已到达的响应必然已经缓冲在 ch 里。这里必须优先取回
		// 它，否则一次成功的中断/转向会被误报为 ErrNoActiveTurn（进程在
		// 响应到达后、Call 尚未醒来时退出就会触发该竞态，见 evalInterrupt
		// 在高并发下的偶发失败）。
		select {
		case res := <-ch:
			return res.Result, res.Err
		default:
		}
		if err := c.Err(); err != nil {
			return nil, err
		}
		return nil, ErrProcessDead
	}
}

func (c *appClient) Notify(_ context.Context, method string, params any) error {
	msg := map[string]any{"method": method}
	if params != nil {
		msg["params"] = params
	}
	return c.writeJSON(msg)
}

func (c *appClient) Respond(_ context.Context, id json.RawMessage, result any) error {
	return c.writeJSON(map[string]any{"id": json.RawMessage(id), "result": result})
}

func (c *appClient) RespondError(_ context.Context, id json.RawMessage, code int64, message string) error {
	return c.writeJSON(map[string]any{
		"id": json.RawMessage(id),
		"error": map[string]any{
			"code":    code,
			"message": sanitizeDiagnostic(message),
		},
	})
}

func (c *appClient) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = c.proc.Stdin().Write(append(data, '\n'))
	return err
}

func (c *appClient) readLoop() {
	var readErr error
	sc := bufio.NewScanner(c.proc.Stdout())
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
scan:
	for sc.Scan() {
		if c.rawSink != nil {
			c.rawSink(sc.Bytes())
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			readErr = fmt.Errorf("%w: invalid JSON-RPC message: %v", ErrProtocol, err)
			_ = c.proc.Kill()
			break scan
		}
		if !c.routeMessage(msg) {
			break scan
		}
	}
	if err := sc.Err(); err != nil {
		readErr = err
	}
	waitErr := c.proc.Wait()
	if readErr == nil && waitErr != nil {
		readErr = &ExitError{Err: waitErr, Stderr: c.stderr.String()}
	}
	failErr := readErr
	if failErr == nil {
		failErr = ErrProcessDead
	}
	c.setDoneErr(readErr)
	c.failPending(failErr)
	c.closeIncomingQueue()
	close(c.done)
}

func (c *appClient) routeMessage(msg rpcMessage) bool {
	if len(msg.ID) > 0 && msg.Method == "" {
		key := strings.Trim(string(msg.ID), `"`)
		c.pendingMu.Lock()
		ch := c.pending[key]
		delete(c.pending, key)
		c.pendingMu.Unlock()
		if ch == nil {
			return true
		}
		if msg.Error != nil {
			ch <- rpcResponse{Err: msg.Error}
			return true
		}
		ch <- rpcResponse{Result: msg.Result}
		return true
	}
	if msg.Method == "" {
		return true
	}
	in := appInbound{Kind: appInboundNotification, Method: msg.Method, Params: append(json.RawMessage(nil), msg.Params...)}
	if len(msg.ID) > 0 {
		in.Kind = appInboundRequest
		in.ID = append(json.RawMessage(nil), msg.ID...)
	}
	return c.enqueueIncoming(in)
}

// enqueueIncoming keeps stdout reading independent from stream consumption.
// app-server may emit a large notification burst before the response to the
// turn/start request. Blocking the stdout reader on a bounded notification
// channel at that point prevents it from ever reaching and routing the RPC
// response, while the consumer cannot start until that response arrives.
func (c *appClient) enqueueIncoming(in appInbound) bool {
	c.queueMu.Lock()
	if c.queueEnd || c.isStopping() {
		c.queueMu.Unlock()
		return false
	}
	c.queue = append(c.queue, in)
	c.queueMu.Unlock()
	select {
	case c.queueSig <- struct{}{}:
	default:
	}
	return true
}

func (c *appClient) dispatchIncoming() {
	defer close(c.incoming)
	for {
		c.queueMu.Lock()
		if len(c.queue) > 0 {
			in := c.queue[0]
			c.queue[0] = appInbound{}
			c.queue = c.queue[1:]
			c.queueMu.Unlock()
			select {
			case c.incoming <- in:
			case <-c.stop:
				return
			}
			continue
		}
		ended := c.queueEnd
		c.queueMu.Unlock()
		if ended {
			return
		}
		select {
		case <-c.queueSig:
		case <-c.stop:
			return
		}
	}
}

func (c *appClient) closeIncomingQueue() {
	c.queueMu.Lock()
	c.queueEnd = true
	c.queueMu.Unlock()
	select {
	case c.queueSig <- struct{}{}:
	default:
	}
}

func (c *appClient) deletePending(key string) {
	c.pendingMu.Lock()
	delete(c.pending, key)
	c.pendingMu.Unlock()
}

func (c *appClient) failPending(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for key, ch := range c.pending {
		delete(c.pending, key)
		ch <- rpcResponse{Err: err}
	}
}

func (c *appClient) setDoneErr(err error) {
	c.doneMu.Lock()
	c.doneErr = err
	c.doneMu.Unlock()
}

func (c *appClient) requestStop() {
	c.stopOnce.Do(func() { close(c.stop) })
}

func (c *appClient) isStopping() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

func (c *appClient) terminate(ctx context.Context, grace time.Duration) error {
	if c == nil || c.proc == nil {
		return nil
	}
	select {
	case <-c.done:
		return nil
	default:
	}
	c.requestStop()
	if wc, ok := c.proc.Stdin().(interface{ Close() error }); ok {
		_ = wc.Close()
	}
	_ = c.proc.Signal(os.Interrupt)
	if grace <= 0 {
		grace = 100 * time.Millisecond
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		_ = c.proc.Kill()
		return ctx.Err()
	case <-timer.C:
	}
	_ = c.proc.Kill()
	select {
	case <-c.done:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
	}
	return nil
}
