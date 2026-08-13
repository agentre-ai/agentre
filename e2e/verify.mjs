// The verification launcher: `node verify.mjs up|down|status`.
//
// It is the ONE thing you start. It brings up an isolated Agentre plus a browser attached to it,
// and then gets out of the way — you drive that browser with `node drive.mjs <command>` instead
// of authoring a spec. See docs/verification.md for the route and e2e/README.md for the harness.
//
// Everything about which app may be launched, and where its state lives, comes from
// lib/target.mjs — this file never invents a path or a port.
import { spawn } from "node:child_process";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, openSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  DEFAULT_FLAVOR,
  IsolationError,
  REPO_ROOT,
  assertIsolatedDataDir,
  blocksSecondFlavor,
  clearSession,
  launchEnv,
  liveSessions,
  portListening,
  prepareDirs,
  readSession,
  resolveTarget,
  writeSession,
} from "./lib/target.mjs";
import { applyVerificationViewport, verificationBrowserArgs } from "./lib/browser.mjs";
import { reapOrphanVite } from "./lib/procs.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, "..");

function parseArgs(argv) {
  const args = { command: argv[0], flags: {} };
  for (let i = 1; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--flavor") args.flags.flavor = argv[++i];
    else if (arg.startsWith("--flavor=")) args.flags.flavor = arg.slice("--flavor=".length);
    else if (arg === "--keep") args.flags.keep = true;
    else if (arg === "--wipe") args.flags.wipe = true;
    else if (arg === "--headed") args.flags.headed = true;
    else if (arg === "--no-browser") args.flags.noBrowser = true;
    else throw new Error(`unknown argument: ${arg}`);
  }
  return args;
}

function flavorOf(flags) {
  return flags.flavor || process.env.AGENTRE_VERIFY_FLAVOR || DEFAULT_FLAVOR;
}

function wailsBin() {
  if (process.env.WAILS) return process.env.WAILS;
  try {
    execFileSync(process.platform === "win32" ? "where" : "which", ["wails"], { stdio: "ignore" });
    return "wails";
  } catch {
    const gopath = execFileSync("go", ["env", "GOPATH"], { encoding: "utf8" }).trim();
    return join(gopath, "bin", "wails");
  }
}

async function urlReady(url) {
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(2000) });
    return res.ok || res.status === 404; // the bridge answers; the SPA route may not exist yet
  } catch {
    return false;
  }
}

async function waitFor(check, { timeoutMs, everyMs = 500, onWait }) {
  const deadline = Date.now() + timeoutMs;
  let waited = 0;
  while (Date.now() < deadline) {
    if (await check()) return true;
    await new Promise((res) => setTimeout(res, everyMs));
    waited += everyMs;
    if (onWait && waited % 10_000 === 0) onWait(waited / 1000);
  }
  return false;
}

function tailLog(logFile, lines = 25) {
  if (!existsSync(logFile)) return "(no log yet)";
  const text = readFileSync(logFile, "utf8").split("\n");
  return text.slice(-lines).join("\n");
}

function alive(pid) {
  if (!pid) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

async function up(flags) {
  const target = resolveTarget(flavorOf(flags));

  // `wails dev` compiles into this checkout's build/bin: a second flavor started here would
  // overwrite the binary the first one is running and kill it mid-run. Concurrency belongs
  // across worktrees — each has its own build dir, its own ports and its own data dir.
  const rivals = blocksSecondFlavor(target.flavor);
  if (rivals.length > 0) {
    console.error(
      `the ${rivals.map((s) => s.flavor).join(", ")} app is already up in this checkout.\n` +
        "`wails dev` builds into this checkout's build/bin, so starting a second flavor here " +
        "overwrites the running binary and kills it.\n" +
        `  make verify-down FLAVOR=${rivals[0].flavor}   # stop it, then start ${target.flavor}\n` +
        "  …or run the other flavor from a separate git worktree (its own ports and dirs).",
    );
    return 1;
  }

  const existing = readSession(target.flavor);
  const portTaken = await portListening(target.devserverPort);

  if (existing && portTaken) {
    console.log(`already up: ${target.baseURL} (flavor ${target.flavor})`);
    if (!existing.browserPid || !(await portListening(target.cdpPort))) {
      await attachBrowser(target, flags, existing);
    }
    printNext(target);
    return 0;
  }

  if (portTaken) {
    // Something is serving this port that this launcher did not start — a hand-run `wails dev`,
    // a leftover from a previous session, or an unrelated program that happens to hold the port.
    // Adopt it only when it is demonstrably on this checkout's isolated data dir; never restart
    // or wipe underneath it, and never touch it when it is not.
    if (!existsSync(target.dbPath)) {
      console.error(
        `:${target.devserverPort} is already serving something that is NOT using ${target.dataDir}.\n` +
          "Refusing to touch it. Stop that process (it may be unrelated to this repo) and retry.",
      );
      return 1;
    }
    const adoptedPid = pidOnPort(target.devserverPort);
    console.log(`adopting the app already serving ${target.baseURL} (pid ${adoptedPid ?? "unknown"})`);
    const session = newSession(target, adoptedPid, { adopted: true });
    await attachBrowser(target, flags, session);
    printNext(target);
    return 0;
  }

  if (existing) clearSession(target.flavor);

  // The isolation contract, enforced before anything is created or spawned.
  assertIsolatedDataDir(target.dataDir);
  prepareDirs(target, { wipe: !flags.keep });

  // `wails dev` needs frontend/dist to exist for the //go:embed (mirrors `make dev`).
  const distDir = join(repoRoot, "frontend", "dist");
  mkdirSync(distDir, { recursive: true });
  writeFileSync(join(distDir, ".keep"), "");

  const wailsArgs = ["dev"];
  if (target.buildTags.length > 0) wailsArgs.push("-tags", target.buildTags.join(","));
  wailsArgs.push("-devserver", `localhost:${target.devserverPort}`);

  // Detached + its own process group: `down` kills the whole group, and this launcher can exit
  // without taking the app with it. Output goes to a file, never a pipe — a piped stdout kept
  // open by wails' orphaned vite child is the classic teardown hang (e2e/README.md §7).
  rmSync(target.logFile, { force: true });
  const logFd = openSync(target.logFile, "a");
  const app = spawn(wailsBin(), wailsArgs, {
    cwd: repoRoot,
    detached: true,
    stdio: ["ignore", logFd, logFd],
    env: { ...process.env, ...launchEnv(target) },
  });
  app.unref();

  console.log(
    `starting ${target.flavor} app (${["wails", ...wailsArgs].join(" ")}) — first run compiles Go + vite, ~30-60s`,
  );
  const ready = await waitFor(() => urlReady(target.baseURL), {
    timeoutMs: 300_000,
    onWait: (s) => console.log(`  … still building (${s}s)`),
  });
  if (!ready) {
    console.error(`app did not come up on ${target.baseURL} within 300s.\n${tailLog(target.logFile)}`);
    try {
      process.kill(-app.pid, "SIGTERM");
    } catch {
      /* already gone */
    }
    return 1;
  }

  const session = newSession(target, app.pid, { adopted: false });
  await attachBrowser(target, flags, session);

  console.log(`up: ${target.baseURL}  data=${target.dataDir}`);
  printNext(target);
  return 0;
}

/** Start (or re-start) the driven browser, record it, and put the native window back out of sight. */
async function attachBrowser(target, flags, session) {
  session.headless = !flags.headed;
  session.browserPid = flags.noBrowser ? null : await startBrowser(target, flags);
  writeSession(target.flavor, session);
  if (session.browserPid && session.headless) {
    session.windowHidden = await hideNativeWindow(session);
    writeSession(target.flavor, session);
    if (!session.windowHidden) {
      console.log("note: could not hide the native window (the app may still be booting)");
    }
  }
  return session;
}

function newSession(target, appPid, { adopted }) {
  return {
    flavor: target.flavor,
    appPid: appPid ?? null,
    adopted,
    browserPid: null,
    cdpPort: target.cdpPort,
    cdpURL: `http://127.0.0.1:${target.cdpPort}`,
    devserverPort: target.devserverPort,
    baseURL: target.baseURL,
    dataDir: target.dataDir,
    keychainDir: target.keychainDir,
    dbPath: target.dbPath,
    logFile: target.logFile,
    buildTags: target.buildTags,
    startedAt: new Date().toISOString(),
  };
}

/** Best-effort pid of whatever holds `port`, so `down` can stop an adopted app too. */
function pidOnPort(port) {
  try {
    if (process.platform === "win32") {
      const out = execFileSync("netstat", ["-ano", "-p", "TCP"], { encoding: "utf8" });
      const row = out.split("\n").find((l) => l.includes(`:${port}`) && l.includes("LISTENING"));
      const pid = row ? Number(row.trim().split(/\s+/).pop()) : NaN;
      return Number.isInteger(pid) ? pid : null;
    }
    const out = execFileSync("lsof", ["-ti", `tcp:${port}`, "-sTCP:LISTEN"], { encoding: "utf8" });
    const pid = Number(out.split("\n")[0].trim());
    return Number.isInteger(pid) ? pid : null;
  } catch {
    return null;
  }
}

async function startBrowser(target, flags) {
  const { chromium } = await import("@playwright/test");
  let executable;
  try {
    executable = chromium.executablePath();
  } catch (err) {
    throw new Error(`Chromium is not installed: run \`cd e2e && pnpm run setup\` (${err.message})`);
  }
  if (!existsSync(executable)) {
    throw new Error(`Chromium is missing at ${executable}: run \`cd e2e && pnpm run setup\``);
  }
  // A fixed large viewport keeps screenshots legible and complete instead of inheriting
  // Chromium's small platform-dependent default. `--headed` only changes visibility.
  const args = verificationBrowserArgs({
    cdpPort: target.cdpPort,
    browserDir: target.browserDir,
    headless: !flags.headed,
  });
  args.push(target.baseURL);

  const browser = spawn(executable, args, { detached: true, stdio: "ignore" });
  browser.unref();

  const ready = await waitFor(() => urlReady(`http://127.0.0.1:${target.cdpPort}/json/version`), {
    timeoutMs: 30_000,
  });
  if (!ready) throw new Error(`Chromium did not expose CDP on :${target.cdpPort}`);
  return browser.pid;
}

/**
 * `wails dev` always creates a native window, and the frontend calls `WindowShow()` on mount —
 * from the driving browser as much as from the native webview, since both talk to the same app
 * over the bridge. So the window cannot be suppressed at launch; it is hidden right after,
 * through the same runtime call. Best-effort: an app that will not hide is not a failed launch.
 */
export async function hideNativeWindow(session) {
  try {
    const { chromium } = await import("@playwright/test");
    const browser = await chromium.connectOverCDP(session.cdpURL, { timeout: 5000 });
    try {
      const page = browser.contexts()[0]?.pages()[0];
      if (!page) return false;
      await page.waitForLoadState("domcontentloaded", { timeout: 5000 });
      await applyVerificationViewport(page);
      await page.evaluate(() => window.runtime?.WindowHide?.());
      return true;
    } finally {
      await browser.close().catch(() => {});
    }
  } catch {
    return false;
  }
}

function printNext(target) {
  console.log(
    [
      "",
      "drive it (one action per call, every call recorded):",
      `  node e2e/drive.mjs snapshot --scenario <slug>`,
      `  node e2e/drive.mjs click "testid=new-chat-button" --scenario <slug>`,
      `  node e2e/drive.mjs shot 01-after-click --scenario <slug>`,
      "",
      `checkout: ${target.instanceId} (its own ports/dirs — other worktrees can run at the same time)`,
      `logs:     ${target.logFile}`,
      `db:       ${target.dbPath}`,
      `stop:     make verify-down FLAVOR=${target.flavor}`,
    ].join("\n"),
  );
}

function down(flags) {
  const flavor = flavorOf(flags);
  const target = resolveTarget(flavor);
  const session = readSession(flavor);
  if (!session) {
    console.log(`no ${flavor} session recorded; reaping leftovers anyway`);
  }
  for (const pid of [session?.browserPid, session?.appPid]) {
    if (!pid) continue;
    for (const target_ of [-pid, pid]) {
      try {
        process.kill(target_, "SIGTERM");
        break;
      } catch {
        /* not a group leader / already gone */
      }
    }
  }
  clearSession(flavor);
  // The reap matches vite by repo path, so it cannot tell this flavor's vite from another's.
  // With another verification app still up, leaking one orphan is far cheaper than killing the
  // live app's dev server (which leaves it listening and serving 502).
  const stillUp = liveSessions();
  if (stillUp.length === 0) {
    reapOrphanVite(repoRoot);
  } else {
    console.log(`keeping stray vite: ${stillUp.map((s) => s.flavor).join(", ")} still up`);
  }
  if (flags.wipe) {
    assertIsolatedDataDir(target.dataDir);
    rmSync(target.dataDir, { recursive: true, force: true });
    rmSync(target.keychainDir, { recursive: true, force: true });
    console.log(`down: ${flavor} stopped, isolated state wiped`);
    return 0;
  }
  console.log(`down: ${flavor} stopped (state kept at ${target.dataDir}; add --wipe to remove)`);
  return 0;
}

async function status(flags) {
  const flavor = flavorOf(flags);
  const target = resolveTarget(flavor);
  const session = readSession(flavor);
  const bridgeUp = await portListening(target.devserverPort);
  const cdpUp = await portListening(target.cdpPort);
  const dbSize = existsSync(target.dbPath) ? statSync(target.dbPath).size : 0;
  console.log(
    [
      `checkout: ${target.instanceId} — ${REPO_ROOT}`,
      `flavor:   ${flavor} (${target.buildTags.length ? `-tags ${target.buildTags.join(",")}` : "real runtimes, no build tag"})`,
      `session:  ${session ? `started ${session.startedAt}` : "none"}`,
      `app:      ${bridgeUp ? "up" : "down"} ${target.baseURL}${session?.appPid ? ` pid=${session.appPid}${alive(session.appPid) ? "" : " (gone)"}` : ""}`,
      `browser:  ${cdpUp ? "up" : "down"} cdp=:${target.cdpPort}${session?.browserPid ? ` pid=${session.browserPid}` : ""}${session ? (session.headless ? " headless" : " headed") : ""}`,
      `data:     ${target.dataDir}`,
      `db:       ${target.dbPath} (${dbSize} bytes)`,
      `keychain: ${target.keychainDir}`,
      `log:      ${target.logFile}`,
    ].join("\n"),
  );
  return bridgeUp ? 0 : 1;
}

async function main() {
  const { command, flags } = parseArgs(process.argv.slice(2));
  switch (command) {
    case "up":
      return up(flags);
    case "down":
      return down(flags);
    case "status":
      return status(flags);
    default:
      console.error(
        "usage: node verify.mjs up|down|status [--flavor fake|real] [--keep|--wipe] [--headed] [--no-browser]",
      );
      return 2;
  }
}

main()
  .then((code) => process.exit(code))
  .catch((err) => {
    console.error(err instanceof IsolationError ? `${err.name}: ${err.message}` : err);
    process.exit(1);
  });
