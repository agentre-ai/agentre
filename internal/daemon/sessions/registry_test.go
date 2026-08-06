package sessions

import (
	"sync"
	"testing"

	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"

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

func TestRegistry_RuntimeGenerationReleaseRequiresExactConnectionOwner(t *testing.T) {
	r := NewRegistry()
	firstConn := new(rpc.Conn)
	reconnected := new(rpc.Conn)

	assert.True(t, r.ClaimRuntimeGeneration(firstConn, 42, "generation-first"))
	assert.False(t, r.ClaimRuntimeGeneration(reconnected, 42, "generation-second"),
		"the same Agentre session cannot be rebound before its old connection releases ownership")
	assert.False(t, r.ReleaseRuntimeGeneration(reconnected, 42, "generation-first"),
		"another connection cannot release the active generation")
	assert.False(t, r.ReleaseRuntimeGeneration(firstConn, 42, "generation-stale"),
		"a stale generation token cannot release its owner's newer work")
	assert.True(t, r.ReleaseRuntimeGeneration(firstConn, 42, "generation-first"))

	assert.True(t, r.ClaimRuntimeGeneration(reconnected, 42, "generation-second"))
	assert.False(t, r.ReleaseRuntimeGeneration(firstConn, 42, "generation-first"),
		"old disconnect cleanup must not unregister the reconnect generation")
	assert.True(t, r.ReleaseRuntimeGeneration(reconnected, 42, "generation-second"))
}
