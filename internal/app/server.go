package app

import (
	"agentre/internal/model/entity/server_state_entity"
	"agentre/internal/service/server_svc"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ServerGetState reads the persisted server_state row. UI uses this on mount to decide
// whether to show the Disconnected hero or the Connected panel.
func (a *App) ServerGetState() (*server_state_entity.ServerState, error) {
	return server_svc.Server().GetState(a.ctx)
}

// ServerCheckURL is the URL-validation probe used by the LoginDialog. It returns
// the server-reported version on success; the UI uses the absence of an error as
// the "healthy" signal.
func (a *App) ServerCheckURL(serverURL string) (string, error) {
	return server_svc.Server().CheckURL(a.ctx, serverURL)
}

// ServerStartLogin kicks off the device flow. On success it opens the user's
// system browser at the verification URL so they don't have to copy/paste.
func (a *App) ServerStartLogin(serverURL string) (*server_svc.StartLoginResult, error) {
	res, err := server_svc.Server().StartLogin(a.ctx, serverURL)
	if err != nil {
		return nil, err
	}
	if res != nil && res.VerificationURIComplete != "" {
		wailsruntime.BrowserOpenURL(a.ctx, res.VerificationURIComplete)
	}
	return res, nil
}

// ServerPollLoginToken is called by the frontend on a fixed interval until it
// returns (true, nil) (success) or a non-nil error (terminal failure).
func (a *App) ServerPollLoginToken(deviceCode string) (bool, error) {
	return server_svc.Server().PollLoginToken(a.ctx, deviceCode)
}

// ServerCancelLogin aborts an in-flight login so the user can retry with a
// different Server URL without restarting the app.
func (a *App) ServerCancelLogin() error {
	return server_svc.Server().CancelLogin(a.ctx)
}

// ServerListDevices returns the user's devices known to the Server.
func (a *App) ServerListDevices() ([]server_svc.Device, error) {
	return server_svc.Server().ListDevices(a.ctx)
}

// ServerLogout best-effort revokes server-side and clears the local state.
func (a *App) ServerLogout() error {
	return server_svc.Server().Logout(a.ctx)
}
