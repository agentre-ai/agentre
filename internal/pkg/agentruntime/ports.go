package agentruntime

import (
	"context"
	"encoding/json"
)

//go:generate mockgen -source ports.go -destination mock_agentruntime/mock_ports.go

// DaemonClientPort 抽象 internal/daemon/client.Client，仅为 RemoteRunner 单测注入用。
// 生产路径上 *client.Client 直接实现这个接口（方法签名已对齐，无需 adapter）。
// 编译期断言落在 daemon/client 包内,避免 agentruntime(抽象层)反向依赖 daemon。
type DaemonClientPort interface {
	Call(ctx context.Context, method string, params, result any) error
	Notify(method string, params any) error
	Handle(method string, fn func(ctx context.Context, params json.RawMessage) (any, error))
	Closed() <-chan struct{}
	Close() error
}
