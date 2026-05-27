package cliprober

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentre/internal/pkg/clienv"
)

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary not portable to windows")
	}
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755))
	return p
}

func TestResolveCLIPath_FoundInFakePATH(t *testing.T) {
	dir := t.TempDir()
	want := writeExecutable(t, dir, "claude", "echo claude")
	t.Setenv("PATH", dir)

	path, found, err := ResolveCLIPath("claudecode")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, want, path)
}

func TestResolveCLIPath_NotFound_NoError(t *testing.T) {
	// clienv.ResolveBinary 会把 /opt/homebrew/bin、~/.local/bin 等都补进搜索路径,
	// 这是桌面 GUI 进程恢复 PATH 的有意行为,无法用 t.Setenv("PATH", ...) 屏蔽。
	// 在装了 claude 的开发机上跳过,只在干净环境真正验证 not-found 语义。
	if path, ok := clienv.ResolveBinary("claude"); ok {
		t.Skipf("system has claude at %q; cannot test not-found on this host", path)
	}
	t.Setenv("PATH", t.TempDir())
	path, found, err := ResolveCLIPath("claudecode")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, path)
}

func TestResolveCLIPath_InvalidType(t *testing.T) {
	_, _, err := ResolveCLIPath("nonsense")
	require.Error(t, err)
}

func TestResolveCLIPath_SkipAppBundleWrapper(t *testing.T) {
	wrapperDir := filepath.Join(t.TempDir(), "Claude.app", "Contents", "MacOS")
	require.NoError(t, os.MkdirAll(wrapperDir, 0o755))
	writeExecutable(t, wrapperDir, "claude", "echo wrapper")

	realDir := t.TempDir()
	want := writeExecutable(t, realDir, "claude", "echo real")

	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+realDir)
	path, found, err := ResolveCLIPath("claudecode")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, want, path)
}
