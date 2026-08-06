// Package sessions is the in-memory map of active chat sessions. Each
// registered session corresponds to one live agentruntime subprocess.
package sessions

import (
	"sync"

	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
)

// Registry implements handlers.SessionRegistryPort via a sync.RWMutex-
// guarded map.
type Registry struct {
	mu                 sync.RWMutex
	m                  map[string]handlers.SessionHandle
	runtimeGenerations map[int64]runtimeGenerationOwner
}

type runtimeGenerationOwner struct {
	connection *rpc.Conn
	generation string
}

func NewRegistry() *Registry {
	return &Registry{
		m:                  map[string]handlers.SessionHandle{},
		runtimeGenerations: map[int64]runtimeGenerationOwner{},
	}
}

func (r *Registry) Register(id string, h handlers.SessionHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[id] = h
}

func (r *Registry) Lookup(id string) (handlers.SessionHandle, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.m[id]
	return h, ok
}

func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}

func (r *Registry) List() []handlers.SessionHandle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]handlers.SessionHandle, 0, len(r.m))
	for _, h := range r.m {
		out = append(out, h)
	}
	return out
}

// ClaimRuntimeGeneration reserves one Agentre session for the exact WebSocket
// connection and opaque Pi generation token that created it. A reconnect must
// wait for the old owner to finish cleanup rather than racing a SessionID-only
// Abort against the new process.
func (r *Registry) ClaimRuntimeGeneration(connection *rpc.Conn, sessionID int64, generation string) bool {
	if connection == nil || sessionID <= 0 || generation == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runtimeGenerations[sessionID]; exists {
		return false
	}
	r.runtimeGenerations[sessionID] = runtimeGenerationOwner{
		connection: connection,
		generation: generation,
	}
	return true
}

// ReleaseRuntimeGeneration removes only the exact connection/generation claim.
// A delayed cleanup from an old socket therefore cannot release a reconnect's
// generation even when both use the same Agentre and provider session IDs.
func (r *Registry) ReleaseRuntimeGeneration(connection *rpc.Conn, sessionID int64, generation string) bool {
	if connection == nil || sessionID <= 0 || generation == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	owner, exists := r.runtimeGenerations[sessionID]
	if !exists || owner.connection != connection || owner.generation != generation {
		return false
	}
	delete(r.runtimeGenerations, sessionID)
	return true
}
