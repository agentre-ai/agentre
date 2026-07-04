// Package orch_svc 编排引擎：异步并行多发 + 完成回报续轮，让 agent 自行编排成树。
package orch_svc

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo"
)

// askEnvelope 记录一个在飞的 ask：ask_id、提问方会话、接收方 agent 与会话、答案通道。
type askEnvelope struct {
	askID         string
	runID         int64
	askerSession  int64
	targetAgentID int64
	targetSession int64
	reply         chan string
}

var (
	errLeaderNotFound  = errors.New("orch: leader agent not found")
	errAgentNotFound   = errors.New("orch: target agent not found")
	errAgentNotAllowed = errors.New("orch: target agent not in allowed set")
	errRunNotActive    = errors.New("orch: run not active")
	errForeignTask     = errors.New("orch: task not in this run")
)

type orchSvc struct {
	chat     ChatGateway
	agents   AgentLookup
	runs     orch_repo.RunRepo
	tasks    orch_repo.TaskRepo
	approval ApprovalGateway
	emit     Emitter
	wf       WorkflowReader

	gatewayBaseURL string

	approvalTimeout time.Duration

	// enqueue 异步触发钩子：nil = 使用 enqueueRun 真实实现；测试注入 no-op 避免竞态。
	enqueue func(runID int64, task *orch_entity.Task, brief string)

	mcp     *orchMCP
	mcpOnce sync.Once

	schedMu    sync.Mutex
	schedulers map[int64]*scheduler // runID -> scheduler
	defaultCap int                  // 测试覆盖并发上限（0=使用 min(16,NumCPU)）

	askMu    sync.Mutex
	pending  map[string]askEnvelope // ask_id -> 在飞的 ask（Task 10 用 ask_id 显式关联）
	askWaits map[int64]int64        // 死锁检测：askerSession -> targetSession（Task 13 用）
}

var defaultOrch = &orchSvc{
	approvalTimeout: 4 * time.Minute,
	schedulers:      map[int64]*scheduler{},
	pending:         map[string]askEnvelope{},
	askWaits:        map[int64]int64{},
}

// Default 取默认服务单例。
func Default() *orchSvc { return defaultOrch }

// RegisterDeps bootstrap 接线；测试注 mock。
func (s *orchSvc) RegisterDeps(chat ChatGateway, agents AgentLookup, runs orch_repo.RunRepo, tasks orch_repo.TaskRepo, approval ApprovalGateway, emit Emitter) {
	s.chat, s.agents, s.runs, s.tasks, s.approval, s.emit = chat, agents, runs, tasks, approval, emit
}

// RegisterWorkflowReader 注入流程库读取器(bootstrap/app 接线)；测试注 mock。
func (s *orchSvc) RegisterWorkflowReader(wr WorkflowReader) { s.wf = wr }

// SetGatewayBaseURL 由 bootstrap 在 gateway 起好后注入；mirror subagent_svc。
func (s *orchSvc) SetGatewayBaseURL(u string) { s.gatewayBaseURL = u }

// emitRunUpdated 向前端推送 orch:run:updated 事件，通知某 Run 的任务状态发生变化。
// emit 为 nil 时（部分单测场景）跳过，不影响测试隔离。
func (s *orchSvc) emitRunUpdated(ctx context.Context, runID int64) {
	if s.emit != nil {
		s.emit.Emit(ctx, "orch:run:updated", map[string]any{"runId": runID})
	}
}
