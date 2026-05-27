package agentruntime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentCwd_UsesAgentDirectory(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENTRE_DATA_DIR", dataDir)

	got, err := AgentCwd(42)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dataDir, "agents", "42"), got)
	info, err := os.Stat(got)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestAgentCwd_RejectsMissingAgentID(t *testing.T) {
	_, err := AgentCwd(0)
	assert.Error(t, err)
}
