package agent_backend_svc

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/agents/app/coding"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/pkg/agentprovider"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/repository/llm_provider_repo"
)

// proberFor 按 backend 类型查 Prober；未注册返 nil。
//
// 默认注册表只包含 builtin in-process 路径；单测可临时替换这个包级 var
// 验证 Test() 的 BackendType 派发。
var proberRegistry = map[agent_backend_entity.BackendType]Prober{
	agent_backend_entity.TypeBuiltin: builtinProber{},
}

// providerBuilder 是 agentprovider.Build 的间接引用，让单测能把 fake provider
// 注入 builtinProber 而不必真的去打 anthropic / openai 网络。生产路径保持透明。
var providerBuilder = agentprovider.Build

// proberFor 查表。
func proberFor(t agent_backend_entity.BackendType) Prober {
	if p, ok := proberRegistry[t]; ok {
		return p
	}
	return nil
}

// builtinProber 跑 cago app/coding，in-process 拉一轮 agent loop，回 assistant 末轮 text。
type builtinProber struct{}

func (builtinProber) Run(ctx context.Context, b *agent_backend_entity.AgentBackend, _ ProbeDeps) (string, error) {
	if b == nil {
		return "", errors.New("nil backend")
	}
	p, err := llm_provider_repo.LLMProvider().FindByKey(ctx, b.LLMProviderKey)
	if err != nil {
		return "", err
	}
	if p == nil || !p.IsActive() {
		return "", errors.New("llm provider missing or inactive")
	}
	prov, err := providerBuilder(p)
	if err != nil {
		return "", err
	}

	cwd, err := os.MkdirTemp("", "agentre-backend-test-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(cwd) }()

	// 必须把 LLMProvider.Model 透传到父 agent，否则 ChatStream req.Model 为空，
	// anthropic / openai 收到空 model 直接 400，呈现"200ms 完成但没调用记录"的假象。
	sys, err := coding.New(ctx, prov, cwd, coding.WithModel(p.Model))
	if err != nil {
		return "", err
	}
	defer func() { _ = sys.Close(ctx) }()

	conv := agent.NewConversation()
	runner, err := sys.Agent().TryRunner(conv)
	if err != nil {
		return "", err
	}
	defer func() { _ = runner.Close() }()

	if err := runner.Wait(ctx, fixedTestPrompt); err != nil {
		return "", err
	}
	return lastAssistantText(conv), nil
}

// buildClaudeCodeEnv 委托到 agentruntime.BuildClaudeCodeEnv。
// 保留包级 helper 以维持现有调用点与测试的命名稳定；逻辑与文档全部迁到
// internal/pkg/agentruntime/clienv.go，与 chat path 的 CLI runner 共享同一份装配规则，
// 避免两处漂移。
func buildClaudeCodeEnv(b *agent_backend_entity.AgentBackend, deps ProbeDeps) (map[string]string, error) {
	return agentruntime.BuildClaudeCodeEnv(b, agentruntime.CLIDeps{
		Token:      deps.Token,
		GatewayURL: deps.GatewayURL,
	})
}

// buildCodexEnv 委托到 agentruntime.BuildCodexEnv；同 buildClaudeCodeEnv。
func buildCodexEnv(b *agent_backend_entity.AgentBackend, deps ProbeDeps) (map[string]string, error) {
	return agentruntime.BuildCodexEnv(b, agentruntime.CLIDeps{
		Token:      deps.Token,
		GatewayURL: deps.GatewayURL,
	})
}

// lastAssistantText 拼接末尾一条 assistant message 的所有 TextBlock 内容。
// 忽略 tool use 等其他 block 类型;固定 prompt 不触发工具,正常路径应直接是 "pong"。
func lastAssistantText(conv *agent.Conversation) string {
	msgs := conv.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != agent.RoleAssistant {
			continue
		}
		var b strings.Builder
		for _, blk := range msgs[i].Content {
			if tb, ok := blk.(blocks.TextBlock); ok {
				b.WriteString(tb.Text)
			}
		}
		return b.String()
	}
	return ""
}
