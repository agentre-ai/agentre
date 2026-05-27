package pairing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDeviceToken_HasEntropy(t *testing.T) {
	t1, err := NewDeviceToken()
	require.NoError(t, err)
	t2, _ := NewDeviceToken()
	assert.NotEqual(t, t1, t2)
	// 32 bytes base64url ≈ 43 chars; allow 42-44 for padding tolerance.
	assert.GreaterOrEqual(t, len(t1), 42)
	assert.LessOrEqual(t, len(t1), 44)
	assert.False(t, strings.ContainsAny(t1, "+/="), "base64url uses -_ not +/=")
}

func TestVerifyDeviceToken(t *testing.T) {
	tok, _ := NewDeviceToken()
	assert.True(t, VerifyDeviceToken(tok, tok))
	assert.False(t, VerifyDeviceToken(tok, tok+"x"))
	assert.False(t, VerifyDeviceToken(tok, ""))
}
