package data_svc

// BundleFormat 是 bundle JSON 的固定标识。
const BundleFormat = "agentre-data-bundle"

// BundleVersion 当前 bundle 格式版本。
const BundleVersion = 1

// BundleV1 是 v1 版导出 JSON 的顶层结构。
type BundleV1 struct {
	Format          string       `json:"format"`
	Version         int          `json:"version"`
	ExportedAt      string       `json:"exportedAt"`
	ExportedFrom    BundleOrigin `json:"exportedFrom"`
	Scopes          []string     `json:"scopes"`
	SecretsIncluded bool         `json:"secretsIncluded"`
	Items           BundleItems  `json:"items"`
}

// BundleOrigin 描述导出来源的 App 元信息。
type BundleOrigin struct {
	AppVersion string `json:"appVersion"`
	Commit     string `json:"commit"`
}

// BundleItems 按 scope 分组的实际数据。
type BundleItems struct {
	LLMProviders  []BundleLLMProvider  `json:"llmProviders,omitempty"`
	AgentBackends []BundleAgentBackend `json:"agentBackends,omitempty"`
	Departments   []BundleDepartment   `json:"departments,omitempty"`
	Agents        []BundleAgent        `json:"agents,omitempty"`
	RemoteDevices []BundleRemoteDevice `json:"remoteDevices,omitempty"`
}

// BundleLLMProvider 一条 LLM 供应商记录（新 1→N 形状）。
//
// 单模型投影已移除：Model / MaxOutput / ContextWindow 不再出现在 Provider 层，
// 改由 Models 子列表逐条携带稳定 ModelKey、ModelID 与 token 元数据；DefaultModelKey
// 指回 Provider 默认启用的子模型。APIKey 只在 includeSecrets 时写入。
type BundleLLMProvider struct {
	ProviderKey     string                   `json:"providerKey"`
	Type            string                   `json:"type"`
	Name            string                   `json:"name"`
	BaseURL         string                   `json:"baseURL"`
	Enabled         bool                     `json:"enabled"`
	DefaultModelKey string                   `json:"defaultModelKey"`
	APIKey          string                   `json:"apiKey"`
	Models          []BundleLLMProviderModel `json:"models"`
}

// BundleLLMProviderModel 一条稳定模型记录。
// ModelKey 是不可变的跨实体稳定引用（Backend / Session / Route 都指向它），
// 导入时原样保留；ContextWindow / MaxOutput 是该模型的 token 元数据。
type BundleLLMProviderModel struct {
	ModelKey      string `json:"modelKey"`
	ModelID       string `json:"modelId"`
	Name          string `json:"name"`
	ContextWindow int    `json:"contextWindow"`
	MaxOutput     int    `json:"maxOutput"`
	Enabled       bool   `json:"enabled"`
}

// BundleAgentBackend 一条 Agent 后端记录。
type BundleAgentBackend struct {
	ExportKey             string `json:"exportKey"`
	Type                  string `json:"type"`
	Name                  string `json:"name"`
	LLMProviderKey        string `json:"llmProviderKey"`
	DeviceID              string `json:"deviceId"`
	CLIPath               string `json:"cliPath"`
	ModelRoutes           string `json:"modelRoutes"`
	Sandbox               string `json:"sandbox"`
	Approval              string `json:"approval"`
	EnvJSON               string `json:"envJSON"`
	ReasoningEffort       string `json:"reasoningEffort"`
	DefaultPermissionMode string `json:"defaultPermissionMode"`
}

// BundleDepartment 一条部门记录。
type BundleDepartment struct {
	ExportKey    string `json:"exportKey"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Icon         string `json:"icon"`
	AccentColor  string `json:"accentColor"`
	ParentKey    string `json:"parentKey"`
	LeadAgentKey string `json:"leadAgentKey"`
	SortOrder    int    `json:"sortOrder"`
}

// BundleAgent 一条 Agent 记录。
//
// AgentBackendKey 与 SkillsJSON 是 R15f 之前的单 backend / 单份技能形状，为兼容
// 与回滚窗口继续写在导出侧，但导入侧不再读——现在的读口是 ExecTargets。ExecTargets
// 为 nil（JSON 里整个不带 execTargets 这个 key）标记"老 bundle"，导入侧据此回落到
// AgentBackendKey + SkillsJSON 的单元素转换（与迁移共用同一份代码，见
// import_apply.go 的 execTargetsFromBundle）；哪怕是显式空数组也算"新 bundle"，
// 不会触发回落。
type BundleAgent struct {
	ExportKey       string             `json:"exportKey"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	AvatarColor     string             `json:"avatarColor"`
	AvatarIcon      string             `json:"avatarIcon"`
	AvatarDataURL   string             `json:"avatarDataURL"`
	SystemBadge     string             `json:"systemBadge"`
	DepartmentKey   string             `json:"departmentKey"`
	ParentAgentKey  string             `json:"parentAgentKey"`
	AgentBackendKey string             `json:"agentBackendKey"`
	SortOrder       int                `json:"sortOrder"`
	PromptJSON      string             `json:"promptJSON"`
	SkillsJSON      string             `json:"skillsJSON"`
	ExecTargets     []BundleExecTarget `json:"execTargets"`
}

// BundleExecTarget Agent 有序执行目标列表里的一档（R15f）：它指向 backend 的稳定
// key、在列表里的顺序、以及这一档自己的技能授权。
type BundleExecTarget struct {
	BackendKey string `json:"backendKey"`
	SortOrder  int    `json:"sortOrder"`
	SkillsJSON string `json:"skillsJSON"`
}

// BundleRemoteDevice 一条远端设备记录。
type BundleRemoteDevice struct {
	InstanceUUID      string `json:"instanceUUID"`
	Name              string `json:"name"`
	URL               string `json:"url"`
	DaemonFingerprint string `json:"daemonFingerprint"`
	TLSMode           string `json:"tlsMode"`
	TLSCertPEM        string `json:"tlsCertPEM"`
	PairedAt          int64  `json:"pairedAt"`
}

// ExportRequest 导出请求。
type ExportRequest struct {
	Scopes         []string `json:"scopes"`
	IncludeSecrets bool     `json:"includeSecrets"`
}

// ExportResult 导出结果。JSON 字段是 base64,因为 wails 的 IPC
// 走 JSON,直接传 []byte 会自动 base64,前端再解。
type ExportResult struct {
	JSON    []byte         `json:"json"`
	Summary map[string]int `json:"summary"`
}

// ItemAction 一条导入条目的处理动作。
type ItemAction string

const (
	ActionCreate    ItemAction = "create"
	ActionOverwrite ItemAction = "overwrite"
	ActionSkip      ItemAction = "skip"
	ActionDuplicate ItemAction = "duplicate"
)

// ImportItem 预览阶段的逐条 diff 信息。
type ImportItem struct {
	Scope         string     `json:"scope"`
	SourceKey     string     `json:"sourceKey"`
	Name          string     `json:"name"`
	Conflict      bool       `json:"conflict"`
	LocalID       int64      `json:"localID,omitempty"`
	LocalName     string     `json:"localName,omitempty"`
	Dangling      bool       `json:"dangling,omitempty"`
	DanglingHint  string     `json:"danglingHint,omitempty"`
	DefaultAction ItemAction `json:"defaultAction"`
}

// ImportPreview 预览返回。
type ImportPreview struct {
	Format          string       `json:"format"`
	Version         int          `json:"version"`
	SecretsIncluded bool         `json:"secretsIncluded"`
	Items           []ImportItem `json:"items"`
}

// ApplyImportRequest 应用导入。
type ApplyImportRequest struct {
	Raw              []byte                `json:"raw"`
	Actions          map[string]ItemAction `json:"actions"`
	FallbackStrategy ItemAction            `json:"fallbackStrategy"`
}

// ApplyImportResult 应用导入返回。
type ApplyImportResult struct {
	Counts map[string]int `json:"counts"`
}
