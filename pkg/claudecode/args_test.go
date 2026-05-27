package claudecode

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildArgs_DefaultsForStreamMode(t *testing.T) {
	got := buildArgs(runSpec{})
	joined := strings.Join(got, " ")

	// 必有 flag：stream-json 协议要求两边都开 + verbose + include-partial-messages + permission acceptEdits 兜底。
	// 不应该出现 -p：claude 接到管道 stdout 时自动非交互，-p 反而会让 result 帧后立刻 exit、
	// 干掉我们想保留的"stdin EOF 才退出"语义。首项断言锁死避免回归。
	assert.Equal(t, "--output-format", got[0], "buildArgs must not prepend -p; first arg should be --output-format")
	assert.NotContains(t, got, "-p")
	assert.Contains(t, joined, "--output-format stream-json")
	assert.Contains(t, joined, "--input-format stream-json")
	assert.Contains(t, joined, "--verbose")
	assert.Contains(t, joined, "--include-partial-messages")
	assert.Contains(t, joined, "--permission-mode acceptEdits")
}

func TestBuildArgs_ResumeAppendsSessionID(t *testing.T) {
	got := buildArgs(runSpec{resumeID: "sess-abc"})
	assert.Contains(t, strings.Join(got, " "), "--resume sess-abc")
}

// TestBuildArgs_ResumeAloneOmitsSessionIDFlag 锁住「resume 路径不能同时带 --session-id」
// 这一硬约束：CLI 收到 `--resume <a> --session-id <b>` 但没有 --fork-session 时会立刻
// 退出（用户看到空白回复无报错），所以上层装配 runSpec 时也必须遵守这条不变量。
func TestBuildArgs_ResumeAloneOmitsSessionIDFlag(t *testing.T) {
	got := buildArgs(runSpec{resumeID: "sess-abc"})
	assert.NotContains(t, strings.Join(got, " "), "--session-id",
		"resume 单飞时必须不带 --session-id；要换 sid 请加 --fork-session")
}

func TestBuildArgs_ForkAlone_NoResume_OK(t *testing.T) {
	// --fork-session 在没有 resume 时无意义但 CLI 允许；buildArgs 透传，校验留给 client。
	got := buildArgs(runSpec{forkSession: true})
	assert.Contains(t, strings.Join(got, " "), "--fork-session")
}

func TestBuildArgs_ResumeSessionAt_RequiresFork(t *testing.T) {
	// 见 spec §4：resume-session-at 单独用会破坏性 rewind 原 session。
	// 这里 buildArgs 仍透传两个 flag，业务校验在 Client 层；测试只确认 argv 拼接顺序。
	got := buildArgs(runSpec{resumeID: "sess-abc", resumeSessionAtUUID: "uuid-1", forkSession: true})
	joined := strings.Join(got, " ")
	assert.Contains(t, joined, "--resume sess-abc")
	assert.Contains(t, joined, "--resume-session-at uuid-1")
	assert.Contains(t, joined, "--fork-session")
}

func TestBuildArgs_ModelAndSystemPrompt(t *testing.T) {
	got := buildArgs(runSpec{model: "claude-sonnet-4-6", systemPrompt: "you are helpful"})
	joined := strings.Join(got, " ")
	assert.Contains(t, joined, "--model claude-sonnet-4-6")
	assert.Contains(t, joined, "--append-system-prompt you are helpful")
}

func TestBuildArgs_IncludesSessionIDAndSettings(t *testing.T) {
	spec := runSpec{
		sessionID: "550e8400-e29b-41d4-a716-446655440000",
		settings:  "/tmp/agentre/settings.json",
	}
	joined := strings.Join(buildArgs(spec), " ")
	assert.Contains(t, joined, "--session-id 550e8400-e29b-41d4-a716-446655440000")
	assert.Contains(t, joined, "--settings /tmp/agentre/settings.json")
}

// TestBuildArgs_EffortLevel 验证 --effort 仅在 spec.effort 非空时拼入。
// claude CLI 支持 low / medium / high / xhigh / max；空值代表「CLI 自身默认」。
func TestBuildArgs_EffortLevel(t *testing.T) {
	t.Run("omitted when empty", func(t *testing.T) {
		joined := strings.Join(buildArgs(runSpec{}), " ")
		assert.NotContains(t, joined, "--effort")
	})

	for _, level := range []string{"low", "medium", "high", "xhigh", "max"} {
		t.Run("included with "+level, func(t *testing.T) {
			joined := strings.Join(buildArgs(runSpec{effort: level}), " ")
			assert.Contains(t, joined, "--effort "+level)
		})
	}
}

// TestBuildArgs_BypassPermissionMode locks in the post-cleanup behavior:
// bypassPermissions is reached purely via --permission-mode; agentre no longer
// emits --dangerously-skip-permissions because that flag is a one-way enabling
// token that CLI refuses to re-enter bypass through `set_permission_mode`.
func TestBuildArgs_BypassPermissionMode(t *testing.T) {
	got := buildArgs(runSpec{permissionMode: "bypassPermissions"})
	joined := strings.Join(got, " ")
	assert.Contains(t, joined, "--permission-mode bypassPermissions")
	assert.NotContains(t, joined, "--dangerously-skip-permissions",
		"agentre must not emit --dangerously-skip-permissions; bypass is reached via --permission-mode")
}

// TestBuildArgs_AllowedToolsCommaSeparated 覆盖 joinComma 拼接，原来 0% 覆盖。
// 单元素：不能有多余逗号；多元素：分隔符正确。
func TestBuildArgs_AllowedToolsCommaSeparated(t *testing.T) {
	single := strings.Join(buildArgs(runSpec{allowedTools: []string{"Read"}}), " ")
	assert.Contains(t, single, "--allowedTools Read")
	assert.NotContains(t, single, "Read,")

	multi := strings.Join(buildArgs(runSpec{
		allowedTools:    []string{"Read", "Bash"},
		disallowedTools: []string{"Write", "Edit"},
	}), " ")
	assert.Contains(t, multi, "--allowedTools Read,Bash")
	assert.Contains(t, multi, "--disallowedTools Write,Edit")
}
