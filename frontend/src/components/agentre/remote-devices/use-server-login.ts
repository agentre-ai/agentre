import { useCallback, useEffect, useState } from "react";

import {
  ServerCancelLogin,
  ServerCheckURL,
  ServerGetState,
  ServerLogout,
  ServerPollLoginToken,
  ServerStartLogin,
} from "../../../../wailsjs/go/app/App";
import type { server_state_entity } from "../../../../wailsjs/go/models";

export type ServerState = server_state_entity.ServerState;

// Mirrors server_state_entity.ServerState.IsLoggedIn() (Go) — the
// desktop is only "logged in" once user, device, and keychain are all bound.
// Kept in sync by hand since the entity method isn't reachable from the
// frontend; internal/model/entity/server_state_entity/server_state.go is the
// source of truth this must match.
export function isLoggedIn(state: ServerState | null): boolean {
  return (
    !!state &&
    state.ServerUserID !== 0 &&
    state.DeviceID !== 0 &&
    state.KeychainAccount !== ""
  );
}

/**
 * Wraps the existing ServerXxx Wails bindings (internal/app/server.go) for
 * the account login entry point + identity/logout card in RemoteDevicesPanel.
 * Owns only state-fetch/refresh; the device-flow polling machinery lives in
 * LoginDialog (login-dialog.tsx), which receives the raw action functions.
 */
export function useServerLogin() {
  const [state, setState] = useState<ServerState | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const s = await ServerGetState();
      setState(s);
    } catch {
      // Fresh install / DB hiccup: treat as logged-out rather than crash the
      // panel — matches useRemoteDevices' account.known=false precedent.
      setState(null);
    }
  }, []);

  useEffect(() => {
    void refresh().finally(() => setLoading(false));
  }, [refresh]);

  return {
    state,
    loggedIn: isLoggedIn(state),
    loading,
    refresh,
    logout: async () => {
      await ServerLogout();
      await refresh();
    },
    checkURL: ServerCheckURL,
    startLogin: ServerStartLogin,
    pollLoginToken: ServerPollLoginToken,
    cancelLogin: ServerCancelLogin,
  };
}
