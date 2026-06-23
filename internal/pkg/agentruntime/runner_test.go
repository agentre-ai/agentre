package agentruntime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
)

type stubRuntime struct{ name string }

func (stubRuntime) Capabilities() capability.Capabilities { return capability.Capabilities{} }

func (s stubRuntime) Run(_ context.Context, _ RunRequest) (<-chan Event, *RunResult, error) {
	ch := make(chan Event)
	close(ch)
	return ch, &RunResult{ProviderSessionID: s.name}, nil
}

func TestRuntimeFor_ReturnsNilForUnknownType(t *testing.T) {
	assert.Nil(t, RuntimeFor("definitely-not-a-real-backend"))
	assert.Nil(t, RuntimeFor(""))
}

func TestSwapRuntimeForTest_RoundTrip(t *testing.T) {
	t.Run("替换未注册的 type 后 restore 会清除", func(t *testing.T) {
		const fakeType agent_backend_entity.BackendType = "swaptest-unknown"
		assert.Nil(t, RuntimeFor(fakeType))
		restore := SwapRuntimeForTest(fakeType, stubRuntime{name: "x"})
		assert.NotNil(t, RuntimeFor(fakeType))
		restore()
		assert.Nil(t, RuntimeFor(fakeType))
	})

	t.Run("替换已注册的 type 后 restore 会还原", func(t *testing.T) {
		const fakeType agent_backend_entity.BackendType = "swaptest-existing"
		original := stubRuntime{name: "orig"}
		RegisterRuntime(fakeType, original)
		t.Cleanup(func() {
			registryMu.Lock()
			delete(registry, fakeType)
			registryMu.Unlock()
		})

		restore := SwapRuntimeForTest(fakeType, stubRuntime{name: "swapped"})
		got := RuntimeFor(fakeType)
		s, ok := got.(stubRuntime)
		assert.True(t, ok)
		assert.Equal(t, "swapped", s.name)

		restore()
		got = RuntimeFor(fakeType)
		s2, ok := got.(stubRuntime)
		assert.True(t, ok)
		assert.Equal(t, "orig", s2.name)
	})
}
