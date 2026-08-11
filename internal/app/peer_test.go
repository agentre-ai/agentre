package app

import (
	"context"
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
