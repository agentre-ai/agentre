package chat_svc

import (
	"testing"

	"agentre/internal/model/entity/agent_backend_entity"

	. "github.com/smartystreets/goconvey/convey"
)

func TestBackendSupportsGroup(t *testing.T) {
	Convey("backendSupportsGroup: claudecode 声明 CapMCPTools → true", t, func() {
		be := &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)}
		So(backendSupportsGroup(be), ShouldBeTrue)
	})
	Convey("backendSupportsGroup: codex 不声明 CapMCPTools → false", t, func() {
		be := &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeCodex)}
		So(backendSupportsGroup(be), ShouldBeFalse)
	})
	Convey("backendSupportsGroup: nil backend → false", t, func() {
		So(backendSupportsGroup(nil), ShouldBeFalse)
	})
}
