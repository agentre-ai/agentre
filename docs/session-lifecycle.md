# Session Lifecycle

This doc owns the rules for creating and reusing `chat_sessions`. Read it before adding a feature that starts agent work from outside the normal chat composer, such as issues, hooks, or remote dispatch.

## Creation Boundary

`chat_svc` is the only service boundary that creates or reuses `chat_sessions`.

- Use `chat_svc.EnsureSession(ctx, req)` for domain-driven session creation.
- Keep the Wails binding thin: parse request -> call the owning service -> return.
- Other domains such as `issue_svc` and `hook_svc` must not call `chat_repo.Session().Create` directly.
- Repositories stay persistence-only; they do not decide whether a session should exist.

New domain-driven creation paths should use `EnsureSession`.

## Known Session Purposes

### Normal Chat

Normal chat creation still happens through `chat_svc.Send` with `SessionID=0`. The first user message creates the session, persists the user and assistant rows, and starts the runtime turn.

### Sidebar Visibility For Out-Of-Band Sessions

The left sidebar reads from `chat-agents-store`, a snapshot loaded by the `ListChatAgents` RPC. For normal chat it stays fresh because `ChatPanel` calls `onSidebarShouldReload` → `reloadSidebarSources()` on new-session / turn-done / steer.

Sessions created **outside** a `ChatPanel` bypass that path: they will not appear in the sidebar list — and, having no row, cannot show a running indicator — until some unrelated reload happens.

The single reusable entry point is `ensureSessionInSidebar(sessionId)` in `frontend/src/stores/sidebar-reload.ts`: if the id is not yet known to `chat-agents-store` it triggers `reloadSidebarSources()`, otherwise it short-circuits (cheap to call per turn). Any frontend event handler that learns of a new out-of-band session should call this so the session enters the list and the agent's run-light turns on.

Any future out-of-band session-creation path — a remote daemon creating a session, issue/hook dispatch — should reuse `ensureSessionInSidebar` from its frontend event handler instead of re-implementing the reload, so the sidebar stays correct without each producer hand-rolling it.

### Issue And Hook Dispatch

Issue and hook features that need to start agent work should call `chat_svc.EnsureSession` instead of writing `chat_sessions` themselves. Add a new `SessionPurpose` only when the identity and reuse key are different from an existing purpose.

For example, a future issue dispatch can define a purpose whose reuse key is `(issue_id, agent_id)` if redispatch should continue the same agent thread, or create a fresh normal chat if each dispatch must be isolated. That decision belongs in `chat_svc`, with the issue service only passing intent.

## Remote Execution

Remote execution does not move session creation to `agentred`.

The desktop app owns the local database and creates/reuses the `chat_sessions` row through `chat_svc`. When a turn starts, runtime selection decides whether execution is local or proxied through `remote.Runtime` to an `agentred` daemon. The remote daemon executes the turn and reports runtime state; it does not own the desktop session lifecycle. It does, however, own the durable record of that turn's event stream: `agentred` journals every notification to its own SQLite store under a gap-free monotonic `seq` before pushing it, so the record survives a dropped connection even though session identity, sidebar state, read state, and issue linkage stay authoritative on the desktop.

A session's push target on `agentred` is the connection that started it — identified by (device fingerprint, the client's own session id) — not "any connection from that device": a desktop can hold several authenticated connections at once (the one running the session, plus others such as a device-status heartbeat), and only the owning connection receives that session's notifications. If that connection dies, `agentred` does not clean up the session or its subprocess; it keeps journaling and suspends pushing until a same-fingerprint connection explicitly takes the session back over via the `runtime.session.attach` RPC.

To make this recoverable, `chat_sessions` carries three columns describing execution position: `exec_device_id` (the paired daemon), `exec_daemon_fingerprint` (that daemon's instance identity), and `event_cursor` (the last `agentred` `seq` the desktop has consumed). The cursor is only valid for the daemon instance that issued it — reinstalling, migrating, or wiping a daemon's data directory changes its fingerprint, and a cursor recorded against the old fingerprint is treated as invalid rather than replayed against the new instance's journal, so the session is instead treated as unrecoverable. On reconnect, for each session it still holds locally, `remote.Runtime` re-attaches ownership, validates and re-syncs the cursor, pulls journaled notifications incrementally since it, and queries pending decisions, replaying everything through the same handlers used for live traffic (`internal/daemon/handlers/session_catchup.go`). `agentred` also exposes a session-listing RPC for this catch-up family, but the desktop currently calls it only to probe whether a daemon supports catch-up at all, not to discover sessions it doesn't already know about.

## Adding A New Session Purpose

When adding a new feature that creates sessions:

1. Add a failing service test for the intended reuse key and error path.
2. Add the smallest `SessionPurpose` and request fields needed by `chat_svc.EnsureSession`.
3. Keep the feature service dependent on a narrow gateway/interface rather than on `chat_repo`.
4. Emit a domain event if the creating service stores the returned `SessionID` and the frontend needs to update live state.
5. If the session is created outside a `ChatPanel` (remote dispatch, issue/hook), have the frontend event handler call `ensureSessionInSidebar(sessionId)` so the new row appears in the sidebar and can show run state.
6. Document the new purpose in this file.
