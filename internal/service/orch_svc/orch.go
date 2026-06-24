// Package orch_svc 编排引擎：异步并行多发 + 完成回报续轮，让 agent 自行编排成树。
package orch_svc

import (
	"errors"
	"sync"
	"time"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo"
)

// 占位类型，Task 9/10 引入真实定义后替换。
type askEnvelope struct{}
type scheduler struct{}

var (
	errLeaderNotFound = errors.New("orch: leader agent not found")
	errAgentNotFound  = errors.New("orch: target agent not found")
	errRunNotActive   = errors.New("orch: run not active")
)

type orchSvc struct {
	chat     ChatGateway
	agents   AgentLookup
	runs     orch_repo.RunRepo
	tasks    orch_repo.TaskRepo
	approval ApprovalGateway
	emit     Emitter

	approvalTimeout time.Duration

	// enqueue 异步触发钩子：nil = 使用 enqueueRun 真实实现；测试注入 no-op 避免竞态。
	enqueue func(runID int64, task *orch_entity.Task, brief string)

	mcp     *orchMCP
	mcpOnce sync.Once

	schedMu    sync.Mutex
	schedulers map[int64]*scheduler // runID -> scheduler

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
