package canonical

// PlanUpdate 是跨 runtime 的计划更新表示,可携带结构化 Steps、Markdown Text,
// 以及可选 action 描述。Steps 主要供底部 TaskProgressBar 读取;带 Actions
// 的 plan block 可在 transcript 中复用计划卡作为下一步操作入口。
//
// 来源:
//   - ToolCall.Canonical:claudecode TodoWrite、codex update_plan;
//   - PlanUpdated event:claudecode TaskCreate/TaskUpdate 聚合快照、codex
//     turn/plan/updated 或 item/plan 完整 Markdown。
//
// Actions 由 runtime translator 在需要交互收尾时装配;chat_svc 只透传并
// 持久化。ResolvePlanAction 只在有计划 action 按钮的卡片上使用。
type PlanUpdate struct {
	Steps   []PlanStep   `json:"steps,omitempty"`
	Text    string       `json:"text,omitempty"` // Codex item/plan 完整 Markdown;旧 RuntimeEvent.PlanText 也映射到这里
	Actions []PlanAction `json:"actions,omitempty"`
}

func (PlanUpdate) canonicalKind() Kind { return KindPlanUpdate }

type PlanStep struct {
	ID     string         `json:"id,omitempty"` // 可选:TodoWrite 输入 id;TaskCreate 聚合来自 tool_result.meta.task.id;Codex 通常为空
	Step   string         `json:"step"`
	Status PlanStepStatus `json:"status"`
}

type PlanStepStatus string

const (
	StepPending    PlanStepStatus = "pending"
	StepInProgress PlanStepStatus = "inProgress"
	StepCompleted  PlanStepStatus = "completed"
	StepCancelled  PlanStepStatus = "canceled"
)
