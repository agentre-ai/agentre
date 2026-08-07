// Package workspacefs 是 agentred daemon 端 workspacefs.* RPC 的 handler 实现。
// 薄封装 internal/pkg/workspacefs 的 ListDir / GitChanges / GitBranches(设计
// 决策 4:核心逻辑放叶子包,host 的本机分支与 daemon handler 共用同一份实
// 现),把 sentinel 错误映射到 wire 层,由 register 把 sentinel 翻成
// *rpc.Error 返给 dispatcher。
package workspacefs

import (
	"context"
	"encoding/json"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	pkgworkspacefs "github.com/agentre-ai/agentre/internal/pkg/workspacefs"
	"github.com/agentre-ai/agentre/internal/pkg/workspacefs/wire"
)

// Options 注入测试 hook。生产用 NewHandlers(Options{}) 全用默认。
type Options struct {
	MaxEntries int // 默认 pkgworkspacefs.DefaultMaxEntries(2000)
}

// Handlers 持有 workspacefs RPC 方法集合,便于将来注入 dependency。
type Handlers struct {
	maxEntries int
}

// NewHandlers 构造 Handlers,未填字段使用安全默认值。
func NewHandlers(opts Options) *Handlers {
	h := &Handlers{maxEntries: opts.MaxEntries}
	if h.maxEntries <= 0 {
		h.maxEntries = pkgworkspacefs.DefaultMaxEntries
	}
	return h
}

// WrapFunc 抽象 daemon 包的 auth 检查闭包(避免 workspacefs 包反向依赖
// daemon)。生产传 requireAuth 包装,测试可传 identity。
type WrapFunc = func(rpc.HandlerFunc) rpc.HandlerFunc

// Register 把 ListDir / GitChanges / GitBranches 挂到 registry。
//   - wrap 用来套 requireAuth(生产)或 identity(单测)
//   - handler 返回的 wire sentinel 在此翻成 *rpc.Error,客户端 FromJSONRPCError
//     反向 rehydrate
func Register(reg *rpc.Registry, h *Handlers, wrap WrapFunc) {
	reg.Register(wire.MethodListDir, wrap(translateSentinel(handleListDir(h))))
	reg.Register(wire.MethodGitChanges, wrap(translateSentinel(handleGitChanges(h))))
	reg.Register(wire.MethodGitBranches, wrap(translateSentinel(handleGitBranches(h))))
}

func handleListDir(h *Handlers) rpc.HandlerFunc {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var req wire.ListDirReq
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &req); err != nil {
				return nil, rpc.ErrInvalidParams
			}
		}
		return h.ListDir(ctx, req)
	}
}

func handleGitChanges(h *Handlers) rpc.HandlerFunc {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var req wire.GitChangesReq
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &req); err != nil {
				return nil, rpc.ErrInvalidParams
			}
		}
		return h.GitChanges(ctx, req)
	}
}

func handleGitBranches(h *Handlers) rpc.HandlerFunc {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var req wire.GitBranchesReq
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &req); err != nil {
				return nil, rpc.ErrInvalidParams
			}
		}
		return h.GitBranches(ctx, req)
	}
}

func translateSentinel(fn rpc.HandlerFunc) rpc.HandlerFunc {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		res, err := fn(ctx, raw)
		if err != nil {
			if mapped := wire.ToJSONRPCError(err); mapped != nil {
				return nil, mapped
			}
		}
		return res, err
	}
}
