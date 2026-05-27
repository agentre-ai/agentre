package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_DispatchHit(t *testing.T) {
	r := NewRegistry()
	r.Register("echo", func(ctx context.Context, params json.RawMessage) (any, error) {
		return map[string]string{"echo": string(params)}, nil
	})

	res, err := r.Dispatch(context.Background(), "echo", json.RawMessage(`"hi"`))
	require.NoError(t, err)
	m, _ := res.(map[string]string)
	assert.Equal(t, `"hi"`, m["echo"])
}

func TestRegistry_DispatchMiss(t *testing.T) {
	r := NewRegistry()
	_, err := r.Dispatch(context.Background(), "missing", nil)
	var rpcErr *Error
	require.True(t, errors.As(err, &rpcErr))
	assert.Equal(t, -32601, rpcErr.Code)
}

func TestRegistry_HandlerErrorPropagates(t *testing.T) {
	r := NewRegistry()
	r.Register("boom", func(ctx context.Context, params json.RawMessage) (any, error) {
		return nil, &Error{Code: -32003, Message: "no llm"}
	})
	_, err := r.Dispatch(context.Background(), "boom", nil)
	var rpcErr *Error
	require.True(t, errors.As(err, &rpcErr))
	assert.Equal(t, -32003, rpcErr.Code)
}
