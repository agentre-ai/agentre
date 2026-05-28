package terminal_svc_test

import (
	"context"
	"testing"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/pkg/pty"
	"agentre/internal/service/terminal_svc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBackend struct{ name string }

func (f *fakeBackend) Open(_ context.Context, _ pty.Spec) (pty.Handle, error) {
	return nil, nil
}

func TestBackendSelector_PicksLocal(t *testing.T) {
	be := &agent_backend_entity.AgentBackend{DeviceID: ""}
	sel := terminal_svc.NewBackendSelector(&fakeBackend{name: "local"}, func(string) (terminal_svc.PTYBackend, error) {
		t.Fatal("should not call remote factory for local")
		return nil, nil
	})
	got, err := sel.Pick(be)
	require.NoError(t, err)
	assert.Equal(t, "local", got.(*fakeBackend).name)
}

func TestBackendSelector_PicksRemote(t *testing.T) {
	be := &agent_backend_entity.AgentBackend{DeviceID: "7"}
	sel := terminal_svc.NewBackendSelector(&fakeBackend{name: "local"}, func(devID string) (terminal_svc.PTYBackend, error) {
		assert.Equal(t, "7", devID)
		return &fakeBackend{name: "remote"}, nil
	})
	got, err := sel.Pick(be)
	require.NoError(t, err)
	assert.Equal(t, "remote", got.(*fakeBackend).name)
}

func TestBackendSelector_NilBackend_ReturnsError(t *testing.T) {
	sel := terminal_svc.NewBackendSelector(&fakeBackend{name: "local"}, nil)
	_, err := sel.Pick(nil)
	require.ErrorIs(t, err, terminal_svc.ErrNoBackend)
}
