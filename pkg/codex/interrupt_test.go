package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStream_InterruptForwardsRPC 走一个完整 turn：start → interrupt → completed
// {status=interrupted}。验证：
//   - Interrupt 发出了 turn/interrupt RPC，参数 threadId/turnId 正确
//   - 服务端回 ack 后，发 turn/completed{status=interrupted}
//   - drain 看到 turn/completed 自然返回（不 emit error，emit Done）
func TestStream_InterruptForwardsRPC(t *testing.T) {
	interruptCaptured := make(chan json.RawMessage, 1)

	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondRPC(h, readRPCReq(t, sc), map[string]any{}) // initialize
		_ = readRPCReq(t, sc)                              // initialized notification
		respondRPC(h, readRPCReq(t, sc), map[string]any{"thread": map[string]any{"id": "thr-1"}})
		respondRPC(h, readRPCReq(t, sc), map[string]any{"turn": map[string]any{"id": "turn-1", "status": "inProgress"}})

		// 等 turn/interrupt
		interruptReq := readRPCReq(t, sc)
		assert.Equal(t, "turn/interrupt", interruptReq.Method)
		interruptCaptured <- interruptReq.Params
		respondRPC(h, interruptReq, map[string]any{})

		// 服务端发 turn/completed{status=interrupted}
		h.send(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thr-1",
				"turnId":   "turn-1",
				"turn":     map[string]any{"id": "turn-1", "status": "interrupted"},
			},
		})
	}

	client := New(WithAppServerRunnerForTesting(runner))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := client.Stream(ctx, "long-job")
	require.NoError(t, err)

	require.NoError(t, stream.Interrupt(ctx))

	select {
	case params := <-interruptCaptured:
		var got map[string]any
		require.NoError(t, json.Unmarshal(params, &got))
		assert.Equal(t, "thr-1", got["threadId"])
		assert.Equal(t, "turn-1", got["turnId"])
	case <-time.After(2 * time.Second):
		t.Fatalf("turn/interrupt never captured")
	}

	// drain 自然退出且不 emit error
	sawError := false
	for stream.Next() {
		if ev := stream.Event(); ev.Kind == EventError {
			sawError = true
		}
	}
	assert.False(t, sawError, "interrupted turn should not emit EventError")
	require.NoError(t, stream.Close(ctx))
}

// TestStream_InterruptAfterTurnCompletedReturnsNoActive 验证：turn 已结束后调
// Interrupt 立即返 ErrNoActiveTurn（不发 RPC、不 panic）。
func TestStream_InterruptAfterTurnCompletedReturnsNoActive(t *testing.T) {
	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondRPC(h, readRPCReq(t, sc), map[string]any{})
		_ = readRPCReq(t, sc)
		respondRPC(h, readRPCReq(t, sc), map[string]any{"thread": map[string]any{"id": "thr-1"}})
		respondRPC(h, readRPCReq(t, sc), map[string]any{"turn": map[string]any{"id": "turn-1", "status": "inProgress"}})
		h.send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thr-1", "turnId": "turn-1", "turn": map[string]any{"id": "turn-1", "status": "completed"}}})
	}

	client := New(WithAppServerRunnerForTesting(runner))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := client.Stream(ctx, "hi")
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Close(ctx))

	err = stream.Interrupt(ctx)
	require.ErrorIs(t, err, ErrNoActiveTurn)
}
