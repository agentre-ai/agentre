package piagent

import (
	"bufio"
	"bytes"
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

const (
	rpcFrameSafetyLimit = 64 * 1024 * 1024
	// Startup may include a 64 MiB get_entries response, so use the existing
	// 30-second RPC/probe boundary rather than the optional 2-second stats window.
	rpcStartupTimeout = 30 * time.Second
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
	extensions     []string
	killGrace      time.Duration
	startupTimeout time.Duration
	runner         processRunner

	// rawSink 若非 nil,子进程 stdout JSON-RPC 帧会先投影成不含 prompt、图片、
	// Session 内容或凭证的诊断摘要，再同步回调一次。debug 协议诊断用。
	rawSink func([]byte)
}

func New(opts ...Option) *Client {
	c := &Client{
		binary:         "pi",
		killGrace:      10 * time.Second,
		startupTimeout: rpcStartupTimeout,
		runner:         execProcessRunner{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type PreparedStream struct {
	stream *Stream
	frame  map[string]any

	mu      sync.Mutex
	started bool
}

func (c *Client) Stream(ctx context.Context, prompt string, opts ...RunOption) (*Stream, error) {
	prepared, err := c.PrepareStream(ctx, prompt, opts...)
	if err != nil {
		return nil, err
	}
	return prepared.Start(ctx)
}

// PrepareStream starts/restores the Pi RPC process, completes any requested
// native fork, and captures the pre-prompt leaf without sending the prompt.
// Start must be called only after the caller has durably recorded the turn.
func (c *Client) PrepareStream(ctx context.Context, prompt string, opts ...RunOption) (*PreparedStream, error) {
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
	startupCtx, cancelStartup := c.startupContext(ctx)
	defer cancelStartup()
	sessionID, err := readSessionID(startupCtx, proc, c.session)
	if err != nil {
		_ = proc.terminate(context.Background(), c.killGrace)
		return nil, err
	}
	if forkAnchor := strings.TrimSpace(spec.forkAnchor); forkAnchor != "" {
		if err := forkSession(startupCtx, proc, forkAnchor); err != nil {
			_ = proc.terminate(context.Background(), c.killGrace)
			return nil, err
		}
		forkedSessionID, err := readSessionID(startupCtx, proc, "")
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
		entries, err := readSessionEntries(startupCtx, proc, "session-entries-before")
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
	return &PreparedStream{stream: stream, frame: frame}, nil
}

func (p *PreparedStream) Start(ctx context.Context) (*Stream, error) {
	if p == nil || p.stream == nil {
		return nil, errStreamClosed
	}
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return nil, errors.New("piagent: prepared stream already started")
	}
	p.started = true
	p.mu.Unlock()
	if err := p.stream.send(ctx, p.frame); err != nil {
		_ = p.stream.Close(context.Background())
		return nil, err
	}
	go p.stream.drain(ctx)
	return p.stream, nil
}

func (p *PreparedStream) SessionID() string {
	if p == nil || p.stream == nil {
		return ""
	}
	return p.stream.SessionID()
}

func (p *PreparedStream) Close(ctx context.Context) error {
	if p == nil || p.stream == nil {
		return nil
	}
	return p.stream.Close(ctx)
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
	startupCtx, cancelStartup := c.startupContext(ctx)
	defer cancelStartup()
	sessionID, err := readSessionID(startupCtx, proc, c.session)
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
	for scanRPCLine(ctx, proc.lines) {
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

func scanRPCLine(ctx context.Context, scanner rpcLineScanner) bool {
	if scanner == nil {
		return false
	}
	if contextual, ok := scanner.(interface {
		ScanContext(context.Context) bool
	}); ok {
		return contextual.ScanContext(ctx)
	}
	return scanner.Scan()
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
	if diagnostic := sanitizeDiagnosticFrame(line); len(diagnostic) > 0 {
		proc.rawSink(diagnostic)
	}
}

func sanitizeDiagnosticFrame(line []byte) []byte {
	// get_entries can legitimately carry tens of MiB of base64 image/session data.
	// Recognize and replace it before JSON decoding so diagnostics do not duplicate
	// that payload in memory merely to report the command name.
	if bytes.Contains(line, []byte(`"type":"response"`)) &&
		bytes.Contains(line, []byte(`"command":"get_entries"`)) {
		return []byte(`{"command":"get_entries","payload":"redacted","type":"response"}`)
	}

	var envelope struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
		Data    struct {
			SessionID    string            `json:"sessionId"`
			Canceled     *bool             `json:"cancelled"` //nolint:misspell // Pi RPC field uses British spelling.
			ContextUsage *contextUsageWire `json:"contextUsage"`
		} `json:"data"`
		Method  string `json:"method"`
		Message struct {
			Role string `json:"role"`
		} `json:"message"`
		Messages []struct {
			Role       string `json:"role"`
			StopReason string `json:"stopReason"`
		} `json:"messages"`
		AssistantMessageEvent struct {
			Type string `json:"type"`
		} `json:"assistantMessageEvent"`
		ToolCallID string `json:"toolCallId"`
		ToolName   string `json:"toolName"`
		IsError    bool   `json:"isError"`
		WillRetry  bool   `json:"willRetry"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil || strings.TrimSpace(envelope.Type) == "" {
		return nil
	}

	out := map[string]any{"type": envelope.Type}
	if envelope.ID != "" {
		out["id"] = envelope.ID
	}
	switch envelope.Type {
	case "response":
		out["command"] = envelope.Command
		out["success"] = envelope.Success
		switch envelope.Command {
		case "get_state":
			if sessionID := strings.TrimSpace(envelope.Data.SessionID); sessionID != "" {
				out["sessionId"] = sessionID
			}
		case "fork":
			if envelope.Data.Canceled != nil {
				out["cancelled"] = *envelope.Data.Canceled //nolint:misspell // Pi RPC field uses British spelling.
			}
		case "get_session_stats":
			if envelope.Data.ContextUsage != nil && envelope.Data.ContextUsage.ContextWindow > 0 {
				out["contextWindow"] = envelope.Data.ContextUsage.ContextWindow
			}
		}
	case "extension_ui_request":
		if envelope.Method != "" {
			out["method"] = envelope.Method
		}
	case "message_start", "message_end":
		if envelope.Message.Role != "" {
			out["role"] = envelope.Message.Role
		}
	case "message_update":
		if envelope.AssistantMessageEvent.Type != "" {
			out["assistantEventType"] = envelope.AssistantMessageEvent.Type
		}
	case "tool_execution_start", "tool_execution_end":
		if envelope.ToolCallID != "" {
			out["toolCallId"] = envelope.ToolCallID
		}
		if envelope.ToolName != "" {
			out["toolName"] = envelope.ToolName
		}
		if envelope.Type == "tool_execution_end" {
			out["isError"] = envelope.IsError
		}
	case "agent_end":
		out["willRetry"] = envelope.WillRetry
		for i := len(envelope.Messages) - 1; i >= 0; i-- {
			message := envelope.Messages[i]
			if message.Role == "assistant" && message.StopReason != "" {
				out["stopReason"] = message.StopReason
				break
			}
		}
	}
	diagnostic, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return diagnostic
}

func looksLikeSessionPath(value string) bool {
	return strings.ContainsAny(value, `/\\`) || strings.HasSuffix(value, ".jsonl")
}

func (c *Client) startupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.startupTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.startupTimeout)
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
	lines := newAsyncRPCLineScanner(h.Stdout())
	p := &rpcProcess{
		handle:     h,
		stdin:      h.Stdin(),
		lines:      lines,
		linesDone:  lines.Done(),
		rawSink:    c.rawSink,
		stderr:     &lockedBuffer{},
		stderrDone: make(chan struct{}),
		done:       make(chan struct{}),
	}
	go func() {
		defer close(p.stderrDone)
		_, _ = io.Copy(p.stderr, h.Stderr())
	}()
	go p.awaitExit()
	return p, nil
}

type rpcLineScanner interface {
	Scan() bool
	Bytes() []byte
	Text() string
	Err() error
}

type asyncRPCLineScanner struct {
	lines       chan []byte
	stop        chan struct{}
	done        chan struct{}
	closeReader func()

	stopOnce sync.Once
	current  []byte
	ctxErr   error
	scanErr  error
}

func newAsyncRPCLineScanner(reader io.Reader) *asyncRPCLineScanner {
	s := &asyncRPCLineScanner{
		lines: make(chan []byte),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	switch closer := reader.(type) {
	case io.Closer:
		s.closeReader = func() { _ = closer.Close() }
	case interface{ Close() }:
		s.closeReader = closer.Close
	}
	go s.scan(reader)
	return s
}

func (s *asyncRPCLineScanner) scan(reader io.Reader) {
	defer close(s.done)
	defer close(s.lines)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), rpcFrameSafetyLimit)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		select {
		case s.lines <- line:
		case <-s.stop:
			return
		}
	}
	s.scanErr = scanner.Err()
}

func (s *asyncRPCLineScanner) Scan() bool {
	return s.ScanContext(context.Background())
}

func (s *asyncRPCLineScanner) ScanContext(ctx context.Context) bool {
	select {
	case line, ok := <-s.lines:
		if !ok {
			s.current = nil
			return false
		}
		s.current = line
		return true
	case <-ctx.Done():
		s.ctxErr = ctx.Err()
		s.Stop()
		return false
	}
}

func (s *asyncRPCLineScanner) Bytes() []byte { return s.current }
func (s *asyncRPCLineScanner) Text() string  { return string(s.current) }
func (s *asyncRPCLineScanner) Err() error {
	if s.ctxErr != nil {
		return s.ctxErr
	}
	return s.scanErr
}
func (s *asyncRPCLineScanner) Stop() {
	s.stopOnce.Do(func() {
		close(s.stop)
		if s.closeReader != nil {
			s.closeReader()
		}
	})
}
func (s *asyncRPCLineScanner) Done() <-chan struct{} { return s.done }

type rpcProcess struct {
	handle     processHandle
	stdin      io.Writer
	lines      rpcLineScanner
	linesDone  <-chan struct{}
	rawSink    func([]byte) // 非 nil时回调不含敏感 payload 的 stdout 诊断摘要
	stderr     *lockedBuffer
	stderrDone chan struct{}
	done       chan struct{} // closed when waitErr is available to every observer
	waitErr    error         // immutable after done is closed
	mu         sync.Mutex
}

func (p *rpcProcess) awaitExit() {
	p.waitErr = p.handle.Wait()
	<-p.stderrDone
	if p.linesDone != nil {
		<-p.linesDone
	}
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
	if stopper, ok := p.lines.(interface{ Stop() }); ok {
		stopper.Stop()
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
