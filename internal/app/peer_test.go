package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

type inboundPeerStub struct {
	started chan struct{}
	stopped chan struct{}
}

func (p *inboundPeerStub) Run(ctx context.Context) error {
	close(p.started)
	<-ctx.Done()
	close(p.stopped)
	return nil
}

// Given App owns a started inbound relay peer, when Shutdown runs, then it
// cancels the peer lifetime and waits for the relay registration to disappear.
func TestAppInboundPeer_GivenRunning_WhenShutdown_ThenStopsBeforeReturning(t *testing.T) {
	stub := &inboundPeerStub{started: make(chan struct{}), stopped: make(chan struct{})}
	previous := newInboundPeer
	newInboundPeer = func(context.Context) (inboundPeer, error) { return stub, nil }
	t.Cleanup(func() { newInboundPeer = previous })

	app := &App{}
	app.startInboundPeer(context.Background())
	select {
	case <-stub.started:
	case <-time.After(time.Second):
		t.Fatal("App did not start its inbound relay peer")
	}

	app.stopInboundPeer(context.Background())
	select {
	case <-stub.stopped:
	case <-time.After(time.Second):
		t.Fatal("App returned before the inbound relay peer stopped")
	}
}

// Given the inbound relay peer exits permanently (its HubLink retry clock gave
// up, so Run returned without a shutdown), when a later login event calls
// startInboundPeer again, then a fresh registration is created rather than
// skipped by the stale cancel token left behind by the dead peer.
func TestAppInboundPeer_GivenPermanentExit_ThenLaterLoginRestartsRegistration(t *testing.T) {
	firstExited := make(chan struct{})
	second := &inboundPeerStub{started: make(chan struct{}), stopped: make(chan struct{})}
	calls := 0
	previous := newInboundPeer
	newInboundPeer = func(context.Context) (inboundPeer, error) {
		calls++
		if calls == 1 {
			return &permanentlyExitingInboundPeer{exited: firstExited}, nil
		}
		return second, nil
	}
	t.Cleanup(func() { newInboundPeer = previous })

	app := &App{}
	app.startInboundPeer(context.Background())
	select {
	case <-firstExited:
	case <-time.After(time.Second):
		t.Fatal("first inbound relay did not exit permanently")
	}
	requireEventuallyNil(t, func() bool {
		app.peerMu.Lock()
		defer app.peerMu.Unlock()
		return app.peerCancel == nil
	}, "dead relay must clear its lifecycle state so a later login can rebuild it")

	app.startInboundPeer(context.Background())
	select {
	case <-second.started:
	case <-time.After(time.Second):
		t.Fatal("later login did not restart the inbound relay after permanent exit")
	}
	app.stopInboundPeer(context.Background())
}

// permanentlyExitingInboundPeer 模拟 HubLink 重试时钟崩溃后的永久退出：Run 直接
// 返回错误，不等待 ctx 取消，也不自行恢复。
type permanentlyExitingInboundPeer struct {
	exited chan struct{}
}

func (p *permanentlyExitingInboundPeer) Run(ctx context.Context) error {
	close(p.exited)
	return errors.New("relay retry clock failed; giving up")
}

func requireEventuallyNil(t *testing.T, condition func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(time.Millisecond)
	}
}
