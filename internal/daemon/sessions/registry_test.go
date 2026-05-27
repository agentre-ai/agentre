package sessions

import (
	"sync"
	"testing"

	"agentre/internal/daemon/handlers"

	"github.com/stretchr/testify/assert"
)

func TestRegistry_RegisterLookupRemove(t *testing.T) {
	r := NewRegistry()
	r.Register("s1", handlers.SessionHandle{SessionID: "s1", BackendType: "claudecode"})
	h, ok := r.Lookup("s1")
	assert.True(t, ok)
	assert.Equal(t, "claudecode", h.BackendType)

	r.Remove("s1")
	_, ok = r.Lookup("s1")
	assert.False(t, ok)
}

func TestRegistry_Concurrent(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i%26))
			r.Register(id, handlers.SessionHandle{SessionID: id})
			r.Lookup(id)
			r.Remove(id)
		}(i)
	}
	wg.Wait()
	assert.Empty(t, r.List())
}
