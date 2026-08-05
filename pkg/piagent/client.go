package piagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/cago-frame/agents/provider"
)

const maxRPCFrameBytes = 16 << 20

type Client struct {
	binary       string
	cwd          string
	env          map[string]string
	model        string
	thinking     string
	systemPrompt string
	// noSession 使用 Pi 的临时 Session 模式（--no-session），不写入 JSONL。
	noSession bool
	// sessionDir 是 Pi session JSONL 的存储目录（--session-dir）。和 cwd（工具
	// 工作目录）分开，避免把 session 文件写进用户项目里。
	sessionDir string
	// session 非空时透传 --session <path|id>，恢复指定 Pi 原生会话。
	session string
	// extensions 透传给 pi 的 --extension（可多次）。Agentre 用它加载内嵌的
	// MCP 桥扩展，把注入的 HTTP MCP server 翻成 pi 一等工具。
	extensions []string
	killGrace  time.Duration
	runner     processRunner

	// rawSink 若非 nil,子进程每读到一行可记录的原始 stdout JSON-RPC 帧就同步回调
	// 一次；extension_ui_request 含敏感交互文案，始终排除。debug 级原始帧转储用；
	// 经 startRPC 注入 rpcProcess,由各 stdout 读点调用。
	rawSink func([]byte)
}

func New(opts ...Option) *Client {
	c := &Client{
		binary:    "pi",
		killGrace: 10 * time.Second,
		runner:    execProcessRunner{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) Stream(ctx context.Context, prompt string, opts ...RunOption) (*Stream, error) {
	spec := runSpec{}
	for _, o := range opts {
		o(&spec)
	}
	// Session resume is wired at the Client level (WithSession → --session); the
	// per-turn spec carries multimodal images透传到 prompt 帧。
	proc, err := c.startRPC(ctx)
	if err != nil {
		return nil, err
	}
	state, err := readSessionState(ctx, proc, c.session)
	if err != nil {
		_ = proc.terminate(context.Background(), c.killGrace)
		return nil, err
	}
	stream := newStream(proc, c.killGrace)
	stream.setSessionID(state.SessionID)
	if state.Model != nil {
		stream.setContextWindow(state.Model.ContextWindow)
	}
	// Ask Pi for its authoritative model window before the first prompt. The
	// response is optional and intentionally not awaited: older/degraded RPC
	// implementations must not delay or block the actual turn.
	stream.markInitialSessionStatsPending()
	if err := stream.send(ctx, map[string]any{
		"id": initialSessionStatsRequestID, "type": "get_session_stats",
	}); err != nil {
		_ = stream.Close(context.Background())
		return nil, err
	}
	frame := map[string]any{"type": "prompt", "message": prompt}
	if imgs := imagesToWire(spec.images); len(imgs) > 0 {
		frame["images"] = imgs
	}
	if err := stream.send(ctx, frame); err != nil {
		_ = stream.Close(context.Background())
		return nil, err
	}
	go stream.drain(ctx)
	return stream, nil
}

func (c *Client) Text(ctx context.Context, prompt string, opts ...RunOption) (string, error) {
	stream, err := c.Stream(ctx, prompt, opts...)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	var stopErr error
	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case EventTextDelta:
			b.WriteString(ev.Text)
		case EventError:
			if ev.Err != nil {
				stopErr = ev.Err
			}
		}
	}
	if err := stream.Close(ctx); err != nil && stopErr == nil {
		stopErr = err
	}
	if stopErr != nil {
		return "", stopErr
	}
	return b.String(), nil
}

func (c *Client) Compact(ctx context.Context, _ string) (*Stream, error) {
	proc, err := c.startRPC(ctx)
	if err != nil {
		return nil, err
	}
	state, err := readSessionState(ctx, proc, c.session)
	if err != nil {
		_ = proc.terminate(context.Background(), c.killGrace)
		return nil, err
	}
	stream := newStream(proc, c.killGrace)
	stream.setSessionID(state.SessionID)
	if err := stream.send(ctx, map[string]any{"type": "compact"}); err != nil {
		_ = stream.Close(context.Background())
		return nil, err
	}
	go stream.drain(ctx)
	return stream, nil
}

func (c *Client) Close(_ context.Context) error { return nil }

func readSessionState(ctx context.Context, proc *rpcProcess, expected string) (sessionStateWire, error) {
	const requestID = "session-state"
	if err := proc.writeJSON(map[string]any{"id": requestID, "type": "get_state"}); err != nil {
		return sessionStateWire{}, err
	}
	for proc.lines.Scan() {
		proc.captureRawFrame(proc.lines.Bytes())
		select {
		case <-ctx.Done():
			return sessionStateWire{}, ctx.Err()
		default:
		}
		var response rpcResponse
		if err := json.Unmarshal(proc.lines.Bytes(), &response); err != nil {
			continue
		}
		if response.Type != "response" || response.Command != "get_state" || response.ID != requestID {
			continue
		}
		if !response.Success {
			return sessionStateWire{}, failureResponseError(response)
		}
		var state sessionStateWire
		if err := json.Unmarshal(response.Data, &state); err != nil {
			return sessionStateWire{}, fmt.Errorf("piagent decode get_state data: %w", err)
		}
		state.SessionID = strings.TrimSpace(state.SessionID)
		if state.SessionID == "" {
			return sessionStateWire{}, errors.New("piagent: get_state returned empty session id")
		}
		expected = strings.TrimSpace(expected)
		if expected != "" && !looksLikeSessionPath(expected) && state.SessionID != expected {
			return sessionStateWire{}, fmt.Errorf("piagent: get_state returned unexpected session id %q, want %q", state.SessionID, expected)
		}
		return state, nil
	}
	return sessionStateWire{}, awaitProcessExitOrScanError(ctx, proc)
}

func looksLikeSessionPath(value string) bool {
	return strings.ContainsAny(value, `/\\`) || strings.HasSuffix(value, ".jsonl")
}

func (c *Client) startRPC(ctx context.Context) (*rpcProcess, error) {
	h, err := c.runner.Start(ctx, procOptions{
		Binary: c.binary,
		Args:   buildRPCArgs(c),
		Cwd:    c.cwd,
		Env:    buildEnv(c.env),
	})
	if err != nil {
		return nil, err
	}
	p := &rpcProcess{
		handle:     h,
		stdin:      h.Stdin(),
		lines:      bufio.NewScanner(h.Stdout()),
		rawSink:    c.rawSink,
		stderr:     &lockedBuffer{},
		stderrDone: make(chan struct{}),
		done:       make(chan struct{}),
	}
	p.lines.Buffer(make([]byte, 0, 64*1024), maxRPCFrameBytes)
	go func() {
		defer close(p.stderrDone)
		_, _ = io.Copy(p.stderr, h.Stderr())
	}()
	go p.awaitExit()
	return p, nil
}

type rpcProcess struct {
	handle     processHandle
	stdin      io.Writer
	lines      *bufio.Scanner
	rawSink    func([]byte) // 非 nil 时同步回调可安全记录的原始 stdout 帧
	stderr     *lockedBuffer
	stderrDone chan struct{}
	done       chan struct{} // closed when waitErr is available to every observer
	waitErr    error         // immutable after done is closed
	mu         sync.Mutex
}

func (p *rpcProcess) awaitExit() {
	p.waitErr = p.handle.Wait()
	<-p.stderrDone
	close(p.done)
}

func (p *rpcProcess) waitResult() error {
	<-p.done
	return p.waitErr
}

func (p *rpcProcess) captureRawFrame(frame []byte) {
	if p.rawSink == nil || isExtensionUIRequestFrame(frame) {
		return
	}
	p.rawSink(frame)
}

func isExtensionUIRequestFrame(frame []byte) bool {
	var probe struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(frame, &probe) == nil && probe.Type == "extension_ui_request"
}

func (p *rpcProcess) writeJSON(v any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	_, err = p.stdin.Write(buf)
	return err
}

func (p *rpcProcess) terminate(ctx context.Context, grace time.Duration) error {
	if p == nil || p.handle == nil {
		return nil
	}
	_ = p.handle.Signal(interruptSignal())
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-p.done:
		return wrapTerminateExitError(p.waitErr, p.stderr.String())
	case <-timer.C:
		_ = p.handle.Kill()
		return wrapExitError(p.waitResult(), p.stderr.String())
	case <-ctx.Done():
		_ = p.handle.Kill()
		return ctx.Err()
	}
}

func wrapExitError(err error, stderr string) error {
	if err == nil {
		return nil
	}
	trimmed := strings.TrimSpace(stderr)
	if strings.Contains(strings.ToLower(trimmed), "no session found matching") {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, trimmed)
	}
	return &ExitError{Err: err, Stderr: stderr}
}

func wrapTerminateExitError(err error, stderr string) error {
	if err == nil || isInterruptExit(err) {
		return nil
	}
	return wrapExitError(err, stderr)
}

func isInterruptExit(err error) bool {
	if err == nil {
		return false
	}
	return strings.TrimSpace(err.Error()) == "signal: interrupt"
}

func failureResponseError(r rpcResponse) error {
	msg := strings.TrimSpace(r.Error)
	if msg == "" {
		msg = string(r.Data)
	}
	if msg == "" {
		msg = "unknown rpc failure"
	}
	return fmt.Errorf("piagent rpc %s failed: %s", r.Command, msg)
}

func processDeadOrScanError(p *rpcProcess) error {
	if err := p.lines.Err(); err != nil {
		return err
	}
	select {
	case <-p.done:
		if p.waitErr == nil {
			return ErrProcessDead
		}
		return wrapExitError(p.waitErr, p.stderr.String())
	case <-time.After(100 * time.Millisecond):
		return ErrProcessDead
	}
}

func awaitProcessExitOrScanError(ctx context.Context, p *rpcProcess) error {
	if err := p.lines.Err(); err != nil {
		return err
	}
	// During the startup handshake, stdout EOF can arrive before Wait and stderr
	// collection finish. Their result is authoritative for classifying a missing
	// resumed session, so wait unless the startup context is canceled.
	select {
	case <-p.done:
		if p.waitErr == nil {
			return ErrProcessDead
		}
		return wrapExitError(p.waitErr, p.stderr.String())
	case <-ctx.Done():
		return ctx.Err()
	}
}

func isAcceptedPromptResponse(r rpcResponse) bool {
	return r.Type == "response" && r.Command == "prompt" && r.Success
}

func isTerminalEvent(ev rpcEvent) bool {
	return ev.Type == "agent_settled"
}

func parseAssistantMessage(raw json.RawMessage) (*assistantMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var msg assistantMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	if msg.Role != "assistant" {
		return nil, nil
	}
	return &msg, nil
}

func usageFromMessage(msg *assistantMessage) provider.Usage {
	if msg == nil || msg.Usage == nil {
		return provider.Usage{}
	}
	return provider.Usage{
		PromptTokens:        msg.Usage.Input,
		CompletionTokens:    msg.Usage.Output,
		CachedTokens:        msg.Usage.CacheRead,
		CacheCreationTokens: msg.Usage.CacheWrite,
	}
}

func lastAssistantFromAgentEnd(raw json.RawMessage) *assistantMessage {
	var msgs []json.RawMessage
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, err := parseAssistantMessage(msgs[i])
		if err == nil && msg != nil {
			return msg
		}
	}
	return nil
}

// userEchoText 从 message_start/message_end 的 message 里取出 user 角色的文本。
// 非 user 角色返回 ok=false。content 可能是字符串或 content block 数组，统一交给
// contentText 抽取。
func userEchoText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var m struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", false
	}
	if m.Role != "user" {
		return "", false
	}
	return contentText(m.Content), true
}

func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}

var errStreamClosed = errors.New("piagent: stream closed")
