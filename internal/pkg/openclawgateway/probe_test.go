package openclawgateway

import (
	"context"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProbeGateway(t *testing.T, methods []string, agents, models any) string {
	t.Helper()
	return newTestGateway(t, func(conn *websocket.Conn, _ int) {
		writeChallenge(t, conn, "probe-nonce")
		connect := readTestRequest(t, conn)
		hello := helloPayload(ProtocolVersion, RequiredOperatorScopes, "probe-conn")
		hello["features"].(map[string]any)["methods"] = methods
		writeTestJSON(t, conn, map[string]any{
			"type": "res", "id": connect.ID, "ok": true, "payload": hello,
		})
		for i := 0; i < 2; i++ {
			var request testRequestFrame
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			switch request.Method {
			case "agents.list":
				writeTestJSON(t, conn, map[string]any{"type": "res", "id": request.ID, "ok": true, "payload": agents})
			case "models.list":
				writeTestJSON(t, conn, map[string]any{"type": "res", "id": request.ID, "ok": true, "payload": models})
			default:
				writeTestJSON(t, conn, map[string]any{
					"type": "res", "id": request.ID, "ok": false,
					"error": map[string]any{"code": "METHOD_NOT_FOUND", "message": "unexpected method"},
				})
			}
		}
	})
}

func probeAgentsFixture() map[string]any {
	return map[string]any{
		"defaultId": "main", "mainKey": "main", "scope": "per-sender",
		"agents": []any{
			map[string]any{
				"id": "main", "name": "Main Agent",
				"model": map[string]any{"primary": "anthropic/claude-sonnet-4-6", "fallbacks": []string{"openai/gpt-5.4"}},
			},
			map[string]any{"id": "reviewer", "name": "Reviewer"},
		},
	}
}

func probeModelsFixture() map[string]any {
	return map[string]any{
		"models": []any{
			map[string]any{"id": "claude-sonnet-4-6", "name": "Claude Sonnet 4.6", "provider": "anthropic", "available": true},
			map[string]any{"id": "gpt-5.4", "name": "GPT-5.4", "provider": "openai", "available": false},
		},
	}
}

func TestProbeDiscoversGatewayAgentsAndModels(t *testing.T) {
	gatewayURL := newProbeGateway(t,
		[]string{"agent", "agent.wait", "chat.abort", "agents.list", "models.list", "exec.approval.list", "exec.approval.resolve"},
		probeAgentsFixture(), probeModelsFixture(),
	)
	result, err := Probe(context.Background(), Config{
		URL: gatewayURL, Identity: testIdentity(t), Platform: "linux",
	}, ProbeSelection{AgentID: "main", Model: "anthropic/claude-sonnet-4-6"})
	require.NoError(t, err)
	assert.Equal(t, "2026.7.1-2", result.GatewayVersion)
	assert.Equal(t, ProtocolVersion, result.Protocol)
	assert.Equal(t, RequiredOperatorScopes, result.GrantedScopes)
	require.Len(t, result.Agents, 2)
	assert.Equal(t, AgentSummary{
		ID: "main", Name: "Main Agent", PrimaryModel: "anthropic/claude-sonnet-4-6",
		Fallbacks: []string{"openai/gpt-5.4"}, Default: true,
	}, result.Agents[0])
	require.Len(t, result.Models, 2)
	assert.Equal(t, ModelSummary{
		ID: "anthropic/claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Provider: "anthropic", Available: true,
	}, result.Models[0])
	assert.Contains(t, result.Methods, "agent.wait")
	assert.Contains(t, result.Events, "exec.approval.requested")
}

func TestProbeRejectsMissingSelectionsAndMethods(t *testing.T) {
	tests := []struct {
		name      string
		methods   []string
		selection ProbeSelection
		want      error
	}{
		{
			name:      "Given a selected agent that does not exist when probing then it is rejected",
			methods:   []string{"agent", "agent.wait", "chat.abort", "agents.list", "models.list", "exec.approval.list", "exec.approval.resolve"},
			selection: ProbeSelection{AgentID: "missing"}, want: ErrSelectedAgentNotFound,
		},
		{
			name:      "Given a selected model that does not exist when probing then it is rejected",
			methods:   []string{"agent", "agent.wait", "chat.abort", "agents.list", "models.list", "exec.approval.list", "exec.approval.resolve"},
			selection: ProbeSelection{Model: "provider/missing"}, want: ErrSelectedModelNotFound,
		},
		{
			name:    "Given an approval RPC is not advertised when probing then the capability contract is rejected",
			methods: []string{"agent", "agent.wait", "chat.abort", "agents.list", "models.list", "exec.approval.resolve"},
			want:    ErrRequiredMethodMissing,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gatewayURL := newProbeGateway(t, tc.methods, probeAgentsFixture(), probeModelsFixture())
			_, err := Probe(context.Background(), Config{
				URL: gatewayURL, Identity: testIdentity(t), Platform: "linux",
			}, tc.selection)
			assert.ErrorIs(t, err, tc.want)
		})
	}
}
