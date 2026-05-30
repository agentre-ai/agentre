package piagent

import "time"

type PermissionMode string

const (
	PermissionDefault PermissionMode = "default"
	PermissionPlan    PermissionMode = "plan"
)

type Option func(*Client)

func WithBinary(path string) Option { return func(c *Client) { c.binary = path } }

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
