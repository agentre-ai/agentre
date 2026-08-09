package workspacefs_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/daemon/workspacefs"
	"github.com/agentre-ai/agentre/internal/pkg/workspacefs/wire"
)

func TestSearchFiles_Happy(t *testing.T) {
	h := workspacefs.NewHandlers(workspacefs.Options{})
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "Target.go"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "other.txt"), nil, 0o644))

	resp, err := h.SearchFiles(context.Background(), wire.SearchFilesReq{Root: root, Query: "target"})
	require.NoError(t, err)
	assert.False(t, resp.Truncated)
	require.Len(t, resp.Hits, 1)
	assert.Equal(t, "src/Target.go", resp.Hits[0].Path)
	assert.False(t, resp.Hits[0].IsDir)
}

func TestSearchFiles_EmptyRoot_NoCwd(t *testing.T) {
	h := workspacefs.NewHandlers(workspacefs.Options{})
	_, err := h.SearchFiles(context.Background(), wire.SearchFilesReq{Root: ""})
	require.Error(t, err)
	assert.True(t, errors.Is(err, wire.ErrNoCwd))
}

func TestSearchFiles_RelativeRoot_InvalidParams(t *testing.T) {
	h := workspacefs.NewHandlers(workspacefs.Options{})
	_, err := h.SearchFiles(context.Background(), wire.SearchFilesReq{Root: "relative/root", Query: "x"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, rpc.ErrInvalidParams))
}

func TestSearchFiles_TruncatedByHitCap(t *testing.T) {
	h := workspacefs.NewHandlers(workspacefs.Options{MaxSearchHits: 2})
	root := t.TempDir()
	for i := 0; i < 4; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(root, "target"+strconv.Itoa(i)), nil, 0o644))
	}

	resp, err := h.SearchFiles(context.Background(), wire.SearchFilesReq{Root: root, Query: "target"})
	require.NoError(t, err)
	assert.Len(t, resp.Hits, 2)
	assert.True(t, resp.Truncated)
}

func TestSearchFiles_TruncatedByDirBudget(t *testing.T) {
	h := workspacefs.NewHandlers(workspacefs.Options{MaxSearchDirs: 1})
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "b"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "b", "target.txt"), nil, 0o644))

	resp, err := h.SearchFiles(context.Background(), wire.SearchFilesReq{Root: root, Query: "target"})
	require.NoError(t, err)
	assert.True(t, resp.Truncated, "目录预算耗尽必须显式回报截断")
	assert.Empty(t, resp.Hits)
}

// TestRegister_SearchFiles 确认新方法真的挂在了 registry 上,且 sentinel 被翻成
// *rpc.Error(与既有五个方法同一条管线)。
func TestRegister_SearchFiles(t *testing.T) {
	reg := rpc.NewRegistry()
	h := workspacefs.NewHandlers(workspacefs.Options{})
	workspacefs.Register(reg, h, func(fn rpc.HandlerFunc) rpc.HandlerFunc { return fn })

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target.txt"), nil, 0o644))

	raw, _ := json.Marshal(wire.SearchFilesReq{Root: dir, Query: "target"})
	res, err := reg.Dispatch(context.Background(), wire.MethodSearchFiles, raw)
	require.NoError(t, err)
	resp, ok := res.(*wire.SearchFilesResp)
	require.True(t, ok)
	require.Len(t, resp.Hits, 1)
	assert.Equal(t, "target.txt", resp.Hits[0].Path)

	// 空 root 触发 wire.ErrNoCwd,客户端拿到的是 *rpc.Error 而不是裸 sentinel。
	raw, _ = json.Marshal(wire.SearchFilesReq{Root: "", Query: "target"})
	_, err = reg.Dispatch(context.Background(), wire.MethodSearchFiles, raw)
	var rpcErr *rpc.Error
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, wire.ErrCodeNoCwd, rpcErr.Code)
}
