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
      // Bind the local HTTP gateway to an OS-chosen free port (0) instead of the fixed default
      // 52401. A running real Agentre already holds 52401, so without this the e2e gateway fails
      // to bind → BaseURL() empty → group_send (and any gateway round-trip) silently dies. Keeps
      // "a running Agentre does not interfere" true for the gateway too, not just the data dir.
      AGENTRE_PROXY_PORT: "0",
    },
  },
});
