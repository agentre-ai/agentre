package wire_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/pkg/remotefs/wire"
)

func TestSentinelRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
	}{
		{"PathRefused", wire.ErrPathRefused, wire.ErrCodePathRefused},
		{"PermDenied", wire.ErrPermDenied, wire.ErrCodePermDenied},
		{"NotFound", wire.ErrNotFound, wire.ErrCodeNotFound},
		{"NotDir", wire.ErrNotDir, wire.ErrCodeNotDir},
		{"MkdirExists", wire.ErrMkdirExists, wire.ErrCodeMkdirExists},
		{"InvalidName", wire.ErrInvalidName, wire.ErrCodeInvalidName},
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
