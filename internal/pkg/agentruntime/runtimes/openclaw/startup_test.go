package openclaw

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
)

// startupReadRequest 与 runtimeReadRequest 的区别:测试结束连接被关闭时安静退出,
// 而不是在非测试 goroutine 里触发 require 断言 panic。
func startupReadRequest(conn *websocket.Conn) (runtimeRequestFrame, bool) {
	var request runtimeRequestFrame
	if err := conn.ReadJSON(&request); err != nil {
		return runtimeRequestFrame{}, false
	}
	return request, true
}

func startupServe(conn *websocket.Conn, reply func(runtimeRequestFrame) map[string]any) {
	for {
		req, ok := startupReadRequest(conn)
		if !ok {
			return
		}
		if payload := reply(req); payload != nil {
			if err := conn.WriteJSON(payload); err != nil {
				return
			}
		}
	}
}

func startupBackend() *agent_backend_entity.AgentBackend {
	return &agent_backend_entity.AgentBackend{
		ID:                  7,
		Type:                string(agent_backend_entity.TypeOpenClaw),
		OpenClawSessionMode: agent_backend_entity.OpenClawSessionPerAgentRESession,
	}
}

// 开轮前的 exec.approval.list 曾经是致命的:真实网关上这个 RPC 只对创建审批的连接/
// 管理员返回内容,报错属常态;而一旦报错整轮就以「故障」收场。开轮不该被它打断 ——
// 待审批本来也会通过 exec.approval.requested 事件到达。
func TestRun_ApprovalListErrorDoesNotAbortTurn(t *testing.T) {
	gatewayURL := runtimeGateway(t, func(conn *websocket.Conn, connection int) {
		runtimeHandshake(t, conn, connection)
		startupServe(conn, func(req runtimeRequestFrame) map[string]any {
			switch req.Method {
			case "exec.approval.list":
				return map[string]any{
					"type": "res", "id": req.ID, "ok": false,
					"error": map[string]any{"code": "FORBIDDEN", "message": "missing scope: operator.admin"},
				}
			case "agent":
				return map[string]any{
					"type": "res", "id": req.ID, "ok": true,
					"payload": map[string]any{"runId": "run-1", "status": "accepted", "sessionKey": "agent:main:agentre:7:1"},
				}
			default:
				return map[string]any{"type": "res", "id": req.ID, "ok": true, "payload": map[string]any{}}
			}
		})
	})

	rt := New(runtimeResolver(t, gatewayURL))
	events, result, err := rt.Run(context.Background(), agentruntime.RunRequest{
		SessionID: 1, Backend: startupBackend(), UserText: "hello",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, events)
	assert.Equal(t, "agent:main:agentre:7:1", result.ProviderSessionID)
}

// 用户在握手/开轮 RPC 还没回来时点停止:runtime 必须回 ErrAborted,让 chat_svc 走
// 中止收敛(idle)而不是错误收敛 —— 后者会把会话永久卡在 running。
func TestRun_CancelDuringStartupReturnsErrAborted(t *testing.T) {
	gatewayURL := runtimeGateway(t, func(conn *websocket.Conn, connection int) {
		runtimeHandshake(t, conn, connection)
		// 收到请求后故意不回应,模拟"网关慢"的开轮窗口。
		startupServe(conn, func(runtimeRequestFrame) map[string]any { return nil })
	})

	rt := New(runtimeResolver(t, gatewayURL))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	_, _, err := rt.Run(ctx, agentruntime.RunRequest{
		SessionID: 2, Backend: startupBackend(), UserText: "hello",
	})

	assert.True(t, errors.Is(err, agentruntime.ErrAborted), "want ErrAborted, got %v", err)
}
