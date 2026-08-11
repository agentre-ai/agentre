// Package peer owns the desktop's inbound, session-level peer surface.
package peer

import (
	"context"
	"encoding/json"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// Inbound keeps the desktop registered through one reconnecting relay link and
// turns relay-initiated virtual channels into private JSON-RPC registries.
// Session methods are registered through RegisterInboundMethods as they are
// added; this type deliberately does not reuse daemon state or handlers.
type Inbound struct {
	link     *rpc.HubLink
	mux      *rpc.Multiplexer
	registry *rpc.Registry
}

// NewInbound constructs the desktop's session-level relay endpoint. The caller
// owns the HubLink configuration and calls Run for the App lifetime.
func NewInbound(link *rpc.HubLink) *Inbound {
	registry := rpc.NewRegistry()
	RegisterInboundMethods(registry)
	return &Inbound{
		link:     link,
		mux:      rpc.NewMultiplexer(link),
		registry: registry,
	}
}

// Run keeps the desktop online until ctx is canceled. HubLink owns reconnect
// and registration while the multiplexer owns only virtual channels, so closing
// the latter cannot accidentally leave an addressable physical connection.
func (p *Inbound) Run(ctx context.Context) error {
	defer p.mux.Close()
	go p.serve(ctx)
	return p.link.Run(ctx)
}

func (p *Inbound) serve(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case channel := <-p.mux.Accept():
			if channel == nil {
				return
			}
			conn := rpc.NewConn(channel, p.registry.Clone())
			go conn.Serve(ctx)
		}
	}
}

// RegisterInboundMethods is the desktop peer's single method-registration
// entry point. Task 3 adds session-list and session-attach handlers here.
func RegisterInboundMethods(registry *rpc.Registry) {
	registry.Register("auth.account", authenticateAccount)
	registry.Register(wire.MethodCapabilities, requireAccount(capabilities))
}

// authenticateAccount completes the existing account-handshake vocabulary.
// The relay has already authenticated both WebSocket ends and enforced their
// same-account relationship before it can forward a virtual channel; this
// desktop-only endpoint records that established authorization per channel.
func authenticateAccount(ctx context.Context, raw json.RawMessage) (any, error) {
	var params rpc.AccountParams
	if err := json.Unmarshal(raw, &params); err != nil || params.Credential == "" || params.DeviceFingerprint == "" {
		return nil, rpc.ErrInvalidParams
	}
	conn := rpc.ConnFromContext(ctx)
	if conn == nil {
		return nil, rpc.ErrUnauthorized
	}
	conn.SetAuth(rpc.AuthState{Authenticated: true, DeviceFingerprint: params.DeviceFingerprint})
	return rpc.ConnectResult{OK: true}, nil
}

func requireAccount(next rpc.HandlerFunc) rpc.HandlerFunc {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		conn := rpc.ConnFromContext(ctx)
		if conn == nil || !conn.Auth().Authenticated {
			return nil, rpc.ErrUnauthorized
		}
		return next(ctx, raw)
	}
}

// capabilities is intentionally the smallest existing runtime method. It
// proves that an authorized relay peer reaches the desktop-owned registry;
// session behavior is added by the session adapter in later tasks.
func capabilities(_ context.Context, raw json.RawMessage) (any, error) {
	var params wire.CapabilitiesParams
	if err := json.Unmarshal(raw, &params); err != nil || params.BackendType == "" {
		return nil, rpc.ErrInvalidParams
	}
	return wire.CapabilitiesResult{}, nil
}
