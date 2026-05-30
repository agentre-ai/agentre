// Package handlers — runtime.go implements the runtime.* RPC surface,
// a 1:1 transparent proxy of agentruntime.Runtime + its 7 optional control
// sub-interfaces (Steerer / SteerCanceler / SteerDrainer / Aborter /
// PermissionModeSetter / AskAnswerSink / ToolPermissionSink). Each RPC
// method either delegates straight to the backend runtime or returns the
// agentruntime sentinel that the wire codec maps to the client.
//
// 单连接寿命内会有多个 sessionID（每个 chat session 一个）。runtime.run 启动一
// 个长连 fanout goroutine 把 backend events 推到 runtime.event notification,
// channel close 后再发 runtime.runResultDone 终态帧;之后 session 从 sessions
// map 摘除,gateway token revoke。所有控制方法（Steer / Abort / ...）按
// sessionID 查 backendType,再 type-assert backend runtime 拿对应的子接口,
// 没实现就返 ErrUnsupported,session 不在就返 ErrNoActiveTurn —— 两者都被
// wire.ToJSONRPCError 翻译成稳定 JSON-RPC error code 跨进程传递。
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/cago-frame/agents/agent/blocks"

	"agentre/internal/daemon/rpc"
	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// RuntimeDeps are the explicit constructor inputs for RuntimeHandlers. All
// fields are required except RuntimeFor (defaults to agentruntime.RuntimeFor).
type RuntimeDeps struct {
	Notify     NotifierPort
	Gateway    GatewayPort
	Lookup     LLMProviderLookupPort
	RuntimeFor func(agent_backend_entity.BackendType) agentruntime.Runtime
}

// RuntimeHandlers groups the runtime.* JSON-RPC handlers and owns the
// per-connection session map so control RPCs can resolve sessionID → backend.
type RuntimeHandlers struct {
	deps RuntimeDeps

	mu       sync.RWMutex
	sessions map[int64]runtimeSession
	// runtimeFor mirrors deps.RuntimeFor but is swappable at runtime via
	// SwapRuntimeFor (used by tests that need to flip the runtime registry
	// after a session is already live).
	runtimeFor func(agent_backend_entity.BackendType) agentruntime.Runtime
}

type runtimeSession struct {
	backendType  agent_backend_entity.BackendType
	gatewayToken string
}

// NewRuntimeHandlers wires the dependencies and prepares the session map.
func NewRuntimeHandlers(deps RuntimeDeps) *RuntimeHandlers {
	if deps.RuntimeFor == nil {
		deps.RuntimeFor = agentruntime.RuntimeFor
	}
	return &RuntimeHandlers{
		deps:       deps,
		sessions:   map[int64]runtimeSession{},
		runtimeFor: deps.RuntimeFor,
	}
}

// SwapRuntimeFor replaces the runtime lookup at runtime — test seam only.
// Production code should construct a new RuntimeHandlers instead of mutating.
func (h *RuntimeHandlers) SwapRuntimeFor(fn func(agent_backend_entity.BackendType) agentruntime.Runtime) {
	h.mu.Lock()
	h.runtimeFor = fn
	h.mu.Unlock()
}

// ── Capabilities ────────────────────────────────────────────────────────────

func (h *RuntimeHandlers) Capabilities(_ context.Context, p wire.CapabilitiesParams) (wire.CapabilitiesResult, error) {
	rt := h.lookupRuntimeByType(agent_backend_entity.BackendType(p.BackendType))
	if rt == nil {
		return wire.CapabilitiesResult{}, fmt.Errorf("no runtime registered for backend type %q", p.BackendType)
	}
	return wire.CapabilitiesResult{Capabilities: rt.Capabilities()}, nil
}

// ── Run ─────────────────────────────────────────────────────────────────────

func (h *RuntimeHandlers) Run(ctx context.Context, p wire.RunParams) (wire.RunAck, error) {
	var be agent_backend_entity.AgentBackend
	if err := json.Unmarshal(p.Backend, &be); err != nil {
		return wire.RunAck{}, fmt.Errorf("parse backend: %w", err)
	}
	bt := agent_backend_entity.BackendType(be.Type)
	if bt == agent_backend_entity.TypeBuiltin {
		return wire.RunAck{}, errors.New("builtin backend not supported in agentred")
	}

	rt := h.lookupRuntimeByType(bt)
	if rt == nil {
		return wire.RunAck{}, fmt.Errorf("backend %q not registered", be.Type)
	}

	// Provider / Gateway 由 daemon 自家解 —— wire 已不再携带客户端版本:
	//   - APIKey 由 daemon 本机 state 读取,不让 desktop 每个 turn 越线漂移;
	//   - GatewayURL 是 daemon 本机 127.0.0.1:<port>,对 daemon spawn 的 CLI 子
	//     进程可达;desktop 本机 URL 在 daemon 上拨不到。
	var provider *llm_provider_entity.LLMProvider
	if be.LLMProviderKey != "" && h.deps.Lookup != nil {
		pv, err := h.deps.Lookup.FindByKey(ctx, be.LLMProviderKey)
		if err != nil {
			return wire.RunAck{}, &rpc.Error{
				Code:    rpc.ErrProviderMissing.Code,
				Message: fmt.Sprintf("LLM provider %q not configured on remote daemon: %v", be.LLMProviderKey, err),
			}
		}
		provider = pv
	}

	var gatewayURL, gatewayToken string
	if provider != nil && h.deps.Gateway != nil {
		gatewayURL = h.deps.Gateway.URL()
		if gatewayURL != "" {
			tok, err := h.deps.Gateway.IssueToken(ctx, &be, time.Hour)
			if err != nil {
				return wire.RunAck{}, fmt.Errorf("gateway token: %w", err)
			}
			gatewayToken = tok
		}
	}

	history, err := decodeHistory(p.History)
	if err != nil {
		if gatewayToken != "" {
			h.deps.Gateway.RevokeToken(gatewayToken)
		}
		return wire.RunAck{}, fmt.Errorf("decode history: %w", err)
	}
	userBlocks, err := decodeUserBlocks(p.UserBlocks)
	if err != nil {
		if gatewayToken != "" {
			h.deps.Gateway.RevokeToken(gatewayToken)
		}
		return wire.RunAck{}, fmt.Errorf("decode user blocks: %w", err)
	}

	req := agentruntime.RunRequest{
		Backend:           &be,
		Provider:          provider,
		AgentID:           p.AgentID,
		SessionID:         p.SessionID,
		Cwd:               p.Cwd,
		SystemPrompt:      p.SystemPrompt,
		ProviderSessionID: p.ProviderSessionID,
		UserText:          p.UserText,
		UserBlocks:        userBlocks,
		History:           history,
		Compact:           p.Compact,
		GatewayURL:        gatewayURL,
		GatewayToken:      gatewayToken,
		ForkAnchor:        p.ForkAnchor,
		PermissionMode:    p.PermissionMode,
		CollaborationMode: p.CollaborationMode,
	}

	events, result, err := rt.Run(ctx, req)
	if err != nil {
		if gatewayToken != "" {
			h.deps.Gateway.RevokeToken(gatewayToken)
		}
		return wire.RunAck{}, err
	}

	h.register(p.SessionID, runtimeSession{backendType: bt, gatewayToken: gatewayToken})
	log.Printf("runtime.run: session started sid=%d backend=%s agentId=%d cwd=%q userTextLen=%d",
		p.SessionID, be.Type, p.AgentID, p.Cwd, len(p.UserText))
	go h.fanout(p.SessionID, events, result)
	// LaunchPermissionMode 由 runtime 在 Run 返回前同步填(claudecode 专用),
	// 通过 ack 回到客户端,chat_svc 在主进程侧落库到 session.PermissionModeAtLaunch。
	ack := wire.RunAck{SessionID: p.SessionID}
	if result != nil {
		ack.LaunchPermissionMode = result.LaunchPermissionMode
	}
	return ack, nil
}

// fanout 把 backend events channel 抽干推到 runtime.event,channel close 后再发
// runtime.runResultDone 终态帧。日志按事件 kind 计数,turn 结束时打一条汇总,
// 排查 stuck-turn / 漏事件时方便对账 client 端实际收到几条。
func (h *RuntimeHandlers) fanout(sid int64, ch <-chan agentruntime.Event, result *agentruntime.RunResult) {
	count := 0
	kindHist := map[string]int{}
	for ev := range ch {
		raw, err := json.Marshal(ev)
		if err != nil {
			log.Printf("runtime.event: marshal failed sid=%d kind=%T err=%v", sid, ev, err)
			continue
		}
		count++
		kind := reflect.TypeOf(ev).Name()
		kindHist[kind]++
		if perr := h.deps.Notify.Notify(wire.NotifyEvent, wire.EventFrame{
			SessionID: sid,
			Event:     json.RawMessage(raw),
		}); perr != nil {
			log.Printf("runtime.event: notify failed sid=%d n=%d kind=%s err=%v", sid, count, kind, perr)
		} else if !isNoisyEventKind(kind) {
			// text/thinking/usage 频率极高,kindHist 汇总即可,不逐条 log。
			log.Printf("runtime.event: sid=%d n=%d kind=%s payload=%s", sid, count, kind, string(raw))
		}
	}
	frame := runResultToFrame(sid, result)
	row, _ := h.unregister(sid)
	if row.gatewayToken != "" && h.deps.Gateway != nil {
		h.deps.Gateway.RevokeToken(row.gatewayToken)
	}
	if perr := h.deps.Notify.Notify(wire.NotifyRunResultDone, frame); perr != nil {
		log.Printf("runtime.runResultDone: notify failed sid=%d err=%v", sid, perr)
	}
	log.Printf("runtime.run: session ended sid=%d totalEvents=%d kinds=%v stopErrMsg=%q stopErrCode=%d",
		sid, count, kindHist, frame.StopErrMsg, frame.StopErrCode)
}

// isNoisyEventKind 标记单 turn 内可能上百次出现的事件类型,逐条 log 会刷屏。
// 它们仍计入 fanout 汇总(kindHist),只是不展开。
func isNoisyEventKind(kind string) bool {
	switch kind {
	case "TextDelta", "ThinkingDelta", "UsageUpdate", "ContextWindowUpdated":
		return true
	}
	return false
}

func runResultToFrame(sid int64, r *agentruntime.RunResult) wire.RunResultDoneFrame {
	if r == nil {
		return wire.RunResultDoneFrame{SessionID: sid}
	}
	f := wire.RunResultDoneFrame{
		SessionID:         sid,
		ProviderSessionID: r.ProviderSessionID,
		UserAnchor:        r.UserAnchor,
		Model:             r.Model,
		ContextWindow:     r.ContextWindow,
	}
	if r.Usage != nil {
		f.Usage = &wire.UsageWire{
			PromptTokens:        r.Usage.PromptTokens,
			CompletionTokens:    r.Usage.CompletionTokens,
			ReasoningTokens:     r.Usage.ReasoningTokens,
			CachedTokens:        r.Usage.CachedTokens,
			CacheCreationTokens: r.Usage.CacheCreationTokens,
			TotalTokens:         r.Usage.TotalTokens,
		}
	}
	if r.StopErr != nil {
		f.StopErrMsg = r.StopErr.Error()
		if rpcErr := wire.ToJSONRPCError(r.StopErr); rpcErr != nil {
			f.StopErrCode = rpcErr.Code
		}
	}
	return f
}

// ── Control RPCs (Steer / CancelSteer / DrainPending / Abort / SetPM /
//                  SubmitAnswer / SubmitToolPermission) ─────────────────────

func (h *RuntimeHandlers) Steer(ctx context.Context, p wire.SteerParams) (wire.OK, error) {
	rt, err := h.resolveSession(p.SessionID)
	if err != nil {
		return wire.OK{}, err
	}
	s, ok := rt.(agentruntime.Steerer)
	if !ok {
		return wire.OK{}, agentruntime.ErrUnsupported
	}
	if err := s.Steer(ctx, p.SessionID, p.QueuedID, p.Text); err != nil {
		return wire.OK{}, err
	}
	return wire.OK{}, nil
}

func (h *RuntimeHandlers) CancelSteer(ctx context.Context, p wire.CancelSteerParams) (wire.CancelSteerResult, error) {
	rt, err := h.resolveSession(p.SessionID)
	if err != nil {
		return wire.CancelSteerResult{}, err
	}
	c, ok := rt.(agentruntime.SteerCanceler)
	if !ok {
		return wire.CancelSteerResult{}, agentruntime.ErrUnsupported
	}
	removed, err := c.CancelSteer(ctx, p.SessionID, p.QueuedID)
	if err != nil {
		return wire.CancelSteerResult{}, err
	}
	return wire.CancelSteerResult{Removed: removed}, nil
}

func (h *RuntimeHandlers) DrainPending(ctx context.Context, p wire.DrainParams) (wire.DrainResult, error) {
	rt, err := h.resolveSession(p.SessionID)
	if err != nil {
		return wire.DrainResult{}, err
	}
	d, ok := rt.(agentruntime.SteerDrainer)
	if !ok {
		return wire.DrainResult{}, agentruntime.ErrUnsupported
	}
	steers := d.DrainPending(ctx, p.SessionID)
	return wire.DrainResult{Steers: steers}, nil
}

func (h *RuntimeHandlers) Abort(ctx context.Context, p wire.AbortParams) (wire.OK, error) {
	rt, err := h.resolveSession(p.SessionID)
	if err != nil {
		return wire.OK{}, err
	}
	a, ok := rt.(agentruntime.Aborter)
	if !ok {
		return wire.OK{}, agentruntime.ErrUnsupported
	}
	if err := a.Abort(ctx, p.SessionID); err != nil {
		return wire.OK{}, err
	}
	return wire.OK{}, nil
}

func (h *RuntimeHandlers) SetPermissionMode(ctx context.Context, p wire.SetPermissionModeParams) (wire.OK, error) {
	rt, err := h.resolveSession(p.SessionID)
	if err != nil {
		return wire.OK{}, err
	}
	m, ok := rt.(agentruntime.PermissionModeSetter)
	if !ok {
		return wire.OK{}, agentruntime.ErrUnsupported
	}
	if err := m.SetPermissionMode(ctx, p.SessionID, p.Mode); err != nil {
		return wire.OK{}, err
	}
	return wire.OK{}, nil
}

func (h *RuntimeHandlers) SubmitAnswer(ctx context.Context, p wire.SubmitAnswerParams) (wire.OK, error) {
	rt, err := h.resolveSession(p.SessionID)
	if err != nil {
		return wire.OK{}, err
	}
	s, ok := rt.(agentruntime.AskAnswerSink)
	if !ok {
		return wire.OK{}, agentruntime.ErrUnsupported
	}
	if err := s.SubmitAnswer(ctx, p.SessionID, p.RequestID, p.Questions, p.Answers, p.Skipped); err != nil {
		return wire.OK{}, err
	}
	return wire.OK{}, nil
}

func (h *RuntimeHandlers) SubmitToolPermission(ctx context.Context, p wire.SubmitToolPermissionParams) (wire.OK, error) {
	rt, err := h.resolveSession(p.SessionID)
	if err != nil {
		return wire.OK{}, err
	}
	s, ok := rt.(agentruntime.ToolPermissionSink)
	if !ok {
		return wire.OK{}, agentruntime.ErrUnsupported
	}
	if err := s.SubmitToolPermission(ctx, p.SessionID, p.RequestID, p.Allow, p.AlwaysAllowSession, p.DenyReason); err != nil {
		return wire.OK{}, err
	}
	return wire.OK{}, nil
}

func (h *RuntimeHandlers) GetGoal(ctx context.Context, p wire.GoalParams) (wire.GoalResult, error) {
	rt, req, release, err := h.resolveGoalController(ctx, p)
	if err != nil {
		return wire.GoalResult{}, err
	}
	defer release()
	g, ok := rt.(agentruntime.GoalController)
	if !ok {
		return wire.GoalResult{}, agentruntime.ErrUnsupported
	}
	goal, err := g.GetGoal(ctx, req)
	if err != nil {
		return wire.GoalResult{}, err
	}
	return wire.GoalResult{Goal: goal}, nil
}

func (h *RuntimeHandlers) SetGoal(ctx context.Context, p wire.GoalParams) (wire.GoalResult, error) {
	rt, req, release, err := h.resolveGoalController(ctx, p)
	if err != nil {
		return wire.GoalResult{}, err
	}
	defer release()
	g, ok := rt.(agentruntime.GoalController)
	if !ok {
		return wire.GoalResult{}, agentruntime.ErrUnsupported
	}
	goal, err := g.SetGoal(ctx, req)
	if err != nil {
		return wire.GoalResult{}, err
	}
	return wire.GoalResult{Goal: goal}, nil
}

func (h *RuntimeHandlers) ClearGoal(ctx context.Context, p wire.GoalParams) (wire.GoalClearResult, error) {
	rt, req, release, err := h.resolveGoalController(ctx, p)
	if err != nil {
		return wire.GoalClearResult{}, err
	}
	defer release()
	g, ok := rt.(agentruntime.GoalController)
	if !ok {
		return wire.GoalClearResult{}, agentruntime.ErrUnsupported
	}
	cleared, err := g.ClearGoal(ctx, req)
	if err != nil {
		return wire.GoalClearResult{}, err
	}
	return wire.GoalClearResult{Cleared: cleared}, nil
}

func (h *RuntimeHandlers) resolveGoalController(ctx context.Context, p wire.GoalParams) (agentruntime.Runtime, agentruntime.GoalRequest, func(), error) {
	req, err := goalRequestFromWire(p)
	if err != nil {
		return nil, agentruntime.GoalRequest{}, func() {}, err
	}
	if req.Backend != nil {
		release, err := h.hydrateGoalProvider(ctx, &req)
		if err != nil {
			return nil, agentruntime.GoalRequest{}, func() {}, err
		}
		rt := h.lookupRuntimeByType(agent_backend_entity.BackendType(req.Backend.Type))
		if rt == nil {
			release()
			return nil, agentruntime.GoalRequest{}, func() {}, agentruntime.ErrNoActiveTurn
		}
		return rt, req, release, nil
	}
	rt, err := h.resolveSession(p.SessionID)
	if err != nil {
		return nil, agentruntime.GoalRequest{}, func() {}, err
	}
	return rt, req, func() {}, nil
}

func (h *RuntimeHandlers) hydrateGoalProvider(ctx context.Context, req *agentruntime.GoalRequest) (func(), error) {
	release := func() {}
	if req.Backend == nil || req.Backend.LLMProviderKey == "" {
		return release, nil
	}
	if h.deps.Lookup != nil {
		pv, err := h.deps.Lookup.FindByKey(ctx, req.Backend.LLMProviderKey)
		if err != nil {
			return release, &rpc.Error{
				Code:    rpc.ErrProviderMissing.Code,
				Message: fmt.Sprintf("LLM provider %q not configured on remote daemon: %v", req.Backend.LLMProviderKey, err),
			}
		}
		req.Provider = pv
	}
	if req.Provider != nil && h.deps.Gateway != nil {
		req.GatewayURL = h.deps.Gateway.URL()
		if req.GatewayURL != "" {
			tok, err := h.deps.Gateway.IssueToken(ctx, req.Backend, time.Hour)
			if err != nil {
				return release, fmt.Errorf("gateway token: %w", err)
			}
			req.GatewayToken = tok
			release = func() { h.deps.Gateway.RevokeToken(tok) }
		}
	}
	return release, nil
}

func goalRequestFromWire(p wire.GoalParams) (agentruntime.GoalRequest, error) {
	var be *agent_backend_entity.AgentBackend
	if len(p.Backend) > 0 {
		var parsed agent_backend_entity.AgentBackend
		if err := json.Unmarshal(p.Backend, &parsed); err != nil {
			return agentruntime.GoalRequest{}, fmt.Errorf("parse backend: %w", err)
		}
		be = &parsed
	}
	return agentruntime.GoalRequest{
		SessionID:         p.SessionID,
		AgentID:           p.AgentID,
		ProviderSessionID: p.ProviderSessionID,
		Backend:           be,
		Cwd:               p.Cwd,
		Objective:         p.Objective,
		Status:            p.Status,
		TokenBudget:       p.TokenBudget,
	}, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func (h *RuntimeHandlers) lookupRuntimeByType(bt agent_backend_entity.BackendType) agentruntime.Runtime {
	h.mu.RLock()
	fn := h.runtimeFor
	h.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(bt)
}

func (h *RuntimeHandlers) resolveSession(sid int64) (agentruntime.Runtime, error) {
	h.mu.RLock()
	row, ok := h.sessions[sid]
	fn := h.runtimeFor
	h.mu.RUnlock()
	if !ok {
		return nil, agentruntime.ErrNoActiveTurn
	}
	if fn == nil {
		return nil, agentruntime.ErrNoActiveTurn
	}
	rt := fn(row.backendType)
	if rt == nil {
		return nil, agentruntime.ErrNoActiveTurn
	}
	return rt, nil
}

func (h *RuntimeHandlers) register(sid int64, row runtimeSession) {
	h.mu.Lock()
	h.sessions[sid] = row
	h.mu.Unlock()
}

func (h *RuntimeHandlers) unregister(sid int64) (runtimeSession, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	row, ok := h.sessions[sid]
	delete(h.sessions, sid)
	return row, ok
}

// decodeHistory turns wire HistoryMessage frames back into the agentruntime
// HistoryMessage shape (typed blocks via blocks.DecodeAll).
func decodeHistory(in []wire.HistoryMessageWire) ([]agentruntime.HistoryMessage, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]agentruntime.HistoryMessage, 0, len(in))
	for _, m := range in {
		bs, err := blocks.DecodeAll(m.Blocks)
		if err != nil {
			return nil, err
		}
		out = append(out, agentruntime.HistoryMessage{Role: m.Role, Blocks: bs})
	}
	return out, nil
}

func decodeUserBlocks(in []blocks.StoredBlock) ([]blocks.ContentBlock, error) {
	if len(in) == 0 {
		return nil, nil
	}
	return blocks.DecodeAll(in)
}
