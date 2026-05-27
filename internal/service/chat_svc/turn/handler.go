package turn

import (
	"context"

	"agentre/internal/pkg/agentruntime"
)

// Handler 处理一种 agentruntime.Event 类型。
//
// 参数:
//   - ctx:turn 的 context;handler 决定何时用 context.WithoutCancel 持久化(spec §1.4)
//   - ev:具体 Event 实例,handler 内做类型断言
//   - acc:累积器(text/thinking/blocks + mutateIndex)
//   - emit:Wails event 推送适配器
//   - view:canonical 投影(blocks → ChatBlock DTO)
//   - turnCtx:本轮 turn 上下文(assistantMsg / session / stream),持久化和 emit 需要
//
// Handler 不直接依赖 chat_repo;持久化由 turnCtx 携带的接口或 handler 内通过 ctx
// 值注入(具体接线见 chat_svc/chat.go runTurn)。
type Handler interface {
	Apply(ctx context.Context, ev agentruntime.Event, acc *Accumulator, emit Emitter, view View, turnCtx *TurnContext) error
}

// Emitter 抽象 chat_svc.emitter,handler 不直接依赖具体 emit 路径,便于单测。
//
// stream 是 Wails event 名(turnCtx.Stream 即可,handler 通常透传);event 是
// 任意 JSON-marshallable payload。
type Emitter interface {
	Emit(ctx context.Context, stream string, event any)
}

// View 提供 canonical 投影 + ChatBlock 构造能力;具体实现在 chat_svc/view 包。
// dispatcher 这一层只看接口,避免循环依赖。
type View interface {
	// ProjectCanonical 把 agentruntime.Event(目前主要是 ToolCall.Canonical)
	// 投影成 wire DTO 的 kind + payload;handler 拼 emit 时调。
	ProjectCanonical(ev agentruntime.Event) (kind string, payload any)
}

// TurnContext 本轮 turn 的 mutable 上下文,handler 通过这个写 assistantMsg 字段、
// 决定 stream name、必要时回写 session 字段。
//
// 字段为 any 是为了避免 turn 子包反向依赖 chat_entity / chat_repo;chat_svc 层
// 把具体类型填进去,handler 内按需类型断言。
type TurnContext struct {
	AssistantMsg any // *chat_entity.ChatMessage
	Session      any // *chat_entity.ChatSession
	Stream       string

	// BackendType 是当前 turn 跑的 runtime 类型("claudecode" / "codex" / "builtin"
	// 等,字符串值与 agent_backend_entity.BackendType 一致)。handler 装配
	// canonical.Actions 时按这个分支(plan_update 的 Codex 路径要装 [execute,
	// refine],Claude 路径要 nil)。chat_svc.newTurnContext 注入。
	BackendType string

	// LaunchPermissionMode 是 session.PermissionModeAtLaunch 快照(claudecode 专用)。
	// ExitPlanMode 审批卡的 actions 列表按这个分支:bypass launch → 第一项给 bypass,
	// 否则给 acceptEdits。handler 不需要再 reach session 实体。
	LaunchPermissionMode string

	// LastPlanWriteContent 本轮 turn 内最近一次 Write 到 *.claude/plans/*.md 的
	// content。claudecode v2.1.x 起 ExitPlanMode 的 input 是 {}(plan 文本通过
	// 先前的 Write 工具写到 ~/.claude/plans/<slug>.md),buildToolPermissionCanonical
	// 在 input["plan"] 为空时回退到这个字段。per-turn 单 goroutine 写入,无锁。
	LastPlanWriteContent string

	// Repo 提供持久化能力;avoid import cycle 用 any。具体 method set 由 chat_svc 注入。
	MessageUpdater MessageUpdater
	SessionUpdater SessionUpdater

	// SessionTransitioner 切换 session waiting / running 状态。UserAsk /
	// ToolPermission Request handler 调 MarkWaiting;Resolved handler 调 MarkRunning。
	// chat_svc 在 newTurnContext 时注入。
	SessionTransitioner SessionTransitioner
}

// MessageUpdater handler 在 UsageUpdate / Error 等场景下写 assistantMsg 走这条。
type MessageUpdater interface {
	Update(ctx context.Context, msg any) error
}

// SessionUpdater handler 在 PermissionModeChanged / ContextWindowUpdated 等场景下
// 写 session 字段走这条。
type SessionUpdater interface {
	Update(ctx context.Context, sess any) error
}

// SessionTransitioner 切 session 状态 — UserAskRequest/ToolPermissionRequest
// 进入 waiting,Resolved 出 waiting。chat_svc 现有 markSessionWaiting/Running
// 实现该接口。
type SessionTransitioner interface {
	MarkWaiting(ctx context.Context, sess any, stream string)
	MarkRunning(ctx context.Context, sess any, stream string)
}
