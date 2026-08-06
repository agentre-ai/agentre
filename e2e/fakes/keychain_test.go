//go:build e2e

package fakes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/pkg/keychain"
)

func TestGivenE2EKeychainDirWhenInstallingOverrideThenSecretsStayInThatDirectory(t *testing.T) {
	dir := t.TempDir()
	original := keychain.Default()
	t.Cleanup(func() { keychain.SetDefault(original) })
	keychain.SetDefault(keychain.NewMemory())
	t.Setenv(e2eKeychainDirEnv, dir)

	installE2EKeychainOverride()

	require.NoError(t, keychain.Default().Set("probe-account", "generated-test-value"))
	got, err := os.ReadFile(filepath.Join(dir, "probe-account"))
	require.NoError(t, err)
	assert.Equal(t, "generated-test-value", string(got))
}

func TestGivenNoE2EKeychainDirWhenInstallingOverrideThenExistingStoreIsPreserved(t *testing.T) {
	original := keychain.Default()
	t.Cleanup(func() { keychain.SetDefault(original) })
	existing := keychain.NewMemory()
	keychain.SetDefault(existing)
	t.Setenv(e2eKeychainDirEnv, "")

	installE2EKeychainOverride()

	assert.Same(t, existing, keychain.Default())
}
