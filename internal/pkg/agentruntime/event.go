package agentruntime

import (
	"encoding/json"

	"github.com/cago-frame/agents/provider"

	"agentre/internal/pkg/agentruntime/canonical"
)

// Event 是 sealed interface,所有 typed event case 必须实现 isEvent()。
// chat_svc 用 type switch 处理;不再有"Kind discriminator + 15 个可选字段"的胖 struct。
//
// 旧 RuntimeEvent 仍在 runner.go 保留(daemon wire format + 旧 fixture 模板用);
// 新代码统一通过 Event 直流。
type Event interface {
	isEvent()
}

// TextDelta 流式纯文本片段。
type TextDelta struct{ Text string }

// ThinkingDelta 流式思考片段(Anthropic 协议把它放在 turn 开头并保住 signature)。
type ThinkingDelta struct{ Text string }

// ToolCall 携带原始工具名 + input;Canonical 在 translator 识别成功时填,nil 表示
// 非 canonical (走 raw tool_use 路径)。同 ToolCallID 多次 emit 视为增量更新
// (canonical 增量),accumulator 用 mutateIndex 覆盖。
type ToolCall struct {
	ID               string
	Name             string
	Input            json.RawMessage
	Canonical        canonical.CanonicalTool
	ParentToolCallID string
}

// ToolResult 工具调用结果。Meta 携带 backend 在 tool_result 旁吐的结构化元数据
// (claudecode 走 CLI 顶层 tool_use_result;codex 当前不发),原始 JSON 字节;
// chat_svc 落 ChatBlock,前端按工具语义 Unmarshal。无 meta 留 nil。
//
// ParentToolCallID:当前 tool_result 属于 subagent 内部工具时指向外层 Agent.tool_use_id;
// 主 agent 自己的工具留空。前端据此把子卡归集到父 SubagentInvocationCard。
type ToolResult struct {
	ToolCallID       string
	Content          string
	IsError          bool
	ParentToolCallID string
	Meta             json.RawMessage
}

// SteerConsumed mid-turn 用户消息被 backend 注入到当前 turn(claudecode user 块 /
// codex turn/steer)。Steers 是 FIFO 顺序的批次,chat_svc 据此把对应 queued
// chat_message 状态推进到 consumed。
type SteerConsumed struct{ Steers []ConsumedSteer }

// UserAskRequest backend 检测到 AskUserQuestion 控制请求时 emit。
// ToolCallID race 时(control_request 比 tool_use 先到)允许为空,前端按 RequestID merge。
// ParentToolCallID:subagent 内部 AskUserQuestion 时指向外层 Agent.tool_use_id。
type UserAskRequest struct {
	RequestID        string
	ToolCallID       string
	ParentToolCallID string
	Questions        []AskQuestion
}

// UserAskResolved backend 完成 SubmitAnswer 反向投回后 emit。Skipped=true 表示用户跳过。
// ParentToolCallID 与对应 UserAskRequest 一致,便于前端 merge 后仍落在子卡上。
type UserAskResolved struct {
	RequestID        string
	ParentToolCallID string
	Answers          []AskAnswer
	Skipped          bool
}

// ToolPermissionRequest backend 收到 can_use_tool(除 AskUserQuestion 以外)时 emit。
type ToolPermissionRequest struct {
	RequestID  string
	ToolCallID string
	ToolName   string
	Input      json.RawMessage
}

// ToolPermissionResolved backend 完成 SubmitToolPermission 反向投回后 emit。
type ToolPermissionResolved struct {
	RequestID   string
	Allowed     bool
	AlwaysAllow bool
	DenyReason  string
}

// PermissionModeChanged CLI 通报自身 permission_mode 已变更。
type PermissionModeChanged struct{ Mode string }

// SubagentStarted / Progress / Done claudecode subagent 生命周期。ToolCallID 指向
// 外层 Task / Agent 工具的调用 id。Info 携带 SubagentInfo 元数据镜像。
type SubagentStarted struct {
	ToolCallID string
	Info       SubagentInfo
}
type SubagentProgress struct {
	ToolCallID string
	Info       SubagentInfo
}
type SubagentDone struct {
	ToolCallID string
	Info       SubagentInfo
}

// Retry 非终止 backend 重试通知。
type Retry struct {
	Message string
	Details string
	Attempt int
	Max     int
}

// UsageUpdate per-API-call usage 上报。TotalInputTokens 由各 runtime translator
// 按 family 聚合(Anthropic = prompt + cached + cacheCreation;OpenAI = prompt),
// 供 chat_svc 直接 patch assistantMsg 与 emit StreamUsage,前端不再做家族判断。
type UsageUpdate struct {
	Usage            *provider.Usage
	TotalInputTokens int
}

// ContextWindowUpdated runtime 探到模型实际可用窗口大小变化时 emit
// (codex modelContextWindow);claudecode / builtin 不发。Tokens=0 视为"未探到"。
type ContextWindowUpdated struct{ Tokens int }

// PlanUpdated runtime 上报的计划更新(claudecode TodoWrite / codex update_plan +
// plan delta)。Plan 携带 canonical 化的步骤 + 完整 Markdown,chat_svc 走 canonical
// 投影到 PlanBlock / PlanUpdateCard,不必再各自适配 wire 格式。
type PlanUpdated struct{ Plan canonical.PlanUpdate }

// CompactBoundary runtime 上报的会话上下文压缩边界(claudecode 的
// system{subtype:"compact_boundary"} 帧;codex 的 contextCompaction item /
// thread/compacted notification)。chat_svc 据此持久化一条
// role=system 的边界 message + emit StreamCompactBoundary,前端折叠旧上下文。
//
// 任一字段为零值表示 CLI 没下发对应 compact_metadata 字段,
// 前端按零值退化展示(不显示数字 / trigger label / 耗时)。
type CompactBoundary struct {
	PreTokens  int
	PostTokens int    // 压缩后保留的 token 数 (摘要 + 必要历史)
	Trigger    string // "auto" | "manual"
	DurationMs int    // 压缩耗时,毫秒
}

// RuntimeStatus runtime 上报的会话级运行状态字符串。当前 claudecode 用它表达
// "compacting" / "requesting" 等带状态的过渡阶段 —— /compact 启动到 compact_boundary
// 之间这段时间持续生效,chat_svc 据此推送 stream event,前端 Composer 替换 typing
// indicator 为 "正在压缩上下文…" chip。
//
// 空 Status 视为"清理/重置"信号,runtime translator 自己决定是否 emit;chat_svc 只
// 关心最后一次非空 Status 和后续 done/error/compact_boundary 之间的窗口。
type RuntimeStatus struct {
	Status string
}

// Done turn 正常结束。
type Done struct{}

// ErrorEvent turn 因错误中止;Err 携带原因。
type ErrorEvent struct{ Err error }

func (TextDelta) isEvent()              {}
func (ThinkingDelta) isEvent()          {}
func (ToolCall) isEvent()               {}
func (ToolResult) isEvent()             {}
func (SteerConsumed) isEvent()          {}
func (UserAskRequest) isEvent()         {}
func (UserAskResolved) isEvent()        {}
func (ToolPermissionRequest) isEvent()  {}
func (ToolPermissionResolved) isEvent() {}
func (PermissionModeChanged) isEvent()  {}
func (SubagentStarted) isEvent()        {}
func (SubagentProgress) isEvent()       {}
func (SubagentDone) isEvent()           {}
func (Retry) isEvent()                  {}
func (UsageUpdate) isEvent()            {}
func (ContextWindowUpdated) isEvent()   {}
func (PlanUpdated) isEvent()            {}
func (CompactBoundary) isEvent()        {}
func (RuntimeStatus) isEvent()          {}
func (Done) isEvent()                   {}
func (ErrorEvent) isEvent()             {}
