//go:build e2e

package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/pkg/keychain"
)

// initKeychain 是 bootstrap 在装配 Server / Remote Device 之前选择 keychain 后端的
// 接缝(见 keychain_e2e.go)。这些用例钉死 e2e 的 keychain 边界
// (docs/specs/2026-08-12-agentred-service-runtime-fixes.md「E2E keychain 初始化与
// 安全边界」):设置 AGENTRE_E2E_KEYCHAIN_DIR 时必须在 Server / Remote Device 装配前
// 建立 file keychain;目录缺失 / 权限不安全 / 不可写 → 启动失败,绝不回退系统 keychain。

func TestInitKeychainGivenE2EKeychainDirSelectsFileBackend(t *testing.T) {
	// e2e runner 用 mkdirSync(…, {mode: 0o700}) 预创建隔离目录;测试同样显式建 0700
	// (本机 t.TempDir() 是 0755,不能直接当 keychain 目录)。
	dir := filepath.Join(t.TempDir(), "kc")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	original := keychain.Default()
	t.Cleanup(func() { keychain.SetDefault(original) })
	t.Setenv(E2EKeychainDirEnv, dir)

	require.NoError(t, initKeychain(context.Background()))

	// 先断言后端类型,再 probe —— 后端仍是系统 keychain 时不得误写生产凭据。
	require.Equal(t, reflect.TypeOf(keychain.NewFile(dir)), reflect.TypeOf(keychain.Default()))
	require.NoError(t, keychain.Default().Set("probe-account", "generated-test-value"))
	got, err := os.ReadFile(filepath.Join(dir, "probe-account"))
	require.NoError(t, err)
	assert.Equal(t, "generated-test-value", string(got))
}

func TestInitKeychainGivenMissingDirFailsWithoutSwappingBackend(t *testing.T) {
	original := keychain.Default()
	t.Cleanup(func() { keychain.SetDefault(original) })
	sentinel := keychain.NewMemory()
	keychain.SetDefault(sentinel)
	t.Setenv(E2EKeychainDirEnv, filepath.Join(t.TempDir(), "does-not-exist"))

	err := initKeychain(context.Background())
	require.Error(t, err)
	// 失败不得偷偷换后端(file 或 system):调用方应终止启动,而不是继续跑。
	assert.Same(t, sentinel, keychain.Default())
}

func TestInitKeychainGivenUnsafePermsFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kc")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	t.Setenv(E2EKeychainDirEnv, dir)

	err := initKeychain(context.Background())
	require.Error(t, err)
}

func TestInitKeychainGivenNoEnvKeepsSystemKeychain(t *testing.T) {
	original := keychain.Default()
	t.Cleanup(func() { keychain.SetDefault(original) })
	t.Setenv(E2EKeychainDirEnv, "")

	require.NoError(t, initKeychain(context.Background()))
	assert.Equal(t, reflect.TypeOf(keychain.NewSystem()), reflect.TypeOf(keychain.Default()))
}

// TestInitGivenE2EKeychainDirSelectsFileBackendBeforeServerWiring 验证真实启动顺序里
// (e2e dir → Init → InitServer → InitRemoteDevice)file keychain 在 Server / Remote
// Device 装配之前确定,而且 InitServer 不会把它覆盖回系统 keychain —— ConnPool /
// watcher / Add 构造时捕获的就是同一个 file backend。
func TestInitGivenE2EKeychainDirSelectsFileBackendBeforeServerWiring(t *testing.T) {
	dataDir := t.TempDir()
	keychainDir := filepath.Join(t.TempDir(), "kc")
	require.NoError(t, os.MkdirAll(keychainDir, 0o700))
	t.Setenv("AGENTRE_DATA_DIR", dataDir)
	t.Setenv("AGENTRE_ENV", "test")
	t.Setenv(E2EKeychainDirEnv, keychainDir)

	runtime, err := Init(context.Background())
	require.NoError(t, err)
	t.Cleanup(runtime.Close)

	require.Equal(t, reflect.TypeOf(keychain.NewFile(keychainDir)), reflect.TypeOf(keychain.Default()))
}
