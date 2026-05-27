// Package agent_backend_svc 暴露 Agent 后端的应用服务接口与请求/响应类型。
//
// 类型定义直接被 Wails 绑定层引用，会被 wails dev / wails build 提取为 TypeScript
// 类型暴露给前端，因此字段名要稳定、json tag 要明确。
package agent_backend_svc

import (
	"context"

	"agentre/internal/model/entity/agent_backend_entity"
)

// BackendItem 单条 Agent 后端配置（已 join LLM Provider 摘要）。
type BackendItem struct {
	ID                int64  `json:"id"`
	Type              string `json:"type"`
	Name              string `json:"name"`
	LLMProviderKey    string `json:"llmProviderKey"`
	LLMProviderName   string `json:"llmProviderName"`
	LLMProviderType   string `json:"llmProviderType"`
	LLMProviderModel  string `json:"llmProviderModel"`
	LLMProviderActive bool   `json:"llmProviderActive"`
	CLIPath           string `json:"cliPath"`
	ModelRoutes       string `json:"modelRoutes"`
	Sandbox           string `json:"sandbox"`
	Approval          string `json:"approval"`
	EnvJSON           string `json:"envJson"`
	ReasoningEffort   string `json:"reasoningEffort"`
	// DefaultPermissionMode 仅 claudecode 使用；新会话起手 mode；
	// '' / default / acceptEdits / plan / bypassPermissions。
	DefaultPermissionMode string `json:"defaultPermissionMode"`
	// DeviceID 关联的远端设备 ID（paired_agents.id 的字符串形式）。空串 = 本地。
	DeviceID string `json:"deviceId"`
	// DeviceName 关联远端设备的显示名；DeviceID 为空时为空串。
	DeviceName string `json:"deviceName"`
	// Online 关联远端设备当前是否在线；DeviceID 为空时为 false。
	Online bool `json:"online"`
	// AgentCount 引用该 backend 的 active Agent 数；List 时由 svc 注入。
	AgentCount int64 `json:"agentCount"`
	Createtime int64 `json:"createtime"`
	Updatetime int64 `json:"updatetime"`
}

// ListBackendsRequest 入参占位。
type ListBackendsRequest struct{}

// ListBackendsResponse 列出全部启用的后端。
type ListBackendsResponse struct {
	Items []*BackendItem `json:"items"`
}

// CreateBackendRequest 新建后端。不同 Type 的字段约束由 agent_backend_entity.BackendKind 校验。
type CreateBackendRequest struct {
	Type                  string `json:"type" binding:"required"`
	Name                  string `json:"name" binding:"required"`
	LLMProviderKey        string `json:"llmProviderKey"`
	CLIPath               string `json:"cliPath"`
	ModelRoutes           string `json:"modelRoutes"`
	Sandbox               string `json:"sandbox"`
	Approval              string `json:"approval"`
	EnvJSON               string `json:"envJson"`
	ReasoningEffort       string `json:"reasoningEffort"`
	DefaultPermissionMode string `json:"defaultPermissionMode"`
	DeviceID              string `json:"deviceId"`
}

// CreateBackendResponse 返回创建后的实体。
type CreateBackendResponse struct {
	Item *BackendItem `json:"item"`
}

// UpdateBackendRequest 更新后端。Type 不可变。
type UpdateBackendRequest struct {
	ID                    int64  `json:"id" binding:"required"`
	Name                  string `json:"name" binding:"required"`
	LLMProviderKey        string `json:"llmProviderKey"`
	CLIPath               string `json:"cliPath"`
	ModelRoutes           string `json:"modelRoutes"`
	Sandbox               string `json:"sandbox"`
	Approval              string `json:"approval"`
	EnvJSON               string `json:"envJson"`
	ReasoningEffort       string `json:"reasoningEffort"`
	DefaultPermissionMode string `json:"defaultPermissionMode"`
	DeviceID              string `json:"deviceId"`
}

// UpdateBackendResponse 返回更新后的实体。
type UpdateBackendResponse struct {
	Item *BackendItem `json:"item"`
}

// DeleteBackendRequest 软删除后端。
type DeleteBackendRequest struct {
	ID int64 `json:"id" binding:"required"`
}

// DeleteBackendResponse 占位返回。
type DeleteBackendResponse struct{}

// TestBackendRequest 请求一次连通性自检。
//
// ID > 0  → 用已保存的 backend 记录作底；UseDraft=true 时再用 draft 字段覆盖。
// ID == 0 → 全部字段从 draft 来,适用于"还没保存就先试"。
//
// RequestID 由前端生成（uuid），用于在测试还在跑时通过 CancelTest 主动中断。
// 留空 → 不可中断（兼容旧路径 / 自动化调用）。
type TestBackendRequest struct {
	ID                    int64  `json:"id"`
	UseDraft              bool   `json:"useDraft"`
	Type                  string `json:"type"`
	Name                  string `json:"name"`
	LLMProviderKey        string `json:"llmProviderKey"`
	CLIPath               string `json:"cliPath"`
	ModelRoutes           string `json:"modelRoutes"`
	Sandbox               string `json:"sandbox"`
	Approval              string `json:"approval"`
	EnvJSON               string `json:"envJson"`
	ReasoningEffort       string `json:"reasoningEffort"`
	DefaultPermissionMode string `json:"defaultPermissionMode"`
	RequestID             string `json:"requestId"`
}

// TestBackendResponse 返回测试结果。
//
// Message 在 OK=true 时是模型回复文本,OK=false 时是人话错误。
type TestBackendResponse struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	LatencyMs int64  `json:"latencyMs"`
}

// CancelTestBackendRequest 中断一个还在跑的 Test。
//
// RequestID 必须与发起 Test 时 TestBackendRequest.RequestID 一致；
// 未知 ID 返回 Canceled=false（不视为错误，避免前端竞态导致刷红）。
type CancelTestBackendRequest struct {
	RequestID string `json:"requestId" binding:"required"`
}

// CancelTestBackendResponse 返回是否真的命中了在跑的请求。
type CancelTestBackendResponse struct {
	Canceled bool `json:"canceled"`
}

// ResolveCLIPathRequest 探测前端选定 CLI 后端类型可用的 binary 绝对路径。
//
// Type 必填，仅接受 "claudecode" / "codex"；其它值返回 AgentBackendInvalidType。
//
// DeviceID 路由 CLI 探测的目标机：
//   - 空串 → 本地，主进程直接走 cliprober 扫本机 $PATH；
//   - 非空（paired_agents.id 的字符串形式）→ 远端，主进程拨该 device 调
//     daemon 的 cli.resolvePath RPC，让远端扫它自己的 $PATH。
type ResolveCLIPathRequest struct {
	Type     string `json:"type" binding:"required"`
	DeviceID string `json:"deviceId"`
}

// ResolveCLIPathResponse 返回 exec.LookPath 命中的绝对路径。
//
// Found=false 时 Path 为空，表示 $PATH 里未挂到对应可执行文件；前端应回退到
// 让用户手填。已注释字段不会被前端写回 backend 表，仅作为编辑器自动填充建议。
type ResolveCLIPathResponse struct {
	Path  string `json:"path"`
	Found bool   `json:"found"`
}

//go:generate mockgen -source types.go -destination mock_prober_test.go -package agent_backend_svc -mock_names Prober=mockProber

// ProbeDeps 由 svc.Test 装配后传给 Prober。
//
//   - 对 builtin 的 codingProber 全部留空（不依赖 gateway）；
//   - CLI 子进程类 Prober 若经本地 gateway 测试，需要 Token + GatewayURL + Model。
type ProbeDeps struct {
	GatewayURL string
	Token      string
	Model      string
}

// Prober 抽象"对一条 backend 跑一轮 agent loop"这个外部依赖。
//
// 默认生产注册表目前只登记 builtinProber（cago app/coding in-process）。
// 单测可注入 fake 或替换注册表，避免真实 LLM / 子进程调用。
type Prober interface {
	Run(ctx context.Context, b *agent_backend_entity.AgentBackend, deps ProbeDeps) (reply string, err error)
}
