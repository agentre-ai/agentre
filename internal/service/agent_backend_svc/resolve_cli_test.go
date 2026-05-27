package agent_backend_svc

import (
	"context"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
)

// TestResolveCLIPath_LocalDispatch 覆盖本地分支（DeviceID 空）：
//   - claudecode / codex / 含空白 / builtin / 未知类型 / nil 请求；
//   - 真正的 PATH 扫描细节归 cliprober 包测试，这里只验 svc 层的派发。
func TestResolveCLIPath_LocalDispatch(t *testing.T) {
	convey.Convey("ResolveCLIPath 本地分支（DeviceID 空）", t, func() {
		svc := &agentBackendSvc{}
		ctx := context.Background()

		convey.Convey("claudecode：err == nil（具体 Found/Path 取决于宿主机 PATH）", func() {
			resp, err := svc.ResolveCLIPath(ctx, &ResolveCLIPathRequest{Type: "claudecode"})
			assert.NoError(t, err)
			assert.NotNil(t, resp)
		})

		convey.Convey("codex：err == nil", func() {
			resp, err := svc.ResolveCLIPath(ctx, &ResolveCLIPathRequest{Type: "codex"})
			assert.NoError(t, err)
			assert.NotNil(t, resp)
		})

		convey.Convey("Type 含两侧空白：TrimSpace 后仍能识别", func() {
			resp, err := svc.ResolveCLIPath(ctx, &ResolveCLIPathRequest{Type: "  claudecode  "})
			assert.NoError(t, err)
			assert.NotNil(t, resp)
		})

		convey.Convey("非 CLI 类型 builtin → AgentBackendInvalidType", func() {
			_, err := svc.ResolveCLIPath(ctx, &ResolveCLIPathRequest{Type: "builtin"})
			assert.Error(t, err)
		})

		convey.Convey("未知类型 → AgentBackendInvalidType", func() {
			_, err := svc.ResolveCLIPath(ctx, &ResolveCLIPathRequest{Type: "wat"})
			assert.Error(t, err)
		})

		convey.Convey("nil 请求 → InvalidParameter", func() {
			_, err := svc.ResolveCLIPath(ctx, nil)
			assert.Error(t, err)
		})
	})
}

// TestParseRemoteDeviceID 单独覆盖 parser 的三种返回。
func TestParseRemoteDeviceID(t *testing.T) {
	convey.Convey("parseRemoteDeviceID", t, func() {
		convey.Convey("空串 → (0, false, nil)", func() {
			id, ok, err := parseRemoteDeviceID("")
			assert.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, int64(0), id)
		})

		convey.Convey("合法数字 → (n, true, nil)", func() {
			id, ok, err := parseRemoteDeviceID("42")
			assert.NoError(t, err)
			assert.True(t, ok)
			assert.Equal(t, int64(42), id)
		})

		convey.Convey("非法字符串 → (0, false, err)", func() {
			_, ok, err := parseRemoteDeviceID("not-a-number")
			assert.Error(t, err)
			assert.False(t, ok)
		})
	})
}
