package chat_entity

import (
	"context"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"
)

func TestSession_Check(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		s    *Session
		ok   bool
	}{
		{"valid", &Session{AgentID: 1, Title: "hi", AgentStatus: "idle"}, true},
		{"missing agent", &Session{Title: "x", AgentStatus: "idle"}, false},
		{"unknown status", &Session{AgentID: 1, AgentStatus: "weird"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.s.Check(ctx)
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestSession_IsActive(t *testing.T) {
	assert.True(t, (&Session{Status: consts.ACTIVE}).IsActive())
	assert.False(t, (&Session{Status: 0}).IsActive())
	assert.False(t, (*Session)(nil).IsActive())
}

func TestSession_ProviderSessionHelpers(t *testing.T) {
	t.Run("HasProviderSession 区分空串与非空", func(t *testing.T) {
		var nilSess *Session
		assert.False(t, nilSess.HasProviderSession(), "nil receiver 应为 false")
		s := &Session{}
		assert.False(t, s.HasProviderSession(), "空串应为 false")
		s.ProviderSessionID = "cc-deadbeef"
		assert.True(t, s.HasProviderSession())
	})
	t.Run("SetProviderSession 写入字段", func(t *testing.T) {
		s := &Session{}
		s.SetProviderSession("codex-abc")
		assert.Equal(t, "codex-abc", s.ProviderSessionID)
	})
	t.Run("SetProviderSession 对 nil receiver 不 panic", func(t *testing.T) {
		var s *Session
		assert.NotPanics(t, func() { s.SetProviderSession("x") })
	})
}
