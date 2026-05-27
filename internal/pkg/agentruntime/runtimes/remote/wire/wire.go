// Package wire 定义 agentre ↔ agentred RPC 协议的参数 / 结果 / 通知帧。
// daemon handlers 和 client *remote.Runtime 共享同一组类型,避免两边手抄 JSON shape 时漂移。
//
// 命名约定:
//   - 所有 RPC 方法都在 "runtime.*" 命名空间下,与 agentruntime.Runtime + 子接口一一对应。
//   - 字段名一律 lowerCamelCase。
//   - 错误码 -32010..-32013 是 agentruntime 标准 sentinel 的稳定 wire 值;
//     ToJSONRPCError / FromJSONRPCError 双向翻译,让 errors.Is(err, agentruntime.ErrXxx)
//     在客户端继续工作。
package wire

import (
	"encoding/json"
	"errors"

	"github.com/cago-frame/agents/agent/blocks"

	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/pkg/jsonrpc"
)

// ── RPC method names ────────────────────────────────────────────────────────

// Method 常量是 daemon registry.Register 与客户端 c.Call 的唯一来源。
const (
	MethodCapabilities         = "runtime.capabilities"
	MethodRun                  = "runtime.run"
	MethodSteer                = "runtime.steer"
	MethodCancelSteer          = "runtime.cancelSteer"
	MethodDrainPending         = "runtime.drainPending"
	MethodAbort                = "runtime.abort"
	MethodSetPermissionMode    = "runtime.setPermissionMode"
	MethodSubmitAnswer         = "runtime.submitAnswer"
	MethodSubmitToolPermission = "runtime.submitToolPermission"

	// daemon → client 通知。
	NotifyEvent         = "runtime.event"
	NotifyRunResultDone = "runtime.runResultDone"
)

// ── Error codes ─────────────────────────────────────────────────────────────

const (
	ErrCodeNoActiveTurn  = -32010
	ErrCodeSteerNotFound = -32011
	ErrCodeUnsupported   = -32012
	ErrCodeAborted       = -32013
)

// ToJSONRPCError 把 agentruntime 的 sentinel 包成 *jsonrpc.Error,daemon 端返回。
// 非 sentinel 错误返 nil,调用方应自己包装(ErrInternal 之类)。
func ToJSONRPCError(err error) *jsonrpc.Error {
	if code, ok := CodeForSentinel(err); ok {
		return &jsonrpc.Error{Code: code, Message: err.Error()}
	}
	return nil
}

// FromJSONRPCError 反向把 *jsonrpc.Error 翻成对应的 agentruntime sentinel。
// 未知 code 返原 err。
func FromJSONRPCError(err error) error {
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) {
		return err
	}
	if sent := SentinelFromCode(rpcErr.Code); sent != nil {
		return sent
	}
	return err
}

// SentinelFromCode 把 wire error code 直接翻成 agentruntime sentinel,无匹配
// 返 nil。客户端只拿到 (code, message) 二元组(走 RunResultDoneFrame 而非
// *jsonrpc.Error)时调它,免去人工合成 *jsonrpc.Error 再走 FromJSONRPCError
// 的绕远路 —— 这也是 runtimes/remote 包能彻底不依赖 daemon/rpc 的关键。
func SentinelFromCode(code int) error {
	switch code {
	case ErrCodeNoActiveTurn:
		return agentruntime.ErrNoActiveTurn
	case ErrCodeSteerNotFound:
		return agentruntime.ErrSteerNotFound
	case ErrCodeUnsupported:
		return agentruntime.ErrUnsupported
	case ErrCodeAborted:
		return agentruntime.ErrAborted
	}
	return nil
}

// CodeForSentinel 把 agentruntime sentinel 翻成 wire error code;非 sentinel
// 返 (0, false)。ToJSONRPCError 的核心,也方便 daemon 端调用方按需自己组帧。
func CodeForSentinel(err error) (int, bool) {
	switch {
	case errors.Is(err, agentruntime.ErrNoActiveTurn):
		return ErrCodeNoActiveTurn, true
	case errors.Is(err, agentruntime.ErrSteerNotFound):
		return ErrCodeSteerNotFound, true
	case errors.Is(err, agentruntime.ErrUnsupported):
		return ErrCodeUnsupported, true
	case errors.Is(err, agentruntime.ErrAborted):
		return ErrCodeAborted, true
	}
	return 0, false
}

// ── RPC types ───────────────────────────────────────────────────────────────

// ProviderSummary describes a single LLM provider configured in the daemon
// state. Returned by health.ping so desktop watcher can render sync status.
type ProviderSummary struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// OK 大部分 mutating 方法 (Steer / Abort / SetPermissionMode / SubmitAnswer /
// SubmitToolPermission) 不需要返回值,统一返这个空 struct 让 JSON-RPC 框架知道
// 是「成功无 payload」。
type OK struct{}

// CapabilitiesParams 按 BackendType 查 daemon 端 runtime 的能力矩阵。
type CapabilitiesParams struct {
	BackendType string `json:"backendType"`
}

// CapabilitiesResult 直接透传 capability.Capabilities — 含 Set 映射 + PermissionModeMeta。
type CapabilitiesResult struct {
	Capabilities capability.Capabilities `json:"capabilities"`
}

// HistoryMessageWire 是 agentruntime.HistoryMessage 的 wire 镜像。blocks 字段
// 走 blocks.StoredBlock(已经是 discriminated envelope)。
type HistoryMessageWire struct {
	Role   string               `json:"role"`
	Blocks []blocks.StoredBlock `json:"blocks,omitempty"`
}

// RunParams 是 runtime.run 的请求体。镜像 agentruntime.RunRequest 跨进程需要的
// 字段子集。Backend 用 json.RawMessage 透传,避免 wire 层硬依赖 entity 内部结构。
//
// 故意没有 Provider / GatewayURL / GatewayToken:
//   - Provider 含明文 APIKey,desktop 不该每个 turn 越线漂移到远端 daemon;
//   - GatewayURL/Token 来自 desktop 本机 127.0.0.1,在 daemon 主机上根本拨不到。
//
// daemon 端 handlers/runtime.go 在 Run 入口处自己用 ProviderLookup + 自家
// Gateway 解出这三者,desktop 端 chat_svc.runTurn 检 be.IsRemote() 后也不再填。
type RunParams struct {
	Backend           json.RawMessage      `json:"backend"`
	AgentID           int64                `json:"agentId"`
	SessionID         int64                `json:"sessionId"`
	Cwd               string               `json:"cwd,omitempty"`
	SystemPrompt      string               `json:"systemPrompt,omitempty"`
	ProviderSessionID string               `json:"providerSessionId,omitempty"`
	UserText          string               `json:"userText,omitempty"`
	History           []HistoryMessageWire `json:"history,omitempty"`
	Compact           bool                 `json:"compact,omitempty"`
	ForkAnchor        string               `json:"forkAnchor,omitempty"`
	PermissionMode    string               `json:"permissionMode,omitempty"`
	CollaborationMode string               `json:"collaborationMode,omitempty"`
	// LLMProviderKey 是 desktop 端关联的 provider stable key（UUID）。
	// daemon 用它做 ProviderLookup（FindByKey），不需要 desktop 越线传 APIKey。
	LLMProviderKey string `json:"llmProviderKey,omitempty"`
}

// RunAck 是 runtime.run 的同步返回,只回 echo 客户端传的 sessionID 供它确认。
// 真实 events 走 NotifyEvent 异步推;终态 RunResult 走 NotifyRunResultDone。
//
// LaunchPermissionMode 是 daemon 端 runtime spawn CLI 子进程实际下发的
// --permission-mode 值(claudecode 专用)。同步随 ack 回来,客户端立即写入
// RunResult.LaunchPermissionMode,让 chat_svc 在主进程侧持久化到
// session.PermissionModeAtLaunch。空串 = runtime 未指定(其它 backend / 复
// 用现有 CLI 进程)。
type RunAck struct {
	SessionID            int64  `json:"sessionId"`
	LaunchPermissionMode string `json:"launchPermissionMode,omitempty"`
}

// SteerParams 等同 agentruntime.Steerer.Steer 的入参。
type SteerParams struct {
	SessionID int64  `json:"sessionId"`
	QueuedID  string `json:"queuedId,omitempty"`
	Text      string `json:"text"`
}

// CancelSteerParams 等同 agentruntime.SteerCanceler.CancelSteer 的入参。
type CancelSteerParams struct {
	SessionID int64  `json:"sessionId"`
	QueuedID  string `json:"queuedId,omitempty"`
}

// CancelSteerResult 返已撤销的 queuedID 列表(空 queuedID 表示「清空所有未消费」,
// daemon 据此返若干 id)。
type CancelSteerResult struct {
	Removed []string `json:"removed,omitempty"`
}

// DrainParams 等同 agentruntime.SteerDrainer.DrainPending 的入参。
type DrainParams struct {
	SessionID int64 `json:"sessionId"`
}

// DrainResult 返本轮 daemon 已 ack 但 hook 没拉走的 mid-turn steer 列表,
// chat_svc 拿来 emit StreamSteerConsumed + persistAutoContinueTurn。
type DrainResult struct {
	Steers []agentruntime.ConsumedSteer `json:"steers,omitempty"`
}

// AbortParams 等同 agentruntime.Aborter.Abort 的入参。
type AbortParams struct {
	SessionID int64 `json:"sessionId"`
}

// SetPermissionModeParams 等同 agentruntime.PermissionModeSetter.SetPermissionMode 的入参。
type SetPermissionModeParams struct {
	SessionID int64  `json:"sessionId"`
	Mode      string `json:"mode"`
}

// SubmitAnswerParams 等同 agentruntime.AskAnswerSink.SubmitAnswer 的入参。
type SubmitAnswerParams struct {
	SessionID int64                      `json:"sessionId"`
	RequestID string                     `json:"requestId"`
	Questions []agentruntime.AskQuestion `json:"questions,omitempty"`
	Answers   []agentruntime.AskAnswer   `json:"answers,omitempty"`
	Skipped   bool                       `json:"skipped,omitempty"`
}

// SubmitToolPermissionParams 等同 agentruntime.ToolPermissionSink.SubmitToolPermission 的入参。
type SubmitToolPermissionParams struct {
	SessionID          int64  `json:"sessionId"`
	RequestID          string `json:"requestId"`
	Allow              bool   `json:"allow"`
	AlwaysAllowSession bool   `json:"alwaysAllowSession,omitempty"`
	DenyReason         string `json:"denyReason,omitempty"`
}

// ── Notification frames ─────────────────────────────────────────────────────

// EventFrame wraps a single agentruntime.Event for delivery over NotifyEvent.
// SessionID is transport metadata so the receiving end can route by session;
// Event payload is the JSON output of one of the 19 sealed Event types
// (see internal/pkg/agentruntime/event_wire.go for the marshaling rules).
type EventFrame struct {
	SessionID int64           `json:"sessionId"`
	Event     json.RawMessage `json:"event"`
}

// RunResultDoneFrame 在 daemon 端 events channel close 之后发一次,带完整 RunResult。
// 客户端拿到后填回 *remote.Runtime 持有的 *RunResult 指针,然后才 close 客户端的
// events channel,匹配 chat_svc 的契约(chat.go:1683-1722 在 channel close 后才读 result)。
//
// StopErrMsg / StopErrCode 用来在客户端把 RunResult.StopErr 重新 hydrate 成正确的
// sentinel(ErrAborted 等)。StopErrCode = 0 表示无 sentinel,StopErrMsg 仅作显示;
// = -32013 表示 ErrAborted;等等。
type RunResultDoneFrame struct {
	SessionID         int64      `json:"sessionId"`
	ProviderSessionID string     `json:"providerSessionId,omitempty"`
	Usage             *UsageWire `json:"usage,omitempty"`
	UserAnchor        string     `json:"userAnchor,omitempty"`
	Model             string     `json:"model,omitempty"`
	ContextWindow     int        `json:"contextWindow,omitempty"`
	StopErrMsg        string     `json:"stopErrMsg,omitempty"`
	StopErrCode       int        `json:"stopErrCode,omitempty"`
}

// UsageWire mirrors provider.Usage with stable lowerCamelCase tags. provider.Usage
// has no JSON tags so we wrap it for wire stability(同 event_wire.go 里同名 helper)。
type UsageWire struct {
	PromptTokens        int `json:"promptTokens"`
	CompletionTokens    int `json:"completionTokens"`
	ReasoningTokens     int `json:"reasoningTokens"`
	CachedTokens        int `json:"cachedTokens"`
	CacheCreationTokens int `json:"cacheCreationTokens"`
	TotalTokens         int `json:"totalTokens"`
}
