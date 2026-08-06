package codex

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
)

func TestBuildLaunchSpec_MCPServers(t *testing.T) {
	Convey("Given RunRequest 带一个 http MCP server", t, func() {
		spec := buildLaunchSpec(agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			MCPServers: []agentruntime.MCPServerSpec{{
				Name: "group",
				URL:  "http://127.0.0.1:9000/mcp/group/",
				Headers: map[string]string{
					"Authorization": "Bearer tok-123",
					"X-Group":       "group-1",
				},
				Tools: []string{"group_send", "group_invite"},
			}},
		}, nil, "/tmp/work")

		Convey("Then Codex --config 注入 mcp_servers 配置并自动放行声明的 tool", func() {
			So(spec.config, ShouldContain, `mcp_servers.group.url="http://127.0.0.1:9000/mcp/group/"`)
			So(spec.config, ShouldContain, `mcp_servers.group.http_headers.Authorization="Bearer tok-123"`)
			So(spec.config, ShouldContain, `mcp_servers.group.http_headers.X-Group="group-1"`)
			So(spec.config, ShouldContain, `mcp_servers.group.enabled_tools=["group_send","group_invite"]`)
			So(spec.config, ShouldContain, `mcp_servers.group.default_tools_approval_mode="approve"`)
		})
	})

	Convey("Given RunRequest 不带 MCPServers(回归)", t, func() {
		spec := buildLaunchSpec(agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
		}, nil, "/tmp/work")

		Convey("Then 不下发任何 mcp_servers 覆盖项", func() {
			for _, cfg := range spec.config {
				So(cfg, ShouldNotStartWith, "mcp_servers.")
			}
		})
	})
}

func TestBuildLaunchSpec_ModelOverride(t *testing.T) {
	Convey("Given RunRequest 带 ModelOverride", t, func() {
		Convey("Then spec.model = override, 优先于 provider.Model", func() {
			spec := buildLaunchSpec(agentruntime.RunRequest{
				Backend: &agent_backend_entity.AgentBackend{
					Type:    string(agent_backend_entity.TypeCodex),
					EnvJSON: "{}",
				},
				Provider:      &llm_provider_entity.LLMProvider{Model: "gpt-5.4"},
				ModelOverride: "gpt-5.5",
			}, nil, "/tmp/work")
			So(spec.model, ShouldEqual, "gpt-5.5")
		})

		Convey("Then override 空白时退回 provider.Model", func() {
			spec := buildLaunchSpec(agentruntime.RunRequest{
				Backend: &agent_backend_entity.AgentBackend{
					Type:    string(agent_backend_entity.TypeCodex),
					EnvJSON: "{}",
				},
				Provider:      &llm_provider_entity.LLMProvider{Model: "gpt-5.4"},
				ModelOverride: "   ",
			}, nil, "/tmp/work")
			So(spec.model, ShouldEqual, "gpt-5.4")
		})

		Convey("Then provider = nil 时 override 仍作为裸模型下发(CLI 登录态)", func() {
			spec := buildLaunchSpec(agentruntime.RunRequest{
				Backend: &agent_backend_entity.AgentBackend{
					Type:    string(agent_backend_entity.TypeCodex),
					EnvJSON: "{}",
				},
				ModelOverride: "gpt-5.6-terra",
			}, nil, "/tmp/work")
			So(spec.model, ShouldEqual, "gpt-5.6-terra")
		})
	})
}

func TestBuildLaunchSpec_EnabledPlugins(t *testing.T) {
	Convey("Given RunRequest 带 Codex plugin 显式覆盖", t, func() {
		spec := buildLaunchSpec(agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
			EnabledPlugins: map[string]bool{
				"browser@openai-bundled":     true,
				"superpowers@openai-curated": false,
			},
		}, nil, "/tmp/work")

		Convey("Then Codex --config 注入 plugins.<id>.enabled 覆盖", func() {
			So(spec.config, ShouldContain, `plugins."browser@openai-bundled".enabled=true`)
			So(spec.config, ShouldContain, `plugins."superpowers@openai-curated".enabled=false`)
		})
	})

	Convey("Given RunRequest 不带 EnabledPlugins(回归)", t, func() {
		spec := buildLaunchSpec(agentruntime.RunRequest{
			Backend: &agent_backend_entity.AgentBackend{
				Type:    string(agent_backend_entity.TypeCodex),
				EnvJSON: "{}",
			},
		}, nil, "/tmp/work")

		Convey("Then 不下发任何 plugins 覆盖项", func() {
			for _, cfg := range spec.config {
				So(cfg, ShouldNotStartWith, "plugins.")
			}
		})
	})
}
