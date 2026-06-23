# Session Lifecycle

This doc owns the rules for creating and reusing `chat_sessions`. Read it before adding a feature that starts agent work from outside the normal chat composer, such as group chat, issues, hooks, or remote dispatch.

## Creation Boundary

`chat_svc` is the only service boundary that creates or reuses `chat_sessions`.

- Use `chat_svc.EnsureSession(ctx, req)` for domain-driven session creation.
- Keep the Wails binding thin: parse request -> call the owning service -> return.
- Other domains such as `group_svc`, `issue_svc`, and `hook_svc` must not call `chat_repo.Session().Create` directly.
- Repositories stay persistence-only; they do not decide whether a session should exist.

`EnsureGroupMemberSession` is a legacy compatibility wrapper. New domain-driven creation paths should prefer `EnsureSession`.

## Known Session Purposes

### Normal Chat

Normal chat creation still happens through `chat_svc.Send` with `SessionID=0`. The first user message creates the session, persists the user and assistant rows, and starts the runtime turn.

### Group Member Backing Session

Group members are first-class `group_members` rows. Their `backing_session_id` may be `0`, meaning the member is in the group but has never received a turn.

Group chat creates the backing session lazily:

- Creating a group does not create member sessions.
- Adding or inviting a member does not create a member session.
- The scheduler creates/reuses the backing session only when a user or agent message is actually delivered to that member.
- After creation, `group_members.backing_session_id` is updated and a `member_updated` group event is emitted so the frontend can enable member-session navigation.

Group backing sessions are ordinary `chat_sessions` rows with `group_id > 0`. They reuse chat history, runtime selection, steering, tool approval, permission mode, and remote execution behavior.

### Sidebar Visibility For Out-Of-Band Sessions

The left sidebar reads from `chat-agents-store`, a snapshot loaded by the `ListChatAgents` RPC. For normal chat it stays fresh because `ChatPanel` calls `onSidebarShouldReload` → `reloadSidebarSources()` on new-session / turn-done / steer.

Sessions created **outside** a `ChatPanel` bypass that path: they will not appear in the sidebar list — and, having no row, cannot show a running indicator — until some unrelated reload happens. A group member backing session is exactly this case: it is created lazily on the @-mention turn, and the member turn runs through the scheduler + `GroupChat`, never through a `ChatPanel`.

The single reusable entry point is `ensureSessionInSidebar(sessionId)` in `frontend/src/stores/sidebar-reload.ts`: if the id is not yet known to `chat-agents-store` it triggers `reloadSidebarSources()`, otherwise it short-circuits (cheap to call per turn). `GroupEventsHost` calls it when it receives a `member_run_state` `running` event on the global `groups:run_state` channel, so the new backing session enters the list and the agent's run-light turns on whether or not the group page is open.

Any future out-of-band session-creation path — a remote daemon creating a session, issue/hook dispatch — should reuse `ensureSessionInSidebar` from its frontend event handler instead of re-implementing the reload, so the sidebar stays correct without each producer hand-rolling it.

### Issue And Hook Dispatch

Issue and hook features that need to start agent work should call `chat_svc.EnsureSession` instead of writing `chat_sessions` themselves. Add a new `SessionPurpose` only when the identity and reuse key are different from an existing purpose.

For example, a future issue dispatch can define a purpose whose reuse key is `(issue_id, agent_id)` if redispatch should continue the same agent thread, or create a fresh normal chat if each dispatch must be isolated. That decision belongs in `chat_svc`, with the issue service only passing intent.

## Remote Execution

Remote execution does not move session creation to `agentred`.

The desktop app owns the local database and creates/reuses the `chat_sessions` row through `chat_svc`. When a turn starts, runtime selection decides whether execution is local or proxied through `remote.Runtime` to an `agentred` daemon. The remote daemon executes the turn and reports runtime state; it does not own the desktop session lifecycle.

This keeps session identity, sidebar state, read state, group membership, issue linkage, and notifications in one local source of truth.

## Adding A New Session Purpose

When adding a new feature that creates sessions:

1. Add a failing service test for the intended reuse key and error path.
2. Add the smallest `SessionPurpose` and request fields needed by `chat_svc.EnsureSession`.
3. Keep the feature service dependent on a narrow gateway/interface rather than on `chat_repo`.
4. Emit a domain event if the creating service stores the returned `SessionID` and the frontend needs to update live state.
5. If the session is created outside a `ChatPanel` (group member, remote dispatch, issue/hook), have the frontend event handler call `ensureSessionInSidebar(sessionId)` so the new row appears in the sidebar and can show run state.
6. Document the new purpose in this file.
