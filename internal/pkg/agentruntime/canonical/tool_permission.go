package canonical

// ToolPermission 通用工具审批(非 ExitPlanMode);前端 ToolPermissionCard 渲染。
// 来源:claudecode 的 tool_permission_request control_request,toolName 任意
// (Bash/Read/MCP 等),由 chat_svc dispatcher_emitter + replay 路径双向填充。
// ExitPlanMode 走 PlanApproveRequest 分支,不走这里。
//
// Resolved/Allowed/AlwaysAllow 由 ToolPermissionResolved event 反向回灌:
// 卡片读 Resolved 切换交互/只读态、读 Allowed 区分通过/拒绝、读 AlwaysAllow
// 表示用户选了"本 session 自动允许"。
type ToolPermission struct {
	RequestID   string         `json:"requestId"`
	ToolName    string         `json:"toolName"`
	ToolInput   map[string]any `json:"toolInput,omitempty"`
	Resolved    bool           `json:"resolved,omitempty"`
	Allowed     bool           `json:"allowed,omitempty"`
	AlwaysAllow bool           `json:"alwaysAllow,omitempty"`
}

func (ToolPermission) canonicalKind() Kind { return KindToolPermission }
