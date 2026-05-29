package app

import (
	"context"
	"strconv"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"agentre/internal/pkg/pty"
	"agentre/internal/pkg/pty/local"
	"agentre/internal/pkg/pty/remote"
	"agentre/internal/service/chat_svc"
	"agentre/internal/service/terminal_svc"
)

// ptyBackendAdapter bridges pty.Backend → terminal_svc.PTYBackend (same
// method set in different packages; explicit wrapper required by Go's
// nominal typing).
type ptyBackendAdapter struct{ be pty.Backend }

func (a ptyBackendAdapter) Open(ctx context.Context, spec pty.Spec) (pty.Handle, error) {
	return a.be.Open(ctx, spec)
}

func newTerminalService(appCtx context.Context) *terminal_svc.Service {
	localBE := local.NewBackend()
	remoteFactory := func(deviceIDStr string) (terminal_svc.PTYBackend, error) {
		deviceID, err := strconv.ParseInt(deviceIDStr, 10, 64)
		if err != nil {
			return nil, err
		}
		// MVP: release intentionally discarded — pool evicts on app shutdown.
		// See chat_svc.BorrowDeviceClient godoc for the leak rationale.
		c, _, err := chat_svc.BorrowDeviceClient(appCtx, deviceID)
		if err != nil {
			return nil, err
		}
		adapter := remote.NewClientAdapter(c)
		return ptyBackendAdapter{be: remote.NewBackend(adapter)}, nil
	}
	selector := terminal_svc.NewBackendSelector(
		ptyBackendAdapter{be: localBE}, remoteFactory,
	)
	emitter := terminal_svc.EmitterFunc(func(_ context.Context, name string, payload any) {
		wailsruntime.EventsEmit(appCtx, name, payload)
	})
	return terminal_svc.NewService(selector, emitter)
}
