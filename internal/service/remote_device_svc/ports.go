// Package remote_device_svc 编排桌面端 LAN 配对 / 探活 / TLS 信任管理。
package remote_device_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/daemon/client"
)

//go:generate mockgen -source ports.go -destination mock_remote_device_svc/mock_ports.go

// DaemonDialPort 抽象一次 RPC over WS 握手。真实现 wrap internal/daemon/client。
//
//   - Pair / Connect 走「短连接」语义：内部 Dial + 调一个 RPC + 关闭，
//     仅返回握手结果，连接不会留给调用方。
//   - Open 走「长连接」语义：内部 Dial + auth.connect 鉴权 + 把连接保留给
//     调用方，由调用方负责 defer Close。给 DialOnce 这类短 RPC 场景用。
type DaemonDialPort interface {
	Pair(ctx context.Context, args PairArgs) (PairResult, error)
	Connect(ctx context.Context, args ConnectArgs) (ConnectResult, error)
	Open(ctx context.Context, args ConnectArgs) (*client.Client, error)
}

// PairArgs 是 auth.pair 的入参。
type PairArgs struct {
	URL               string
	TLSMode           string
	TLSCertPEM        string
	Code              string
	DeviceName        string
	DeviceFingerprint string
}

// PairResult 是 auth.pair 的返回。
type PairResult struct {
	DeviceToken       string
	DaemonFingerprint string
	InstanceUUID      string
}

// ConnectArgs 是 auth.connect 的入参。
type ConnectArgs struct {
	URL                       string
	TLSMode                   string
	TLSCertPEM                string
	DeviceFingerprint         string
	DeviceToken               string
	ExpectedDaemonFingerprint string
}

// ConnectResult 是 auth.connect 的返回；ActualFingerprint 在 -32001 时由服务端 error.data 提供，
// 正常成功时填 expected。
type ConnectResult struct {
	InstanceUUID      string
	ActualFingerprint string
}

// KeychainPort 抽象 OS keychain（internal/pkg/keychain 接口的窄子集）。
type KeychainPort interface {
	Get(account string) (string, error)
	Set(account, secret string) error
	Delete(account string) error
}

// WatcherPort 是 remote_device_svc 反向消费 watcher_svc 的窄接口。SetWatcher 注入;
// 单测可以注入 mock 验证 Start/Stop/Restart 被调到。
type WatcherPort interface {
	Start(ctx context.Context, deviceID int64) error
	Stop(deviceID int64)
	Restart(ctx context.Context, deviceID int64) error
}
