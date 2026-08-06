// Package sessions owns the daemon-level ownership of in-flight runtime
// generations. A generation is one prepared/started backend turn on this
// daemon; it belongs to the exact WebSocket connection and opaque generation
// token that created it, so a reconnect can never take over — or be swept by —
// another connection's cleanup that only knows the Agentre session id.
package sessions

import (
	"sync"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
)

// Registry is the sync.Mutex-guarded generation ownership table.
type Registry struct {
	mu                 sync.Mutex
	runtimeGenerations map[int64]runtimeGenerationOwner
}

type runtimeGenerationOwner struct {
	connection *rpc.Conn
	generation string
}

func NewRegistry() *Registry {
	return &Registry{
		runtimeGenerations: map[int64]runtimeGenerationOwner{},
	}
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
