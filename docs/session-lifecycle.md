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

The desktop app owns the local database and creates/reuses the `chat_sessions` row through `chat_svc`. When a turn starts, runtime selection decides whether execution is local or proxied through `remote.Runtime` to an `agentred` daemon. The remote daemon executes the turn and reports runtime state; it does not own the desktop session lifecycle.

This keeps session identity, sidebar state, read state, issue linkage, and notifications in one local source of truth.

## Provider Session Mapping

When a runtime has its own provider-side session identity, the AgentRE `chat_sessions` row remains the UI/history source of truth and stores only the provider mapping in `ProviderSessionID`. A runtime must not replace AgentRE message history with provider history during ordinary resume.

The OpenClaw backend uses the deterministic key `agentre:<backendID>:<chatSessionID>` when `ProviderSessionID` is empty, returns that key through `RunResult.ProviderSessionID`, and reuses the persisted value on later turns. Reconnect reconciles the active Gateway run; it does not import or overwrite chat history.

## Adding A New Session Purpose

When adding a new feature that creates sessions:

1. Add a failing service test for the intended reuse key and error path.
2. Add the smallest `SessionPurpose` and request fields needed by `chat_svc.EnsureSession`.
3. Keep the feature service dependent on a narrow gateway/interface rather than on `chat_repo`.
4. Emit a domain event if the creating service stores the returned `SessionID` and the frontend needs to update live state.
5. If the session is created outside a `ChatPanel` (remote dispatch, issue/hook), have the frontend event handler call `ensureSessionInSidebar(sessionId)` so the new row appears in the sidebar and can show run state.
6. Document the new purpose in this file.
