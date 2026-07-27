package piagent

import (
	"strings"
	"time"
)

type PermissionMode string

const (
	PermissionDefault PermissionMode = "default"
	PermissionPlan    PermissionMode = "plan"
)

type Option func(*Client)

func WithBinary(path string) Option { return func(c *Client) { c.binary = path } }

// WithRawSink 注册一个原始行回调:子进程每读到一行原始 stdout(未解析的 JSON-RPC 帧)
// 就同步调用一次。用于 debug 级原始帧转储 —— runtime 层把它接到 logger.Debug,由
// 「Debug Logging」开关热控。回调收到的 []byte 是 scanner 复用缓冲,**不得跨调用留存**。
// nil(默认)= 零采样开销。
func WithRawSink(sink func([]byte)) Option { return func(c *Client) { c.rawSink = sink } }

func WithCwd(path string) Option { return func(c *Client) { c.cwd = path } }

// WithSessionDir 设置 Pi session JSONL 的存储目录（--session-dir），独立于 cwd。
func WithSessionDir(path string) Option { return func(c *Client) { c.sessionDir = path } }

// WithSession 设置要新建/resume 的 Pi session 文件路径（--session）。同一会话跨
// turn 传入相同路径即可复用上下文。
func WithSession(path string) Option { return func(c *Client) { c.session = path } }

func WithEnv(env map[string]string) Option {
	return func(c *Client) { c.env = cloneMap(env) }
}

func WithModel(model string) Option { return func(c *Client) { c.model = model } }

func WithSystemPrompt(prompt string) Option {
	return func(c *Client) { c.systemPrompt = prompt }
}

func WithThinking(level string) Option {
	return func(c *Client) { c.thinking = level }
}

// WithExtension 透传一个 pi 扩展文件路径（--extension <path>），可多次调用。
func WithExtension(path string) Option {
	return func(c *Client) {
		if p := strings.TrimSpace(path); p != "" {
			c.extensions = append(c.extensions, p)
		}
	}
}

func WithKillGrace(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.killGrace = d
		}
	}
}

func WithRPCProcessRunnerForTesting(r processRunner) Option {
	return func(c *Client) {
		if r != nil {
			c.runner = r
		}
	}
}
