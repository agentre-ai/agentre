package agent_backend_entity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/pkg/code"
)

// BackendKind 把「不同 backend 类型的取值约束」做成可扩展集合。
//
// 新增一种 backend 类型时实现 BackendKind 并在 backendKinds 包级 var 里登记，
// agent_backend_entity.AgentBackend.Check 会自动按类型分派。**禁止 init() 副作用**——
// 静态 map 让测试可以在不重置全局状态的情况下并行跑。
type BackendKind interface {
	// Type 该 kind 对应的字符串类型常量。
	Type() BackendType

	// KnownAliases 列出本 kind 支持的 model_routes alias 键（claudecode = OPUS/SONNET/HAIKU；
	// codex 暂为空集，强制 routes == "{}"）。
	KnownAliases() []string

	// ProviderTypeMatch 判断 LLMProvider.type 是否与本 kind 严格匹配；alias provider 同集合。
	ProviderTypeMatch(t llm_provider_entity.ProviderType) bool

	// AllowsCLIPath 是否允许 cli_path 字段非空；builtin 不允许，claudecode/codex 允许。
	AllowsCLIPath() bool

	// ValidateExtra 对 sandbox / approval / env_json 等独有字段做校验。
	// 在公共校验（name / type / provider / model_routes alias 集合 / env_json 解析）通过之后调用。
	ValidateExtra(ctx context.Context, b *AgentBackend) error
}

// backendKinds 是包级静态注册表。新增 BackendKind 实现时在这里追加一行即可，
// 不要在 init() 里改。
var backendKinds = map[BackendType]BackendKind{
	TypeBuiltin:    builtinKind{},
	TypeClaudeCode: claudeCodeKind{},
	TypeCodex:      codexKind{},
	TypePiAgent:    piAgentKind{},
}

// KindFor 查表，找不到返 nil。Service 在 Test/Create/Update 前用它分派 Prober。
func KindFor(t BackendType) BackendKind {
	if k, ok := backendKinds[t]; ok {
		return k
	}
	return nil
}

// reservedEnvKeys 是 App 自己写入 ANTHROPIC_BASE_URL 等保留键的白名单。
// 用户在 env_json 里设置以下键将被 entity.Check 拒绝（AgentBackendReservedEnvKey）；
// 其它 ANTHROPIC_* / OPENAI_* 键（如 ANTHROPIC_LOG）可自由覆盖。
var reservedEnvKeys = map[string]struct{}{
	"AGENTRE_GATEWAY_URL":            {},
	"AGENTRE_GATEWAY_TOKEN":          {},
	"ANTHROPIC_BASE_URL":             {},
	"ANTHROPIC_API_KEY":              {},
	"ANTHROPIC_AUTH_TOKEN":           {},
	"ANTHROPIC_MODEL":                {},
	"ANTHROPIC_DEFAULT_OPUS_MODEL":   {},
	"ANTHROPIC_DEFAULT_SONNET_MODEL": {},
	"ANTHROPIC_DEFAULT_HAIKU_MODEL":  {},
	"OPENAI_API_KEY":                 {},
	"OPENAI_BASE_URL":                {},
	"OPENAI_API_BASE":                {},
	"PI_OFFLINE":                     {},
	"PI_CODING_AGENT_DIR":            {},
	"PI_CODING_AGENT_SESSION_DIR":    {},
}

// IsReservedEnvKey 提供给 service / 前端预校验。
func IsReservedEnvKey(key string) bool {
	_, ok := reservedEnvKeys[key]
	return ok
}

// builtinKind builtin 不接受新列，所有四项必须保持默认空值。
type builtinKind struct{}

func (builtinKind) Type() BackendType                                         { return TypeBuiltin }
func (builtinKind) KnownAliases() []string                                    { return nil }
func (builtinKind) ProviderTypeMatch(t llm_provider_entity.ProviderType) bool { return true }
func (builtinKind) AllowsCLIPath() bool                                       { return false }

func (builtinKind) ValidateExtra(ctx context.Context, b *AgentBackend) error {
	if strings.TrimSpace(b.LLMProviderKey) == "" {
		return i18n.NewError(ctx, code.AgentBackendLLMProviderRequired)
	}
	if strings.TrimSpace(b.CLIPath) != "" {
		return i18n.NewError(ctx, code.AgentBackendCLIPathNotAllowed)
	}
	// 新增列对 builtin 无意义；非默认值即报错。
	if !isEmptyJSONObject(b.ModelRoutes) ||
		strings.TrimSpace(b.Sandbox) != "" ||
		strings.TrimSpace(b.Approval) != "" ||
		!isEmptyJSONObject(b.EnvJSON) ||
		strings.TrimSpace(b.DefaultPermissionMode) != "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	return nil
}

// claudeCodeKind 走 claude CLI，匹配 anthropic provider，支持 OPUS/SONNET/HAIKU 三级路由。
type claudeCodeKind struct{}

func (claudeCodeKind) Type() BackendType      { return TypeClaudeCode }
func (claudeCodeKind) KnownAliases() []string { return []string{"OPUS", "SONNET", "HAIKU"} }
func (claudeCodeKind) ProviderTypeMatch(t llm_provider_entity.ProviderType) bool {
	return t == llm_provider_entity.TypeAnthropic
}
func (claudeCodeKind) AllowsCLIPath() bool { return true }

func (claudeCodeKind) ValidateExtra(ctx context.Context, b *AgentBackend) error {
	// LLMProviderKey == "" 表示不关联供应商，走 claude CLI 自身的登录态（claude login）。
	if strings.TrimSpace(b.Sandbox) != "" || strings.TrimSpace(b.Approval) != "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if mode := strings.TrimSpace(b.DefaultPermissionMode); mode != "" && !IsValidPermissionMode(mode) {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	return nil
}

// codexKind 走 codex CLI，严格匹配 openai-response。
// model_routes 必须为空（codex 没有 tier 概念）。
type codexKind struct{}

func (codexKind) Type() BackendType      { return TypeCodex }
func (codexKind) KnownAliases() []string { return nil }
func (codexKind) ProviderTypeMatch(t llm_provider_entity.ProviderType) bool {
	return t == llm_provider_entity.TypeOpenAIResponse
}
func (codexKind) AllowsCLIPath() bool { return true }

func (codexKind) ValidateExtra(ctx context.Context, b *AgentBackend) error {
	// LLMProviderKey == "" 表示不关联供应商，走 codex CLI 自身的登录态（codex login）。
	if !isEmptyJSONObject(b.ModelRoutes) {
		return i18n.NewError(ctx, code.AgentBackendUnknownAlias)
	}
	if err := validateSandbox(ctx, b.Sandbox); err != nil {
		return err
	}
	if err := validateApproval(ctx, b.Approval); err != nil {
		return err
	}
	// default_permission_mode 是 claudecode 专属字段；codex 自有 sandbox/approval 通道，
	// 不复用该字段。
	if strings.TrimSpace(b.DefaultPermissionMode) != "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	return nil
}

// piAgentKind 走 Pi coding agent RPC mode。Pi 自己读取 ~/.pi/agent 的模型与认证配置，
// Agentre 不把它绑定到 LLMProvider gateway。
type piAgentKind struct{}

func (piAgentKind) Type() BackendType      { return TypePiAgent }
func (piAgentKind) KnownAliases() []string { return nil }
func (piAgentKind) ProviderTypeMatch(llm_provider_entity.ProviderType) bool {
	return false
}
func (piAgentKind) AllowsCLIPath() bool { return true }

func (piAgentKind) ValidateExtra(ctx context.Context, b *AgentBackend) error {
	if strings.TrimSpace(b.LLMProviderKey) != "" ||
		!isEmptyJSONObject(b.ModelRoutes) ||
		strings.TrimSpace(b.Sandbox) != "" ||
		strings.TrimSpace(b.Approval) != "" ||
		strings.TrimSpace(b.DefaultPermissionMode) != "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	return nil
}

// validateSandbox 校验 codex sandbox 枚举；空字符串表示走 CLI 默认。
func validateSandbox(ctx context.Context, v string) error {
	switch strings.TrimSpace(v) {
	case "", "read-only", "workspace-write", "danger-full-access":
		return nil
	default:
		return i18n.NewError(ctx, code.AgentBackendInvalidSandbox)
	}
}

// validateApproval 校验 codex approval policy 枚举；空字符串表示 never。
func validateApproval(ctx context.Context, v string) error {
	switch strings.TrimSpace(v) {
	case "", "untrusted", "on-failure", "on-request", "never":
		return nil
	default:
		return i18n.NewError(ctx, code.AgentBackendInvalidApproval)
	}
}

// isEmptyJSONObject 判断字符串是否表示空 JSON 对象（"" / "{}" / 含空白的 "{}"）。
func isEmptyJSONObject(s string) bool {
	t := strings.TrimSpace(s)
	return t == "" || t == "{}"
}

// ParseModelRoutes 把 model_routes 字段解析成 map[alias]providerKey。
// JSON 格式：{"OPUS":"<uuid>","SONNET":"<uuid>"} — value 必须是非空字符串（UUID 形态由 service 层做 cross-check）。
// alias 键统一转 ToUpper；解析失败或 value 为空串均返回 error（统一交 service 报）。
// 调用方负责再用 BackendKind.KnownAliases() 把 alias 集合限定到本类型。
func ParseModelRoutes(s string) (map[string]string, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return map[string]string{}, nil
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("parse model_routes: %w", err)
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, fmt.Errorf("model_routes alias %q has empty value", k)
		}
		out[strings.ToUpper(strings.TrimSpace(k))] = v
	}
	return out, nil
}

// ParseEnvJSON 把 env_json 字段解析成 map[string]string。空 / "{}" 视作空 map。
func ParseEnvJSON(s string) (map[string]string, error) {
	t := strings.TrimSpace(s)
	if t == "" || t == "{}" {
		return map[string]string{}, nil
	}
	out := make(map[string]string)
	if err := json.Unmarshal([]byte(t), &out); err != nil {
		return nil, err
	}
	return out, nil
}
