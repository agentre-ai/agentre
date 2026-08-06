package codex

import (
	"strings"
	"sync"
)

const retainedTurnIDs = 64

// turnHistory quarantines recently terminal turn IDs for one persistent
// app-server process. Late notifications must never become the identity of a
// subsequent Stream.
type turnHistory struct {
	mu    sync.Mutex
	ids   map[string]struct{}
	order []string
}

func newTurnHistory() *turnHistory {
	return &turnHistory{ids: map[string]struct{}{}}
}

func (h *turnHistory) Remember(id string) {
	if h == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.ids[id]; exists {
		return
	}
	h.ids[id] = struct{}{}
	h.order = append(h.order, id)
	if len(h.order) > retainedTurnIDs {
		oldest := h.order[0]
		h.order = h.order[1:]
		delete(h.ids, oldest)
	}
}

func (h *turnHistory) Contains(id string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	_, exists := h.ids[strings.TrimSpace(id)]
	return exists
}
