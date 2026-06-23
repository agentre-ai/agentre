// Package wire 定义 agentre ↔ agentred 的 remotefs.* RPC 协议:参数 / 结果 /
// 错误 sentinel 与 JSON-RPC error code 的双向翻译。daemon 端 handler 与 host
// 端 svc 共享这一份类型,避免 JSON shape 漂移。
//
// 命名约定与 internal/pkg/agentruntime/runtimes/remote/wire 一致:
//   - 方法在 "remotefs.*" 命名空间下
//   - 字段名 lowerCamelCase
//   - 错误码 -32030..-32035 是稳定 wire 值,wrapGuarded handler 返回
//     *rpc.Error 由本包翻译,客户端用 FromJSONRPCError rehydrate。
package wire

import (
	"errors"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
)

// ── RPC method names ────────────────────────────────────────────────────────

const (
	MethodListDir = "remotefs.listDir"
	MethodMkdir   = "remotefs.mkdir"
)

// ── Error codes ─────────────────────────────────────────────────────────────

const (
	ErrCodePathRefused = -32030
	ErrCodePermDenied  = -32031
	ErrCodeNotFound    = -32032
	ErrCodeNotDir      = -32033
	ErrCodeMkdirExists = -32034
	ErrCodeInvalidName = -32035
)

// ── Sentinel errors ─────────────────────────────────────────────────────────

var (
	ErrPathRefused = errors.New("remotefs: path refused")
	ErrPermDenied  = errors.New("remotefs: permission denied")
	ErrNotFound    = errors.New("remotefs: not found")
	ErrNotDir      = errors.New("remotefs: not a directory")
	ErrMkdirExists = errors.New("remotefs: target already exists")
	ErrInvalidName = errors.New("remotefs: invalid name")
)

// ToJSONRPCError 把 remotefs sentinel 包成 *rpc.Error,daemon handler 返回。
// 非 sentinel 返 nil,调用方应自己包装(ErrInternal 之类)。
func ToJSONRPCError(err error) *rpc.Error {
	switch {
	case errors.Is(err, ErrPathRefused):
		return &rpc.Error{Code: ErrCodePathRefused, Message: err.Error()}
	case errors.Is(err, ErrPermDenied):
		return &rpc.Error{Code: ErrCodePermDenied, Message: err.Error()}
	case errors.Is(err, ErrNotFound):
		return &rpc.Error{Code: ErrCodeNotFound, Message: err.Error()}
	case errors.Is(err, ErrNotDir):
		return &rpc.Error{Code: ErrCodeNotDir, Message: err.Error()}
	case errors.Is(err, ErrMkdirExists):
		return &rpc.Error{Code: ErrCodeMkdirExists, Message: err.Error()}
	case errors.Is(err, ErrInvalidName):
		return &rpc.Error{Code: ErrCodeInvalidName, Message: err.Error()}
	}
	return nil
}

// FromJSONRPCError 反向把 *rpc.Error 翻成 sentinel。未知 code 返原 err。
// host svc 拿到后再 i18n.NewError(ctx, code.RemoteFsXxx) 包给前端。
func FromJSONRPCError(err error) error {
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) {
		return err
	}
	switch rpcErr.Code {
	case ErrCodePathRefused:
		return ErrPathRefused
	case ErrCodePermDenied:
		return ErrPermDenied
	case ErrCodeNotFound:
		return ErrNotFound
	case ErrCodeNotDir:
		return ErrNotDir
	case ErrCodeMkdirExists:
		return ErrMkdirExists
	case ErrCodeInvalidName:
		return ErrInvalidName
	}
	return err
}

// ── ListDir ─────────────────────────────────────────────────────────────────

// ListDirReq.Path 为空 → daemon 端解析为 $HOME。
type ListDirReq struct {
	Path string `json:"path"`
}

type Entry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`  // 字节;目录恒为 0
	ModTime int64  `json:"mtime"` // unix seconds
	Symlink bool   `json:"symlink,omitempty"`
}

type ListDirResp struct {
	Path      string  `json:"path"` // resolved 后的绝对路径
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated,omitempty"` // 超 maxEntries 时为 true
}

// ── Mkdir ───────────────────────────────────────────────────────────────────

type MkdirReq struct {
	Parent string `json:"parent"` // 必填绝对路径
	Name   string `json:"name"`   // 不含 /,daemon 再校验
}

type MkdirResp struct {
	Path string `json:"path"` // = filepath.Join(parent, name)
}
