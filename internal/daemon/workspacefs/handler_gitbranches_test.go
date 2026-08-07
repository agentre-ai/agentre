package workspacefs_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/workspacefs"
	"github.com/agentre-ai/agentre/internal/pkg/workspacefs/wire"
)

func TestGitBranches_ListsLocal(t *testing.T) {
	h := workspacefs.NewHandlers(workspacefs.Options{})
	dir := initRepo(t)
	runGit(t, dir, "branch", "feature/x")

	resp, err := h.GitBranches(context.Background(), wire.GitBranchesReq{Root: dir})
	require.NoError(t, err)
	assert.False(t, resp.NotARepo)

	names := map[string]wire.Branch{}
	for _, b := range resp.Branches {
		names[b.Name] = b
	}
	assert.Contains(t, names, "main")
	assert.False(t, names["main"].Remote)
	assert.Contains(t, names, "feature/x")
}

func TestGitBranches_NonRepo_Degrades(t *testing.T) {
	h := workspacefs.NewHandlers(workspacefs.Options{})
	dir := t.TempDir()

	resp, err := h.GitBranches(context.Background(), wire.GitBranchesReq{Root: dir})
	require.NoError(t, err)
	assert.True(t, resp.NotARepo)
	assert.Empty(t, resp.Branches)
}
