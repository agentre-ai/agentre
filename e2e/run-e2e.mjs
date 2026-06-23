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
