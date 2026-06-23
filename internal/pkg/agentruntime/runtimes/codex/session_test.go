package codex

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
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
