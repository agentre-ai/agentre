package workspacefs_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/daemon/workspacefs"
	"github.com/agentre-ai/agentre/internal/pkg/workspacefs/wire"
)

func TestRegister_AllThreeMethods_TranslateSentinel(t *testing.T) {
	reg := rpc.NewRegistry()
	h := workspacefs.NewHandlers(workspacefs.Options{})
	// wrap 透传 = 等价于不做 auth 检查,方便单测;生产由 daemon 传 requireAuth 闭包。
	workspacefs.Register(reg, h, func(fn rpc.HandlerFunc) rpc.HandlerFunc { return fn })

	dir := t.TempDir()

	// listDir 的越界 relPath 触发 wire.ErrPathRefused,验证客户端能拿到
	// *rpc.Error 而不是裸 sentinel。
	raw, _ := json.Marshal(wire.ListDirReq{Root: dir, RelPath: "../etc"})
	_, err := reg.Dispatch(context.Background(), wire.MethodListDir, raw)
	var rpcErr *rpc.Error
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, wire.ErrCodePathRefused, rpcErr.Code)

	// gitChanges 的 scope=="branch" 缺 baseRef 触发 wire.ErrBaselineRequired
	// (仅在真仓库里生效:NotARepo 的短路检查在 baseline 检查之前,见
	// pkgworkspacefs.GitChanges,所以这里必须用真仓库而非普通 tmpdir)。
	repoDir := initRepo(t)
	raw, _ = json.Marshal(wire.GitChangesReq{Root: repoDir, Scope: wire.ScopeBranch})
	_, err = reg.Dispatch(context.Background(), wire.MethodGitChanges, raw)
	rpcErr = nil
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, wire.ErrCodeBaselineRequired, rpcErr.Code)

	// gitBranches 走通(非仓库降级,不报错),确认三个方法都真的挂上了 registry。
	raw, _ = json.Marshal(wire.GitBranchesReq{Root: dir})
	res, err := reg.Dispatch(context.Background(), wire.MethodGitBranches, raw)
	require.NoError(t, err)
	resp, ok := res.(*wire.GitBranchesResp)
	require.True(t, ok)
	assert.True(t, resp.NotARepo)

	// readFile 越界 relPath 触发 wire.ErrPathRefused,客户端拿到 *rpc.Error。
	raw, _ = json.Marshal(wire.ReadFileReq{Root: dir, RelPath: "../etc"})
	_, err = reg.Dispatch(context.Background(), wire.MethodReadFile, raw)
	rpcErr = nil
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, wire.ErrCodePathRefused, rpcErr.Code)

	// readFile 空 root 触发 wire.ErrNoCwd(会话配置问题,与越界区分)。
	raw, _ = json.Marshal(wire.ReadFileReq{Root: ""})
	_, err = reg.Dispatch(context.Background(), wire.MethodReadFile, raw)
	rpcErr = nil
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, wire.ErrCodeNoCwd, rpcErr.Code)

	// gitFileContent 在非仓库上降级(不报错),确认它也真的挂上了 registry。
	raw, _ = json.Marshal(wire.GitFileContentReq{Root: dir, RelPath: "a.txt"})
	res, err = reg.Dispatch(context.Background(), wire.MethodGitFileContent, raw)
	require.NoError(t, err)
	gfcResp, ok := res.(*wire.GitFileContentResp)
	require.True(t, ok)
	assert.True(t, gfcResp.NotARepo)
}

func TestRegister_ReadFileGitFileContent_TranslateSentinel(t *testing.T) {
	reg := rpc.NewRegistry()
	h := workspacefs.NewHandlers(workspacefs.Options{})
	workspacefs.Register(reg, h, func(fn rpc.HandlerFunc) rpc.HandlerFunc { return fn })

	// gitFileContent 越界 relPath 触发 wire.ErrPathRefused,与 readFile 同一道
	// 边界(真仓库里仍先过路径闸门)。
	repoDir := initRepo(t)
	raw, _ := json.Marshal(wire.GitFileContentReq{Root: repoDir, RelPath: "../etc"})
	_, err := reg.Dispatch(context.Background(), wire.MethodGitFileContent, raw)
	var rpcErr *rpc.Error
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, wire.ErrCodePathRefused, rpcErr.Code)

	// gitFileContent 空 root 触发 wire.ErrNoCwd。
	raw, _ = json.Marshal(wire.GitFileContentReq{Root: ""})
	_, err = reg.Dispatch(context.Background(), wire.MethodGitFileContent, raw)
	rpcErr = nil
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, wire.ErrCodeNoCwd, rpcErr.Code)
}
