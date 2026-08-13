// Cross-platform e2e runner: runs `playwright test` (forwarding extra args), then cleans up
// AFTER Playwright has fully exited (webServer torn down, app gone, db closed, vite orphaned).
//
// Why a Node wrapper instead of cleaning up elsewhere — see e2e/README.md §7:
//   - globalTeardown runs while Playwright still MANAGES the webServer → killing there
//     SIGTERMs the live server (exit 143).
//   - a Makefile `pkill -f "wails dev …"` self-matches the recipe shell's own /proc/<pid>/cmdline
//     on Linux and SIGTERMs make. The runner's cmdline is `node run-e2e.mjs`, so it's safe.
import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import { existsSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { blocksFreshRun, liveSessions, portListening, resolveTarget } from "./lib/target.mjs";
import { reapOrphanVite } from "./lib/procs.mjs";

const here = dirname(fileURLToPath(import.meta.url)); // e2e/
const repoRoot = join(here, "..");
// The committed suite runs on the `fake` verification target, so one already-running
// `make verify-up` app can serve both (see lib/target.mjs). These must stay equal to the paths
// playwright.config.ts hardcodes — pinned by lib/target.test.mjs.
const { dataDir, keychainDir, logFile: webserverLog, devserverPort: DEVSERVER_PORT } =
  resolveTarget("fake");

const require = createRequire(import.meta.url);
const playwrightCli = require.resolve("@playwright/test/cli");

// Keep-alive (fast inner loop) mode — see e2e/README.md §4. The caller owns a hand-started
// `wails dev -tags e2e` on :34216, so this runner must NOT tear anything down (no orphan-vite
// reap, no temp-dir removal) or the next iteration's reuse breaks. playwright.config.ts reads the
// same flag to skip the data-dir wipe and force reuseExistingServer.
const reuseExisting = process.env.AGENTRE_E2E_REUSE === "1";

async function main() {
  // The isolation guards are the one thing every mode depends on; run them before anything is
  // launched so a broken contract fails here rather than half-way through a suite.
  const guards = spawn(process.execPath, ["--test", "lib/*.test.mjs"], {
    cwd: here,
    stdio: "inherit",
  });
  const guardsCode = await new Promise((res) => guards.on("exit", res));
  if (guardsCode !== 0) {
    console.error("isolation guard tests failed — refusing to launch an app.");
    process.exit(guardsCode ?? 1);
  }

  // Reuse mode is an explicit "I started the server" contract: fail fast with instructions
  // instead of letting Playwright quietly start a fresh app against a stale data dir. Two
  // conditions must hold: :34216 is listening AND it is serving the e2e temp data dir (the
  // agentre.db file is where a correctly-overridden server writes it). A server up on the real
  // data dir (AGENTRE_DATA_DIR not set) would silently seed the user's real DB — reject it.
  // A fresh run wipes and re-seeds `dataDir` before launching and deletes it after passing. With
  // a `make verify-up` app living on that same dir, that is silent destruction of a running app
  // — refuse, and name the two ways out.
  const blocked = blocksFreshRun(liveSessions(), { reuse: reuseExisting });
  if (blocked.length > 0) {
    console.error(
      `a verification app is up on ${dataDir} (flavor ${blocked.map((s) => s.flavor).join(", ")}).\n` +
        "A fresh suite run would wipe the data dir underneath it. Either:\n" +
        "  make verify-down            # stop it, then re-run\n" +
        "  AGENTRE_E2E_REUSE=1 …       # run against it instead (no wipe, no teardown)",
    );
    process.exit(1);
  }

  if (reuseExisting) {
    const up = await portListening(DEVSERVER_PORT);
    const dbPath = join(dataDir, "agentre.db");
    const dbPresent = existsSync(dbPath);
    if (!up || !dbPresent) {
      console.error(
        "AGENTRE_E2E_REUSE=1 reuses an already-running verification app, but " +
          (!up
            ? `nothing is listening on :${DEVSERVER_PORT}.`
            : `:${DEVSERVER_PORT} is up but '${dbPath}' is missing — the running server is not using the isolated data dir.`) +
          "\nStart it with the launcher (it owns the isolated data dir, keychain dir and gateway port):\n" +
          "  make verify-up\n" +
          "Runs without AGENTRE_E2E_REUSE keep starting (and tearing down) their own fresh server.",
      );
      process.exit(1);
    }
  }

  const child = spawn(
    process.execPath,
    [playwrightCli, "test", ...process.argv.slice(2)],
    { cwd: here, stdio: "inherit" },
  );

  child.on("exit", (code) => {
    // Reuse mode: the caller owns the server, the data dir and the log — leave them all alone.
    if (!reuseExisting) cleanup(code === 0);
    // Mirror the child's outcome; a signal-killed run (code === null) counts as failure.
    process.exit(code ?? 1);
  });
}

// Always reap the orphan vite (hygiene). On FAILURE keep the temp data dir (agentre.db + logs)
// and the webserver log so you / CI can inspect them; the next fresh run wipes the dir at start
// anyway (playwright.config main-runner rm+mkdir). On success, remove both. Never called in reuse
// mode, where the caller owns the server.
function cleanup(passed) {
  // Skip the reap while a `make verify-up` app is running: the reap matches vite by repo path,
  // so it would take that app's dev server down with it (see lib/target.mjs liveSessions).
  if (liveSessions().length === 0) reapOrphanVite(repoRoot);
  if (passed) {
    rmSync(dataDir, { recursive: true, force: true });
    rmSync(keychainDir, { recursive: true, force: true });
    rmSync(webserverLog, { force: true });
  }
}

main();
