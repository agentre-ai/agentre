import { defineConfig, devices } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { launchEnv, prepareDirs, resolveTarget } from "./lib/target.mjs";

// The suite runs on the `fake` verification target, so `make verify-up` and this config resolve
// to the same app: one already-running app can serve both. lib/target.mjs derives every path and
// port from THIS checkout, which is what lets two worktrees run at once — and derives them
// deterministically, because Playwright loads this config in the main runner AND in each worker
// (a random mkdtemp would give the db-oracle worker a different dir than the app ever wrote to).
const target = resolveTarget("fake");
const { logFile: WEBSERVER_LOG } = target;

// Reuse (fast inner loop) mode: AGENTRE_E2E_REUSE=1 — run against the app `make verify-up`
// already has up, and keep the data dir that app owns (no wipe, no rebuild, no app restart
// between iterations). Default (unset) stays hermetic: fresh data dir + Playwright manages its
// own webServer. The runner (run-e2e.mjs) reads the same flag to fail fast when no server is up
// and to skip teardown. See e2e/README.md §4.
const reuseExisting = process.env.AGENTRE_E2E_REUSE === "1";

// Only the main runner (TEST_WORKER_INDEX undefined), not workers, prepares a fresh dir — and it
// runs before the webServer launches. Workers reuse the same path to read the db the app wrote.
// In reuse mode the caller owns the data dir and the running app: never wipe it out from under
// the server, or the DB oracle and the app diverge.
if (process.env.TEST_WORKER_INDEX === undefined && !reuseExisting) {
  prepareDirs(target, { wipe: true });
}
// `wails dev` needs frontend/dist to exist for the //go:embed (mirrors `make dev`). Done here
// in Node — not via shell `mkdir -p`/`touch` — so the webServer command stays shell-agnostic
// and runs on native Windows (cmd) too. Idempotent, so it stays unconditional in both modes.
const distDir = join(dirname(fileURLToPath(import.meta.url)), "..", "frontend", "dist");
mkdirSync(distDir, { recursive: true });
writeFileSync(join(distDir, ".keep"), "");

Object.assign(process.env, launchEnv(target));

// This checkout's own bridge port (never `wails dev`'s default 34115, never another worktree's)
// → no collision and no false-green against some other running app.
const DEVSERVER = `localhost:${target.devserverPort}`;
const BASE_URL = target.baseURL;

export default defineConfig({
  testDir: "./tests",
  timeout: 60_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  // HTML report only in CI — a passing local poke shouldn't leave a multi-hundred-KB
  // playwright-report/ behind. Local runs use the list reporter; failures still retain traces +
  // screenshots (use.trace / use.screenshot below). Force a local HTML report with `CI=1`.
  reporter: process.env.CI
    ? [
        ["list"],
        ["html", { open: "never" }],
      ]
    : [["list"]],
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
    // Reuse mode forces reuse; otherwise reuse a dev server already on this checkout's port
    // (never in CI, which must get a fresh hermetic server each job).
    reuseExistingServer: reuseExisting || !process.env.CI,
    timeout: 240_000,
    stdout: "ignore",
    stderr: "ignore",
    // Exactly the overrides the launcher injects — data dir, AGENTRE_ENV=test, the isolated
    // keychain dir, and AGENTRE_PROXY_PORT=0 so the local HTTP gateway binds a free port instead
    // of the fixed 52401 a running real Agentre already holds (without it BaseURL() is empty and
    // every gateway round-trip silently dies).
    env: launchEnv(target),
  },
});
