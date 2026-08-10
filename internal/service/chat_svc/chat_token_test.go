package chat_svc

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/httpgateway"
)

// recordingGateway is a TokenIssuer that records the TTLs it was asked to issue
// with, hands back a distinct token per call, and records revocations — enough
// to assert the chat hook token is permanent + stable per session.
type recordingGateway struct {
	mu        sync.Mutex
	issued    int
	ttls      []time.Duration
	revoked   []string
	issuedFor []string          // 每次签发时传入的 provider key
	rerouted  []string          // 每次「改路由不换 token」的调用
	routes    map[string]string // token → 当前路由到的 provider key
	state     string            // GatewayStatus.State；newRecordingGateway 默认 running
}

func (g *recordingGateway) IssueToken(_ context.Context, _ *agent_backend_entity.AgentBackend, ttl time.Duration) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.issued++
	g.ttls = append(g.ttls, ttl)
	return fmt.Sprintf("tok-%d", g.issued), nil
}

func (g *recordingGateway) IssueTokenFor(_ context.Context, _ *agent_backend_entity.AgentBackend, providerKey string, ttl time.Duration) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.issued++
	g.ttls = append(g.ttls, ttl)
	g.issuedFor = append(g.issuedFor, providerKey)
	tok := fmt.Sprintf("tok-%d", g.issued)
	g.routes[tok] = providerKey
	return tok, nil
}

func (g *recordingGateway) SetTokenProvider(token, providerKey string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	prev, ok := g.routes[token]
	if !ok {
		return "", false
	}
	g.routes[token] = providerKey
	g.rerouted = append(g.rerouted, token+"→"+providerKey)
	return prev, true
}

// routeOf 返回该 token 当前的路由目标（测试断言用）。
func (g *recordingGateway) routeOf(token string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.routes[token]
}

func newRecordingGateway() *recordingGateway {
	return &recordingGateway{routes: map[string]string{}, state: "running"}
}

// newStoppedGateway 是「网关已注入但没在跑」（用户在设置里停了本机网关 / 端口占用起不来）。
func newStoppedGateway() *recordingGateway {
	g := newRecordingGateway()
	g.state = "stopped"
	return g
}
func (g *recordingGateway) RevokeToken(t string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.revoked = append(g.revoked, t)
}
func (g *recordingGateway) URL() string { return "http://gw" }
func (g *recordingGateway) Status() httpgateway.GatewayStatus {
	return httpgateway.GatewayStatus{State: g.state}
}

func claudeBackendFixture() *agent_backend_entity.AgentBackend {
	return &agent_backend_entity.AgentBackend{ID: 7, Type: string(agent_backend_entity.TypeClaudeCode)}
}

// TestSignChatTokenFor_PermanentAndStablePerSession reproduces the root cause of
// "steer 等整轮才发出去 on long sessions": the gateway token is baked into the
// persistent claude subprocess at spawn and never refreshed on turn reuse, so it
// MUST be permanent (ttl=0) and stable across turns. A finite TTL (the old
// 15-minute chatTokenTTL) expires while the subprocess is still alive → the
// PostToolUse hook gets 401 on /hook/v1/inbox → the SteerInbox is never drained
// mid-turn.
func TestSignChatTokenFor_PermanentAndStablePerSession(t *testing.T) {
	gw := newRecordingGateway()
	s := &chatSvc{gateway: gw}
	be := claudeBackendFixture()

	url1, tok1 := s.signChatTokenFor(context.Background(), be, 42, "")
	_, tok2 := s.signChatTokenFor(context.Background(), be, 42, "") // next turn, same session

	if url1 == "" || tok1 == "" {
		t.Fatalf("expected signed url+token, got url=%q tok=%q", url1, tok1)
	}
	if tok1 != tok2 {
		t.Fatalf("hook token must be STABLE across turns for one session: turn1=%q turn2=%q", tok1, tok2)
	}
	if gw.issued != 1 {
		t.Fatalf("must mint exactly ONE token per session (reuse across turns); minted %d", gw.issued)
	}
	for _, ttl := range gw.ttls {
		if ttl != 0 {
			t.Fatalf("hook token must be PERMANENT (ttl=0) to outlive the cached subprocess; got ttl=%v", ttl)
		}
	}
}

// TestSignChatTokenFor_DistinctPerSession guards that the per-session cache keys
// correctly — different sessions must not share a token.
func TestSignChatTokenFor_DistinctPerSession(t *testing.T) {
	gw := newRecordingGateway()
	s := &chatSvc{gateway: gw}
	be := claudeBackendFixture()

	_, a := s.signChatTokenFor(context.Background(), be, 1, "")
	_, b := s.signChatTokenFor(context.Background(), be, 2, "")
	if a == b {
		t.Fatalf("distinct sessions must get distinct tokens: s1=%q s2=%q", a, b)
	}
}

// TestRevokeChatToken_RevokesAndEvicts: tearing a session down (Delete →
// CloseSession) revokes its permanent token and drops the cache entry, so a
// future same-id session re-mints a fresh one (no stale-token reuse, no leak).
func TestRevokeChatToken_RevokesAndEvicts(t *testing.T) {
	gw := newRecordingGateway()
	s := &chatSvc{gateway: gw}
	be := claudeBackendFixture()

	_, tok := s.signChatTokenFor(context.Background(), be, 42, "")
	s.revokeChatToken(42)

	if len(gw.revoked) != 1 || gw.revoked[0] != tok {
		t.Fatalf("revokeChatToken must revoke session token %q; revoked=%v", tok, gw.revoked)
	}
	_, tok2 := s.signChatTokenFor(context.Background(), be, 42, "")
	if tok2 == tok {
		t.Fatalf("after revoke the re-signed token must be fresh; got same %q", tok2)
	}
	if gw.issued != 2 {
		t.Fatalf("expected a re-mint after revoke; issued=%d", gw.issued)
	}
}

// TestSignChatTokenFor_IssuesForEffectiveProvider 钉死决策 3 的签发侧：首轮就按本轮的
// effective provider（会话 provider_key > agent 绑定）签 token，而不是按 agent 绑定 ——
// 否则会话选了 X、CLI 的请求还是打到 agent 绑定那家。
func TestSignChatTokenFor_IssuesForEffectiveProvider(t *testing.T) {
	gw := newRecordingGateway()
	s := &chatSvc{gateway: gw}
	be := claudeBackendFixture()
	be.LLMProviderKey = "agent-bound"

	_, tok := s.signChatTokenFor(context.Background(), be, 42, "session-picked")

	if got := gw.routeOf(tok); got != "session-picked" {
		t.Fatalf("token must route to the effective provider; routed to %q", got)
	}
	if len(gw.issuedFor) != 1 || gw.issuedFor[0] != "session-picked" {
		t.Fatalf("expected exactly one issue for the effective provider; issuedFor=%v", gw.issuedFor)
	}
}

// TestSignChatTokenFor_SwitchReroutesWithoutReissuing 钉死决策 3 的切换侧：会话换了供应
// 商后的下一轮，token **字符串必须不变**（它首轮就烤进了 CLI 子进程 env，重签 = 在跑的
// 子进程立刻 401，正是被修过的那次事故），改的只是它在网关里的路由目标。
func TestSignChatTokenFor_SwitchReroutesWithoutReissuing(t *testing.T) {
	gw := newRecordingGateway()
	s := &chatSvc{gateway: gw}
	be := claudeBackendFixture()
	be.LLMProviderKey = "agent-bound"

	_, tok1 := s.signChatTokenFor(context.Background(), be, 42, "agent-bound")
	_, tok2 := s.signChatTokenFor(context.Background(), be, 42, "session-picked") // 切换后的下一轮

	if tok1 != tok2 {
		t.Fatalf("provider switch must NOT change the session token: before=%q after=%q", tok1, tok2)
	}
	if gw.issued != 1 {
		t.Fatalf("provider switch must not re-issue a token; issued=%d", gw.issued)
	}
	if got := gw.routeOf(tok2); got != "session-picked" {
		t.Fatalf("existing token must be re-routed to the new provider; routed to %q", got)
	}
	if len(gw.rerouted) != 1 {
		t.Fatalf("expected exactly one re-route call; got %v", gw.rerouted)
	}

	// 同一供应商的续轮不再重复改路由（避免每轮都写一次 + 刷日志）。
	_, tok3 := s.signChatTokenFor(context.Background(), be, 42, "session-picked")
	if tok3 != tok1 {
		t.Fatalf("unchanged provider must keep the same token; got %q", tok3)
	}
	if got := gw.routeOf(tok3); got != "session-picked" {
		t.Fatalf("route must stay on the switched provider; routed to %q", got)
	}
}

// TestPrepareTurnRun_LocalTokenRoutesToTurnProvider 钉死桌面 turn 路径这一端：本地 CLI
// 后端组装 RunRequest 时，交给子进程的那个 gateway token 必须路由到**本轮真正要跑的那家
// 供应商**（turn 入口已按会话 provider_key 覆盖解析过的 prov），而不是 agent 绑定。
func TestPrepareTurnRun_LocalTokenRoutesToTurnProvider(t *testing.T) {
	gw := newRecordingGateway()
	s := &chatSvc{gateway: gw}
	runner := &directRunRunner{request: make(chan agentruntime.RunRequest, 1)}
	t.Cleanup(agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner))
	prevCwd := resolveCwdFn
	t.Cleanup(func() { resolveCwdFn = prevCwd })
	dir := t.TempDir()
	resolveCwdFn = func(context.Context, *chat_entity.Session) (string, error) { return dir, nil }

	sess := &chat_entity.Session{ID: 100, AgentID: 7, ProviderKey: "session-picked"}
	a := &agent_entity.Agent{ID: 7, AgentBackendID: 12}
	be := &agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "agent-bound",
	}
	prov := &llm_provider_entity.LLMProvider{
		ProviderKey: "session-picked", Type: string(llm_provider_entity.TypeAnthropic),
		Model: "claude-x", Status: consts.ACTIVE,
	}

	prepared, err := s.prepareTurnRun(context.Background(), sess, a, be, prov, nil, nil, "", false, false)
	require.NoError(t, err)
	require.NotNil(t, prepared)
	require.NotEmpty(t, prepared.req.GatewayToken, "本地 claudecode 仍要签 token")
	assert.Equal(t, "session-picked", gw.routeOf(prepared.req.GatewayToken),
		"token 必须路由到本轮的 provider，而不是 agent 绑定")
}

// TestPrepareTurnRun_SessionProviderNeedsRunningGateway 钉死规格「登录态与后端可用性」：
// 未绑供应商的 CLI 登录态会话选了一个 agentre 供应商接管时，「该轮起签发网关 token、装配
// 网关 env / config，请求经本机网关转发到所选供应商」—— 网关没在跑就装不上 ANTHROPIC_*
// / codex model_provider，子进程会**静默**退回 CLI 自身登录态、把这段对话打到用户没选的
// 那家上游。这一轮存在 effective provider 却拿不到网关，必须与「backend 已绑 provider 时
// resolveAgentBackend 直接报 ChatBackendGatewayUnavailable」同一口径地报错，不静默降级。
//
// 反向那一半是硬不变量 1：整轮压根没有 effective provider（真·CLI 登录态）时行为完全不变
// —— 照常起轮，只是不带网关参数。
func TestPrepareTurnRun_SessionProviderNeedsRunningGateway(t *testing.T) {
	newTurn := func(t *testing.T) (*chatSvc, *chat_entity.Session, *agent_entity.Agent, *agent_backend_entity.AgentBackend) {
		t.Helper()
		runner := &directRunRunner{request: make(chan agentruntime.RunRequest, 1)}
		t.Cleanup(agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner))
		prevCwd := resolveCwdFn
		t.Cleanup(func() { resolveCwdFn = prevCwd })
		dir := t.TempDir()
		resolveCwdFn = func(context.Context, *chat_entity.Session) (string, error) { return dir, nil }

		return &chatSvc{gateway: newStoppedGateway()},
			&chat_entity.Session{ID: 100, AgentID: 7},
			&agent_entity.Agent{ID: 7, AgentBackendID: 12},
			// 登录态：agent 这一档没绑任何供应商。
			&agent_backend_entity.AgentBackend{ID: 12, Type: string(agent_backend_entity.TypeClaudeCode)}
	}

	t.Run("会话选了供应商但网关没在跑 → 报错而不是退回 CLI 登录态", func(t *testing.T) {
		prov := &llm_provider_entity.LLMProvider{
			ProviderKey: "session-picked", Type: string(llm_provider_entity.TypeAnthropic),
			Model: "claude-x", Status: consts.ACTIVE,
		}
		s, sess, a, be := newTurn(t)
		sess.ProviderKey = "session-picked"

		_, err := s.prepareTurnRun(context.Background(), sess, a, be, prov, nil, nil, "", false, false)
		require.Error(t, err, "本轮有 effective provider 却没有网关，必须报错")
	})

	t.Run("真·CLI 登录态（无任何供应商）不受影响：照常起轮、不带网关参数", func(t *testing.T) {
		s, sess, a, be := newTurn(t)

		prepared, err := s.prepareTurnRun(context.Background(), sess, a, be, nil, nil, nil, "", false, false)
		require.NoError(t, err)
		require.NotNil(t, prepared)
		assert.Empty(t, prepared.req.GatewayToken)
		assert.Empty(t, prepared.req.GatewayURL)
	})
}

// TestPrepareTurnRun_PiAgentSessionProviderRunsWithoutGateway 是上面那条门控的边界：
// 「有 effective provider 就必须有网关」只对**把 LLM 流量指向本机网关**的后端成立
// （claudecode 的 ANTHROPIC_BASE_URL / codex 的 model_provider 都是启动期从网关派生的）。
// pi-agent 不走网关 —— 它把 provider.APIKey 直接注进子进程 env
// （agentruntime.BuildPiAgentProviderEnv），GatewayURL/Token 在整个 piagent runtime 里
// 没有任何消费点。所以网关没在跑时这一轮照样能正常执行，把它打成
// ChatBackendGatewayUnavailable 等于凭空失败一条本来跑得通的会话。
func TestPrepareTurnRun_PiAgentSessionProviderRunsWithoutGateway(t *testing.T) {
	runner := &directRunRunner{request: make(chan agentruntime.RunRequest, 1)}
	t.Cleanup(agentruntime.SwapRuntimeForTest(agent_backend_entity.TypePiAgent, runner))
	prevCwd := resolveCwdFn
	t.Cleanup(func() { resolveCwdFn = prevCwd })
	dir := t.TempDir()
	resolveCwdFn = func(context.Context, *chat_entity.Session) (string, error) { return dir, nil }

	s := &chatSvc{gateway: newStoppedGateway()}
	// 登录态 pi 后端（这一档没绑供应商）+ 会话自己选了一个 agentre 供应商。
	sess := &chat_entity.Session{ID: 100, AgentID: 7, ProviderKey: "session-picked"}
	a := &agent_entity.Agent{ID: 7, AgentBackendID: 12}
	be := &agent_backend_entity.AgentBackend{ID: 12, Type: string(agent_backend_entity.TypePiAgent)}
	prov := &llm_provider_entity.LLMProvider{
		ProviderKey: "session-picked", Type: string(llm_provider_entity.TypeAnthropic),
		Model: "claude-x", APIKey: "sk-x", Status: consts.ACTIVE,
	}

	prepared, err := s.prepareTurnRun(context.Background(), sess, a, be, prov, nil, nil, "", false, false)
	require.NoError(t, err, "pi 不经网关取 LLM，网关没在跑不该把这一轮打成失败")
	require.NotNil(t, prepared)
	require.NotNil(t, prepared.req.Provider)
	assert.Equal(t, "session-picked", prepared.req.Provider.ProviderKey,
		"所选供应商仍随 RunRequest 下发 —— pi 从它取 APIKey")
}
