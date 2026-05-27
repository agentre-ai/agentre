package remote_device_svc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"agentre/internal/model/entity/paired_agentred_entity"
	"agentre/internal/pkg/code"
	"agentre/internal/pkg/keychain"
)

// pairingCodeLen 与 spec §4.3 一致 — base32 6 字符。
const pairingCodeLen = 6

func nowMs() int64 { return time.Now().UnixMilli() }

func (s *service) Add(ctx context.Context, req AddRequest) (*DeviceView, error) {
	if err := validateAddRequest(ctx, req); err != nil {
		return nil, err
	}
	existing, err := s.repo.FindByURL(ctx, req.URL)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, i18n.NewError(ctx, code.RemoteDeviceAlreadyPaired)
	}
	name, err := s.deriveDisplayName(ctx, req)
	if err != nil {
		return nil, err
	}
	fp, err := s.ensureDeviceFingerprint()
	if err != nil {
		return nil, err
	}
	result, err := s.dial.Pair(ctx, PairArgs{
		URL: req.URL, TLSMode: req.TLSMode, TLSCertPEM: req.TLSCertPEM,
		Code: req.PairingCode, DeviceName: name, DeviceFingerprint: fp,
	})
	if err != nil {
		return nil, translatePairError(ctx, err)
	}
	row := &paired_agentred_entity.PairedAgentred{
		Name: name, URL: req.URL,
		DaemonFingerprint: result.DaemonFingerprint, InstanceUUID: result.InstanceUUID,
		TLSMode: req.TLSMode, TLSCertPEM: req.TLSCertPEM,
		PairedAt: nowMs(), Status: 1, // consts.ACTIVE
	}
	if err := row.Check(ctx); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return nil, err
	}
	if err := s.keychain.Set(keychainAccountForToken(row.ID), result.DeviceToken); err != nil {
		if delErr := s.repo.Delete(ctx, row.ID); delErr != nil {
			logger.Ctx(ctx).Warn("rollback after keychain.Set failed", zap.Error(delErr))
		}
		return nil, i18n.NewError(ctx, code.RemoteDeviceKeychainFailed)
	}
	if s.watcher != nil {
		_ = s.watcher.Start(ctx, row.ID)
	}
	return toView(row), nil
}

func validateAddRequest(ctx context.Context, req AddRequest) error {
	if strings.TrimSpace(req.URL) == "" {
		return i18n.NewError(ctx, code.RemoteDeviceURLInvalid)
	}
	if !strings.HasPrefix(req.URL, "ws://") && !strings.HasPrefix(req.URL, "wss://") {
		return i18n.NewError(ctx, code.RemoteDeviceURLInvalid)
	}
	if !strings.HasSuffix(req.URL, "/rpc") {
		return i18n.NewError(ctx, code.RemoteDeviceURLInvalid)
	}
	if len(strings.TrimSpace(req.PairingCode)) != pairingCodeLen {
		return i18n.NewError(ctx, code.RemoteDevicePairingInvalid)
	}
	return nil
}

func (s *service) deriveDisplayName(ctx context.Context, req AddRequest) (string, error) {
	if n := strings.TrimSpace(req.DisplayName); n != "" {
		return n, nil
	}
	u, err := url.Parse(req.URL)
	if err != nil {
		return "", i18n.NewError(ctx, code.RemoteDeviceURLInvalid)
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil {
		rows, err := s.repo.List(ctx)
		if err != nil {
			return "", err
		}
		n := 1
		for _, r := range rows {
			if strings.HasPrefix(r.Name, "agentred-") {
				n++
			}
		}
		return fmt.Sprintf("agentred-%d", n), nil
	}
	first := strings.SplitN(host, ".", 2)[0]
	if first == "" {
		first = host
	}
	return first, nil
}

func (s *service) ensureDeviceFingerprint() (string, error) {
	fp, err := s.keychain.Get(accountForDeviceFingerprint)
	if err == nil && fp != "" {
		return fp, nil
	}
	if err != nil && !errors.Is(err, keychain.ErrNotFound) {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	// "sha256:" 前缀与 agentred spec §4.3 / state.json key 格式一致。
	newFP := "sha256:" + hex.EncodeToString(sum[:])
	if err := s.keychain.Set(accountForDeviceFingerprint, newFP); err != nil {
		return "", err
	}
	return newFP, nil
}

// translatePairError maps DaemonDialPort errors back to local i18n codes. The
// real DialPort implementation in dial.go wraps JSON-RPC error codes into
// sentinel errors below; the unit tests just pass plain errors and get the
// generic Unauthorized code.
var (
	ErrPairingInvalid = errors.New("pairing invalid")
	ErrUnauthorized   = errors.New("unauthorized")
	ErrTLSConfig      = errors.New("tls config invalid")
)

func translatePairError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, ErrPairingInvalid):
		return i18n.NewError(ctx, code.RemoteDevicePairingInvalid)
	case errors.Is(err, ErrUnauthorized):
		return i18n.NewError(ctx, code.RemoteDeviceUnauthorized)
	case errors.Is(err, ErrTLSConfig):
		return i18n.NewError(ctx, code.RemoteDeviceTLSConfigInvalid)
	default:
		// i18n.NewError 不保留 cause；把原始错误打到日志方便排查 LAN 网络 / TLS 握手问题。
		logger.Ctx(ctx).Warn("remote device dial failed", zap.Error(err))
		return i18n.NewError(ctx, code.RemoteDeviceDialFailed)
	}
}
