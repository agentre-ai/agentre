package agent_backend_entity

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/model/entity/llm_provider_entity"
)

func TestPiAgentKind(t *testing.T) {
	Convey("Given pi-agent backend type", t, func() {
		kind := KindFor(TypePiAgent)

		Convey("When resolving kind metadata Then it is a CLI backend without provider coupling", func() {
			So(kind, ShouldNotBeNil)
			So(kind.Type(), ShouldEqual, TypePiAgent)
			So(kind.KnownAliases(), ShouldBeEmpty)
			So(kind.AllowsCLIPath(), ShouldBeTrue)
			So(kind.ProviderTypeMatch(llm_provider_entity.TypeAnthropic), ShouldBeFalse)
			So(kind.ProviderTypeMatch(llm_provider_entity.TypeOpenAIResponse), ShouldBeFalse)
		})

		Convey("When validating extra fields Then codex-only and claudecode-only fields are rejected", func() {
			ctx := context.Background()
			So(kind.ValidateExtra(ctx, &AgentBackend{Type: string(TypePiAgent), Name: "pi", ModelRoutes: "{}", EnvJSON: "{}"}), ShouldBeNil)
			So(kind.ValidateExtra(ctx, &AgentBackend{Type: string(TypePiAgent), Name: "pi", Sandbox: "read-only", ModelRoutes: "{}", EnvJSON: "{}"}), ShouldNotBeNil)
			So(kind.ValidateExtra(ctx, &AgentBackend{Type: string(TypePiAgent), Name: "pi", Approval: "never", ModelRoutes: "{}", EnvJSON: "{}"}), ShouldNotBeNil)
			So(kind.ValidateExtra(ctx, &AgentBackend{Type: string(TypePiAgent), Name: "pi", DefaultPermissionMode: "plan", ModelRoutes: "{}", EnvJSON: "{}"}), ShouldNotBeNil)
		})
	})
}
