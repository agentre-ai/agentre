# End-to-end testing harness (Playwright + fake agent runtime)

**Date:** 2026-06-09
**Status:** Approved (design)
**Scope:** `agentre` desktop app (Go backend + React frontend)

## Problem

The app has solid unit/component coverage (Vitest + happy-dom) and Go unit tests
(sqlmock + mockgen), but **no end-to-end coverage**: nothing exercises the real React
frontend talking to the real Go services over Wails IPC. The frontend tests alias every
Wails binding to hand-written `vi.fn()` stubs (`frontend/vite.config.ts`,
`frontend/src/__tests__/mocks/`), so whole classes of regressions — IPC wiring, dispatcher
behavior, session lifecycle, "stuck running" status writes — are invisible to the suite.

The one thing a naive e2e run cannot do is run the **real** agent backend: a real turn
spawns claude-code / codex subprocesses, which is slow, nondeterministic, costs tokens, and
needs external auth. We want true e2e for everything *except* the agent execution, which we
replace with a deterministic fake.

We considered upgrading to Wails v3 first. Rejected: v3's e2e story is the **same**
mechanism (Playwright against `wails dev` at `:34115`), v3 is still alpha (no beta/stable
timeline as of 2026-06), and migrating a complex app to alpha software buys zero e2e gain.
v2.12.0 already supports this fully.

## Goal

- A single command (`make e2e`) runs a real frontend + real Go services e2e smoke test
  locally on macOS, deterministically and repeatably.
- Only the agent runtime is faked; all other backend code paths (services, dispatcher,
  handlers, DB, IPC) run for real.
- The fake agent code is **never compiled into production binaries**.
- Each run uses an isolated, throwaway data root — no contention with the user's real DB or
  the `make dev` (`agentre-dev`) root.
- Designed to lift into CI later (Linux + xvfb + webkit deps) without redesign; **not**
  wired into CI this round.
- First milestone is **one core smoke chain** (new session → send message → see streamed
  reply → tab returns to idle), proving the harness end-to-end. Broader coverage follows.

## Non-goals (this round)

- CI integration (the harness is built CI-ready; wiring GitHub Actions is a later task).
- Group-chat / settings / multi-backend coverage — claude-code backend only to start.
- Driving the native webview directly (Playwright uses its own chromium against `:34115`;
  the native window that `wails dev` opens is incidental and ignored).
- Real agent (claude-code/codex) e2e, real SaaS login — out of scope by design.

## Architecture

Playwright (its own chromium) → `http://localhost:34115` (the `wails dev` server, which
exposes the real frontend **with backend bindings**) → real Go services → **fake
`agentruntime.Runtime`** instead of claude-code/codex.

```
Playwright chromium ──HTTP/Wails──▶ wails dev (:34115)
                                       │  real React + real Go services
                                       │  real dispatcher / handlers / DB (temp dir)
                                       ▼
                               agentruntime.RuntimeFor(claudecode)
                                       └─▶ fake runtime (build tag `e2e`)
                                            emits deterministic events
```

### 1. Harness — Playwright launches the tagged dev backend

- Add Playwright under `frontend/` (`@playwright/test` + chromium browser). pnpm is the
  source of truth.
- `frontend/playwright.config.ts` declares a `webServer` that launches the backend built
  with the `e2e` build tag (`wails dev -tags e2e`) from the repo root, injecting env:
  - `AGENTRE_DATA_DIR=<fresh temp dir>` — isolated DB/logs (highest precedence in
    `internal/pkg/paths/paths.go`, overrides the dev `-dev` suffix).
  - `AGENTRE_ENV=test` — quiet logger level (`internal/bootstrap/cago.go`).
  - `url: http://localhost:34115`, `reuseExistingServer: true` locally.
- The exact webServer invocation (cwd plumbing / a thin `make e2e-serve` target vs inline
  command) is finalized in the implementation plan; intent: one process, tagged build,
  e2e env, serving `:34115`.

### 2. Fake agent runtime — behind build tag `e2e`

- New package `internal/pkg/agentruntime/runtimes/fake/` (entire package `//go:build e2e`)
  implementing `agentruntime.Runtime` (`internal/pkg/agentruntime/registry.go`):
  - `Capabilities()` → a permissive capability set sufficient for the chat UI.
  - `Run(ctx, req)` → returns an event channel that emits a **deterministic** sequence
    derived from the request's user prompt: `TextDelta` chunks + `Done`, plus a populated
    `*RunResult`. Default behavior = echo the prompt back.
  - Emits the sealed `agentruntime.Event` types (`internal/pkg/agentruntime/event.go`); no
    business logic is bypassed downstream.
- **Extension point (placeholder, unused by first smoke):** a tiny prompt-prefix directive
  so future specs can request specific shapes — e.g. `@e2e:tool` → `ToolCall`+`ToolResult`,
  `@e2e:error` → `ErrorEvent`. Documented but not exercised yet.

### 3. Injection seam — OCP, no scattered `if env==e2e`

- The real runtimes auto-register in their package `init()` (e.g.
  `runtimes/claudecode/runtime.go`). The fake overrides via the existing
  `agentruntime.RegisterRuntime(type, impl)` registry — called from an explicit installer
  invoked early in `main()`, so it runs after all package inits and wins the registry slot.
- Two-file seam next to `main.go`:
  - `e2e_install.go` (`//go:build e2e`) → `installE2EFakes()` registers the fake for
    `agent_backend_entity.TypeClaudeCode` and seeds minimal state (see §4).
  - `e2e_install_noop.go` (`//go:build !e2e`) → `installE2EFakes()` is a no-op.
- Production builds (`make build`, default tags) compile only the no-op; the fake package
  and its imports are absent from the binary.

### 4. Data isolation & seeding

- `AGENTRE_DATA_DIR` → fresh temp dir per run; migrations build an empty DB on boot.
  Playwright global teardown removes the dir.
- The e2e installer (build-tag gated) seeds the **minimal viable state** for the smoke
  chain using existing service accessors (not raw SQL — respect the layering rule): at
  minimum one **local claude-code agent backend** so the UI can create a session and send.
  The exact minimal entity set (backend / agent / workspace cwd) is verified against the
  code when writing the plan.
- No external network: fake runtime spawns no subprocess; core chat needs no SaaS login
  (confirmed).

### 5. First smoke spec — `frontend/e2e/smoke-chat.spec.ts`

Vertical slice:

1. Open the app at `:34115`; main chat view loads.
2. Create / start a new chat session against the seeded backend.
3. Type a known prompt (e.g. `ping`) and send.
4. Assert the transcript shows the fake's deterministic streamed reply (streaming render
   completes).
5. Assert the session tab returns to **idle** and does not stay stuck on `running`
   (directly guards the known "stuck running / lost status write" failure mode).

Selectors prefer ARIA roles / `data-testid`. If a key node lacks a stable selector, add
**minimal** `data-testid` hooks to the relevant React components — in-scope for this task,
no broader churn.

## Files

New:

- `frontend/playwright.config.ts`
- `frontend/e2e/smoke-chat.spec.ts`, `frontend/e2e/global-setup.ts`,
  `frontend/e2e/global-teardown.ts`, `frontend/e2e/helpers/*`
- `internal/pkg/agentruntime/runtimes/fake/` (runtime + Go unit test)
- `e2e_install.go` (`//go:build e2e`), `e2e_install_noop.go` (`//go:build !e2e`)

Changed:

- `Makefile` — add `make e2e` (and possibly `make e2e-serve`); **not** added to `make test`.
- `frontend/package.json` — `@playwright/test` devDep + e2e scripts.
- `main.go` — call `installE2EFakes()` early in startup.
- `.gitignore` — `frontend/playwright-report`, `frontend/test-results`, e2e temp data dir.
- Minimal `data-testid` additions to chat components only if needed for stable selectors.

## Tests (TDD — written first, watched to fail)

- **Fake runtime (Go, build tag `e2e`):** a unit test asserts `Run()` emits the expected
  deterministic event sequence (`TextDelta…` + `Done` + `RunResult`) for a given prompt —
  red → green before wiring it into the registry.
- **Smoke spec (Playwright):** `smoke-chat.spec.ts` is the e2e contract; it fails first
  (harness/seam/fake absent) and drives the build to green.
- Existing `make test` (Go race + Vitest) must stay green and fast; e2e runs separately via
  `make e2e`.

## Out of scope

- Wails v3 upgrade (explicitly rejected above).
- Group-chat, settings, issue-tracker, terminal e2e coverage — later specs reuse this
  harness + the fake-runtime extension point.
- CI pipeline, headless Linux/xvfb wiring.
- Faking codex / builtin / remote runtimes (the seam supports them; not needed yet).
