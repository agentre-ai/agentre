// Cross-platform e2e runner: runs `playwright test` (forwarding extra args), then cleans up
// AFTER Playwright has fully exited (webServer torn down, app gone, db closed, vite orphaned).
//
// Why a Node wrapper instead of cleaning up elsewhere — see e2e/README.md §7:
//   - globalTeardown runs while Playwright still MANAGES the webServer → killing there
//     SIGTERMs the live server (exit 143).
//   - a Makefile `pkill -f "wails dev …"` self-matches the recipe shell's own /proc/<pid>/cmdline
//     on Linux and SIGTERMs make. The runner's cmdline is `node run-e2e.mjs`, so it's safe.
import { execFileSync, spawn } from "node:child_process";
import { createRequire } from "node:module";
import { existsSync, rmSync } from "node:fs";
import { connect } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url)); // e2e/
const repoRoot = join(here, "..");
// These must match the paths in playwright.config.ts.
const dataDir = join(tmpdir(), "agentre-e2e-data");
const webserverLog = join(tmpdir(), "agentre-e2e-webserver.log");
const DEVSERVER_PORT = 34216;

const require = createRequire(import.meta.url);
const playwrightCli = require.resolve("@playwright/test/cli");

// Keep-alive (fast inner loop) mode — see e2e/README.md §4. The caller owns a hand-started
// `wails dev -tags e2e` on :34216, so this runner must NOT tear anything down (no orphan-vite
// reap, no temp-dir removal) or the next iteration's reuse breaks. playwright.config.ts reads the
// same flag to skip the data-dir wipe and force reuseExistingServer.
const reuseExisting = process.env.AGENTRE_E2E_REUSE === "1";

function portListening(port) {
  return new Promise((resolve) => {
    const sock = connect({ host: "localhost", port });
    const done = (ok) => {
      sock.destroy();
      resolve(ok);
    };
    sock.setTimeout(1500, () => done(false));
    sock.once("connect", () => done(true));
    sock.once("error", () => done(false));
  });
}

async function main() {
  // Reuse mode is an explicit "I started the server" contract: fail fast with instructions
  // instead of letting Playwright quietly start a fresh app against a stale data dir. Two
  // conditions must hold: :34216 is listening AND it is serving the e2e temp data dir (the
  // agentre.db file is where a correctly-overridden server writes it). A server up on the real
  // data dir (AGENTRE_DATA_DIR not set) would silently seed the user's real DB — reject it.
  if (reuseExisting) {
    const up = await portListening(DEVSERVER_PORT);
    const dbPath = join(dataDir, "agentre.db");
    const dbPresent = existsSync(dbPath);
    if (!up || !dbPresent) {
      console.error(
        "AGENTRE_E2E_REUSE=1 reuses a hand-started 'wails dev -tags e2e', but " +
          (!up
            ? `nothing is listening on :${DEVSERVER_PORT}.`
            : `:${DEVSERVER_PORT} is up but '${dbPath}' is missing — the running server is not using the e2e data dir.`) +
          "\nStart it once with the same overrides the harness injects, then iterate:\n" +
          `  mkdir -p "${dataDir}"\n` +
          `  AGENTRE_DATA_DIR="${dataDir}" AGENTRE_ENV=test AGENTRE_PROXY_PORT=0 \\` +
          `    wails dev -tags e2e -devserver localhost:${DEVSERVER_PORT} > "${webserverLog}" 2>&1 &\n` +
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

main();
