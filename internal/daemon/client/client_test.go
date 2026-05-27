package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTLSConfig_Default(t *testing.T) {
	c, err := BuildTLSConfig(TLSDefault, "")
	require.NoError(t, err)
	assert.False(t, c.InsecureSkipVerify)
	assert.Nil(t, c.VerifyPeerCertificate)
}

func TestBuildTLSConfig_DefaultEmptyMode(t *testing.T) {
	// Empty string mode is treated as default for backward-compat with
	// callers that don't set mode explicitly.
	c, err := BuildTLSConfig("", "")
	require.NoError(t, err)
	assert.False(t, c.InsecureSkipVerify)
}

func TestBuildTLSConfig_SkipVerify(t *testing.T) {
	c, err := BuildTLSConfig(TLSSkipVerify, "")
	require.NoError(t, err)
	assert.True(t, c.InsecureSkipVerify)
}

func TestBuildTLSConfig_PinCert_Bad(t *testing.T) {
	_, err := BuildTLSConfig(TLSPinCert, "not a pem")
	assert.Error(t, err)
}

func TestBuildTLSConfig_CABundle_Bad(t *testing.T) {
	_, err := BuildTLSConfig(TLSCABundle, "not a pem")
	assert.Error(t, err)
}

func TestBuildTLSConfig_Unknown(t *testing.T) {
	_, err := BuildTLSConfig("nonsense", "")
	assert.Error(t, err)
}
