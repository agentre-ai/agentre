# E2E Harness Guide (Playwright × the real Wails app)

> How to drive the **real running Agentre app** end-to-end with Playwright — both the committed
> core-flow suite and **ad-hoc functional verification of a feature you just finished**. Written
> for agents (Claude / Codex) and developers.
>
> This doc **owns** the GUI-e2e harness. For SQLite / log / table-to-feature debugging see
> [debugging.md](./debugging.md). The design rationale is archived in
> [`superpowers/specs/2026-06-10-e2e-harness-hardening-design.md`](./superpowers/specs/2026-06-10-e2e-harness-hardening-design.md)
> (current) and [`…2026-06-09-e2e-testing-design.md`](./superpowers/specs/2026-06-09-e2e-testing-design.md)
> (the prior snapshot) — those are archived snapshots, not living docs; **this file** is the
> living reference.

Agentre is an IPC-only Wails desktop app — there is no HTTP API to hit. But `wails dev` exposes
the app over a browser-accessible IPC bridge, so Playwright (Chromium) can open it like a normal
page and drive the **real React frontend → real Wails IPC → real Go service/repository → real
SQLite**. The one thing it does **not** run for real is the agent backend: a real turn spawns
claude-code / codex subprocesses (slow, nondeterministic, needs external auth), so e2e replaces
**only** the `agentruntime.Runtime` with a deterministic fake. Every other backend path
(services, dispatcher, handlers, DB, IPC, migrations) runs for real.

## 1. Two modes — pick the right one

| | **Committed core-flow suite** | **Ad-hoc functional verification** |
|---|---|---|
| Lives in | `e2e/tests/*.spec.ts` (committed) | `e2e/scratch/*.spec.ts` (**gitignored**) |
| Run with | `make e2e` | `make e2e-scratch` |
| Lifetime | permanent regression guard | throwaway — write, run, observe, delete |
| What goes here | **only core / critical flows** | "I just built X — does it work in the real app?" |

**The bar for a committed spec is high.** A committed GUI e2e spec is slow (builds + runs the
real app, ~30 s per run even with the fake) and a maintenance liability. Only add one for a
**core flow** (app boots, new session → send → streamed reply → idle). Everything else gets
**verified ad-hoc** (mode 2) and thrown away. When in doubt, verify ad-hoc; promote to a
committed spec only once the flow is clearly core and stable.

## 2. Architecture

```
make e2e  →  cd e2e && pnpm test  →  node run-e2e.mjs   (spawns playwright, reaps residue after)
  └─ playwright (workers:1, testDir ./tests)
       ├─ webServer:  wails dev -tags e2e -devserver localhost:34216 > "$LOG" 2>&1
       │                 ├─ vite (frontend HMR)
       │                 └─ agentre app (-tags e2e) → real services → <tmp>/agentre-e2e-data/agentre.db
       │                       └─ agentruntime.RuntimeFor(claudecode) overridden by the FAKE (echo)
       └─ chromium → http://localhost:34216   (Wails IPC websocket bridge → real Go backend)
                         └─ specs assert on the UI …
                              … and on the DB via a direct read-only node:sqlite query (oracle)
```

Playwright drives its **own** chromium against `:34216`. The native webview window that
`wails dev` opens is incidental and ignored. The app is launched with these env overrides
(injected by `e2e/playwright.config.ts`):

| Env | Effect |
|---|---|
| `AGENTRE_DATA_DIR=<tmp>/agentre-e2e-data` | DB / config / logs under a throwaway dir — the highest-precedence data-root override (`internal/pkg/paths/paths.go`), so it never collides with your real DB or the `make dev` root |
| `AGENTRE_ENV=test` | quiet logger level (`internal/bootstrap/cago.go` `appEnv()`) |
| `AGENTRE_PROXY_PORT=0` | bind the local HTTP gateway to an OS-chosen **free** port instead of the fixed default 52401 (`internal/bootstrap/cago.go` `loadProxyAddr` → `proxyPortFromEnv`). The fixed port is **not** data-dir-scoped, so a running real Agentre already holds 52401; without this override the e2e gateway fails to bind → `BaseURL()` is empty → every gateway round-trip (`group_send`, hooks, LLM forward) silently dies. `BaseURL()` reports the real bound port via the listener, so nothing hardcodes 52401 |

The bridge runs on a **dedicated port 34216** (not Wails' default 34115) so it never reuses — or
collides with — a `make dev` dev server you already have open.

### The build-tag seam (why the fake never ships)

Real runtimes auto-register in their package `init()` (e.g. `runtimes/claudecode`). The fake
**overrides** that slot through the existing registry — no scattered `if env == e2e`:

1. Every `internal/pkg/agentruntime/runtimes/fake/*.go` carries `//go:build e2e`, so the package
   and its imports are absent from any default build.
2. `main()` calls `installE2EFakes(ctx)` (`main.go`) after bootstrap, before `wails.Run`. In an
   `e2e` build that resolves to `e2e_install.go`, which calls
   `agentruntime.RegisterRuntime(TypeClaudeCode, fakert.New())` **after** all package `init()`s,
   so the fake wins the slot, and seeds a local backend attached to the system CEO agent.
3. In a default build it resolves to `e2e_install_noop.go` — a no-op. `make build` / `make run`
   compile neither the fake nor its registration; the production binary is identical to one
   written without e2e at all.

The fake (`internal/pkg/agentruntime/runtimes/fake/runtime.go`) echoes the prompt back as
`ReplyPrefix + req.UserText` (`ReplyPrefix = "e2e-fake-reply: "`) in 8-rune `TextDelta` chunks,
then `Done`. Same prompt → byte-identical stream, every run. It has its own `//go:build e2e`
unit test asserting the emitted sequence (red→green before it's wired into the registry).

## 3. Isolation & safety guarantees

A run is fully hermetic, and in particular **a running Agentre does not interfere**:

- **Data** — DB / config / logs live under `<tmp>/agentre-e2e-data` (`agentre.db`), removed by
  `run-e2e.mjs` after a **passing** run (kept on failure for debugging — see §7). Your real
  `~/Library/Application Support/Agentre` is never touched.
- **Single-instance lock** — set only when `!isWailsDevMode()` (`main.go`); e2e runs via
  `wails dev` (sets the `devserver` env → dev mode), so the lock is **already skipped**, and its
  id is data-dir-scoped (`singleInstanceUniqueID(dataDir)`) regardless. So an e2e run launches
  even with a real Agentre open. **No backend Go change was needed** for hermeticity — contrast
  the opskat harness, which had to add an explicit `OPSKAT_E2E` lock-skip + `ResolvedDataDir`.
- **Bridge port** — 34216, dedicated; never collides with a `make dev` on 34115.
- **Gateway port** — the local HTTP gateway's default 52401 is **not** data-dir-scoped, so a
  running real Agentre holds it; e2e sets `AGENTRE_PROXY_PORT=0` to bind a free port instead (see
  §2's env table). Without this the gateway degrades and every gateway round-trip (`group_send`,
  hooks) silently fails — `group-chat.spec.ts` would go red against a perfectly-good backend.

Run one e2e invocation at a time locally (the temp data-dir path is fixed). CI runners are
isolated, so each job's run is independent.

## 4. Running the committed suite

```bash
cd e2e && pnpm run setup   # one-time: install deps + Chromium (skip if already done / on CI)
make e2e                   # or, equivalently: cd e2e && pnpm test
```

Prereqs: `wails` CLI on PATH, `pnpm`, Node with the built-in `node:sqlite` (Node ≥ 22; the repo
runs Node 24+/26). `pnpm run setup` installs the e2e deps and Chromium **once**; `make e2e` only
runs the suite — no per-run install. The first run builds Go + Vite (~30 s) and **opens a native
Agentre window** — expected; the test drives the `:34216` browser instance, not that window. The
window closes when the suite ends.

**Platforms.** Runs on macOS, Linux, and (best-effort) native Windows. `make e2e` is a thin alias
for `cd e2e && pnpm test`, so on Windows (no `make`) run `cd e2e && pnpm test` directly. *All*
orchestration and cleanup live in `e2e/run-e2e.mjs` (cross-platform Node) — there are no
shell-only `pkill` / `mkdir -p` / `touch` steps. CI exercises the Linux path; the Windows reap
branch (PowerShell CIM) is by-inspection only.

**Debug loop.** The config sets `reuseExistingServer: !process.env.CI`, so locally you can leave a
server up and re-run specs against it without rebuilding each time:

```bash
# terminal 1 — start the harness server once (env matches the config), leave it running:
cd e2e && AGENTRE_DATA_DIR="${TMPDIR:-/tmp}/agentre-e2e-data" AGENTRE_ENV=test \
  wails dev -tags e2e -devserver localhost:34216    # run from repo root; Ctrl-C to stop
# terminal 2 — re-run specs against the reused server (add --debug to step, --headed to watch):
cd e2e && pnpm exec playwright test            # or: pnpm exec playwright test --debug
```

The HTML report lands at `e2e/playwright-report/` (gitignored); traces are
`retain-on-failure`, screenshots `only-on-failure`. webServer output → `$TMPDIR/agentre-e2e-webserver.log`.
e2e is **not** part of `make test` / `make check` — it runs only on demand.

**In CI:** the committed suite runs on every PR / push to `main` / `develop/*` as the `E2E` job
(`.github/workflows/ci.yml`, `ubuntu-22.04`). It installs xvfb + GTK/WebKit + the wails CLI,
installs the `e2e/` package deps + Chromium, then runs `xvfb-run -a make e2e`; on failure it
uploads `e2e/playwright-report`, `e2e/test-results`, and the webServer log as artifacts. The
ad-hoc scratch mode is local-only.

## 5. Writing a committed core-flow spec

Only when the flow is genuinely core (§1). Principles, learned from the smoke spec:

- **Selectors: ARIA role or `data-testid`, not visible text.** Text is i18n'd and brittle. The
  smoke chain keys off `new-chat-button`, `new-agent-chat-item`, `agent-picker-item-<id>`,
  `[role="tab"][data-active="true"]`, `.ProseMirror` (the composer),
  `getByRole("main") … button[type="submit"]`, and `tab-spinner`.
- **Need a stable hook that doesn't exist?** Add a **minimal** `data-testid` to the component —
  in-scope for the spec's task. No broader churn, no renaming sweep.
- **Assert on the deterministic fake output.** Match `/e2e-fake-reply: <prompt>/` to confirm the
  round-trip, not some incidental string.
- **Corroborate side effects against the DB.** Asserting the UI updated is necessary but not
  sufficient — confirm the real write with the oracle in `e2e/fixtures/db.ts` (a read-only
  `node:sqlite` query against the temp `agentre.db`). It's independent of the app's own service
  layer, so it catches "UI says OK but the DB never got written". The smoke spec asserts
  `runningSessionCount()` polls to `0` after the turn — a direct regression guard for the
  "stuck running / lost status write" bug at the source of truth, not just the UI spinner. Add
  more read-only helpers there as needed (`PRAGMA busy_timeout`).
- **Keep it deterministic and single-worker.** The config runs `workers: 1`,
  `fullyParallel: false` against one shared backend + one shared DB; don't write specs that
  assume isolation between them or rely on wall-clock timing.

## 6. Ad-hoc functional verification — the workflow after finishing a feature

The default way to answer **"I just built X — does it work end-to-end in the real app?"** without
committing a test. Drive the real app, then read observable side-effects (UI, DB, logs).

1. **Write a throwaway spec** under `e2e/scratch/` (gitignored). Same conventions as §5 —
   `data-testid` locators, auto-wait, the DB oracle. If the feature needs a UI hook that doesn't
   exist yet, add a `data-testid` (additive); if it surfaces a real bug, fix the producer per the
   [Fix Discipline](./development.md).
2. **Run it against the real app:**
   ```bash
   make e2e-scratch        # runs every e2e/scratch/*.spec.ts via the live harness
   # or a single file (still through the runner, so cleanup happens):
   cd e2e && pnpm run test:scratch scratch/<file>.spec.ts
   ```
   `playwright.scratch.config.ts` reuses the exact webServer / env / isolation / teardown as the
   committed suite — only `testDir` points at `./scratch`.
3. **Observe.** Read the spec's assertions, then corroborate: the temp `agentre.db` (query with a
   `fixtures/db.ts` helper, or open it read-only at `$AGENTRE_DATA_DIR/agentre.db`); the app's
   structured log under `<tmp>/agentre-e2e-data/`; the webServer's stdout at
   `$TMPDIR/agentre-e2e-webserver.log`; on failure, Playwright's trace/screenshot under
   `e2e/test-results/`. (Log/DB reading: see [debugging.md](./debugging.md).)
4. **Discard.** The scratch file is gitignored — delete it when done. If the flow turns out to be
   core and worth guarding forever, *promote* it: move it into `e2e/tests/`, harden, commit (§5).

See [`e2e/scratch/README.md`](../e2e/scratch/README.md) for a copy-paste starter.

## 7. Harness engineering — hard-won lessons (symptom → root cause → fix)

These bit the harness (here and in the sibling opskat harness this design is based on); keep them
in mind when changing it.

- **Suite hangs forever after tests pass.** *Cause:* `wails dev` orphans its `vite` child, which
  keeps the **piped** webServer stdout's write end open, so Playwright's teardown never finishes.
  *Fix:* `stdout/stderr: "ignore"` + redirect the command's own output to a file
  (`wails dev … > "$LOG" 2>&1`); readiness is detected via `url` polling, not stdout. (This is the
  root cause an earlier agentre harness dodged by dropping `webServer` entirely; with it fixed,
  `webServer` is the simplest correct option.)
- **All green but `exit 143` / `make: *** Terminated`.** *Cause:* reaping inside `globalTeardown`
  SIGTERMs Playwright's *still-managed* webServer; reaping via a Makefile `pkill -f "wails dev …"`
  self-matches the recipe shell's own `/proc/<pid>/cmdline` on Linux and SIGTERMs `make`. *Fix:*
  do **all** post-run cleanup in `e2e/run-e2e.mjs` — it spawns `playwright test`, and *after*
  Playwright tears the webServer down (app gone, db closed, `vite` orphaned) it reaps the orphan
  `vite` (scoped to this repo's `frontend` so it never touches a sibling checkout) and removes the
  temp dir, then exits with Playwright's code. The runner's cmdline is `node run-e2e.mjs`, so the
  fallback `pkill` no longer self-matches; cross-platform, and a bare `pnpm test` behaves like
  `make e2e`.
- **DB oracle reads a dir the app never wrote.** *Cause:* Playwright re-evaluates
  `playwright.config.ts` in **every worker**, so a module-top-level random `mkdtemp` yields a
  different dir per process. *Fix:* a **deterministic** fixed dir (`join(tmpdir(),
  "agentre-e2e-data")`), cleaned/created **only in the main runner**
  (`if (process.env.TEST_WORKER_INDEX === undefined)`) before the webServer launches; workers
  reuse the same path.
- **False-green against the wrong app.** *Cause:* a dev server on Wails' default 34115 +
  `reuseExistingServer` reusing it. *Fix:* dedicated port **34216** + `reuseExistingServer:!CI`.
- **Kept-on-failure for debugging.** On a failing run `run-e2e.mjs` deliberately **keeps** the
  temp data dir (`agentre.db` + logs) and the webServer log so you / CI can inspect them; the next
  run wipes the dir at start anyway. On success it removes both.

## 8. Extending the harness

- **Fake a new event** (tool call / error) → branch the fake `Run` on a prompt prefix (e.g. an
  `@e2e:…` directive) to emit `ToolCall` / `ErrorEvent` from the sealed `agentruntime.Event` set.
  **Not implemented yet** — it's the intended seam; add it red→green (with a fake-runtime unit
  test) when a spec first needs it.
- **Fake an injected MCP tool call** → when the real backend would call an injected MCP tool, the
  fake makes the same HTTP `tools/call` like a real CLI. **Done for `group_send`**: a group member
  turn injects a `group` MCP server (`group_svc.buildGroupMCP`), so the fake `Run` detects it
  (`findGroupSendServer`) and POSTs `group_send` to the gateway `/mcp/group/` — driving the real
  `IngestAgentMessage` so the member reply bubbles into the group transcript
  (`group-chat.spec.ts` asserts the visible bubble + the `agentGroupMessageCount()` DB twin). Model
  any future injected-tool fidelity on this; it's the deterministic-fake-as-MCP-client seam.
- **Fake another backend** (codex / builtin / remote) → add a fake package under `//go:build e2e`
  + one more `RegisterRuntime` line in `e2e_install.go`. Never a patch to production control flow.
- **A new UI assertion target** → add a `data-testid` (additive) in the same style as §5.
- **A new persistence oracle** → add a read-only `node:sqlite` helper to `e2e/fixtures/db.ts`.

## 9. File map

| Path | Role | Committed? |
|---|---|---|
| `e2e/run-e2e.mjs` | cross-platform runner: spawns `playwright test`, then reaps orphan `vite` + removes temp dir after it exits (kept on failure) | yes |
| `e2e/playwright.config.ts` | base harness: temp dir + env + `frontend/dist` prep, webServer (`wails dev -tags e2e -devserver 34216`) | yes |
| `e2e/playwright.scratch.config.ts` | extends base, `testDir: ./scratch` for throwaway specs | yes |
| `e2e/fixtures/db.ts` | read-only `node:sqlite` DB oracle (`runningSessionCount`, …) | yes |
| `e2e/tests/*.spec.ts` | committed **core-flow** specs (`smoke-chat.spec.ts`) | yes |
| `e2e/scratch/*.spec.ts` | throwaway functional-verification specs | **no (gitignored)** |
| `e2e/scratch/README.md` | scratch convention + starter template | yes |
| `e2e/package.json` → `setup` / `test` / `test:scratch` | one-time install+Chromium / run suite / run scratch | yes |
| `Makefile` → `e2e` / `e2e-scratch` | thin aliases for `cd e2e && pnpm test` / `pnpm run test:scratch` | yes |
| `e2e_install.go` (`//go:build e2e`) / `e2e_install_noop.go` (`//go:build !e2e`) | register the fake + seed / no-op | yes |
| `internal/pkg/agentruntime/runtimes/fake/` | the deterministic fake runtime (entire package `//go:build e2e`) | yes |

claudecode backend only. The committed suite covers single-chat, session reload, and the
group-chat round-trip (the fake acts as the `group_send` MCP client so the member reply bubbles in
— see §8). Settings / multi-backend / codex / remote e2e remain future specs that reuse this same
harness and the fake-runtime seam above.
