// Package canonical 定义跨 runtime 的"特殊工具"统一抽象。每个 CanonicalTool
// 子类型对应一个 wire kind,前端按 kind 路由到对应卡片。runtime translator 把
// 自家工具(claudecode Write / codex fileChange{created} / 等)翻译到对应
// canonical 子类型,attach 到 agentruntime.ToolCall.Canonical 字段。
package canonical

// Kind 与 wire 协议字符串一一对应;前端 TS discriminated union 直接吃。
type Kind string

const (
	KindFileWrite          Kind = "file.write"
	KindFileEdit           Kind = "file.edit"
	KindUserAsk            Kind = "user.ask"
	KindPlanUpdate         Kind = "plan.update"
	KindPlanApproveRequest Kind = "plan.approve_request"
	KindAgentSpawn         Kind = "agent.spawn"
	KindToolPermission     Kind = "tool.permission"
)

// CanonicalTool 是 sealed interface;所有具体类型在子文件实现 canonicalKind()。
// agentruntime.ToolCall.Canonical 字段持有它。
type CanonicalTool interface {
	canonicalKind() Kind
}

// KindOf 取 canonical 实例的 kind 值。nil-safe(返回空串)。
func KindOf(c CanonicalTool) Kind {
	if c == nil {
		return ""
	}
	return c.canonicalKind()
}
