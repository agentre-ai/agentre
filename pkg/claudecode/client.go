package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Client Claude Code CLI 的高层封装。零值不可用——必须通过 New 构造。
type Client struct {
	binary               string
	cwd                  string
	env                  map[string]string
	model                string
	systemPrompt         string
	permissionMode       string
	sessionID            string
	settings             string
	effort               string
	permissionPromptTool string

	// spawner 仅给单测注入用——nil 时走真实 startProcess。包内私有，不暴露 Exported
	// Option；测试通过 pipeSpawner 注入返回 pipe-backed *process 的 fake spawner，
	// 在 in-memory 里跑完整 stream-json 协议（避开真子进程的 spawn 开销 + 不依赖 sh）。
	spawner func(ctx context.Context, spec processSpec) (*process, error)
}

// spawn 是 Stream/OpenSession 内部调 startProcess 的统一入口；优先走注入的
// spawner（测试缝），fallback 到生产实现。
func (c *Client) spawn(ctx context.Context, spec processSpec) (*process, error) {
	if c.spawner != nil {
		return c.spawner(ctx, spec)
	}
	return startProcess(ctx, spec)
}

func New(opts ...Option) *Client {
	c := &Client{binary: "claude"}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Stream 起一次 turn。调用方：
//
//	for stream.Next() { ev := stream.Event(); ... }
//	if err := stream.Close(ctx); err != nil { ... }
func (c *Client) Stream(ctx context.Context, prompt string, opts ...RunOption) (*Stream, error) {
	spec := runSpec{
		model:                c.model,
		systemPrompt:         c.systemPrompt,
		permissionMode:       c.permissionMode,
		sessionID:            c.sessionID,
		settings:             c.settings,
		effort:               c.effort,
		permissionPromptTool: c.permissionPromptTool,
	}
	for _, o := range opts {
		o(&spec)
	}
	if spec.resumeSessionAtUUID != "" && !spec.forkSession {
		return nil, errors.New("claudecode: ResumeSessionAt requires ForkSession (would destructively rewind source session)")
	}

	args := buildArgs(spec)
	p, err := c.spawn(ctx, processSpec{
		binary: c.binary,
		args:   args,
		cwd:    c.cwd,
		env:    c.env,
	})
	if err != nil {
		return nil, err
	}

	// stdin 喂一个 user frame：与 cago 协议保持一致。
	frame := map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": []map[string]any{{"type": "text", "text": prompt}}},
	}
	enc, err := json.Marshal(frame)
	if err != nil {
		_ = p.stdin.Close()
		_, _ = p.wait(ctx)
		return nil, err
	}
	if _, err := fmt.Fprintf(p.stdin, "%s\n", enc); err != nil {
		_ = p.stdin.Close()
		_, _ = p.wait(ctx)
		return nil, err
	}
	// stdin 保持开：为常驻模式 / 多轮复用同一子进程留出空间。result 帧本身不依赖
	// stdin EOF 触发——CLI 收到一条 user frame 处理完就 emit result，下一轮等下一条
	// frame。Close 时统一关闭 stdin。
	return &Stream{proc: p, dec: newFrameDecoder(p.stdout)}, nil
}

// Text 一次性 prompt → assistant 完整文本：起 Stream、串接所有 EventTextDelta、
// 返回最终字符串。适合 backend probe / 简单问答场景，调用方不关心中间事件。
//
// 错误处理：流式过程中如果遇到 EventError，返回该错误并丢弃已累计文本。
// 子进程错误（exit ≠ 0）通过 Close 透传 *ProcessExitError，让 prober errors.As 拿到 stderr。
func (c *Client) Text(ctx context.Context, prompt string, opts ...RunOption) (string, error) {
	stream, err := c.Stream(ctx, prompt, opts...)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case EventTextDelta:
			b.WriteString(ev.Text)
		case EventError:
			_ = stream.Close(ctx)
			return "", ev.Err
		}
	}
	if err := stream.Close(ctx); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Close 客户端资源回收。pkg/claudecode 的 Client 自身无长生命周期资源（每次 Stream
// 都重新 spawn 子进程），保留方法是为了与 cago 的 Runner.Close(ctx) 调用约定对齐，
// 便于 prober.go 这类切换路径无感迁移。
func (c *Client) Close(_ context.Context) error { return nil }

// Model 返回 Client 当前持有的 model id（即下一次 Stream/OpenSession 会下发的
// --model argv 值）。WithModel 没设置时返空串。供调用方装配后做 invariant 校验，
// 也便于单测断言 WithModel 实际进了 Client。
func (c *Client) Model() string { return c.model }

// Stream 一次 turn 的事件流 + 子进程句柄。
type Stream struct {
	proc *process
	dec  *frameDecoder
	err  error
}

func (s *Stream) Next() bool {
	if !s.dec.Next() {
		s.err = s.dec.Err()
		return false
	}
	return true
}

func (s *Stream) Event() Event { return s.dec.Event() }

func (s *Stream) SessionID() string { return s.dec.SessionID() }

// Close 关闭 stdin/stdout pipe 并 wait 子进程，回收 stderr 错误诊断。
func (s *Stream) Close(ctx context.Context) error {
	if s.proc != nil {
		if s.proc.stdin != nil {
			_ = s.proc.stdin.Close()
		}
		_ = s.proc.stdout.Close()
		_, werr := s.proc.wait(ctx)
		if s.err == nil {
			s.err = werr
		}
	}
	return s.err
}
