package orch_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
)

//go:generate mockgen -source deps.go -destination mock_orch_svc/mock_deps.go

// EnsureOrchSessionInput 创建/复用一条编排会话的入参。
type EnsureOrchSessionInput struct {
	AgentID         int64
	ParentSessionID int64 // 0 = 根（Leader）会话
	ProjectID       int64
	RunID           int64
	Isolate         bool // true = 独立 git worktree
	// Title 会话标题种子文本（Leader=Goal、派发子会话=Brief、Ask 会话=问题）。
	// 编排会话是先建会话再发消息，走不到 chat_svc 首条消息自动起标题的分支，
	// 故在此显式传入, 否则标题落空、侧栏显示「(未命名会话)」。
	Title string
}

// TurnDone 一轮跑完的信号（结果文本另经 FinalAssistantText 取）。
type TurnDone struct {
	SessionID int64
	OK        bool
}

// ChatGateway 编排对 chat_svc 的最小依赖（生产用瘦适配器映射到 chat_svc.Chat()）。
type ChatGateway interface {
	EnsureOrchSession(ctx context.Context, in EnsureOrchSessionInput) (int64, error)
	SendAndForget(ctx context.Context, sessionID int64, text string) error
	// Enqueue 把文本 steer 进该会话**正在进行**的 turn（对方 busy 时用，复用 chat_svc.Enqueue）。
	Enqueue(ctx context.Context, sessionID int64, text string) error
	ObserveTurn(sessionID int64) (<-chan TurnDone, func())
	FinalAssistantText(ctx context.Context, sessionID int64) (string, error)
	// LatestAssistantText 取会话「当前/末条」assistant 文本(running 任务 peek;settled 用 FinalAssistantText)。
	LatestAssistantText(ctx context.Context, sessionID int64) (string, error)
	AgentStatus(ctx context.Context, sessionID int64) (string, error)
	// AbortTurn 尽力硬打断会话在跑的一轮(复用 chat_svc.Stop;无活跃 turn → 视作无害成功)。
	AbortTurn(ctx context.Context, sessionID int64) error
}

// AgentLookup 花名册查询（复用 agent_repo）。
type AgentLookup interface {
	Find(ctx context.Context, id int64) (*agent_entity.Agent, error)
	FindByName(ctx context.Context, name string) (*agent_entity.Agent, error)
	List(ctx context.Context) ([]*agent_entity.Agent, error)
}

// Emitter 向前端推事件（生产包 wails EventsEmit；测试可 nil）。
type Emitter interface {
	Emit(ctx context.Context, name string, payload any)
}

// ApprovalGateway 审批卡登记/决议(chat_svc 通用工具审批网关的窄投影)。
type ApprovalGateway interface {
	BeginToolApproval(ctx context.Context, sessionID int64, blk *blocks.ToolApprovalBlock) (<-chan bool, error)
	FinishToolApproval(ctx context.Context, sessionID int64, requestID, status, result string) error
}

// WorkflowReader 编排对流程库的最小依赖：按 ID 取已投影的流程正文(用于 CreateRun 快照)。
type WorkflowReader interface {
	FlowContentByID(ctx context.Context, id int64) (string, error)
}
