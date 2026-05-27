package httpgateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"agentre/internal/model/entity/agent_backend_entity"
)

func TestTokenRegistry_IssueResolveRevoke(t *testing.T) {
	r := NewTokenRegistry()

	b := &agent_backend_entity.AgentBackend{
		ID:             7,
		Type:           string(agent_backend_entity.TypeClaudeCode),
		LLMProviderKey: "key-3",
		ModelRoutes:    `{"OPUS":"key-5","sonnet":"key-6"}`,
	}
	tok, err := r.Issue(b, 60*time.Second)
	assert.NoError(t, err)
	assert.NotEmpty(t, tok)
	assert.Equal(t, 1, r.Size())

	got, ok := r.Resolve(tok)
	if assert.True(t, ok) {
		assert.Equal(t, int64(7), got.BackendID)
		assert.Equal(t, "key-3", got.MainProviderKey)
		assert.Equal(t, agent_backend_entity.TypeClaudeCode, got.BackendType)
		// alias 已规范成大写
		assert.Equal(t, "key-5", got.Routes["OPUS"])
		assert.Equal(t, "key-6", got.Routes["SONNET"])
	}

	r.Revoke(tok)
	_, ok = r.Resolve(tok)
	assert.False(t, ok)
	assert.Equal(t, 0, r.Size())
}

func TestTokenRegistry_RejectInvalidBackend(t *testing.T) {
	r := NewTokenRegistry()
	cases := []struct {
		name string
		b    *agent_backend_entity.AgentBackend
	}{
		{"nil", nil},
		{"malformed routes", &agent_backend_entity.AgentBackend{ID: 1, LLMProviderKey: "key-2", ModelRoutes: `{not json`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.Issue(tc.b, time.Minute)
			assert.Error(t, err)
		})
	}
}

// TestTokenRegistry_IssueWithoutProvider 守护 CLI 登录模式（backend 没绑 LLM
// provider）：token 仍能发出来，给 PostToolUse hook 子进程访问 /hook/v1/inbox
// 用。LLM 转发端点会因 ResolveModel→"" 自然失败（gateway handle 里 lookup
// provider 返回 nil → 502），不会被误用作 LLM bypass。
func TestTokenRegistry_IssueWithoutProvider(t *testing.T) {
	r := NewTokenRegistry()
	tok, err := r.Issue(&agent_backend_entity.AgentBackend{
		ID:             42,
		Type:           string(agent_backend_entity.TypeClaudeCode),
		LLMProviderKey: "",
	}, time.Minute)
	assert.NoError(t, err)
	assert.NotEmpty(t, tok)

	entry, ok := r.Resolve(tok)
	if assert.True(t, ok) {
		assert.Equal(t, int64(42), entry.BackendID)
		assert.Equal(t, "", entry.MainProviderKey, "hook-only token: provider key is empty")
		assert.Empty(t, entry.Routes)
	}
}

func TestTokenRegistry_ExpireOnResolve(t *testing.T) {
	r := NewTokenRegistry()
	frozen := time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return frozen }

	tok, err := r.Issue(&agent_backend_entity.AgentBackend{ID: 1, LLMProviderKey: "key-1"}, 30*time.Second)
	assert.NoError(t, err)
	assert.Equal(t, 1, r.Size())

	// 还没到点：命中
	_, ok := r.Resolve(tok)
	assert.True(t, ok)

	// 到点：未命中并被清掉
	r.now = func() time.Time { return frozen.Add(31 * time.Second) }
	_, ok = r.Resolve(tok)
	assert.False(t, ok)
	assert.Equal(t, 0, r.Size())
}

func TestTokenRegistry_ZeroTTLNeverExpires(t *testing.T) {
	r := NewTokenRegistry()
	tok, err := r.Issue(&agent_backend_entity.AgentBackend{ID: 1, LLMProviderKey: "key-1"}, 0)
	assert.NoError(t, err)
	// 模拟未来：仍命中
	r.now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	_, ok := r.Resolve(tok)
	assert.True(t, ok)
}

func TestTokenEntry_ResolveModel(t *testing.T) {
	entry := TokenEntry{
		MainProviderKey: "key-1",
		Routes:          map[string]string{"OPUS": "key-5", "SONNET": "key-6"},
	}

	cases := []struct {
		input string
		want  string
		hit   bool
	}{
		{"OPUS", "key-5", true},
		{"opus", "key-5", true},
		{"  Sonnet ", "key-6", true},
		{"HAIKU", "key-1", false},
		{"", "key-1", false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, hit := entry.ResolveModel(tc.input)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.hit, hit)
		})
	}
}

func TestTokenEntry_ResolveModelEmptyRoutes(t *testing.T) {
	entry := TokenEntry{MainProviderKey: "key-42"}
	got, hit := entry.ResolveModel("anything")
	assert.Equal(t, "key-42", got)
	assert.False(t, hit)
}
