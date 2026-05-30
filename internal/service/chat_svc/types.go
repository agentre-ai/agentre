// internal/service/chat_svc/types.go
package chat_svc

import (
	"agentre/internal/service/chat_svc/blocks"
	"agentre/internal/service/chat_svc/view"
)

// Chat Wails 事件名形如 "chat:event:<sessionID>:<assistantMessageID>"。
// 前端用 EventsOn 注册该名字接收 chunk / done / error。
const StreamEventPrefix = "chat:event"

// ChatStreamEventKind 是 Wails 事件 payload 里的 kind 枚举。
type ChatStreamEventKind string

const (
	StreamChunk            ChatStreamEventKind = "chunk"
	StreamThinking         ChatStreamEventKind = "thinking"
	StreamToolUse          ChatStreamEventKind = "tool_use"
	StreamToolResult       ChatStreamEventKind = "tool_result"
	StreamSteerConsumed    ChatStreamEventKind = "steer_consumed"
	StreamSubagentStarted  ChatStreamEventKind = "subagent_started"
	StreamSubagentProgress ChatStreamEventKind = "subagent_progress"
	StreamSubagentDone     ChatStreamEventKind = "subagent_done"
	StreamRetry            ChatStreamEventKind = "retry"
	StreamMessageEnd       ChatStreamEventKind = "message_end"
	StreamDone             ChatStreamEventKind = "done"
	StreamError            ChatStreamEventKind = "error"
	StreamClosed           ChatStreamEventKind = "closed"
	// StreamAborted 用户点「停止」中断本轮 turn 时 emit。语义上是 Done 的兄弟：
	// 流以正常方式结束（partial 内容保留 + agentStatus=idle），但前端要渲染成
	// 「已停止」标签而不是 error 红字。Message 字段携带最终的 assistant 消息状态
	// （包含 abort 之前已经流出的 blocks）。
	StreamAborted ChatStreamEventKind = "aborted"
	// StreamAskUserQuestion backend 检测到 AskUserQuestion 类工具调用时 emit。
	// 前端渲染交互卡片，用户答完后调 AnswerUserQuestion 回灌。Answered=true 的
	// 事件代表"已回答"态切换（无需重新建 block，按 RequestID 找到既有 block 更新）。
	StreamAskUserQuestion ChatStreamEventKind = "ask_user_question"
	// StreamPlanUpdate backend 收到 runtime plan delta/update_plan 时 emit。
	// 前端把它落为 type:"plan" + canonical.plan.update live block,作为底部
	// TaskProgressBar 的数据源;若 canonical.plan.update.actions 非空,同一个
	// type:"plan" block 会复用 PlanCard 作为下一步操作入口。tool_use 形式的
	// plan.update 仍按普通 tool card 展示。
	StreamPlanUpdate ChatStreamEventKind = "plan_update"
	// StreamToolPermissionRequest backend 收到非 AskUserQuestion 类 can_use_tool
	// 时 emit。前端渲染审批卡片，用户决策后调 AnswerToolPermission 回灌。
	// Resolved=true 的事件代表"已审批"态切换（按 RequestID 找到既有 block 更新）。
	StreamToolPermissionRequest ChatStreamEventKind = "tool_permission_request"
	// StreamSessionStatus 推送 session 级 status patch（agentStatus + needsAttention）。
	// 用于 turn 进行中遇到 ask / 审批等待时把 toolbar 翻成橙色 WAITING，应答后翻回
	// RUNNING。前端按 stream name 已知 sessionId，patch 体只带新状态。
	StreamSessionStatus ChatStreamEventKind = "session_status"
	// StreamUsage 在 turn 内每次模型内部 API call 边界推一条，携带当前 assistant
	// 消息「本次 API call 之后看到的输入大小」（per-call usage）。前端 Composer
	// 进度条据此阶梯式刷新「已用上下文」，不必等 StreamDone 才更新。
	// chat_svc 同时把 token 列写回 chat_messages 行（context.WithoutCancel 抗
	// abort），让刷新页面也能看到中间态。
	StreamUsage ChatStreamEventKind = "usage"
	// StreamCompactBoundary backend 收到 runtime CompactBoundary 时 emit
	// (claudecode system.compact_boundary;manual / auto 同等)。前端据此在 transcript
	// 内嵌"上下文已压缩"分隔卡片,并默认折叠最后一个 compact_boundary 之前的全部消息
	// (DB 保留,展开可见)。同时 chat_svc 在当前 assistant message blocks 末尾追加
	// CompactBoundaryBlock 持久化,LoadSession 重放可重建 UI。
	StreamCompactBoundary ChatStreamEventKind = "compact_boundary"
	// StreamRuntimeStatus runtime 中间状态通知（如 compacting）。
	// 前端 chat-streams-host 据此切 typing indicator 样式。
	StreamRuntimeStatus ChatStreamEventKind = "runtime_status"
)

// ChatStreamEvent 是 EventsEmit 出去的统一 payload。
type ChatStreamEvent struct {
	Kind    ChatStreamEventKind `json:"kind"`
	Delta   string              `json:"delta,omitempty"`
	Message *ChatMessage        `json:"message,omitempty"`
	Error   string              `json:"error,omitempty"`

	// steer_consumed 事件填充：queuedIds 用于前端清 queue chip；
	// previousAssistantMessage / userMessages / assistantMessage 用于把当前
	// assistant 段收口、插入正式 user 段，并把后续 live stream 切到新的 assistant。
	QueuedIDs                []string      `json:"queuedIds,omitempty"`
	PreviousAssistantMessage *ChatMessage  `json:"previousAssistantMessage,omitempty"`
	UserMessages             []ChatMessage `json:"userMessages,omitempty"`
	AssistantMessage         *ChatMessage  `json:"assistantMessage,omitempty"`

	// tool_use 事件填充（StreamToolUse）。
	ToolUseID string         `json:"toolUseId,omitempty"`
	ToolName  string         `json:"toolName,omitempty"`
	ToolInput map[string]any `json:"toolInput,omitempty"`
	// Canonical 是前端消费的统一工具识别投影 — runtime translator 算出来后,
	// handler emit、dispatcher_emitter 转 wire CanonicalDTO。前端按
	// CanonicalDTO.kind 分发到 canonical-tool/<kind>/card.tsx;不识别走 RawToolCard。
	Canonical *view.CanonicalDTO `json:"canonical,omitempty"`

	// tool_result 事件填充（StreamToolResult）。
	ToolResult string `json:"toolResult,omitempty"`
	IsError    bool   `json:"isError,omitempty"`
	// ToolResultMeta backend 透传过来的工具结构化元数据（claudecode CLI 顶层
	// tool_use_result;codex 当前不发）。前端按工具语义解码,典型用例是 TaskCreate
	// 用它把系统分配的 task id 喂给前端做 task-progress 关联。无 meta 时留 nil。
	ToolResultMeta map[string]any `json:"toolResultMeta,omitempty"`

	// subagent 内部产生的 tool_use / tool_result 在这里附上外层 Agent.tool_use_id；
	// 主 agent 自己的工具留空。前端据此把子 block 从主 transcript 移走，挂到父卡。
	ParentToolCallID string `json:"parentToolUseId,omitempty"`

	// StreamSubagent* 事件填充：外层 Agent.tool_use_id + 元数据快照。
	// 前端按 ToolUseID 找到对应的 ChatBlock 并 merge Subagent 字段。
	Subagent *ChatBlockSubagent `json:"subagent,omitempty"`

	// StreamAskUserQuestion 事件填充：交互问题载荷或答完后的状态切换。
	AskUserQuestion *ChatBlockAskUserQuestion `json:"askUserQuestion,omitempty"`

	// StreamToolPermissionRequest 事件填充：审批载荷或审批后的状态切换。
	ToolPermission *ChatBlockToolPermission `json:"toolPermission,omitempty"`

	// StreamRetry 事件填充：后端/上游的非终态重试通知。本轮 turn 继续运行。
	RetryAttempt     int    `json:"retryAttempt,omitempty"`
	RetryMaxAttempts int    `json:"retryMaxAttempts,omitempty"`
	RetryMessage     string `json:"retryMessage,omitempty"`
	RetryDetails     string `json:"retryDetails,omitempty"`
	RetryAt          int64  `json:"retryAt,omitempty"`

	// StreamSessionStatus 事件填充：session 级 status patch。
	SessionStatus *ChatSessionStatusPatch `json:"sessionStatus,omitempty"`

	// StreamUsage 事件填充：当前 assistant 的 per-call token 快照。
	Usage *ChatStreamUsage `json:"usage,omitempty"`

	// StreamCompactBoundary 事件填充:压缩边界元数据 + 落库的 assistantMessageId
	// (前端按 messageId + boundary 在 blocks 中的位置切分折叠)。
	Compact *ChatCompactBoundary `json:"compact,omitempty"`

	// StreamRuntimeStatus 事件填充：runtime 中间状态快照。
	RuntimeStatus *ChatRuntimeStatus `json:"runtimeStatus,omitempty"`
}

// ChatCompactBoundary 是 StreamCompactBoundary 事件的 payload。MessageID 是 boundary
// 所挂的 assistant message ID(同 turn 内自然 = TurnContext.assistantMsg.ID);Seq 该消息
// 在会话内的顺序号(前端按 Seq + 这个 blockIndex 定位边界);PreTokens / Trigger 透传
// CLI compact_metadata 字段(零值表示 CLI 没下发);At 是落库的 unix 毫秒,跟 block 同值。
type ChatCompactBoundary struct {
	MessageID int64  `json:"messageId"`
	Seq       int    `json:"seq"`
	PreTokens int    `json:"preTokens,omitempty"`
	Trigger   string `json:"trigger,omitempty"` // "auto" | "manual"
	At        int64  `json:"at"`
}

// ChatRuntimeStatus 是 StreamRuntimeStatus 事件的 payload。
// Status 是 runtime 上报的中间状态字符串（如 "compacting" / "requesting"）。
// Compacting 为 true 时前端把 typing indicator 切换为压缩动画。
type ChatRuntimeStatus struct {
	Status     string `json:"status"`
	Compacting bool   `json:"compacting,omitempty"`
}

// ChatStreamUsage 是 StreamUsage 事件 payload。字段与 ChatMessage 上的 token 列同名，
// 前端按 backend / provider 家族决定如何聚合（Anthropic 系叠加 cached + cacheCreation，
// OpenAI 系仅看 promptTokens）—— 与 computeComposerContextUsage 现有口径一致。
type ChatStreamUsage struct {
	MessageID           int64 `json:"messageId,omitempty"`
	PromptTokens        int   `json:"promptTokens,omitempty"`
	CompletionTokens    int   `json:"completionTokens,omitempty"`
	CachedTokens        int   `json:"cachedTokens,omitempty"`
	CacheCreationTokens int   `json:"cacheCreationTokens,omitempty"`
	ReasoningTokens     int   `json:"reasoningTokens,omitempty"`
	// TotalInputTokens runtime translator 按 family 聚合的本次 API call 输入大小。
	// 前端不再做 family 判断,直接读这个值显示 "已用上下文"。
	TotalInputTokens int `json:"totalInputTokens,omitempty"`
}

// ChatSessionStatusPatch 是 StreamSessionStatus 事件的 payload。
// AgentStatus 总是带最新值（idle/running/waiting/error），NeedsAttention 也总是带；
// 前端按字段直接覆盖 ChatSessionDetail 即可，不需要再做 diff。
type ChatSessionStatusPatch struct {
	AgentStatus    string `json:"agentStatus"`
	NeedsAttention bool   `json:"needsAttention"`
	// PermissionMode 可选：只在 CLI 通报 permission mode 变更时填（被动 ExitPlanMode 流程）。
	// 缺省（omitempty）时前端不动 ChatSessionDetail.permissionMode；带值时直接覆盖。
	PermissionMode string `json:"permissionMode,omitempty"`
	// ContextWindow 可选:runtime 探到模型实际窗口大小时填(codex modelContextWindow)。
	// 前端按非零值覆盖 ChatSessionDetail.contextWindow;0 = 没探到,保留旧值。
	ContextWindow int `json:"contextWindow,omitempty"`
}

// ChatBlock 是 backend → 前端的简化投影：把 cago/agents StoredBlock 拍平。
// 已支持的 Type：text / thinking / tool_use / tool_result / ask_user_question / unknown（兜底）。
type ChatBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"` // text / thinking / tool_result 文本
	Image *ChatBlockImage `json:"image,omitempty"`

	// tool_use:
	ToolUseID string         `json:"toolUseId,omitempty"`
	ToolName  string         `json:"toolName,omitempty"`
	ToolInput map[string]any `json:"toolInput,omitempty"`

	// tool_result:
	IsError bool `json:"isError,omitempty"`
	// ToolResultMeta backend 在 tool_result 旁吐的工具结构化元数据(claudecode CLI
	// 顶层 tool_use_result;codex 当前不发)。Claude Code 的 TaskCreate 走这条通道
	// 把系统分配的 task id 喂给前端 —— CLI 不在 tool input 里回 id。前端 task-progress
	// 派生层据此把 TaskCreate ↔ TaskUpdate 关联起来。普通工具帧没有 meta 时留 nil。
	ToolResultMeta map[string]any `json:"toolResultMeta,omitempty"`

	// 当前 block 是 subagent 内部产生时，指向外层 Agent.tool_use_id；
	// 主 agent 自己的 block 留空。前端按它把子 block 归集到父 SubagentInvocationCard。
	ParentToolCallID string `json:"parentToolUseId,omitempty"`

	// 仅外层 Agent / Task 工具的 tool_use block 上填，缓存 subagent 元数据快照
	// （subagent_type / 累计 token / last_tool_name / status 等）。
	Subagent *ChatBlockSubagent `json:"subagent,omitempty"`

	// ask_user_question block 专用：交互问题与答题状态。
	AskUserQuestion *ChatBlockAskUserQuestion `json:"askUserQuestion,omitempty"`

	// tool_permission_request block 专用：工具审批载荷与决策状态。
	ToolPermission *ChatBlockToolPermission `json:"toolPermission,omitempty"`

	// Canonical 是 runtime translator 算出的统一工具识别投影 — wire 形态由
	// chat_svc/view/CanonicalDTO 提供。前端按 kind 分发到 canonical-tool/<kind>/card.tsx。
	// Live emit 路径:dispatcher_emitter 从 handler m["canonical"] 转;
	// Replay 路径:view/project.go 重建 block 时按 runtime translator 重算。
	Canonical *view.CanonicalDTO `json:"canonical,omitempty"`

	// Compact 仅 type="compact_boundary" 时填:压缩边界元数据(pre_tokens / trigger / at)。
	// 前端按 trigger 区分文案、按 at 显示时间,按"最后一条 compact_boundary 之前"切分折叠。
	Compact *ChatBlockCompactBoundary `json:"compact,omitempty"`

	Raw map[string]any `json:"raw,omitempty"` // unknown 兜底
}

type ChatBlockImage struct {
	Name      string `json:"name,omitempty"`
	MediaType string `json:"mediaType"`
	DataURL   string `json:"dataUrl"`
}

// ChatBlockCompactBoundary 是 type=compact_boundary block 的 wire payload,
// 镜像 blocks.CompactBoundaryBlock 三个字段。
type ChatBlockCompactBoundary struct {
	PreTokens int    `json:"preTokens,omitempty"`
	Trigger   string `json:"trigger,omitempty"`
	At        int64  `json:"at"`
}

// ChatBlockAskUserQuestion 是前端渲染 AskUserQuestion 卡片需要的全部状态。
//
// RequestID 来自 control_request.request_id，是前端答题后回传 AnswerUserQuestion
// 的句柄。ToolUseID 关联到同 turn 内 assistant 帧里的 tool_use 块；race 情况下
// （control_request 比 tool_use 先到）可能为空，前端按 RequestID 占位、等 tool_use
// 帧到了 merge。
//
// Answered + Answers + Skipped 在用户提交后更新；持久化到 chat_messages.blocks
// 让历史回放也能看到"已选 X / 用户跳过"。
type ChatBlockAskUserQuestion struct {
	RequestID string                  `json:"requestId"`
	Questions []blocks.AskQuestionDTO `json:"questions"`
	Answered  bool                    `json:"answered,omitempty"`
	Answers   []blocks.AskAnswerDTO   `json:"answers,omitempty"`
	Skipped   bool                    `json:"skipped,omitempty"`
}

// ChatBlockToolPermission 是前端渲染工具审批卡片需要的全部状态。
//
// RequestID 来自 control_request.request_id，是前端审批后回传 AnswerToolPermission
// 的句柄。ToolInput 是 control_request.input 解析后的对象（前端按 ToolName 自行
// pretty-print，比如 Bash 突出 command 字段）。
//
// Resolved + Allowed + AlwaysAllow 在用户审批后更新；持久化到 chat_messages.blocks
// 让历史回放也能看到"已允许 / 已拒绝"。
type ChatBlockToolPermission struct {
	RequestID   string         `json:"requestId"`
	ToolName    string         `json:"toolName"`
	ToolInput   map[string]any `json:"toolInput"`
	Resolved    bool           `json:"resolved,omitempty"`
	Allowed     bool           `json:"allowed,omitempty"`
	AlwaysAllow bool           `json:"alwaysAllow,omitempty"`
}

// ChatBlockSubagent 是 claudecode.SubagentMeta / agentruntime.SubagentInfo 在前端投影里的镜像。
//
// task_started 给到完整 prompt / subagent_type；task_progress 阶段性带 last_tool_name + cumulative usage；
// task_notification 给 status + 最终 usage。所有字段对老数据自动为零值，向前兼容。
type ChatBlockSubagent struct {
	TaskID          string `json:"taskId,omitempty"`
	SubagentType    string `json:"subagentType,omitempty"`
	TaskDescription string `json:"taskDescription,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
	LastToolName    string `json:"lastToolName,omitempty"`
	ToolUses        int    `json:"toolUses,omitempty"`
	TotalTokens     int    `json:"totalTokens,omitempty"`
	DurationMs      int    `json:"durationMs,omitempty"`
	Status          string `json:"status,omitempty"` // running | completed | failed
}

type ChatMessage struct {
	ID                  int64       `json:"id"`
	SessionID           int64       `json:"sessionId"`
	Role                string      `json:"role"`
	Blocks              []ChatBlock `json:"blocks"`
	Model               string      `json:"model"`
	PromptTokens        int         `json:"promptTokens"`
	CompletionTokens    int         `json:"completionTokens"`
	CachedTokens        int         `json:"cachedTokens"`
	CacheCreationTokens int         `json:"cacheCreationTokens"`
	ReasoningTokens     int         `json:"reasoningTokens"`
	// TotalInputTokens runtime translator 按 family 聚合好的本次 API call 输入大小。
	// 前端 Composer 进度条「已用上下文」按此读,不再做 backend-family-specific 加法。
	TotalInputTokens int    `json:"totalInputTokens"`
	DurationMs       int    `json:"durationMs"`
	ErrorText        string `json:"errorText"`
	Seq              int    `json:"seq"`
	Createtime       int64  `json:"createtime"`
}

type ChatSessionLite struct {
	ID             int64  `json:"id"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	NeedsAttention bool   `json:"needsAttention"`
	LastMessageAt  int64  `json:"lastMessageAt"`
	// LastReadAt 由 chat_svc.MarkSessionRead 推进；前端 sidebar 折叠态 attention bubble 用
	// LastMessageAt > LastReadAt 判定「未读」。
	LastReadAt int64 `json:"lastReadAt"`
}

type ChatSessionDetail struct {
	ID                 int64  `json:"id"`
	AgentID            int64  `json:"agentId"`
	AgentName          string `json:"agentName"`
	AgentColor         string `json:"agentColor"`
	AgentIcon          string `json:"agentIcon"`
	AgentAvatarDataURL string `json:"agentAvatarDataUrl"`
	// BackendType 是 agent 绑定的 backend 类型（builtin/claudecode/codex），
	// 前端用来决定「复制启动命令」等仅对 CLI 后端有效的菜单项是否可见。
	BackendType string `json:"backendType"`
	// LLMProviderType 是 backend 绑定的主 LLM provider 类型（anthropic / openai-chat /
	// openai-response）。前端用它和 BackendType 一起判定 Usage 字段语义：Anthropic 系
	// 的 PromptTokens 只含未缓存输入，要叠加 CachedTokens + CacheCreationTokens 才是
	// 总上下文；OpenAI 系的 PromptTokens 已是总数。空串表示后端未绑定 provider（CLI 登录态）。
	LLMProviderType string `json:"llmProviderType"`
	Title           string `json:"title"`
	AgentStatus     string `json:"agentStatus"`
	// NeedsAttention 是由 AgentStatus=="waiting" 派生的兼容字段，不单独持久化。
	// 前端 toolbar 同时叠 displayStatus 兜底：即便 session_status stream 事件丢失，
	// LoadSession 拉到这个字段为 true 也能把状态翻成橙色 WAITING。
	NeedsAttention bool  `json:"needsAttention"`
	LastMessageAt  int64 `json:"lastMessageAt"`
	LastReadAt     int64 `json:"lastReadAt"`
	Createtime     int64 `json:"createtime"`
	// ContextWindow 当前 agent 绑定 backend 的主 LLM provider 的上下文窗口（token 数）。
	// 解析顺序：provider.ContextWindow > 0 → 直接用；否则 cago 内置 catalog 兜底；都没有 → 0
	// （前端约定 0 时不展示上下文用量条）。
	ContextWindow int `json:"contextWindow"`
	// PermissionMode 是 CLI 后端会话当前模式：claudecode 使用 permission mode，
	// codex 使用 default / plan collaboration mode 子集。空串是历史兼容值；
	// 前端按 backend normalize 成对应默认值显示。builtin 不使用。
	PermissionMode string `json:"permissionMode"`
	// PermissionModeAtLaunch 是 claudecode CLI 子进程 spawn 时下发的 mode 快照；
	// runtime Shift+Tab 切换不动它。前端 pill 用它判定 bypass 选项是否还可点。
	// 空串表示「还没 spawn 过 / 老会话」。
	PermissionModeAtLaunch string `json:"permissionModeAtLaunch"`
	// 远端 device 归属 + 远端 cwd, 给前端 chat header 渲染"远端运行"小字使用。
	// 空 DeviceID = 本地;非空 = paired_agentred.id 字符串化。
	// DeviceName 来自 paired_agentreds.display_name;Online 由 LastSeenAt 推算。
	// Cwd 是该 session 真正的工作目录:本地 = project.path (或 AgentCwd 兜底);
	// 远端 = project_locations.path (resolveSessionCwd 已经做完路由)。
	DeviceID   string `json:"deviceID"`
	DeviceName string `json:"deviceName"`
	Online     bool   `json:"online"`
	Cwd        string `json:"cwd"`
	// ProjectID = 0 表示自由会话；> 0 时受 project_svc 管控。
	// 前端 ChatPanel 用它派生 breadcrumb 路径。
	ProjectID int64 `json:"projectId,omitempty"`
}

type ChatAgentItem struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	AvatarColor   string `json:"avatarColor"`
	AvatarIcon    string `json:"avatarIcon"`
	AvatarDataURL string `json:"avatarDataUrl"`
	BackendType   string `json:"backendType"`
	// DefaultPermissionMode 是 claudecode 后端管理员预设的 spawn 时 mode；其它后端
	// （codex / builtin）一律留空。前端新会话场景下用它作为 pill 起手值兜底，并把
	// 同值随 SendChatMessage.permissionMode 透回，让 chat_svc.createPermissionMode
	// 的「raw 非空就直接用」分支照样落到管理员预设上。
	DefaultPermissionMode string            `json:"defaultPermissionMode"`
	Chattable             bool              `json:"chattable"`
	Pinned                bool              `json:"pinned"`
	ChattableHint         string            `json:"chattableHint"`
	ActiveCount           int               `json:"activeCount"`
	RecentCount           int               `json:"recentCount"`
	TotalSessions         int64             `json:"totalSessions"`
	SessionIDs            []int64           `json:"sessionIds"`
	Sessions              []ChatSessionLite `json:"sessions"`
	AttentionSessions     []ChatSessionLite `json:"attentionSessions"`

	// 远端 device 归属 — 给前端 DeviceTag 渲染本地/远端 chip 用。
	// 空 DeviceID = 本地 backend；非空 = paired_agentred.id 字符串化。
	// DeviceName 来自 paired_agentreds.display_name；Online 由 LastSeenAt 推算。
	DeviceID   string `json:"deviceID"`
	DeviceName string `json:"deviceName"`
	Online     bool   `json:"online"`
}

// ChatSessionGitState 是一次 git 状态快照。Branch 为空 + NotARepo=true 意味着 cwd
// 不在 git 仓库内, 前端把整个 chip 区折叠掉。HasUpstream=false 时 Ahead/Behind 不渲染。
type ChatSessionGitState struct {
	Branch      string `json:"branch"`
	Worktree    string `json:"worktree"`
	Dirty       int    `json:"dirty"`
	Ahead       int    `json:"ahead"`
	Behind      int    `json:"behind"`
	HasUpstream bool   `json:"hasUpstream"`
	NotARepo    bool   `json:"notARepo"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type GetSessionGitStateRequest struct {
	SessionID int64 `json:"sessionId"`
}

type GetSessionGitStateResponse struct {
	State ChatSessionGitState `json:"state"`
}

// ── Request / Response shapes ────────────────────────────────────────────────

type ListAgentsRequest struct{}
type ListAgentsResponse struct {
	Agents []ChatAgentItem `json:"agents"`
}

type LoadSessionRequest struct {
	SessionID int64 `json:"sessionId"`
}
type LoadSessionResponse struct {
	Session  ChatSessionDetail `json:"session"`
	Messages []ChatMessage     `json:"messages"`
}

// ListAgentSessionsRequest 给「查看全部 N 个会话」popover 翻页拉数据用。
// Limit==0 时服务侧用默认页大小 20；上限 100。
type ListAgentSessionsRequest struct {
	AgentID int64 `json:"agentId"`
	Offset  int   `json:"offset"`
	Limit   int   `json:"limit"`
}
type ListAgentSessionsResponse struct {
	Sessions []ChatSessionLite `json:"sessions"`
	Total    int64             `json:"total"`
	HasMore  bool              `json:"hasMore"`
}

type SendRequest struct {
	SessionID int64       `json:"sessionId"` // 0 = 新建
	AgentID   int64       `json:"agentId"`
	Text      string      `json:"text"`
	Images    []SendImage `json:"images,omitempty"`
	// 新建会话路径（SessionID=0）专用：把会话挂到指定项目。
	// 已存在的会话不应再传 ProjectID —— Send 会忽略它，project 在 Create 时定型。
	ProjectID int64 `json:"projectId,omitempty"`
	// PermissionMode 是 CLI 后端会话启动模式：
	//   - claudecode: default / acceptEdits / plan / bypassPermissions
	//   - codex: default / plan
	// 空串表示不改已有会话；新建 codex 会话空串按 default 落库。
	PermissionMode string `json:"permissionMode,omitempty"`
}
type SendImage struct {
	Name    string `json:"name,omitempty"`
	DataURL string `json:"dataUrl"`
}
type SendResponse struct {
	SessionID          int64  `json:"sessionId"`
	UserMessageID      int64  `json:"userMessageId"`
	AssistantMessageID int64  `json:"assistantMessageId"`
	Stream             string `json:"stream"`
}

type CompactRequest struct {
	SessionID int64 `json:"sessionId"`
}

type CompactResponse struct {
	SessionID          int64  `json:"sessionId"`
	AssistantMessageID int64  `json:"assistantMessageId"`
	Stream             string `json:"stream"`
}

type ChatGoal struct {
	ThreadID        string `json:"threadId"`
	Objective       string `json:"objective"`
	Status          string `json:"status"`
	TokenBudget     *int   `json:"tokenBudget,omitempty"`
	TokensUsed      int    `json:"tokensUsed"`
	TimeUsedSeconds int    `json:"timeUsedSeconds"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

type GoalRequest struct {
	SessionID int64 `json:"sessionId"`
}

type SetGoalRequest struct {
	SessionID   int64   `json:"sessionId"`
	Objective   *string `json:"objective,omitempty"`
	Status      *string `json:"status,omitempty"`
	TokenBudget *int    `json:"tokenBudget,omitempty"`
}

type StartGoalRequest struct {
	AgentID        int64   `json:"agentId"`
	ProjectID      int64   `json:"projectId,omitempty"`
	PermissionMode string  `json:"permissionMode,omitempty"`
	Objective      *string `json:"objective,omitempty"`
	Status         *string `json:"status,omitempty"`
	TokenBudget    *int    `json:"tokenBudget,omitempty"`
}

type StartGoalResponse struct {
	SessionID int64     `json:"sessionId"`
	Goal      *ChatGoal `json:"goal,omitempty"`
}

type ClearGoalRequest struct {
	SessionID int64 `json:"sessionId"`
}

type GoalResponse struct {
	Goal *ChatGoal `json:"goal,omitempty"`
}

type ClearGoalResponse struct {
	Cleared bool `json:"cleared"`
}

type RenameRequest struct {
	SessionID int64  `json:"sessionId"`
	Title     string `json:"title"`
}
type RenameResponse struct{}

type DeleteRequest struct {
	SessionID int64 `json:"sessionId"`
}
type DeleteResponse struct{}

// MarkSessionReadRequest 把 last_read_at 推进到至少 Timestamp (unix ms)。
// Timestamp <= 0 时服务侧改用当前时间。语义单调：repo 层只在新 ts 严格大于旧值时落库。
type MarkSessionReadRequest struct {
	SessionID int64 `json:"sessionId"`
	Timestamp int64 `json:"timestamp"`
}
type MarkSessionReadResponse struct{}

// RegenerateRequest 触发"从指定 assistant 消息重新生成"：
//   - 截掉对应 user 消息（含）开始的所有 chat_messages
//   - 用同一段 user 文本重新走一遍 turn
//
// builtin 通过截 DB + history 重建生效；claudecode 走 provider_anchor fork；
// codex 按目标 user 到末尾的 user 消息数执行 thread/rollback。
type RegenerateRequest struct {
	SessionID      int64  `json:"sessionId"`
	MessageID      int64  `json:"messageId"` // 目标 assistant 消息 id
	PermissionMode string `json:"permissionMode,omitempty"`
}

// EnqueueRequest 是 AI 还在回答时用户发的「下一条」消息。service 把它
// 注入到当前正在跑的 turn（claudecode 走 SteerInbox + PostToolUse hook —
// 没被 hook 拉走的残留在 turn 结束时由 runTurn DrainPending 收尾自动起新一轮；
// codex 走 turn/steer RPC）。不会落 chat_messages 表 —— 语义上是"下次 AI
// 看到的提示"，不是历史消息。
type EnqueueRequest struct {
	SessionID int64  `json:"sessionId"`
	Text      string `json:"text"`
}

// EnqueueResponse 把刚入队消息的稳定 ID 回传给前端。前端按它显示 chip 并
// 用作后续 CancelQueued 的 handle。Cancellable=false 表示当前后端（codex）
// 一发即不可撤，前端把对应 chip 上的 X 替换为锁图标。
type EnqueueResponse struct {
	SessionID   int64  `json:"sessionId"`
	Queued      bool   `json:"queued"`
	QueuedID    string `json:"queuedId"`
	Cancellable bool   `json:"cancellable"`
}

// CancelQueuedRequest 撤回排队消息。QueuedID 为空 = 清空当前会话的整条队列。
type CancelQueuedRequest struct {
	SessionID int64  `json:"sessionId"`
	QueuedID  string `json:"queuedId"`
}

// CancelQueuedResponse 返回实际被撤回的 queued ID 列表（FIFO）。前端用它
// 同步前端的 queue state。
type CancelQueuedResponse struct {
	Removed []string `json:"removed"`
}

// StopRequest 用户点「停止」中断当前 turn。SessionID 标识哪个会话；当前会话
// 必须正在跑（agentStatus=running/waiting），否则返回 ChatStopNoActive。
type StopRequest struct {
	SessionID int64 `json:"sessionId"`
}

// StopResponse Stopped=true 表示 abort 路径已经触发（不代表 turn 此刻已完全结束
// —— 异步 cleanup 在 runTurn goroutine 完成）。前端按 StreamAborted 事件翻 UI。
type StopResponse struct {
	Stopped bool `json:"stopped"`
}

// SetPermissionModeRequest 用户切换 CLI 会话模式。
// claudecode 可取 {default, acceptEdits, plan, bypassPermissions}；
// codex 可取 {default, plan}。
//
// 持久化语义：mode 总是写入 chat_sessions.permission_mode 后再尝试下发到
// 当前活跃 CLI 子进程。如果 CLI 还没起 / 已被 LRU evict，runtime 下发会被
// 跳过（不报错），下一次 spawn CLI 时会读 DB 用 --permission-mode 启动；
// 因此前端切 pill **不需要**先发一条消息把进程拉起来。
type SetPermissionModeRequest struct {
	SessionID int64  `json:"sessionId"`
	Mode      string `json:"mode"`
}

// SetPermissionModeResponse Applied=true 表示请求已被后端接受（DB 已落）。
// runtime 是否已即时下发到活跃 CLI 由后端 best-effort，CLI 不在时下次 spawn
// 自然生效。前端不需要区分这两种情形。
type SetPermissionModeResponse struct {
	Applied bool   `json:"applied"`
	Mode    string `json:"mode"`
}

// LaunchCommandRequest / Response 用于「复制启动命令」菜单：
// 把当前 session 关联的 CLI 后端配置拼成可在终端粘贴运行的命令。
// Token 字段固定为占位符 <TOKEN>，不发放实际 token；用户自行替换。
type LaunchCommandRequest struct {
	SessionID int64 `json:"sessionId"`
}

type LaunchCommandResponse struct {
	Command     string `json:"command"`
	BackendType string `json:"backendType"`
}

// EditRequest 触发"编辑历史 user 消息并重跑"：
//   - 截掉目标 user 消息（含）开始的所有 chat_messages
//   - 用新文本 Text 走一遍 turn
//
// 跟 Regenerate 共用 fork 路径：claudecode 走 provider_anchor fork；
// codex 走 thread/rollback；builtin 通过截 DB + history 重建生效。
type EditRequest struct {
	SessionID      int64  `json:"sessionId"`
	MessageID      int64  `json:"messageId"` // 目标 user 消息 id
	Text           string `json:"text"`      // 新的 user 文本
	PermissionMode string `json:"permissionMode,omitempty"`
}
