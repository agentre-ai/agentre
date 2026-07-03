package orch_svc

import (
	"context"
	"errors"
	"fmt"
	"html"
	"time"

	"github.com/google/uuid"
)

var (
	errUnknownAsk      = errors.New("orch: 未知或已过期的 ask_id")
	errReplyForeignAsk = errors.New("orch: 你不是该提问的接收者")

	// ErrSessionBusy 目标会话正在跑 turn，SendAndForget 拒绝注入新消息。
	// Ask 收到此哨兵后回退到 Enqueue（steer 进当前 turn）。
	ErrSessionBusy = errors.New("orch: target session has an in-flight turn")
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
	run, err := s.runs.Find(ctx, from.RunID)
	if err != nil {
		return "", err
	}
	if run != nil && !run.IsAgentAllowed(target.ID, run.LeaderAgentID) {
		return "", errAgentNotAllowed
	}
	var projectID int64
	if run != nil {
		projectID = run.ProjectID
	}
	toSession, err := s.resolveOrCreateAgentSession(ctx, from.RunID, projectID, target.ID, question)
	if err != nil {
		return "", err
	}
	askID := uuid.NewString()
	env := askEnvelope{
		askID:         askID,
		runID:         from.RunID,
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

	// 取发问方 agent 名，用于 XML 属性 from（对方可见自己被哪个 agent 提问）。
	askerName := ""
	if a, _ := s.agents.Find(ctx, from.AgentID); a != nil {
		askerName = a.Name
	}

	// XML 注入：闭合标签是天然边界，busy steer 进对方当前 turn 时不被其输出污染。
	// askerName/question 来自 LLM 输出，可能含 < > & " → html.EscapeString 转义防破坏 XML 边界。
	// askID 是 UUID，无需转义。
	msg := fmt.Sprintf(
		`<peer_ask ask_id="%s" from="%s">%s</peer_ask>`+"\n"+`请调用 reply(ask_id="%s", answer=...) 回复。`,
		askID, html.EscapeString(askerName), html.EscapeString(question), askID,
	)
	if err := s.chat.SendAndForget(ctx, toSession, msg); err != nil {
		if errors.Is(err, ErrSessionBusy) {
			// 对方正在跑 turn → steer 进它当前 turn。
			if e2 := s.chat.Enqueue(ctx, toSession, msg); e2 != nil {
				return "", e2
			}
		} else {
			return "", err
		}
	}

	// 快照 emit（避免后续 goroutine 延迟读 s.emit 与 RegisterDeps 产生 data race）。
	emit := s.emit
	if emit != nil {
		emit.Emit(ctx, "orch:run:ask", map[string]any{
			"runId": from.RunID, "askId": askID,
			"askerAgentId": from.AgentID, "askerSessionId": fromSessionID,
			"targetAgentId": target.ID, "targetSessionId": toSession,
			"question": question,
		})
	}
	emitReply := func(answer string, timedOut bool) {
		if emit != nil {
			emit.Emit(ctx, "orch:run:reply", map[string]any{
				"runId": env.runID, "askId": askID, "answer": answer, "timedOut": timedOut,
			})
		}
	}
	select {
	case ans := <-env.reply:
		emitReply(ans, false)
		return ans, nil
	case <-ctx.Done():
		emitReply("", true)
		return "", ctx.Err()
	case <-timeAfter(s.approvalTimeout):
		emitReply("", true)
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
// title 仅在新建时用作会话标题种子(取提问内容)，复用已有会话时忽略。
// projectID 由调用方从已加载的 run 中提取（避免二次查库）。
func (s *orchSvc) resolveOrCreateAgentSession(ctx context.Context, runID, projectID, agentID int64, title string) (int64, error) {
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
	return s.chat.EnsureOrchSession(ctx, EnsureOrchSessionInput{AgentID: agentID, RunID: runID, Title: title, ProjectID: projectID})
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
