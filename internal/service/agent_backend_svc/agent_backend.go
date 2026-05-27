package agent_backend_svc

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/pkg/code"
	"agentre/internal/pkg/httpgateway"
	"agentre/internal/repository/agent_backend_repo"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/llm_provider_repo"
	"agentre/internal/service/remote_device_svc"
)

const (
	testProbeTimeout = 30 * time.Second
	fixedTestPrompt  = "Reply with the single word 'pong' and nothing else."
	testTokenTTL     = 60 * time.Second
)

// AgentBackendSvc Agent 后端应用服务。
type AgentBackendSvc interface {
	List(ctx context.Context, req *ListBackendsRequest) (*ListBackendsResponse, error)
	Create(ctx context.Context, req *CreateBackendRequest) (*CreateBackendResponse, error)
	Update(ctx context.Context, req *UpdateBackendRequest) (*UpdateBackendResponse, error)
	Delete(ctx context.Context, req *DeleteBackendRequest) (*DeleteBackendResponse, error)
	Test(ctx context.Context, req *TestBackendRequest) (*TestBackendResponse, error)
	CancelTest(ctx context.Context, req *CancelTestBackendRequest) (*CancelTestBackendResponse, error)
	ResolveCLIPath(ctx context.Context, req *ResolveCLIPathRequest) (*ResolveCLIPathResponse, error)
}

type agentBackendSvc struct {
	now     func() int64
	prober  Prober
	gateway httpgateway.TokenIssuer

	// remoteCLI 用于 device 非空场景拨远端 daemon 调 cli.* RPC。
	// nil → 走 realRemoteCLI 默认实现（dial → call → close）；单测注入 fake。
	remoteCLI remoteCLIPort

	// probes 维护「正在跑的测试」的 cancel 函数；key = 前端传入的 RequestID。
	// 用于实现 CancelTest：用户在 UI 上点取消时调 cancel，prober ctx 立刻 Done。
	probesMu sync.Mutex
	probes   map[string]context.CancelFunc
}

// 默认单例不预置 prober；Test() 在 s.prober == nil 时按 entity.Type 查
// proberRegistry。硬编码 builtinProber 会让其它 backend 错走 in-process 路径，
// s.prober 字段仅留给单测注入 mock 用。
var defaultAgentBackend AgentBackendSvc = &agentBackendSvc{
	now:    func() int64 { return time.Now().Unix() },
	probes: map[string]context.CancelFunc{},
}

// RegisterGateway 由 bootstrap 注入 httpgateway 单例。
func RegisterGateway(g httpgateway.TokenIssuer) {
	if s, ok := defaultAgentBackend.(*agentBackendSvc); ok {
		s.gateway = g
	}
}

// AgentBackend 取默认服务单例。
func AgentBackend() AgentBackendSvc { return defaultAgentBackend }

func (s *agentBackendSvc) List(ctx context.Context, _ *ListBackendsRequest) (*ListBackendsResponse, error) {
	rows, err := agent_backend_repo.AgentBackend().List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	counts, err := agent_repo.Agent().CountByBackends(ctx, ids)
	if err != nil {
		return nil, err
	}
	items := make([]*BackendItem, 0, len(rows))
	for _, row := range rows {
		// LLMProviderKey == "" 表示 claudecode/codex 后端走 CLI 自身登录，无需查 provider。
		var provider *llm_provider_entity.LLMProvider
		if row.LLMProviderKey != "" {
			p, err := llm_provider_repo.LLMProvider().FindByKey(ctx, row.LLMProviderKey)
			if err != nil {
				return nil, err
			}
			provider = p
		}
		item := toItem(ctx, row, provider)
		item.AgentCount = counts[row.ID]
		items = append(items, item)
	}
	return &ListBackendsResponse{Items: items}, nil
}

func (s *agentBackendSvc) Create(ctx context.Context, req *CreateBackendRequest) (*CreateBackendResponse, error) {
	now := s.now()
	b := &agent_backend_entity.AgentBackend{
		Type:                  strings.TrimSpace(req.Type),
		Name:                  strings.TrimSpace(req.Name),
		LLMProviderKey:        strings.TrimSpace(req.LLMProviderKey),
		CLIPath:               strings.TrimSpace(req.CLIPath),
		ModelRoutes:           strings.TrimSpace(req.ModelRoutes),
		Sandbox:               strings.TrimSpace(req.Sandbox),
		Approval:              strings.TrimSpace(req.Approval),
		EnvJSON:               strings.TrimSpace(req.EnvJSON),
		ReasoningEffort:       strings.TrimSpace(req.ReasoningEffort),
		DefaultPermissionMode: strings.TrimSpace(req.DefaultPermissionMode),
		DeviceID:              strings.TrimSpace(req.DeviceID),
		Status:                consts.ACTIVE,
		Createtime:            now,
		Updatetime:            now,
	}
	if err := b.Check(ctx); err != nil {
		return nil, err
	}
	if err := s.validateDeviceID(ctx, b.DeviceID); err != nil {
		return nil, err
	}

	dup, err := agent_backend_repo.AgentBackend().FindByName(ctx, b.Name)
	if err != nil {
		return nil, err
	}
	if dup != nil {
		return nil, i18n.NewError(ctx, code.AgentBackendNameDuplicated)
	}

	provider, err := s.resolveProviderForSave(ctx, b)
	if err != nil {
		return nil, err
	}
	if err := s.validateRouteProviders(ctx, b); err != nil {
		return nil, err
	}

	if err := agent_backend_repo.AgentBackend().Create(ctx, b); err != nil {
		return nil, err
	}
	return &CreateBackendResponse{Item: toItem(ctx, b, provider)}, nil
}

func (s *agentBackendSvc) Update(ctx context.Context, req *UpdateBackendRequest) (*UpdateBackendResponse, error) {
	existing, err := agent_backend_repo.AgentBackend().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, i18n.NewError(ctx, code.AgentBackendNotFound)
	}

	newName := strings.TrimSpace(req.Name)
	if newName != existing.Name {
		dup, err := agent_backend_repo.AgentBackend().FindByName(ctx, newName)
		if err != nil {
			return nil, err
		}
		if dup != nil && dup.ID != existing.ID {
			return nil, i18n.NewError(ctx, code.AgentBackendNameDuplicated)
		}
	}

	existing.Name = newName
	existing.LLMProviderKey = strings.TrimSpace(req.LLMProviderKey)
	existing.CLIPath = strings.TrimSpace(req.CLIPath)
	existing.ModelRoutes = strings.TrimSpace(req.ModelRoutes)
	existing.Sandbox = strings.TrimSpace(req.Sandbox)
	existing.Approval = strings.TrimSpace(req.Approval)
	existing.EnvJSON = strings.TrimSpace(req.EnvJSON)
	existing.ReasoningEffort = strings.TrimSpace(req.ReasoningEffort)
	existing.DefaultPermissionMode = strings.TrimSpace(req.DefaultPermissionMode)
	existing.DeviceID = strings.TrimSpace(req.DeviceID)
	existing.Updatetime = s.now()

	if err := existing.Check(ctx); err != nil {
		return nil, err
	}
	if err := s.validateDeviceID(ctx, existing.DeviceID); err != nil {
		return nil, err
	}

	provider, err := s.resolveProviderForSave(ctx, existing)
	if err != nil {
		return nil, err
	}
	if err := s.validateRouteProviders(ctx, existing); err != nil {
		return nil, err
	}

	if err := agent_backend_repo.AgentBackend().Update(ctx, existing); err != nil {
		return nil, err
	}
	return &UpdateBackendResponse{Item: toItem(ctx, existing, provider)}, nil
}

func (s *agentBackendSvc) Test(ctx context.Context, req *TestBackendRequest) (*TestBackendResponse, error) {
	entity, err := s.resolveBackendForTest(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := entity.Check(ctx); err != nil {
		return nil, err
	}
	// 远端 device → 不在本地装 deps / gateway / provider，由 daemon 自己装。
	// 主进程只负责拨号 + 转发参数 + 折叠结果。provider FK 校验也下放给 daemon，
	// 因为远端可能有自己的 provider 状态视图（如离线时本地 provider 表过期）。
	if did, ok, perr := parseRemoteDeviceID(entity.DeviceID); perr != nil {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	} else if ok {
		return s.probeRemote(ctx, entity, did)
	}
	// builtin 必须有 active provider；claudecode / codex 关联了 provider 则严格匹配类型，
	// 未关联时表示走 CLI 自身登录态，跳过 provider 校验。
	var matchedProvider *llm_provider_entity.LLMProvider
	if entity.IsBuiltin() {
		if _, err := s.requireActiveProvider(ctx, entity.LLMProviderKey); err != nil {
			return nil, err
		}
	} else if entity.LLMProviderKey != "" {
		p, err := s.requireMatchingProvider(ctx, entity)
		if err != nil {
			return nil, err
		}
		matchedProvider = p
	}

	// 单元测试用 s.prober 注入 fake 跳过 LLM；正常路径按 type 查询注册表。
	// 未注册的 backend type 返回 AgentBackendInvalidType。
	prober := s.prober
	if prober == nil {
		prober = proberFor(agent_backend_entity.BackendType(entity.Type))
	}
	if prober == nil {
		return nil, i18n.NewError(ctx, code.AgentBackendInvalidType)
	}

	deps := ProbeDeps{}
	// claudecode / codex 且关联了 provider → 经 gateway 走临时 token；
	// 未关联 provider → 不签 token，让 CLI 直接用 claude/codex login 状态。
	if !entity.IsBuiltin() && entity.LLMProviderKey != "" {
		if s.gateway == nil {
			return &TestBackendResponse{OK: false, Message: i18n.NewError(ctx, code.AgentBackendGatewayUnavailable).Error()}, nil
		}
		if st := s.gateway.Status(); st.State != "running" {
			return &TestBackendResponse{OK: false, Message: i18n.NewError(ctx, code.AgentBackendGatewayUnavailable).Error()}, nil
		}
		tok, err := s.gateway.IssueToken(ctx, entity, testTokenTTL)
		if err != nil {
			return &TestBackendResponse{OK: false, Message: err.Error()}, nil
		}
		defer s.gateway.RevokeToken(tok)
		deps.Token = tok
		deps.GatewayURL = s.gateway.URL()
		if matchedProvider != nil {
			deps.Model = matchedProvider.Model
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, testProbeTimeout)
	defer cancel()

	// 注册 cancel：前端 CancelTest 拿同一 RequestID 触发，prober ctx 立刻 Done。
	// RequestID 留空 → 不可中断（兼容自动化 / 旧前端）。
	if req.RequestID != "" {
		s.registerProbe(req.RequestID, cancel)
		defer s.unregisterProbe(req.RequestID)
	}

	start := time.Now()
	reply, err := prober.Run(probeCtx, entity, deps)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		msg := err.Error()
		switch {
		case errors.Is(err, context.Canceled):
			msg = "已取消"
		case errors.Is(err, context.DeadlineExceeded):
			msg = "测试超时（30s）"
		}
		return &TestBackendResponse{OK: false, Message: msg, LatencyMs: latency}, nil
	}
	return &TestBackendResponse{OK: true, Message: strings.TrimSpace(reply), LatencyMs: latency}, nil
}

// CancelTest 中断一个还在跑的 Test。
// 未知 RequestID 返回 Canceled=false 而不是错误：前端竞态（Test 已经返回但 cancel 慢半拍）属常态。
func (s *agentBackendSvc) CancelTest(_ context.Context, req *CancelTestBackendRequest) (*CancelTestBackendResponse, error) {
	if req == nil || req.RequestID == "" {
		return &CancelTestBackendResponse{Canceled: false}, nil
	}
	s.probesMu.Lock()
	cancel, ok := s.probes[req.RequestID]
	s.probesMu.Unlock()
	if !ok {
		return &CancelTestBackendResponse{Canceled: false}, nil
	}
	cancel()
	return &CancelTestBackendResponse{Canceled: true}, nil
}

func (s *agentBackendSvc) registerProbe(id string, cancel context.CancelFunc) {
	s.probesMu.Lock()
	defer s.probesMu.Unlock()
	if s.probes == nil {
		s.probes = map[string]context.CancelFunc{}
	}
	s.probes[id] = cancel
}

func (s *agentBackendSvc) unregisterProbe(id string) {
	s.probesMu.Lock()
	defer s.probesMu.Unlock()
	delete(s.probes, id)
}

// resolveBackendForTest 组装用于测试的 entity:
//   - ID>0: 取保存记录;UseDraft=true 时用 draft 覆盖
//   - ID==0: 直接从 draft 拼一个临时 entity
func (s *agentBackendSvc) resolveBackendForTest(ctx context.Context, req *TestBackendRequest) (*agent_backend_entity.AgentBackend, error) {
	var saved *agent_backend_entity.AgentBackend
	if req.ID > 0 {
		got, err := agent_backend_repo.AgentBackend().Find(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		if got == nil {
			return nil, i18n.NewError(ctx, code.AgentBackendNotFound)
		}
		saved = got
		if !req.UseDraft {
			return saved, nil
		}
	}
	out := &agent_backend_entity.AgentBackend{Status: consts.ACTIVE}
	if saved != nil {
		*out = *saved
	}
	if typ := strings.TrimSpace(req.Type); typ != "" {
		out.Type = typ
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		out.Name = name
	}
	if strings.TrimSpace(req.LLMProviderKey) != "" {
		out.LLMProviderKey = strings.TrimSpace(req.LLMProviderKey)
	}
	out.CLIPath = strings.TrimSpace(req.CLIPath)
	out.ModelRoutes = strings.TrimSpace(req.ModelRoutes)
	out.Sandbox = strings.TrimSpace(req.Sandbox)
	out.Approval = strings.TrimSpace(req.Approval)
	out.EnvJSON = strings.TrimSpace(req.EnvJSON)
	out.ReasoningEffort = strings.TrimSpace(req.ReasoningEffort)
	out.DefaultPermissionMode = strings.TrimSpace(req.DefaultPermissionMode)
	return out, nil
}

func (s *agentBackendSvc) Delete(ctx context.Context, req *DeleteBackendRequest) (*DeleteBackendResponse, error) {
	existing, err := agent_backend_repo.AgentBackend().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, i18n.NewError(ctx, code.AgentBackendNotFound)
	}
	inUse, err := agent_repo.Agent().ListByBackend(ctx, existing.ID)
	if err != nil {
		return nil, err
	}
	if len(inUse) > 0 {
		return nil, i18n.NewError(ctx, code.AgentBackendInUse)
	}
	if err := agent_backend_repo.AgentBackend().Delete(ctx, existing.ID); err != nil {
		return nil, err
	}
	return &DeleteBackendResponse{}, nil
}

// resolveProviderForSave 在 Create / Update 路径上统一处理 provider 关联：
//   - builtin 必须有 provider（entity.Check 已强制 LLMProviderKey 非空）。
//   - claudecode / codex 在 LLMProviderKey == "" 时表示走 CLI 自身登录，跳过 FindByKey。
//   - LLMProviderKey != "" 时要求严格匹配 BackendKind 的 provider 类型集合。
func (s *agentBackendSvc) resolveProviderForSave(ctx context.Context, b *agent_backend_entity.AgentBackend) (*llm_provider_entity.LLMProvider, error) {
	if b.LLMProviderKey == "" {
		return nil, nil
	}
	return s.requireMatchingProvider(ctx, b)
}

// requireActiveProvider 把「provider 必须存在且 active」的两次错误码合并到一处。
func (s *agentBackendSvc) requireActiveProvider(ctx context.Context, key string) (*llm_provider_entity.LLMProvider, error) {
	p, err := llm_provider_repo.LLMProvider().FindByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, i18n.NewError(ctx, code.AgentBackendLLMProviderNotFound)
	}
	if !p.IsActive() {
		return nil, i18n.NewError(ctx, code.AgentBackendLLMProviderInactive)
	}
	return p, nil
}

func (s *agentBackendSvc) requireMatchingProvider(ctx context.Context, b *agent_backend_entity.AgentBackend) (*llm_provider_entity.LLMProvider, error) {
	p, err := s.requireActiveProvider(ctx, b.LLMProviderKey)
	if err != nil {
		return nil, err
	}
	kind := b.Kind()
	if kind == nil {
		return nil, i18n.NewError(ctx, code.AgentBackendInvalidType)
	}
	if !kind.ProviderTypeMatch(llm_provider_entity.ProviderType(p.Type)) {
		return nil, i18n.NewError(ctx, code.AgentBackendProviderTypeMismatch)
	}
	return p, nil
}

func (s *agentBackendSvc) validateRouteProviders(ctx context.Context, b *agent_backend_entity.AgentBackend) error {
	routes, err := agent_backend_entity.ParseModelRoutes(b.ModelRoutes)
	if err != nil {
		return i18n.NewError(ctx, code.AgentBackendUnknownAlias)
	}
	if len(routes) == 0 {
		return nil
	}
	kind := b.Kind()
	if kind == nil {
		return i18n.NewError(ctx, code.AgentBackendInvalidType)
	}
	for _, providerKey := range routes {
		p, err := llm_provider_repo.LLMProvider().FindByKey(ctx, providerKey)
		if err != nil {
			return err
		}
		if p == nil || !p.IsActive() || !kind.ProviderTypeMatch(llm_provider_entity.ProviderType(p.Type)) {
			return i18n.NewError(ctx, code.AgentBackendAliasProviderInvalid)
		}
	}
	return nil
}

// toItem 把 entity + 关联 provider（可能为 nil）打平成前端 DTO。
// ctx 用于查询关联远端设备信息（DeviceName / Online）。
func toItem(ctx context.Context, b *agent_backend_entity.AgentBackend, p *llm_provider_entity.LLMProvider) *BackendItem {
	item := &BackendItem{
		ID:                    b.ID,
		Type:                  b.Type,
		Name:                  b.Name,
		LLMProviderKey:        b.LLMProviderKey,
		CLIPath:               b.CLIPath,
		ModelRoutes:           b.ModelRoutes,
		Sandbox:               b.Sandbox,
		Approval:              b.Approval,
		EnvJSON:               b.EnvJSON,
		ReasoningEffort:       b.ReasoningEffort,
		DefaultPermissionMode: b.DefaultPermissionMode,
		DeviceID:              b.DeviceID,
		Createtime:            b.Createtime,
		Updatetime:            b.Updatetime,
	}
	if p != nil {
		item.LLMProviderName = p.Name
		item.LLMProviderType = p.Type
		item.LLMProviderModel = p.Model
		item.LLMProviderActive = p.IsActive()
	}
	if id, ok := b.DeviceIDInt(); ok {
		if dv, err := remote_device_svc.Default().Get(ctx, id); err == nil && dv != nil {
			item.DeviceName = dv.Name
			item.Online = dv.Online
		}
	}
	return item
}

// validateDeviceID 校验 device_id 引用的远端设备存在且未删除。
// 空串 = 本地，跳过校验。
func (s *agentBackendSvc) validateDeviceID(ctx context.Context, deviceID string) error {
	if deviceID == "" {
		return nil
	}
	id, err := strconv.ParseInt(deviceID, 10, 64)
	if err != nil {
		return i18n.NewError(ctx, code.AgentBackendInvalidDevice)
	}
	if _, err := remote_device_svc.Default().Get(ctx, id); err != nil {
		return i18n.NewError(ctx, code.AgentBackendInvalidDevice)
	}
	return nil
}
