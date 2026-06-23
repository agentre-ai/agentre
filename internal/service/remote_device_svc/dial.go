// internal/service/remote_device_svc/dial.go
package remote_device_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/agentre-ai/agentre/internal/daemon/client"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
)

// realDial wraps internal/daemon/client to satisfy DaemonDialPort.
type realDial struct{}

// NewDaemonDial constructs the production DaemonDialPort.
func NewDaemonDial() DaemonDialPort { return &realDial{} }

func (realDial) Pair(ctx context.Context, args PairArgs) (PairResult, error) {
	tlsCfg, err := client.BuildTLSConfig(client.TLSMode(args.TLSMode), args.TLSCertPEM)
	if err != nil {
		return PairResult{}, fmt.Errorf("%w: %v", ErrTLSConfig, err)
	}
	c, err := client.Dial(ctx, client.Options{URL: args.URL, TLSConfig: tlsCfg})
	if err != nil {
		return PairResult{}, err
	}
	defer func() { _ = c.Close() }()
	var res rpc.PairResult
	if err := c.Call(ctx, "auth.pair", rpc.PairParams{
		Code: args.Code, DeviceName: args.DeviceName, DeviceFingerprint: args.DeviceFingerprint,
	}, &res); err != nil {
		return PairResult{}, translatePairRPCError(err)
	}
	return PairResult{
		DeviceToken: res.DeviceToken, DaemonFingerprint: res.DaemonFingerprint, InstanceUUID: res.InstanceUUID,
	}, nil
}

func (realDial) Connect(ctx context.Context, args ConnectArgs) (ConnectResult, error) {
	tlsCfg, err := client.BuildTLSConfig(client.TLSMode(args.TLSMode), args.TLSCertPEM)
	if err != nil {
		return ConnectResult{}, fmt.Errorf("%w: %v", ErrTLSConfig, err)
	}
	c, err := client.Dial(ctx, client.Options{URL: args.URL, TLSConfig: tlsCfg})
	if err != nil {
		return ConnectResult{}, err
	}
	defer func() { _ = c.Close() }()
	var res rpc.ConnectResult
	if err := c.Call(ctx, "auth.connect", rpc.ConnectParams{
		DeviceFingerprint: args.DeviceFingerprint, DeviceToken: args.DeviceToken,
		ExpectedDaemonFingerprint: args.ExpectedDaemonFingerprint,
	}, &res); err != nil {
		return ConnectResult{}, translateConnectRPCError(err)
	}
	return ConnectResult{InstanceUUID: res.InstanceUUID, ActualFingerprint: args.ExpectedDaemonFingerprint}, nil
}

// Open 与 Connect 同样跑 TLS 握手 + auth.connect 鉴权，但**不**关闭连接，
// 把 *client.Client 直接交给调用方。调用方必须 defer c.Close()。
// 给 DialOnce 这类「短 RPC 但需要保持已鉴权会话」的场景用。
func (realDial) Open(ctx context.Context, args ConnectArgs) (*client.Client, error) {
	tlsCfg, err := client.BuildTLSConfig(client.TLSMode(args.TLSMode), args.TLSCertPEM)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTLSConfig, err)
	}
	c, err := client.Dial(ctx, client.Options{URL: args.URL, TLSConfig: tlsCfg})
	if err != nil {
		return nil, err
	}
	var res rpc.ConnectResult
	if err := c.Call(ctx, "auth.connect", rpc.ConnectParams{
		DeviceFingerprint: args.DeviceFingerprint, DeviceToken: args.DeviceToken,
		ExpectedDaemonFingerprint: args.ExpectedDaemonFingerprint,
	}, &res); err != nil {
		_ = c.Close()
		return nil, translateConnectRPCError(err)
	}
	return c, nil
}

// translatePairRPCError maps daemon JSON-RPC error codes to the svc-internal
// sentinels consumed by Add's translatePairError. Unmapped errors pass through
// and are caught by the default branch (RemoteDeviceDialFailed).
func translatePairRPCError(err error) error {
	var rpcErr *rpc.Error
	if errors.As(err, &rpcErr) {
		switch rpcErr.Code {
		case -32004: // rpc.ErrPairing
			return ErrPairingInvalid
		case -32001: // rpc.ErrUnauthorized
			return ErrUnauthorized
		}
	}
	return err
}

// translateConnectRPCError additionally distinguishes TOFU mismatch from
// generic Unauthorized by checking if the daemon's HandleConnect set a
// fingerprint-related message.
func translateConnectRPCError(err error) error {
	var rpcErr *rpc.Error
	if errors.As(err, &rpcErr) {
		switch rpcErr.Code {
		case -32001:
			if isFingerprintMismatch(rpcErr) {
				return ErrTOFUMismatch
			}
			return ErrUnauthorized
		}
	}
	return err
}

// isFingerprintMismatch inspects the JSON-RPC error to decide whether the
// daemon's -32001 was a TOFU mismatch (vs a stale token). The daemon's
// HandleConnect emits message "daemon fingerprint mismatch (TOFU)" — we
// detect that case-insensitively. Some implementations may instead populate
// error.data.actualFingerprint; we cover both.
func isFingerprintMismatch(e *rpc.Error) bool {
	if strings.Contains(strings.ToLower(e.Message), "fingerprint") {
		return true
	}
	if len(e.Data) > 0 {
		var m map[string]any
		if json.Unmarshal(e.Data, &m) == nil {
			if _, has := m["actualFingerprint"]; has {
				return true
			}
		}
	}
	return false
}
