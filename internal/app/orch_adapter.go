package app

import (
	"context"
	"errors"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/cago-frame/cago/pkg/utils/httputils"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
)

// ──────────────────────────────────────────────────────────
// orchChatAdapter — orch_svc.ChatGateway → chat_svc.Chat()
// ──────────────────────────────────────────────────────────

// turnInfo stash — per-session 最近完成轮的 message ID + agent 状态。
type turnInfo struct {
	msgID  int64
	status string // "idle" | "error"
}

// orchChatAdapter 把 orch_svc.ChatGateway 的窄接口映射到 chat_svc 默认单例。
// 它维护一个 per-session stash，把 chat_svc 以 messageID 为键的 FinalAssistantText
// 桥接成 orch_svc 以 sessionID 为键的调用约定。
type orchChatAdapter struct {
	// last: sessionID(int64) → turnInfo
	last sync.Map
}

// EnsureOrchSession 创建编排子会话（run_id>0，不复用）。
// ParentSessionID 由 orch_svc 的 Task 树追踪，不传给 chat_svc。
// Isolate/worktree 暂不支持（chat_svc.EnsureSessionRequest 尚无对应字段），留作后续扩展。
func (a *orchChatAdapter) EnsureOrchSession(ctx context.Context, in orch_svc.EnsureOrchSessionInput) (int64, error) {
	resp, err := chat_svc.Chat().EnsureSession(ctx, &chat_svc.EnsureSessionRequest{
		Purpose:   chat_svc.SessionPurposeOrchChild,
		AgentID:   in.AgentID,
		ProjectID: in.ProjectID,
		RunID:     in.RunID,
	})
	if err != nil {
		return 0, err
	}
	return resp.SessionID, nil
}

// SendAndForget 非阻塞触发该会话下一轮。
// 当 chat_svc 返回 ChatSendInFlight（会话已有进行中的 turn），映射为
// orch_svc.ErrSessionBusy，让 orch.Ask 回退到 Enqueue（steer）路径。
func (a *orchChatAdapter) SendAndForget(ctx context.Context, sessionID int64, text string) error {
	_, err := chat_svc.Chat().Send(ctx, &chat_svc.SendRequest{
		SessionID:             sessionID,
		Text:                  text,
		EmitTurnStartedBypass: true,
	})
	if err != nil {
		var herr *httputils.Error
		if errors.As(err, &herr) && herr.Code == code.ChatSendInFlight {
			return orch_svc.ErrSessionBusy // 让 orch.Ask 回退 steer
		}
	}
	return err
}

// Enqueue 把文本 steer 进该会话正在进行的 turn（对方 busy 时用）。
func (a *orchChatAdapter) Enqueue(ctx context.Context, sessionID int64, text string) error {
	_, err := chat_svc.Chat().Enqueue(ctx, &chat_svc.EnqueueRequest{
		SessionID: sessionID,
		Text:      text,
	})
	return err
}

// ObserveTurn 订阅 sessionID 的下一次 turn 完成，把 chat_svc.TurnResult 转换成
// orch_svc.TurnDone，并在发出信号前把 (msgID, status) 写入 stash，确保
// watchCompletion 后续调用 FinalAssistantText/AgentStatus 时已经可见。
func (a *orchChatAdapter) ObserveTurn(sessionID int64) (<-chan orch_svc.TurnDone, func()) {
	src, cancel := chat_svc.Chat().ObserveTurn(sessionID)
	out := make(chan orch_svc.TurnDone, 1)
	go func() {
		defer close(out)
		for r := range src {
			status := "idle"
			if r.Err != nil || r.Aborted {
				status = "error"
			}
			// stash 写入必须在发送 TurnDone 之前，保证 happens-before。
			a.last.Store(sessionID, turnInfo{msgID: r.AssistantMessageID, status: status})
			out <- orch_svc.TurnDone{
				SessionID: sessionID,
				OK:        r.Err == nil && !r.Aborted,
			}
		}
	}()
	return out, cancel
}

// FinalAssistantText 用 stash 中的 messageID 查询 chat_svc（chat_svc 以 messageID 为键）。
func (a *orchChatAdapter) FinalAssistantText(ctx context.Context, sessionID int64) (string, error) {
	v, ok := a.last.Load(sessionID)
	if !ok {
		return "", nil
	}
	info := v.(turnInfo)
	return chat_svc.Chat().FinalAssistantText(ctx, info.msgID)
}

// AgentStatus 从 stash 读取最近完成轮的 agent 状态（"idle" | "error"）。
// stash 中尚无记录时默认返回 "idle"（表示尚未跑过，orch 的 watchCompletion 不会误判）。
func (a *orchChatAdapter) AgentStatus(_ context.Context, sessionID int64) (string, error) {
	v, ok := a.last.Load(sessionID)
	if !ok {
		return "idle", nil
	}
	return v.(turnInfo).status, nil
}

// ──────────────────────────────────────────────────────────
// orchAgentAdapter — orch_svc.AgentLookup → agent_repo.Agent()
// ──────────────────────────────────────────────────────────

type orchAgentAdapter struct{}

func (orchAgentAdapter) Find(ctx context.Context, id int64) (*agent_entity.Agent, error) {
	return agent_repo.Agent().Find(ctx, id)
}

func (orchAgentAdapter) FindByName(ctx context.Context, name string) (*agent_entity.Agent, error) {
	return agent_repo.Agent().FindByName(ctx, name)
}

func (orchAgentAdapter) List(ctx context.Context) ([]*agent_entity.Agent, error) {
	return agent_repo.Agent().List(ctx)
}

// ──────────────────────────────────────────────────────────
// orchEmitter — orch_svc.Emitter → wails EventsEmit
// ──────────────────────────────────────────────────────────

type orchEmitter struct{ a *App }

func (e orchEmitter) Emit(_ context.Context, name string, payload any) {
	wailsruntime.EventsEmit(e.a.ctx, name, payload)
}
