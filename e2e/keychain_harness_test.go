package e2e_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 隔离 keychain 只有在「每一条启动路径都注入 AGENTRE_KEYCHAIN_DIR」时才成立。这两个用例把
// 两条路径分别钉死,并且跑在默认构建里(make test-backend),不需要 e2e build tag。

func TestGivenCoreE2EHarnessWhenLaunchingAppThenItProvidesAnIsolatedKeychainDir(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	e2eDir := filepath.Dir(filename)
	config, err := os.ReadFile(filepath.Join(e2eDir, "playwright.config.ts")) //nolint:gosec // G304: directory comes from this test file; filename is fixed.
	require.NoError(t, err)
	source := string(config)

	// 委托而不是自己拼路径:committed suite 与 verify.mjs 必须解析到同一个隔离目标,
	// 任何一处自己造 dataDir / keychainDir / env 都会让两条路径悄悄分叉。
	assert.Contains(t, source, `resolveTarget("fake")`)
	assert.Contains(t, source, "prepareDirs(target,")
	assert.Contains(t, source, "launchEnv(target)")
	assert.NotContains(t, source, "AGENTRE_DATA_DIR:",
		"the suite must take its env from launchEnv(), not hand-assemble the overrides")

	readme, err := os.ReadFile(filepath.Join(e2eDir, "README.md")) //nolint:gosec // G304: directory comes from this test file; filename is fixed.
	require.NoError(t, err)
	assert.Contains(t, string(readme), "AGENTRE_KEYCHAIN_DIR")
}

// verify.mjs 起的 app 与 Playwright 起的 app 走同一份 launchEnv:少注入一项,验证就会写进
// 真实 system keychain / 真实数据目录,而且没有任何报错——所以这里逐项断言。
func TestGivenVerificationLauncherWhenStartingAppThenItInjectsTheIsolationOverrides(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	target, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "lib", "target.mjs"))
	require.NoError(t, err)
	source := string(target)

	launchEnv := source[strings.Index(source, "export function launchEnv"):]
	for _, key := range []string{
		"AGENTRE_DATA_DIR",
		"AGENTRE_KEYCHAIN_DIR",
		"AGENTRE_PROXY_PORT",
		"AGENTRE_ENV",
	} {
		assert.Contains(t, launchEnv, key, "launchEnv must inject %s", key)
	}
	// keychain 目录必须以 0700 建出来:bootstrap 对权限不安全的目录直接终止启动。
	assert.Contains(t, source, "mkdirSync(target.keychainDir, { recursive: true, mode: 0o700 })")
}
