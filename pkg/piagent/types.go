package piagent

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

type rpcResponse struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Command string          `json:"command,omitempty"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcEvent struct {
	Type                  string          `json:"type"`
	Message               json.RawMessage `json:"message,omitempty"`
	Messages              json.RawMessage `json:"messages,omitempty"`
	AssistantMessageEvent assistantDelta  `json:"assistantMessageEvent,omitempty"`
	ToolCallID            string          `json:"toolCallId,omitempty"`
	ToolName              string          `json:"toolName,omitempty"`
	Args                  json.RawMessage `json:"args,omitempty"`
	Result                json.RawMessage `json:"result,omitempty"`
	IsError               bool            `json:"isError,omitempty"`
	Reason                string          `json:"reason,omitempty"`
	ErrorMessage          string          `json:"errorMessage,omitempty"`
}

type assistantDelta struct {
	Type     string          `json:"type,omitempty"`
	Delta    string          `json:"delta,omitempty"`
	Content  string          `json:"content,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Partial  json.RawMessage `json:"partial,omitempty"`
	ToolCall json.RawMessage `json:"toolCall,omitempty"`
}

type assistantMessage struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	Provider     string          `json:"provider"`
	Model        string          `json:"model"`
	Usage        *usageWire      `json:"usage"`
	StopReason   string          `json:"stopReason"`
	ErrorMessage string          `json:"errorMessage,omitempty"`
}

type usageWire struct {
	Input      int       `json:"input"`
	Output     int       `json:"output"`
	CacheRead  int       `json:"cacheRead"`
	CacheWrite int       `json:"cacheWrite"`
	Cost       *costWire `json:"cost,omitempty"`
}

type sessionStatsWire struct {
	ContextUsage *contextUsageWire `json:"contextUsage,omitempty"`
}

type contextUsageWire struct {
	ContextWindow int `json:"contextWindow"`
}

type costWire struct {
	Total float64 `json:"total"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

func buildEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := os.Environ()
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

func buildRPCArgs(c *Client) []string {
	args := []string{"--mode", "rpc"}
	if strings.TrimSpace(c.sessionDir) != "" {
		args = append(args, "--session-dir", strings.TrimSpace(c.sessionDir))
	}
	if strings.TrimSpace(c.session) != "" {
		args = append(args, "--session", strings.TrimSpace(c.session))
	}
	if strings.TrimSpace(c.systemPrompt) != "" {
		args = append(args, "--append-system-prompt", strings.TrimSpace(c.systemPrompt))
	}
	if strings.TrimSpace(c.model) != "" {
		args = append(args, "--model", strings.TrimSpace(c.model))
	}
	if thinking := normalizeThinkingLevel(c.thinking); thinking != "" {
		args = append(args, "--thinking", thinking)
	}
	for _, ext := range c.extensions {
		args = append(args, "--extension", ext)
	}
	return args
}

func normalizeThinkingLevel(level string) string {
	switch strings.TrimSpace(level) {
	case "low", "medium", "high", "xhigh":
		return strings.TrimSpace(level)
	case "max":
		return "xhigh"
	default:
		return ""
	}
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
