package piagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/piagent/mcpbridge"
	pkgpi "github.com/agentre-ai/agentre/pkg/piagent"
)

var defaultRuntime = New()

func init() {
	agentruntime.RegisterRuntime(agent_backend_entity.TypePiAgent, defaultRuntime)
}

type activeSession struct {
	mu             sync.Mutex
	stream         steerStream
	interrupter    interruptable
	pending        []agentruntime.ConsumedSteer
	abortRequested bool
}

type Runtime struct {
	mu     sync.Mutex
	active map[int64]*activeSession
}

func New() *Runtime { return &Runtime{active: map[int64]*activeSession{}} }

func (r *Runtime) Capabilities() capability.Capabilities {
	return capability.Capabilities{
		Set: map[capability.Capability]bool{
			capability.CapSteer:               true,
			capability.CapAbort:               true,
			capability.CapImageInput:          true,
			capability.CapCompact:             true,
			capability.CapReportContextWindow: true,
			capability.CapMCPTools:            true,
		},
	}
}

func (r *Runtime) Run(ctx context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	if req.Backend == nil {
		return nil, nil, fmt.Errorf("agentruntime/runtimes/piagent: nil backend")
	}
	cwd := req.Cwd
	if cwd == "" {
		var err error
		cwd, err = agentruntime.AgentCwd(req.AgentID)
		if err != nil {
			return nil, nil, err
		}
	}
	env, err := BuildPiAgentEnv(req.Backend)
	if err != nil {
		logger.Ctx(ctx).Error("piagent runtime: BuildPiAgentEnv failed", zap.Int64("sessionID", req.SessionID), zap.Error(err))
		return nil, nil, err
	}
	// 绑定供应商：APIKey 空视为配置错误（消息只含 provider key，不含密钥）；
	// 否则把 AGENTRE_PI_API_KEY_* 注入本次子进程 env（密钥永不落盘）。
	if req.Provider != nil {
		if strings.TrimSpace(req.Provider.APIKey) == "" {
			return nil, nil, fmt.Errorf("piagent runtime: provider %q has empty APIKey", req.Provider.ProviderKey)
		}
		env = agentruntime.BuildPiAgentProviderEnv(env, req.Provider)
	}
	sess, err := sessionFactory(req, env, cwd)
	if err != nil {
		logger.Ctx(ctx).Error("piagent runtime: session factory failed", zap.Int64("sessionID", req.SessionID), zap.String("cwd", cwd), providerKeyField(req), zap.Error(err))
		return nil, nil, err
	}

	var s stream
	if req.Compact {
		s, err = sess.Compact(ctx)
	} else {
		s, err = sess.Stream(ctx, req.UserText, req.CollaborationMode, extractImages(req.UserBlocks))
	}
	if err != nil {
		_ = sess.Close(context.Background())
		if len(req.MCPServers) > 0 {
			_ = mcpbridge.RemoveConfig(req.SessionID)
		}
		return nil, nil, mapSessionError(err)
	}
	active := &activeSession{stream: sess.ActiveStream(), interrupter: sess.ActiveInterruptor()}
	r.register(req.SessionID, active)

	out := make(chan agentruntime.Event, 32)
	modelID := piResultModelPlaceholder(req)
	result := &agentruntime.RunResult{ProviderSessionID: sess.ID(), Model: modelID}
	logFields := make([]zap.Field, 0, 7)
	logFields = append(logFields,
		zap.Int64("sessionID", req.SessionID),
		zap.Int64("agentID", req.AgentID),
		zap.String("cwd", cwd),
		zap.String("providerSessionID", result.ProviderSessionID),
		zap.String("model", result.Model),
		zap.Bool("compact", req.Compact),
	)
	logFields = append(logFields, providerKeyField(req))
	logger.Ctx(ctx).Info("piagent runtime: turn starting", logFields...)

	go func() {
		defer close(out)
		defer r.unregister(req.SessionID)
		// turn 结束、pi 子进程退出后删除含 token 的会话配置（仅注入过才需要），
		// 避免凭证文件随会话数累积。注册在 sess.Close 之前 → LIFO 后于 Close 执行。
		if len(req.MCPServers) > 0 {
			defer func() { _ = mcpbridge.RemoveConfig(req.SessionID) }()
		}
		defer func() { _ = sess.Close(context.Background()) }()
		drainStream(ctx, req, cwd, s, out, result, active)
	}()
	return out, result, nil
}

func (r *Runtime) Abort(ctx context.Context, sessionID int64) error {
	r.mu.Lock()
	a := r.active[sessionID]
	r.mu.Unlock()
	if a == nil || a.interrupter == nil {
		return agentruntime.ErrNoActiveTurn
	}
	a.setAbortRequested(true)
	if err := a.interrupter.Interrupt(ctx); err != nil {
		a.setAbortRequested(false)
		return err
	}
	return nil
}

func (r *Runtime) Steer(ctx context.Context, sessionID int64, queuedID string, text string) error {
	r.mu.Lock()
	a := r.active[sessionID]
	r.mu.Unlock()
	if a == nil || a.stream == nil {
		return agentruntime.ErrNoActiveTurn
	}
	a.addPending(queuedID, text)
	if err := a.stream.Steer(ctx, text); err != nil {
		a.removePending(queuedID)
		return err
	}
	return nil
}

func (r *Runtime) register(sessionID int64, a *activeSession) {
	if sessionID <= 0 {
		return
	}
	r.mu.Lock()
	r.active[sessionID] = a
	r.mu.Unlock()
}

func (r *Runtime) unregister(sessionID int64) {
	if sessionID <= 0 {
		return
	}
	r.mu.Lock()
	delete(r.active, sessionID)
	r.mu.Unlock()
}

func (a *activeSession) setAbortRequested(requested bool) {
	a.mu.Lock()
	a.abortRequested = requested
	a.mu.Unlock()
}

func (a *activeSession) wasAbortRequested() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.abortRequested
}

func (a *activeSession) addPending(id, text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending = append(a.pending, agentruntime.ConsumedSteer{QueuedID: id, Text: text})
}

func (a *activeSession) removePending(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.pending[:0]
	for _, it := range a.pending {
		if it.QueuedID != id {
			out = append(out, it)
		}
	}
	a.pending = out
}

// consumePendingSteer 按 FIFO 找第一条文本匹配的 pending steer，命中即移除并返回。
// 只有 Pi 真正把 steer 注入对话（回显成 EventUserMessage）时才调用，避免助手输出
// 文字恰好等于 steer 文本造成误判。
func (a *activeSession) consumePendingSteer(text string) (agentruntime.ConsumedSteer, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, it := range a.pending {
		if it.Text == text {
			a.pending = append(a.pending[:i], a.pending[i+1:]...)
			return it, true
		}
	}
	return agentruntime.ConsumedSteer{}, false
}

func drainStream(ctx context.Context, req agentruntime.RunRequest, _ string, s stream, out chan<- agentruntime.Event, result *agentruntime.RunResult, active *activeSession) {
	var usage *provider.Usage
	var stopErr error
	trackers := make(map[string]*subagentTracker)
	for s.Next() {
		raw := s.Event()
		if raw.Kind == pkgpi.EventUserMessage {
			// Pi 把 steer 注入回显成 user message；对照 pending FIFO 命中即 consumed。
			if active != nil {
				if steer, ok := active.consumePendingSteer(raw.Text); ok {
					out <- agentruntime.SteerConsumed{Steers: []agentruntime.ConsumedSteer{steer}}
				}
			}
			continue
		}
		contextWindowChanged := raw.ContextWindow > 0 && raw.ContextWindow != result.ContextWindow
		if contextWindowChanged {
			// Usage snapshots also carry the authoritative Pi window so it survives
			// a missing/failed round-end stats refresh and is persisted by chat_svc.
			result.ContextWindow = raw.ContextWindow
		}
		if raw.Kind == pkgpi.EventContextWindow && !contextWindowChanged {
			// Context window 未变化时不重复向前端 emit patch。
			raw.ContextWindow = 0
		}
		if raw.Kind == pkgpi.EventDone {
			// pkg/piagent 用 EventDone 标记底层流终止；runtime 在 loop 结束后统一
			// emit agentruntime.Done，避免向 chat_svc 重复发送 message_end。
			continue
		}
		if handleSubagentToolEvent(raw, out, trackers) {
			continue
		}
		if raw.Model != "" {
			// Pi 在 usage 帧上报真实模型 id；piagent 不绑 provider，靠这里把模型回
			// 吐给 chat_svc（result.Model → assistantMsg.Model）。上下文窗口只采用
			// Pi RPC get_state / get_session_stats 返回值，避免自定义 provider 复用
			// 公共模型名时被 Agentre catalog 的同名模型元数据错误覆盖。
			result.Model = raw.Model
		}
		events, u, err := translate(raw)
		for _, ev := range events {
			out <- ev
		}
		if u != nil {
			usage = u
		}
		if err != nil {
			stopErr = err
		}
	}
	if err := s.Err(); err != nil && stopErr == nil {
		stopErr = err
	}
	if active != nil && active.wasAbortRequested() {
		stopErr = agentruntime.ErrAborted
	}
	if usage != nil {
		result.Usage = usage
	}
	if stopErr != nil {
		stopErr = mapSessionError(stopErr)
		result.StopErr = stopErr
		if errors.Is(stopErr, agentruntime.ErrAborted) {
			finalizeAbortedSubagents(out, trackers)
		} else {
			finalizeIncompleteSubagents(out, trackers, true)
		}
		logPiFailureDiagnostics(ctx, req, s)
		logger.Ctx(ctx).Warn("piagent.drainStream: turn failed", piTurnLogFields(req, result, stopErr)...)
		out <- agentruntime.ErrorEvent{Err: stopErr}
		return
	}
	finalizeIncompleteSubagents(out, trackers, false)
	logger.Ctx(ctx).Info("piagent.drainStream: turn done", piTurnLogFields(req, result, nil)...)
	out <- agentruntime.Done{}
}

func finalizeAbortedSubagents(out chan<- agentruntime.Event, trackers map[string]*subagentTracker) {
	finalizeTrackedSubagents(out, trackers, func(tracker *subagentTracker) bool {
		return tracker.abort()
	})
}

func finalizeIncompleteSubagents(out chan<- agentruntime.Event, trackers map[string]*subagentTracker, turnFailed bool) {
	finalizeTrackedSubagents(out, trackers, func(tracker *subagentTracker) bool {
		return tracker.finishIncomplete(turnFailed)
	})
}

func finalizeTrackedSubagents(out chan<- agentruntime.Event, trackers map[string]*subagentTracker, finalize func(*subagentTracker) bool) {
	toolCallIDs := make([]string, 0, len(trackers))
	for toolCallID := range trackers {
		toolCallIDs = append(toolCallIDs, toolCallID)
	}
	sort.Strings(toolCallIDs)
	for _, toolCallID := range toolCallIDs {
		tracker := trackers[toolCallID]
		if finalize(tracker) {
			out <- agentruntime.SubagentProgress{ToolCallID: toolCallID, Info: tracker.info()}
		}
		out <- agentruntime.SubagentDone{ToolCallID: toolCallID, Info: tracker.info()}
		delete(trackers, toolCallID)
	}
}

func handleSubagentToolEvent(raw pkgpi.Event, out chan<- agentruntime.Event, trackers map[string]*subagentTracker) bool {
	switch raw.Kind {
	case pkgpi.EventPreToolUse:
		tracker, spawn := defaultSubagentSelector.selectCandidate(raw.Tool.Name, raw.Tool.ID, raw.Tool.Input)
		call := agentruntime.ToolCall{
			ID: raw.Tool.ID, Name: raw.Tool.Name, Input: raw.Tool.Input,
			Canonical: recognizeCanonical(raw.Tool.Name, raw.Tool.Input),
		}
		if tracker != nil {
			call.Canonical = *spawn
			trackers[raw.Tool.ID] = tracker
		}
		out <- call
		if tracker != nil {
			out <- agentruntime.SubagentStarted{ToolCallID: raw.Tool.ID, Info: tracker.info()}
		}
		return true
	case pkgpi.EventToolUseUpdate:
		tracker := trackers[raw.Tool.ID]
		if tracker == nil {
			return true
		}
		events, changed := tracker.consumeUpdate(raw.Tool.PartialResult)
		for _, event := range events {
			out <- event
		}
		if changed {
			out <- agentruntime.SubagentProgress{ToolCallID: raw.Tool.ID, Info: tracker.info()}
		}
		return true
	case pkgpi.EventPostToolUse:
		tracker := trackers[raw.Tool.ID]
		if tracker == nil {
			return false
		}
		events, changed := tracker.consumeFinal(raw.Tool.Details, raw.Tool.IsError, raw.Tool.Content)
		for _, event := range events {
			out <- event
		}
		if changed {
			out <- agentruntime.SubagentProgress{ToolCallID: raw.Tool.ID, Info: tracker.info()}
		}
		out <- agentruntime.SubagentDone{ToolCallID: raw.Tool.ID, Info: tracker.info()}
		delete(trackers, raw.Tool.ID)
		out <- agentruntime.ToolResult{ToolCallID: raw.Tool.ID, Content: raw.Tool.Content, IsError: raw.Tool.IsError}
		return true
	default:
		return false
	}
}

func mapSessionError(err error) error {
	if err == nil || !errors.Is(err, pkgpi.ErrSessionNotFound) {
		return err
	}
	return fmt.Errorf("%w: %w", agentruntime.ErrSessionNotFound, err)
}

type diagnosticsStream interface {
	Diagnostics() pkgpi.StreamDiagnostics
}

func logPiFailureDiagnostics(ctx context.Context, req agentruntime.RunRequest, s stream) {
	ds, ok := s.(diagnosticsStream)
	if !ok {
		return
	}
	d := ds.Diagnostics()
	if d.FinalErrorMessage == "" && d.FinalErrorFrame == "" && d.StderrTail == "" {
		return
	}
	fields := []zap.Field{
		zap.Int64("sessionID", req.SessionID),
		zap.Int64("agentID", req.AgentID),
		zap.Bool("compact", req.Compact),
	}
	if d.FinalErrorEventType != "" {
		fields = append(fields, zap.String("piEventType", d.FinalErrorEventType))
	}
	if d.FinalErrorStopReason != "" {
		fields = append(fields, zap.String("piStopReason", d.FinalErrorStopReason))
	}
	if d.FinalErrorMessage != "" {
		fields = append(fields, zap.Int("piErrorMessageBytes", len(d.FinalErrorMessage)))
	}
	if d.FinalErrorFrame != "" {
		fields = append(fields, zap.Int("piFinalErrorFrameBytes", len(d.FinalErrorFrame)))
	}
	if d.StderrTail != "" {
		fields = append(fields, zap.Int("piStderrBytes", len(d.StderrTail)))
	}
	logger.Ctx(ctx).Debug("piagent.logPiFailureDiagnostics: turn failed diagnostics", fields...)
}

func providerKeyField(req agentruntime.RunRequest) zap.Field {
	if req.Provider != nil {
		return zap.String("providerKey", req.Provider.ProviderKey)
	}
	return zap.Skip()
}

func piTurnLogFields(req agentruntime.RunRequest, result *agentruntime.RunResult, err error) []zap.Field {
	fields := []zap.Field{
		zap.Int64("sessionID", req.SessionID),
		zap.Int64("agentID", req.AgentID),
		zap.Bool("compact", req.Compact),
	}
	fields = append(fields, providerKeyField(req))
	if result != nil {
		fields = append(fields,
			zap.String("providerSessionID", result.ProviderSessionID),
			zap.Int("contextWindow", result.ContextWindow),
		)
		if result.Usage != nil {
			fields = append(fields,
				zap.Int("promptTokens", result.Usage.PromptTokens),
				zap.Int("completionTokens", result.Usage.CompletionTokens),
				zap.Int("cachedTokens", result.Usage.CachedTokens),
				zap.Int("cacheCreationTokens", result.Usage.CacheCreationTokens),
				zap.Int("totalInputTokens", result.Usage.PromptTokens+result.Usage.CachedTokens+result.Usage.CacheCreationTokens),
			)
		}
	}
	if err != nil {
		fields = append(fields,
			zap.String("errorClass", fmt.Sprintf("%T", err)),
			zap.Int("errorBytes", len(err.Error())),
		)
	}
	return fields
}
