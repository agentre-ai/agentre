package orch_svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	errUnknownAsk      = errors.New("orch: 未知或已过期的 ask_id")
	errReplyForeignAsk = errors.New("orch: 你不是该提问的接收者")
)

// Ask 向另一个 agent 提问：把带 ask_id 的问题注入对方活会话（保留其上下文），阻塞等其 reply（≤4 分钟）。
func (s *orchSvc) Ask(ctx context.Context, fromSessionID int64, agentName, question string) (string, error) {
	from, err := s.tasks.FindBySession(ctx, fromSessionID)
	if err != nil || from == nil {
		return "", errRunNotActive
	}
	target, err := s.agents.FindByName(ctx, agentName)
	if err != nil || target == nil {
		return "", errAgentNotFound
	}
	toSession, err := s.resolveOrCreateAgentSession(ctx, from.RunID, target.ID)
	if err != nil {
		return "", err
	}
	askID := uuid.NewString()
	env := askEnvelope{
		askID:         askID,
		askerSession:  fromSessionID,
		targetAgentID: target.ID,
		targetSession: toSession,
		reply:         make(chan string, 1),
	}

	s.askMu.Lock()
	s.pending[askID] = env
	s.askMu.Unlock()
	s.recordAskWait(fromSessionID, toSession) // 死锁检测边（Task 13 读 askWaits）
	if cycle, found := s.detectAskCycle(ctx, from.RunID); found && s.emit != nil {
		s.emit.Emit(ctx, "orch:run:deadlock", map[string]any{"runId": from.RunID, "cycle": cycle})
	}
	defer func() {
		s.askMu.Lock()
		delete(s.pending, askID)
		s.askMu.Unlock()
		s.clearAskWait(fromSessionID)
	}()

	// 注入对方活会话：它带着自己的上下文回答，并被告知用 ask_id 调 reply。
	msg := fmt.Sprintf("【收到提问 ask_id=%s】%s\n请根据你自己的上下文，调用 reply(ask_id=\"%s\", answer=...) 回复。", askID, question, askID)
	if err := s.chat.SendAndForget(ctx, toSession, msg); err != nil {
		return "", err
	}

	select {
	case ans := <-env.reply:
		return ans, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timeAfter(s.approvalTimeout):
		return "", fmt.Errorf("orch.Ask: 等待 %s 回复超时", agentName)
	}
}

// Reply 目标 agent 用 ask_id 回复；校验回复者就是被提问者（防别的 agent 串答）。
func (s *orchSvc) Reply(_ context.Context, replierAgentID int64, askID, answer string) error {
	s.askMu.Lock()
	env, ok := s.pending[askID]
	s.askMu.Unlock()
	if !ok {
		return errUnknownAsk
	}
	if env.targetAgentID != replierAgentID {
		return errReplyForeignAsk
	}
	env.reply <- answer // 缓冲(1)，非阻塞；Ask 侧 select 接走
	return nil
}

// resolveOrCreateAgentSession 取目标 agent 在 Run 内的活会话（带上下文）；没有则新建一条。
func (s *orchSvc) resolveOrCreateAgentSession(ctx context.Context, runID, agentID int64) (int64, error) {
	rows, err := s.tasks.ListByRun(ctx, runID)
	if err != nil {
		return 0, err
	}
	var fallback int64
	for _, t := range rows {
		if t.AgentID != agentID {
			continue
		}
		if !t.IsTerminal() {
			return t.SessionID, nil // 优先活会话（上下文最全）
		}
		fallback = t.SessionID
	}
	if fallback != 0 {
		return fallback, nil // 退而求其次：该 agent 在本 Run 的历史会话（仍带上下文）
	}
	// 该 agent 在本 Run 还没有会话 → 建一条（无前置任务上下文，只能据 persona + 问题答）。
	return s.chat.EnsureOrchSession(ctx, EnsureOrchSessionInput{AgentID: agentID, RunID: runID})
}

// recordAskWait/clearAskWait 维护 ask 等待边（死锁检测用，Task 13 读 askWaits）。
func (s *orchSvc) recordAskWait(from, to int64) {
	s.askMu.Lock()
	s.askWaits[from] = to
	s.askMu.Unlock()
}

func (s *orchSvc) clearAskWait(from int64) {
	s.askMu.Lock()
	delete(s.askWaits, from)
	s.askMu.Unlock()
}

// timeAfter 包装 time.After，便于测试替身（避免真等 4 分钟）。
var timeAfter = time.After
