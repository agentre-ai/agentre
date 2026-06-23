// Package remotefs 是 agentred daemon 端 remotefs.* RPC 的 handler 实现。
// 提供 ListDir / Mkdir 的真 fs 调用,错误映射到 wire sentinel,由
// register 把 sentinel 翻成 *rpc.Error 返给 dispatcher。
package remotefs

import (
	"context"
	"encoding/json"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/pkg/remotefs/pathguard"
	"github.com/agentre-ai/agentre/internal/pkg/remotefs/wire"
)

// Options 注入测试 hook。生产用 NewHandlers(Options{}) 全用默认。
type Options struct {
	HomeFn     pathguard.HomeFunc // 默认 os.UserHomeDir
	MaxEntries int                // 默认 2000
}

const defaultMaxEntries = 2000

// Handlers 持有 remotefs RPC 方法集合,便于将来注入 dependency。
type Handlers struct {
	homeFn     pathguard.HomeFunc
	maxEntries int
}

// NewHandlers 构造 Handlers,未填字段使用安全默认值。
func NewHandlers(opts Options) *Handlers {
	h := &Handlers{
		homeFn:     opts.HomeFn,
		maxEntries: opts.MaxEntries,
	}
	if h.homeFn == nil {
		h.homeFn = osUserHomeDir
	}
	if h.maxEntries <= 0 {
		h.maxEntries = defaultMaxEntries
	}
	return h
}

// WrapFunc 抽象 daemon 包的 auth 检查闭包(避免 remotefs 包反向依赖 daemon)。
// 生产传 requireAuth 包装,测试可传 identity。
type WrapFunc = func(rpc.HandlerFunc) rpc.HandlerFunc

// Register 把 ListDir / Mkdir 挂到 registry。
//   - wrap 用来套 requireAuth(生产)或 identity(单测)
//   - handler 返回的 wire sentinel 在此翻成 *rpc.Error,客户端 FromJSONRPCError
//     反向 rehydrate
func Register(reg *rpc.Registry, h *Handlers, wrap WrapFunc) {
	reg.Register(wire.MethodListDir, wrap(translateSentinel(handleListDir(h))))
	reg.Register(wire.MethodMkdir, wrap(translateSentinel(handleMkdir(h))))
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

func handleMkdir(h *Handlers) rpc.HandlerFunc {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var req wire.MkdirReq
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, rpc.ErrInvalidParams
		}
		return h.Mkdir(ctx, req)
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
