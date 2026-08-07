package wire_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	remotefswire "github.com/agentre-ai/agentre/internal/pkg/remotefs/wire"
	"github.com/agentre-ai/agentre/internal/pkg/workspacefs/wire"
)

func TestSentinelRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
	}{
		{"PathRefused", wire.ErrPathRefused, wire.ErrCodePathRefused},
		{"BaselineRequired", wire.ErrBaselineRequired, wire.ErrCodeBaselineRequired},
		{"NoCwd", wire.ErrNoCwd, wire.ErrCodeNoCwd},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rpcErr := wire.ToJSONRPCError(c.err)
			assert.NotNil(t, rpcErr)
			assert.Equal(t, c.code, rpcErr.Code)

			back := wire.FromJSONRPCError(rpcErr)
			assert.True(t, errors.Is(back, c.err))
		})
	}
}

func TestToJSONRPCError_NonSentinel(t *testing.T) {
	assert.Nil(t, wire.ToJSONRPCError(errors.New("random")))
}

func TestFromJSONRPCError_UnknownCode(t *testing.T) {
	src := &rpc.Error{Code: -9999, Message: "x"}
	got := wire.FromJSONRPCError(src)
	assert.Equal(t, src, got)
}

// TestErrorCodes_DoNotOverlapRemotefs 锁死设计决策 5 的一个具体后果:
// workspacefs.* 与 remotefs.* 是独立方法族,各自的 error code 段不能重叠——
// 重叠会让同一个 JSON-RPC error code 在两个协议里代表不同语义,客户端按
// FromJSONRPCError rehydrate 时就会翻错 sentinel。
//
// 判定方式是把 workspacefs 的 code 真喂给 remotefs 的翻译器,而不是照抄一份
// remotefs code 常量表来比对:抄下来的表不会随 remotefs 新增 code 更新,那样
// 这条守卫就只在写它的那天成立。remotefs 认不出来时按约定原样返回,拿到的还是
// 同一个 *rpc.Error;一旦翻出了别的 sentinel,就是撞号了。
func TestErrorCodes_DoNotOverlapRemotefs(t *testing.T) {
	for _, code := range []int{wire.ErrCodePathRefused, wire.ErrCodeBaselineRequired, wire.ErrCodeNoCwd} {
		src := &rpc.Error{Code: code, Message: "x"}
		assert.Samef(t, src, remotefswire.FromJSONRPCError(src),
			"workspacefs code %d 被 remotefs.* 翻成了它自己的 sentinel,两个方法族撞号", code)
	}
	// 反向同理:remotefs 的 code 不能被 workspacefs 认领。
	for _, code := range []int{
		remotefswire.ErrCodePathRefused, remotefswire.ErrCodePermDenied,
		remotefswire.ErrCodeNotFound, remotefswire.ErrCodeNotDir,
		remotefswire.ErrCodeMkdirExists, remotefswire.ErrCodeInvalidName,
	} {
		src := &rpc.Error{Code: code, Message: "x"}
		assert.Samef(t, src, wire.FromJSONRPCError(src),
			"remotefs code %d 被 workspacefs.* 翻成了它自己的 sentinel,两个方法族撞号", code)
	}
}
