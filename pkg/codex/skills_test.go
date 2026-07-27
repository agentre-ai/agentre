package codex

import (
	"bufio"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientListSkills(t *testing.T) {
	// Given a Codex app-server that exposes enabled and disabled skills for one cwd.
	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)

		req := readRPCReq(t, sc)
		assert.Equal(t, "skills/list", req.Method)
		assert.JSONEq(t, `{"cwds":["/tmp/work"],"forceReload":true}`, string(req.Params))
		respondRPC(h, req, map[string]any{
			"data": []map[string]any{{
				"cwd": "/tmp/work",
				"skills": []map[string]any{
					{"name": "lore:lore-memory", "description": "Recall memory", "path": "/skills/lore/SKILL.md", "scope": "user", "enabled": true},
					{"name": "disabled-skill", "description": "Off", "path": "/skills/off/SKILL.md", "scope": "user", "enabled": false},
				},
				"errors": []any{},
			}},
		})
	}

	client := New(
		WithBinary("codex-test"),
		WithCwd("/tmp/work"),
		WithAppServerRunnerForTesting(runner),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// When skills are listed, Then the wrapper preserves protocol metadata and enabled state.
	skills, err := client.ListSkills(ctx, []string{"/tmp/work"}, true)
	require.NoError(t, err)
	require.Len(t, skills, 2)
	assert.Equal(t, "lore:lore-memory", skills[0].Name)
	assert.Equal(t, "Recall memory", skills[0].Description)
	assert.True(t, skills[0].Enabled)
	assert.Equal(t, "disabled-skill", skills[1].Name)
	assert.False(t, skills[1].Enabled)
	require.Len(t, runner.opts, 1)
	assert.Equal(t, []string{"app-server", "--listen", "stdio://"}, runner.opts[0].Args)
}

func TestPluginEnabledConfig(t *testing.T) {
	// Given sparse plugin overrides, When rendered, Then config keys are deterministic and blank ids are ignored.
	assert.Equal(t, []string{
		`plugins."browser@openai-bundled".enabled=true`,
		`plugins."superpowers@openai-curated".enabled=false`,
	}, PluginEnabledConfig(map[string]bool{
		"superpowers@openai-curated": false,
		"":                           true,
		"browser@openai-bundled":     true,
	}))
	assert.Nil(t, PluginEnabledConfig(nil))
}
