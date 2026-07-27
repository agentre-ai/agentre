// Package openclaw implements the AgentRE runtime backed exclusively by the
// OpenClaw Gateway WebSocket RPC protocol. It never invokes OpenClaw through
// ACP, HTTP, CLI, or an embedded process.
package openclaw

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/openclawgateway"
)

type ConfigResolver interface {
	ResolveOpenClawConfig(ctx context.Context, backendID int64) (openclawgateway.Config, error)
}

type ConfigResolverFunc func(context.Context, int64) (openclawgateway.Config, error)

func (f ConfigResolverFunc) ResolveOpenClawConfig(ctx context.Context, backendID int64) (openclawgateway.Config, error) {
	return f(ctx, backendID)
}

type Runtime struct {
	resolverMu sync.RWMutex
	resolver   ConfigResolver

	mu     sync.RWMutex
	active map[int64]*activeTurn
}

var defaultRuntime = New(nil)

func init() {
	agentruntime.RegisterRuntime(agent_backend_entity.TypeOpenClaw, defaultRuntime)
}

func Default() *Runtime { return defaultRuntime }

func RegisterConfigResolver(resolver ConfigResolver) {
	defaultRuntime.SetConfigResolver(resolver)
}

func New(resolver ConfigResolver) *Runtime {
	return &Runtime{resolver: resolver, active: make(map[int64]*activeTurn)}
}

func (r *Runtime) SetConfigResolver(resolver ConfigResolver) {
	r.resolverMu.Lock()
	r.resolver = resolver
	r.resolverMu.Unlock()
}

func (r *Runtime) Capabilities() capability.Capabilities {
	return capability.Capabilities{Set: map[capability.Capability]bool{
		capability.CapAbort:        true,
		capability.CapExecApproval: true,
	}}
}

func (r *Runtime) Run(ctx context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	if req.Backend == nil || !req.Backend.IsOpenClaw() {
		return nil, nil, fmt.Errorf("openclaw runtime: OpenClaw backend is required")
	}
	if req.Backend.IsRemote() {
		return nil, nil, fmt.Errorf("openclaw runtime: remote secret enrollment is unavailable")
	}
	if req.SessionID <= 0 || req.Backend.ID <= 0 {
		return nil, nil, fmt.Errorf("openclaw runtime: backend and chat session IDs are required")
	}
	r.resolverMu.RLock()
	resolver := r.resolver
	r.resolverMu.RUnlock()
	if resolver == nil {
		return nil, nil, fmt.Errorf("openclaw runtime: config resolver is unavailable")
	}
	config, err := resolver.ResolveOpenClawConfig(ctx, req.Backend.ID)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(config.URL) == "" {
		config.URL = req.Backend.OpenClawGatewayURL
	}
	client, err := openclawgateway.NewClient(config)
	if err != nil {
		return nil, nil, err
	}
	hello, err := client.Start(ctx)
	if err != nil {
		client.Close()
		if ctx.Err() != nil {
			// 用户在握手期间点了停止 —— 是中止不是故障。
			return nil, nil, agentruntime.ErrAborted
		}
		return nil, nil, err
	}
	if err := openclawgateway.ValidateRuntimeFeatures(hello); err != nil {
		client.Close()
		return nil, nil, err
	}
	modelOverride := ""
	if hasGrantedScope(hello.Auth.Scopes, "operator.admin") {
		modelOverride = strings.TrimSpace(req.Backend.OpenClawDefaultModel)
	}
	// 开轮对账是尽力而为:待审批本来也会通过 exec.approval.requested 事件到达,而
	// exec.approval.list 在真实网关上只对创建该审批的连接/管理员可见,拿不到属常态。
	// 早期版本把这里当致命错误,结果用户在这一次 RPC 往返期间点停止 → ctx cancel →
	// 整轮以「故障」收场,会话卡死在 running。ctx 被取消要明确回 ErrAborted。
	initialApprovals, listErr := listExecApprovals(ctx, client)
	if listErr != nil {
		if ctx.Err() != nil {
			client.Close()
			return nil, nil, agentruntime.ErrAborted
		}
		initialApprovals = nil
	}
	// Start publishes the initial ready snapshot. Drain it here so every value
	// consumed by activeTurn means a reconnect and therefore requires state
	// reconciliation before accepting more events.
	select {
	case <-client.Ready():
	default:
	}

	sessionKey := strings.TrimSpace(req.ProviderSessionID)
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("agentre:%d:%d", req.Backend.ID, req.SessionID)
	}
	runID := uuid.NewString()
	params := struct {
		Message        string `json:"message"`
		AgentID        string `json:"agentId,omitempty"`
		Model          string `json:"model,omitempty"`
		SessionKey     string `json:"sessionKey"`
		Deliver        bool   `json:"deliver"`
		IdempotencyKey string `json:"idempotencyKey"`
	}{
		Message:        req.UserText,
		AgentID:        strings.TrimSpace(req.Backend.OpenClawAgentID),
		Model:          modelOverride,
		SessionKey:     sessionKey,
		Deliver:        false,
		IdempotencyKey: runID,
	}
	var ack struct {
		RunID      string `json:"runId"`
		Status     string `json:"status"`
		SessionKey string `json:"sessionKey"`
	}
	callErr := client.Call(ctx, "agent", params, &ack)
	if callErr == nil && strings.TrimSpace(ack.RunID) != "" {
		runID = strings.TrimSpace(ack.RunID)
	}
	if callErr == nil && strings.TrimSpace(ack.SessionKey) != "" {
		sessionKey = strings.TrimSpace(ack.SessionKey)
	}
	if callErr != nil && !errors.Is(callErr, openclawgateway.ErrDisconnected) &&
		!errors.Is(callErr, context.DeadlineExceeded) {
		client.Close()
		if ctx.Err() != nil {
			return nil, nil, agentruntime.ErrAborted
		}
		return nil, nil, callErr
	}

	result := &agentruntime.RunResult{
		ProviderSessionID: sessionKey,
		Model:             modelOverride,
	}
	active := &activeTurn{
		runtime:          r,
		ctx:              ctx,
		client:           client,
		sessionID:        req.SessionID,
		sessionKey:       sessionKey,
		runID:            runID,
		out:              make(chan agentruntime.Event, 64),
		result:           result,
		sessionDescribe:  slices.Contains(hello.Features.Methods, sessionDescribeMethod),
		approvals:        make(map[string]*approvalState),
		initialApprovals: initialApprovals,
	}
	if !r.register(active) {
		client.Close()
		return nil, nil, fmt.Errorf("openclaw runtime: session already has an active turn")
	}
	go active.consume()
	return active.out, result, nil
}

func hasGrantedScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}

func (r *Runtime) ResolveExecApproval(ctx context.Context, sessionID int64, approvalID, decision string) (agentruntime.ExecApprovalResolution, error) {
	r.mu.RLock()
	active := r.active[sessionID]
	r.mu.RUnlock()
	if active == nil {
		return agentruntime.ExecApprovalResolution{}, agentruntime.ErrNoActiveTurn
	}
	return active.resolveApproval(ctx, approvalID, decision)
}

func (r *Runtime) Abort(ctx context.Context, sessionID int64) error {
	r.mu.RLock()
	active := r.active[sessionID]
	r.mu.RUnlock()
	if active == nil {
		return agentruntime.ErrNoActiveTurn
	}
	return active.abort(ctx)
}

func (r *Runtime) register(active *activeTurn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active[active.sessionID] != nil {
		return false
	}
	r.active[active.sessionID] = active
	return true
}

func (r *Runtime) unregister(active *activeTurn) {
	r.mu.Lock()
	if r.active[active.sessionID] == active {
		delete(r.active, active.sessionID)
	}
	r.mu.Unlock()
}
