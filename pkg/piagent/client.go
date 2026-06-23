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
	// sessionDir 是 Pi session JSONL 的存储目录（--session-dir）。和 cwd（工具
	// 工作目录）分开，避免把 session 文件写进用户项目里。
	sessionDir string
	// session 非空时透传 --session <path>：Pi 会在该路径不存在时新建、存在时
	// resume，从而跨 turn 复用同一会话历史。
	session string
	// extensions 透传给 pi 的 --extension（可多次）。Agentre 用它加载内嵌的
	// MCP 桥扩展，把注入的 HTTP MCP server 翻成 pi 一等工具。
	extensions []string
	killGrace  time.Duration
	runner     processRunner
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
	stream := newStream(proc, c.killGrace)
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
	stream := newStream(proc, c.killGrace)
	if err := stream.send(ctx, map[string]any{"type": "compact"}); err != nil {
		_ = stream.Close(context.Background())
		return nil, err
	}
	go stream.drain(ctx)
	return stream, nil
}

func (c *Client) Close(_ context.Context) error { return nil }

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
		handle: h,
		stdin:  h.Stdin(),
		lines:  bufio.NewScanner(h.Stdout()),
		stderr: &lockedBuffer{},
		done:   make(chan error, 1),
	}
	p.lines.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	go func() { _, _ = io.Copy(p.stderr, h.Stderr()) }()
	go func() { p.done <- h.Wait() }()
	return p, nil
}

type rpcProcess struct {
	handle processHandle
	stdin  io.Writer
	lines  *bufio.Scanner
	stderr *lockedBuffer
	done   chan error
	mu     sync.Mutex
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
	case err := <-p.done:
		return wrapTerminateExitError(err, p.stderr.String())
	case <-timer.C:
		_ = p.handle.Kill()
		return wrapExitError(<-p.done, p.stderr.String())
	case <-ctx.Done():
		_ = p.handle.Kill()
		return ctx.Err()
	}
}

func wrapExitError(err error, stderr string) error {
	if err == nil {
		return nil
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
	case err := <-p.done:
		if err == nil {
			return ErrProcessDead
		}
		return wrapExitError(err, p.stderr.String())
	default:
		return ErrProcessDead
	}
}

func isAcceptedPromptResponse(r rpcResponse) bool {
	return r.Type == "response" && r.Command == "prompt" && r.Success
}

func isTerminalEvent(ev rpcEvent) bool {
	if ev.Type != "agent_end" {
		return false
	}
	msg := lastAssistantFromAgentEnd(ev.Messages)
	if msg == nil {
		return true
	}
	return strings.TrimSpace(msg.StopReason) != "toolUse"
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
