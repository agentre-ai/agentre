package canonical

// PlanApproveRequest 退出 plan 模式审批;前端 PlanApproveCard 渲染。
// 来源:claudecode ExitPlanMode(走 tool_permission_request 通道,by ToolName 识别);
// 其它 runtime 可通过 PlanUpdate.Actions 提供同一张计划卡的 action 入口。
//
// Resolved/Allowed/DenyReason 由 ToolPermissionResolved event 反向回灌:
// PlanApproveCard 读 Resolved 切换交互/只读态、读 Allowed 区分通过/拒绝、读
// DenyReason 显示用户反馈。dispatcher_emitter + toChatBlock 双路径都填这些字段。
type PlanApproveRequest struct {
	RequestID  string `json:"requestId"`
	PlanText   string `json:"planText"`
	Resolved   bool   `json:"resolved,omitempty"`
	Allowed    bool   `json:"allowed,omitempty"`
	DenyReason string `json:"denyReason,omitempty"`

	// Actions:Claude ExitPlanMode 的可选按钮组,由 handler 读
	// session.PermissionModeAtLaunch 装配(bypass launch → [bypass, manual, refine];
	// 否则 → [accept_edits, manual, refine])。resolved 后前端按只读态忽略。
	Actions []PlanAction `json:"actions,omitempty"`
}

func (PlanApproveRequest) canonicalKind() Kind { return KindPlanApproveRequest }
