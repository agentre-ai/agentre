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

	// rawSink 若非 nil,子进程普通 stdout JSON-RPC 帧会同步回调一次；含完整
	// Session 内容的 get_entries response 在边界处过滤。debug 原始帧转储用。
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
	sessionID, err := readSessionID(ctx, proc, c.session)
	if err != nil {
		_ = proc.terminate(context.Background(), c.killGrace)
		return nil, err
	}
	if forkAnchor := strings.TrimSpace(spec.forkAnchor); forkAnchor != "" {
		if err := forkSession(ctx, proc, forkAnchor); err != nil {
			_ = proc.terminate(context.Background(), c.killGrace)
			return nil, err
		}
		forkedSessionID, err := readSessionID(ctx, proc, "")
		if err != nil {
			_ = proc.terminate(context.Background(), c.killGrace)
			return nil, err
		}
		if forkedSessionID == sessionID {
			_ = proc.terminate(context.Background(), c.killGrace)
			return nil, fmt.Errorf("piagent: fork did not change session id %q", sessionID)
		}
		sessionID = forkedSessionID
	}
	stream := newStream(proc, c.killGrace)
	stream.setSessionID(sessionID)
	if spec.captureUserAnchor {
		entries, err := readSessionEntries(ctx, proc, "session-entries-before")
		if err != nil {
			_ = stream.Close(context.Background())
			return nil, err
		}
		if leafID, ok := validSessionEntriesLeaf(entries); ok {
			stream.setUserAnchorBoundary(leafID)
		}
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
	sessionID, err := readSessionID(ctx, proc, c.session)
	if err != nil {
		_ = proc.terminate(context.Background(), c.killGrace)
		return nil, err
	}
	stream := newStream(proc, c.killGrace)
	stream.setSessionID(sessionID)
	if err := stream.send(ctx, map[string]any{"type": "compact"}); err != nil {
		_ = stream.Close(context.Background())
		return nil, err
	}
	go stream.drain(ctx)
	return stream, nil
}

func (c *Client) Close(_ context.Context) error { return nil }

func readSessionID(ctx context.Context, proc *rpcProcess, expected string) (string, error) {
	const requestID = "session-state"
	response, err := callRPC(ctx, proc, map[string]any{"id": requestID, "type": "get_state"}, "get_state", requestID)
	if err != nil {
		return "", err
	}
	var state sessionStateWire
	if err := json.Unmarshal(response.Data, &state); err != nil {
		return "", fmt.Errorf("piagent decode get_state data: %w", err)
	}
	sessionID := strings.TrimSpace(state.SessionID)
	if sessionID == "" {
		return "", errors.New("piagent: get_state returned empty session id")
	}
	expected = strings.TrimSpace(expected)
	if expected != "" && !looksLikeSessionPath(expected) && sessionID != expected {
		return "", fmt.Errorf("piagent: get_state returned unexpected session id %q, want %q", sessionID, expected)
	}
	return sessionID, nil
}

func forkSession(ctx context.Context, proc *rpcProcess, entryID string) error {
	const requestID = "session-fork"
	response, err := callRPC(
		ctx,
		proc,
		map[string]any{"id": requestID, "type": "fork", "entryId": entryID},
		"fork",
		requestID,
	)
	if err != nil {
		return err
	}
	var result forkResultWire
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return fmt.Errorf("piagent decode fork data: %w", err)
	}
	if result.Canceled == nil {
		return errors.New("piagent: fork response omitted cancellation state")
	}
	if *result.Canceled {
		return errors.New("piagent: fork was canceled")
	}
	return nil
}

func readSessionEntries(ctx context.Context, proc *rpcProcess, requestID string) (sessionEntriesWire, error) {
	response, err := callRPC(
		ctx,
		proc,
		map[string]any{"id": requestID, "type": "get_entries"},
		"get_entries",
		requestID,
	)
	if err != nil {
		return sessionEntriesWire{}, err
	}
	var entries sessionEntriesWire
	if err := json.Unmarshal(response.Data, &entries); err != nil {
		return sessionEntriesWire{}, fmt.Errorf("piagent decode get_entries data: %w", err)
	}
	return entries, nil
}

func callRPC(
	ctx context.Context,
	proc *rpcProcess,
	request map[string]any,
	command string,
	requestID string,
) (rpcResponse, error) {
	if err := proc.writeJSON(request); err != nil {
		return rpcResponse{}, err
	}
	for proc.lines.Scan() {
		line := proc.lines.Bytes()
		select {
		case <-ctx.Done():
			return rpcResponse{}, ctx.Err()
		default:
		}
		var envelope struct {
			Type   string `json:"type"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			emitRawFrame(proc, line)
			continue
		}
		if command == "fork" && envelope.Type == "extension_ui_request" && extensionUIRequiresResponse(envelope.Method) {
			// A session_before_fork dialog may contain selected prompt text. Return
			// only the method name and keep the full request out of diagnostics.
			return rpcResponse{}, fmt.Errorf(
				"piagent rpc fork requires unsupported extension UI response for method %q",
				envelope.Method,
			)
		}
		emitRawFrame(proc, line)
		var response rpcResponse
		if err := json.Unmarshal(line, &response); err != nil {
			continue
		}
		if response.Type != "response" || response.Command != command || response.ID != requestID {
			continue
		}
		if !response.Success {
			return rpcResponse{}, failureResponseError(response)
		}
		return response, nil
	}
	return rpcResponse{}, awaitProcessExitOrScanError(ctx, proc)
}

func validSessionEntriesLeaf(entries sessionEntriesWire) (string, bool) {
	leafID := strings.TrimSpace(entries.LeafID)
	if len(entries.Entries) == 0 {
		return leafID, leafID == ""
	}
	if leafID == "" {
		return "", false
	}
	seen := make(map[string]struct{}, len(entries.Entries))
	for _, entry := range entries.Entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			return "", false
		}
		seen[id] = struct{}{}
	}
	_, ok := seen[leafID]
	return leafID, ok
}

func extensionUIRequiresResponse(method string) bool {
	switch strings.TrimSpace(method) {
	case "select", "confirm", "input", "editor":
		return true
	default:
		return false
	}
}

func emitRawFrame(proc *rpcProcess, line []byte) {
	if proc == nil || proc.rawSink == nil {
		return
	}
	var envelope struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	if json.Unmarshal(line, &envelope) == nil &&
		envelope.Type == "response" && envelope.Command == "get_entries" {
		return
	}
	proc.rawSink(line)
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
	p.lines.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
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
	rawSink    func([]byte) // 非 nil 时回调已过滤 get_entries 的原始 stdout 帧
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
