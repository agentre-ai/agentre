// Package agent_backend_entity 维护 Agent 后端的充血实体。
//
// 一条 AgentBackend = 一个可被多个 Agent 共享引用的「后端实例」：
//   - Type=TypeBuiltin   走 cago github.com/cago-frame/agents/app/coding，绑定一个 LLMProvider；
//   - Type=TypeClaudeCode  通过 cliagent/claudecode 拉 claude CLI，绑定 anthropic 类型 provider；
//   - Type=TypeCodex     通过 github.com/agentre-ai/agentre/pkg/codex 拉 codex CLI，绑定 openai-response provider。
//
// 不同 type 的字段约束由 BackendKind（kinds.go）分派，entity.Check 不再直接 switch type。
package agent_backend_entity

import (
	"context"
	"strconv"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

// BackendType Agent 后端实现类型。
type BackendType string

const (
	// TypeBuiltin 走 cago app/coding，in-process 跑工具调用。
	TypeBuiltin BackendType = "builtin"
	// TypeClaudeCode 包装本地 claude CLI（cliagent/claudecode）。
	TypeClaudeCode BackendType = "claudecode"
	// TypeCodex 包装本地 codex CLI（github.com/agentre-ai/agentre/pkg/codex），默认走 OpenAI Responses API。
	TypeCodex BackendType = "codex"
	// TypePiAgent 包装本地 pi CLI（@earendil-works/pi-coding-agent RPC mode）。
	TypePiAgent BackendType = "piagent"
)

// AgentBackend 一条 Agent 后端配置记录。
type AgentBackend struct {
	ID             int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Type           string `gorm:"column:type;type:text;not null"`
	Name           string `gorm:"column:name;type:text;not null"`
	LLMProviderKey string `gorm:"column:llm_provider_key;type:text;not null;default:''"`
	// DeviceID 关联的远端设备 ID（paired_agents.id 的字符串形式）。
	// 空串表示本地运行；非空表示用户意图绑定远端，服务层按 DeviceIDInt() 派发。
	DeviceID string `gorm:"column:device_id;type:text;not null;default:''"`
	CLIPath  string `gorm:"column:cli_path;type:text;not null;default:''"`
	// ModelRoutes 仅 claudecode 使用：`{"OPUS":"<provider-key>","SONNET":"<provider-key>","HAIKU":"<provider-key>"}` 子集，
	// 任意 alias 缺省时回落到主 LLMProviderKey。
	ModelRoutes string `gorm:"column:model_routes;type:text;not null;default:'{}'"`
	// Sandbox 仅 codex 使用：read-only / workspace-write / danger-full-access；空 = CLI 默认。
	Sandbox string `gorm:"column:sandbox;type:text;not null;default:''"`
	// Approval 仅 codex 使用：untrusted / on-failure / on-request / never；空 = never。
	Approval string `gorm:"column:approval;type:text;not null;default:''"`
	// EnvJSON claudecode / codex 共用：`{"K":"V"}` 自定义透传环境变量；保留键拒入。
	EnvJSON string `gorm:"column:env_json;type:text;not null;default:'{}'"`
	// ReasoningEffort 思考力度档位（六档）。空串 = 走模型/CLI 默认；其余取值见 effort.go。
	ReasoningEffort string `gorm:"column:reasoning_effort;type:text;not null;default:''"`
	// DefaultPermissionMode 仅 claudecode 使用：CLI 子进程 spawn 时下发的
	// --permission-mode 值；空串走 pkg/claudecode 默认（acceptEdits）。
	// 取值：'' / default / acceptEdits / plan / bypassPermissions。
	// 其它后端类型必须保持空串，否则 entity.Check 报 InvalidParameter。
	DefaultPermissionMode string `gorm:"column:default_permission_mode;type:text;not null;default:''"`
	// DefaultModel 仅 claudecode 使用：spawn claude 子进程时下发的 --model 值。
	// 走 CLI 登录态（未绑 provider）时用它指定自定义模型（如 claude-fable-5）；
	// 绑了 provider 时 provider.Model 优先，本字段仅在 provider.Model 为空时兜底。
	// 空串 = 不下发 --model，走 CLI 默认。其它后端类型必须保持空串。
	DefaultModel string `gorm:"column:default_model;type:text;not null;default:''"`
	Status       int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime   int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime   int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

// TableName 绑定表名。
func (*AgentBackend) TableName() string { return "agent_backends" }

// IsActive 是否处于启用态（未被软删除）。
func (b *AgentBackend) IsActive() bool { return b != nil && b.Status == consts.ACTIVE }

// IsBuiltin / IsClaudeCode / IsCodex 类型判断；nil receiver 返回 false。
func (b *AgentBackend) IsBuiltin() bool {
	return b != nil && BackendType(b.Type) == TypeBuiltin
}

func (b *AgentBackend) IsClaudeCode() bool {
	return b != nil && BackendType(b.Type) == TypeClaudeCode
}

func (b *AgentBackend) IsCodex() bool {
	return b != nil && BackendType(b.Type) == TypeCodex
}

func (b *AgentBackend) IsPiAgent() bool {
	return b != nil && BackendType(b.Type) == TypePiAgent
}

// IsLocal DeviceID 为空时为本地模式；nil receiver 返回 false。
func (b *AgentBackend) IsLocal() bool { return b != nil && b.DeviceID == "" }

// IsRemote DeviceID 非空即视为"绑了远端意图"，无论字段值是否可解析为整数；nil receiver 返回 false。
func (b *AgentBackend) IsRemote() bool { return b != nil && b.DeviceID != "" }

// DeviceIDInt 解析 DeviceID 为 paired_agents.id。空串或解析失败时返回 (0, false)。
func (b *AgentBackend) DeviceIDInt() (int64, bool) {
	if b == nil || b.DeviceID == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(b.DeviceID, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// Kind 返回当前 backend 对应的 BackendKind；未知类型返 nil。
func (b *AgentBackend) Kind() BackendKind {
	if b == nil {
		return nil
	}
	return KindFor(BackendType(b.Type))
}

// Check 校验通用字段 + 通过 BackendKind 分派类型独有校验。
//
// 规则：
//   - nil receiver → AgentBackendNotFound
//   - 空 name → InvalidParameter
//   - 未知 type → AgentBackendInvalidType
//   - env_json 反序列化失败 → AgentBackendInvalidEnvJSON
//   - env_json 含保留键 → AgentBackendReservedEnvKey
//   - model_routes 反序列化失败 / 含未知 alias → AgentBackendUnknownAlias
//   - cli_path 在不允许该字段的 kind 上非空 → AgentBackendCLIPathNotAllowed
//   - 其它 kind 独有字段（sandbox / approval / llm_provider_key）由 ValidateExtra 抛
func (b *AgentBackend) Check(ctx context.Context) error {
	if b == nil {
		return i18n.NewError(ctx, code.AgentBackendNotFound)
	}
	if strings.TrimSpace(b.Name) == "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	kind := b.Kind()
	if kind == nil {
		return i18n.NewError(ctx, code.AgentBackendInvalidType)
	}

	// env_json：反序列化 + 保留键白名单。
	envMap, err := ParseEnvJSON(b.EnvJSON)
	if err != nil {
		return i18n.NewError(ctx, code.AgentBackendInvalidEnvJSON)
	}
	for k := range envMap {
		if IsReservedEnvKey(k) {
			return i18n.NewError(ctx, code.AgentBackendReservedEnvKey)
		}
	}

	// model_routes：反序列化 + alias 集合限定。空集对所有 kind 都合法。
	routes, err := ParseModelRoutes(b.ModelRoutes)
	if err != nil {
		return i18n.NewError(ctx, code.AgentBackendUnknownAlias)
	}
	if len(routes) > 0 {
		known := kind.KnownAliases()
		if len(known) == 0 {
			return i18n.NewError(ctx, code.AgentBackendUnknownAlias)
		}
		allowed := make(map[string]struct{}, len(known))
		for _, a := range known {
			allowed[a] = struct{}{}
		}
		for alias, providerKey := range routes {
			if _, ok := allowed[alias]; !ok {
				return i18n.NewError(ctx, code.AgentBackendUnknownAlias)
			}
			if strings.TrimSpace(providerKey) == "" {
				return i18n.NewError(ctx, code.AgentBackendAliasProviderInvalid)
			}
		}
	}

	// cli_path：kind 决定能否非空。
	if !kind.AllowsCLIPath() && strings.TrimSpace(b.CLIPath) != "" {
		return i18n.NewError(ctx, code.AgentBackendCLIPathNotAllowed)
	}

	// reasoning_effort：三种 type 共用同一枚举；codex 的 max 由启动层 clamp，
	// 不在 entity 层拒绝（避免 type 切换时丢值）。
	if !IsValidReasoningEffort(b.ReasoningEffort) {
		return i18n.NewError(ctx, code.AgentBackendInvalidReasoningEffort)
	}

	return kind.ValidateExtra(ctx, b)
}

// validPermissionModes 与 pkg/claudecode/session.go::validPermissionModes 对齐。
// 空串单独由 caller 处理（claudecode 允许 "" = 走 acceptEdits 默认）。
var validPermissionModes = map[string]struct{}{
	"default":           {},
	"acceptEdits":       {},
	"plan":              {},
	"bypassPermissions": {},
}

// IsValidPermissionMode 校验 default_permission_mode 取值（不含空串语义）。
func IsValidPermissionMode(mode string) bool {
	_, ok := validPermissionModes[mode]
	return ok
}
