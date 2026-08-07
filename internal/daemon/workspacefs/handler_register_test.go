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
}
