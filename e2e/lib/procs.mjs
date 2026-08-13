import { execFileSync } from "node:child_process";
import { join } from "node:path";

/**
 * `wails dev` orphans its vite child on shutdown (a separate process group on Unix), which a
 * group-kill misses. Reap by command line, scoped to THIS repo's frontend so a sibling
 * checkout's vite (e.g. agentre-server) is never touched. Best-effort.
 */
export function reapOrphanVite(repoRoot) {
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
