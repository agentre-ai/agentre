package piagent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/piagent/mcpbridge"
	"github.com/agentre-ai/agentre/internal/pkg/llmcatalog"
	pkgpi "github.com/agentre-ai/agentre/pkg/piagent"
)

var defaultRuntime = New()

func init() {
	agentruntime.RegisterRuntime(agent_backend_entity.TypePiAgent, defaultRuntime)
}

type activeSession struct {
	mu          sync.Mutex
	stream      steerStream
	interrupter interruptable
	pending     []agentruntime.ConsumedSteer
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
	sess, err := sessionFactory(req, env, cwd)
	if err != nil {
		logger.Ctx(ctx).Error("piagent runtime: session factory failed", zap.Int64("sessionID", req.SessionID), zap.String("cwd", cwd), zap.Error(err))
		return nil, nil, err
	}

	var s stream
	if req.Compact {
		s, err = sess.Compact(ctx)
	} else {
		s, err = sess.Stream(ctx, req.UserText, req.CollaborationMode, extractImages(req.UserBlocks))
	}
	if err != nil {
		return nil, nil, err
	}
	active := &activeSession{stream: sess.ActiveStream(), interrupter: sess.ActiveInterruptor()}
	r.register(req.SessionID, active)

	out := make(chan agentruntime.Event, 32)
	modelID := defaultModelForBackend(req.Backend)
	if req.Provider != nil && strings.TrimSpace(req.Provider.Model) != "" {
		modelID = strings.TrimSpace(req.Provider.Model)
	}
	result := &agentruntime.RunResult{ProviderSessionID: sess.ID(), Model: modelID}
	logger.Ctx(ctx).Info("piagent runtime: turn starting",
		zap.Int64("sessionID", req.SessionID),
		zap.Int64("agentID", req.AgentID),
		zap.String("cwd", cwd),
		zap.String("providerSessionID", result.ProviderSessionID),
		zap.String("model", result.Model),
		zap.Bool("compact", req.Compact),
	)

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
		if sid := s.SessionID(); sid != "" {
			result.ProviderSessionID = sid
		}
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
	return a.interrupter.Interrupt(ctx)
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

func contextWindowForModel(model string) int {
	info, ok := llmcatalog.Lookup(model)
	if !ok {
		return 0
	}
	return info.ContextWindow
}

func drainStream(ctx context.Context, req agentruntime.RunRequest, cwd string, s stream, out chan<- agentruntime.Event, result *agentruntime.RunResult, active *activeSession) {
	var usage *provider.Usage
	var stopErr error
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
		if raw.Kind == pkgpi.EventContextWindow {
			if raw.ContextWindow > 0 && raw.ContextWindow != result.ContextWindow {
				result.ContextWindow = raw.ContextWindow
			} else {
				// Context window 未变化时不重复向前端 emit patch。
				raw.ContextWindow = 0
			}
		}
		if raw.Kind == pkgpi.EventDone {
			// pkg/piagent 用 EventDone 标记底层流终止；runtime 在 loop 结束后统一
			// emit agentruntime.Done，避免向 chat_svc 重复发送 message_end。
			continue
		}
		if raw.Model != "" {
			// Pi 在 usage 帧上报真实模型 id；piagent 不绑 provider，靠这里把模型回
			// 吐给 chat_svc（result.Model → assistantMsg.Model）。同时用 Agentre
			// 宽容 catalog 查上下文窗口并实时上报，给前端 Composer 用量条提供分母。
			result.Model = raw.Model
			if cw := contextWindowForModel(raw.Model); cw > 0 && cw != result.ContextWindow {
				result.ContextWindow = cw
				out <- agentruntime.ContextWindowUpdated{Tokens: cw}
			}
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
	if usage != nil {
		result.Usage = usage
	}
	if sid := s.SessionID(); sid != "" {
		result.ProviderSessionID = sid
	}
	if stopErr != nil {
		result.StopErr = stopErr
		logPiFailureDiagnostics(ctx, req, cwd, s)
		logger.Ctx(ctx).Warn("piagent runtime: turn failed", piTurnLogFields(req, cwd, result, stopErr)...)
		out <- agentruntime.ErrorEvent{Err: stopErr}
		return
	}
	logger.Ctx(ctx).Info("piagent runtime: turn done", piTurnLogFields(req, cwd, result, nil)...)
	out <- agentruntime.Done{}
}

type diagnosticsStream interface {
	Diagnostics() pkgpi.StreamDiagnostics
}

func logPiFailureDiagnostics(ctx context.Context, req agentruntime.RunRequest, cwd string, s stream) {
	ds, ok := s.(diagnosticsStream)
	if !ok {
		return
	}
	d := ds.Diagnostics()
	if d.FinalErrorFrame == "" && d.StderrTail == "" {
		return
	}
	fields := []zap.Field{
		zap.Int64("sessionID", req.SessionID),
		zap.Int64("agentID", req.AgentID),
		zap.String("cwd", cwd),
		zap.Bool("compact", req.Compact),
	}
	if d.FinalErrorEventType != "" {
		fields = append(fields, zap.String("piEventType", d.FinalErrorEventType))
	}
	if d.FinalErrorStopReason != "" {
		fields = append(fields, zap.String("piStopReason", d.FinalErrorStopReason))
	}
	if d.FinalErrorMessage != "" {
		fields = append(fields, zap.String("piErrorMessage", d.FinalErrorMessage))
	}
	if d.FinalErrorFrame != "" {
		fields = append(fields, zap.String("piFinalErrorFrame", d.FinalErrorFrame))
	}
	if d.StderrTail != "" {
		fields = append(fields, zap.String("piStderrTail", d.StderrTail))
	}
	logger.Ctx(ctx).Debug("piagent runtime: turn failed diagnostics", fields...)
}

func piTurnLogFields(req agentruntime.RunRequest, cwd string, result *agentruntime.RunResult, err error) []zap.Field {
	fields := []zap.Field{
		zap.Int64("sessionID", req.SessionID),
		zap.Int64("agentID", req.AgentID),
		zap.String("cwd", cwd),
		zap.Bool("compact", req.Compact),
	}
	if result != nil {
		fields = append(fields,
			zap.String("providerSessionID", result.ProviderSessionID),
			zap.String("model", result.Model),
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
		fields = append(fields, zap.Error(err))
	}
	return fields
}
