# E2E Harness Guide (Playwright × the real Wails app)

> How to drive the **real running Agentre app** end-to-end with Playwright — both the committed
> core-flow suite and **ad-hoc functional verification of a feature you just finished**. Written
> for agents (Claude / Codex) and developers.
>
> This doc **owns** the GUI-e2e harness. For SQLite / log / table-to-feature debugging see
> [debugging.md](../docs/debugging.md). This file is the living reference for the harness.

Agentre is an IPC-only Wails desktop app — there is no HTTP API to hit. But `wails dev` exposes
the app over a browser-accessible IPC bridge, so Playwright (Chromium) can open it like a normal
page and drive the **real React frontend → real Wails IPC → real Go service/repository → real
SQLite**. The external agent boundary does **not** run for real: a real turn spawns claude-code / codex subprocesses and discovers skills from those CLIs (slow, nondeterministic, needs external auth), so e2e replaces the `agentruntime.Runtime` with a deterministic fake and registers deterministic Claude/Codex skill discoverers. Every downstream app path (services, dispatcher, handlers, DB, IPC, migrations) runs for real.

## 1. Three modes — pick the right one

| | **Driven app** | **Committed core-flow suite** | **Scratch spec** |
|---|---|---|---|
| You author | **nothing** | `e2e/tests/*.spec.ts` (committed) | `e2e/scratch/**/*.spec.ts` (**gitignored**) |
| Run with | `make verify-up`, then `node e2e/drive.mjs <action>` (§4a) | `make e2e` | `make e2e-scratch TASK="scratch/<name>/"` |
| Lifetime | the app stays up until `make verify-down` | permanent regression guard | throwaway — write, run, observe, delete |
| What goes here | "I just built X — does it work in the real app?", one-off looks, bug reproduction, spec acceptance | **only core / critical flows** | replay, timing or concurrency **as the contract** |

**Reach for the driven app first.** Starting an app and clicking through it costs one command;
a spec that only navigates and screenshots is a spec you will delete, written slowly. Author a
scratch spec when the *sequence itself* must be replayed or when timing/concurrency is what you
are asserting — otherwise drive it.

**The bar for a committed spec is high.** A committed GUI e2e spec is slow (builds + runs the
real app, ~30 s per run even with the fake) and a maintenance liability. Only add one for a
**core flow** (app boots, new session → send → streamed reply → idle). Everything else gets
verified ad-hoc and thrown away — **what that ad-hoc run must produce is
[docs/verification.md](../docs/verification.md)'s**, not this file's.

## 2. Architecture

```
make e2e  →  cd e2e && pnpm test  →  node run-e2e.mjs   (spawns playwright, reaps residue after)
  └─ playwright (workers:1, testDir ./tests)
       ├─ webServer:  wails dev -tags e2e -devserver localhost:<this checkout's port> > "$LOG" 2>&1
       │                 ├─ vite (frontend HMR)
       │                 └─ agentre app (-tags e2e) → real services → <instance>/fake/data/agentre.db
       │                       └─ agentruntime.RuntimeFor(claudecode) overridden by the FAKE (echo)
       └─ chromium → http://localhost:<port>  (Wails IPC websocket bridge → real Go backend)
                         └─ specs assert on the UI …
                              … and on the DB via a direct read-only node:sqlite query (oracle)
```

Playwright drives its **own** chromium against that port. The native webview window that
`wails dev` opens is incidental and ignored. The app is launched with these env overrides
(injected by `e2e/playwright.config.ts`):

| Env | Effect |
|---|---|
| `AGENTRE_DATA_DIR=<instance>/<flavor>/data` | DB / config / logs under a throwaway dir — the highest-precedence data-root override (`internal/pkg/paths/paths.go`), so it never collides with your real DB or the `make dev` root |
| `AGENTRE_KEYCHAIN_DIR=<instance>/<flavor>/keychain` | device tokens, fingerprint, and e2e login secrets use an isolated 0700 file-keychain directory instead of the production system keychain; bootstrap rejects a missing or unsafe directory. **Not gated on the `e2e` build tag** (`internal/bootstrap/keychain.go`), so a real-runtime launch (§4a `FLAVOR=real`) is isolated the same way |
| `AGENTRE_ENV=test` | quiet logger level (`internal/bootstrap/cago.go` `appEnv()`) |
| `AGENTRE_PROXY_PORT=0` | bind the local HTTP gateway to an OS-chosen **free** port instead of the fixed default 52401 (`internal/bootstrap/cago.go` `loadProxyAddr` → `proxyPortFromEnv`). The fixed port is **not** data-dir-scoped, so a running real Agentre already holds 52401; without this override the e2e gateway fails to bind → `BaseURL()` is empty → every gateway round-trip (MCP tool calls, hooks, LLM forward) silently dies. `BaseURL()` reports the real bound port via the listener, so nothing hardcodes 52401 |

`<instance>` is `$TMPDIR/agentre-verify/<id>`, where `<id>` is a hash of **this checkout's
absolute path** — so a worktree gets its own dirs, its own ports and its own session file, and two
checkouts can verify at the same time. The bridge port comes from the same derivation (never
Wails' default 34115), so a run never reuses — or collides with — a `make dev` server you already
have open. `make verify-status` prints the id and the resolved paths.

### The build-tag seam (why the fake never ships)

Real runtimes auto-register in their package `init()` (e.g. `runtimes/claudecode`). The fake
**overrides** that slot through the existing registry — no scattered `if env == e2e`:

1. Every `internal/pkg/agentruntime/runtimes/fake/*.go` carries `//go:build e2e`, so the package
   and its imports are absent from any default build.
2. `main()` calls `fakes.Install(context.Background())` (`main.go`, right after `bootstrap.Init`
   and before `wails.Run`). In an `e2e` build that resolves to `e2e/fakes/install.go`
   (`//go:build e2e`), which calls `agentruntime.RegisterRuntime(TypeClaudeCode, fakert.New())`
   **after** all package `init()`s, so the fake wins the slot, and seeds a local backend attached
   to the system CEO agent.
3. In a default build it resolves to `e2e/fakes/install_noop.go` (`//go:build !e2e`) — an empty
   `Install`. Default builds compile this empty seam and the `fakes.Install(...)` call, but none of the fake runtime, skill discoverers, seeding, or registrations; production behavior and data are unchanged.

> `e2e/fakes/` is a **separate Go package** living under `e2e/` deliberately: it keeps the Go
> sources out of the same directory as the TS/Playwright toolchain while staying next to what
> they serve.

The fake (`internal/pkg/agentruntime/runtimes/fake/runtime.go`) echoes the prompt back as
`ReplyPrefix + req.UserText` (`ReplyPrefix = "e2e-fake-reply: "`) in 8-rune `TextDelta` chunks,
then `Done`. Same prompt → byte-identical stream, every run. It has its own `//go:build e2e`
unit test asserting the emitted sequence (red→green before it's wired into the registry).

## 3. Isolation & safety guarantees

A run is fully hermetic, and in particular **a running Agentre does not interfere**:

- **Data** — DB / config / logs live under `<instance>/fake/data` (`agentre.db`), removed by
  `run-e2e.mjs` after a **passing** run (kept on failure for debugging — see §7). Your real
  `~/Library/Application Support/agentre` is never touched.
- **Keychain** — device tokens, fingerprint, and seeded login credentials live under the 0700
  `<instance>/fake/keychain` file backend. The runner removes it after a passing run and keeps
  it with other evidence on failure; the e2e process never reads, writes, or deletes production
  system-keychain entries.
- **Single-instance lock** — set only when `!isWailsDevMode()` (`main.go`); e2e runs via
  `wails dev` (sets the `devserver` env → dev mode), so the lock is **already skipped**, and its
  id is data-dir-scoped (`singleInstanceUniqueID(dataDir)`) regardless. So an e2e run launches
  even with a real Agentre open. **No backend Go change was needed** for hermeticity — contrast
  the opskat harness, which had to add an explicit `OPSKAT_E2E` lock-skip + `ResolvedDataDir`.
- **Bridge port** — derived per checkout (a 4-port block in 34300–35500: bridge + CDP for each
  flavor), so it never collides with `make dev` on 34115, with the sibling dual-end runner's
  34217 (§11), or with another worktree's run.
- **Gateway port** — the local HTTP gateway's default 52401 is **not** data-dir-scoped, so a
  running real Agentre holds it; e2e sets `AGENTRE_PROXY_PORT=0` to bind a free port instead (see
  §2's env table). Without this the gateway degrades and every gateway round-trip (MCP tool calls,
  hooks) silently fails — committed specs that exercise injected tools would go red against a
  perfectly-good backend.

Run one e2e invocation at a time **per checkout** — inside one checkout the data dir, the ports
and `build/bin` are shared. Across worktrees they are not: every path and port is derived from the
checkout's own absolute path, so a second worktree can run its own verification simultaneously.
CI runners are isolated, so each job's run is independent.

**These are not conventions — they are checked.** [`lib/target.mjs`](lib/target.mjs) is the only
place a data dir, keychain dir or port is named, and it refuses everything else:
`assertIsolatedDataDir` rejects the installed app's root and your own `make dev` root
(`<config>/agentre-dev`) by name, `assertSanctionedURL` rejects any origin but the target's own
bridge, and `drive.mjs sql` accepts only read-only statements. The guards are unit-tested
(`pnpm run test:guards`, also run by `run-e2e.mjs` **before** any app is launched), and
`e2e/keychain_harness_test.go` pins the keychain override in `make test-backend`.

Two more rules fall out of what a checkout shares:

- **Reaping the stray `vite`** matches by repo path and cannot tell one launch's dev server from
  another's, so teardown skips the reap while any other session in this checkout is live
  (`shouldReapVite`). Killing it instead leaves a live app listening and serving 502 with nothing
  logged.
- **One flavor at a time per checkout** (`blocksSecondFlavor`): `wails dev` compiles into this
  checkout's `build/bin`, so starting the second flavor here overwrites the binary the first is
  running and kills it mid-run — the log shows only `no such file or directory`. Run the second
  flavor from another worktree.

## 4. Running the committed suite

```bash
cd e2e && pnpm run setup   # one-time: install deps + Chromium (skip if already done / on CI)
make e2e                   # or, equivalently: cd e2e && pnpm test
```

Prereqs: `wails` CLI on PATH, `pnpm`, and Node 24+ with stable built-in `node:sqlite` (the E2E CI job pins Node 24). `pnpm run setup` installs the e2e deps and Chromium **once**; `make e2e` only
runs the suite — no per-run install. The first run builds Go + Vite (~30 s) and **opens a native
Agentre window** — expected; the test drives its own browser instance, not that window. The
window closes when the suite ends.

**Platforms.** Runs on macOS, Linux, and (best-effort) native Windows. `make e2e` is a thin alias
for `cd e2e && pnpm test`, so on Windows (no `make`) run `cd e2e && pnpm test` directly. *All*
orchestration and cleanup live in `e2e/run-e2e.mjs` (cross-platform Node) — there are no
shell-only `pkill` / `mkdir -p` / `touch` steps. CI exercises the Linux path; the Windows reap
branch (PowerShell CIM) is by-inspection only.

**Debug loop.** Stay inside the real runner so frontend preparation, the temp data directory, `AGENTRE_PROXY_PORT=0`, and cleanup remain identical to an ordinary run:

```bash
cd e2e && pnpm test --debug     # Playwright inspector
cd e2e && pnpm test --headed    # watch the browser
```

Never hand-start a Wails server against the fixed e2e data directory: a plain suite run prepares
that directory for a fresh start, so a separately running app and the DB oracle diverge. Use the
launcher below — it owns the directory, and the runner rejects a server that is not on it.

**Fast inner loop (reuse).** Every normal run pays the full start-up tax (Go compile + vite + app boot + re-seed, ~30 s even with the fake). When iterating on a scratch spec — tweak, re-run, observe — reuse the app `make verify-up` (§4a) already has running:

```bash
make verify-up                                              # once
cd e2e && AGENTRE_E2E_REUSE=1 pnpm run test:scratch "scratch/<task>/"
AGENTRE_E2E_REUSE=1 make e2e-scratch TASK="scratch/<task>/"  # or via make
```

`AGENTRE_E2E_REUSE=1` switches `playwright.config.ts` and `run-e2e.mjs` into **reuse mode**: the temp data dir is **not** wiped (the running app owns it), `reuseExistingServer` is forced on, and the runner performs **no teardown** (no orphan-vite reap, no temp-dir removal) so the app survives for the next iteration. It is an explicit "the launcher started the server" contract: the runner fails fast unless **both** this checkout's bridge port is listening **and** its `agentre.db` exists (a server up on the real data dir — e.g. started without `AGENTRE_DATA_DIR` — would silently seed your real DB, so it is rejected). Stop it with `make verify-down`. The default (flag unset) is unchanged and fully hermetic: a fresh data dir each run, with Playwright managing and tearing down its own server.

## 4a. The driven app — `make verify-up` + `drive.mjs`

The mode you reach for first (§1). One launcher brings up an isolated app plus a browser attached
to it; from then on each `drive.mjs` call performs **one** action against that live app and
appends what it did to the scenario's ledger. No spec, no rebuild between steps.

```bash
make verify-up                     # fake runtime, -tags e2e
make verify-up FLAVOR=real         # real runtimes, no build tag (real claude/codex CLIs)
make verify-up VERIFY_FLAGS=--headed        # or --no-browser / --keep (keep the previous state)
make verify-status                 # exits non-zero when nothing is up
make verify-down                   # VERIFY_FLAGS=--wipe also removes the isolated state
```

| Target | Runtime | Data dir | Use it for |
|---|---|---|---|
| `fake` (default) | deterministic fake (`-tags e2e`), echoes `e2e-fake-reply: <prompt>` | `<instance>/fake/data` | anything downstream of the agent boundary: UI, IPC, services, DB, migrations |
| `real` | the real claudecode / codex runtimes, real CLI subprocesses | `<instance>/real/data` | behaviour that *is* the CLI: frame handling, auth, model selection, skills |

Ports and dirs are derived per checkout (§3), so `make verify-status` is how you read them —
nothing hardcodes a number.

**Nothing is visible by default.** The browser runs headless and every driven page is explicitly
set to a fixed **1440×900 viewport**, so full-page evidence remains legible and consistent instead
of inheriting Chromium's small platform-dependent default. Because the frontend calls
`WindowShow()` on mount — from the driving browser as much as from the native webview — the
launcher hides the native window again right after boot, and `drive.mjs goto` re-hides it after
each navigation. `VERIFY_FLAGS=--headed` is there for when you want to watch; driven pages keep
the same viewport size.

The launcher writes a session handshake (`<instance>/<flavor>.json`) that the driver reads, so no
command repeats a port or a path. It is idempotent — a second `verify-up` reports
"already up" — and it **adopts** an app someone already started on the sanctioned data dir rather
than restarting or wiping underneath it. A server on that port that is *not* on the sanctioned
dir is refused outright.

Driving, one action per call:

```bash
export AGENTRE_VERIFY_SCENARIO=<slug>       # → e2e/scratch/<slug>/{logs,screenshots}

node e2e/drive.mjs snapshot [selector] [--limit N]   # visible elements + how to address them
node e2e/drive.mjs goto /                            # same-origin paths only
node e2e/drive.mjs click "testid=new-chat-button"    # testid= / role=button[name="Save"] / text= / label= / CSS
node e2e/drive.mjs fill ".ProseMirror" "hello"
node e2e/drive.mjs press Enter                       # or: press "testid=x" Enter
node e2e/drive.mjs wait "testid=x" --state visible
node e2e/drive.mjs text "main"                       # visible text of a region
node e2e/drive.mjs shot 01-after-send [selector] [--full]
node e2e/drive.mjs eval "document.title"
node e2e/drive.mjs sql "select count(*) from chat_sessions where status='running'"
node e2e/drive.mjs logs 40
```

- **`snapshot` first.** Locators come from what it prints (`testid=…` wherever the component has
  one) — never from visible text, which is i18n'd and moves.
- **`sql` is the independent oracle**, read-only by construction: it reads the isolated
  `agentre.db` directly, so it catches "UI says OK but the DB never got written". Together with
  `logs` it keeps working after the app is gone, while you write the report.
- **Every call is recorded** to `e2e/scratch/<slug>/logs/drive.log` — command, timestamp, and
  `ok:`/`FAILED:` with the outcome. That ledger *is* the run's record; the report cites it.
- **A dead app names itself.** The driver checks the bridge before acting, because a stopped app
  and a blank page look identical through CDP (both snapshot as "0 elements").

Missing a stable hook? Add a **minimal** `data-testid` to the component — additive, in scope for
the task at hand, no renaming sweep (§5).

`wails dev` rebuilds on Go/TS edits, so a long-lived driven app restarts under you when the
working tree changes; if its `vite` is killed the app keeps listening but serves 502. Both show
up as the driver's "not serving any more" error — `make verify-down && make verify-up` is the fix.

The HTML report lands at `e2e/playwright-report/` (gitignored) **in CI only** — a local run uses the list reporter and keeps traces/screenshots on failure (`CI=1` forces a local HTML report). webServer output → `$TMPDIR/agentre-e2e-webserver.log`. e2e is **not** part of `make test` / `make check` — it runs only on demand.

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

## 6. Ad-hoc functional verification

**Most verification authors nothing**: the driven app (§4a) and the `agrctl` / `agentred`
binaries reach every surface with no file written. Author a scratch spec only when the sequence
must be *replayed* or when timing/concurrency is the contract; it then follows §5's conventions
(`data-testid` locators, auto-wait, the DB oracle), and the difference is only that it lives
under `e2e/scratch/` and is never committed.

**The workflow itself — when driving the real app is warranted, where the evidence goes, the
`report.md` a run owes, and how to report a bug reproduction honestly — is owned by
[docs/verification.md](../docs/verification.md).** It is not repeated here.

Three harness facts it relies on:

- The scenario directory is shared: `drive.mjs --scenario <slug>` writes into
  `e2e/scratch/<slug>/{logs,screenshots}`, the same directory a scratch spec and its `report.md`
  live in, so a run that starts driven and ends up needing a spec keeps one pile of evidence.
- `playwright.scratch.config.ts` reuses the exact webServer / env / isolation / teardown as the
  committed suite — only `testDir` points at `./scratch`, and Playwright scans it **recursively**,
  so `scratch/<task-name>/verify.spec.ts` is picked up.
- While the scenario runs, assert DB side effects through `fixtures/db.ts`. After a **failed** run, the runner preserves the temp `agentre.db`, app logs, webServer log, and Playwright trace/screenshots for inspection; after success it deletes the temp DB/logs, so the report must retain the deciding assertion/output rather than promising a post-run query. (Log/DB reading: [debugging.md](../docs/debugging.md).)

If the flow turns out to be core and worth guarding forever, *promote* it: move it into
`e2e/tests/`, harden, commit (§5).

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
  derived from the checkout path — `lib/target.mjs`), cleaned/created **only in the main runner**
  (`if (process.env.TEST_WORKER_INDEX === undefined)`) before the webServer launches; workers
  reuse the same path.
- **False-green against the wrong app.** *Cause:* a dev server on Wails' default 34115 +
  `reuseExistingServer` reusing it. *Fix:* a dedicated per-checkout port + `reuseExistingServer:!CI`.
- **Kept-on-failure for debugging.** On a failing run `run-e2e.mjs` deliberately **keeps** the
  temp data dir (`agentre.db` + logs) and the webServer log so you / CI can inspect them; the next
  run wipes the dir at start anyway. On success it removes both.

## 8. Extending the harness

- **Fake a new event** (tool call / error) → branch the fake `Run` on a prompt-prefix directive, `e2e-<name>:<args>` in the user's message. This seam is **already implemented** for several flows (each red→green with a fake-runtime unit test): `e2e-subagent-call:<agent>:<prompt>` (calls the injected `/mcp/subagent/` `agent_call`), `e2e-org-create-dept:<name>` and `e2e-hook-create:<name>` (approved write tools on the injected `/mcp/org/` / `/mcp/hook/`), `e2e-ask:<question>` (AskUserQuestion expiry), `e2e-bg-task:<label>` (background-turn → CLI auto-continue), `e2e-long-thinking:<runes>` (long thinking stream), and `e2e-assert-system:<needle>` (system-prompt assertion). Add a new one the same way — red→green with a fake-runtime unit test — when a spec first needs it.
- **Fake an injected MCP tool call** → when the real backend would call an injected MCP tool, the
  fake makes the same HTTP `tools/call` like a real CLI. **Done for the `org` / `subagent` / `hook` tools**:
  when the agent has the relevant tool enabled, the real backend injects the MCP server, so the
  fake `Run` detects it (`findGroupToolServer`) and POSTs the appropriate tool call to the gateway
  endpoint — driving the real service handler. Model any future injected-tool fidelity on this;
  it's the deterministic-fake-as-MCP-client seam.
- **Fake another backend** (codex / builtin / remote) → add a fake package under `//go:build e2e`
  + one more `RegisterRuntime` line in `e2e/fakes/install.go`. Never a patch to production control flow.
- **A new UI assertion target** → add a `data-testid` (additive) in the same style as §5.
- **A new persistence oracle** → add a read-only `node:sqlite` helper to `e2e/fixtures/db.ts`.

## 9. File map

| Path | Role | Committed? |
|---|---|---|
| `e2e/lib/target.mjs` | **the only place** a flavor, port, data dir or keychain dir is named; the isolation guards (`assertIsolatedDataDir`, `assertSanctionedURL`, `shouldReapVite`) and the session handshake | yes |
| `e2e/lib/target.test.mjs` | `node --test` guards for the above (`pnpm run test:guards`; `run-e2e.mjs` runs them before launching anything) | yes |
| `e2e/lib/procs.mjs` | the orphan-`vite` reap, shared by the runner and the launcher | yes |
| `e2e/verify.mjs` | the launcher: `up` / `down` / `status` for a flavor, incl. the attached CDP browser (§4a) | yes |
| `e2e/drive.mjs` | the driver: one action per invocation against the live app, recorded into the scenario (§4a) | yes |
| `e2e/run-e2e.mjs` | cross-platform runner: spawns `playwright test`, then reaps orphan `vite` + removes temp dir after it exits (kept on failure) | yes |
| `e2e/playwright.config.ts` | base harness: resolves the `fake` target from `lib/target.mjs`, preps dirs + `frontend/dist`, webServer (`wails dev -tags e2e -devserver <derived port>`) | yes |
| `e2e/playwright.scratch.config.ts` | extends base, `testDir: ./scratch` for throwaway specs | yes |
| `e2e/fixtures/db.ts` | read-only `node:sqlite` DB oracle (`runningSessionCount`, …) | yes |
| `e2e/tests/*.spec.ts` | committed **core-flow** specs (chat smoke/reload, git-preview tabs, plus approved org/subagent tool flows) | yes |
| `e2e/scratch/<slug>/` | one scenario's evidence: `logs/drive.log`, `screenshots/`, `report.md`, and a `verify.spec.ts` when one was warranted | **no (gitignored)** |
| `e2e/scratch/README.md` | scratch convention + starter template | yes |
| `e2e/package.json` → `setup` / `test` / `test:guards` / `test:scratch` / `test:sync` / `verify` / `drive` | one-time install+Chromium / run suite / run the isolation guards / run scratch / run the sync suite (§10) / launcher / driver | yes |
| `Makefile` → `e2e` / `e2e-scratch` / `e2e-sync` / `verify-up` / `verify-status` / `verify-down` | thin aliases for `cd e2e && pnpm test` / `pnpm run test:scratch` (requires `TASK=scratch/…`) / `pnpm run test:sync` / `node verify.mjs …` (`FLAVOR=`, `VERIFY_FLAGS=`) | yes |
| `e2e/fakes/install.go` (`//go:build e2e`) / `install_noop.go` (`//go:build !e2e`) | register the fake + seed / no-op | yes |
| `e2e/fakes/login.go` (`//go:build e2e`) | seed a logged-in desktop from env, for §10 / §11 only (no-op without it) | yes |
| `e2e/fakes/remote.go` (`//go:build e2e`) | seed a paired agentred + an agent bound to it, for §11 only (no-op without it) | yes |
| `e2e/run-e2e-sync.mjs` / `playwright.sync.config.ts` / `sync/` / `fixtures/sync.ts` | the sync suite: runner, config, specs, oracles (§10) | yes |
| `internal/pkg/agentruntime/runtimes/fake/` | the deterministic fake runtime (entire package `//go:build e2e`) | yes |

Turn execution uses the fake Claude Code runtime only; E2E also seeds a Codex backend and deterministic Claude/Codex skill discovery, but does not fake or execute Codex turns. The committed suite covers chat smoke, session reload, and the approved org/subagent injected-tool flows (the fake acts as an MCP client — see §8).
Settings / multi-backend / codex / remote e2e remain future specs that reuse this same harness
and the fake-runtime seam above.

## 10. The sync suite (`make e2e-sync`) — a real server and a simulated peer

`e2e/sync/` is a **third** mode, separate from §1's two. It verifies workspace
multi-device sync (`docs/specs/2026-08-07-workspace-sync.md`), which by definition
cannot be checked with one app: it needs a real `agentre-server`, a real account,
and a second client.

```
make e2e-sync  →  cd e2e && pnpm run test:sync  →  node run-e2e-sync.mjs
  ├─ builds agentre-server + cmd/synce2e            (GOWORK=off, sibling checkout)
  ├─ starts the server on a FREE port against the developer's PostgreSQL + Redis
  │    (a scratch copy of configs/config.yaml — the repo's own file is untouched)
  ├─ seeds ONE throwaway account with three devices, straight into PostgreSQL
  ├─ puts a cut-able proxy in front of the server (the desktop talks to that)
  ├─ runs playwright with playwright.sync.config.ts
  └─ ALWAYS deletes exactly the rows it seeded, then prints the residue
```

**It is not in CI and not part of `make e2e`.** It depends on the developer's
database; when that is unreachable the runner fails with a message naming what is
missing — it never skips and never reports a pass it did not get.

Three things it adds that the other modes do not have:

| Piece | Why |
|---|---|
| `synce2e seed` / `cleanup` (agentre-server `cmd/synce2e`) | Desktop login ends at GitHub OAuth, which nobody can click here. Seeding writes an account + devices + one refresh token per device directly; every run gets its own account and its own fingerprints, and cleanup is scoped to that one user id — no `TRUNCATE`, nothing that belongs to anyone else |
| `e2e/fakes/login.go` (`//go:build e2e`) | Turns the seeded identity (passed in through `AGENTRE_E2E_SERVER_URL` / `_SERVER_USER_ID` / `_DEVICE_ID` / `_DEVICE_FINGERPRINT` / `_REFRESH_TOKEN`) into what a completed login leaves behind: the `server_state` row + two keychain entries + an access token. Without those env vars it is a no-op, so §4's suite still runs fully offline |
| `synce2e peer` | A simulated second desktop: same `/v1/sync/*` endpoints, its own device identity, its own cursor and replica, and — the dimension R2b/R15 exist for — **its own set of paired agentred fingerprints**. It is deliberately not a second Wails instance: the bridge port and data dir are fixed values, so only one real app can run at a time |

Gotchas learned building it:

- **`wails dev` runs the app binary twice** — once to generate the frontend
  bindings, once for real — so `fakes.Install` executes twice per run. Refresh
  tokens rotate on use, so `login.go` prefers whatever is already in the keychain
  over the seeded env value; replaying the spent one is "refresh token reuse
  detected" and the desktop logs itself out.
- **The network cut is a flag file.** `run-e2e-sync.mjs` proxies the desktop's
  traffic and destroys every connection while `SYNCE2E_OFFLINE_FLAG` exists; specs
  call `cutNetwork()` / `restoreNetwork()`. No control port, no IPC.
- **The local-path report only rides the 30 s ticker** (R16, by design), so the
  spec that checks it legitimately waits ~half a minute; the suite timeout is
  raised accordingly.
- **The delete spec pins two whole-queue wedges.** A tombstone the server rejects
  fails the *entire* push batch, and `pushBatch` dequeues nothing on error — so one
  delete stops every later upload from that desktop, and the downlink with it
  (`pull` runs only after `flush` succeeds). Both known causes are fixed: the
  tombstone's `"payload": null` (now `omitempty`) and the `project_location`
  tombstone's absent `project_sync_id` (the server's natural-key guard now skips
  deleted items). The spec deletes a project that has a path record, so it covers
  the cascade rather than the plain project tombstone alone.

## 11. The dual-end run — this app **and** a browser on one agentred

A fourth mode, and the only one this repo does not drive: it is orchestrated from
the sibling `agentre-server` checkout (`cd agentre-server/e2e && pnpm dual`,
`run-e2e-web.mjs --dual`). That runner starts a real server + a real `agentred`,
seeds one throwaway account, and then brings up **two** ends against it — a
browser and this app (`wails dev -tags e2e -devserver 34217`, its own temp data
dir, the seeded login of §10). One Playwright test drives both.

It exists for the handful of requirements that only two simultaneous ends can
decide (`docs/specs/2026-08-10-web-session-access.md` R7 / R18 / R19 / R6 / R10):
a browser's message must land in **this app's** transcript as a real user row
carrying `chat.message.fromDevice`, not as a reply with no question.

What this repo contributes:

| Piece | Why |
| --- | --- |
| `e2e/fakes/remote.go` (`//go:build e2e`) | The app joins that agentred the way a claimed machine would: one `paired_agentreds` row (`AGENTRE_E2E_AGENTRED_FINGERPRINT` / `_URL`) + a claudecode backend bound to it + the `E2E Remote Agent` that uses it. Every turn with that agent therefore goes `remote.Runtime → ConnPool.Borrow → /v1/relay/client` — the browser's own endpoint. The paired row's URL points at a dead loopback port on purpose: `Borrow` races LAN-direct against relay, and only the relay leg is under test. It re-runs `bootstrap.InitRemoteDevice` first, because `login.go` **replaces** the `server_svc` singleton the connection pool captured at boot (a real login mutates that instance instead), and a pool still holding the old one dials with no access token |
| `e2e/fakes/login.go` | the same seeded login §10 uses |

Run it from `agentre-server/e2e`; its README owns the runner, the ports and the
runtime gotchas the scenario had to be shaped around.
