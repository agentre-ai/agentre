package codex

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"agentre/internal/pkg/agentruntime"
	"agentre/pkg/codex"
)

// defaultModelID 是 Agentre 在 codex CLI login 路径没有显式 provider model、
// 且 runtime 没法从事件里可靠观测模型时写入 RunResult.Model 的兜底值。
const defaultModelID = "gpt-5.5"

// cxStream 是 codex.Stream 的窄接口。
type cxStream interface {
	Next() bool
	Event() codex.Event
	SessionID() string
}

// cxSteerStream codex.Stream 实现的 turn/steer。
type cxSteerStream interface {
	Steer(ctx context.Context, text string) error
}

// cxInterruptable codex.Stream 实现的 turn/interrupt。Abort 发 RPC 让 app
// server 终止当前 turn —— 不杀子进程。
type cxInterruptable interface {
	Interrupt(ctx context.Context) error
}

type cxUserInputStream interface {
	SubmitUserInput(ctx context.Context, requestID string, answers map[string][]string) error
}

type cxClientAdapter struct {
	client *codex.Client
	sid    string

	streamMu sync.Mutex
	stream   *codex.Stream
}

func (a *cxClientAdapter) ID() string                      { return a.sid }
func (a *cxClientAdapter) Close(ctx context.Context) error { return a.client.Close(ctx) }

func (a *cxClientAdapter) Stream(ctx context.Context, prompt string, collaborationMode string) (cxStream, error) {
	var opts []codex.RunOption
	if a.sid != "" {
		opts = append(opts, codex.Resume(a.sid))
	}
	if strings.TrimSpace(collaborationMode) != "" {
		opts = append(opts, codex.RunCollaborationMode(codex.CollaborationMode(strings.TrimSpace(collaborationMode))))
	}
	s, err := a.client.Stream(ctx, prompt, opts...)
	if err != nil {
		return nil, err
	}
	a.streamMu.Lock()
	a.stream = s
	a.streamMu.Unlock()
	return s, nil
}

func (a *cxClientAdapter) Compact(ctx context.Context) (cxStream, error) {
	if strings.TrimSpace(a.sid) == "" {
		return nil, fmt.Errorf("agentruntime/runtimes/codex: missing provider session id for compact")
	}
	s, err := a.client.Compact(ctx, a.sid)
	if err != nil {
		return nil, err
	}
	a.streamMu.Lock()
	a.stream = s
	a.streamMu.Unlock()
	return s, nil
}

// RewindTo 走 thread/rollback,把 sid 推回 numTurns 之前的状态。anchor 是十进制
// numTurns(chat_svc 按 user msg count 算)。
func (a *cxClientAdapter) RewindTo(ctx context.Context, anchor string) (string, error) {
	if strings.TrimSpace(a.sid) == "" {
		return "", fmt.Errorf("agentruntime/runtimes/codex: missing provider session id for rollback")
	}
	numTurns, err := strconv.Atoi(strings.TrimSpace(anchor))
	if err != nil || numTurns <= 0 {
		return "", fmt.Errorf("agentruntime/runtimes/codex: invalid rollback anchor %q", anchor)
	}
	res, err := a.client.RollbackThread(ctx, a.sid, numTurns)
	if err != nil {
		return "", err
	}
	a.sid = res.ThreadID
	return a.sid, nil
}

func (a *cxClientAdapter) ActiveStream() cxSteerStream {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if a.stream == nil {
		return nil
	}
	return a.stream
}

func (a *cxClientAdapter) ActiveInterruptor() cxInterruptable {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if a.stream == nil {
		return nil
	}
	return a.stream
}

type cxSessionHandle interface {
	Close(context.Context) error
	ID() string
	Stream(ctx context.Context, prompt string, collaborationMode string) (cxStream, error)
	Compact(ctx context.Context) (cxStream, error)
	RewindTo(ctx context.Context, anchor string) (string, error)
	ActiveStream() cxSteerStream
	ActiveInterruptor() cxInterruptable
}

type codexLaunchSpec struct {
	binary       string
	cwd          string
	env          map[string]string
	model        string
	systemPrompt string
	sandbox      codex.SandboxMode
	approval     codex.ApprovalPolicy
	config       []string
}

func gatewayDeps(req agentruntime.RunRequest) CLIDeps {
	if req.Backend == nil || req.Backend.LLMProviderKey == "" {
		return CLIDeps{}
	}
	return CLIDeps{Token: req.GatewayToken, GatewayURL: req.GatewayURL}
}

func buildLaunchSpec(req agentruntime.RunRequest, env map[string]string, cwd string) codexLaunchSpec {
	binary := strings.TrimSpace(req.Backend.CLIPath)
	if binary == "" {
		binary = DefaultBinary()
	}
	spec := codexLaunchSpec{
		binary:       binary,
		cwd:          cwd,
		env:          env,
		systemPrompt: req.SystemPrompt,
		config:       BuildCodexConfig(gatewayDeps(req)),
	}
	if eff := reasoningEffortConfigValue(req.Backend.ReasoningEffort); eff != "" {
		spec.config = append(spec.config, `model_reasoning_effort="`+eff+`"`)
	}
	if req.Provider != nil {
		spec.model = strings.TrimSpace(req.Provider.Model)
	}
	if sb := strings.TrimSpace(req.Backend.Sandbox); sb != "" {
		spec.sandbox = codex.SandboxMode(sb)
	}
	if ap := strings.TrimSpace(req.Backend.Approval); ap != "" {
		spec.approval = codex.ApprovalPolicy(ap)
	}
	return spec
}

// reasoningEffortConfigValue 把落库的 reasoning_effort 映射为 codex CLI 配置值。
// 与顶层 clienv.go.codexReasoningEffortConfigValue 等价 —— low/medium/high/xhigh
// 直传,max 向下并到 high;off / 非法值 → "" 不下发。
func reasoningEffortConfigValue(s string) string {
	switch s {
	case "low", "medium", "high", "xhigh":
		return s
	case "max":
		return "high"
	default:
		return ""
	}
}

func (s codexLaunchSpec) options() []codex.Option {
	opts := []codex.Option{
		codex.WithBinary(s.binary),
		codex.WithCwd(s.cwd),
		codex.WithEnv(s.env),
		codex.WithSystemPrompt(s.systemPrompt),
	}
	if s.model != "" {
		opts = append(opts, codex.WithModel(s.model))
	}
	for _, c := range s.config {
		opts = append(opts, codex.WithConfig(c))
	}
	if s.sandbox != "" {
		opts = append(opts, codex.WithSandbox(s.sandbox))
	}
	if s.approval != "" {
		opts = append(opts, codex.WithApproval(s.approval))
	}
	return opts
}

// cxSessionFactory 生产路径;测试 SetSessionFactoryForTest 替换。
var cxSessionFactory = func(req agentruntime.RunRequest, env map[string]string, cwd string) (cxSessionHandle, error) {
	client := codex.New(buildLaunchSpec(req, env, cwd).options()...)
	return &cxClientAdapter{client: client, sid: req.ProviderSessionID}, nil
}

// SetSessionFactoryForTest 仅测试用;restore 闭包恢复默认。
func SetSessionFactoryForTest(fn func(agentruntime.RunRequest, map[string]string, string) (cxSessionHandle, error)) func() {
	old := cxSessionFactory
	cxSessionFactory = fn
	return func() { cxSessionFactory = old }
}
