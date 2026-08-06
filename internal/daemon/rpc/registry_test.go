package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
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

func TestRegistry_GivenClone_WhenEitherRegistryChanges_ThenHandlerSnapshotsStayIndependent(t *testing.T) {
	bootstrap := NewRegistry()
	bootstrap.Register("static", func(context.Context, json.RawMessage) (any, error) {
		return "bootstrap", nil
	})

	clone := bootstrap.Clone()
	clone.Register("connection.only", func(context.Context, json.RawMessage) (any, error) {
		return "private", nil
	})

	got, err := clone.Dispatch(context.Background(), "static", nil)
	require.NoError(t, err)
	assert.Equal(t, "bootstrap", got)
	_, err = bootstrap.Dispatch(context.Background(), "connection.only", nil)
	require.ErrorIs(t, err, ErrMethodNotFound)

	bootstrap.Register("static", func(context.Context, json.RawMessage) (any, error) {
		return "bootstrap-updated", nil
	})
	got, err = clone.Dispatch(context.Background(), "static", nil)
	require.NoError(t, err)
	assert.Equal(t, "bootstrap", got, "a clone must retain the handler snapshot taken at clone time")

	clone.Register("static", func(context.Context, json.RawMessage) (any, error) {
		return "private-updated", nil
	})
	got, err = bootstrap.Dispatch(context.Background(), "static", nil)
	require.NoError(t, err)
	assert.Equal(t, "bootstrap-updated", got)
}

func TestRegistry_GivenConcurrentRegistration_WhenCloned_ThenSnapshotsAreRaceSafe(t *testing.T) {
	bootstrap := NewRegistry()
	bootstrap.Register("static", func(context.Context, json.RawMessage) (any, error) {
		return "available", nil
	})

	const iterations = 100
	start := make(chan struct{})
	snapshots := make(chan *Registry, iterations)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			bootstrap.Register(fmt.Sprintf("dynamic.%d", i), func(context.Context, json.RawMessage) (any, error) {
				return nil, nil
			})
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			snapshots <- bootstrap.Clone()
		}
	}()
	close(start)
	wg.Wait()
	close(snapshots)

	for snapshot := range snapshots {
		got, err := snapshot.Dispatch(context.Background(), "static", nil)
		require.NoError(t, err)
		assert.Equal(t, "available", got)
	}
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
