package server_svc

import (
	"context"
	"sync"

	"agentre/internal/model/entity/server_state_entity"
)

// ServerSvc 桌面端接入 Hub 的服务接口。
type ServerSvc interface {
	GetState(ctx context.Context) (*server_state_entity.ServerState, error)
	StartLogin(ctx context.Context, serverURL string) (*StartLoginResult, error)
	PollLoginToken(ctx context.Context, deviceCode string) (bool, error)
	CancelLogin(ctx context.Context) error
	ListDevices(ctx context.Context) ([]Device, error)
	Logout(ctx context.Context) error
	Refresh(ctx context.Context) error
	ClearLogin(ctx context.Context) error
	// CheckURL validates that the given Server URL is reachable + healthy without
	// affecting the singleton service's state. Returns the hub-reported version.
	CheckURL(ctx context.Context, serverURL string) (string, error)
	SetEmitter(emit func(any))
}

var defaultSvc ServerSvc

// Hub 返回默认实现单例。
func Server() ServerSvc { return defaultSvc }

// SetDefault 由 bootstrap 注入实现。
func SetDefault(s ServerSvc) { defaultSvc = s }

// service 是 ServerSvc 的具体实现。
type service struct {
	mu            sync.Mutex
	client        *serverClient
	loginInFlight bool
	emitState     func(any) // 由 bootstrap 注入的 Wails 事件发射器；测试时可为 nil
}

// New 构造一个 service。client + emit 由 bootstrap 装配。
func New(client *serverClient, emit func(any)) ServerSvc {
	return &service{client: client, emitState: emit}
}

// Refresh exposes the package-private refresh() for callers like bootstrap.HubBoot.
func (s *service) Refresh(ctx context.Context) error { return s.refresh(ctx) }

// SetEmitter swaps the Wails event emitter at runtime. Called from app.go.startup
// once the wails context exists; safe to call before any concurrent reader.
func (s *service) SetEmitter(emit func(any)) {
	s.mu.Lock()
	s.emitState = emit
	s.mu.Unlock()
}

// emit safely invokes the registered Wails event emitter, if any.
// Reads s.emitState under the same mutex SetEmitter uses to write it.
func (s *service) emit(payload any) {
	s.mu.Lock()
	fn := s.emitState
	s.mu.Unlock()
	if fn != nil {
		fn(payload)
	}
}

// getClient returns the current serverClient under the mutex.
// client field is protected by s.mu (shared with loginInFlight + emitState).
func (s *service) getClient() *serverClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client
}

// setClient atomically replaces the serverClient under the mutex.
func (s *service) setClient(c *serverClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = c
}

// GetState, ListDevices, and Logout are implemented in state.go, devices.go, and logout.go.
