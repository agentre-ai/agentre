package agentruntime

import (
	"fmt"
	"strconv"
	"strings"

	"agentre/internal/model/entity/agent_backend_entity"
)

const (
	codexGatewayProviderID = "agentre-gateway"
	codexGatewayAPIKeyEnv  = "OPENAI_API_KEY" // #nosec G101 -- env var name, not a credential value.
)

// CLIDeps CLI runner 装配子进程 env 时需要的临时凭证。
// 空对象 = 不签 token，让 CLI 自身 login 状态生效。
type CLIDeps struct {
	Token      string
	GatewayURL string
}

// BuildClaudeCodeEnv 装配 claudecode 子进程的环境变量：
//
//   - **AGENTRE_GATEWAY_URL / AGENTRE_GATEWAY_TOKEN**：deps 非空时一律设置。
//     PostToolUse hook 子进程（agentre claudecode hook post-tool）用它访问
//     /hook/v1/inbox 拉排队消息。不走 ANTHROPIC_*，避免被 claude CLI 当成「替
//     OAuth 把 LLM 路由到 gateway」的开关（CLI 登录模式下我们没 provider 转
//     发，会直接挂 LLM）。
//   - **ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN**：只在 backend 真的绑了 LLM
//     provider 时设——这种情况下确实希望 claude CLI 把 LLM 请求路由到 gateway，
//     gateway 按 model_routes 转发到对应 provider。CLI 登录模式留空，让 claude
//     自身 OAuth 直连 anthropic.com。
//   - 用户自定义 env_json 追加（保留键已被 entity.Check 拒入）；
//   - 如果 backend.model_routes 含 OPUS/SONNET/HAIKU，注入 ANTHROPIC_DEFAULT_*_MODEL = alias 字符串
//     （上游识别 alias，gateway 按 alias 路由到对应 provider 后再改写成真实 model id）。
//
// 只写 AUTH_TOKEN 不写 API_KEY：
//   - ANTHROPIC_API_KEY 走 x-api-key 头，定位是「直连官方 API」；CLI 检测到 `claude login`
//     订阅 session 时仍会优先 OAuth 直连 anthropic.com，绕过本地 gateway。
//   - ANTHROPIC_AUTH_TOKEN 走 `Authorization: Bearer <token>`，是 anthropic 官方文档里
//     压住 OAuth、把请求转到自定义代理的标准开关，用户参考脚本里也是只设 AUTH_TOKEN。
//   - 两个都写会让上游同时收到 Bearer + x-api-key，部分代理对冲突 auth header 会直接 400。
func BuildClaudeCodeEnv(b *agent_backend_entity.AgentBackend, deps CLIDeps) (map[string]string, error) {
	env := map[string]string{}
	if deps.GatewayURL != "" && deps.Token != "" {
		// hook 子进程总能拿到这两个，无论是否 CLI 登录模式。
		env["AGENTRE_GATEWAY_URL"] = deps.GatewayURL
		env["AGENTRE_GATEWAY_TOKEN"] = deps.Token
		// 只有当 backend 绑了 LLM provider 时才让 claude CLI 走 gateway 做 LLM 转发。
		if b != nil && b.LLMProviderKey != "" {
			env["ANTHROPIC_BASE_URL"] = deps.GatewayURL
			env["ANTHROPIC_AUTH_TOKEN"] = deps.Token
		}
	}
	routes, err := agent_backend_entity.ParseModelRoutes(b.ModelRoutes)
	if err != nil {
		return nil, fmt.Errorf("parse model_routes: %w", err)
	}
	for alias := range routes {
		switch strings.ToUpper(alias) {
		case "OPUS":
			env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = "opus"
		case "SONNET":
			env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = "sonnet"
		case "HAIKU":
			env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = "haiku"
		}
	}
	user, err := agent_backend_entity.ParseEnvJSON(b.EnvJSON)
	if err != nil {
		return nil, fmt.Errorf("parse env_json: %w", err)
	}
	for k, v := range user {
		env[k] = v
	}
	return env, nil
}

// BuildCodexEnv 装配 codex 子进程的环境变量：
//   - 关联 provider 时（deps 非空）：只写 OPENAI_API_KEY=<一次性 token>；
//     base_url / model_provider 由 BuildCodexConfig 写入 Codex CLI config override；
//   - 未关联 provider 时（deps 空）：不写 API_KEY，让 codex CLI 自身的 login 状态生效；
//   - 用户自定义 env_json 追加。
func BuildCodexEnv(b *agent_backend_entity.AgentBackend, deps CLIDeps) (map[string]string, error) {
	env := map[string]string{}
	if deps.GatewayURL != "" && deps.Token != "" {
		env[codexGatewayAPIKeyEnv] = deps.Token
	}
	user, err := agent_backend_entity.ParseEnvJSON(b.EnvJSON)
	if err != nil {
		return nil, fmt.Errorf("parse env_json: %w", err)
	}
	for k, v := range user {
		env[k] = v
	}
	return env, nil
}

// BuildPiAgentEnv 装配 pi-agent 子进程环境变量：
//   - 默认 PI_OFFLINE=1，避免桌面启动路径触发 update/network startup；
//   - Pi 自行读取 ~/.pi/agent/models.json / settings.json / auth.json，不走 Agentre gateway；
//   - 用户自定义 env_json 追加（保留键已被 entity.Check 拒入）。
func BuildPiAgentEnv(b *agent_backend_entity.AgentBackend) (map[string]string, error) {
	env := map[string]string{"PI_OFFLINE": "1"}
	user, err := agent_backend_entity.ParseEnvJSON(b.EnvJSON)
	if err != nil {
		return nil, fmt.Errorf("parse env_json: %w", err)
	}
	for k, v := range user {
		env[k] = v
	}
	return env, nil
}

// codexReasoningEffortConfigValue 把落库的 reasoning_effort 映射为 codex CLI 配置值。
// codex 支持 low / medium / high / xhigh；entity 层允许的 max 在这里向下并到 high，
// 让用户跨后端切换时不丢档位语义。off / 非法值（含大小写错、含空格）→ "" 表示不下发，
// 走 codex 自身默认。
func codexReasoningEffortConfigValue(s string) string {
	switch s {
	case "low", "medium", "high", "xhigh":
		return s
	case "max":
		return "high"
	default:
		return ""
	}
}

// BuildCodexConfig 返回 Codex CLI 的 -c 覆盖项，让 codex app-server 明确使用
// Agentre 本地 gateway provider。token 不放进 config，避免出现在进程 argv 里。
//
// base_url 必须带 /v1：Codex CLI 在 wire_api="responses" 下直接拼 `<base_url>/responses`，
// 而 gateway 路由是 `/v1/responses`；不带 /v1 会 404。
func BuildCodexConfig(deps CLIDeps) []string {
	baseURL := strings.TrimRight(strings.TrimSpace(deps.GatewayURL), "/")
	if baseURL == "" || strings.TrimSpace(deps.Token) == "" {
		return nil
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	provider := codexGatewayProviderID
	return []string{
		"model_provider=" + strconv.Quote(provider),
		"model_providers." + provider + ".name=" + strconv.Quote("Agentre Gateway"),
		"model_providers." + provider + ".base_url=" + strconv.Quote(baseURL),
		"model_providers." + provider + ".env_key=" + strconv.Quote(codexGatewayAPIKeyEnv),
		"model_providers." + provider + ".wire_api=" + strconv.Quote("responses"),
	}
}
