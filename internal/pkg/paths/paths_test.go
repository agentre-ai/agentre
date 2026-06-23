package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentredDataDir_EnvOverride(t *testing.T) {
	t.Setenv("AGENTRED_DATA_DIR", "/tmp/agentred-custom")
	dir, err := AgentredDataDir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/agentred-custom", dir)
}

func TestAgentredDataDir_Default(t *testing.T) {
	t.Setenv("AGENTRED_DATA_DIR", "")
	dir, err := AgentredDataDir()
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(dir, AppNameAgentred),
		"got %q want suffix %q", dir, AppNameAgentred)
	base, _ := os.UserConfigDir()
	assert.Equal(t, filepath.Join(base, AppNameAgentred), dir)
}

func TestAgentredDataDir_IsolatedFromAgentre(t *testing.T) {
	t.Setenv("AGENTRE_DATA_DIR", "")
	t.Setenv("AGENTRED_DATA_DIR", "")
	t.Setenv("devserver", "")
	d1, _ := AppDataDir()
	d2, _ := AgentredDataDir()
	assert.NotEqual(t, d1, d2, "agentred dir must not collide with agentre dir")
}

func TestIsDevMode(t *testing.T) {
	t.Setenv("devserver", "http://localhost:34115")
	assert.True(t, IsDevMode(), "devserver set => dev mode")

	t.Setenv("devserver", "  ")
	assert.False(t, IsDevMode(), "blank devserver => not dev mode")

	t.Setenv("devserver", "")
	assert.False(t, IsDevMode(), "unset devserver => not dev mode")
}

func TestAppDataDir_DevMode(t *testing.T) {
	t.Setenv("AGENTRE_DATA_DIR", "")
	t.Setenv("devserver", "http://localhost:34115")
	dir, err := AppDataDir()
	require.NoError(t, err)
	base, _ := os.UserConfigDir()
	assert.Equal(t, filepath.Join(base, AppNameDev), dir)
	assert.NotEqual(t, filepath.Join(base, AppName), dir,
		"dev data dir must not collide with the installed app dir")
}

func TestAppDataDir_EnvOverrideWinsInDevMode(t *testing.T) {
	t.Setenv("AGENTRE_DATA_DIR", "/tmp/agentre-custom")
	t.Setenv("devserver", "http://localhost:34115")
	dir, err := AppDataDir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/agentre-custom", dir,
		"explicit AGENTRE_DATA_DIR must win even in dev mode")
}

func TestAppDataDir_DefaultProd(t *testing.T) {
	t.Setenv("AGENTRE_DATA_DIR", "")
	t.Setenv("devserver", "")
	dir, err := AppDataDir()
	require.NoError(t, err)
	base, _ := os.UserConfigDir()
	assert.Equal(t, filepath.Join(base, AppName), dir,
		"prod path must be unchanged when not in dev mode")
}
