// Package sessions is the in-memory map of active chat sessions. Each
// registered session corresponds to one live agentruntime subprocess.
package sessions

import (
	"sync"

	"agentre/internal/daemon/handlers"
)

// Registry implements handlers.SessionRegistryPort via a sync.RWMutex-
// guarded map.
type Registry struct {
	mu sync.RWMutex
	m  map[string]handlers.SessionHandle
}

func NewRegistry() *Registry {
	return &Registry{m: map[string]handlers.SessionHandle{}}
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
