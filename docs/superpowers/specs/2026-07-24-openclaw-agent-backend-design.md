# OpenClaw Agent Backend implementation record

> Status: implemented on branch `feat/openclaw-integration-mockup`; final repository-wide verification is recorded in the implementation report.
> Protocol validation uses a local fake Gateway only. No production Gateway or real user data is used.

This document began as the OpenClaw design snapshot. It now records the implementation that replaced the earlier assumptions, including the exact RPC surface, persisted data, security boundary, supported capabilities, and known limitations.

Protocol shapes were fact-checked on 2026-07-24 against the locally installed official OpenClaw `2026.7.1-2 (0790d9f)`, primarily `docs/gateway/protocol.md`, `docs/gateway/operator-scopes.md`, `docs/tools/exec-approvals.md`, and the shipped Gateway schemas/handlers under `dist/`. This is source-level protocol verification; the automated tests still use only a local fake Gateway and do not contact a real Gateway.

## 1. Scope and invariants

OpenClaw is a Gateway-native AgentRE backend. Its production turn path is:

```text
React UI
  -> internal/app Wails bindings
  -> agent_backend_svc / chat_svc
  -> agentruntime openclaw.Runtime
  -> OpenClaw Gateway WebSocket RPC protocol 4
```

The implementation does not use ACP, an OpenResponses/Chat Completions HTTP API, an OpenClaw CLI subprocess, or a UI mock as a runtime fallback. The frontend does not expose a new HTTP application API.

The local desktop path is implemented. Remote `agentred` execution is deliberately unavailable until AgentRE has remote secret enrollment/reference support; the desktop does not serialize a Gateway token into daemon run parameters.

## 2. Persisted model and secret storage

`agent_backends` stores only presentation-safe configuration:

| Field | Meaning |
| --- | --- |
| `type = "openclaw"` | backend discriminator |
| `openclaw_gateway_url` | Gateway WebSocket URL |
| `openclaw_agent_id` | selected/default Gateway agent; empty uses the Gateway default |
| `openclaw_default_model` | optional configured model override |
| `openclaw_session_mode` | fixed to `per-agentre-session` |
| `device_id` | empty for the supported local path; a non-empty remote device currently reports capability unavailable |

Migration `202607240001_openclaw_agent_backend` appends those four OpenClaw columns. It contains no token, ciphertext, secret reference, or TLS-bypass column.

The Gateway token is stored through `internal/pkg/keychain` under:

```text
agentre.openclaw.backend.<backendID>.token
```

The stable Ed25519 device identity seed uses the separate keychain account:

```text
agentre.openclaw.device.identity.seed
```

Create, update, retain, replace, explicit clear, and delete all update the keychain and database transaction boundary with rollback behavior. Wails DTOs expose only `hasToken`; the actual token is a transient argument to the OpenClaw-specific create/update/test bindings and is never returned to the frontend. Editing starts with an empty token input and never rehydrates the saved value.

### 2.1 Entity validation

- `ws://` is accepted only for loopback hosts such as `127.0.0.1`, `localhost`, and `::1`.
- Non-loopback Gateways require `wss://`.
- URL userinfo, query strings, and fragments are rejected so credentials cannot hide in the URL.
- OpenClaw configuration is mutually exclusive with LLM provider, CLI path, model routes, sandbox, approval policy, EnvJSON, reasoning, default permission-mode, and the generic default-model fields.
- Session mode values other than `per-agentre-session` are rejected.

## 3. Gateway WebSocket protocol client

`internal/pkg/openclawgateway` is a leaf transport package with no service, repository, Wails, or agentruntime dependency.

### 3.1 Handshake

1. Dial the validated WebSocket URL.
2. Require the first frame to be `event/connect.challenge` and read its nonce.
3. Send the first request as `req/connect` with `minProtocol = 4` and `maxProtocol = 4`.
4. Identify the client as role `operator`; attach the token, stable device public key, and Ed25519 challenge proof.
5. Require `hello-ok`, negotiated protocol 4, and all required granted scopes:
   - `operator.read`
   - `operator.write`
   - `operator.approvals`
6. Validate that the Gateway advertises every runtime RPC and event listed in section 4.

The challenge proof payload uses OpenClaw's `v3` device-auth signature contract; this version label is independent of Gateway RPC protocol 4. The signed newline-delimited fields follow the official order exactly:

```text
v3
deviceId
clientId
clientMode
role
comma-joined scopes
signedAtMs
token
nonce
normalized platform
normalized deviceFamily
```

The implementation uses the official `cli` client ID and mode enum values.

### 3.2 Frames, timeouts, gaps, and reconnect

- Requests are `req` frames with unique AgentRE request IDs.
- Responses are routed to pending calls by response `id`; response order is irrelevant.
- Structured RPC errors preserve `code`, `details.reason`, retryability, and retry delay while redacting the configured token from messages.
- Unknown events are delivered generically and ignored by runtimes that do not consume them.
- Connection event sequence is deduplicated. A forward sequence gap is surfaced on a separate gap channel.
- Default handshake timeout is 15 seconds and request timeout is 30 seconds.
- Reconnect uses exponential backoff from 1 to 30 seconds and repeats the authenticated handshake.
- A disconnect fails pending transport calls. An active turn never resends the user message on reconnect.

## 4. Probe contract

`TestOpenClawAgentBackend` performs a read-only Gateway-native handshake and discovery. It requires these advertised methods:

```text
agent
agent.wait
chat.abort
agents.list
models.list
exec.approval.list
exec.approval.resolve
```

It also requires these events:

```text
agent
chat
exec.approval.requested
exec.approval.resolved
```

Discovery calls are:

```text
agents.list {}
models.list {"view":"configured"}
```

The response includes Gateway version, negotiated protocol, granted scopes, methods, events, agents, and models. A configured agent/model that is absent is rejected. The UI displays structured error codes for authentication/RPC errors, scope and protocol mismatch, missing method/event, missing agent/model, timeout, unavailable secret storage, remote-secret unavailability, and generic connection failures.

Probe never starts a turn and never reads provider history.

## 5. Session and turn lifecycle

Each AgentRE `chat_session` maps to exactly one stable OpenClaw session key:

```text
agentre:<backendID>:<chatSessionID>
```

An existing `RunRequest.ProviderSessionID` takes precedence. The selected value is returned through `RunResult.ProviderSessionID`, so normal AgentRE persistence reuses it on later turns. There is no provider-history import and no code path that overwrites AgentRE's persisted UI history from OpenClaw history.

The turn is submitted exactly once with:

```text
agent {
  message,
  agentId?,
  model?,
  sessionKey,
  deliver: false,
  idempotencyKey
}
```

The UUID idempotency key is generated before submission and is also the provisional run identity if the acknowledgement is lost. A reconnect or event gap reconciles with `agent.wait {runId, timeoutMs: 0}`. The runtime does not blindly replay `agent`.

Events are isolated by `runId`, `sessionKey`, and the inner agent/chat sequence. Duplicate and old-run frames are discarded. The translator supports:

- assistant text delta/snapshot;
- thinking delta/snapshot;
- tool start and tool result (the result closes the UI tool lifecycle);
- usage and final RunResult usage;
- lifecycle/chat final, error, and abort.

Abort is exactly `chat.abort {sessionKey, runId}` and is idempotent at the runtime boundary through the stable session/run identity plus a one-call guard. An aborted turn ends with `ErrAborted`; it is distinct from a transport failure.

The official `agent` request schema accepts and requires `idempotencyKey`, so AgentRE supplies one. The official closed schemas for `chat.abort` and `exec.approval.resolve`, however, do not accept an arbitrary `idempotencyKey`; adding one would make otherwise valid protocol-4 requests fail validation. Those operations therefore converge idempotently using the protocol's stable `sessionKey + runId` or approval ID and AgentRE's local in-flight/terminal guards. This is an intentional schema-driven exception to the original blanket side-effect-key requirement, not an undocumented omission.

## 6. Exec approval lifecycle

Exec approvals are a dedicated capability (`CapExecApproval` / `ExecApprovalSink`), not `ToolPermissionSink` and not tool completion.

### 6.1 Request and reconciliation

- The runtime listens for `exec.approval.requested` and `exec.approval.resolved`.
- Before submitting the initial turn it calls `exec.approval.list {}`.
- On ready/reconnect it reconciles approvals first, then calls `agent.wait`.
- List and event records are scoped to the current `sessionKey` and deduplicated by approval ID.
- An approval missing from the next pending list becomes `expired`.
- `expiresAtMs` is enforced by a per-approval cancelable timer even while the Gateway remains connected.

Only the safe UI projection is persisted: ID, command text/preview, allowed decisions, host/node/agent, timestamps, status, decision, and resolver identity. Environment and the canonical `systemRunPlan` are not copied to the block.

For Node execution, Gateway `systemRunPlan` is authoritative. AgentRE reads its presentation/session fields when present and never reconstructs, changes, or sends the plan back.

### 6.2 Decisions and terminal races

The UI renders only the intersection of Gateway `allowedDecisions` and:

```text
allow-once
allow-always
deny
```

Resolution sends exactly:

```text
exec.approval.resolve {"id":"...", "decision":"..."}
```

The runtime rejects a decision not present in the request's dynamic allow-list. Duplicate clicks and duplicate resolves send at most one RPC. `APPROVAL_ALREADY_RESOLVED` converges to a resolved terminal; `APPROVAL_NOT_FOUND` and local expiry converge to expired. Multiple simultaneous approvals keep the session waiting until the last pending approval becomes terminal.

Approval terminal emits and persists an updated `exec_approval` card but does not emit a tool result, close the stream, or claim that execution finished. The command's later tool/run lifecycle remains separate.

## 7. Wails and frontend

OpenClaw-specific Wails bindings are:

```text
CreateOpenClawAgentBackend(req, token)
UpdateOpenClawAgentBackend(req, token, clearToken)
TestOpenClawAgentBackend(req, token)
ResolveExecApproval(req)
```

The production backend editor is implemented in the normal AgentRE settings component system. It does not import `frontend/src/mockups/*`. New OpenClaw backends are locked to the local device; the editor supports Gateway URL, write-only token semantics, fixed session mode, connection testing, discovered agent/model selection, structured errors, and an explicit remote capability-unavailable message. An existing remote OpenClaw record can still be opened without silently rewriting its `device_id`, but its device selector remains disabled and probe/run requests return capability unavailable.

The production exec approval card uses AgentRE transcript primitives plus shadcn Button/Badge/Spinner. Loading disables every decision, an in-flight guard prevents duplicate calls, errors are inline and retryable, and resolved/expired cards are read-only. Requested and terminal events update one block by approval ID. Reattaching an active session moves pending approval overlays into the live stream exactly once; terminal history remains persisted.

All new static copy is present in both `en` and `zh-CN` locales. Dynamic command, host, node, agent, and resolver values are not translated.

## 8. Remote daemon boundary

OpenClaw typed approval events are representable by the existing sealed runtime event wire, but the daemon does not register the OpenClaw runtime. A daemon run request for backend type `openclaw` returns:

```text
openclaw backend not supported in agentred: remote secret enrollment is unavailable
```

Daemon `RunParams` has no OpenClaw token or generic secret field. This is an intentional security boundary, not a transport TODO hidden behind a plaintext token.

## 9. Capability matrix and limitations

| Capability | Status |
| --- | --- |
| local Gateway protocol/auth/discovery | supported |
| stable per-AgentRE-session mapping | supported |
| text/thinking/tool/usage/final/error/abort | supported |
| reconnect, gap detection, run reconciliation | supported |
| exec approvals and reconnect reconciliation | supported |
| remote `agentred` | unavailable until remote secret enrollment/reference exists |
| plugin approvals | not implemented; event/adapter extension point only |
| OpenClaw ask-user | unavailable; no verified request/reply protocol is claimed |
| steer/cancel-steer | unavailable |
| provider rewind/fork/compact/delete | unavailable |
| attachments/image input | unavailable |
| OpenClaw subagent/autonomous events | unavailable |
| provider history import | intentionally not performed |
| CLI/ACP/HTTP runtime fallback | intentionally absent |

## 10. Test evidence map

All Gateway protocol tests use local `httptest` WebSocket servers and generated non-secret credentials. Important suites include:

- entity URL/field/session/secret serialization and migration schema tests;
- token and device-identity keychain lifecycle tests;
- challenge/connect, auth/scope/protocol failures, out-of-order responses, unknown events, gaps, timeout, and reconnect tests;
- probe discovery and selected agent/model/method/event boundary tests;
- runtime session reuse, translation, isolation/deduplication, abort, idempotency, reconnect, `agent.wait`, and approval lifecycle tests;
- chat block persistence/handler/service/Wails projection tests;
- daemon explicit-unavailable and event-wire round-trip tests;
- production backend editor, approval card, stream/store/host/reattach/transcript, and i18n tests.

The implementation has not been validated against a real production Gateway. Protocol drift must be handled by extending the fake fixtures from confirmed official Gateway frames and keeping connect-time method/event negotiation strict.

## 11. Design deviations resolved during implementation

- The design proposed a database ciphertext field. The implementation instead uses the project's keychain abstraction and stores no secret column; this removes Wails/DB leak paths.
- The design diagram proposed daemon-side OpenClaw execution. The current wire cannot enroll or reference a daemon-local secret safely, so remote execution is explicitly unavailable.
- The design mentioned optional `sessions.create/resolve/describe` and history reconciliation. The implemented MVP uses the stable `sessionKey` directly and reconciles the active run with `agent.wait`; it does not read provider history.
- The design described exec approvals as a possible reuse of tool permission. The implementation gives them separate sealed events, capability, persistence block, Wails action, and UI lifecycle so approval resolution cannot be confused with tool execution completion.
- The design required an idempotency key on every side-effect RPC. Official protocol-4 schemas require it on `agent` but reject extra keys on `chat.abort` and `exec.approval.resolve`; those two operations use stable protocol identities plus local one-call/terminal convergence instead.
- Plugin approvals and OpenClaw ask-user remain unavailable because their concrete protocol was not confirmed in this implementation.
