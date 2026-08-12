package keychain

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGivenPlatformDirectoryModesWhenValidatingFileKeychainThenOnlyMeaningfulPermissionBitsAreEnforced(t *testing.T) {
	t.Run("Unix rejects group or other access", func(t *testing.T) {
		assert.False(t, fileDirPermissionsAreIsolated(fs.FileMode(0o755), "linux"))
		assert.True(t, fileDirPermissionsAreIsolated(fs.FileMode(0o700), "darwin"))
	})

	t.Run("Windows does not reject its synthetic permission bits", func(t *testing.T) {
		assert.True(t, fileDirPermissionsAreIsolated(fs.FileMode(0o777), "windows"))
	})
}
