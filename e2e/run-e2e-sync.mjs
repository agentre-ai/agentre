// Runner for the LOCAL END-TO-END SYNC suite (e2e/sync/) — the one harness that
// needs more than the desktop app: a real agentre-server process, the developer's
// PostgreSQL + Redis, a seeded account, and a Go simulated peer.
//
// Everything stateful lives here rather than in playwright.config.ts, because the
// config is re-evaluated in every worker while all of this must happen exactly
// once, before the app boots:
//
//   1. locate the sibling agentre-server checkout and read its DSN
//   2. build the server + the `synce2e` harness tool     (GOWORK=off)
//   3. start the server on a free port, against the developer's PostgreSQL/Redis
//   4. seed one throwaway account with three devices (desktop + two peers)
//   5. put a controllable proxy in front of the server — dropping a flag file
//      makes it cut every connection, which is R7's "server unreachable"
//   6. run playwright with the harness env exported
//   7. ALWAYS delete exactly the rows this run seeded, and report the residue
//
// A dead database is a hard failure with a message naming what is missing — this
// suite must never silently skip or pretend to pass (spec「本地端到端的三个前置条件」).
import { execFileSync, spawn } from "node:child_process";
import { createServer, request } from "node:http";
import { createRequire } from "node:module";
import { createServer as createTcpServer } from "node:net";
import { existsSync, mkdirSync, openSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url)); // e2e/
const repoRoot = join(here, "..");
const runID = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;

const dataDir = join(tmpdir(), "agentre-e2e-sync-data");
const keychainDir = join(tmpdir(), "agentre-e2e-sync-keychain");
const workDir = join(tmpdir(), "agentre-e2e-sync-work");
const webserverLog = join(tmpdir(), "agentre-e2e-sync-webserver.log");
const serverLog = join(workDir, "agentre-server.log");
const offlineFlag = join(workDir, "offline");

let serverProc = null;
let proxy = null;
let toolBin = "";
let dsn = "";
let seeded = null;

main().catch((err) => {
  console.error(`\n[sync-e2e] ${err.message}\n`);
  void finish(1);
});

async function main() {
  const serverDir = locateServerCheckout();
  dsn = readDSN(serverDir);

  rmSync(workDir, { recursive: true, force: true });
  mkdirSync(workDir, { recursive: true });

  console.log("[sync-e2e] building agentre-server + synce2e …");
  const serverBin = join(workDir, "agentre-server");
  toolBin = join(workDir, "synce2e");
  goBuild(serverDir, serverBin, "./cmd/server");
  goBuild(serverDir, toolBin, "./cmd/synce2e");

  const serverPort = await freePort();
  const serverURL = `http://127.0.0.1:${serverPort}`;
  const serverCwd = writeServerConfig(serverDir, serverPort);

  console.log(`[sync-e2e] starting agentre-server on ${serverURL} …`);
  serverProc = spawn(serverBin, [], {
    cwd: serverCwd,
    stdio: ["ignore", openLog(), openLog()],
  });
  serverProc.on("exit", (code) => {
    if (code !== null && code !== 0) console.error(`[sync-e2e] server exited with ${code}`);
  });
  await waitForServer(serverURL);

  console.log(`[sync-e2e] seeding account synce2e-${runID} …`);
  seeded = runTool([
    "seed",
    "--dsn",
    dsn,
    "--run-id",
    runID,
    "--devices",
    "desktop:desktop:darwin,peer:desktop:linux,peerwin:desktop:windows",
  ]);
  const byName = Object.fromEntries(seeded.devices.map((d) => [d.name, d]));

  const proxyPort = await freePort();
  proxy = startProxy(serverPort, proxyPort);
  const proxyURL = `http://127.0.0.1:${proxyPort}`;
  console.log(`[sync-e2e] desktop will talk to ${proxyURL} (cut-able proxy → ${serverURL})`);

  const peerState = join(workDir, "peer.json");
  const peerWinState = join(workDir, "peerwin.json");
  runTool([
    "peer", "init", "--state", peerState, "--base-url", serverURL,
    "--device-id", String(byName.peer.device_id),
    "--refresh-token", byName.peer.refresh_token,
    "--paired", agentredFingerprint(),
  ]);
  runTool([
    "peer", "init", "--state", peerWinState, "--base-url", serverURL,
    "--device-id", String(byName.peerwin.device_id),
    "--refresh-token", byName.peerwin.refresh_token,
  ]);

  // A fresh data dir + keychain dir per run: the seeded login must be the only
  // identity this app has ever had.
  rmSync(dataDir, { recursive: true, force: true });
  mkdirSync(dataDir, { recursive: true });
  rmSync(keychainDir, { recursive: true, force: true });
  mkdirSync(keychainDir, { recursive: true, mode: 0o700 });
  const distDir = join(repoRoot, "frontend", "dist");
  mkdirSync(distDir, { recursive: true });
  writeFileSync(join(distDir, ".keep"), "");

  Object.assign(process.env, {
    AGENTRE_DATA_DIR: dataDir,
    AGENTRE_ENV: "test",
    AGENTRE_KEYCHAIN_DIR: keychainDir,
    AGENTRE_E2E_SERVER_URL: proxyURL,
    AGENTRE_E2E_SERVER_USER_ID: String(seeded.user_id),
    AGENTRE_E2E_DEVICE_ID: String(byName.desktop.device_id),
    AGENTRE_E2E_DEVICE_FINGERPRINT: byName.desktop.fingerprint,
    AGENTRE_E2E_REFRESH_TOKEN: byName.desktop.refresh_token,
    SYNCE2E_BIN: toolBin,
    SYNCE2E_DSN: dsn,
    SYNCE2E_RUN_ID: runID,
    SYNCE2E_SERVER_URL: serverURL,
    SYNCE2E_PEER_STATE: peerState,
    SYNCE2E_PEERWIN_STATE: peerWinState,
    SYNCE2E_PEER_DEVICE_ID: String(byName.peer.device_id),
    SYNCE2E_PEERWIN_DEVICE_ID: String(byName.peerwin.device_id),
    SYNCE2E_AGENTRED_FINGERPRINT: agentredFingerprint(),
    SYNCE2E_OFFLINE_FLAG: offlineFlag,
    SYNCE2E_WEBSERVER_LOG: webserverLog,
  });

  const require = createRequire(import.meta.url);
  const playwrightCli = require.resolve("@playwright/test/cli");
  const child = spawn(
    process.execPath,
    [playwrightCli, "test", "--config", "playwright.sync.config.ts", ...process.argv.slice(2)],
    { cwd: here, stdio: "inherit" },
  );
  child.on("exit", (code) => void finish(code ?? 1));
}

// ── teardown ────────────────────────────────────────────────────────────────

let finishing = false;
async function finish(code) {
  if (finishing) return;
  finishing = true;
  const passed = code === 0;

  if (toolBin && dsn && seeded) {
    // Cleanup runs on every exit path, including a crashed suite: this database is
    // shared with the developer's other work, so leaving rows behind is not an option.
    try {
      const res = runTool(["cleanup", "--dsn", dsn, "--run-id", runID]);
      const residue = Object.entries(res.residue ?? {}).filter(([, n]) => n > 0);
      console.log(
        `[sync-e2e] cleaned up account ${res.user_id}: ` +
          `${JSON.stringify(res.deleted)}${residue.length ? ` RESIDUE ${JSON.stringify(residue)}` : " (no residue)"}`,
      );
      if (residue.length) code = code || 1;
    } catch (err) {
      console.error(`[sync-e2e] CLEANUP FAILED — rows may remain for run ${runID}: ${err.message}`);
      code = code || 1;
    }
  }

  if (proxy) proxy.close();
  if (serverProc && serverProc.exitCode === null) serverProc.kill("SIGTERM");
  reapOrphanVite();

  if (passed && code === 0) {
    rmSync(dataDir, { recursive: true, force: true });
    rmSync(keychainDir, { recursive: true, force: true });
    rmSync(webserverLog, { force: true });
    rmSync(workDir, { recursive: true, force: true });
  } else {
    console.log(`[sync-e2e] kept ${workDir}, ${dataDir} and ${webserverLog} for inspection`);
  }
  process.exit(code);
}

for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => void finish(1));

// ── pieces ──────────────────────────────────────────────────────────────────

// The agentred fingerprint the peer pretends to be paired with. It is never a
// real machine — R2b's judgment is "is there a pairing row", and the desktop
// deliberately has none, which is exactly the asymmetry the suite verifies.
function agentredFingerprint() {
  return `synce2e-${runID}-buildbox`;
}

function locateServerCheckout() {
  const override = process.env.AGENTRE_SERVER_DIR;
  const candidates = override ? [override] : [];
  let dir = repoRoot;
  for (let i = 0; i < 6; i++) {
    candidates.push(join(dir, "agentre-server"));
    dir = dirname(dir);
  }
  for (const candidate of candidates) {
    if (existsSync(join(candidate, "go.mod")) && existsSync(join(candidate, "cmd", "server"))) {
      return resolve(candidate);
    }
  }
  throw new Error(
    "cannot find the agentre-server checkout (looked for a sibling 'agentre-server' with cmd/server). " +
      "Set AGENTRE_SERVER_DIR to point at it.",
  );
}

function readDSN(serverDir) {
  const configPath = join(serverDir, "configs", "config.yaml");
  if (!existsSync(configPath)) {
    throw new Error(`${configPath} is missing — the suite needs the server's real DSN.`);
  }
  const match = /^\s*dsn:\s*(\S+)\s*$/m.exec(readFileSync(configPath, "utf8"));
  if (!match) throw new Error(`no db.dsn found in ${configPath}`);
  return match[1];
}

// writeServerConfig copies the developer's config into a scratch cwd with two
// edits: a free listen port (so a real server on 8443 is untouched) and absolute
// JWT key paths. The repo's own config file is never modified — which also keeps
// cago's habit of writing zero values back to the config out of the checkout.
function writeServerConfig(serverDir, port) {
  const cwd = join(workDir, "server");
  mkdirSync(join(cwd, "configs"), { recursive: true });
  let text = readFileSync(join(serverDir, "configs", "config.yaml"), "utf8");
  text = text.replace(/(http:\s*\n\s*address:\s*\n\s*-\s*)(\S+)/, `$1127.0.0.1:${port}`);
  text = text.replace(
    /(private_key_pem_path:\s*)(\S+)/,
    (_m, key, value) => key + resolve(serverDir, value),
  );
  text = text.replace(
    /(public_key_pem_path:\s*)(\S+)/,
    (_m, key, value) => key + resolve(serverDir, value),
  );
  writeFileSync(join(cwd, "configs", "config.yaml"), text);
  return cwd;
}

function goBuild(dir, out, pkg) {
  try {
    execFileSync("go", ["build", "-o", out, pkg], {
      cwd: dir,
      env: { ...process.env, GOWORK: "off" },
      stdio: ["ignore", "inherit", "inherit"],
    });
  } catch {
    throw new Error(`go build ${pkg} failed in ${dir}`);
  }
}

// A single append-mode fd shared by stdout+stderr keeps ordering readable.
function openLog() {
  return openSync(serverLog, "a");
}

async function waitForServer(baseURL) {
  const deadline = Date.now() + 90_000;
  for (;;) {
    if (serverProc.exitCode !== null) {
      throw new Error(
        `agentre-server exited during startup (code ${serverProc.exitCode}). ` +
          `Most likely PostgreSQL or Redis from configs/config.yaml is unreachable. See ${serverLog}`,
      );
    }
    try {
      const status = await probe(`${baseURL}/v1/keys`);
      if (status === 200) return;
    } catch {
      // not up yet
    }
    if (Date.now() > deadline) {
      throw new Error(
        `agentre-server never became healthy on ${baseURL}. ` +
          `Check that PostgreSQL and Redis from agentre-server/configs/config.yaml are reachable. See ${serverLog}`,
      );
    }
    await sleep(500);
  }
}

function probe(url) {
  return new Promise((resolvePromise, reject) => {
    const req = request(url, { method: "GET", timeout: 3_000 }, (res) => {
      res.resume();
      resolvePromise(res.statusCode);
    });
    req.on("timeout", () => req.destroy(new Error("timeout")));
    req.on("error", reject);
    req.end();
  });
}

// startProxy forwards to the server unless the offline flag file exists, in which
// case it destroys the connection. That is the whole "network cut" mechanism: no
// control port, no IPC — the spec creates/removes one file.
function startProxy(upstreamPort, listenPort) {
  const server = createServer((req, res) => {
    if (existsSync(offlineFlag)) {
      req.destroy();
      res.destroy();
      return;
    }
    const upstream = request(
      {
        host: "127.0.0.1",
        port: upstreamPort,
        path: req.url,
        method: req.method,
        headers: req.headers,
      },
      (up) => {
        res.writeHead(up.statusCode ?? 502, up.headers);
        up.pipe(res);
      },
    );
    upstream.on("error", () => res.destroy());
    req.pipe(upstream);
  });
  server.listen(listenPort, "127.0.0.1");
  return server;
}

function runTool(args) {
  const out = execFileSync(toolBin, args, { encoding: "utf8", timeout: 120_000 });
  return JSON.parse(out);
}

function freePort() {
  return new Promise((resolvePromise, reject) => {
    const srv = createTcpServer();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const { port } = srv.address();
      srv.close(() => resolvePromise(port));
    });
  });
}

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

// Same orphan-vite hygiene as run-e2e.mjs: `wails dev` leaves it behind.
function reapOrphanVite() {
  const frontend = join(repoRoot, "frontend");
  try {
    if (process.platform === "win32") {
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
    // best-effort
  }
}
