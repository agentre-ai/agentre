package agentruntime

//go:generate mockgen -source runner.go -destination mock_agentruntime/mock_runner.go

import (
	"context"

	"github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/agents/provider"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/llm_provider_entity"
)

// EventKind 统一事件离散类型。chat_svc 按这个枚举做 switch。
type EventKind string

const (
	EventTextDelta     EventKind = "text_delta"
	EventThinkingDelta EventKind = "thinking_delta"
	EventToolUseStart  EventKind = "tool_use_start"
	EventToolUseEnd    EventKind = "tool_use_end"
	EventToolResult    EventKind = "tool_result"
	EventSteerConsumed EventKind = "steer_consumed"
	// subagent 生命周期（仅 claudecode backend 当前产生；codex 不发）。
	// chat_svc 把元数据 merge 到对应的外层 Agent ChatBlock 的 Subagent 字段上，
	// 并 emit StreamSubagent* 给前端做卡片态切换。
	EventSubagentStarted  EventKind = "subagent_started"
	EventSubagentProgress EventKind = "subagent_progress"
	EventSubagentDone     EventKind = "subagent_done"
	// EventAskUserQuestion backend 检测到 ask_user_question 类型的工具调用
	// 时 emit（Claude Code 的内置 AskUserQuestion、Codex / 内置 Agent
	// 后续注册的同语义 function tool 都翻译到这里）。service 接住后 push
	// 给前端渲染卡片；用户答完后通过 AskAnswerSink 反向投回给具体 backend，
	// 由 backend 各自实现"同 turn 注入答案"的协议细节。
	EventAskUserQuestion EventKind = "ask_user_question"
	// EventAskUserQuestionAnswered backend 完成 SubmitAnswer 反向投回后 emit。
	// 用于把 Answered/Skipped/Answers 状态写回 acc 里那条 AskUserQuestionBlock,
	// 这样 turn finalize 时 SetBlocks 落盘的 chat_messages.blocks 就是终态;
	// service 同时 forward 一条 StreamAskUserQuestion 给前端做兜底 merge。
	EventAskUserQuestionAnswered EventKind = "ask_user_question_answered"
	// EventPlanUpdated carries Codex collaboration plan updates. It is separate
	// from EventToolUseStart because the UI needs a visible assistant plan even
	// when Codex does not emit normal text in plan mode.
	EventPlanUpdated EventKind = "plan_updated"
	// EventToolPermissionRequest backend 收到 can_use_tool（除 AskUserQuestion 以外）
	// 时 emit。service 推前端渲染审批卡，用户决定 allow/deny 后通过
	// ToolPermissionSink 反向投回 backend。前端可以读 Input 自己 pretty-print。
	EventToolPermissionRequest EventKind = "tool_permission_request"
	// EventToolPermissionResolved backend 完成 SubmitToolPermission 反向投回后 emit。
	// service 把终态 patch 回 acc 里那条 ToolPermissionRequestBlock，并 forward
	// 一条 StreamToolPermissionResolved 给前端做兜底 merge。
	EventToolPermissionResolved EventKind = "tool_permission_resolved"
	// EventPermissionModeChanged claudecode CLI 通报自身 permission mode 已变更
	// （被动 ExitPlanMode 流程 / 主动 set_permission_mode 回执）。
	// chat_svc 接住后落 chat_sessions.permission_mode 并推 StreamSessionStatus patch。
	EventPermissionModeChanged EventKind = "permission_mode_changed"
	EventRetry                 EventKind = "retry"
	// EventUsage backend 在 turn 中途上报「本次 API call 之后模型当前看到的输入大小」。
	// claudecode 每个主 agent assistant 帧、codex 的 token_count notification 各产一条。
	// chat_svc 接住后更新 assistantMsg 的 token 列 + 落库 + emit StreamUsage 给前端，
	// 让 Composer 进度条在 turn 内随工具循环阶梯式刷新。Usage 字段必填，其它字段留空。
	EventUsage           EventKind = "usage"
	EventCompactBoundary EventKind = "compact_boundary"
	// EventRuntimeStatus backend 通报会话级运行状态字符串 (compacting / requesting ...)。
	// 与 EventPermissionModeChanged 同源帧但承载独立信号 —— chat_svc 推一条 stream
	// event,前端在 Composer / typing indicator 处显示 "正在压缩上下文…" 等过渡 chip。
	EventRuntimeStatus EventKind = "runtime_status"
	EventError         EventKind = "error"
	EventDone          EventKind = "done"
)

// ToolUseEvent EventToolUseStart / End 携带。Input 是原始 JSON；chat_svc 自己 unmarshal 到 map。
//
// ParentToolCallID：当前 tool_use 是 subagent 内部调用时指向外层 Agent.tool_use_id；
// 主 agent 自己的工具留空。前端据此把子卡归集到父 SubagentInvocationCard。
// 注:JSON wire 字段仍叫 parentToolUseId（来自 Anthropic CLI 协议），仅 Go field 重命名。
//
// Subagent：仅外层 Agent / Task 父调用上填，透传 claudecode.SubagentMeta 元数据。
type ToolUseEvent struct {
	ID               string
	Name             string
	Input            []byte
	ParentToolCallID string
	Subagent         *SubagentInfo
}

// ToolResultEvent EventToolResult 携带。
//
// ResultMeta 透传 backend 在 tool_result 旁吐的工具结构化元数据（claudecode 走 CLI
// 顶层 tool_use_result；codex 当前不发）。原始 JSON,留给 chat_svc 落 ChatBlock,
// 前端按工具语义 Unmarshal。无 meta 的工具留 nil。
type ToolResultEvent struct {
	ToolUseID        string
	Content          string
	IsError          bool
	ParentToolCallID string
	ResultMeta       []byte
}

// SubagentInfo 是 claudecode.SubagentMeta 在 runtime 层的镜像，由
// EventSubagent* 事件以及外层 Agent 工具的 ToolUseEvent 携带。
//
// 字段含义见 claudecode.SubagentMeta。
type SubagentInfo struct {
	TaskID          string
	SubagentType    string
	TaskDescription string
	Prompt          string
	LastToolName    string
	ToolUses        int
	TotalTokens     int
	DurationMs      int
	Status          string // running | completed | failed | canceled (canceled 由 chat_svc 在 turn abort 收尾时推断,runtime 层不主动产出)
}

// ConsumedSteer is a queued mid-turn user message that the backend has now
// incorporated into the active conversation.
type ConsumedSteer struct {
	QueuedID string
	Text     string
}

// RetryEvent is a non-terminal backend retry notification. It is surfaced to
// the UI while the turn keeps running.
type RetryEvent struct {
	Message           string
	AdditionalDetails string
	Attempt           int
	MaxAttempts       int
}

// RuntimeEvent 统一流事件。
type RuntimeEvent struct {
	Kind       EventKind
	Text       string
	ToolUse    *ToolUseEvent
	ToolResult *ToolResultEvent
	Steers     []ConsumedSteer
	Retry      *RetryEvent
	Err        error

	// EventSubagent* 携带：Subagent 元数据；外层 Agent.tool_use_id 放在 ToolUseID。
	Subagent  *SubagentInfo
	ToolUseID string

	// EventAskUserQuestion 携带：解析后的 question 列表 + backend 提供的
	// requestID（claudecode 走 control_request.request_id；codex 走
	// item/tool/requestUserInput 的 JSON-RPC request id）。
	AskUserQuestion *AskUserQuestionEvent

	// EventPlanUpdated 携带：Codex app-server 上报的当前计划步骤。
	Plan []PlanStep
	// PlanText 携带 Codex app-server 以 plan item 形式输出的完整 Markdown plan。
	// 新版 app-server 在 plan mode 下发 item/plan/delta + item/completed{type:"plan"}，
	// 不再一定发 turn/plan/updated。
	PlanText string

	// EventToolPermissionRequest / EventToolPermissionResolved 携带。
	ToolPermission *ToolPermissionEvent

	// EventPermissionModeChanged 携带：CLI 通报切换后的新 mode 值。
	// 合法值同 pkg/claudecode PermissionMode 白名单（default/plan/acceptEdits/bypassPermissions）。
	PermissionMode string

	// EventUsage 携带：本次 API call 之后模型当前看到的输入大小（per-call usage）。
	// 非 nil 才有效；chat_svc 据此 patch assistantMsg.PromptTokens 等列并 emit StreamUsage。
	Usage *provider.Usage

	// TotalInputTokens EventUsage 携带:由 runtime translator 按 family 已聚合的
	// 总输入(Anthropic = prompt + cached + cacheCreation;OpenAI = prompt)。
	// 新 sealed Event 必填;老 RuntimeEvent 路径靠 chat.go 自己做家族数学兜底。
	TotalInputTokens int
}

// ToolPermissionEvent EventToolPermission{Request,Resolved} 携带。
//
// RequestID 是 backend 私有的回写句柄（claudecode = control_request.request_id），
// service 在调 ToolPermissionSink.SubmitToolPermission 时按它定位 waiter。
//
// Resolved 字段仅 EventToolPermissionResolved 时填：SubmitToolPermission 完成
// 反向投回后 emit 一帧带这些字段，让 service 把终态 patch 回 acc 里那条
// ToolPermissionRequestBlock，确保 turn finalize 时落盘正确。
type ToolPermissionEvent struct {
	RequestID string
	// ToolCallID 关联到 assistant 流里的 tool_use 块；race 时（control_request 比
	// tool_use 先到）允许为空。claudecode emitter 不填(走 RequestID 单 key 索引);
	// 新 sealed Event 直填。
	ToolCallID string
	ToolName   string
	Input      []byte // 原 control_request.input 透传字节；前端自己 JSON.parse
	// Resolved 状态：
	Resolved    bool
	Allowed     bool
	AlwaysAllow bool // 是否勾选了 "Allow for this session"
	// DenyReason 仅 Resolved=true && Allowed=false 时有意义,记录用户在审批卡上
	// 输入的拒绝理由(claudecode 把它塞进 PermissionResult.Message 回灌给 LLM)。
	// 老 RuntimeEvent 路径不回填(deny reason 直接走 SubmitToolPermission 入参);
	// 新 sealed Event 携带 reason 字段。
	DenyReason string
}

// ToolPermissionSink 由具体 backend runner 在 Run 启动时把 session-scoped 句柄
// 注入到 service；service 拿到前端 AnswerToolPermission 请求后调
// SubmitToolPermission 把决策投回该 session 当前阻塞的 control 协议。
//
// 各 backend 实现细节：
//   - claudecode：构造 PermissionResult 写一帧 control_response 到子进程 stdin；
//     allow 时 UpdatedInput 用原 input 解析的 map；alwaysAllow=true 时附加
//     updatedPermissions=[{type:"addRules", rules:[{toolName}], behavior:"allow",
//     destination:"session"}]，由 CLI SDK 自己维护后续 allow rules。
//
// denyReason 仅 allow=false 时生效：写入 PermissionResult.Message，CLI 把它
// 作为 tool_result 回灌给 LLM 让 AI 拿到具体反馈。空字符串走默认文案。
// allow=true 时忽略。
//
// 当前只有 claudecode 走这条；其它 backend 还没有等价工具审批协议，留接口扩展。
type ToolPermissionSink interface {
	SubmitToolPermission(ctx context.Context, sessionID int64, requestID string, allow, alwaysAllowSession bool, denyReason string) error
}

// AskUserQuestionEvent EventAskUserQuestion 携带。
//
// RequestID 是 backend 私有的回写句柄（claudecode = control_request.request_id），
// service 在调 AskAnswerSink.SubmitAnswer 时按它定位 waiter。
//
// ToolUseID 关联到 assistant 流里的 tool_use 块；race 时（control_request 比
// tool_use 先到）允许为空，前端 merge 时按 RequestID 占位。
type AskUserQuestionEvent struct {
	RequestID        string
	ToolUseID        string
	ParentToolCallID string
	Questions        []AskQuestion
	// Answered / Skipped / Answers 仅 EventAskUserQuestionAnswered 时填:
	// SubmitAnswer 完成后 emit 一帧带这些字段,让 service 层把终态 patch 回
	// acc 里那条 AskUserQuestionBlock,确保 turn finalize 时落盘正确。
	Answered bool
	Skipped  bool
	Answers  []AskAnswer
}

// AskAnswerSink 由具体 backend runner 在 Run 启动时把 session-scoped
// 句柄注入到 service；service 拿到前端 AnswerUserQuestion 请求后
// 调 SubmitAnswer 把答案投回该 session 当前阻塞的 control 协议。
//
// 各 backend 实现细节：
//   - claudecode：写一帧 control_response 到子进程 stdin，updatedInput.answers
//     按 question 文本聚合 csv labels（OtherAnswerLabel 已替换为 OtherText）
//   - codex：响应 app-server 的 item/tool/requestUserInput JSON-RPC 请求，
//     answers 按 Codex question id 聚合
//   - 内置 Agent：直接走 in-process chan
//
// skipped == true 表示用户跳过：各 backend 应回 deny 让 LLM 优雅看到拒答信号，
// 而不是 allow 一个空 map（会让 turn 静默挂死，见 hapi gotcha #4）。
type AskAnswerSink interface {
	SubmitAnswer(ctx context.Context, sessionID int64, requestID string, questions []AskQuestion, answers []AskAnswer, skipped bool) error
}

type PlanStep struct {
	Step   string
	Status string
}

// HistoryMessage builtin 用，CLI runner 忽略（它们的 history 在 cliagent Session 内）。
type HistoryMessage struct {
	Role   string
	Blocks []blocks.ContentBlock
}

// RunRequest 一次 Send 的入参。
type RunRequest struct {
	Backend   *agent_backend_entity.AgentBackend
	Provider  *llm_provider_entity.LLMProvider // 可为 nil（CLI 后端走自身 login）
	AgentID   int64                            // Agent 工作目录 key：<AppDataDir>/agents/<agentID>
	SessionID int64                            // chat_sessions.ID；provider session resume / builtin conv id 用
	// Cwd 非空时直接用作 runner 工作目录；为空时各 runner 回退到 AgentCwd(AgentID)
	// 兜底（保留老的 Agent 级目录行为）。chat_svc 在拼 RunRequest 时调
	// project_svc.ResolveSessionCwd 解析 project 维度的 cwd 注入此字段，避免
	// agentruntime 反向依赖 project_svc。
	Cwd               string
	SystemPrompt      string
	ProviderSessionID string // 空 = 新建；非空 = resume
	UserText          string
	UserBlocks        []blocks.ContentBlock // 非空时为本轮用户输入权威 blocks；UserText 仅保留文本索引/兼容
	History           []HistoryMessage      // 仅 builtin
	GatewayURL        string                // 关联 provider 的 CLI 后端要；builtin 不用
	GatewayToken      string                // 同上，一次性 token
	Compact           bool                  // Codex 原生 compact turn；不创建普通 user prompt

	// ForkAnchor 非空时 = "重新生成"路径：runner 应当把 provider 会话从 ForkAnchor
	// 之后的所有内容丢弃，再以 UserText 重发一次。
	//
	//   - claudecode: anchor 是 JSONL 里 user msg 的 parentUuid（来自上一轮 RunResult.UserAnchor）。
	//     runner 用 `--resume <ProviderSessionID> --resume-session-at <ForkAnchor> --fork-session`
	//     一次性完成 rewind + 新 turn；返回新的 ProviderSessionID（fork 出来的 sid）。
	//   - codex: anchor 是十进制 numTurns；runner 先调用 `thread/rollback`，
	//     再 resume 同一个 thread 发起新 turn。
	//   - builtin: 不使用 ForkAnchor；chat_svc 截 DB 后从 history 重建。
	ForkAnchor string

	// PermissionMode 仅 claudecode runner 在 spawn 新 CLI 子进程时透传给
	// --permission-mode；命中 LRU cache 的复用路径不重传（运行时切换走
	// PermissionModeSetter 接口的 control_request）。空串 = 不附 flag，让
	// pkg/claudecode args.go 的默认值 (acceptEdits) 生效。chat_svc.runTurn 从
	// chat_sessions.permission_mode 取值填入。
	PermissionMode string

	// CollaborationMode 仅 codex runner 使用，传给 app-server turn/start 的
	// collaborationMode.mode。合法值 default / plan；空串 = 不发字段，保留旧行为。
	CollaborationMode string

	// LLMProviderKey 是 desktop 端关联的 provider stable key（UUID）。
	// 远端 backend 场景下透传给 daemon，daemon 用 ProviderLookup.FindByKey
	// 解出本机 state 里的 provider，不需要 desktop 每个 turn 传 APIKey。
	// 本地 backend 场景下此字段不使用。
	LLMProviderKey string
}

// RunResult 由 runner 在事件流 close 后填充；chat_svc 在 drain 完 channel 后读。
type RunResult struct {
	ProviderSessionID string
	Usage             *provider.Usage
	StopErr           error

	// UserAnchor 是 provider 端对本轮 user prompt 的稳定标识，用于将来
	// 「重新生成 assistant N」时反向定位到 user N 之前的会话点。
	//
	// 仅 claudecode 当前会填（读 ~/.claude/projects/<slug>/<sid>.jsonl 的 message uuid）；
	// codex / builtin 留空 —— codex 的 thread/rollback 数量由 chat_svc 按 user 消息数计算；
	// builtin 不存在 provider 端 session，重生由 chat_messages 自己截断。
	UserAnchor string

	// Model 本轮实际调用的模型 id：
	//   - builtin：req.Provider.Model（chat_svc 已写过 message.Model，runner 不必再填）；
	//   - claudecode：从 system.init.model 抓，CLI login / 显式 --model 两条路径都走得通；
	//   - codex：取启动时的 spec.model，CLI 自身 login 时回退 Agentre 的默认模型。
	// 空字符串表示 runner 没办法可靠探到模型，调用方按自己的回退策略处理。
	Model string

	// ContextWindow 是 runtime 上报的模型上下文窗口大小（tokens）：
	//   - codex：从 thread/tokenUsage/updated 通知的 modelContextWindow 字段抓（部分版本 codex
	//     app-server 会推这个值）；
	//   - piagent：优先读 Pi RPC get_session_stats.contextUsage.contextWindow，
	//     再从 usage 帧的真实 model id 查 llmcatalog 兜底；
	//   - claudecode：通过 ContextWindowUpdated 事件实时上报，RunResult 通常留 0；
	//   - builtin：不报。
	// 0 表示 runner 没探到，chat_svc 用 provider.ContextWindow > cago catalog 兜底。
	// 非 0 时是这一层最权威的优先级（用户实际跑出来的窗口）。
	ContextWindow int

	// LaunchPermissionMode 是 runtime spawn CLI 子进程时实际下发的
	// --permission-mode 值（claudecode 专用,其它 runtime 留空）。runner 在
	// Run() 返回前同步填好,chat_svc 拿到后写入 session.PermissionModeAtLaunch。
	//
	// 历史:旧实现 runtime 直接 chat_repo.Session().UpdatePermissionModeAtLaunch,
	// 在 agentred daemon(不 bootstrap chat_repo)里触发 nil panic。把状态回吐
	// 给 chat_svc 持久化,避免 runtime 层反向依赖 repository。
	LaunchPermissionMode string
}

// Steerer is implemented by Runtimes that support mid-turn message
// injection. Steer is called by chat_svc.Enqueue when the user sends a
// follow-up message while a turn is in flight. queuedID is generated by
// chat_svc per Enqueue call and is the handle used by SteerCanceler to
// withdraw a specific queued message before it's consumed.
type Steerer interface {
	Steer(ctx context.Context, sessionID int64, queuedID, text string) error
}

// SteerCanceler is implemented by BackendRunners that allow withdrawing
// a previously enqueued Steer message before the runner has consumed it
// (i.e. the AI hasn't yet seen it). Defined as a separate interface so a
// runner can support Steer but not Cancel — codex is the canonical example:
// its turn/steer RPC is fire-and-forget with no withdraw verb.
//
// queuedID matches the ID passed to Steer.
//
//   - empty queuedID = clear all pending entries for sessionID.
//   - returns the list of IDs actually removed in FIFO order (empty slice
//     when queuedID was non-empty but no entry matched).
//   - ErrSteerNotFound when queuedID is non-empty and not in the queue
//     (already consumed by the runtime or never existed).
type SteerCanceler interface {
	CancelSteer(ctx context.Context, sessionID int64, queuedID string) ([]string, error)
}

// SteerDrainer is implemented by BackendRunners that keep an in-memory queue
// of mid-turn steer messages waiting to be consumed by the model. After a turn
// ends, chat_svc calls DrainPending to take any leftovers and replay them as
// the user prompt of an auto-started next turn — this replaces the old
// claudecode Stop-hook trick (which used decision=block to keep the model
// iterating, but surfaced as a red "Stop hook error" banner in Claude's TUI
// and dropped messages that arrived during the hook's own execution).
//
// Returns nil/empty when there is nothing pending or no active session for
// sessionID. Implementations must clear their internal queue as part of
// returning the slice — Drain semantics, not Peek.
//
// **Side effect (race-fix)**: when the returned slice is non-empty, the
// implementation MUST atomically mark the session as "still in turn" again,
// so user Steer calls arriving between this DrainPending and the next
// runner.Run still succeed (otherwise they fall through to Send → lock held
// → ChatSendInFlight and the message is dropped). Caller MUST follow up with
// another Run for the merged text — failing to do so leaves the session
// stuck in active state until LRU evict / next manual Send.
//
// codex deliberately does NOT implement SteerDrainer: its turn/steer RPC is
// fire-and-forget, so there is no agentre-side queue to drain.
type SteerDrainer interface {
	DrainPending(ctx context.Context, sessionID int64) []ConsumedSteer
}

// Aborter is implemented by BackendRunners that support stopping the in-flight
// turn (user clicks "停止"). chat_svc.Stop calls Abort to unblock the runner's
// blocking I/O — claudecode writes a control_request{interrupt} frame, codex
// sends turn/interrupt RPC, builtin cancels turnCtx. Implementations MUST be
// idempotent and MUST be safe to call concurrently with the runner's own
// drain goroutine.
//
// Returns ErrNoActiveTurn when there is no in-flight turn for sessionID
// (already finished, never started, or unknown to this runner). chat_svc
// translates that to code.ChatStopNoActive.
//
// Defined as a separate interface (like SteerCanceler) so a backend can be
// added without abort support if needed; all three current runners implement it.
type Aborter interface {
	Abort(ctx context.Context, sessionID int64) error
}

// PermissionModeSetter 由支持运行时切换 permission mode 的 runner 实现。
// 当前只有 claudecode runner 实现；codex / builtin 不参与权限门概念。
//
// 调用语义：mode 取 {default, acceptEdits, plan, bypassPermissions}，对 sessionID
// 对应的常驻 CLI 子进程下发 control_request{set_permission_mode}。CLI 在两个
// Turn 之间应用，后续 turn 立即生效。
//
// 返回错误：
//   - ErrNoActiveTurn：sessionID 在 runner 的 LRU 缓存里找不到（还没起过 turn
//     或子进程已被 evict）。chat_svc 翻译给上层。
//   - 其它 error：mode 非法 / I/O 失败 / CLI 拒绝。
type PermissionModeSetter interface {
	SetPermissionMode(ctx context.Context, sessionID int64, mode string) error
}

// Errors live in errors.go; registry / RuntimeFor / RegisterRuntime / SwapRuntimeForTest live in registry.go.
