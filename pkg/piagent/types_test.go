package piagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildRPCArgsIncludesSystemPromptAndThinking(t *testing.T) {
	args := buildRPCArgs(&Client{
		sessionDir:   "/data/pi-sessions",
		model:        "gpt-5.5",
		thinking:     "high",
		systemPrompt: "be concise",
	})

	assert.Equal(t, []string{
		"--mode", "rpc",
		"--session-dir", "/data/pi-sessions",
		"--append-system-prompt", "be concise",
		"--model", "gpt-5.5",
		"--thinking", "high",
		"--no-context-files",
	}, args)
}

// session 非空时下发 --session，让 Pi 跨 turn resume 同一会话；--session-dir 用
// 专用目录而非 cwd，避免 session 文件污染工作目录。
func TestBuildRPCArgsResumesSession(t *testing.T) {
	args := buildRPCArgs(&Client{
		cwd:        "/work/project", // 工作目录不应进 args（它只作 cmd.Dir）
		sessionDir: "/data/pi-sessions",
		session:    "/data/pi-sessions/agentre-7.jsonl",
	})

	assert.Equal(t, []string{
		"--mode", "rpc",
		"--session-dir", "/data/pi-sessions",
		"--session", "/data/pi-sessions/agentre-7.jsonl",
		"--no-context-files",
	}, args)
}

func TestBuildRPCArgsClampsUnsupportedMaxThinking(t *testing.T) {
	args := buildRPCArgs(&Client{thinking: "max"})
	assert.Equal(t, []string{"--mode", "rpc", "--thinking", "xhigh", "--no-context-files"}, args)
}

func TestBuildRPCArgsOmitsInvalidThinking(t *testing.T) {
	args := buildRPCArgs(&Client{thinking: "ultra"})
	assert.Equal(t, []string{"--mode", "rpc", "--no-context-files"}, args)
}
