package remote_fs_svc

import (
	"context"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	mockRT "github.com/agentre-ai/agentre/internal/pkg/agentruntime/mock_agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/remotefs/wire"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
	mockRD "github.com/agentre-ai/agentre/internal/service/remote_device_svc/mock_remote_device_svc"
)

func setupSvc(t *testing.T) (
	context.Context,
	*mockRD.MockRemoteDeviceSvc,
	*mockRD.MockConnPool,
	*mockRD.MockLease,
	*mockRT.MockDaemonClientPort,
	*remoteFsImpl,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	rd := mockRD.NewMockRemoteDeviceSvc(ctrl)
	pool := mockRD.NewMockConnPool(ctrl)
	lease := mockRD.NewMockLease(ctrl)
	client := mockRT.NewMockDaemonClientPort(ctrl)
	svc := &remoteFsImpl{rdSvc: rd}
	return context.Background(), rd, pool, lease, client, svc
}

func TestListDir(t *testing.T) {
	convey.Convey("ListDir", t, func() {
		convey.Convey("deviceID 非法", func() {
			_, _, _, _, _, svc := setupSvc(t)
			_, err := svc.ListDir(context.Background(), "abc", "/home")
			assert.Error(t, err)
		})

		convey.Convey("path 落黑名单 → 不 Borrow", func() {
			ctx, _, _, _, _, svc := setupSvc(t)
			_, err := svc.ListDir(ctx, "7", "/proc")
			assert.Error(t, err)
		})

		convey.Convey("Borrow 报 device not found", func() {
			ctx, rd, pool, _, _, svc := setupSvc(t)
			rd.EXPECT().Pool().Return(pool)
			pool.EXPECT().Borrow(ctx, int64(7)).Return(nil, remote_device_svc.ErrDeviceNotFound)
			_, err := svc.ListDir(ctx, "7", "/home/me")
			assert.Error(t, err)
		})

		convey.Convey("Borrow ok + Call wire.ErrPermDenied → RemoteFsPermDenied", func() {
			ctx, rd, pool, lease, client, svc := setupSvc(t)
			rd.EXPECT().Pool().Return(pool)
			pool.EXPECT().Borrow(ctx, int64(7)).Return(lease, nil)
			lease.EXPECT().Client().Return(client)
			lease.EXPECT().Release()
			client.EXPECT().
				Call(ctx, wire.MethodListDir, wire.ListDirReq{Path: "/home/me"}, gomock.Any()).
				Return(&rpc.Error{Code: wire.ErrCodePermDenied, Message: "x"})
			_, err := svc.ListDir(ctx, "7", "/home/me")
			require.Error(t, err)
		})

		convey.Convey("Borrow ok + Call ok → 透传 view", func() {
			ctx, rd, pool, lease, client, svc := setupSvc(t)
			rd.EXPECT().Pool().Return(pool)
			pool.EXPECT().Borrow(ctx, int64(7)).Return(lease, nil)
			lease.EXPECT().Client().Return(client)
			lease.EXPECT().Release()
			client.EXPECT().
				Call(ctx, wire.MethodListDir, wire.ListDirReq{Path: "/home/me"}, gomock.Any()).
				DoAndReturn(func(_ context.Context, _ string, _ any, out any) error {
					resp := out.(*wire.ListDirResp)
					resp.Path = "/home/me"
					resp.Entries = []wire.Entry{
						{Name: "Work", IsDir: true, ModTime: 1700000000},
						{Name: "f.txt", IsDir: false, Size: 12, ModTime: 1700000001},
					}
					resp.Truncated = true
					return nil
				})
			view, err := svc.ListDir(ctx, "7", "/home/me")
			require.NoError(t, err)
			assert.Equal(t, "/home/me", view.Path)
			assert.True(t, view.Truncated)
			require.Len(t, view.Entries, 2)
			assert.Equal(t, "Work", view.Entries[0].Name)
			assert.True(t, view.Entries[0].IsDir)
			assert.Equal(t, int64(12), view.Entries[1].Size)
		})
	})
}

func TestMkdir(t *testing.T) {
	convey.Convey("Mkdir", t, func() {
		convey.Convey("名字非法 → 不 Borrow", func() {
			_, _, _, _, _, svc := setupSvc(t)
			_, err := svc.Mkdir(context.Background(), "7", "/home", "a/b")
			assert.Error(t, err)
		})

		convey.Convey("parent 落黑名单 → 不 Borrow", func() {
			_, _, _, _, _, svc := setupSvc(t)
			_, err := svc.Mkdir(context.Background(), "7", "/proc", "x")
			assert.Error(t, err)
		})

		convey.Convey("Borrow ok + Call ErrMkdirExists", func() {
			ctx, rd, pool, lease, client, svc := setupSvc(t)
			rd.EXPECT().Pool().Return(pool)
			pool.EXPECT().Borrow(ctx, int64(7)).Return(lease, nil)
			lease.EXPECT().Client().Return(client)
			lease.EXPECT().Release()
			client.EXPECT().
				Call(ctx, wire.MethodMkdir, wire.MkdirReq{Parent: "/home/me", Name: "dup"}, gomock.Any()).
				Return(&rpc.Error{Code: wire.ErrCodeMkdirExists, Message: "x"})
			_, err := svc.Mkdir(ctx, "7", "/home/me", "dup")
			assert.Error(t, err)
		})

		convey.Convey("happy", func() {
			ctx, rd, pool, lease, client, svc := setupSvc(t)
			rd.EXPECT().Pool().Return(pool)
			pool.EXPECT().Borrow(ctx, int64(7)).Return(lease, nil)
			lease.EXPECT().Client().Return(client)
			lease.EXPECT().Release()
			client.EXPECT().
				Call(ctx, wire.MethodMkdir, wire.MkdirReq{Parent: "/home/me", Name: "new"}, gomock.Any()).
				DoAndReturn(func(_ context.Context, _ string, _ any, out any) error {
					out.(*wire.MkdirResp).Path = "/home/me/new"
					return nil
				})
			view, err := svc.Mkdir(ctx, "7", "/home/me", "new")
			require.NoError(t, err)
			assert.Equal(t, "/home/me/new", view.Path)
		})
	})
}
