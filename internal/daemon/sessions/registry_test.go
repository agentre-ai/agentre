package sessions

import (
	"sync"
	"testing"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"

	"github.com/stretchr/testify/assert"
)

func TestRegistry_RuntimeGenerationReleaseRequiresExactConnectionOwner(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
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
		start := make(chan struct{})
		results := make(chan bool, 32)
		var wg sync.WaitGroup
		for stale := 0; stale < cap(results); stale++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				results <- r.ReleaseRuntimeGeneration(firstConn, 42, "generation-first")
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		for released := range results {
			assert.False(t, released, "old cleanup must not unregister the reconnect generation")
		}
		assert.False(t, r.ClaimRuntimeGeneration(firstConn, 42, "generation-third"),
			"the retry claim must remain registered after stale finalizers finish")
		assert.True(t, r.ReleaseRuntimeGeneration(reconnected, 42, "generation-second"))
	}
}
