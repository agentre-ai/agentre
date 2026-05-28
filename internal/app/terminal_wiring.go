package app

import (
	"context"
	"strconv"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/pkg/pty"
	"agentre/internal/pkg/pty/local"
	"agentre/internal/pkg/pty/remote"
	agentbackendrepo "agentre/internal/repository/agent_backend_repo"
	agentrepo "agentre/internal/repository/agent_repo"
	chatrepo "agentre/internal/repository/chat_repo"
	"agentre/internal/service/chat_svc"
	"agentre/internal/service/terminal_svc"
)

type sessionLookupAdapter struct{}

func (sessionLookupAdapter) Lookup(ctx context.Context, sessionID int64) (
	*chat_entity.Session, *agent_backend_entity.AgentBackend, string, error,
) {
	sess, err := chatrepo.Session().Find(ctx, sessionID)
	if err != nil {
		return nil, nil, "", err
	}
	if sess == nil {
		return nil, nil, "", terminal_svc.ErrSessionNotFound
	}
	agent, err := agentrepo.Agent().Find(ctx, sess.AgentID)
	if err != nil {
		return nil, nil, "", err
	}
	var be *agent_backend_entity.AgentBackend
	if agent != nil && agent.AgentBackendID > 0 {
		be, err = agentbackendrepo.AgentBackend().Find(ctx, agent.AgentBackendID)
		if err != nil {
			return nil, nil, "", err
		}
	}
	cwd, err := chat_svc.ResolveSessionCwd(ctx, sess, be)
	if err != nil {
		return nil, nil, "", err
	}
	return sess, be, cwd, nil
}

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
		// MVP: lease is borrowed here and never explicitly released —
		// the lease stays alive for the daemon connection lifetime,
		// cleaned up on app shutdown. Acceptable for single-user MVP.
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
	return terminal_svc.NewService(sessionLookupAdapter{}, selector, emitter)
}
