# Codex Backend Behavior Eval

This guide defines the deterministic acceptance suite for Agentre's Codex
app-server adapter. It is intended for CI and independent review (including a
later Pi judge), not as a substitute for protocol inspection. The fixture
speaks JSON-RPC over the same stdin/stdout boundary as `codex app-server` and
does not call a model or consume model tokens.

The primary protocol reference for this suite is the locally inspected
`codex-cli 0.145.0` schema. Additive fields and unknown notifications remain
forward compatible. A malformed JSON-RPC frame, an unknown server request, or
a contradictory terminal state must remain diagnostic rather than being
reported as success.

## Deterministic command

Run the machine-readable eval from the repository root:

```bash
go test -json -race ./pkg/codex -run '^TestCodexBehaviorEval$' -count=1
```

Every invariant has a stable test path of the form
`TestCodexBehaviorEval/<scenario>/<invariant>`. A failed JSON record therefore
identifies the behavior that regressed without parsing free-form logs.

Run the adapter, runtime projection, and chat state projection together with:

```bash
go test -race ./pkg/codex ./internal/pkg/agentruntime/runtimes/codex ./internal/service/chat_svc/handlers ./internal/service/chat_svc/turn -count=1
```

## Scenario rubric

| Scenario | Required invariant |
| --- | --- |
| Normal turn | Streamed text is preserved; exactly one done event enters `completed`. |
| Streaming text and tool | Text/tool ordering is stable; each tool ID has at most one start and one terminal result. |
| Approval allow | Request ID is correlated and the command response is `accept`. |
| Approval deny | Request ID is correlated and the command response is `decline`; denial is not reported as success. |
| User input | String or numeric JSON-RPC IDs correlate to the exact question-ID answer map. |
| Plan and `update_plan` | `item/plan/delta` emits cumulative text snapshots; structured plan updates remain available as steps and one tool projection. |
| Usage and context | Last-call usage and `modelContextWindow` reach the terminal projection. |
| Steer | `turn/steer` targets the current thread with `expectedTurnId`; a terminal turn rejects late steer. |
| Interrupt | `turn/interrupt` sends only `threadId` and `turnId`; the matching interrupted completion enters `canceled`. |
| EOF/crash | Exit before a terminal notification enters `failed`, emits a diagnostic error, and makes the process non-reusable. |
| Duplicate/out-of-order/unknown | Wrong-turn traffic is ignored, duplicate tool terminals are idempotent, and additive unknown notifications do not stop known progress. |
| Cross-turn isolation | A recently terminal turn cannot adopt identity or write events into the next Stream on a persistent process. |

The focused hardening specs in `pkg/codex/hardening_test.go` additionally cover
malformed JSON, JSON-RPC Method Not Found responses, server-side request
resolution, failed turns without optional error details, bounded/redacted
diagnostics, response-write failure retention, explicit/concurrent/context-driven
interrupt convergence (including missing RPC ACK or terminal notification), and
late approval traffic.

## State-machine acceptance rules

- A turn starts in `starting`, proceeds through `running`, may enter `waiting`
  or `interrupting`, and ends in exactly one of `completed`, `failed`, or
  `canceled`.
- Repeating the same state or terminal is idempotent. A backward transition,
  conflicting terminal, or progress after terminal cannot rewrite state.
- `waiting` is derived from the set of unresolved approval/input requests. It
  clears only when the last request resolves or the turn becomes terminal.
- `interrupting` is not terminal. Reuse is allowed only after a matching
  terminal notification; an interrupt grace timeout terminates and evicts the
  process.
- Terminal entry clears request waiters and quarantines the turn ID. A late
  response, request, item, or `turn/started` frame cannot affect the next turn.
- A session owns the app-server process and thread. A Stream owns one turn. The
  runtime active map is only the current Agentre-session control route and may
  be released only by its owning turn.

## Independent judge guidance

Treat every scenario row as pass/fail. The critical rows are interrupt,
EOF/crash, duplicate/out-of-order/unknown, and cross-turn isolation: any
failure there is a hard rejection because it can produce stuck-running,
double-terminal, or cross-turn corruption. Do not award credit solely because
the process exits zero; inspect the individual JSON subtest records and the
state-machine assertions.

The real CLI integration tests are optional and **consume real model tokens**.
Run them only with explicit authorization and a configured local Codex account:

```bash
CODEX_REAL_CLI=1 go test -tags codexcli ./pkg/codex -run '^TestRealCodexCLI' -count=1
```

`thread/rollback` is still present in 0.145.0 but is marked deprecated in the
generated schema. The current fork/rewind capability is truthful for 0.145.0;
future Codex upgrades must re-audit that capability before changing or
replacing it.
