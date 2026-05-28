package terminal_svc

import (
	"context"
	"errors"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/pkg/pty"
)

//go:generate mockgen -source=backend.go -destination=mocks/mock_backend.go -package=mocks

type PTYBackend interface {
	Open(ctx context.Context, spec pty.Spec) (pty.Handle, error)
}

type RemoteBackendFactory func(deviceID string) (PTYBackend, error)

type BackendSelector struct {
	local        PTYBackend
	remoteFactor RemoteBackendFactory
}

func NewBackendSelector(local PTYBackend, remote RemoteBackendFactory) *BackendSelector {
	return &BackendSelector{local: local, remoteFactor: remote}
}

var ErrNoBackend = errors.New("no backend on session agent")

func (s *BackendSelector) Pick(be *agent_backend_entity.AgentBackend) (PTYBackend, error) {
	if be == nil {
		return nil, ErrNoBackend
	}
	if be.IsLocal() {
		return s.local, nil
	}
	return s.remoteFactor(be.DeviceID)
}
