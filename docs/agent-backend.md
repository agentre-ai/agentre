# Onboarding a New AI Agent Backend

This is the path, the change points, the constraints, and the pitfalls you must walk through when adding a new Agent backend (e.g. Gemini CLI / your own CLI / another in-process SDK). Read all of it before writing code — the `agentruntime.Runtime` interface looks narrow, but the supporting pieces you have to fill in are spread across six layers: entity / repo / service / wire / daemon / frontend.

> Prerequisite reading: [architecture.md](architecture.md) (layering conventions), [development.md](development.md) (TDD/BDD + Fix Discipline).

---

## 0. Questions to settle first

Ask yourself before writing code, and **if anything is unclear, stop and ask the user** — don't get halfway and discover you have to start over:

1. **Run mode**: is it in-process (like builtin, directly consuming an LLMProvider through cago app/coding), or does it wrap a local CLI subprocess (like claudecode / codex / piagent)? The two are completely different in ProviderType matching, cli_path validation, env passthrough, and Prober implementation.
2. **Remote execution support**: does it need to be dispatched to a LAN machine via the `agentred` daemon? If so, make sure the `RuntimeFor` that init() registers does not depend on desktop-only services (chat_repo / GUI), and that the wire protocol covers all RPC frames.
3. **Capability matrix**: can it mid-turn steer? Can it abort? Does it have a `can_use_tool` protocol? Does it support ask_user_question? Does it support plan / permission mode switching? Can it fork a session? List these out item by item — this directly determines which optional `agentruntime` sub-interfaces you need to implement.
4. **Protocol shape**: is the event stream stdout JSONL (claudecode)? JSON-RPC over stdio (codex app-server)? Or an in-memory channel (builtin)? Whether the translator is a stateless pure function vs. a stateful aggregator determines where you write your first test.
5. **Session reuse vs. single spawn**: does each turn start a new process (codex's current approach), or is the subprocess long-lived and reused via an LRU cache (claudecode)? Reuse means managing idle evict, abort unlocking, and cross-turn state (permission mode / steer queue).

---

## 0.5 Quick reference for existing backends (interface / capability / Permission Mode)

The repository currently has four built-in backends, with clearly distinct roles. **Pick the right slot for your new backend before you start** by mapping it to one of these:

- **builtin** — runs cago `app/coding` in-process, directly consuming the `llm_provider` config. Its role is "lightweight built-in"; it exposes steer / cancel / abort / image input — the capabilities that an in-process single-provider mode naturally supports — without CLI subprocess overhead, and without advanced protocols like plan / tool approval.
- **claudecode** — wraps the local `claude` CLI (Anthropic family), communicating bidirectionally via stdout JSONL + `control_request` frames, with the subprocess long-lived and the session reused via LRU. The most fully featured: it can run plan / `can_use_tool` / `AskUserQuestion` / Subagent / fork-session / mid-turn permission-mode switching / image input.
- **codex** — wraps the local `codex` CLI (OpenAI family), interacting via the JSON-RPC-over-stdio app-server protocol, starting a new process per turn (fire-and-forget). Natively supports context-window reporting, native compact turns, image input, and tool approval via the app-server `requestApproval` protocol (allow/deny + remember-for-session, though with **no DenyReason feedback** and no `can_use_tool`-shaped control frames), but cannot switch permission mode mid-turn and does not emit Subagent events.
- **piagent** — wraps the local `pi` CLI (Pi coding agent RPC mode); it is not bound to an Agentre `LLMProvider`, but reads Pi's own `~/.pi/agent` config and auth. Supports steer / abort / compact / image input / context-window reporting; session context is resumed across turns via an Agentre-dedicated Pi session file. It does not support tool approval, reverse Q&A, fork-session, or permission mode meta.

> The repository also has `runtimes/remote/`, which is **not a standalone backend** — it is the proxy used when the desktop calls a remote `agentred` daemon. Its capability is synced from the daemon-side real backend via `Prefetch`, so it is not listed separately in this section.

Data sources (any schema change must be synced in three places):

- The `Capabilities()` implementation in `internal/pkg/agentruntime/runtimes/{builtin,claudecode,codex,piagent}/runtime.go`
- The cap constants in `internal/pkg/agentruntime/capability/capability.go`
- The matrix assertions in `runtime_test.go::TestXxxCapabilities`

### Capability matrix (Capabilities)

Each row is a **reverse channel** (host→backend), **except the final `CapAutonomousTurn` row, which is the only forward channel** (backend→host: the backend spontaneously emits a whole turn). The rightmost column gives the capability's semantics and "why ❌" — before copying anything, confirm whether your backend actually has an equivalent protocol; if not, honestly return `ErrUnsupported` instead of force-fitting a fake implementation.

| Capability (constant / wire string) | Sub-interface | builtin | claudecode | codex | piagent | Description |
| --- | --- | --- | --- | --- | --- | --- |
| `CapSteer` / `"steer"` | `Steerer` | ✅ | ✅ | ✅ | ✅ | mid-turn injection of a user message; chat_svc generates the queuedID, and after the backend actually consumes it, it must emit `SteerConsumed` |
| `CapCancelSteer` / `"cancel_steer"` | `SteerCanceler` | ✅ | ✅ | ❌ | ❌ | retract after injection; once codex / piagent steer has entered the protocol it cannot be recalled |
| `CapDrainSteer` / `"drain_steer"` | `SteerDrainer` | ❌ | ✅ | ❌ | ❌ | leftover between turns is automatically forwarded into the next turn; only claudecode maintains a local hook queue |
| `CapAbort` / `"abort"` | `Aborter` | ✅ | ✅ | ✅ | ✅ | the "Stop" button; must be idempotent + must unblock all blocking I/O, otherwise the frontend will be stuck on "generating" forever |
| `CapSetPermission` / `"set_permission_mode"` | `PermissionModeSetter` | ❌ | ✅ mid-turn | ✅ launch only | ❌ | switch permission mode at runtime; the codex protocol does not allow mid-turn switching, so it persists to DB and takes effect on the next spawn; piagent does not expose permission mode meta |
| `CapAnswerUserAsk` / `"answer_user_ask"` | `AskAnswerSink` | ❌ | ✅ | ✅ | ❌ | reverse-asking the user a question (single-select / multi-select / Other / password field); Skip must go through deny rather than an empty map, otherwise the turn silently hangs |
| `CapToolPermission` / `"tool_permission_gate"` | `ToolPermissionSink` | ❌ | ✅ `can_use_tool` | ✅ `requestApproval` | ❌ | allow/deny approval before tool execution + "Remember for session" (alwaysAllowSession). claudecode goes through the `can_use_tool` control_request and feeds DenyReason back to the LLM as a tool_result; codex goes through the app-server `requestApproval` protocol — it carries allow/deny + remember-for-session but has **no DenyReason feedback** (the deny-message param is ignored, the protocol has no deny field). piagent has no equivalent protocol |
| `CapForkSession` / `"fork_session"` | `RunRequest.ForkAnchor` built in | ❌ | ✅ `--fork-session` | ✅ `thread/rollback` | ❌ | "Regenerate" derives a new session from a given anchor and reruns |
| `CapReportContextWindow` / `"report_context_window"` | emit `ContextWindowUpdated` | ❌ | ✅ | ✅ | ✅ | the runtime emits after probing the model's actual context-window size, for the frontend usage bar; the claudecode SDK does not report the window itself, so the translator looks it up in `llmcatalog` on the `system.init` frame as a fallback; piagent reports it at the end of each round via the Pi RPC `get_session_stats.contextUsage.contextWindow`, and falls back to looking up `llmcatalog` by the model from the usage frame |
| `CapCompact` / `"compact"` | `RunRequest.Compact=true` | ❌ | ❌ | ✅ | ✅ | native compact turn — have the LLM summarize history and then clear the occupied space; piagent goes through Pi RPC compact |
| `CapImageInput` / `"image_input"` | `RunRequest.UserBlocks` contains `blocks.ImageBlock` | ✅ | ✅ | ✅ | ✅ | the user message can carry PNG / JPEG / WebP images. builtin passes cago blocks through directly; claudecode encodes inline images into a base64 `image` content block of the stream-json user frame (image first, text after — natively supported by the CLI); codex materializes inline images into temporary local files and then goes through the app-server `localImage`; piagent passes the RPC image content through |
| `CapGoal` / `"goal"` | `GoalController` | ❌ | ❌ | ✅ | ❌ | session/thread-level **objective** state: the host reads/sets/clears a persistent goal (`Objective` + `Status` + optional `TokenBudget`, plus `TokensUsed` / `TimeUsedSeconds` counters) bound to the provider thread. Only codex has it natively (app-server thread-goal protocol, `runtimes/codex/runtime.go` `GetGoal/SetGoal/ClearGoal`); chat_svc surfaces it via `GetGoal` / `SetGoal` / `StartGoal` / `ClearGoal`, and `remote.Runtime` forwards it over `runtime.goal.{get,set,clear}`. builtin / claudecode / piagent don't declare it, so chat_svc returns `ErrUnsupported` |
| `CapMCPTools` / `"mcp_tools"` | `RunRequest.MCPServers` (no sub-interface) | ❌ | ✅ | ✅ | ❌ | host→backend **launch-time MCP injection**: the runtime accepts `RunRequest.MCPServers` (`[]MCPServerSpec`: `Name` / `URL` / `Headers`, http transport) and starts the turn with those extra MCP tool servers wired in. claudecode renders each spec into a `--mcp-config` entry (`{"mcpServers":{"<name>":{"type":"http","url":"...","headers":{...}}}}`) and auto-adds `mcp__<Name>__<tool>` entries to `--allowedTools`; codex renders each spec into one-shot `--config mcp_servers.<name>...` overrides (`url`, `http_headers`, `enabled_tools`, `default_tools_approval_mode="approve"`) and bypasses its persistent app-server cache for MCP-injected turns so launch-time config is always loaded. Group-chat orchestration is the first consumer: a member backend **must** declare this cap to be admitted to a group (`group_svc.backendSupportsGroup` → `chat_svc.AgentBackendHasCapability`); builtin / piagent ignore `RunRequest.MCPServers` |
| `CapSkills` / `"skills"` | `RunRequest.EnabledPlugins` (no sub-interface) | ❌ | ✅ | ✅ | ❌ | host→backend **launch-time skill-pack/plugin injection**: the runtime accepts `RunRequest.EnabledPlugins` (`map[string]bool`, **sparse** — only the agent's explicit overrides: force-on=true / force-off=false; plugins not listed inherit the user's global CLI config). claudecode merges it into `--settings`'s `enabledPlugins` at spawn (`buildSkillsSettings`); codex renders it into one-shot `--config plugins."<id>".enabled=<bool>` overrides and bypasses the persistent app-server cache for plugin-injected turns so launch-time config is always loaded. Granularity is **per-plugin / skill-pack only** for Agentre's cross-backend UI. builtin / piagent ignore `RunRequest.EnabledPlugins`. See §7 |
| `CapAutonomousTurn` / `"autonomous_turn"` | `AutonomousTurnSource` | ❌ | ✅ | ❌ | ❌ | **the only forward channel (backend→host)**: the backend spontaneously runs a whole turn with *no* user input. claudecode's CLI, after a `run_in_background` Bash task completes, autonomously injects `<task-notification>` and runs a full turn (a second `result` frame); `pkg/claudecode.Session`'s persistent reader routes it to `AutonomousTurns()`, the runtime bridges each one to `agentruntime.AutonomousTurn`, and chat_svc's per-session watcher (`driveAutonomousTurn`) persists it as a **pure assistant turn (no user row)** and surfaces it live via the session-level `chat:autonomous:<sessionID>` event. Backends without this behavior simply don't declare the cap |

> **Rule**: calling the corresponding interface of an undeclared cap must return `agentruntime.ErrUnsupported` (a sentinel error, transparent across processes, which chat_svc translates into a wire code accordingly). Declaring cap=true but not implementing the interface will be caught by the `TestXxxCapabilities` matrix test (type-assert failure).

> **Group-chat passthrough (no per-backend work)**: alongside MCP injection, orchestration also feeds role / roster context through `chat_svc` `SendRequest.SystemPromptSuffix`, which is concatenated onto `RunRequest.SystemPrompt` upstream (empty for single chat ⇒ byte-identical to today). A runtime therefore just sees a longer system prompt and needs no special handling — only `RunRequest.MCPServers` requires a `CapMCPTools` runtime to actually consume it.

### Runtime characteristics

This table answers "what does it look like when it runs" — process shape, session lifetime, translator design, plan / Subagent event sources. Which slot a new backend falls into is basically determined by the protocol shape you pick, so **decide first, then write code** — don't switch midway.

| Dimension | builtin | claudecode | codex | piagent | Notes |
| --- | --- | --- | --- | --- | --- |
| Run shape | in-process (cago app/coding + LLMProvider) | CLI subprocess (stdout JSONL) | CLI subprocess (JSON-RPC over stdio app-server) | CLI subprocess (Pi RPC mode) | determines the Prober / cli_path validation / env assembly path |
| ProviderType binding | any LLM provider | Anthropic family (incl. gateway proxy) | OpenAI / Codex family | not bound to a provider; reads `~/.pi/agent` | the entity `BackendKind.ProviderTypeMatch` implementation; piagent always false |
| Session mode | turn-scoped cago `Runner`, destroyed when the turn ends | long-lived subprocess + LRU cache reuse, reusing the session across turns | new process per turn, no local reuse | new Pi client per turn; resumed via `<AppDataDir>/piagent/sessions/agentre-<sessionID>.jsonl` | reuse means managing idle evict / abort unlocking / cross-turn state |
| Translator | pure function (cago events → sealed) | pure function + `task_aggregator` maintaining the Subagent list across turns | pure function | pure function (Pi RPC events → sealed) | state aggregation is uniformly done in the `Run` drain loop; the translator must be able to run independently in a table-driven test |
| Plan source | does not emit | `TodoWrite` tool inline (canonical) + `Task*` incremental aggregation (PlanUpdated snapshot) | `turn/plan/updated` (Steps) + `item/plan/delta` (Text) merged into a single PlanUpdated | does not emit | what downstream chat_svc sees is the same sealed `agentruntime.PlanUpdated` |
| Reverse Q&A | not supported | control_request `can_use_tool` + `AskUserQuestion` tool | app-server `item/tool/requestUserInput` JSON-RPC | not supported | claudecode uses the question text as the key, codex uses the question ID as the key |
| Subagent events | ❌ | ✅ `SubagentStarted/Progress/Done` | ❌ | ❌ | only claudecode has a native `Task` tool protocol |
| Remote daemon | ✅ | ✅ | ✅ | ✅ | the runtime must not depend on desktop-only services (chat_repo / GUI); state is returned via `RunResult` |

### PermissionModeMeta

Permission mode is a **session-level permission state machine** (not plan content). Each backend's mode set and mid-turn switching ability are completely different — the frontend `PermissionModePill` reads this meta directly to decide rendering; backends that do not declare `CapSetPermission` (builtin / piagent) have no meta.

| Field | builtin | claudecode | codex | piagent | Field meaning |
| --- | --- | --- | --- | --- | --- |
| `AllowedModes` | — (does not declare CapSetPermission) | `default, acceptEdits, plan, bypassPermissions` | `default, plan` | — (does not declare CapSetPermission) | the set of valid mode names for this backend; the service layer applies it as a whitelist |
| `DefaultMode` | — | `"acceptEdits"` | `"default"` | — | the default mode used for UI display / computation (the default value chat_svc persists) |
| `LaunchDefaultMode` | — | `""` (does not attach `--permission-mode`; pkg/claudecode internally falls back to acceptEdits) | `"default"` (the protocol requires an explicit collaborationMode at launch, **cannot be empty**) | — | the fallback string the wire layer uses at spawn; empty vs. non-empty distinguishes "user didn't explicitly pick" vs. "explicitly picked default" |
| `SwitchableDuringTurn` | — | `true` (writes a control_request to switch immediately) | `false` (takes effect on the next spawn after persisting) | — | whether the frontend pill is clickable when `agentStatus=="waiting"` |
| `Order` | — | same as AllowedModes | same as AllowedModes | — | the pill cycle order; the UI nexts through it directly in order |

> **Easily confused fields**: `chat_sessions.permission_mode` = the CLI runtime's current mode (modified by SetPermissionMode); `chat_sessions.permission_mode_at_launch` = the snapshot delivered at spawn (returned by the runtime via `RunResult.LaunchPermissionMode`). The frontend uses the former to display the current state; the latter decides whether `bypassPermissions` appears in the pill — the item only shows if bypass was explicitly chosen at launch, to avoid it being abused after the fact.

---

## 1. The overall onboarding path (the 7 mandatory layers)

A new backend = one cut along each of the following 7 layers, none of which can be skipped:

| Layer | Location | Mandatory? |
| --- | --- | --- |
| 1. Entity type | `internal/model/entity/agent_backend_entity/{agent_backend.go, kinds.go}` | Yes |
| 2. Database migration | `migrations/YYYYMMDDNNNN_*.go` + append to the end of `migrationList()` | Only when adding a new column |
| 3. CLI/Prober/Env wiring | `internal/pkg/agentruntime/clienv.go` + `internal/service/agent_backend_svc/{prober.go, resolve_cli.go}` | Mandatory for the CLI kind; Prober only for in-process |
| 4. Runtime + Translator | `internal/pkg/agentruntime/runtimes/<name>/{runtime.go, translator.go}` | Yes |
| 5. Daemon import | blank import in `internal/daemon/runtime_imports.go` | Mandatory if remote is supported |
| 6. Wails bindings and svc types | `internal/service/agent_backend_svc/types.go` + `internal/app/agent.go` (if a new field is introduced) | Only when a new field |
| 7. Frontend bindings + UI gating | regenerate `frontend/wailsjs/` (`make generate`) + capability pill / selector | Yes |

---

## 2. What must be done at each layer

### 2.1 Entity (`agent_backend_entity`)

- Add a `BackendType` constant in `agent_backend.go`:

  ```go
  TypeMyAgent BackendType = "myagent"
  ```

- Implement the `BackendKind` interface in `kinds.go` and register it in `backendKinds`:

  ```go
  type myAgentKind struct{}
  func (myAgentKind) Type() BackendType { return TypeMyAgent }
  func (myAgentKind) KnownAliases() []string { return nil }            // nil if there are no model_routes
  func (myAgentKind) ProviderTypeMatch(t llm_provider_entity.ProviderType) bool {
      return t == llm_provider_entity.TypeXxx
  }
  func (myAgentKind) AllowsCLIPath() bool { return true }              // fill true for the CLI kind
  func (myAgentKind) ValidateExtra(ctx context.Context, b *AgentBackend) error {
      // validate the kind-specific fields here: sandbox / approval / default_permission_mode / env_json;
      // the common fields (name / type / env_json reserved keys / model_routes alias set / reasoning_effort)
      // have already been run through AgentBackend.Check, so **do not repeat them**.
      return nil
  }
  ```

- The `IsXxx()` convenience predicate methods are not mandatory, but adding them improves readability for chat_svc / the frontend (see `IsClaudeCode/IsCodex/IsPiAgent`).
- Rich domain model boundary: **new fields go into the entity first, and the methods (validation, defaults, serialization) are written in the entity too**; the service only does cross-entity orchestration. `AgentBackend.Check` already dispatches to `BackendKind.ValidateExtra` — never write another switch in the service.

### 2.2 Database migration

Only touch this when there are new fields. Rules:

- **Create a new** `YYYYMMDDNNNN_xxx.go` in `migrations/`, exporting `migrationYYYYMMDDNNNN()`.
- **Do not modify existing migrations** — even fixes are written as new patch migrations.
- DDL goes through native SQL (`tx.Exec(...)`); do not rely on the implicit behavior of `AutoMigrate`.
- Append to the **end** of `migrations/migrations.go::migrationList()`.
- The default value must let existing rows pass entity.Check (usually `'' / '{}' / 0`).

### 2.3 Prober + CLI probing + Env wiring

`agent_backend_svc.Prober` abstracts "run one self-check round against a backend → return reply or error", for the frontend "test connectivity" button.

- Register in `prober.go`:

  ```go
  var proberRegistry = map[agent_backend_entity.BackendType]Prober{
      agent_backend_entity.TypeBuiltin:   builtinProber{},
      agent_backend_entity.TypeMyAgent:   myAgentProber{},
  }
  ```

- If a CLI-kind backend wants to test through a local gateway, extract the env wiring into `internal/pkg/agentruntime/clienv.go` (`BuildClaudeCodeEnv` / `BuildCodexEnv` / `BuildPiAgentEnv` are all here), **sharing the same wiring rules as the chat-path runtime** (the actual chat run and Test must not drift; see `BuildClaudeCodeEnv` / `BuildCodexEnv` / `BuildPiAgentEnv`). Backends like piagent that do not go through the Agentre gateway must also share the same env builder, to avoid the prober and the runtime drifting in their understanding of `env_json` / reserved keys.
- If it is the CLI kind, add a `Type` branch in `internal/pkg/cliprober/` (currently `cliprober.ResolveCLIPath` recognizes `claudecode` / `codex` / `piagent`; any other value returns `ErrInvalidType`), so the frontend editor can scan the local binary's absolute path. `agent_backend_svc/resolve_cli.go` is the entry adapter; it does not classify types itself and does not need changing.

### 2.4 Runtime (core)

Create a new package `internal/pkg/agentruntime/runtimes/<name>/`, with at least 3 files:

1. **`runtime.go`** — implements the `agentruntime.Runtime` interface:

   ```go
   var defaultRuntime = New()

   func init() {
       agentruntime.RegisterRuntime(agent_backend_entity.TypeMyAgent, defaultRuntime)
   }

   type Runtime struct { /* sessions map[int64]*active */ }

   func New() *Runtime { ... }

   func (r *Runtime) Capabilities() capability.Capabilities { ... }

   func (r *Runtime) Run(ctx context.Context, req agentruntime.RunRequest) (
       <-chan agentruntime.Event, *agentruntime.RunResult, error,
   ) { ... }
   ```

   - **`Capabilities()` must return a stable result** (the same across repeated calls on the same runtime); the frontend capability gating / remote prefetch both depend on this semantics.
   - The channel returned by `Run` **must be closed when the turn ends** (regardless of Done / Error / Abort). **`*RunResult` must not be read before the channel is closed** — `RunResult` is filled asynchronously.
   - When `RunRequest.Cwd` is non-empty, use it directly as cwd; when empty, fall back to `agentruntime.AgentCwd(req.AgentID)`. **Do not reverse-depend on `project_svc`**.
   - When launching a local CLI subprocess, call `procattr.ApplyNoConsoleWindow(cmd)` before `cmd.Start()`. This is required for Windows GUI builds; otherwise every agent turn can flash a transient console window.
   - When `ForkAnchor` is non-empty, implement "Regenerate" — see claudecode using `--fork-session` and codex using `thread/rollback`.
   - Call `unregister(sessionID)` to clear the sessions map before the main goroutine exits, to avoid a leak.

2. **`translator.go`** — translates the backend's own events into sealed `agentruntime.Event`:

   ```go
   func translate(ev myAgentEvent) (events []agentruntime.Event, usage *provider.Usage, stopErr error) { ... }
   ```

   - **Keep it a pure function** — one frame in, 0/1/n frames out, **does not read or write runtime state**. State aggregation (merging consecutive SteerConsumed arriving at the same safe point / pairing the pending steer FIFO) is done in the `runtime.Run` drain loop.
   - Tool recognition goes through `internal/pkg/agentruntime/canonical` — whatever can be recognized as `FileWrite/FileEdit/PlanUpdate/AgentSpawn` fills `ToolCall.Canonical`, so the UI projection / frontend cards reuse the same set; leave the unrecognizable as raw.
   - `EventUsage`: the translator internally computes `TotalInputTokens` per family (Anthropic = prompt+cached+cacheCreation; OpenAI = prompt), so downstream no longer makes family judgments.

3. **`runtime_test.go`** + **`translator_test.go`** — mandatory for TDD:
   - **Capabilities matrix test**: the declared cap=true must match the implemented sub-interfaces (Steerer / Aborter / etc.). See `runtime_test.go::TestXxxCapabilities`.
   - **Translator pure-fn test**: table-driven coverage of every event kind + boundaries (empty tool input / error frame / partial field missing). Use Convey-nested scenario descriptions.
   - **Run integration test**: use a fake backend client / fake session to verify event batch ordering, `SteerConsumed` merging, Abort unlocking, and the RunResult terminal state.

#### Optional control sub-interfaces

Implement according to the Capabilities declaration (**if you declared cap=true, you must implement the corresponding interface**; the matrix test will catch it):

| Cap | Interface | When to implement |
| --- | --- | --- |
| `CapSteer` | `Steerer` | supports mid-turn injection of a user message |
| `CapCancelSteer` | `SteerCanceler` | can still retract after injection (claudecode has it; codex does not) |
| `CapDrainSteer` | `SteerDrainer` | leftover between turns is auto-forwarded (claudecode only) |
| `CapAbort` | `Aborter` | the user's "Stop" button — basically must be implemented |
| `CapSetPermission` | `PermissionModeSetter` | switch permission mode at runtime |
| `CapAnswerUserAsk` | `AskAnswerSink` | handle reverse ask_user_question |
| `CapToolPermission` | `ToolPermissionSink` | handle the `can_use_tool` protocol |
| `CapForkSession` | `RunRequest.ForkAnchor` built-in semantics | "Regenerate" goes through fork |
| `CapReportContextWindow` | emit `ContextWindowUpdated` | the runtime can probe the model's actual window (codex has it natively in the protocol; claudecode falls back via `llmcatalog.Lookup(model)`; piagent prefers reading the Pi RPC `get_session_stats.contextUsage.contextWindow`, then falls back to looking up `llmcatalog` by the model from the usage frame) |
| `CapCompact` | `RunRequest.Compact=true` built-in semantics | native compact turn |
| `CapImageInput` | `RunRequest.UserBlocks` image blocks | supports multimodal user input; when unsupported, chat_svc rejects an image-carrying turn before calling the runtime |
| `CapGoal` | `GoalController` | exposes a session/thread-level objective the host can get / set / clear (codex thread goal; remote-forwarded) |
| `CapMCPTools` | `RunRequest.MCPServers` (no sub-interface) | the runtime consumes MCP tool servers injected at launch; orchestration (group chat) gates membership on this cap — see the matrix above |
| `CapSkills` | `RunRequest.EnabledPlugins` (no sub-interface) | the runtime injects per-agent skill-pack/plugin overrides at launch; sparse map, unlisted plugins inherit global CLI config — see the matrix above and §7 |
| `CapAutonomousTurn` | `AutonomousTurnSource` | the backend spontaneously produces a turn with no user input (claudecode background-task auto-continue) — **the only forward channel**; see item I below |

For caps you do not implement: **chat_svc returns `ErrUnsupported` when it receives the frontend request** — the error code has already been made a sentinel at the wire layer for transparent cross-process propagation, so **do not invent your own error**.

#### The control interfaces one by one (**must-read before onboarding**)

The following breaks down the protocol details, field semantics, and claudecode/codex/piagent differences of the reverse channels (A–H), plus the **one forward channel** `AutonomousTurnSource` (item I). When onboarding a new backend, **go through each item** against your implementation; don't guess by intuition.

##### A. Steerer / SteerCanceler / SteerDrainer — mid-turn injection

Interface signatures (`internal/pkg/agentruntime/runner.go:366-411`):

```go
type Steerer interface {
    Steer(ctx context.Context, sessionID int64, queuedID, text string) error
}
type SteerCanceler interface {
    CancelSteer(ctx context.Context, sessionID int64, queuedID string) ([]string, error)
}
type SteerDrainer interface {
    DrainPending(ctx context.Context, sessionID int64) []ConsumedSteer
}
```

- **queuedID** is generated by chat_svc at Enqueue time (UUID); it is the write-back handle for the subsequent CancelSteer.
- **Consumption receipt**: after the backend actually injects this text into the conversation, it **must** emit `agentruntime.SteerConsumed{Steers: [{QueuedID, Text}]}` via the translator — chat_svc uses it to advance the corresponding chat_message state to `consumed`. SteerConsumed arriving consecutively at the same safe point is **merged into a single batch in the `Run` drain loop** (see builtin/runtime.go `flushSteers`), preserving the single-frame emit wire behavior.
- **CancelSteer semantics**:
  - `queuedID == ""` → clears the entire pending queue and returns the list of cleared IDs (FIFO)
  - `queuedID` non-empty → single retract; returns `ErrSteerNotFound` if not in the queue (already consumed by the AI / never enqueued)
- **DrainPending side effect**: when returning a non-empty slice, it **must** atomically mark the session back to "still in turn", otherwise a Steer arriving in chat_svc between two Runs would land in ChatSendInFlight and be dropped. codex / piagent deliberately do not implement SteerDrainer — once their steer enters the protocol there is no local hook queue to drain.
- **claudecode implementation**: pushes to the CLI hook subprocess via `httpgateway.SteerInbox`.
- **codex implementation**: calls `*codex.Stream.Steer(text)`, maintaining a local pending queue for echo pairing.
- **piagent implementation**: calls `Steer(text)` on the Pi RPC stream; after Pi injects the steer and echoes it back as a user message, the runtime pairs it via a local pending FIFO and emits `SteerConsumed`.

##### B. Aborter — the "Stop" button

Interface signature (runner.go:426-428):

```go
type Aborter interface {
    Abort(ctx context.Context, sessionID int64) error
}
```

- **Idempotent + concurrency-safe**: it may be called at the same time as the runner's own drain goroutine.
- **Must unblock I/O**: claudecode writes a `control_request{interrupt}` frame to stdin; codex calls the `turn/interrupt` RPC; piagent calls the Pi RPC stream `Interrupt`; builtin cancels `turnCtx`. **The ctx cancel must unblock all blocking reads** — this is the prerequisite for "Stop" to take effect.
- **Return value**: when the sessionID has no in-flight turn, return `ErrNoActiveTurn`, which chat_svc translates into `code.ChatStopNoActive`.
- **RunResult.StopErr**: when the user actively aborts, the runner **should** fill `agentruntime.ErrAborted`, so that chat_svc can distinguish the three states "normal Done / user abort / real error".

##### C. ToolPermissionSink — `can_use_tool` tool approval

> **piagent currently does not support this** (no equivalent protocol). **codex does** — via the app-server `requestApproval` protocol (`runtimes/codex/runtime.go` `SubmitToolPermission`); it carries allow/deny + alwaysAllowSession but **drops DenyReason** (the deny-message param is ignored, since the codex protocol has no deny-feedback field). The following uses claudecode as the blueprint; copy the relevant parts when a new backend has an equivalent protocol.

Interface signature (runner.go:218-220):

```go
type ToolPermissionSink interface {
    SubmitToolPermission(ctx context.Context, sessionID int64, requestID string,
        allow, alwaysAllowSession bool, denyReason string) error
}
```

**The complete call chain**:

1. **Backend receives a can_use_tool control request** (all tools except AskUserQuestion go through here) → translator/runtime emits:

   ```go
   agentruntime.ToolPermissionRequest{
       RequestID:  "ctl-xxx",        // backend-private handle (claudecode = control_request.request_id)
       ToolCallID: "toolu_xxx",      // links to the tool_use in the assistant stream; may be empty in a race
       ToolName:   "Bash",           // tool name; the service uses it to recognize the ExitPlanMode special case
       Input:      rawInputJSONBytes,// the raw control_request.input bytes; the frontend JSON.parses it itself
   }
   ```

2. **chat_svc persists** (`internal/service/chat_svc/tool_permission.go:22-53`):
   - Converts it to a `blocks.ToolPermissionBlock` and adds it to the acc.
   - Projects it into a `ChatBlock{Type:"tool_permission_request"}` pushed to the frontend; canonical carries `ToolPermission{...}` or (when ToolName=="ExitPlanMode") `PlanApproveRequest{...}`.

3. **Frontend renders the approval card**: two buttons Allow / Deny; Allow can check "Remember for session" (alwaysAllowSession), Deny can fill in a deny reason.

4. **Frontend replies → service**: calls `AnswerToolPermission(sessionID, requestID, allow, alwaysAllowSession, denyReason, targetPermissionMode)`.

5. **service projects it back in reverse**:
   - `selectRunner()` type-asserts to `ToolPermissionSink` and calls `SubmitToolPermission(...)`.
   - claudecode implementation: writes a control_response frame to the subprocess stdin:
     - `allow=true` → `PermissionResult{Behavior:"allow", UpdatedInput: parsedInput}`; when `alwaysAllowSession=true`, it additionally attaches `UpdatedPermissions=[{type:"addRules", rules:[{toolName}], behavior:"allow", destination:"session"}]`, and the CLI maintains the subsequent allow rules itself.
     - `allow=false` → `PermissionResult{Behavior:"deny", Message: denyReason || "User denied..."}`; the CLI feeds the Message **back to the LLM as a tool_result**, so the AI gets the specific feedback and re-plans.

6. **After the runtime completes the write-back**, it emits the terminal frame:

   ```go
   agentruntime.ToolPermissionResolved{
       RequestID: "ctl-xxx",
       Allowed: true, AlwaysAllow: true, DenyReason: "",
   }
   ```

   chat_svc patches it back into that ToolPermissionBlock in the acc, ensuring correct on-disk persistence when the turn finalizes.

**Key points for onboarding a new backend**:
- The ToolName field must be filled — chat_svc uses it for special-case recognition (ExitPlanMode).
- DenyReason is not just a log — it must be feedable back to the LLM as a tool_result by the backend (otherwise the AI does not know why it was denied).
- Pass the Input raw bytes through; do not parse and re-marshal them (the frontend may rely on the original key order / numeric precision).

##### D. AskAnswerSink — reverse-asking the user a question

Interface signature (runner.go:255-257):

```go
type AskAnswerSink interface {
    SubmitAnswer(ctx context.Context, sessionID int64, requestID string,
        questions []AskQuestion, answers []AskAnswer, skipped bool) error
}
```

**The key difference from ToolPermission**: AskUserQuestion is **structured Q&A** (single-select / multi-select / Other / password field), not a binary allow/deny; the backend must aggregate the answer map according to its own protocol.

**The complete call chain**:

1. **Backend detects the AskUserQuestion tool/control request** → emits:

   ```go
   agentruntime.UserAskRequest{
       RequestID:        "ctl-xxx",
       ToolCallID:       "toolu_xxx",   // may be empty in a race; the frontend uses RequestID as a placeholder
       ParentToolCallID: "task-...",    // when called inside a subagent, points to the outer Agent.tool_use_id
       Questions: []AskQuestion{{
           ID: "q1", Question: "...", Header: "...",
           MultiSelect: true, IsOther: true, IsSecret: false,
           Options: []AskOption{{Label, Description}},
       }},
   }
   ```

2. **chat_svc persists `blocks.UserAskBlock`** + projects ChatBlock + the canonical UserAsk DTO (`internal/service/chat_svc/ask_user_question.go:21-83`).

3. **Frontend renders the UserAskCard**: single-select radio / multi-select checkbox / Other text field / IsSecret password field; provides two buttons Answer + Skip.

4. **Frontend replies → service** `AnswerUserQuestion(sessionID, requestID, answers, skipped)`, where answers is `[]AskAnswerDTO{QuestionIndex, Labels[], OtherText}`.

5. **service projects it back in reverse** `sink.SubmitAnswer(ctx, sessionID, requestID, nil, rtAnswers, skipped)`. **Leave the questions parameter nil** — the backend cached the questions list when it became the waiter, so passing nil lets the backend skip the length check.

6. **runtime writes back to the backend**:
   - **claudecode**: writes a control_response; `UpdatedInput.answers` aggregates the csv labels per question text (`OtherAnswerLabel` replaced with `OtherText`); `Behavior:"allow"`.
   - **codex**: responds to the app-server's `item/tool/requestUserInput` JSON-RPC, with payload = `map[codexQuestionID][]string`; assembled per `buildUserInputAnswers`.
   - **builtin Agent**: in-process channel.
   - **piagent**: currently does not declare `CapAnswerUserAsk`, so the frontend will not open this reverse channel.

7. **Skipped semantics**: must let the LLM gracefully see the refusal signal; **do not** allow an empty map (which silently hangs the turn, hapi gotcha #4):
   - claudecode: writes a deny message.
   - codex: `SubmitUserInput(requestID, map[string][]string{})` — an explicit empty map.

8. **After the runtime completes**, it emits `UserAskResolved{RequestID, Answers, Skipped}`, and chat_svc patches it back into the UserAskBlock.

**Key points for onboarding a new backend**:
- `OtherAnswerLabel` is a sentinel constant (`agentruntime.OtherAnswerLabel`); when you see this label, replace it with `AskAnswer.OtherText` — do not send it to the LLM as-is.
- AskQuestion.ID is backend-private (codex uses it as the key; claudecode uses the question text as the key) — the translator must preserve it and not drop it.
- waiter cache: when the runtime receives a UserAskRequest, it **must** cache (RequestID → questions), so that SubmitAnswer can look it back up when questions=nil.

##### E. PermissionModeSetter — switching permission mode at runtime

Interface signature (runner.go:441-443):

```go
type PermissionModeSetter interface {
    SetPermissionMode(ctx context.Context, sessionID int64, mode string) error
}
```

> This is a **session-level permission switch**, not plan content. Keep the two concepts separate:
> - **PermissionMode** = the runtime state machine for "does it require approval by default / is it in plan mode";
> - **Plan content** = the AI's current todo list (see §F).

**Valid mode values** (from `capability.PermissionModeMeta.AllowedModes`):
- claudecode: `{default, acceptEdits, plan, bypassPermissions}`
- codex: `{default, plan}` (**runtime switching prohibited**, `SwitchableDuringTurn: false`)

**Two different fields**:

| Field | Meaning | Who writes |
| --- | --- | --- |
| `chat_sessions.permission_mode` | the CLI runtime's current mode (changed by SetPermissionMode) | chat_svc's PermissionModeWriter |
| `chat_sessions.permission_mode_at_launch` | the `--permission-mode` snapshot delivered at spawn | the runtime returns it via `RunResult.LaunchPermissionMode`, and chat_svc writes it to DB |

Historical lesson: the runtime must not call `chat_repo.Session().Update...` directly — the agentred daemon does not bootstrap chat_repo, so it would nil-panic. **State is returned via RunResult.**

**DefaultMode vs LaunchDefaultMode** (`capability.PermissionModeMeta`):

| Field | Use | claudecode | codex |
| --- | --- | --- | --- |
| `DefaultMode` | the default mode name for UI display/computation | `"acceptEdits"` | `"default"` |
| `LaunchDefaultMode` | the fallback string the wire layer uses at spawn | `""` (does not attach the flag, letting pkg/claudecode fall back to acceptEdits) | `"default"` (the protocol requires an explicit collaborationMode at every launch) |

**Two trigger paths**:

1. **Active switch** (the frontend clicks the PermissionModePill):
   - frontend → `SetPermissionMode(sessionID, mode)`
   - service persists `chat_sessions.permission_mode` → attempts `runner.SetPermissionMode(ctx, sessionID, mode)`
   - the claudecode runtime writes a control_request to the CLI; codex returns `ErrUnsupported`, and starts the next spawn reading the new mode from DB.

2. **Passive switch** (the CLI reports it itself):
   - the runtime emits `agentruntime.PermissionModeChanged{Mode: "acceptEdits"}`
   - `handlers.PermissionModeChangedHandler` persists a `PermissionModeChangeBlock` + writes to DB via `PermissionModeWriter.SetMode` + emits a `StreamSessionStatus` patch.

**Frontend PermissionModePill behavior** (capability projection):

```ts
const meta = caps.PermissionModeMeta
const canSwitch = meta.SwitchableDuringTurn && agentStatus !== "waiting"
const order = meta.Order         // pill cycle order
const showBypass = session.permission_mode_at_launch === "bypassPermissions"
// bypassPermissions only appears in the pill if it was explicitly chosen at launch, to avoid being abused after the fact
```

##### F. ExitPlanMode — the special approval flow for plan → acceptEdits

ExitPlanMode is a plan-exit protocol implemented by **reusing the ToolPermission channel** (claudecode-specific; codex has no equivalent):

1. After the CLI finishes planning in plan mode, it calls the `ExitPlanMode` tool → the backend emits `ToolPermissionRequest{ToolName: "ExitPlanMode", Input: {plan: "..."}}`.
2. chat_svc detects `ToolName=="ExitPlanMode"` in `tool_permission.go:34-41`, and **additionally assembles** `Canonical = PlanApproveRequest{Plan, Actions}`, so the frontend renders with `PlanApproveCard` (instead of the generic ToolPermissionCard).
3. `Actions` is assembled by `handlers.BuildPlanApproveActions(launchPermissionMode)` in the ToolPermissionRequest handler (`internal/service/chat_svc/handlers/plan_approve.go:16-32`), with the rules:
   - normal launch (empty / default / acceptEdits / plan) → `[plan.approve.accept_edits, plan.approve.manual, plan.refine]`
   - launch="bypassPermissions" → the first item is **replaced** with `plan.approve.bypass_permissions` (not appended), yielding `[plan.approve.bypass_permissions, plan.approve.manual, plan.refine]`
   - `plan.refine` carries `RequiresFeedback: true` — the frontend expands a feedback textarea; after the user submits, it goes through `Allow=false` + `DenyReason=feedback` (the CLI feeds the message back to the AI as a tool_result to continue planning), **not** allow + switch back to plan mode
4. The frontend calls `AnswerToolPermission` according to the action the user clicked: approve kinds → `Allow=true, TargetPermissionMode=mapPlanApproveAction(actionID)` (see `plan_action.go:198-210`: bypass→`bypassPermissions` / accept_edits→`acceptEdits` / manual→`default`); refine → `Allow=false, DenyReason=feedback`.
5. The service first calls `SubmitToolPermission()` (after the CLI receives an approve, it automatically switches plan → default). When `Allow=true` and `TargetPermissionMode` is non-empty and not `"default"`, it **relays** a call to `SetPermissionMode()` to switch to the target — so `acceptEdits` / `bypassPermissions` relay, `Manual` (=`default`) does not relay, and `refine` does not relay either since `Allow=false` (`tool_permission.go:131`).

**Key points for onboarding a new backend**: for a backend that has plan-exit semantics, naming the ToolName `"ExitPlanMode"` lets it directly reuse the frontend PlanApproveCard — do not invent a new tool name.

##### G. Plan content update — the PlanUpdated event

Interface shape (**one-way emit, no reverse channel**):

```go
agentruntime.PlanUpdated{Plan: canonical.PlanUpdate{
    Text:    "...",             // the full Markdown plan (codex item/plan/delta passthrough)
    Steps:   []canonical.PlanStep{{Step, Status: pending|inProgress|completed|canceled}},
    Actions: []canonical.PlanAction{...},  // claudecode does not fill it (plan exit goes through the §F ExitPlanMode channel);
                                           // codex, when in plan mode + Text is non-empty, has the translator
                                           // attachPlanModeActions append [Execute, Refine]
}}
```

**Two wire shapes merged into one sealed event**:

- **claudecode has two paths** —
  - `TodoWrite` tool calls go through the translator → `ToolCall.Canonical = canonical.PlanUpdate` (**does not** emit a separate PlanUpdated event; the frontend just reads canonical off the ToolCall);
  - `TaskCreate` / `TaskUpdate` incremental calls have `claudecode/task_aggregator.go` maintain the full task list across turns, emitting a full `agentruntime.PlanUpdated` snapshot on each change.
- **codex**: two triggers — the `turn/plan/updated` notification sends `Steps[]`; `item/plan/delta + item/completed{type:"plan"}` streams `Text`. The translator funnels them into the same PlanUpdated event, so downstream no longer branches on the two states (`runtimes/codex/translator.go:57-80`). codex also, while in plan mode, attaches `[plan.execute, plan.refine]` two actions to the PlanUpdate via `attachPlanModeActions`, and the frontend PlanCard renders the buttons directly.
- **PlanText preserves the trailing newline**: after trimming, the frontend markdown renderer mistakes it for "no trailing newline" and breaks the formatting — use only `strings.TrimSpace` to judge "is it empty", and **do not** trim before emitting.

**chat_svc persists a PlanBlock** (`internal/service/chat_svc/plan_block.go:16-73`):
- projects it into a `ChatBlock{Type:"plan"}`
- the frontend PlanCard renders the full text; `TaskProgressBar` reads `steps[].status` for the progress bar
- multiple PlanUpdated within the same turn go through mutate (overwriting by PlanBlock key), without repeatedly landing new blocks

##### H. Subagent lifecycle — claudecode Task-tool-specific

Interface shape (**one-way emit, 3 events**):

```go
agentruntime.SubagentStarted{ToolCallID, Info: SubagentInfo{...}}
agentruntime.SubagentProgress{ToolCallID, Info: SubagentInfo{TotalTokens, LastToolName, ToolUses}}
agentruntime.SubagentDone{ToolCallID, Info: SubagentInfo{Status: "completed"|"failed", DurationMs, TotalTokens}}
```

- **ToolCallID** = the tool_use id of the outer parent `Task` / Agent tool.
- **Info.Status**: the runtime only produces `running` / `completed` / `failed`; `canceled` is inferred by `handlers.MarkRunningSubagentsCancelled` during turn-abort cleanup (after the CLI is interrupted, Done will not arrive, and leaving it running would make the frontend AgentSpawnCard spin forever).
- **The ParentToolCallID field of ToolCall / ToolResult**: tools called inside the subagent fill in the outer Task tool_use id, so the frontend groups the child cards under the parent SubagentInvocationCard.
- **codex / builtin / piagent currently do not emit these** — only claudecode has a native subagent protocol. Consider onboarding this set of events when a new backend has a similar fork-execute tool.

##### I. AutonomousTurnSource — the forward turn (backend→host)

This is the **only forward channel**: every other sub-interface is the host reaching into the backend, but here the backend tells the host "I just ran a whole turn on my own." Today only claudecode needs it.

**Why it exists**: when a turn ends with a `run_in_background` Bash task still running, the claude CLI emits `result` to close the turn but keeps the subprocess alive; when the task finishes it **autonomously** injects a `<task-notification>` and runs a *complete* second turn (init → text/tools → a second `result`) without any new stdin. The old per-turn reader stopped at the first `result`, so those autonomous frames sat unread and desynced every later turn ("can't continue the conversation"). See the design spec `docs/superpowers/specs/2026-06-04-claudecode-background-task-autonomous-turn-design.md`.

Interface signature (`internal/pkg/agentruntime/runner.go`):

```go
type AutonomousTurnSource interface {
    AutonomousTurns(sessionID int64) <-chan AutonomousTurn
}
type AutonomousTurn struct {
    Events  <-chan Event // same shape as Run's stream; closes after the turn's result
    Result  *RunResult   // readable after Events closes
    Trigger string       // "background_task"
}
```

Wiring, layer by layer:

- **`pkg/claudecode.Session`** runs one persistent `readLoop` that owns stdout for the subprocess lifetime, demuxing frames to a single active-turn slot. A turn that *opens* with a background-shaped `task_notification` (`isBackgroundTaskNotification`: has `output_file`, no `subagent_type`) is routed to a new autonomous sink exposed via `Session.AutonomousTurns()`; ordinary user turns are a FIFO of pending slots. This is what fixes the desync — it is independent of whether anything consumes the channel.
- **Runtime bridge** (`runtimes/claudecode/autoturn.go`): ranges `Session.AutonomousTurns()` and, per AutoTurn, reuses `drainStream` (same translator / control protocol / task aggregation) to produce `agentruntime.AutonomousTurn`. It deliberately does **not** call `active.setOut()` — `a.out` is written only by the user-turn `Run` goroutine (chat-lock-serialized), so the autonomous turn must not race it; the cost is that an async tool-permission/ask resolution *inside* an autonomous turn won't echo live (rare; reload fixes it).
- **chat_svc watcher** (`internal/service/chat_svc/autonomous_turn.go`): `runTurn` lazily type-asserts the runner to `AutonomousTurnSource` and starts one `startAutonomousWatcher` per session (deduped; exits when the channel closes on evict / `CloseSession`). `driveAutonomousTurn` persists each turn as a **pure assistant message (no user row)** and surfaces it live.
- **Concurrency constraint (do not violate)**: the watcher must **never hold the chat per-session lock while draining** `at.Events`. If it blocked on that lock, the Session reader would stall (evOut undrained → active slot never frees → the user's next turn blocks on `Session.turnMu` while holding the chat lock) → deadlock. Cross-turn serialization is provided naturally by the Session's single active slot; the rare overlap (user sends mid-autonomous-turn) is last-write-wins on the session row and reconciled by the frontend `StreamDone → reloadSession`.

**Frontend surfacing**: there is no per-turn stream name for a turn the user never initiated, so `driveAutonomousTurn` emits a session-level `chat:autonomous:<sessionID>` event (`StreamAutonomousStarted`, carrying the new assistant message + the per-turn stream name). `ChatPanel` keeps a standing subscription to that channel; on receipt it inserts the assistant row + `openStream`s, after which the turn streams exactly like a normal one. The transcript renders an `AutoTriggerBanner` before any assistant message whose immediately-preceding message is not a user message (the structural signature of an autonomous turn).

#### Sentinel errors (must use them, do not invent new ones)

```go
agentruntime.ErrNoActiveTurn   // the sessionID has no in-flight turn
agentruntime.ErrSteerNotFound  // the queuedID given to CancelSteer has been consumed / does not exist
agentruntime.ErrAborted        // the user actively aborted; written to RunResult.StopErr
agentruntime.ErrUnsupported    // the current runtime does not support this cap
```

When a new error must have cross-process semantics, **first add the sentinel + error code in `errors.go` + `wire.go` together**; do not temporarily stringify in the daemon handler.

### 2.5 Daemon import (remote execution)

To let a new backend run on the `agentred` daemon, change **only** `internal/daemon/runtime_imports.go`:

```go
import (
    _ "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/builtin"
    _ "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/claudecode"
    _ "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/codex"
    _ "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/piagent"
    _ "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/myagent"   // ← add this line
)
```

- Registration relies on init() side effects — so a runtime package's `init()` does **only** `RegisterRuntime`, and **does not start a goroutine / read environment variables / open files**, otherwise the daemon process triggers the side effect at startup.
- The daemon side does not bootstrap chat_repo — the runtime internally cannot reverse-depend on the repository package. Runtime state that needs to be persisted (e.g. `LaunchPermissionMode`) is returned to chat_svc via `RunResult` fields.
- `runtime_imports_test.go` enumerates `RegisteredRuntimes()` and runs a round of capability protocol tests — make sure this test still passes after the new runtime is added.

### 2.6 Service / Wails bindings

Only touch this when adding new fields:

- `internal/service/agent_backend_svc/types.go`: add fields to `BackendItem` / `CreateBackendRequest` / `UpdateBackendRequest` / `TestBackendRequest`. **Stable field names, explicit json tags** — `make generate` promotes them into the frontend TS types.
- `internal/service/agent_backend_svc/agent_backend.go`: read/write the new fields in buildEntity / mapItem.
- `internal/app/agent.go`: the binding-layer methods do **only** parse → svc → return; business logic stuffed into `App` will be missed by `go test`.

### 2.7 Frontend

- `make generate` regenerates the `frontend/wailsjs/` bindings.
- Editor UI (`frontend/src/components/agentre/agent-backends.tsx` + `agent-backends-utils.ts`): add the type option and new-field form controls — **use shadcn `@/components/ui/*` uniformly**, and do not add a native `<select>`.
- Capability gating: the frontend hooks `useBackendCapabilities` / `useSessionCapabilities` (`frontend/src/components/agentre/capability/`) call the Wails bindings `GetBackendCapabilities` / `GetSessionCapabilities` (`internal/app/chat.go` → `chat_svc/ipc/capability.go`), returning `Capabilities.Set` + `PermissionModeMeta`. The component reads `caps.has("steer")` / `caps.has("set_permission_mode")` etc. to gate the steer chip / abort button / permission mode pill / ask_user_question card. After adding a new cap to the capability enum, there is no need to change the hook — only change the consuming end.

---

## 3. Extra considerations for remote execution (`agentred`)

The desktop can dispatch a single chat to a LAN `agentred` to run:

```
desktop UI → internal/app → chat_svc → remote.Runtime
           → JSON-RPC over WebSocket (wire.MethodRun)
           → daemon/handlers/RuntimeHandlers
           → agentruntime.RuntimeFor(backendType) — runs your newly written *Runtime
```

To make this path work:

1. The Runtime does not depend on desktop-only services (chat_repo / GUI / system tray). State that needs to be returned goes through `RunResult` fields; do not call the repo directly.
2. New sentinel errors are added in sync to `wire.ErrCode*` — otherwise `errors.Is(err, agentruntime.ErrXxx)` will fail on the client.
3. New Event types are added in sync to the `wire.Event*` codec — the `runtime.event` notification is dispatched by the sealed Event tag.
4. Test the remote path: bring up a pair of in-memory `client.Client` ↔ `daemon.handlers.RuntimeHandlers` and verify the cross-process semantics of capability negotiation / Run / Abort / Steer.
5. **Forward channel (`AutonomousTurnSource`)**: if the daemon-side runtime implements it, the daemon starts a per-session fanout (`startAutonomousFanout`, deduped) that forwards each turn over three notifications — `runtime.autonomousTurn.started` (`AutonomousTurnStartedFrame`) → `runtime.autonomousTurn.event` (reuses `EventFrame`) → `runtime.autonomousTurn.done` (reuses `RunResultDoneFrame`). The client `remote.Runtime` reconstructs them into `agentruntime.AutonomousTurn`s on a session-keyed `autoSessions` map **independent of the per-Run `sessions` map** (autonomous turns arrive *after* `runResultDone`), and tears them down on connection close. `remote.Runtime` therefore always satisfies `AutonomousTurnSource`; for daemon backends that don't forward (codex/builtin), the channel simply stays idle.

---

## 4. The mandatory TDD / BDD test checklist

**In the Red phase, write, run, and see the test fail first, then implement.** Do not write implementation code without a failing test — this is the hard rule of [development.md](development.md) §0.

| Test | Location | What it verifies |
| --- | --- | --- |
| Entity Check table-driven | `*_test.go` alongside kinds.go | name / type / env_json reserved keys / model_routes alias / cli_path permissibility / kind-specific fields |
| Capabilities matrix | `runtimes/<name>/runtime_test.go` | the declared cap=true ↔ the corresponding interface implemented (type assert) |
| Translator pure function | `runtimes/<name>/translator_test.go` | every backend event kind + boundaries (empty input / partial fields / error frame) |
| Run integration | `runtimes/<name>/runtime_test.go` | event batch ordering, SteerConsumed merging, Abort unlocking, RunResult terminal state, ctx cancel behavior |
| ToolPermissionSink | `runtimes/<name>/control_test.go` | all three states Allow / Deny / alwaysAllowSession are written back to wire; DenyReason passed through to the LLM; the Resolved frame returns all fields |
| AskAnswerSink | `runtimes/<name>/control_test.go` | OtherAnswerLabel replaced with OtherText; Skipped goes through deny rather than an empty map; the waiter cache is looked up by RequestID |
| PermissionModeSetter | `runtimes/<name>/control_test.go` | the active-switch wire frame shape + the passive PermissionModeChanged emit; `LaunchPermissionMode` returned via RunResult |
| Plan/Subagent events | `runtimes/<name>/translator_test.go` | PlanText preserves the trailing newline + Steps merging; Subagent Started/Progress/Done all-three-states fields complete |
| Prober | `agent_backend_svc/prober_test.go` | provider missing / network error / normal tool loop all translate to an appropriate reply/err |
| Wire round-trip | `runtimes/remote/wire/wire_test.go` | the new Event / new sentinel codec is symmetric |
| Daemon registry | `daemon/runtime_imports_test.go` | the new backend appears in `RegisteredRuntimes()` |
| Service create/update/delete | `agent_backend_svc/agent_backend_test.go` | mockgen mock repo, verifying validation + persisted fields |

repo unit tests always use `testutils.Database(t)` + sqlmock, **never start a real SQLite** — see [development.md](development.md) §test stack.

---

## 5. Common pitfalls / anti-patterns (do not repeat them)

1. **Do not stuff business logic into `internal/app/`**. Wails bindings only do `parse → svc.Xxx().Method(ctx, ...) → return`, otherwise `go test` will not cover it.
2. **Do not start a goroutine / read env / open files in the runtime `init()`**. The daemon process triggers the side effect at bootstrap, and unit tests cannot control it. `init()` does only `RegisterRuntime`.
3. **Do not switch on backendType in chat_svc**. When a new backend appears, extract an interface and let the runtime declare its own capability — see the capability matrix. `if backend.Type == "claudecode" { ... }` is a smell.
4. **Do not normalize repeatedly**. env / model_routes are normalized once in entity.Check; the service / runtime must not do it a second time.
5. **Do not make the translator stateful**. State aggregation is done in the `Run` drain loop; the translator must be able to run independently in tests with table-driven assertions.
6. **Do not hard-code runtime resources like SteerInbox / SessionCache into `New()`**. Use package-level `SetXxx` injectors (see `claudecode.Runtime.SetSteerInbox`), wire them up at bootstrap, and swap in a fake in unit tests.
7. **Do not invent your own error to express `ErrNoActiveTurn` / `ErrUnsupported`**. These sentinels are transparent across processes; an invented string would leave chat_svc unable to translate it.
8. **Do not let the runtime reverse-depend on repository / chat_svc**. The daemon process did not bootstrap the repo — one call would nil-panic. State is returned via `RunResult`.
9. **Do not ignore ctx**. After the ctx received by `Run` is canceled, it must unblock all I/O — this is the prerequisite for chat_svc to implement the "Stop" button (claudecode via control_request, codex via turn/interrupt, builtin via canceling turnCtx).
10. **Do not do a drive-by refactor in the same commit that adds a capability**. The diff only touches the producer + its tests. When you see unrelated dirty data, flag it first — see CLAUDE.md / AGENTS.md §3 / [development.md](development.md) §Fix Discipline.

---

## 6. Pre-commit self-check checklist

- [ ] New `BackendType` constant + `BackendKind` implementation + registered in `backendKinds`
- [ ] The migration file for new fields is appended to the end of `migrationList()`, the DDL uses native SQL, and the default value lets existing rows pass Check
- [ ] The new runtime package's `init()` only calls `RegisterRuntime`, with no side effects
- [ ] The Capabilities declaration matches the actually implemented sub-interfaces (the matrix test passes)
- [ ] The reverse channels are implemented: `SubmitToolPermission` allow/deny/alwaysAllowSession + DenyReason passed through to the LLM; `SubmitAnswer` handles the OtherText sentinel + Skipped going through deny; `SetPermissionMode` writes the backend via wire; `PermissionModeChanged` returns to chat_svc via emit
- [ ] The runtime internally caches the AskUserQuestion waiter (looking up questions by RequestID), and SubmitAnswer does not error when it receives nil questions
- [ ] When reusing the ToolPermission channel for ExitPlanMode, the ToolName string is exactly `"ExitPlanMode"` (do not invent a new tool name)
- [ ] PlanUpdated's Text field preserves the trailing newline, only using TrimSpace to judge emptiness; Steps merge into the same PlanBlock without repeated landing
- [ ] The Translator is a pure function, with table-driven tests covering the happy path + at least one boundary/error
- [ ] `RunRequest.Cwd` is preferred when non-empty; `ForkAnchor` goes through fork when non-empty; `ctx` cancel can unblock
- [ ] `RunResult` is **not** read before the events channel is closed; new fields (ProviderSessionID / Usage / Model / LaunchPermissionMode / ContextWindow / UserAnchor) are filled according to backend capability
- [ ] Remote: the `runtime_imports.go` blank import has been added; the new Event / sentinel has the wire codec + round-trip test added
- [ ] The Wails type's new fields have stable json tags; `make generate` has regenerated `frontend/wailsjs/`
- [ ] The Prober has been registered in `proberRegistry`; the CLI-kind backend's env wiring is in `agentruntime/clienv.go` and shared with the chat path
- [ ] Key flows are logged: `logger.Ctx(ctx)`, message uses the lowercase `package.Method:` prefix, fields use `zap.Xxx` (see [development.md](development.md) §logging)
- [ ] `make check` (lint + test) all passes; the new package's service/repository layer coverage ≥80%

---

## 7. 技能包（Skill Pack / plugin）注入 —— `CapSkills`

> 已落地，`CapSkills` 已并入 §0.5 矩阵。代码：`internal/pkg/agentskill`（leaf 目录域）+ `internal/service/skill_svc`（组合服务）+ `chat_svc/turn_skills.go`（注入接缝）+ `runtimes/claudecode/skills.go`（`--settings` 渲染）+ `runtimes/codex/session.go`（`--config plugins.*.enabled` 渲染）。设计 / CLI 实测快照见 `superpowers/specs/2026-06-12-agent-skills-tools-design.md` 与 `superpowers/plans/2026-06-12-agent-skills-pr1-backend.md`（归档稿，不随代码更新）。
>
> 给 agent 按 **plugin / skill-pack** 粒度配技能，是与 `CapMCPTools` 同构的 launch-time 注入：per-agent 配置 → spawn 时 CLI 配置覆盖 → 每会话子进程独立。

### 7.1 Claude Code CLI 控制机制（已实测，claude 2.1.174 —— 这部分是 CLI 客观事实）

| 关注点 | 结论（命令 / 实验） |
| --- | --- |
| 发现已装包 | `claude plugin list --json` → `[{id:"name@marketplace", enabled, scope}]`；`--available --json` 加 marketplace 可装项；`claude plugin details <id>` 列包内 skill 名 + token 成本 |
| per-session 真相 | `--output-format stream-json` 的 `system.init` 帧带 `skills[]` / `plugins[]`（runtime 已解析此帧）。plugin skill 命名 `superpowers:brainstorming`，个人裸 skill `cago` |
| 开关控制 | `--settings '{"enabledPlugins":{"<id>":true/false}}'` 在 **launch 时**完整控制 plugin 及其 skills。实测：关 `superpowers@claude-plugins-official` → init 帧 `skills` 从 32 降到 18（其 14 个整包消失，且从 `plugins[]` 移除） |
| 粒度边界 | 只能按 **plugin** 开关；**单个 skill 不可**（`<name>@skills-dir:false` 无效）；个人 `~/.claude/skills/*` 裸 skill 不受 `enabledPlugins` 控制；`--disable-slash-commands` = 关全部 |
| 叠加语义 | `--settings` 的 `enabledPlugins` 叠加（additional）到全局：注入**全量**（每个已装 plugin → 含 false）→ 与全局完全隔离；注入**稀疏子集** → 未列出沿用全局。agentre 取后者（继承，见 §7.2） |

### 7.2 per-agent 独立怎么成立（共享安装也不冲突）

同后端的多 agent 共用一份 `~/.claude/` 安装与 `settings.json`，但 agentre **不改共享文件**，而是**每次 spawn 子进程时单独传 `--settings`**：

- 一 chat-session = 一 claude 子进程（LRU 按 `sessionKey(SessionID)`，`runtimes/claudecode/runtime.go`）；群成员各自 `BackingSessionID`（`group_svc/scheduler.go`）。
- 传 agent 的**显式覆盖** map（强制开=true / 强制关=false，**稀疏**）→ 未列出的 plugin 沿用全局 `~/.claude`（继承），agent 在全局基线上叠加自己的开关；同后端两 agent 可并发跑不同技能集。
- 与已上线 org 工具（per-agent `MCPServers`→`--mcp-config`，`session.go` `ccBuildClientOpts`）**同模式**。
- **caveat**：launch-time 生效，cache-hit 复用不重下发 → 改授权**下次 spawn** 生效（新会话即时；活跃缓存会话下次重启）。无 per-call gateway，纯 launch-time（对比 org 工具有 gateway 每次调用复检）。

Codex 同样走 launch-time 覆盖：`codex plugin list --json` 发现已安装 plugin，`RunRequest.EnabledPlugins` 渲染为 `--config plugins."<id>".enabled=<bool>`。带 plugin 覆盖的 turn 会绕过 persistent app-server cache，确保本轮启动配置生效；未覆盖时仍继承用户全局 `~/.codex/config.toml`。

### 7.3 接入的接口（已并入 §0.5 矩阵）

| 接缝 | 形态 |
| --- | --- |
| 能力 | `capability.CapSkills`（claudecode / codex 声明）；前端 `caps.has("skills")` 门控技能区 |
| RunRequest | `EnabledPlugins map[string]bool`（**稀疏**：仅 agent 显式覆盖；未列出沿用全局=继承）；仅 `CapSkills` runtime 消费，其它忽略（软降级，同 `MCPServers`/`CapMCPTools`） |
| claudecode | 纯函数 `buildSkillsSettings(map, base) string`（`runtimes/claudecode/skills.go`）把 `{"enabledPlugins":…}` 合进 `--settings`；`acquireSession`（`runtime.go`）spawn 时调用 |
| codex | 纯函数 `buildPluginConfig(map) []string`（`runtimes/codex/session.go`）把覆盖渲为 `plugins."<id>".enabled=<bool>`；带覆盖时绕过 session cache |
| 目录域 | leaf `internal/pkg/agentskill`：`SkillPack` 类型 + `Recommended()` 静态精选 + `Discoverer` 按 backend 注册表（claudecode / codex 实现 = 解析各自 `plugin list --json`，blank import 注册，仿 runtime/prober） |
| 服务 | `skill_svc`（消费者侧窄接口 `AgentLookup`/`BackendLookup` 注入）：`ListAgentSkillPacks`（推荐+发现合并去重，Wails 绑定 `App.ListAgentSkillPacks`）/ `EnabledPluginsMap`（注入用，回 agent 显式覆盖）；保存复用 `agent_svc.Update`（`agents.skills_json` 存 `{id,enabled}`，id=plugin id；迁移 `202606120001_agent_skills_reset` 把旧 `{label}` 清成 `{id}`） |
| 注入点 | `chat_svc` `runTurn` 组 RunRequest 时按 `CapSkills` 填 `EnabledPlugins`（`turn_skills.go` 接缝，仿 `turn_mcp.go`） |
| 远端 | `EnabledPlugins` 已随 remote wire 透传到 daemon runtime；目录发现仍在 `ListAgentSkillPacks` 所在进程执行，远端 CLI 不可在桌面访问时会软降级为空发现 |
