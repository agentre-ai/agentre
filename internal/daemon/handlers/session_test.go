package handlers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/daemon/handlers/mock_handlers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestSessionList_Empty(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ms := mock_handlers.NewMockSessionRegistryPort(ctrl)
	ms.EXPECT().List().Return(nil)
	h := handlers.NewSessionHandlers(ms)
	res, err := h.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.Sessions)
}

func TestSessionList_Populated(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ms := mock_handlers.NewMockSessionRegistryPort(ctrl)
	ms.EXPECT().List().Return([]handlers.SessionHandle{
		{SessionID: "s1", BackendType: "claudecode", Workdir: "/a", StartedAt: 10},
		{SessionID: "s2", BackendType: "codex", Workdir: "/b", StartedAt: 20},
	})
	h := handlers.NewSessionHandlers(ms)
	res, err := h.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, res.Sessions, 2)
	assert.Equal(t, "claudecode", res.Sessions[0].BackendType)
}

func TestSessionGet_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ms := mock_handlers.NewMockSessionRegistryPort(ctrl)
	ms.EXPECT().Lookup("nope").Return(handlers.SessionHandle{}, false)
	h := handlers.NewSessionHandlers(ms)
	_, err := h.Get(context.Background(), handlers.SessionGetParams{SessionID: "nope"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, handlers.ErrSessionNotFound))
}

func TestSessionGet_Hit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ms := mock_handlers.NewMockSessionRegistryPort(ctrl)
	ms.EXPECT().Lookup("s1").Return(handlers.SessionHandle{
		SessionID: "s1", BackendType: "claudecode", Workdir: "/x", StartedAt: 42,
	}, true)
	h := handlers.NewSessionHandlers(ms)
	res, err := h.Get(context.Background(), handlers.SessionGetParams{SessionID: "s1"})
	require.NoError(t, err)
	assert.Equal(t, "s1", res.SessionID)
	assert.Equal(t, "active", res.Status)
}
