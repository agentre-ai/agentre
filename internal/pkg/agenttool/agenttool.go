// Package agenttool 维护 agent 级内置工具注册表(静态元数据)。leaf 层:
// 只描述 key/挂载路径/MCP tool 名,不 import service —— handler 实现在
// internal/service/orgtool_svc,由 bootstrap 按 MCPPath 挂到 gateway。
package agenttool

// Definition 一个内置 agent 工具(以 MCP server 形态注入会话)。
type Definition struct {
	Key       string   // agents.tools_json 的 key,也是 MCPServerSpec.Name
	MCPPath   string   // gateway 挂载路径
	ToolNames []string // server 暴露的 MCP tool 名(全部进 allowedTools,审批在服务端)
}

// KeyOrg 组织架构读写工具。
const KeyOrg = "org"

// KeyWorkflow 协作流程(SOP)读写工具。
const KeyWorkflow = "workflow"

// KeyGroupCreate 拉群带流程工具(group_create)。刻意等于 group_svc.toolKeyGroupCreate
// 与审批卡 ToolKey,保持注入/审批/门控全链路同值。注入仍走专用 BuildCreateTurnMCP
// (create token 与成员 token 不同),registry 项用于「可用工具清单 + ToolEnabled 查询」。
const KeyGroupCreate = "group_create"

// KeySubagent 调用子 agent 工具(把子任务委派给另一具名 agent,同步拿回输出)。
const KeySubagent = "subagent"

var registry = []Definition{
	{Key: KeyOrg, MCPPath: "/mcp/org/", ToolNames: []string{
		"org_get",
		"org_create_department", "org_update_department", "org_delete_department",
		"org_create_agent", "org_update_agent", "org_delete_agent",
	}},
	{Key: KeyWorkflow, MCPPath: "/mcp/workflow/", ToolNames: []string{
		"workflow_list", "workflow_create", "workflow_update", "workflow_delete",
	}},
	{Key: KeyGroupCreate, MCPPath: "/mcp/group/", ToolNames: []string{"group_create"}},
	{Key: KeySubagent, MCPPath: "/mcp/subagent/", ToolNames: []string{"agent_list", "agent_call"}},
}

// Registry 返回全部内置工具定义(只读副本)。
func Registry() []Definition {
	out := make([]Definition, len(registry))
	copy(out, registry)
	return out
}

// Lookup 按 key 找定义。
func Lookup(key string) (Definition, bool) {
	for _, d := range registry {
		if d.Key == key {
			return d, true
		}
	}
	return Definition{}, false
}

// Keys 返回全部工具 key(给前端可用工具清单)。
func Keys() []string {
	out := make([]string, 0, len(registry))
	for _, d := range registry {
		out = append(out, d.Key)
	}
	return out
}
