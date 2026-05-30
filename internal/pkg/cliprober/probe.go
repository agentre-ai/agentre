package cliprober

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"agentre/pkg/claudecode"
	"agentre/pkg/codex"
	"agentre/pkg/piagent"
)

// ProbeRequest 调用方装配好的 CLI 子进程参数。Env 必须完整 —— 包括
// gateway URL / token / 用户自定义 env，cliprober 内部不再做任何 LLM
// provider 查询或 gateway 调用。
//
// CodexConfigs 对应 codex CLI 的 -c key=value 覆盖项；
// 由上层（主进程 service 或 daemon handler）从 agentruntime.BuildCodexConfig
// 取到后塞进来，cliprober 自身不依赖 agentruntime / GORM / entity。
type ProbeRequest struct {
	Type         string            // "claudecode" | "codex"
	CLIPath      string            // 空 → 用 type 默认 binary 名（claude / codex）
	Sandbox      string            // codex
	Approval     string            // codex
	Model        string            // codex
	Env          map[string]string // 已装好的最终 env
	CodexConfigs []string          // codex -c 覆盖项，仅 codex 使用
}

// ProbeResponse Probe 成功路径的返回。
type ProbeResponse struct {
	Text string // assistant 末轮文本，通常是 "pong"
}

// Probe 按 req.Type 派发到对应 prober 实现。
func Probe(ctx context.Context, req ProbeRequest) (*ProbeResponse, error) {
	switch strings.TrimSpace(req.Type) {
	case "claudecode":
		return probeClaudeCode(ctx, req)
	case "codex":
		return probeCodex(ctx, req)
	case "piagent":
		return probePiAgent(ctx, req)
	default:
		return nil, ErrInvalidType
	}
}

// 子进程错误整形辅助 —— 从 service/agent_backend_svc/prober.go 完整迁过来。
// 上游 claudecode/codex 已经截到 ≤4KB；这里再裁到 ~280 字，避免 flash banner 撑得太长。
const cliStderrSnippetLimit = 280

// proberErr 包一层让 CLI 子进程错误对外呈现更友好的 Error()，
// 同时保留 Unwrap 以便上层继续走 errors.Is(err, context.DeadlineExceeded/Canceled)。
type proberErr struct {
	msg string
	err error
}

func (e *proberErr) Error() string { return e.msg }
func (e *proberErr) Unwrap() error { return e.err }

// wrapCLIProberError 仅在识别到 exit / stderr 类错误时改写 Error()；
// 对 deadline / canceled / 其它通用 error 原样透传，保持上层 errors.Is 行为不变。
func wrapCLIProberError(err error) error {
	if err == nil {
		return nil
	}
	if msg, ok := formatCLIProberError(err); ok {
		return &proberErr{msg: msg, err: err}
	}
	return err
}

// formatCLIProberError 把 claudecode/codex 子进程错误整理成一行带 exit / stderr 的人话。
// 返回 (msg, true) 表示识别到子进程退出/exec 错误；(_, false) 表示不属于 CLI 范畴的错误，
// 调用方应保留原 err 以免吞掉 context.DeadlineExceeded 等 sentinel。
func formatCLIProberError(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	var cc *claudecode.ProcessExitError
	if errors.As(err, &cc) {
		return formatExitDetail("claudecode 进程", cc.Code, cc.Stderr), true
	}
	var cx *codex.ExitError
	if errors.As(err, &cx) {
		inner := ""
		if cx.Err != nil {
			inner = cx.Err.Error()
		}
		// codex.ExitError 没有原始 exit code 字段（包在 Err 里），尽力从 *exec.ExitError 取一下。
		code := -1
		var ee *exec.ExitError
		if errors.As(cx.Err, &ee) {
			code = ee.ExitCode()
		}
		if code >= 0 {
			return formatExitDetail("codex 进程", code, joinNonEmpty(inner, cx.Stderr)), true
		}
		msg := strings.TrimSpace(joinNonEmpty(inner, cx.Stderr))
		if msg == "" {
			msg = err.Error()
		}
		return "codex 进程退出: " + truncateStderr(msg), true
	}
	var px *piagent.ExitError
	if errors.As(err, &px) {
		inner := ""
		if px.Err != nil {
			inner = px.Err.Error()
		}
		code := -1
		var ee *exec.ExitError
		if errors.As(px.Err, &ee) {
			code = ee.ExitCode()
		}
		if code >= 0 {
			return formatExitDetail("piagent 进程", code, joinNonEmpty(inner, px.Stderr)), true
		}
		msg := strings.TrimSpace(joinNonEmpty(inner, px.Stderr))
		if msg == "" {
			msg = err.Error()
		}
		return "piagent 进程退出: " + truncateStderr(msg), true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return formatExitDetail("子进程", ee.ExitCode(), string(ee.Stderr)), true
	}
	return "", false
}

func formatExitDetail(label string, code int, stderr string) string {
	trimmed := truncateStderr(stderr)
	if trimmed == "" {
		return fmt.Sprintf("%s 退出码 %d", label, code)
	}
	return fmt.Sprintf("%s 退出码 %d: %s", label, code, trimmed)
}

func truncateStderr(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// 把换行/制表替换成空格，避免在单行 flash 里出现"换行→压缩"导致的拼接歧义。
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		}
		return r
	}, s)
	if len(s) > cliStderrSnippetLimit {
		return s[:cliStderrSnippetLimit] + "…"
	}
	return s
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ": ")
}
