package chat_svc

import (
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"

	. "github.com/smartystreets/goconvey/convey"
)

func TestBackendSupportsGroup(t *testing.T) {
	Convey("backendSupportsGroup: claudecode 声明 CapMCPTools → true", t, func() {
		be := &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)}
		So(backendSupportsGroup(be), ShouldBeTrue)
	})
	Convey("backendSupportsGroup: codex 声明 CapMCPTools → true", t, func() {
		be := &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeCodex)}
		So(backendSupportsGroup(be), ShouldBeTrue)
	})
	Convey("backendSupportsGroup: nil backend → false", t, func() {
		So(backendSupportsGroup(nil), ShouldBeFalse)
	})
}
