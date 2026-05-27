package cliprober

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentre/pkg/claudecode"
	"agentre/pkg/codex"
)

func TestProbe_UnknownType(t *testing.T) {
	_, err := Probe(context.Background(), ProbeRequest{Type: "nope"})
	require.ErrorIs(t, err, ErrInvalidType)
}

func TestWrapCLIProberError_NilPassthrough(t *testing.T) {
	assert.Nil(t, wrapCLIProberError(nil))
}

func TestWrapCLIProberError_PreservesSentinel(t *testing.T) {
	// 上层依赖 errors.Is(err, context.DeadlineExceeded) 出"测试超时"文案；
	// 包装层不能吞掉这个 sentinel。
	wrapped := wrapCLIProberError(context.DeadlineExceeded)
	assert.True(t, errors.Is(wrapped, context.DeadlineExceeded))

	wrapped = wrapCLIProberError(context.Canceled)
	assert.True(t, errors.Is(wrapped, context.Canceled))
}

func TestWrapCLIProberError_WrapsExit(t *testing.T) {
	original := &claudecode.ProcessExitError{Code: 1, Stderr: "boom"}
	wrapped := wrapCLIProberError(original)
	assert.NotNil(t, wrapped)
	assert.Contains(t, wrapped.Error(), "退出码 1")
	// Unwrap 还能拿到原始 typed error。
	var cc *claudecode.ProcessExitError
	assert.True(t, errors.As(wrapped, &cc))
	assert.Equal(t, 1, cc.Code)
}

func TestFormatCLIProberError(t *testing.T) {
	t.Run("claudecode ProcessExitError → 含 exit code + stderr", func(t *testing.T) {
		err := &claudecode.ProcessExitError{Code: 127, Stderr: "command not found: claude"}
		msg, ok := formatCLIProberError(err)
		require.True(t, ok)
		assert.Contains(t, msg, "claudecode 进程")
		assert.Contains(t, msg, "退出码 127")
		assert.Contains(t, msg, "command not found: claude")
	})

	t.Run("codex ExitError → 含 codex 进程退出 + stderr", func(t *testing.T) {
		err := &codex.ExitError{Err: errors.New("kill -9 received"), Stderr: "fatal: token revoked"}
		msg, ok := formatCLIProberError(err)
		require.True(t, ok)
		assert.Contains(t, msg, "codex 进程退出")
		assert.Contains(t, msg, "fatal: token revoked")
	})

	t.Run("普通 error → 不识别为 CLI 错误，调用方应保留原 err", func(t *testing.T) {
		_, ok := formatCLIProberError(errors.New("401 unauthorized"))
		assert.False(t, ok)
	})

	t.Run("nil err 不 panic", func(t *testing.T) {
		_, ok := formatCLIProberError(nil)
		assert.False(t, ok)
	})
}

func TestTruncateStderr(t *testing.T) {
	long := strings.Repeat("x", 500)
	out := truncateStderr(long)
	assert.LessOrEqual(t, len(out), cliStderrSnippetLimit+len("…"))
	assert.True(t, strings.HasSuffix(out, "…"))
}

func TestProbe_ClaudeCode_FakeCLI_ExitNonZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary not portable to windows")
	}
	// 不真测端到端 claudecode SDK（依赖网络），只覆盖入口分发 + 错误整形：
	// fake CLI exit 9 应走 wrapCLIProberError 路径返回非 nil 错误。
	dir := t.TempDir()
	fake := writeExecutable(t, dir, "claude", "exit 9")
	_, err := Probe(context.Background(), ProbeRequest{
		Type:    "claudecode",
		CLIPath: fake,
		Env:     map[string]string{"PATH": filepath.Dir(fake)},
	})
	require.Error(t, err)
}
