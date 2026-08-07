package workspace_fs_svc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	mockRT "github.com/agentre-ai/agentre/internal/pkg/agentruntime/mock_agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/pkg/workspacefs/wire"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
	mockRD "github.com/agentre-ai/agentre/internal/service/remote_device_svc/mock_remote_device_svc"
)

// ── helpers ─────────────────────────────────────────────────────────────────

type rig struct {
	ctx    context.Context
	rd     *mockRD.MockRemoteDeviceSvc
	pool   *mockRD.MockConnPool
	lease  *mockRD.MockLease
	client *mockRT.MockDaemonClientPort
	svc    *workspaceFsImpl
}

// newRig 装配一个只有 mock 依赖的 svc。resolver 是本服务自声明的窄接口,单测
// 直接注入闭包 —— 不连 DB,也不 import chat_svc。
func newRig(t *testing.T, deviceID int64, cwd string) *rig {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	r := &rig{
		ctx:    context.Background(),
		rd:     mockRD.NewMockRemoteDeviceSvc(ctrl),
		pool:   mockRD.NewMockConnPool(ctrl),
		lease:  mockRD.NewMockLease(ctrl),
		client: mockRT.NewMockDaemonClientPort(ctrl),
	}
	r.svc = &workspaceFsImpl{
		rdSvc: r.rd,
		resolver: func(context.Context, int64) (int64, string, error) {
			return deviceID, cwd, nil
		},
	}
	return r
}

// expectCall 声明一次完整的远端往返(Borrow → Client → Call → Release)。
func (r *rig) expectCall(method string, req any) *gomock.Call {
	r.rd.EXPECT().Pool().Return(r.pool)
	r.pool.EXPECT().Borrow(r.ctx, gomock.Any()).Return(r.lease, nil)
	r.lease.EXPECT().Client().Return(r.client)
	r.lease.EXPECT().Release()
	return r.client.EXPECT().Call(r.ctx, method, req, gomock.Any())
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // G204: test helper, args 来自测试内常量
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
}

// initRepo 建一个真实临时 git 仓库(本机分支直接调叶子包,需要真目录;这不是
// 数据库依赖)。
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "user.email", "t@t")
	runGit(t, dir, "config", "user.name", "t")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

// ── 路由分叉:deviceID 空/非空 ───────────────────────────────────────────────

func TestListDir_RoutesByDeviceID(t *testing.T) {
	convey.Convey("ListDir 按 deviceID 路由", t, func() {
		convey.Convey("deviceID=0 → 本机 in-process,不借租约", func() {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644))
			require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
			r := newRig(t, 0, dir)
			// rd 上没有任何 EXPECT:一旦走了远端分支,gomock 会直接判错。

			view, err := r.svc.ListDir(r.ctx, 42, "", false)
			require.NoError(t, err)
			assert.Equal(t, dir, view.Path)
			names := map[string]bool{}
			for _, e := range view.Entries {
				names[e.Name] = e.IsDir
			}
			assert.Contains(t, names, "a.txt")
			assert.True(t, names["sub"])
		})

		convey.Convey("deviceID≠0 → 走 RPC,root 用服务解析出的 cwd", func() {
			r := newRig(t, 7, "/remote/work")
			r.expectCall(wire.MethodListDir, wire.ListDirReq{
				Root: "/remote/work", RelPath: "sub", IncludeIgnored: true,
			}).DoAndReturn(func(_ context.Context, _ string, _ any, out any) error {
				resp := out.(*wire.ListDirResp)
				resp.Path = "/remote/work/sub"
				resp.Entries = []wire.Entry{{Name: "f.go", Size: 12, ModTime: 1700000000, GitIgnored: true}}
				resp.Truncated = true
				return nil
			})

			view, err := r.svc.ListDir(r.ctx, 42, "sub", true)
			require.NoError(t, err)
			assert.Equal(t, "/remote/work/sub", view.Path)
			assert.True(t, view.Truncated)
			require.Len(t, view.Entries, 1)
			assert.Equal(t, "f.go", view.Entries[0].Name)
			assert.True(t, view.Entries[0].GitIgnored)
		})
	})
}

// ── 会话解析失败 / cwd 为空的降级 ───────────────────────────────────────────

func TestWorkspaceResolution_Degrades(t *testing.T) {
	convey.Convey("会话解析", t, func() {
		convey.Convey("cwd 为空 → 报错且不借租约", func() {
			r := newRig(t, 7, "") // 远端会话但该设备上没配项目路径
			_, err := r.svc.ListDir(r.ctx, 42, "", false)
			require.Error(t, err)
			assert.Equal(t, i18n.NewError(r.ctx, code.WorkspaceFsNoCwd).Error(), err.Error())
		})

		convey.Convey("resolver 报错 → 原样透出(会话不存在等由 chat_svc 决定文案)", func() {
			r := newRig(t, 0, "/tmp")
			sentinel := errors.New("session not found")
			r.svc.resolver = func(context.Context, int64) (int64, string, error) {
				return 0, "", sentinel
			}
			_, err := r.svc.GitBranches(r.ctx, 42)
			assert.ErrorIs(t, err, sentinel)
		})

		convey.Convey("sessionID 非法 → 不调 resolver", func() {
			r := newRig(t, 0, "/tmp")
			r.svc.resolver = func(context.Context, int64) (int64, string, error) {
				t.Fatal("resolver must not be called for an invalid sessionID")
				return 0, "", nil
			}
			_, err := r.svc.ListDir(r.ctx, 0, "", false)
			assert.Error(t, err)
		})
	})
}

// ── 越界路径:两端都拒 ───────────────────────────────────────────────────────

func TestListDir_PathRefused(t *testing.T) {
	convey.Convey("越界路径", t, func() {
		convey.Convey("本机:叶子包 sentinel → WorkspaceFsPathRefused", func() {
			r := newRig(t, 0, t.TempDir())
			_, err := r.svc.ListDir(r.ctx, 42, "../etc", false)
			require.Error(t, err)
			assert.Equal(t, i18n.NewError(r.ctx, code.WorkspaceFsPathRefused).Error(), err.Error())
		})

		convey.Convey("远端:wire code → 同一个 WorkspaceFsPathRefused", func() {
			r := newRig(t, 7, "/remote/work")
			r.expectCall(wire.MethodListDir, wire.ListDirReq{Root: "/remote/work", RelPath: "../etc"}).
				Return(&rpc.Error{Code: wire.ErrCodePathRefused, Message: "refused"})
			_, err := r.svc.ListDir(r.ctx, 42, "../etc", false)
			require.Error(t, err)
			assert.Equal(t, i18n.NewError(r.ctx, code.WorkspaceFsPathRefused).Error(), err.Error())
		})
	})
}

// ── 远端 daemon 版本过旧 ────────────────────────────────────────────────────

func TestRemote_MethodNotFound_IsDaemonOutdated(t *testing.T) {
	convey.Convey("旧 daemon 不认识 workspacefs.* → 版本过旧,而不是通用调用失败", t, func() {
		r := newRig(t, 7, "/remote/work")
		r.expectCall(wire.MethodListDir, gomock.Any()).
			Return(&rpc.Error{Code: -32601, Message: "Method not found"})

		_, err := r.svc.ListDir(r.ctx, 42, "", false)
		require.Error(t, err)
		assert.Equal(t, i18n.NewError(r.ctx, code.WorkspaceFsDaemonOutdated).Error(), err.Error())
		assert.NotEqual(t, i18n.NewError(r.ctx, code.RemoteRunnerCallFailed).Error(), err.Error(),
			"必须与通用远端调用失败区分开,否则前端无法提示升级 agentred")
	})
}

func TestRemote_BorrowFailure_IsDeviceOffline(t *testing.T) {
	convey.Convey("借不到租约 → 设备不在线", t, func() {
		r := newRig(t, 7, "/remote/work")
		r.rd.EXPECT().Pool().Return(r.pool)
		r.pool.EXPECT().Borrow(r.ctx, int64(7)).Return(nil, errors.New("dial fail"))

		_, err := r.svc.GitBranches(r.ctx, 42)
		require.Error(t, err)
		assert.Equal(t, i18n.NewError(r.ctx, code.WorkspaceFsDeviceOffline).Error(), err.Error())
	})

	convey.Convey("设备不存在 / 凭据失效各有自己的文案", t, func() {
		r := newRig(t, 7, "/remote/work")
		r.rd.EXPECT().Pool().Return(r.pool)
		r.pool.EXPECT().Borrow(r.ctx, int64(7)).Return(nil, remote_device_svc.ErrDeviceNotFound)
		_, err := r.svc.GitBranches(r.ctx, 42)
		require.Error(t, err)
		assert.Equal(t, i18n.NewError(r.ctx, code.RemoteDeviceNotFound).Error(), err.Error())
	})
}

// ── GitBranches:currentBranch + defaultBaseline 过 RPC ─────────────────────

func TestGitBranches_RemoteCarriesCurrentBranchAndDefaultBaseline(t *testing.T) {
	convey.Convey("远端会话拿得到当前分支与推断出的默认基线", t, func() {
		r := newRig(t, 7, "/remote/work")
		r.expectCall(wire.MethodGitBranches, wire.GitBranchesReq{Root: "/remote/work"}).
			DoAndReturn(func(_ context.Context, _ string, _ any, out any) error {
				resp := out.(*wire.GitBranchesResp)
				resp.Branches = []wire.Branch{{Name: "main"}, {Name: "origin/main", Remote: true}}
				resp.CurrentBranch = "feature/x"
				resp.DefaultBaseline = "origin/main"
				return nil
			})

		view, err := r.svc.GitBranches(r.ctx, 42)
		require.NoError(t, err)
		assert.False(t, view.NotARepo)
		assert.Equal(t, "feature/x", view.CurrentBranch)
		assert.Equal(t, "origin/main", view.DefaultBaseline)
		require.Len(t, view.Branches, 2)
		assert.True(t, view.Branches[1].Remote)
	})
}

func TestGitBranches_LocalUsesLeafPackage(t *testing.T) {
	convey.Convey("本机会话的当前分支与默认基线同样来自叶子包", t, func() {
		dir := initRepo(t)
		runGit(t, dir, "checkout", "-q", "-b", "feature/x")
		r := newRig(t, 0, dir)

		view, err := r.svc.GitBranches(r.ctx, 42)
		require.NoError(t, err)
		assert.Equal(t, "feature/x", view.CurrentBranch)
		assert.Equal(t, "main", view.DefaultBaseline)
	})

	convey.Convey("非 git 目录 → NotARepo,不报错", t, func() {
		r := newRig(t, 0, t.TempDir())
		view, err := r.svc.GitBranches(r.ctx, 42)
		require.NoError(t, err)
		assert.True(t, view.NotARepo)
	})
}

// ── GitChanges:基线解析 ────────────────────────────────────────────────────

func TestGitChanges_ScopeValidation(t *testing.T) {
	convey.Convey("未知 scope → 参数错误,不解析会话", t, func() {
		r := newRig(t, 0, "/tmp")
		r.svc.resolver = func(context.Context, int64) (int64, string, error) {
			t.Fatal("resolver must not be called for an unknown scope")
			return 0, "", nil
		}
		_, err := r.svc.GitChanges(r.ctx, 42, "bogus", "")
		assert.Error(t, err)
	})
}

func TestGitChanges_UncommittedScope_SkipsBaselineLookup(t *testing.T) {
	convey.Convey("未提交档不需要基线 → 只发一次 RPC", t, func() {
		r := newRig(t, 7, "/remote/work")
		r.expectCall(wire.MethodGitChanges, wire.GitChangesReq{
			Root: "/remote/work", Scope: wire.ScopeUncommitted,
		}).DoAndReturn(func(_ context.Context, _ string, _ any, out any) error {
			resp := out.(*wire.GitChangesResp)
			resp.Changes = []wire.Change{{Path: "a.go", Status: "modified", Added: 3, Deleted: 1}}
			return nil
		})

		view, err := r.svc.GitChanges(r.ctx, 42, "uncommitted", "")
		require.NoError(t, err)
		assert.Empty(t, view.BaseRef, "未提交档没有基线可言")
		require.Len(t, view.Changes, 1)
		assert.Equal(t, "a.go", view.Changes[0].Path)
		assert.Equal(t, 3, view.Changes[0].Added)
	})
}

func TestGitChanges_BranchScope_BaselineResolution(t *testing.T) {
	branchesResp := func(defaultBaseline string, names ...string) func(context.Context, string, any, any) error {
		return func(_ context.Context, _ string, _ any, out any) error {
			resp := out.(*wire.GitBranchesResp)
			for _, n := range names {
				resp.Branches = append(resp.Branches, wire.Branch{Name: n})
			}
			resp.DefaultBaseline = defaultBaseline
			return nil
		}
	}

	convey.Convey("baseRef 为空 → 用远端推断出的默认基线,并回报实际用的那个", t, func() {
		r := newRig(t, 7, "/remote/work")
		r.expectCall(wire.MethodGitBranches, wire.GitBranchesReq{Root: "/remote/work"}).
			DoAndReturn(branchesResp("origin/main", "main", "origin/main"))
		r.expectCall(wire.MethodGitChanges, wire.GitChangesReq{
			Root: "/remote/work", Scope: wire.ScopeBranch, BaseRef: "origin/main",
		}).Return(nil)

		view, err := r.svc.GitChanges(r.ctx, 42, "branch", "")
		require.NoError(t, err)
		assert.Equal(t, "origin/main", view.BaseRef)
	})

	convey.Convey("用户选过的基线仍在清单里 → 优先于默认推断", t, func() {
		r := newRig(t, 7, "/remote/work")
		r.expectCall(wire.MethodGitBranches, wire.GitBranchesReq{Root: "/remote/work"}).
			DoAndReturn(branchesResp("main", "main", "develop"))
		r.expectCall(wire.MethodGitChanges, wire.GitChangesReq{
			Root: "/remote/work", Scope: wire.ScopeBranch, BaseRef: "develop",
		}).Return(nil)

		view, err := r.svc.GitChanges(r.ctx, 42, "branch", "develop")
		require.NoError(t, err)
		assert.Equal(t, "develop", view.BaseRef)
	})

	convey.Convey("持久化的基线已不存在 → 回落默认推断(设计决策 9)", t, func() {
		r := newRig(t, 7, "/remote/work")
		r.expectCall(wire.MethodGitBranches, wire.GitBranchesReq{Root: "/remote/work"}).
			DoAndReturn(branchesResp("main", "main"))
		r.expectCall(wire.MethodGitChanges, wire.GitChangesReq{
			Root: "/remote/work", Scope: wire.ScopeBranch, BaseRef: "main",
		}).Return(nil)

		view, err := r.svc.GitChanges(r.ctx, 42, "branch", "deleted-branch")
		require.NoError(t, err)
		assert.Equal(t, "main", view.BaseRef, "回落到默认基线而不是拿一个不存在的 ref 去算 merge-base")
	})

	convey.Convey("推断不出基线 → 成功返回空基线的空结果,由前端出 C5 空态", t, func() {
		r := newRig(t, 7, "/remote/work")
		r.expectCall(wire.MethodGitBranches, wire.GitBranchesReq{Root: "/remote/work"}).
			DoAndReturn(branchesResp("", "trunk"))
		// 不再发 gitChanges:空基线送过去只会换回 ErrBaselineRequired。

		view, err := r.svc.GitChanges(r.ctx, 42, "branch", "")
		require.NoError(t, err)
		assert.Empty(t, view.BaseRef)
		assert.Empty(t, view.Changes)
		assert.False(t, view.NotARepo)
	})

	convey.Convey("非 git 仓库 → NotARepo,不再问变动", t, func() {
		r := newRig(t, 7, "/remote/work")
		r.expectCall(wire.MethodGitBranches, wire.GitBranchesReq{Root: "/remote/work"}).
			DoAndReturn(func(_ context.Context, _ string, _ any, out any) error {
				out.(*wire.GitBranchesResp).NotARepo = true
				return nil
			})

		view, err := r.svc.GitChanges(r.ctx, 42, "branch", "")
		require.NoError(t, err)
		assert.True(t, view.NotARepo)
	})
}

func TestGitChanges_LocalBranchScope_EndToEnd(t *testing.T) {
	convey.Convey("本机会话:基线推断与变动计算都走叶子包", t, func() {
		dir := initRepo(t)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644))
		runGit(t, dir, "add", "a.txt")
		runGit(t, dir, "commit", "-q", "-m", "seed")
		runGit(t, dir, "checkout", "-q", "-b", "feature")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\nmore\n"), 0o644))
		r := newRig(t, 0, dir)

		view, err := r.svc.GitChanges(r.ctx, 42, "branch", "")
		require.NoError(t, err)
		assert.Equal(t, "main", view.BaseRef, "本机同样按 origin/HEAD→main→master 推断")
		require.Len(t, view.Changes, 1)
		assert.Equal(t, "a.txt", view.Changes[0].Path)
		assert.Equal(t, "modified", view.Changes[0].Status)
		assert.Equal(t, 1, view.Changes[0].Added)
	})

	convey.Convey("本机会话:未提交档", t, func() {
		dir := initRepo(t)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("a\nb\n"), 0o644))
		r := newRig(t, 0, dir)

		view, err := r.svc.GitChanges(r.ctx, 42, "uncommitted", "")
		require.NoError(t, err)
		require.Len(t, view.Changes, 1)
		assert.Equal(t, "new.txt", view.Changes[0].Path)
		assert.Equal(t, "untracked", view.Changes[0].Status)
		assert.Equal(t, 2, view.Changes[0].Added)
	})
}

// ── 包级注入 ────────────────────────────────────────────────────────────────

func TestRegisterSessionWorkspaceResolver(t *testing.T) {
	convey.Convey("composition root 注入的 resolver 被 Default() 实例用上", t, func() {
		t.Cleanup(func() { RegisterSessionWorkspaceResolver(nil) })
		dir := t.TempDir()
		RegisterSessionWorkspaceResolver(func(_ context.Context, sessionID int64) (int64, string, error) {
			assert.Equal(t, int64(99), sessionID)
			return 0, dir, nil
		})

		view, err := Default().ListDir(context.Background(), 99, "", false)
		require.NoError(t, err)
		assert.Equal(t, dir, view.Path)
	})

	convey.Convey("没有注入 resolver → 报无工作目录,而不是 panic", t, func() {
		t.Cleanup(func() { RegisterSessionWorkspaceResolver(nil) })
		RegisterSessionWorkspaceResolver(nil)
		_, err := Default().ListDir(context.Background(), 99, "", false)
		assert.Error(t, err)
	})
}
