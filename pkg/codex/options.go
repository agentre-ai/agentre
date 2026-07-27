package codex

import "time"

type SandboxMode string

const (
	SandboxReadOnly         SandboxMode = "read-only"
	SandboxWorkspaceWrite   SandboxMode = "workspace-write"
	SandboxDangerFullAccess SandboxMode = "danger-full-access"
)

type ApprovalPolicy string

const (
	ApprovalUntrusted ApprovalPolicy = "untrusted"
	ApprovalOnFailure ApprovalPolicy = "on-failure"
	ApprovalOnRequest ApprovalPolicy = "on-request"
	ApprovalNever     ApprovalPolicy = "never"
)

type CollaborationMode string

const (
	CollaborationDefault CollaborationMode = "default"
	CollaborationPlan    CollaborationMode = "plan"
)

const defaultModelID = "gpt-5.5"

type Option func(*Client)

func WithBinary(path string) Option { return func(c *Client) { c.binary = path } }

func WithCwd(path string) Option { return func(c *Client) { c.cwd = path } }

func WithEnv(env map[string]string) Option {
	return func(c *Client) {
		c.env = cloneMap(env)
	}
}

func WithModel(model string) Option { return func(c *Client) { c.model = model } }

func WithSystemPrompt(prompt string) Option {
	return func(c *Client) { c.systemPrompt = prompt }
}

func WithSandbox(mode SandboxMode) Option { return func(c *Client) { c.sandbox = mode } }

func WithApproval(policy ApprovalPolicy) Option { return func(c *Client) { c.approval = policy } }

func WithConfig(expr string) Option {
	return func(c *Client) {
		if expr != "" {
			c.config = append(c.config, expr)
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

func WithAppServerRunnerForTesting(r appServerRunner) Option {
	return func(c *Client) {
		if r != nil {
			c.runner = r
		}
	}
}

// WithRawSink 注册一个原始行回调:app-server 每读到一行原始 stdout(未解析的 JSON-RPC
// 帧)就同步调用一次。用于 debug 级原始帧转储 —— runtime 层把它接到 logger.Debug,由
// 「Debug Logging」开关热控。回调收到的 []byte 是 scanner 复用缓冲,**不得跨调用留存**。
// nil(默认)= 零采样开销。
func WithRawSink(sink func([]byte)) Option { return func(c *Client) { c.rawSink = sink } }
