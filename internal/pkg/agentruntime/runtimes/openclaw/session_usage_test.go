package openclaw

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
)

// realSessionDescribePayload 复刻真实网关(OpenClaw 2026.7.1-2)对
// sessions.describe 的应答:流式帧里既没有 usage 也没有 model,但会话记录里两者都有。
func realSessionDescribePayload() map[string]any {
	return map[string]any{
		"session": map[string]any{
			"key":              "agent:main:agentre:12:70",
			"inputTokens":      17399,
			"outputTokens":     259,
			"totalTokens":      27127,
			"totalTokensFresh": true,
			"status":           "done",
			"modelProvider":    "huu",
			"model":            "gpt-5.6-sol",
			"contextTokens":    253000,
			"hasActiveRun":     false,
		},
	}
}

// 真实网关一整轮下来不发任何 usage 帧,助手消息因此永远是「模型空 + ↑0 ↓0」。
// 网关自己按会话记着这两样,收轮时补一次 sessions.describe 就能如实展示。
func TestRunPublishesSessionUsageWhenGatewayOmitsUsageFrames(t *testing.T) {
	t.Run("Given a Gateway that reports usage only through sessions.describe, when the turn ends, then the recorded tokens and model reach AgentRE", func(t *testing.T) {
		gatewayURL := runtimeGateway(t, func(conn *websocket.Conn, connection int) {
			runtimeHandshakeWithMethods(t, conn, connection, "sessions.describe")
			request := runtimeReadAgentRequest(t, conn)
			var params runtimeAgentParams
			require.NoError(t, json.Unmarshal(request.Params, &params))
			runtimeWrite(t, conn, map[string]any{
				"type": "res", "id": request.ID, "ok": true,
				"payload": map[string]any{"runId": params.IdempotencyKey, "status": "accepted"},
			})
			runtimeWrite(t, conn, map[string]any{
				"type": "event", "event": "agent", "seq": 2,
				"payload": map[string]any{
					"runId": params.IdempotencyKey, "sessionKey": params.SessionKey,
					"seq": 1, "stream": "assistant", "ts": 1,
					"data": map[string]any{"text": "391", "delta": "391"},
				},
			})
			runtimeWrite(t, conn, map[string]any{
				"type": "event", "event": "agent", "seq": 3,
				"payload": map[string]any{
					"runId": params.IdempotencyKey, "sessionKey": params.SessionKey,
					"seq": 2, "stream": "lifecycle", "ts": 2,
					"data": map[string]any{"phase": "end"},
				},
			})
			startupServe(conn, func(req runtimeRequestFrame) map[string]any {
				if req.Method != "sessions.describe" {
					return map[string]any{"type": "res", "id": req.ID, "ok": true, "payload": map[string]any{}}
				}
				var describeParams struct {
					Key string `json:"key"`
				}
				require.NoError(t, json.Unmarshal(req.Params, &describeParams))
				assert.Equal(t, "agentre:12:70", describeParams.Key)
				return map[string]any{
					"type": "res", "id": req.ID, "ok": true,
					"payload": realSessionDescribePayload(),
				}
			})
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		runtime := New(runtimeResolver(t, gatewayURL))
		events, result, err := runtime.Run(ctx, agentruntime.RunRequest{
			Backend: runtimeBackend(), SessionID: 70, UserText: "how many tokens?",
		})
		require.NoError(t, err)
		collected := collectRuntimeEvents(t, events)

		var usage *agentruntime.UsageUpdate
		for _, event := range collected {
			if update, ok := event.(agentruntime.UsageUpdate); ok {
				usage = &update
			}
		}
		require.NotNil(t, usage, "want a usage event derived from sessions.describe, got %#v", collected)
		require.NotNil(t, usage.Usage)
		assert.Equal(t, 17399, usage.Usage.PromptTokens)
		assert.Equal(t, 259, usage.Usage.CompletionTokens)
		assert.Equal(t, 27127, usage.Usage.TotalTokens)
		assert.Equal(t, 17399, usage.TotalInputTokens)
		assert.Equal(t, "huu/gpt-5.6-sol", result.Model)
		require.NotNil(t, result.Usage)
		assert.Equal(t, 259, result.Usage.CompletionTokens)
	})
}

// sessions.describe 不属于 requiredRuntimeMethods:网关没广播就不能调,更不能因此
// 影响收轮。
func TestRunSkipsSessionUsageWhenGatewayDoesNotAdvertiseIt(t *testing.T) {
	t.Run("Given a Gateway without sessions.describe, when the turn ends, then no describe RPC is issued and the turn still completes", func(t *testing.T) {
		methods := make(chan string, 8)
		gatewayURL := runtimeGateway(t, func(conn *websocket.Conn, connection int) {
			runtimeHandshake(t, conn, connection)
			request := runtimeReadAgentRequest(t, conn)
			var params runtimeAgentParams
			require.NoError(t, json.Unmarshal(request.Params, &params))
			runtimeWrite(t, conn, map[string]any{
				"type": "res", "id": request.ID, "ok": true,
				"payload": map[string]any{"runId": params.IdempotencyKey, "status": "accepted"},
			})
			runtimeWrite(t, conn, map[string]any{
				"type": "event", "event": "agent", "seq": 2,
				"payload": map[string]any{
					"runId": params.IdempotencyKey, "sessionKey": params.SessionKey,
					"seq": 1, "stream": "lifecycle", "ts": 1,
					"data": map[string]any{"phase": "end"},
				},
			})
			startupServe(conn, func(req runtimeRequestFrame) map[string]any {
				methods <- req.Method
				return map[string]any{"type": "res", "id": req.ID, "ok": true, "payload": map[string]any{}}
			})
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		runtime := New(runtimeResolver(t, gatewayURL))
		events, result, err := runtime.Run(ctx, agentruntime.RunRequest{
			Backend: runtimeBackend(), SessionID: 71, UserText: "no describe here",
		})
		require.NoError(t, err)
		collected := collectRuntimeEvents(t, events)

		close(methods)
		for method := range methods {
			assert.NotEqual(t, "sessions.describe", method)
		}
		assert.Empty(t, result.Model)
		require.NotEmpty(t, collected)
		_, done := collected[len(collected)-1].(agentruntime.Done)
		assert.True(t, done, "want the turn to complete, got %#v", collected[len(collected)-1])
	})
}

// 补 usage 是尽力而为:describe 报错只是少了几个数字,不该把整轮变成失败。
func TestRunKeepsTurnHealthyWhenSessionDescribeFails(t *testing.T) {
	t.Run("Given sessions.describe fails, when the turn ends, then the turn completes without an error event", func(t *testing.T) {
		gatewayURL := runtimeGateway(t, func(conn *websocket.Conn, connection int) {
			runtimeHandshakeWithMethods(t, conn, connection, "sessions.describe")
			request := runtimeReadAgentRequest(t, conn)
			var params runtimeAgentParams
			require.NoError(t, json.Unmarshal(request.Params, &params))
			runtimeWrite(t, conn, map[string]any{
				"type": "res", "id": request.ID, "ok": true,
				"payload": map[string]any{"runId": params.IdempotencyKey, "status": "accepted"},
			})
			runtimeWrite(t, conn, map[string]any{
				"type": "event", "event": "agent", "seq": 2,
				"payload": map[string]any{
					"runId": params.IdempotencyKey, "sessionKey": params.SessionKey,
					"seq": 1, "stream": "lifecycle", "ts": 1,
					"data": map[string]any{"phase": "end"},
				},
			})
			startupServe(conn, func(req runtimeRequestFrame) map[string]any {
				if req.Method == "sessions.describe" {
					return map[string]any{
						"type": "res", "id": req.ID, "ok": false,
						"error": map[string]any{"code": "FORBIDDEN", "message": "missing scope: operator.read"},
					}
				}
				return map[string]any{"type": "res", "id": req.ID, "ok": true, "payload": map[string]any{}}
			})
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		runtime := New(runtimeResolver(t, gatewayURL))
		events, result, err := runtime.Run(ctx, agentruntime.RunRequest{
			Backend: runtimeBackend(), SessionID: 72, UserText: "describe fails",
		})
		require.NoError(t, err)
		collected := collectRuntimeEvents(t, events)

		for _, event := range collected {
			_, isError := event.(agentruntime.ErrorEvent)
			assert.False(t, isError, "usage backfill must not fail the turn: %#v", event)
		}
		assert.Nil(t, result.Usage)
		require.NotEmpty(t, collected)
		_, done := collected[len(collected)-1].(agentruntime.Done)
		assert.True(t, done, "want the turn to complete, got %#v", collected[len(collected)-1])
	})
}
