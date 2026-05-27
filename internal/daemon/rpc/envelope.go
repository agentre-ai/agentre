// Package rpc owns the JSON-RPC 2.0 transport — envelope marshaling,
// auth gate, method dispatch, and the WS/Unix-socket transports.
// Business handlers live in daemon/handlers and consume *Conn.
// Spec reference: docs/superpowers/specs/2026-05-21-agentred-mvp-design.md §3.
//
// Error / Frame.Error 的数据类型已下沉到 internal/pkg/jsonrpc,本包通过 type alias
// 暴露,让上层(agentruntime/runtimes/remote/wire、remotefs/wire)只依赖纯协议
// 包而不反向依赖 daemon。
package rpc

import (
	"encoding/json"

	"agentre/internal/pkg/jsonrpc"
)

// Frame is the on-wire JSON-RPC 2.0 message. A single shape carries
// requests, responses, and notifications — discriminated by presence
// of ID + Method + (Result|Error).
type Frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

func (f Frame) IsRequest() bool      { return f.Method != "" && len(f.ID) > 0 }
func (f Frame) IsNotification() bool { return f.Method != "" && len(f.ID) == 0 }
func (f Frame) IsResponse() bool     { return len(f.ID) > 0 && f.Method == "" }
func (f Frame) IsError() bool        { return f.Error != nil }

// Error 是 JSON-RPC 2.0 标准 error object 的 alias —— 原始定义在
// internal/pkg/jsonrpc。type alias 让所有现有 rpc.Error 调用点无需修改,
// 同时让 wire 层只依赖 jsonrpc 包。
type Error = jsonrpc.Error

// Standard JSON-RPC 2.0 + agentred custom codes 透传自 jsonrpc 包,
// 让 daemon/rpc 内部代码继续 `rpc.ErrXxx` 引用,变量保持单实例。
var (
	ErrParse           = jsonrpc.ErrParse
	ErrInvalidRequest  = jsonrpc.ErrInvalidRequest
	ErrMethodNotFound  = jsonrpc.ErrMethodNotFound
	ErrInvalidParams   = jsonrpc.ErrInvalidParams
	ErrInternal        = jsonrpc.ErrInternal
	ErrUnauthorized    = jsonrpc.ErrUnauthorized
	ErrSessionNotFound = jsonrpc.ErrSessionNotFound
	ErrProviderMissing = jsonrpc.ErrProviderMissing
	ErrPairing         = jsonrpc.ErrPairing
	ErrShuttingDown    = jsonrpc.ErrShuttingDown
)
