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
	d1, _ := AppDataDir()
	d2, _ := AgentredDataDir()
	assert.NotEqual(t, d1, d2, "agentred dir must not collide with agentre dir")
}
