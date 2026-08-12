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

func TestGivenCoreE2EHarnessWhenLaunchingAppThenItProvidesAnIsolatedKeychainDir(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	e2eDir := filepath.Dir(filename)
	config, err := os.ReadFile(filepath.Join(e2eDir, "playwright.config.ts")) //nolint:gosec // G304: directory comes from this test file; filename is fixed.
	require.NoError(t, err)
	source := string(config)

	assert.Contains(t, source, `const keychainDir = join(tmpdir(), "agentre-e2e-keychain");`)
	assert.Contains(t, source, "mkdirSync(keychainDir, { recursive: true, mode: 0o700 });")
	assert.Contains(t, source, "AGENTRE_E2E_KEYCHAIN_DIR: keychainDir")

	readme, err := os.ReadFile(filepath.Join(e2eDir, "README.md")) //nolint:gosec // G304: directory comes from this test file; filename is fixed.
	require.NoError(t, err)
	assert.Contains(t, string(readme), `AGENTRE_E2E_KEYCHAIN_DIR="$TMPDIR/agentre-e2e-keychain"`)
}

func TestGivenReusableE2EHarnessWhenPrintingStartupInstructionsThenItRequiresTheSameKeychainOverride(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	runner, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "run-e2e.mjs"))
	require.NoError(t, err)
	source := string(runner)

	assert.True(t, strings.Contains(source, "AGENTRE_E2E_KEYCHAIN_DIR=\"${keychainDir}\""),
		"reuse instructions must not start an e2e app without the isolated keychain override")
}
