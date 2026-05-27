package rpc

import (
	"context"
	"encoding/json"
	"sync"
)

// HandlerFunc is the canonical method signature. Implementations parse
// params from json.RawMessage on demand and return a value to be marshaled
// into the response Result, or an error (preferably *rpc.Error so the
// dispatcher passes the code through verbatim).
type HandlerFunc func(ctx context.Context, params json.RawMessage) (any, error)

// Registry maps method name → handler. Safe for concurrent registration
// during bootstrap (called from the main goroutine) and concurrent Dispatch
// at runtime.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]HandlerFunc
}

func NewRegistry() *Registry {
	return &Registry{handlers: map[string]HandlerFunc{}}
}

// Register installs (or overwrites) the handler for a method.
func (r *Registry) Register(method string, h HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[method] = h
}

// Dispatch routes a request to its handler. Returns ErrMethodNotFound when
// the method has no handler installed.
func (r *Registry) Dispatch(ctx context.Context, method string, params json.RawMessage) (any, error) {
	r.mu.RLock()
	h, ok := r.handlers[method]
	r.mu.RUnlock()
	if !ok {
		return nil, ErrMethodNotFound
	}
	return h(ctx, params)
}

// Methods returns registered method names, useful for debug/status endpoints.
func (r *Registry) Methods() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.handlers))
	for m := range r.handlers {
		out = append(out, m)
	}
	return out
}
