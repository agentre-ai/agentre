package app

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/peer"
	"github.com/agentre-ai/agentre/internal/service/server_svc"
)

// inboundPeer is the App-lifetime boundary for the desktop's relay presence.
type inboundPeer interface {
	Run(context.Context) error
}

var newInboundPeer = func(ctx context.Context) (inboundPeer, error) {
	link, err := server_svc.Server().NewInboundHubLink(ctx)
	if err != nil {
		return nil, err
	}
	return peer.NewInbound(link), nil
}

// startInboundPeer begins at most one inbound relay registration for this App
// process. A fresh login has no registration until the login event retries this
// method; an already-persisted login may start before token refresh completes,
// because HubLink re-resolves the token on every reconnect.
func (a *App) startInboundPeer(ctx context.Context) {
	a.peerMu.Lock()
	defer a.peerMu.Unlock()
	if a.peerCancel != nil {
		// A registration whose Run has already returned is dead even if its
		// cleanup goroutine has not yet cleared the lifecycle state. Rebuild on
		// this login instead of dropping it on the stale cancel token (the login
		// can land between Run-return and cleanup); the dead goroutine's
		// identity check in startInboundPeer's cleanup will not clear a fresh
		// registration.
		select {
		case <-a.peerDone:
			a.peerCancel = nil
			a.peerDone = nil
		default:
			return
		}
	}
	inbound, err := newInboundPeer(ctx)
	if errors.Is(err, server_svc.ErrNotLoggedIn) {
		return
	}
	if err != nil {
		logger.Ctx(ctx).Warn("app.startInboundPeer: create inbound relay", zap.Error(err))
		return
	}
	peerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	a.peerCancel = cancel
	a.peerDone = done
	go func() {
		defer close(done)
		if runErr := inbound.Run(peerCtx); runErr != nil {
			logger.Default().Warn("app.startInboundPeer: inbound relay stopped", zap.Error(runErr))
		}
		// HubLink 永久退出（如重试时钟崩溃）后，清掉本登记的 cancel/done：否则
		// peerCancel != nil 会让之后的 logged_in 事件永远无法重建入站登记。若登记
		// 已被 stopInboundPeer 清空或被更新的登记替换，身份比对失败、不重复清理。
		a.peerMu.Lock()
		if a.peerDone == done {
			a.peerCancel = nil
			a.peerDone = nil
		}
		a.peerMu.Unlock()
	}()
}

// stopInboundPeer cancels the relay connection and waits for HubLink to close
// its WebSocket, so a completed App shutdown is no longer addressable.
func (a *App) stopInboundPeer(_ context.Context) {
	a.peerMu.Lock()
	cancel, done := a.peerCancel, a.peerDone
	a.peerCancel = nil
	a.peerDone = nil
	a.peerMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}
