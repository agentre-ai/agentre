package agent_backend_svc

import (
	"context"
	"encoding/base64"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cago-frame/cago/configs"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/pkg/httpgateway"
	"github.com/agentre-ai/agentre/internal/pkg/keychain"
	"github.com/agentre-ai/agentre/internal/pkg/openclawgateway"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
)

const (
	testProbeTimeout = 30 * time.Second
	fixedTestPrompt  = "Reply with the single word 'pong' and nothing else."
	testTokenTTL     = 60 * time.Second
)

var ErrOpenClawRemoteSecretUnavailable = errors.New("openclaw remote secret enrollment is unavailable")

// AgentBackendSvc Agent 后端应用服务。
type AgentBackendSvc interface {
	List(ctx context.Context, req *ListBackendsRequest) (*ListBackendsResponse, error)
	Create(ctx context.Context, req *CreateBackendRequest) (*CreateBackendResponse, error)
	CreateOpenClaw(ctx context.Context, req *CreateBackendRequest, token string) (*CreateBackendResponse, error)
	Update(ctx context.Context, req *UpdateBackendRequest) (*UpdateBackendResponse, error)
	UpdateOpenClaw(ctx context.Context, req *UpdateBackendRequest, token string, clearToken bool) (*UpdateBackendResponse, error)
	Delete(ctx context.Context, req *DeleteBackendRequest) (*DeleteBackendResponse, error)
	Test(ctx context.Context, req *TestBackendRequest) (*TestBackendResponse, error)
	TestOpenClaw(ctx context.Context, req *TestBackendRequest, token string) (*TestBackendResponse, error)
	CancelTest(ctx context.Context, req *CancelTestBackendRequest) (*CancelTestBackendResponse, error)
	ResolveCLIPath(ctx context.Context, req *ResolveCLIPathRequest) (*ResolveCLIPathResponse, error)
	ScanAndCreateAgentBackends(ctx context.Context, req *ScanAndCreateAgentBackendsRequest) (*ScanAndCreateAgentBackendsResponse, error)
}

type agentBackendSvc struct {
	now     func() int64
	prober  Prober
	gateway httpgateway.TokenIssuer
	secrets keychain.Keychain

	openClawProbe openClawProbeFunc
	identityMu    sync.Mutex

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
		item := s.toItem(ctx, row, provider)
		item.AgentCount = counts[row.ID]
		items = append(items, item)
	}
	return &ListBackendsResponse{Items: items}, nil
}

func (s *agentBackendSvc) Create(ctx context.Context, req *CreateBackendRequest) (*CreateBackendResponse, error) {
	return s.create(ctx, req, "", false)
}

// CreateOpenClaw receives token as a transient method argument instead of a
// Wails DTO field. The token is persisted only after the backend has a stable ID.
func (s *agentBackendSvc) CreateOpenClaw(ctx context.Context, req *CreateBackendRequest, token string) (*CreateBackendResponse, error) {
	return s.create(ctx, req, token, true)
}

func (s *agentBackendSvc) create(ctx context.Context, req *CreateBackendRequest, token string, requireOpenClaw bool) (*CreateBackendResponse, error) {
	if req == nil {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
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
		DefaultModel:          strings.TrimSpace(req.DefaultModel),
		OpenClawGatewayURL:    strings.TrimSpace(req.OpenClawGatewayURL),
		OpenClawAgentID:       strings.TrimSpace(req.OpenClawAgentID),
		OpenClawDefaultModel:  strings.TrimSpace(req.OpenClawDefaultModel),
		OpenClawSessionMode:   strings.TrimSpace(req.OpenClawSessionMode),
		DeviceID:              strings.TrimSpace(req.DeviceID),
		Status:                consts.ACTIVE,
		Createtime:            now,
		Updatetime:            now,
	}
	if requireOpenClaw && !b.IsOpenClaw() {
		return nil, i18n.NewError(ctx, code.AgentBackendInvalidType)
	}
	if b.IsOpenClaw() {
		normalized, err := agent_backend_entity.NormalizeOpenClawGatewayURL(b.OpenClawGatewayURL)
		if err != nil {
			return nil, i18n.NewError(ctx, code.InvalidParameter)
		}
		b.OpenClawGatewayURL = normalized
		if b.OpenClawSessionMode == "" {
			b.OpenClawSessionMode = agent_backend_entity.OpenClawSessionPerAgentRESession
		}
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
	if b.IsOpenClaw() && token != "" {
		store := s.secretStore()
		if store == nil {
			_ = agent_backend_repo.AgentBackend().Delete(ctx, b.ID)
			return nil, errors.New("openclaw secret store unavailable")
		}
		if err := store.Set(openClawTokenAccount(b.ID), token); err != nil {
			rollbackErr := agent_backend_repo.AgentBackend().Delete(ctx, b.ID)
			return nil, errors.Join(err, rollbackErr)
		}
	}
	return &CreateBackendResponse{Item: s.toItem(ctx, b, provider)}, nil
}

func (s *agentBackendSvc) Update(ctx context.Context, req *UpdateBackendRequest) (*UpdateBackendResponse, error) {
	return s.update(ctx, req, "", false, false)
}

// UpdateOpenClaw changes non-sensitive config and applies an explicit secret
// intent. Empty token with clearToken=false preserves the existing keychain item.
func (s *agentBackendSvc) UpdateOpenClaw(ctx context.Context, req *UpdateBackendRequest, token string, clearToken bool) (*UpdateBackendResponse, error) {
	return s.update(ctx, req, token, clearToken, true)
}

func (s *agentBackendSvc) update(ctx context.Context, req *UpdateBackendRequest, token string, clearToken, requireOpenClaw bool) (*UpdateBackendResponse, error) {
	if req == nil {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	existing, err := agent_backend_repo.AgentBackend().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, i18n.NewError(ctx, code.AgentBackendNotFound)
	}
	if requireOpenClaw && !existing.IsOpenClaw() {
		return nil, i18n.NewError(ctx, code.AgentBackendInvalidType)
	}
	before := *existing

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
	existing.DefaultModel = strings.TrimSpace(req.DefaultModel)
	existing.OpenClawGatewayURL = strings.TrimSpace(req.OpenClawGatewayURL)
	existing.OpenClawAgentID = strings.TrimSpace(req.OpenClawAgentID)
	existing.OpenClawDefaultModel = strings.TrimSpace(req.OpenClawDefaultModel)
	existing.OpenClawSessionMode = strings.TrimSpace(req.OpenClawSessionMode)
	existing.DeviceID = strings.TrimSpace(req.DeviceID)
	existing.Updatetime = s.now()
	if existing.IsOpenClaw() {
		normalized, err := agent_backend_entity.NormalizeOpenClawGatewayURL(existing.OpenClawGatewayURL)
		if err != nil {
			return nil, i18n.NewError(ctx, code.InvalidParameter)
		}
		existing.OpenClawGatewayURL = normalized
		if existing.OpenClawSessionMode == "" {
			existing.OpenClawSessionMode = agent_backend_entity.OpenClawSessionPerAgentRESession
		}
	}

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
	if existing.IsOpenClaw() && (token != "" || clearToken) {
		store := s.secretStore()
		if store == nil {
			rollbackErr := agent_backend_repo.AgentBackend().Update(ctx, &before)
			return nil, errors.Join(errors.New("openclaw secret store unavailable"), rollbackErr)
		}
		var secretErr error
		if clearToken {
			secretErr = store.Delete(openClawTokenAccount(existing.ID))
			if errors.Is(secretErr, keychain.ErrNotFound) {
				secretErr = nil
			}
		} else {
			secretErr = store.Set(openClawTokenAccount(existing.ID), token)
		}
		if secretErr != nil {
			rollbackErr := agent_backend_repo.AgentBackend().Update(ctx, &before)
			return nil, errors.Join(secretErr, rollbackErr)
		}
	}
	return &UpdateBackendResponse{Item: s.toItem(ctx, existing, provider)}, nil
}

func (s *agentBackendSvc) Test(ctx context.Context, req *TestBackendRequest) (*TestBackendResponse, error) {
	return s.test(ctx, req, "", false)
}

func (s *agentBackendSvc) TestOpenClaw(ctx context.Context, req *TestBackendRequest, token string) (*TestBackendResponse, error) {
	return s.test(ctx, req, token, true)
}

func (s *agentBackendSvc) test(ctx context.Context, req *TestBackendRequest, transientToken string, requireOpenClaw bool) (*TestBackendResponse, error) {
	if req == nil {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	entity, err := s.resolveBackendForTest(ctx, req)
	if err != nil {
		return nil, err
	}
	if requireOpenClaw && !entity.IsOpenClaw() {
		return nil, i18n.NewError(ctx, code.AgentBackendInvalidType)
	}
	// OpenClaw 草稿的配置问题走结构化 Code(前端本地化),不要塌成中文「参数错误」。
	if entity.IsOpenClaw() {
		if issue := openClawDraftIssue(entity); issue != nil {
			return issue, nil
		}
	}
	if err := entity.Check(ctx); err != nil {
		return nil, err
	}
	if entity.IsOpenClaw() {
		if entity.IsRemote() {
			return &TestBackendResponse{OK: false, Code: "OPENCLAW_REMOTE_SECRET_UNAVAILABLE"}, nil
		}
		return s.testOpenClaw(ctx, req, entity, transientToken)
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

type openClawProbeFunc func(
	ctx context.Context,
	config openclawgateway.Config,
	selection openclawgateway.ProbeSelection,
) (*openclawgateway.ProbeResult, error)

const openClawIdentityAccount = "agentre.openclaw.device.identity.seed"

func (s *agentBackendSvc) testOpenClaw(
	ctx context.Context,
	req *TestBackendRequest,
	backend *agent_backend_entity.AgentBackend,
	transientToken string,
) (*TestBackendResponse, error) {
	store := s.secretStore()
	if store == nil {
		return &TestBackendResponse{OK: false, Code: "OPENCLAW_SECRET_UNAVAILABLE"}, nil
	}
	token := transientToken
	if token == "" && backend.ID > 0 {
		stored, err := store.Get(openClawTokenAccount(backend.ID))
		switch {
		case err == nil:
			token = stored
		case errors.Is(err, keychain.ErrNotFound):
		default:
			return &TestBackendResponse{OK: false, Code: "OPENCLAW_SECRET_UNAVAILABLE", Message: err.Error()}, nil
		}
	}
	identity, err := s.openClawIdentity()
	if err != nil {
		return &TestBackendResponse{OK: false, Code: "OPENCLAW_SECRET_UNAVAILABLE", Message: err.Error()}, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, testProbeTimeout)
	defer cancel()
	if req.RequestID != "" {
		s.registerProbe(req.RequestID, cancel)
		defer s.unregisterProbe(req.RequestID)
	}
	probe := s.openClawProbe
	if probe == nil {
		probe = openclawgateway.Probe
	}
	start := time.Now()
	result, err := probe(probeCtx, openclawgateway.Config{
		URL:           backend.OpenClawGatewayURL,
		Token:         token,
		Identity:      identity,
		ClientVersion: configs.Version,
		Platform:      runtime.GOOS,
	}, openclawgateway.ProbeSelection{
		AgentID: backend.OpenClawAgentID,
		Model:   backend.OpenClawDefaultModel,
	})
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return &TestBackendResponse{
			OK: false, Code: openClawProbeErrorCode(err), Message: err.Error(), LatencyMs: latency,
		}, nil
	}
	response := &TestBackendResponse{
		OK:             true,
		LatencyMs:      latency,
		GatewayVersion: result.GatewayVersion,
		Protocol:       result.Protocol,
		GrantedScopes:  append([]string(nil), result.GrantedScopes...),
		Methods:        append([]string(nil), result.Methods...),
		Events:         append([]string(nil), result.Events...),
		OpenClawAgents: make([]OpenClawAgentOption, 0, len(result.Agents)),
		OpenClawModels: make([]OpenClawModelOption, 0, len(result.Models)),
	}
	for _, agent := range result.Agents {
		response.OpenClawAgents = append(response.OpenClawAgents, OpenClawAgentOption{
			ID: agent.ID, Name: agent.Name, PrimaryModel: agent.PrimaryModel,
			Fallbacks: append([]string(nil), agent.Fallbacks...), Default: agent.Default,
		})
	}
	for _, model := range result.Models {
		response.OpenClawModels = append(response.OpenClawModels, OpenClawModelOption{
			ID: model.ID, Name: model.Name, Provider: model.Provider, Available: model.Available,
		})
	}
	return response, nil
}

// openClawDraftIssue 把 OpenClaw 草稿的配置错误翻成结构化 Code。entity.Check 对所有
// 这些情况一律返回 code.InvalidParameter,前端只能拿到后端 i18n 的中文「参数错误」:
// 既分不清是 URL 还是名称有问题,还把中文糊进英文 UI。返回 nil 表示草稿本身没问题。
func openClawDraftIssue(backend *agent_backend_entity.AgentBackend) *TestBackendResponse {
	if backend == nil {
		return nil
	}
	issue := func(code string) *TestBackendResponse {
		return &TestBackendResponse{OK: false, Code: code}
	}
	if strings.TrimSpace(backend.Name) == "" {
		return issue("OPENCLAW_NAME_REQUIRED")
	}
	if _, err := agent_backend_entity.NormalizeOpenClawGatewayURL(backend.OpenClawGatewayURL); err != nil {
		switch {
		case errors.Is(err, agent_backend_entity.ErrOpenClawGatewayURLRequired):
			return issue("OPENCLAW_URL_REQUIRED")
		case errors.Is(err, agent_backend_entity.ErrOpenClawGatewayURLScheme):
			return issue("OPENCLAW_URL_SCHEME")
		case errors.Is(err, agent_backend_entity.ErrOpenClawGatewayURLHost):
			return issue("OPENCLAW_URL_HOST")
		case errors.Is(err, agent_backend_entity.ErrOpenClawGatewayURLCredentials):
			return issue("OPENCLAW_URL_CREDENTIALS")
		case errors.Is(err, agent_backend_entity.ErrOpenClawGatewayURLPlaintextRemote):
			return issue("OPENCLAW_URL_PLAINTEXT_REMOTE")
		default:
			return issue("OPENCLAW_URL_INVALID")
		}
	}
	if strings.TrimSpace(backend.OpenClawSessionMode) != agent_backend_entity.OpenClawSessionPerAgentRESession {
		return issue("OPENCLAW_SESSION_MODE_INVALID")
	}
	return nil
}

func (s *agentBackendSvc) openClawIdentity() (*openclawgateway.DeviceIdentity, error) {
	s.identityMu.Lock()
	defer s.identityMu.Unlock()
	store := s.secretStore()
	if store == nil {
		return nil, errors.New("openclaw secret store unavailable")
	}
	encoded, err := store.Get(openClawIdentityAccount)
	if err == nil {
		seed, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
		if decodeErr != nil {
			return nil, errors.New("openclaw device identity is invalid")
		}
		return openclawgateway.NewDeviceIdentityFromSeed(seed)
	}
	if !errors.Is(err, keychain.ErrNotFound) {
		return nil, err
	}
	identity, err := openclawgateway.GenerateDeviceIdentity()
	if err != nil {
		return nil, err
	}
	encoded = base64.RawURLEncoding.EncodeToString(identity.Seed())
	if err := store.Set(openClawIdentityAccount, encoded); err != nil {
		return nil, err
	}
	return identity, nil
}

// resolveOpenClawRuntimeConfig is the only boundary that turns persisted
// non-sensitive backend configuration plus keychain state into a live Gateway
// client config. The returned token must never cross DTO or daemon wire types.
func (s *agentBackendSvc) resolveOpenClawRuntimeConfig(ctx context.Context, backendID int64) (openclawgateway.Config, error) {
	backend, err := agent_backend_repo.AgentBackend().Find(ctx, backendID)
	if err != nil {
		return openclawgateway.Config{}, err
	}
	if backend == nil || !backend.IsOpenClaw() {
		return openclawgateway.Config{}, i18n.NewError(ctx, code.AgentBackendNotFound)
	}
	if backend.IsRemote() {
		return openclawgateway.Config{}, ErrOpenClawRemoteSecretUnavailable
	}
	if err := backend.Check(ctx); err != nil {
		return openclawgateway.Config{}, err
	}
	store := s.secretStore()
	if store == nil {
		return openclawgateway.Config{}, errors.New("openclaw secret store unavailable")
	}
	token, err := store.Get(openClawTokenAccount(backend.ID))
	if errors.Is(err, keychain.ErrNotFound) {
		token = ""
		err = nil
	}
	if err != nil {
		return openclawgateway.Config{}, err
	}
	identity, err := s.openClawIdentity()
	if err != nil {
		return openclawgateway.Config{}, err
	}
	return openclawgateway.Config{
		URL: backend.OpenClawGatewayURL, Token: token, Identity: identity,
		ClientVersion: configs.Version, Platform: runtime.GOOS,
	}, nil
}

// ResolveOpenClawRuntimeConfig is the bootstrap adapter for the default
// service. It is intentionally not exposed as an App/Wails method.
func ResolveOpenClawRuntimeConfig(ctx context.Context, backendID int64) (openclawgateway.Config, error) {
	service, ok := defaultAgentBackend.(*agentBackendSvc)
	if !ok || service == nil {
		return openclawgateway.Config{}, errors.New("openclaw backend service unavailable")
	}
	return service.resolveOpenClawRuntimeConfig(ctx, backendID)
}

// openClawGatewayAuthCodes 是网关直接给出的鉴权类 code。真实网关(2026.7.1-2)对
// token 不匹配回的却是 INVALID_REQUEST + "unauthorized: ..." —— 只按 code 匹配会
// 漏掉它,前端于是把原始协议串当文案显示。故同时看 details.reason 与 message。
var openClawGatewayAuthCodes = map[string]struct{}{
	"AUTH_FAILED": {}, "UNAUTHORIZED": {}, "FORBIDDEN": {},
}

func normalizeOpenClawRPCCode(rpcErr *openclawgateway.RPCError) string {
	rpcCode := strings.ToUpper(strings.TrimSpace(rpcErr.Code))
	reason := strings.ToLower(strings.TrimSpace(rpcErr.Reason))
	message := strings.ToLower(rpcErr.Message)
	switch {
	case rpcCode == "NOT_PAIRED" || reason == "not_paired":
		return "OPENCLAW_NOT_PAIRED"
	default:
	}
	if _, ok := openClawGatewayAuthCodes[rpcCode]; ok {
		return "AUTH_FAILED"
	}
	if reason == "unauthorized" || strings.HasPrefix(message, "unauthorized") {
		return "AUTH_FAILED"
	}
	if rpcCode == "" {
		return "OPENCLAW_CONNECTION_FAILED"
	}
	return rpcCode
}

func openClawProbeErrorCode(err error) string {
	var rpcErr *openclawgateway.RPCError
	switch {
	case errors.As(err, &rpcErr):
		return normalizeOpenClawRPCCode(rpcErr)
	case errors.Is(err, openclawgateway.ErrRequiredScopeMissing):
		return "OPENCLAW_SCOPE_MISSING"
	case errors.Is(err, openclawgateway.ErrProtocolMismatch):
		return "OPENCLAW_PROTOCOL_MISMATCH"
	case errors.Is(err, openclawgateway.ErrSelectedAgentNotFound):
		return "OPENCLAW_AGENT_NOT_FOUND"
	case errors.Is(err, openclawgateway.ErrSelectedModelNotFound):
		return "OPENCLAW_MODEL_NOT_FOUND"
	case errors.Is(err, openclawgateway.ErrRequiredMethodMissing):
		return "OPENCLAW_METHOD_MISSING"
	case errors.Is(err, openclawgateway.ErrRequiredEventMissing):
		return "OPENCLAW_EVENT_MISSING"
	case errors.Is(err, context.Canceled):
		return "OPENCLAW_PROBE_CANCELED"
	case errors.Is(err, context.DeadlineExceeded):
		return "OPENCLAW_PROBE_TIMEOUT"
	default:
		return "OPENCLAW_CONNECTION_FAILED"
	}
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
	out.DefaultModel = strings.TrimSpace(req.DefaultModel)
	out.OpenClawGatewayURL = strings.TrimSpace(req.OpenClawGatewayURL)
	out.OpenClawAgentID = strings.TrimSpace(req.OpenClawAgentID)
	out.OpenClawDefaultModel = strings.TrimSpace(req.OpenClawDefaultModel)
	out.OpenClawSessionMode = strings.TrimSpace(req.OpenClawSessionMode)
	if out.IsOpenClaw() {
		if out.OpenClawSessionMode == "" {
			out.OpenClawSessionMode = agent_backend_entity.OpenClawSessionPerAgentRESession
		}
		if normalized, err := agent_backend_entity.NormalizeOpenClawGatewayURL(out.OpenClawGatewayURL); err == nil {
			out.OpenClawGatewayURL = normalized
		}
	}
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
	var restoreToken string
	var removedToken bool
	if existing.IsOpenClaw() {
		store := s.secretStore()
		if store == nil {
			return nil, errors.New("openclaw secret store unavailable")
		}
		value, getErr := store.Get(openClawTokenAccount(existing.ID))
		switch {
		case getErr == nil:
			restoreToken = value
			if err := store.Delete(openClawTokenAccount(existing.ID)); err != nil && !errors.Is(err, keychain.ErrNotFound) {
				return nil, err
			}
			removedToken = true
		case errors.Is(getErr, keychain.ErrNotFound):
		default:
			return nil, getErr
		}
	}
	if err := agent_backend_repo.AgentBackend().Delete(ctx, existing.ID); err != nil {
		if removedToken {
			restoreErr := s.secretStore().Set(openClawTokenAccount(existing.ID), restoreToken)
			return nil, errors.Join(err, restoreErr)
		}
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
func (s *agentBackendSvc) toItem(ctx context.Context, b *agent_backend_entity.AgentBackend, p *llm_provider_entity.LLMProvider) *BackendItem {
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
		DefaultModel:          b.DefaultModel,
		OpenClawGatewayURL:    b.OpenClawGatewayURL,
		OpenClawAgentID:       b.OpenClawAgentID,
		OpenClawDefaultModel:  b.OpenClawDefaultModel,
		OpenClawSessionMode:   b.OpenClawSessionMode,
		DeviceID:              b.DeviceID,
		Createtime:            b.Createtime,
		Updatetime:            b.Updatetime,
	}
	if b.IsOpenClaw() {
		if store := s.secretStore(); store != nil {
			_, err := store.Get(openClawTokenAccount(b.ID))
			item.HasToken = err == nil
		}
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

func (s *agentBackendSvc) secretStore() keychain.Keychain {
	if s.secrets != nil {
		return s.secrets
	}
	return keychain.Default()
}

func openClawTokenAccount(backendID int64) string {
	return "agentre.openclaw.backend." + strconv.FormatInt(backendID, 10) + ".token"
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
