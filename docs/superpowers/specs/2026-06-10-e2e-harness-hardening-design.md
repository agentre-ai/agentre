# E2E Harness Hardening — adopt opskat PR #173's runner/layout (design)

**Date:** 2026-06-10
**Status:** design (awaiting implementation)
**Reference:** [opskat/opskat#173](https://github.com/opskat/opskat/pull/173) — "GUI E2E 测试流程(Playwright × 真实 Wails 应用)"
**Supersedes harness from:** [`2026-06-09-e2e-testing-design.md`](./2026-06-09-e2e-testing-design.md) (the current Makefile-orchestrated, `frontend/e2e/`-nested harness)

## Goal

Harden agentre's Playwright e2e harness by adopting opskat PR #173's proven design: a
**root-level standalone `e2e/` package**, Playwright's `webServer` re-enabled with the
**teardown-hang root cause fixed** (orphan-vite-holds-the-pipe → redirect output to a file),
a **thin cross-platform Node runner** (`run-e2e.mjs`) that reaps residue after Playwright
exits, a **dedicated devserver port** so it never collides with a real `wails dev`, and a
**`node:sqlite` DB oracle** that asserts persistence independent of the app's service layer.

This is *not* a from-scratch harness — agentre already has the hard parts (the `e2e`
build-tag fake runtime, `e2e_install.go` seeding, the smoke chain). We are restructuring the
**orchestration + layout + docs** to match opskat, and adding the DB oracle.

## Why (the concrete problems being fixed)

1. **Latent CI self-kill.** `make e2e` runs as one shell whose command line literally
   contains `wails dev -tags e2e` (both the launch line and `pkill -f "wails dev -tags e2e"`
   in the `trap`). On Linux `pkill -f` matches `/proc/<pid>/cmdline`, so the recipe shell can
   SIGTERM itself — exactly the `exit 143` / `make: *** Terminated` bug opskat hit. Moving all
   orchestration into Node (whose cmdline is `node run-e2e.mjs`) removes the self-match.
2. **No cross-platform support.** The `trap`/`pkill`/`curl`-poll orchestration is Unix-only.
   opskat's Node runner + PowerShell-CIM fallback gives native Windows (best-effort) + macOS +
   Linux from one code path.
3. **Port collision / false-green risk.** agentre's e2e uses the **default** wails devserver
   port `34115`. A real `make dev` (or a sibling app) on `34115` + Playwright's reuse could
   false-green against the wrong app. opskat's dedicated port `34216` removes this.
4. **No source-of-truth assertion.** The smoke spec only checks the UI spinner. The
   "stuck running / lost status write" bug class is a *DB* bug (the `chat_sessions.agent_status`
   write is dropped). A `node:sqlite` oracle asserting `agent_status` directly catches it at
   the source of truth, not just the UI symptom.

## What we adopt from opskat vs. what differs

| Aspect | opskat #173 | agentre (this design) |
|---|---|---|
| Runner architecture | Playwright `webServer` + thin post-cleanup `run-e2e.mjs` | **same** |
| Teardown-hang fix | `stdout/stderr:"ignore"` + `wails dev … > "$LOG" 2>&1` | **same** (this is the root cause agentre originally dodged by dropping `webServer`) |
| Dedicated port | 34216 | **34216** |
| Layout | root `e2e/` standalone pnpm package | **same** (moved out of `frontend/e2e/`) |
| Scratch specs | `e2e/scratch/` + `playwright.scratch.config.ts` + `make test-e2e-scratch` | `e2e/scratch/` + `playwright.scratch.config.ts` + `make e2e-scratch` (replaces `frontend/e2e/local/` + `E2E_SPEC=`) |
| DB oracle | `node:sqlite` `findAssetByName` (real backend) | `node:sqlite` `runningSessionCount` (fake runtime; assert no stuck `running` session) |
| **Backend under test** | **real** runtime; verifies real writes | **fake** runtime (build tag `e2e`) — *unchanged*; the DB oracle still proves the real services/DB write path |
| Backend Go enablers | `OPSKAT_E2E=1` lock skip, `ResolvedDataDir()`, socket isolation | **none needed** — see "No backend changes" below |

The one philosophical difference stays: agentre **fakes the agent runtime** (subprocess-free,
deterministic), so we do not run real claude-code/codex. Everything *else* (React → Wails IPC
→ Go services → dispatcher → SQLite → migrations) runs for real, and the DB oracle now proves
the real write path.

## No backend changes (rationale)

opskat needed `main.go` / `bootstrap` / socket changes to make its real backend hermetic.
agentre needs **zero** Go changes, verified:

- **Single-instance lock** is set only when `!isWailsDevMode()` (`main.go:88`); e2e runs via
  `wails dev` (sets the `devserver` env → dev mode), so the lock is already skipped. It is
  also data-dir-scoped (`singleInstanceUniqueID(dataDir)` = sha256 of the data dir), so even
  in prod two data dirs never collide.
- **Data/DB/config/logs** already isolate via `AGENTRE_DATA_DIR` — the highest-precedence
  override in `paths.AppDataDir()` (`paths.go:38`), winning even in dev mode. DB lands at
  `$AGENTRE_DATA_DIR/agentre.db`.
- **Sockets** — the only unix socket (`agentred.sock`) belongs to the *daemon* binary
  (`internal/daemon/ipc.go`), not the desktop app; irrelevant here.
- **Fake runtime + seeding** already exist behind `//go:build e2e` (`e2e_install.go`,
  `internal/pkg/agentruntime/runtimes/fake/`), registered in `main()` after bootstrap.

So the diff is confined to: the new `e2e/` package, `Makefile`, `.github/workflows/ci.yml`,
`.gitignore`, `frontend/package.json` (cleanup), and the docs.

## Target architecture

```
make e2e  →  cd e2e && pnpm test  →  node run-e2e.mjs   (spawns playwright, reaps residue after)
  └─ playwright (workers:1, testDir ./tests)
       ├─ webServer: wails dev -tags e2e -devserver localhost:34216 > "$LOG" 2>&1
       │                 ├─ vite (frontend HMR)
       │                 └─ agentre app (-tags e2e) → real services → <tmp>/agentre-e2e-data/agentre.db
       │                       └─ agentruntime.RuntimeFor(claudecode) overridden by FAKE (deterministic echo)
       └─ chromium → http://localhost:34216  (Wails IPC bridge → real Go backend)
            └─ specs assert on the UI …
                 … and on the DB via a read-only node:sqlite query (independent oracle)
```

Env injected by `e2e/playwright.config.ts` (read by the app via existing wiring):

| Env | Effect |
|---|---|
| `AGENTRE_DATA_DIR=<tmp>/agentre-e2e-data` | DB/config/logs under a throwaway dir (highest-precedence override) |
| `AGENTRE_ENV=test` | quiet logger level (`bootstrap/cago.go appEnv()`) |

No master key / extensions env (those are opskat-specific). Dev-mode detection (and thus the
skipped single-instance lock) comes for free from `wails dev` setting the `devserver` env.

## New `e2e/` package (root-level, standalone)

```
e2e/
  package.json                 # @playwright/test + @types/node; scripts: setup/test/test:scratch
  tsconfig.json
  pnpm-lock.yaml               # committed
  run-e2e.mjs                  # cross-platform runner (thin): spawn playwright, reap residue after
  playwright.config.ts         # base: temp dir + env + dist prep, webServer on :34216
  playwright.scratch.config.ts # extends base, testDir ./scratch
  fixtures/db.ts               # read-only node:sqlite oracle (runningSessionCount, …)
  tests/
    smoke-chat.spec.ts         # committed core smoke chain (moved from frontend/e2e/) + DB assertion
  scratch/
    README.md                  # convention + starter template (committed; *.spec.ts gitignored)
```

`frontend/e2e/` (incl. `global-teardown.ts`) is **deleted**.

### `e2e/run-e2e.mjs` (thin, cross-platform)

Adapted from opskat verbatim where it applies. Because Playwright's `webServer` owns the
server lifecycle and group-kills it on teardown, the runner only does **post-exit** cleanup:

```js
// Cross-platform e2e runner: runs `playwright test` (forwarding extra args), then cleans up
// AFTER Playwright has fully exited (webServer torn down, app gone, db closed, vite orphaned).
// Why here and not globalTeardown/Makefile: see docs/e2e-harness-guide.md §7.
import { execFileSync, spawn } from "node:child_process";
import { createRequire } from "node:module";
import { rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));          // e2e/
const repoRoot = join(here, "..");
const dataDir = join(tmpdir(), "agentre-e2e-data");           // must match playwright.config.ts
const webserverLog = join(tmpdir(), "agentre-e2e-webserver.log");

const require = createRequire(import.meta.url);
const playwrightCli = require.resolve("@playwright/test/cli");

const child = spawn(process.execPath, [playwrightCli, "test", ...process.argv.slice(2)], {
  cwd: here, stdio: "inherit",
});
child.on("exit", (code) => { cleanup(code === 0); process.exit(code ?? 1); });

// Always reap the orphan vite (hygiene). On FAILURE keep the temp dir (agentre.db + logs) and
// the webserver log so you can inspect them / CI can upload them; the next run wipes the dir at
// start anyway (playwright.config main-runner rm+mkdir). On success, remove both.
function cleanup(passed) {
  reapOrphanVite();
  if (passed) {
    rmSync(dataDir, { recursive: true, force: true });
    rmSync(webserverLog, { force: true });
  }
}

// `wails dev` orphans its vite child on shutdown; reap by command line, scoped to THIS repo's
// frontend so a sibling checkout's vite (e.g. agentre-server / a real agentre) is never touched.
function reapOrphanVite() {
  const frontend = join(repoRoot, "frontend");
  try {
    if (process.platform === "win32") {
      const ps =
        "Get-CimInstance Win32_Process | Where-Object { " +
        `$_.ProcessId -ne $PID -and $_.CommandLine -like '*${frontend}*vite*' } | ` +
        "ForEach-Object { Stop-Process -Id $_.ProcessId -Force }";
      execFileSync("powershell", ["-NoProfile", "-NonInteractive", "-Command", ps], { stdio: "ignore" });
    } else {
      execFileSync("pkill", ["-f", `${frontend}.*vite`], { stdio: "ignore" });
    }
  } catch { /* best-effort hygiene */ }
}
```

### `e2e/playwright.config.ts` (base)

Key points (mirrors opskat, agentre-tuned): deterministic fixed `dataDir`; dir prep + env set
**only in the main runner** (`TEST_WORKER_INDEX === undefined`) so per-worker re-eval doesn't
diverge the oracle's path; `frontend/dist/.keep` created in Node (shell-agnostic for Windows);
`webServer` redirects output to a file and uses `stdout/stderr:"ignore"` (the hang fix);
readiness via `url` polling.

```ts
import { defineConfig, devices } from "@playwright/test";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const dataDir = join(tmpdir(), "agentre-e2e-data");
if (process.env.TEST_WORKER_INDEX === undefined) {
  rmSync(dataDir, { recursive: true, force: true });
  mkdirSync(dataDir, { recursive: true });
  const distDir = join(__dirname, "..", "frontend", "dist"); // wails //go:embed needs it
  mkdirSync(distDir, { recursive: true });
  writeFileSync(join(distDir, ".keep"), "");
}
process.env.AGENTRE_DATA_DIR = dataDir;
process.env.AGENTRE_ENV = "test";

const DEVSERVER = "localhost:34216";
const BASE_URL = `http://${DEVSERVER}`;
const WEBSERVER_LOG = join(tmpdir(), "agentre-e2e-webserver.log");

export default defineConfig({
  testDir: "./tests",
  timeout: 60_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  reporter: [["list"], ["html", { open: "never" }]],
  use: { baseURL: BASE_URL, trace: "retain-on-failure", screenshot: "only-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: `wails dev -tags e2e -devserver ${DEVSERVER} > "${WEBSERVER_LOG}" 2>&1`,
    cwd: "..",
    url: BASE_URL,
    reuseExistingServer: !process.env.CI,
    timeout: 240_000,
    stdout: "ignore",
    stderr: "ignore",
    env: { AGENTRE_DATA_DIR: dataDir, AGENTRE_ENV: "test" },
  },
});
```

### `e2e/playwright.scratch.config.ts`

```ts
import { defineConfig } from "@playwright/test";
import base from "./playwright.config";
// Throwaway specs from ./scratch against the SAME live harness; only testDir differs.
export default defineConfig({ ...base, testDir: "./scratch" });
```

### `e2e/fixtures/db.ts` (DB oracle)

```ts
import { DatabaseSync } from "node:sqlite";
import { tmpdir } from "node:os";
import { join } from "node:path";

const dbPath = () =>
  join(process.env.AGENTRE_DATA_DIR ?? join(tmpdir(), "agentre-e2e-data"), "agentre.db");

// Count chat_sessions stuck in agent_status='running'. Read-only, independent of the Go
// service layer — proves the real status write hit disk (guards "stuck running / lost status
// write"). After a finished turn this must be 0.
export function runningSessionCount(): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare("SELECT COUNT(*) AS n FROM chat_sessions WHERE agent_status = 'running'")
      .get() as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}
```

### `e2e/tests/smoke-chat.spec.ts`

The existing smoke chain (unchanged steps), **plus** a DB-level guard after the turn:

```ts
import { runningSessionCount } from "../fixtures/db";
// … existing steps: new session → send "ping" → see /e2e-fake-reply: ping/ → tab-spinner count 0 …

// DB twin of the spinner check: the source of truth has no stuck-running session.
await expect.poll(() => runningSessionCount(), { timeout: 15_000 }).toBe(0);
```

`expect.poll` because the DB write may lag the UI spinner by a tick.

### `e2e/package.json` / `tsconfig.json`

```json
{
  "name": "agentre-e2e",
  "private": true,
  "version": "0.0.0",
  "description": "Agentre GUI end-to-end tests (Playwright × Wails dev bridge, fake runtime)",
  "scripts": {
    "setup": "pnpm install && pnpm exec playwright install chromium",
    "test": "node run-e2e.mjs",
    "test:scratch": "node run-e2e.mjs -c playwright.scratch.config.ts"
  },
  "devDependencies": { "@playwright/test": "^1.60.0", "@types/node": "^22.0.0" }
}
```

`tsconfig.json` mirrors opskat (`ES2022` / `ESNext` / `bundler` / `types:["node"]` / strict /
`noEmit`).

## Wiring changes

### `Makefile`

```make
# E2E: Playwright 驱动真实 wails dev(-tags e2e fake runtime)跑 GUI 端到端。详见 docs/e2e-harness-guide.md。
# 一次性装依赖+浏览器:cd e2e && pnpm run setup(CI 在独立步骤装)。编排与清理都在 e2e/run-e2e.mjs(跨平台)。
e2e:
	cd e2e && pnpm test

# 临时功能验证:跑 e2e/scratch/ 里的一次性 spec(不提交)。
e2e-scratch:
	cd e2e && pnpm run test:scratch
```

- `.PHONY`: `e2e-serve` → `e2e-scratch`.
- **Removed:** the whole `trap`/`pkill`/`curl`-poll block, `E2E_SPEC`, `E2E_INCLUDE_LOCAL`,
  and the `e2e-serve` target (debug loop now uses `reuseExistingServer` — see doc).

### `.github/workflows/ci.yml` (the `E2E` job)

- `setup-node` `node-version: "24"` (`node:sqlite` is stable/unflagged; agentre local is Node 26).
- Install both lockfiles: keep `cd frontend && pnpm install --frozen-lockfile`; add
  `cd e2e && pnpm install --frozen-lockfile && pnpm exec playwright install --with-deps chromium`.
- Cache key includes both `frontend/pnpm-lock.yaml` and `e2e/pnpm-lock.yaml`.
- Run step stays `xvfb-run -a make e2e`.
- Artifacts on failure → `e2e/playwright-report`, `e2e/test-results`, `/tmp/agentre-e2e-webserver.log`.

(The existing job already installs the wails CLI + GTK/WebKit; reuse that.)

### `.gitignore` (repo root)

```
# e2e (Playwright)
e2e/node_modules/
e2e/test-results/
e2e/playwright-report/
e2e/.last-run.json
# 一次性功能验证脚本不提交,仅保留 README
e2e/scratch/*
!e2e/scratch/README.md
```

Remove the now-dead `frontend/playwright-report/` and `frontend/e2e/local/` lines.

### `frontend/package.json` (cleanup — consequence of the move)

- Remove `"@playwright/test": "^1.60.0"` from `devDependencies`.
- Remove the `"e2e": "playwright test -c e2e/playwright.config.ts"` script.
- Refresh `frontend/pnpm-lock.yaml` (`cd frontend && pnpm install`).

## Docs

Rename `docs/e2e-testing.md` → **`docs/e2e-harness-guide.md`** (parity with opskat + the
filename the user referenced) and rewrite into opskat's §-structure adapted to agentre's
fake-runtime harness:

1. Two modes (committed `e2e/tests/` vs throwaway `e2e/scratch/`)
2. Architecture (the diagram above; fake-runtime + build-tag seam)
3. Isolation & safety (AGENTRE_DATA_DIR, dev-mode lock skip, port 34216)
4. Running the committed suite (`pnpm run setup` / `make e2e`; Windows = `cd e2e && pnpm test`)
5. Writing a committed core-flow spec (`data-testid`, no sleeps, DB oracle, fake-output assertion)
6. Ad-hoc functional verification (`make e2e-scratch`)
7. Harness engineering — hard-won lessons (the §7 symptom→cause→fix table, agentre-tuned)
8. Extending the harness (new fake event via prompt directive; new DB oracle; new testid)
9. File map

Update references: `AGENTS.md:69` and `docs/doc-maintenance.md:45` (filename, `make e2e` /
`make e2e-scratch`, `e2e/scratch/`). Keep `2026-06-09-e2e-testing-design.md` as the prior
snapshot; point the living doc at this spec as the current design. (Read `docs/doc-maintenance.md`
before editing the contributor docs.)

## Decisions taken (flag for review — easy to veto)

- **Doc renamed** `e2e-testing.md` → `e2e-harness-guide.md` (vs. keeping the old name). Chosen
  for opskat parity + the filename you named.
- **`local/` → `scratch/`** throwaway-spec convention (opskat naming), with a dedicated
  `playwright.scratch.config.ts` + `make e2e-scratch`, replacing the `E2E_SPEC=local/…` toggle.
- **`make e2e-serve` dropped.** The debug loop now relies on `reuseExistingServer:!CI` (start a
  server once, re-run specs). Documented in the guide.
- **No `boot` spec** (per your earlier choice). The dedicated port `34216` +
  `reuseExistingServer:!CI` removes most false-green risk; the existing smoke spec's
  `new-chat-button` assertion is an implicit app-identity check.

## Verification plan

- **DB oracle (TDD).** Land `fixtures/db.ts` + the smoke assertion; prove it's meaningful by
  temporarily asserting `toBe(1)` (or pointing at a pre-turn state) and watching it fail, then
  revert to `toBe(0)` green. The runner/config are infra — verified observationally:
  - `make e2e` → exits **0**, native window closes, **no residual `wails`/`vite`** processes,
    temp dir + webserver log removed, your real `~/Library/Application Support/Agentre` untouched.
  - `make e2e-scratch` with a throwaway spec runs + cleans up; `e2e/scratch/*.spec.ts` stays
    gitignored.
  - `cd e2e && pnpm exec tsc --noEmit` clean; `make lint` / `cd frontend && pnpm lint` clean;
    `make test-backend` green (no backend change, but confirm nothing broke).
  - CI `E2E` job green on Linux (xvfb).
- **Windows reap branch** marked best-effort / by-inspection (same caveat as opskat) — CI is
  Linux-only.

## Out of scope

- Real claude-code/codex e2e (the fake-runtime seam stays).
- Group-chat / settings / multi-backend / codex / remote specs (future, reuse this harness).
- Any backend Go change (none required — see above).
