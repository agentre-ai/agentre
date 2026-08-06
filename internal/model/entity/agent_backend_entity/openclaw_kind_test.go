package agent_backend_entity

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validOpenClawBackend() *AgentBackend {
	return &AgentBackend{
		Type:                 string(TypeOpenClaw),
		Name:                 "Local OpenClaw",
		ModelRoutes:          "{}",
		EnvJSON:              "{}",
		OpenClawGatewayURL:   "ws://127.0.0.1:18789",
		OpenClawAgentID:      "main",
		OpenClawDefaultModel: "anthropic/claude-sonnet-4-6",
		OpenClawSessionMode:  OpenClawSessionPerAgentRESession,
	}
}

func TestOpenClawBackendKind(t *testing.T) {
	ctx := context.Background()

	t.Run("Given a loopback ws gateway when the backend is checked then it is valid", func(t *testing.T) {
		assert.NoError(t, validOpenClawBackend().Check(ctx))
	})

	t.Run("Given a remote wss gateway when the backend is checked then it is valid", func(t *testing.T) {
		backend := validOpenClawBackend()
		backend.OpenClawGatewayURL = "wss://gateway.example.com/openclaw"
		assert.NoError(t, backend.Check(ctx))
	})

	for _, gatewayURL := range []string{
		"http://127.0.0.1:18789",
		"ws://192.168.1.10:18789",
		"ws://gateway.example.com:18789",
		"wss://token@gateway.example.com/openclaw",
		"wss://gateway.example.com/openclaw?token=secret",
		"wss:///missing-host",
	} {
		t.Run("Given an unsafe or invalid gateway URL when checked then it is rejected: "+gatewayURL, func(t *testing.T) {
			backend := validOpenClawBackend()
			backend.OpenClawGatewayURL = gatewayURL
			assert.Error(t, backend.Check(ctx))
		})
	}

	t.Run("Given an unknown session mode when checked then it is rejected", func(t *testing.T) {
		backend := validOpenClawBackend()
		backend.OpenClawSessionMode = "shared"
		assert.Error(t, backend.Check(ctx))
	})

	mutuallyExclusive := []struct {
		name  string
		apply func(*AgentBackend)
	}{
		{name: "llm provider", apply: func(b *AgentBackend) { b.LLMProviderKey = "provider" }},
		{name: "cli path", apply: func(b *AgentBackend) { b.CLIPath = "/usr/bin/openclaw" }},
		{name: "model routes", apply: func(b *AgentBackend) { b.ModelRoutes = `{"OPUS":"provider"}` }},
		{name: "sandbox", apply: func(b *AgentBackend) { b.Sandbox = "workspace-write" }},
		{name: "approval", apply: func(b *AgentBackend) { b.Approval = "on-request" }},
		{name: "environment", apply: func(b *AgentBackend) { b.EnvJSON = `{"TOKEN":"secret"}` }},
		{name: "reasoning effort", apply: func(b *AgentBackend) { b.ReasoningEffort = "high" }},
		{name: "permission mode", apply: func(b *AgentBackend) { b.DefaultPermissionMode = "default" }},
		{name: "legacy default model", apply: func(b *AgentBackend) { b.DefaultModel = "wrong-field" }},
	}
	for _, tc := range mutuallyExclusive {
		t.Run("Given OpenClaw with mutually exclusive "+tc.name+" when checked then it is rejected", func(t *testing.T) {
			backend := validOpenClawBackend()
			tc.apply(backend)
			assert.Error(t, backend.Check(ctx))
		})
	}

	t.Run("Given an OpenClaw backend when marshaled then no secret field can leak", func(t *testing.T) {
		typ := reflect.TypeOf(AgentBackend{})
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name := strings.ToLower(field.Name + " " + field.Tag.Get("json") + " " + field.Tag.Get("gorm"))
			require.NotContains(t, name, "token", "token material must not be modeled in AgentBackend")
			require.NotContains(t, name, "secret", "secret material must not be modeled in AgentBackend")
		}
		raw, err := json.Marshal(validOpenClawBackend())
		require.NoError(t, err)
		assert.NotContains(t, strings.ToLower(string(raw)), "token")
		assert.NotContains(t, strings.ToLower(string(raw)), "secret")
	})
}

func TestNormalizeOpenClawGatewayURL(t *testing.T) {
	t.Run("Given surrounding whitespace and an uppercase host when normalized then the stable safe URL is returned", func(t *testing.T) {
		got, err := NormalizeOpenClawGatewayURL("  ws://LOCALHOST:18789/  ")
		require.NoError(t, err)
		assert.Equal(t, "ws://localhost:18789/", got)
	})
}
