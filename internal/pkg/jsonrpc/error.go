// Package jsonrpc 提供 JSON-RPC 2.0 协议的共享数据类型(Error envelope + 标准错误码)。
//
// 任何走 JSON-RPC 协议的子系统(daemon/rpc transport、agentruntime/runtimes/remote/wire、
// remotefs/wire)都依赖这里,避免反向依赖 internal/daemon — 协议是底层、transport
// 是实现,方向应该是 transport → 协议。
package jsonrpc

import "encoding/json"

// Error 是 JSON-RPC 2.0 标准 error object,实现 error 接口让 handler 可以直接 return。
//
// 历史:这个类型原来在 internal/daemon/rpc/envelope.go,后移到 jsonrpc 包,
// daemon/rpc 通过 type alias 继续暴露。这样 agentruntime / remotefs 的 wire 包
// 不再需要 import daemon/rpc,根除 agentruntime → daemon 的反向依赖。
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Standard JSON-RPC 2.0 + agentred custom codes. Spec §3.6.
var (
	ErrParse           = &Error{Code: -32700, Message: "Parse error"}
	ErrInvalidRequest  = &Error{Code: -32600, Message: "Invalid request"}
	ErrMethodNotFound  = &Error{Code: -32601, Message: "Method not found"}
	ErrInvalidParams   = &Error{Code: -32602, Message: "Invalid params"}
	ErrInternal        = &Error{Code: -32603, Message: "Internal error"}
	ErrUnauthorized    = &Error{Code: -32001, Message: "Unauthorized"}
	ErrSessionNotFound = &Error{Code: -32002, Message: "Session not found"}
	ErrProviderMissing = &Error{Code: -32003, Message: "LLM provider not configured"}
	ErrPairing         = &Error{Code: -32004, Message: "Pairing code invalid / expired / rate-limited"}
	ErrShuttingDown    = &Error{Code: -32005, Message: "Daemon shutting down"}
)
