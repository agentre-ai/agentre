package wire_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
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
func TestErrorCodes_DoNotOverlapRemotefs(t *testing.T) {
	remotefsCodes := map[int]bool{
		-32030: true, -32031: true, -32032: true, -32033: true, -32034: true, -32035: true,
	}
	for _, code := range []int{wire.ErrCodePathRefused, wire.ErrCodeBaselineRequired} {
		assert.Falsef(t, remotefsCodes[code], "workspacefs code %d overlaps remotefs.* wire code", code)
	}
}
