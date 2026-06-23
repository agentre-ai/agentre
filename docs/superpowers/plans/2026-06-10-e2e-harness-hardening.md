# E2E Harness Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restructure agentre's Playwright e2e harness into a root-level standalone `e2e/` package using opskat PR #173's proven design (Playwright `webServer` + file-redirect hang-fix + thin cross-platform runner + dedicated port + `node:sqlite` DB oracle), with no backend Go changes.

**Architecture:** Playwright owns the `wails dev -tags e2e -devserver localhost:34216` lifecycle via `webServer`; its stdout is redirected to a file (so the orphaned vite child can't hold the pipe open and hang teardown). A thin `e2e/run-e2e.mjs` spawns `playwright test` and, after it exits, reaps the orphan vite + temp dir (keeping them on failure). A read-only `node:sqlite` oracle asserts the real DB write path. The agent runtime stays faked behind the existing `//go:build e2e` seam.

**Tech Stack:** Node ≥22 (`node:sqlite`), Playwright `@playwright/test`, pnpm 10.33, wails v2 `dev -devserver`, Go (unchanged), Make.

**Reference:** spec `docs/superpowers/specs/2026-06-10-e2e-harness-hardening-design.md`; opskat source mirrored at `/tmp/opskat-e2e-harness-guide.md` + `/tmp/opskat-173.diff` (read-only references — do not copy opskat strings like `opskat`/`34216`-vs-port verbatim without the agentre swaps noted per task).

**Working branch:** `develop/group` (per user). Commit only the files each task names — keep the diff isolated.

> **Sandbox note:** `git` write ops in this environment may need the Bash tool's `dangerouslyDisableSandbox: true`. `cd` into a subdir inside a compound command can prompt; prefer running from repo root and passing paths, or `cd e2e && …` as a single command.

---

### Task 1: Update root `.gitignore` for the new `e2e/` package

Do this first so the later `pnpm install` never tempts a `node_modules` commit.

**Files:**
- Modify: `.gitignore`

- [ ] **Step 1: Replace the dead `frontend/e2e` ignore lines with `e2e/` ones**

Current lines (around 26-29):
```
# Playwright e2e
frontend/playwright-report/
# Throwaway feature-verification specs (run via `make e2e E2E_SPEC=...`); never committed.
frontend/e2e/local/
```

Replace that whole block with:
```
# e2e (Playwright) — standalone root package
e2e/node_modules/
e2e/test-results/
e2e/playwright-report/
e2e/.last-run.json
# 一次性功能验证脚本不提交(约定见 docs/e2e-harness-guide.md),仅保留 README
e2e/scratch/*
!e2e/scratch/README.md
```

(The general `node_modules/` and `frontend/dist/` lines elsewhere in the file stay.)

- [ ] **Step 2: Verify**

Run: `git check-ignore e2e/node_modules e2e/scratch/foo.spec.ts e2e/scratch/README.md; echo "---"; git status --short`
Expected: first two paths echo back (ignored), `e2e/scratch/README.md` does NOT (not ignored), and `git status` shows only the modified `.gitignore`.

- [ ] **Step 3: Commit**

```bash
git add .gitignore
git commit -m "🙈 gitignore: track e2e/ standalone package, drop frontend/e2e ignores"
```

---

### Task 2: Scaffold the standalone `e2e/` pnpm package

**Files:**
- Create: `e2e/package.json`
- Create: `e2e/tsconfig.json`
- Create (generated): `e2e/pnpm-lock.yaml`, `e2e/node_modules/` (ignored)

- [ ] **Step 1: Create `e2e/package.json`**

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
  "devDependencies": {
    "@playwright/test": "^1.60.0",
    "@types/node": "^22.0.0"
  }
}
```

- [ ] **Step 2: Create `e2e/tsconfig.json`**

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "types": ["node"],
    "strict": true,
    "noEmit": true,
    "skipLibCheck": true
  },
  "include": ["**/*.ts"]
}
```

- [ ] **Step 3: Install deps + Chromium**

Run: `cd e2e && pnpm install && pnpm exec playwright install chromium`
Expected: creates `e2e/pnpm-lock.yaml` + `e2e/node_modules/`; Chromium downloads (or "already installed").

- [ ] **Step 4: Verify the package resolves**

Run: `cd e2e && pnpm exec tsc --noEmit`
Expected: exits 0 (no `.ts` files yet → trivially clean).
Run: `git status --short e2e`
Expected: `e2e/package.json`, `e2e/tsconfig.json`, `e2e/pnpm-lock.yaml` shown; `e2e/node_modules/` NOT shown (ignored).

- [ ] **Step 5: Commit**

```bash
git add e2e/package.json e2e/tsconfig.json e2e/pnpm-lock.yaml
git commit -m "✨ e2e: scaffold standalone Playwright package"
```

---

### Task 3: Add the cross-platform runner + Playwright configs

**Files:**
- Create: `e2e/run-e2e.mjs`
- Create: `e2e/playwright.config.ts`
- Create: `e2e/playwright.scratch.config.ts`

- [ ] **Step 1: Create `e2e/run-e2e.mjs`**

```js
// Cross-platform e2e runner: runs `playwright test` (forwarding extra args), then cleans up
// AFTER Playwright has fully exited (webServer torn down, app gone, db closed, vite orphaned).
//
// Why a Node wrapper instead of cleaning up elsewhere — see docs/e2e-harness-guide.md §7:
//   - globalTeardown runs while Playwright still MANAGES the webServer → killing there
//     SIGTERMs the live server (exit 143).
//   - a Makefile `pkill -f "wails dev …"` self-matches the recipe shell's own /proc/<pid>/cmdline
//     on Linux and SIGTERMs make. The runner's cmdline is `node run-e2e.mjs`, so it's safe.
import { execFileSync, spawn } from "node:child_process";
import { createRequire } from "node:module";
import { rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url)); // e2e/
const repoRoot = join(here, "..");
// These must match the paths in playwright.config.ts.
const dataDir = join(tmpdir(), "agentre-e2e-data");
const webserverLog = join(tmpdir(), "agentre-e2e-webserver.log");

const require = createRequire(import.meta.url);
const playwrightCli = require.resolve("@playwright/test/cli");

const child = spawn(
  process.execPath,
  [playwrightCli, "test", ...process.argv.slice(2)],
  { cwd: here, stdio: "inherit" },
);

child.on("exit", (code) => {
  cleanup(code === 0);
  // Mirror the child's outcome; a signal-killed run (code === null) counts as failure.
  process.exit(code ?? 1);
});

// Always reap the orphan vite (hygiene). On FAILURE keep the temp data dir (agentre.db + logs)
// and the webserver log so you / CI can inspect them; the next run wipes the dir at start
// anyway (playwright.config main-runner rm+mkdir). On success, remove both.
function cleanup(passed) {
  reapOrphanVite();
  if (passed) {
    rmSync(dataDir, { recursive: true, force: true });
    rmSync(webserverLog, { force: true });
  }
}

// `wails dev` orphans its vite child on shutdown (a separate process group on Unix), which
// Playwright's group-kill misses. Reap by command line, scoped to THIS repo's frontend so a
// sibling checkout's vite (e.g. agentre-server) is never touched. Best-effort.
function reapOrphanVite() {
  const frontend = join(repoRoot, "frontend");
  try {
    if (process.platform === "win32") {
      // No pkill on Windows; match via CIM and force-kill. `-ne $PID` excludes THIS PowerShell
      // (its own command line contains the pattern), or we'd recreate the self-kill we avoid.
      const ps =
        "Get-CimInstance Win32_Process | Where-Object { " +
        `$_.ProcessId -ne $PID -and $_.CommandLine -like '*${frontend}*vite*' } | ` +
        "ForEach-Object { Stop-Process -Id $_.ProcessId -Force }";
      execFileSync("powershell", ["-NoProfile", "-NonInteractive", "-Command", ps], {
        stdio: "ignore",
      });
    } else {
      execFileSync("pkill", ["-f", `${frontend}.*vite`], { stdio: "ignore" });
    }
  } catch {
    // best-effort hygiene; nothing to reap.
  }
}
```

- [ ] **Step 2: Create `e2e/playwright.config.ts`**

```ts
import { defineConfig, devices } from "@playwright/test";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// Deterministic data dir so every config re-eval resolves the SAME path: Playwright loads this
// config in the main runner AND in each worker, so a random mkdtemp would yield a different dir
// per process — the db-oracle worker would then read a file the app (launched by the main
// process) never wrote.
const dataDir = join(tmpdir(), "agentre-e2e-data");

// Only the main runner (TEST_WORKER_INDEX undefined), not workers, prepares a fresh dir — and it
// runs before the webServer launches. Workers reuse the same path to read the db the app wrote.
if (process.env.TEST_WORKER_INDEX === undefined) {
  rmSync(dataDir, { recursive: true, force: true });
  mkdirSync(dataDir, { recursive: true });
  // `wails dev` needs frontend/dist to exist for the //go:embed (mirrors `make dev`). Done here
  // in Node — not via shell `mkdir -p`/`touch` — so the webServer command stays shell-agnostic
  // and runs on native Windows (cmd) too.
  const distDir = join(__dirname, "..", "frontend", "dist");
  mkdirSync(distDir, { recursive: true });
  writeFileSync(join(distDir, ".keep"), "");
}

process.env.AGENTRE_DATA_DIR = dataDir;
process.env.AGENTRE_ENV = "test";

// Dedicated wails dev server port for e2e (avoids the default 34115 → no collision/false-green
// against a real `make dev`).
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
  use: {
    baseURL: BASE_URL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    // -tags e2e compiles the fake runtime + seeding; -devserver binds the IPC bridge to our
    // dedicated port. Output → a file (not Playwright's pipe): wails dev orphans its vite child
    // on shutdown, and a piped stdout the orphan keeps open would stop teardown from ever
    // finishing (hang). Readiness is detected via `url` polling, not stdout. `> "file" 2>&1` is
    // valid in both POSIX sh and Windows cmd.
    command: `wails dev -tags e2e -devserver ${DEVSERVER} > "${WEBSERVER_LOG}" 2>&1`,
    cwd: "..",
    url: BASE_URL,
    reuseExistingServer: !process.env.CI,
    timeout: 240_000,
    stdout: "ignore",
    stderr: "ignore",
    env: {
      AGENTRE_DATA_DIR: dataDir,
      AGENTRE_ENV: "test",
    },
  },
});
```

- [ ] **Step 3: Create `e2e/playwright.scratch.config.ts`**

```ts
import { defineConfig } from "@playwright/test";
import base from "./playwright.config";

// Runs throwaway functional-verification specs from ./scratch against the SAME live harness
// (webServer / env injection / isolation / teardown) as the committed suite — only the test
// directory differs. Importing the base config also runs its module-top-level setup (fresh temp
// data dir + env). Usage/convention: docs/e2e-harness-guide.md §6 + e2e/scratch/README.md.
export default defineConfig({ ...base, testDir: "./scratch" });
```

- [ ] **Step 4: Verify syntax/types**

Run: `cd e2e && node --check run-e2e.mjs && pnpm exec tsc --noEmit`
Expected: both exit 0. (`tsc` sees the two config `.ts` files; they type-check. No `tests/` dir yet → Playwright not invoked here.)

- [ ] **Step 5: Commit**

```bash
git add e2e/run-e2e.mjs e2e/playwright.config.ts e2e/playwright.scratch.config.ts
git commit -m "✨ e2e: cross-platform runner + webServer config on :34216"
```

---

### Task 4: Move the smoke spec into `e2e/tests/`, delete the old `frontend/e2e/`

**Files:**
- Move: `frontend/e2e/smoke-chat.spec.ts` → `e2e/tests/smoke-chat.spec.ts`
- Delete: `frontend/e2e/playwright.config.ts`, `frontend/e2e/global-teardown.ts`
- Delete dir: `frontend/e2e/` (after the move; `local/` is gitignored/empty)

- [ ] **Step 1: Move the spec with git**

Run:
```bash
mkdir -p e2e/tests
git mv frontend/e2e/smoke-chat.spec.ts e2e/tests/smoke-chat.spec.ts
git rm frontend/e2e/playwright.config.ts frontend/e2e/global-teardown.ts
```
Expected: the spec is staged as a rename; the two configs staged as deletions.

- [ ] **Step 2: Confirm the moved spec needs no path edits**

The spec imports only `@playwright/test` and uses `page.goto("/")` + `data-testid` locators (no relative imports yet). Open `e2e/tests/smoke-chat.spec.ts` and confirm there are no `../` imports or hardcoded ports. (DB-oracle import is added in Task 6, not now.) No edit expected.

- [ ] **Step 3: Verify the old dir is gone and types still pass**

Run: `ls frontend/e2e 2>/dev/null; echo "exit=$?"; cd e2e && pnpm exec tsc --noEmit`
Expected: `frontend/e2e` is gone (or only an empty/ignored `local/`), `tsc` exits 0.

- [ ] **Step 4: Commit**

```bash
git add -A frontend/e2e e2e/tests
git commit -m "♻️ e2e: move smoke-chat spec to e2e/tests, drop frontend/e2e"
```

---

### Task 5: Wire the Makefile + first green end-to-end run (integration checkpoint)

**Files:**
- Modify: `Makefile` (the `.PHONY` line + the `e2e-serve`/`e2e` block, ~lines 1, 123-149)

- [ ] **Step 1: Update `.PHONY`**

In the `.PHONY:` line (line 1), replace `e2e e2e-serve` with `e2e e2e-scratch`.

- [ ] **Step 2: Replace the `e2e-serve` + `e2e` targets**

Delete the entire current block (the `e2e-serve:` comment+recipe at ~123-130 and the `e2e:` comment+recipe at ~132-149) and replace with:

```make
# E2E:Playwright 驱动真实 wails dev(-tags e2e 的确定性 fake runtime)跑 GUI 端到端。
# 详见 docs/e2e-harness-guide.md。一次性装依赖+浏览器:cd e2e && pnpm run setup(CI 在独立
# 步骤装,故这里不重复)。编排与收尾清理(回收残留 vite、删临时目录)都在 e2e/run-e2e.mjs
# 里用 Node 跨平台完成;配方只做 shell 无关的 cd && pnpm,cmd/sh 皆可。
e2e:
	cd e2e && pnpm test

# 临时功能验证:跑 e2e/scratch/ 里的一次性 spec(不提交)。约定/用法见 docs/e2e-harness-guide.md。
e2e-scratch:
	cd e2e && pnpm run test:scratch
```

- [ ] **Step 3: Verify the recipe expands**

Run: `make -n e2e`
Expected: prints `cd e2e && pnpm test` (no `trap`/`pkill`/`curl`).

- [ ] **Step 4: First green end-to-end run (the real integration check)**

Run: `make e2e`
Expected: builds the `-tags e2e` backend + vite (first run takes minutes), a native Agentre window opens, Playwright drives `:34216`, the smoke spec passes, the window closes, the command **exits 0**.

- [ ] **Step 5: Verify clean teardown + isolation**

Run (immediately after): `pgrep -fl "wails dev -tags e2e"; pgrep -fl "$(pwd)/frontend.*vite"; ls "$TMPDIR/agentre-e2e-data" 2>/dev/null; echo "exit=$?"`
Expected: no `wails`/`vite` processes left; the temp data dir is gone (removed on success). Your real `~/Library/Application Support/Agentre` is untouched.

- [ ] **Step 6: Commit**

```bash
git add Makefile
git commit -m "✨ e2e: make e2e/e2e-scratch run via e2e/ package (drop trap/pkill orchestration)"
```

---

### Task 6: DB oracle (TDD) + DB-level smoke assertion

This is the one TDD task. We add the assertion first (red because the fixture doesn't exist), then the fixture (green), then prove the assertion is meaningful.

**Files:**
- Create: `e2e/fixtures/db.ts`
- Modify: `e2e/tests/smoke-chat.spec.ts`

- [ ] **Step 1: Add the failing DB assertion to the smoke spec**

At the top of `e2e/tests/smoke-chat.spec.ts`, add the import under the existing `@playwright/test` import:
```ts
import { runningSessionCount } from "../fixtures/db";
```
At the end of the `test(...)` body, after the existing `tab-spinner` `toHaveCount(0)` assertion, add:
```ts
  // DB-level twin of the spinner check: the source of truth (chat_sessions.agent_status) has no
  // session stuck in "running" — a direct regression guard for "stuck running / lost status write".
  await expect.poll(() => runningSessionCount(), { timeout: 15_000 }).toBe(0);
```

- [ ] **Step 2: Run and watch it fail for the right reason**

Run: `make e2e`
Expected: FAIL — Playwright errors importing `../fixtures/db` (module not found / cannot resolve `runningSessionCount`). This confirms the assertion is wired before the fixture exists.

- [ ] **Step 3: Create the fixture**

`e2e/fixtures/db.ts`:
```ts
import { DatabaseSync } from "node:sqlite";
import { tmpdir } from "node:os";
import { join } from "node:path";

// The e2e temp DB. AGENTRE_DATA_DIR is set by playwright.config.ts (in every process, incl.
// workers); fall back to the same deterministic path for safety.
const dbPath = () =>
  join(process.env.AGENTRE_DATA_DIR ?? join(tmpdir(), "agentre-e2e-data"), "agentre.db");

// Count chat_sessions stuck in agent_status='running'. Read-only, independent of the Go service
// layer — proves the real status write hit disk. After a finished turn this must be 0.
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

- [ ] **Step 4: Run and watch it pass**

Run: `make e2e`
Expected: PASS — the smoke chain completes and `runningSessionCount()` polls to `0`.
(If `node:sqlite` warns "ExperimentalWarning", that's benign on the local Node; ignore.)

- [ ] **Step 5: Prove the assertion is meaningful**

Temporarily change the assertion to `.toBe(1)`, run `make e2e`, and confirm it now FAILS with "Expected: 1, Received: 0" (proving the oracle really reads the DB and the value is a real 0, not a vacuous pass). Then revert to `.toBe(0)` and re-run → PASS.

- [ ] **Step 6: Type-check + commit**

Run: `cd e2e && pnpm exec tsc --noEmit`
Expected: exits 0.
```bash
git add e2e/fixtures/db.ts e2e/tests/smoke-chat.spec.ts
git commit -m "✨ e2e: node:sqlite DB oracle + smoke asserts no stuck-running session"
```

---

### Task 7: Scratch (throwaway) spec convention

**Files:**
- Create: `e2e/scratch/README.md`

- [ ] **Step 1: Create `e2e/scratch/README.md`**

```markdown
# e2e/scratch — throwaway functional-verification specs

Drop one-off `*.spec.ts` here to verify a feature you just finished, end-to-end, against the
**real running app**. Everything in this folder **except this README is gitignored** — these
scripts are not committed; write, run, observe, delete.

This is the GUI counterpart of "verify by observing": drive the real app, then read the
observable side-effects (UI assertions + the temp DB + logs). Full workflow, conventions, and
gotchas: **[docs/e2e-harness-guide.md](../../docs/e2e-harness-guide.md)** (§6).

## Run

```bash
make e2e-scratch             # runs every e2e/scratch/*.spec.ts via the live harness
# or a single file (still through the runner, so cleanup happens):
cd e2e && pnpm run test:scratch scratch/<file>.spec.ts
```

Reuses the same harness as the committed suite: launches `wails dev -tags e2e` on port 34216
with a temp data dir + `AGENTRE_ENV=test`. A native Agentre window opens (expected). webServer
output → `$TMPDIR/agentre-e2e-webserver.log`; the app's logs + `agentre.db` are under the temp
data dir (`$AGENTRE_DATA_DIR`).

## Starter template

```ts
// e2e/scratch/verify-my-feature.spec.ts  (gitignored — delete when done)
import { test, expect } from "@playwright/test";
import { runningSessionCount } from "../fixtures/db"; // read-only node:sqlite oracle; add helpers as needed

test("my feature works end-to-end", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("new-chat-button")).toBeVisible();

  // 1. Drive the UI like a user (data-testid locators + auto-wait, no sleeps). Add a data-testid
  //    in the component if you need a stable hook (additive only).
  // 2. Assert the UI reflects the change (and the fake reply: /e2e-fake-reply: <prompt>/).
  // 3. Corroborate the side-effect independently — e.g. no session stuck running:
  // await expect.poll(() => runningSessionCount()).toBe(0);
});
```

If a flow proves to be **core and stable**, promote it: move the spec into `e2e/tests/`,
harden it, and commit (see the harness guide §5). Otherwise just delete it.
```

- [ ] **Step 2: Verify the scratch path works end-to-end**

Run:
```bash
cat > e2e/scratch/verify-smoke.spec.ts <<'EOF'
import { test, expect } from "@playwright/test";
test("scratch path runs", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("new-chat-button")).toBeVisible();
});
EOF
make e2e-scratch
```
Expected: the scratch spec runs against the live harness and PASSES; clean teardown.

- [ ] **Step 3: Confirm scratch specs are gitignored, then delete the throwaway**

Run: `git status --short e2e/scratch; git check-ignore e2e/scratch/verify-smoke.spec.ts; rm e2e/scratch/verify-smoke.spec.ts`
Expected: only `e2e/scratch/README.md` shows as untracked/new; `verify-smoke.spec.ts` is ignored (echoed by `check-ignore`), then removed.

- [ ] **Step 4: Commit**

```bash
git add e2e/scratch/README.md
git commit -m "📝 e2e: scratch (throwaway verification) convention + starter"
```

---

### Task 8: Remove the now-dead Playwright wiring from `frontend/`

**Files:**
- Modify: `frontend/package.json`
- Modify (generated): `frontend/pnpm-lock.yaml`

- [ ] **Step 1: Remove the dep + script**

In `frontend/package.json`:
- Delete the `"e2e": "playwright test -c e2e/playwright.config.ts"` line from `scripts`.
- Delete the `"@playwright/test": "^1.60.0",` line from `devDependencies`.

- [ ] **Step 2: Refresh the lockfile**

Run: `cd frontend && pnpm install`
Expected: updates `frontend/pnpm-lock.yaml`, removing `@playwright/test`.

- [ ] **Step 3: Verify frontend still lints + unit-tests**

Run: `make test-frontend` (or `cd frontend && pnpm test`)
Expected: Vitest runs green (no spec depended on `@playwright/test`).
Run: `cd frontend && pnpm lint`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add frontend/package.json frontend/pnpm-lock.yaml
git commit -m "🔥 frontend: drop @playwright/test (moved to e2e/ package)"
```

---

### Task 9: Update CI `E2E` job

**Files:**
- Modify: `.github/workflows/ci.yml` (the `e2e:` job, ~lines 86-129)

- [ ] **Step 1: Read the current job to anchor the edits**

Run: `sed -n '86,130p' .github/workflows/ci.yml`
Note the current step names/order (checkout, setup-go, setup-node, install Linux deps + xvfb, install Playwright Chromium under `frontend`, run `xvfb-run -a make e2e`, upload artifacts).

- [ ] **Step 2: Apply these changes to the `e2e:` job**

1. In `setup-node`, set `node-version: "24"` (was implicit/older — `node:sqlite` is stable/unflagged on 24; agentre local is 26) and make the pnpm cache key cover both lockfiles:
   ```yaml
         with:
           node-version: "24"
           cache: "pnpm"
           cache-dependency-path: |
             frontend/pnpm-lock.yaml
             e2e/pnpm-lock.yaml
   ```
2. Ensure frontend deps are installed (vite needs them for `wails dev`): keep/add
   ```yaml
       - name: Install frontend dependencies
         run: cd frontend && pnpm install --frozen-lockfile
   ```
3. Replace the old `cd frontend && pnpm exec playwright install …` step with an e2e-package install step:
   ```yaml
       - name: Install e2e dependencies
         run: cd e2e && pnpm install --frozen-lockfile && pnpm exec playwright install --with-deps chromium
   ```
4. Keep the run step `run: xvfb-run -a make e2e`.
5. Update the artifacts step to the new paths:
   ```yaml
       - name: Upload e2e artifacts
         if: failure()
         uses: actions/upload-artifact@v4
         with:
           name: e2e-artifacts
           path: |
             e2e/playwright-report
             e2e/test-results
             /tmp/agentre-e2e-webserver.log
           if-no-files-found: ignore
   ```
   (Keep whatever `upload-artifact` major version the repo already pins; only change the `path:` list and add `if-no-files-found: ignore`.)

- [ ] **Step 3: Verify the YAML parses**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml')); print('ok')"`
Expected: `ok`. (If `actionlint` is installed, run it too.)

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "👷 ci: e2e job installs e2e/ deps, Node 24, artifacts under e2e/"
```

---

### Task 10: Rewrite + rename the harness doc; fix references

**Files:**
- Read first: `docs/doc-maintenance.md` (required before editing contributor docs)
- Rename + rewrite: `docs/e2e-testing.md` → `docs/e2e-harness-guide.md`
- Modify: `AGENTS.md` (line ~69)
- Modify: `docs/doc-maintenance.md` (table row ~45)

- [ ] **Step 1: Read the doc-maintenance rules**

Run: `sed -n '1,80p' docs/doc-maintenance.md`
Follow its git-aware fact-checking + "fix/delete stale facts directly, no deprecation comments" rules while rewriting.

- [ ] **Step 2: Rename the file**

Run: `git mv docs/e2e-testing.md docs/e2e-harness-guide.md`

- [ ] **Step 3: Rewrite `docs/e2e-harness-guide.md`**

Replace its contents with a guide structured like opskat's (reference: `/tmp/opskat-e2e-harness-guide.md`), swapping every opskat fact for the agentre fact. The doc must contain these sections and facts (prose; keep it the living reference):

1. **Header / intro** — Playwright drives the **real React → Wails IPC → real Go services → real SQLite**; the one faked piece is the **agent runtime** (deterministic echo, `//go:build e2e`). Owns the GUI-e2e harness.
2. **Two modes** — committed core suite `e2e/tests/*.spec.ts` (`make e2e`) vs throwaway `e2e/scratch/*.spec.ts` (`make e2e-scratch`, gitignored). High bar for committed specs.
3. **Architecture** — the diagram from the spec (`make e2e → cd e2e && pnpm test → node run-e2e.mjs → playwright(webServer: wails dev -tags e2e -devserver :34216) → chromium`); env table: `AGENTRE_DATA_DIR=<tmp>/agentre-e2e-data`, `AGENTRE_ENV=test`; note the fake runtime overrides the claudecode registry slot.
4. **Isolation & safety** — data/DB/logs under `$AGENTRE_DATA_DIR` (highest-precedence override), removed on success by `run-e2e.mjs` (kept on failure for debugging); single-instance lock auto-skipped in dev mode + data-dir-scoped → coexists with a real Agentre; dedicated port 34216 avoids the default-34115 collision; **no `node:sqlite` write — read-only oracle**.
5. **Writing a committed core-flow spec** — `data-testid` selectors (list the smoke spec's: `new-chat-button`, `new-agent-chat-item`, `agent-picker-item-<id>`, `[role=tab][data-active=true]`, `.ProseMirror`, `tab-spinner`), no sleeps (auto-wait), assert on the fake output (`/e2e-fake-reply: <prompt>/`), corroborate via the DB oracle (`runningSessionCount`), single-worker determinism.
6. **Ad-hoc functional verification** — the `e2e/scratch/` workflow (write → `make e2e-scratch` → observe UI + `$AGENTRE_DATA_DIR/agentre.db` + logs + traces → delete; promote to `e2e/tests/` only if core).
7. **Harness engineering — hard-won lessons (symptom → cause → fix)** — adapt opskat's §7 table to the facts that apply to agentre:
   - *Suite hangs after green* → orphan vite holds the **piped** webServer stdout → fix: `stdout/stderr:"ignore"` + `> "$LOG" 2>&1` + `url` polling.
   - *exit 143 / make Terminated* → cleanup in globalTeardown SIGTERMs the still-managed server; a Makefile `pkill -f "wails dev …"` self-matches the recipe shell on Linux → fix: all cleanup in `run-e2e.mjs` after Playwright exits (cmdline is `node run-e2e.mjs`, no self-match), cross-platform.
   - *DB oracle reads a dir the app never wrote* → config re-eval per worker + random dir → fix: deterministic fixed dir, prepared only in the main runner (`TEST_WORKER_INDEX === undefined`).
   - *False-green against the wrong app* → default port 34115 + reuse → fix: dedicated 34216 + `reuseExistingServer:!CI`.
   - State plainly: **no backend Go changes were needed** (lock already dev-skipped + data-dir-scoped; `AGENTRE_DATA_DIR` already isolates) — contrast opskat which needed `OPSKAT_E2E`/`ResolvedDataDir`.
8. **Extending the harness** — fake a new event by branching the fake runtime on a prompt prefix (the intended-but-unimplemented `@e2e:` seam — keep the existing doc's wording); add a new `data-testid` (additive); add a new read-only oracle to `e2e/fixtures/db.ts`; fake another backend = add a fake package under `//go:build e2e` + one `RegisterRuntime` line in `e2e_install.go`.
9. **File map** — table of `e2e/` files + roles + committed? (run-e2e.mjs, playwright.config.ts, playwright.scratch.config.ts, fixtures/db.ts, tests/*.spec.ts, scratch/*.spec.ts [gitignored], scratch/README.md, package.json scripts, Makefile `e2e`/`e2e-scratch`).
10. **Scope & CI** — committed suite runs in `.github/workflows/ci.yml` `E2E` job on `pull_request` + pushes to `main`/`develop/*`, ubuntu + xvfb + `make e2e`; claudecode-only; group/settings/codex/remote are future specs reusing this harness.
11. Point the "design rationale" line at `docs/superpowers/specs/2026-06-10-e2e-harness-hardening-design.md` (current) and keep `2026-06-09-e2e-testing-design.md` named as the prior snapshot.

- [ ] **Step 4: Fix the two references**

In `AGENTS.md` (~line 69), update the bullet: `docs/e2e-testing.md` → `docs/e2e-harness-guide.md`; `make e2e` / `make e2e-serve` → `make e2e` / `make e2e-scratch`; `frontend/e2e/local/` → `e2e/scratch/`.
In `docs/doc-maintenance.md` (~line 45 table row), the same filename + `make e2e`/`make e2e-scratch` + `e2e/scratch/` swaps.

- [ ] **Step 5: Verify no dangling references remain**

Run: `grep -rn "e2e-testing.md\|frontend/e2e\|make e2e-serve\|E2E_SPEC\|:34115" --include="*.md" . | grep -v node_modules | grep -v superpowers/specs`
Expected: no hits (the only allowed historical mentions live inside the archived spec files under `docs/superpowers/specs/`, which the `grep -v` excludes).

- [ ] **Step 6: Commit**

```bash
git add docs/e2e-harness-guide.md AGENTS.md docs/doc-maintenance.md
git commit -m "📝 docs: rewrite e2e harness guide for e2e/ package + DB oracle (rename from e2e-testing.md)"
```

---

## Final verification (after all tasks)

- [ ] `make e2e` → green, exits 0, no residual `wails`/`vite`, temp dir removed, real data dir untouched.
- [ ] `make e2e-scratch` with a throwaway spec → green + cleanup; `e2e/scratch/*.spec.ts` stays gitignored.
- [ ] `cd e2e && pnpm exec tsc --noEmit` → clean.
- [ ] `make test-backend` → green (sanity: no backend change broke anything).
- [ ] `cd frontend && pnpm lint && pnpm test` → clean/green.
- [ ] `make lint` (Go) → clean.
- [ ] CI `E2E` job green on the PR (Linux/xvfb) — the real cross-platform-on-Linux proof.
- [ ] Windows reap branch: by-inspection only (CI is Linux); marked best-effort.

## Notes / out of scope

- No backend Go changes (verified in the spec): single-instance lock is dev-skipped + data-dir-scoped; `AGENTRE_DATA_DIR` isolates DB/config/logs; the only unix socket is the separate `agentred` daemon binary.
- No `boot` spec (per user); the dedicated port + `reuseExistingServer:!CI` + the smoke spec's `new-chat-button` assertion cover the false-green risk.
- Real claude-code/codex e2e, and group/settings/multi-backend specs, remain future work on this same harness.
