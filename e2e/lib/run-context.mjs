import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import {
  chmodSync,
  createWriteStream,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  writeFileSync,
} from "node:fs";
import { access, cp, mkdir, readFile, readdir, realpath, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
export const REPO_ROOT = resolve(here, "..", "..");
function makePrivateDir(path) {
  mkdirSync(path, { recursive: true, mode: 0o700 });
  if (process.platform !== "win32") chmodSync(path, 0o700);
}

async function reserveLoopbackPort() {
  return new Promise((resolvePort, reject) => {
    const server = createServer();
    server.unref();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      const port = typeof address === "object" && address ? address.port : 0;
      server.close((error) => (error ? reject(error) : resolvePort(port)));
    });
  });
}

export async function reserveDistinctLoopbackPorts(reserve = reserveLoopbackPort) {
  const first = await reserve();
  let second = await reserve();
  while (second === first) second = await reserve();
  return [first, second];
}

export async function createRunContext({ prefix = "agentre-e2e-" } = {}) {
  const runRoot = mkdtempSync(join(tmpdir(), prefix));
  if (process.platform !== "win32") chmodSync(runRoot, 0o700);
  const dataDir = join(runRoot, "data");
  const keychainDir = join(runRoot, "keychain");
  const browserDir = join(runRoot, "browser");
  const logsDir = join(runRoot, "logs");
  const playwrightDir = join(runRoot, "playwright");
  for (const dir of [dataDir, keychainDir, browserDir, logsDir, playwrightDir]) makePrivateDir(dir);

  const token = randomBytes(32).toString("hex");
  const manifestPath = join(runRoot, "manifest.json");
  writeFileSync(
    manifestPath,
    `${JSON.stringify({ runRoot, dataDir, keychainDir, token }, null, 2)}\n`,
    { mode: 0o600 },
  );
  if (process.platform !== "win32") chmodSync(manifestPath, 0o600);

  const [port, vitePort] = await reserveDistinctLoopbackPorts();
  return {
    runRoot,
    dataDir,
    keychainDir,
    browserDir,
    logsDir,
    playwrightDir,
    manifestPath,
    token,
    port,
    vitePort,
    baseURL: `http://127.0.0.1:${port}`,
    viteURL: `http://127.0.0.1:${vitePort}`,
    controlToken: randomBytes(32).toString("hex"),
    syncRecorderPath: join(logsDir, "fake-sync-recorder.json"),
    appLog: join(logsDir, "app.log"),
    env: {
      AGENTRE_E2E_MANIFEST: manifestPath,
      AGENTRE_E2E_TOKEN: token,
      AGENTRE_DATA_DIR: dataDir,
      AGENTRE_KEYCHAIN_DIR: keychainDir,
      AGENTRE_ENV: "test",
      AGENTRE_PROXY_PORT: "0",
    },
    remove: () => rm(runRoot, { recursive: true, force: true }),
  };
}

export function appEnvironment(run, parentEnv = process.env) {
  const env = { ...parentEnv };
  for (const name of Object.keys(env)) {
    if (name.startsWith("AGENTRE_E2E_") || name === "AGENTRE_DATA_DIR" || name === "AGENTRE_KEYCHAIN_DIR") {
      delete env[name];
    }
  }
  return { ...env, ...run.env };
}

export function fakePeerEnvironment(run, parentEnv = process.env) {
  return {
    ...appEnvironment(run, parentEnv),
    AGENTRE_E2E_CONTROL_TOKEN: run.controlToken,
  };
}

export function playwrightEnvironment(run, parentEnv = process.env) {
  const env = { ...parentEnv };
  for (const name of Object.keys(env)) {
    if (
      name.startsWith("AGENTRE_E2E_") ||
      name === "AGENTRE_KEYCHAIN_DIR" ||
      name === "AGENTRE_ENV" ||
      name === "AGENTRE_PROXY_PORT"
    ) {
      delete env[name];
    }
  }
  return {
    ...env,
    AGENTRE_DATA_DIR: run.dataDir,
    AGENTRE_E2E_BASE_URL: run.baseURL,
    AGENTRE_E2E_RUN_ID: basename(run.runRoot),
    AGENTRE_E2E_PLAYWRIGHT_DIR: run.playwrightDir,
    AGENTRE_E2E_CONTROL_TOKEN: run.controlToken,
    ...(run.syncIdentity
      ? {
          AGENTRE_E2E_SYNC_SERVER_URL: run.syncIdentity.serverURL,
          AGENTRE_E2E_SYNC_USER_ID: String(run.syncIdentity.userID),
          AGENTRE_E2E_SYNC_DEVICE_ID: String(run.syncIdentity.deviceID),
          AGENTRE_E2E_SYNC_DEVICE_FINGERPRINT: run.syncIdentity.deviceFingerprint,
          AGENTRE_E2E_SYNC_PEER_DEVICE_ID: String(run.syncIdentity.peerDeviceID),
        }
      : {}),
    TMPDIR: run.browserDir,
    TMP: run.browserDir,
    TEMP: run.browserDir,
  };
}

function childExitResult(child) {
  return new Promise((resolveResult) => {
    let resolved = false;
    const resolveOnce = (result) => {
      if (resolved) return;
      resolved = true;
      resolveResult(result);
    };
    child.once("error", () => resolveOnce({ code: 1, signal: null }));
    child.once("exit", (code, signal) =>
      resolveOnce({ code: code ?? 1, signal }),
    );
  });
}

export async function generateWailsBindings(
  run,
  {
    bindingsDir = join(REPO_ROOT, "frontend", "wailsjs"),
    frontendDistDir = join(REPO_ROOT, "frontend", "dist"),
    parentEnv = process.env,
    spawnProcess = spawnLogged,
  } = {},
) {
  if (!run?.runRoot || !run?.logsDir) {
    throw new Error("Wails binding generation context is unavailable; use make e2e");
  }

  const generationRoot = join(run.runRoot, "wails-bindings");
  const dataDir = join(generationRoot, "data");
  const keychainDir = join(generationRoot, "keychain");
  const appDataDir = join(generationRoot, "appdata");
  const localAppDataDir = join(generationRoot, "local-appdata");
  const xdgConfigDir = join(generationRoot, "xdg-config");
  const xdgDataDir = join(generationRoot, "xdg-data");
  const xdgCacheDir = join(generationRoot, "xdg-cache");
  const tempDir = join(generationRoot, "tmp");
  for (const path of [
    generationRoot,
    dataDir,
    keychainDir,
    appDataDir,
    localAppDataDir,
    xdgConfigDir,
    xdgDataDir,
    xdgCacheDir,
    tempDir,
  ]) {
    makePrivateDir(path);
  }

  const env = { ...parentEnv };
  for (const name of Object.keys(env)) {
    if (
      name.startsWith("AGENTRE_E2E_") ||
      name === "AGENTRE_DATA_DIR" ||
      name === "AGENTRE_KEYCHAIN_DIR"
    ) {
      delete env[name];
    }
  }
  await rm(bindingsDir, { recursive: true, force: true });
  await mkdir(frontendDistDir, { recursive: true, mode: 0o700 });
  await writeFile(join(frontendDistDir, ".keep"), "", { mode: 0o600 });

  Object.assign(env, {
    AGENTRE_DATA_DIR: dataDir,
    AGENTRE_KEYCHAIN_DIR: keychainDir,
    AGENTRE_ENV: "test",
    AGENTRE_PROXY_PORT: "0",
    APPDATA: appDataDir,
    LOCALAPPDATA: localAppDataDir,
    XDG_CONFIG_HOME: xdgConfigDir,
    XDG_DATA_HOME: xdgDataDir,
    XDG_CACHE_HOME: xdgCacheDir,
    TMPDIR: tempDir,
    TMP: tempDir,
    TEMP: tempDir,
  });

  try {
    let child;
    try {
      child = spawnProcess("wails", ["generate", "module"], {
        cwd: REPO_ROOT,
        env,
        logPath: join(run.logsDir, "wails-bindings.log"),
      });
    } catch {
      throw new Error("failed to generate Wails bindings; use make e2e");
    }
    const result = await childExitResult(child);
    if (result.code !== 0 || result.signal) {
      throw new Error("failed to generate Wails bindings; use make e2e");
    }

    try {
      await Promise.all([
        access(join(bindingsDir, "runtime", "runtime.js")),
        access(join(bindingsDir, "go", "app", "App.js")),
      ]);
    } catch {
      throw new Error("required Wails bindings are missing; use make e2e");
    }
  } finally {
    await rm(generationRoot, { recursive: true, force: true });
  }
}

const credentialEvidenceFailures = Object.freeze({
  unavailable: "remote device credential evidence is unavailable for the runner file keychain",
  canonicalization: "runner file keychain cannot be canonicalized",
  outside: "remote device credential evidence is outside the runner file keychain",
  inspection: "runner file keychain cannot be inspected",
  missing: "remote device credential was not persisted in the runner file keychain",
});

function credentialEvidenceError(reason) {
  return new Error(`${credentialEvidenceFailures[reason]}; use make e2e (the E2E runner)`);
}

export async function assertRemoteDeviceCredentialPersisted(run) {
  const expectedToken = run?.remoteIdentity?.deviceToken;
  if (!run?.runRoot || !run?.keychainDir || !expectedToken) {
    throw credentialEvidenceError("unavailable");
  }

  let canonicalRunRoot;
  let canonicalKeychainDir;
  try {
    [canonicalRunRoot, canonicalKeychainDir] = await Promise.all([
      realpath(run.runRoot),
      realpath(run.keychainDir),
    ]);
  } catch {
    throw credentialEvidenceError("canonicalization");
  }
  const keychainRelative = relative(canonicalRunRoot, canonicalKeychainDir);
  if (
    keychainRelative === "" ||
    keychainRelative === ".." ||
    keychainRelative.startsWith(`..${sep}`) ||
    isAbsolute(keychainRelative)
  ) {
    throw credentialEvidenceError("outside");
  }

  try {
    const entries = await readdir(canonicalKeychainDir, { withFileTypes: true });
    for (const entry of entries) {
      if (!entry.isFile()) continue;
      const content = await readFile(join(canonicalKeychainDir, entry.name), "utf8");
      if (content === expectedToken) return;
    }
  } catch {
    throw credentialEvidenceError("inspection");
  }
  throw credentialEvidenceError("missing");
}

export async function waitForURL(url, { timeoutMs = 240_000, intervalMs = 250 } = {}) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url, { redirect: "manual" });
      if (response.status < 500) return;
      lastError = new Error(`HTTP ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, intervalMs));
  }
  throw new Error(`timed out waiting for ${url}: ${lastError?.message ?? "not reachable"}`);
}

export function spawnLogged(command, args, { cwd = REPO_ROOT, env = process.env, logPath }) {
  makePrivateDir(dirname(logPath));
  const log = createWriteStream(logPath, { flags: "a", mode: 0o600 });
  const child = spawn(command, args, {
    cwd,
    env,
    detached: process.platform !== "win32",
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stdout.pipe(log, { end: false });
  child.stderr.pipe(log, { end: false });
  child.once("close", () => log.end());
  return child;
}

export async function terminateProcessTree(pid) {
  if (!Number.isInteger(pid) || pid <= 0) return;
  if (process.platform === "win32") {
    await new Promise((resolveDone) => {
      const killer = spawn("taskkill", ["/PID", String(pid), "/T", "/F"], { stdio: "ignore" });
      killer.once("exit", resolveDone);
      killer.once("error", resolveDone);
    });
    return;
  }
  try {
    process.kill(-pid, "SIGTERM");
  } catch (error) {
    if (error?.code === "ESRCH") return;
    throw error;
  }
  await new Promise((resolveWait) => setTimeout(resolveWait, 1500));
  try {
    process.kill(-pid, 0);
    process.kill(-pid, "SIGKILL");
  } catch (error) {
    if (error?.code !== "ESRCH") throw error;
  }
}

export class ProcessSupervisor {
  constructor(terminate = terminateProcessTree) {
    this.terminate = terminate;
    this.children = new Map();
    this.stopping = null;
  }

  track(child) {
    if (child?.pid) {
      this.children.set(child.pid, child);
      child.once?.("exit", () => this.children.delete(child.pid));
      child.once?.("error", () => this.children.delete(child.pid));
    }
    return child;
  }

  async stopAll() {
    if (this.stopping) return this.stopping;
    this.stopping = Promise.allSettled(
      [...this.children.keys()].map((pid) => this.terminate(pid)),
    ).then((results) => {
      this.children.clear();
      const failures = results
        .filter((result) => result.status === "rejected")
        .map((result) => result.reason);
      if (failures.length > 0) {
        throw new AggregateError(
          failures,
          `failed to terminate ${failures.length} supervised process tree${failures.length === 1 ? "" : "s"}`,
        );
      }
    });
    return this.stopping;
  }
}

function redact(value, secrets) {
  let out = value;
  for (const secret of secrets.filter(Boolean)) out = out.split(secret).join("[REDACTED]");
  return out;
}

async function sanitizeTextFiles(root, secrets) {
  if (!existsSync(root)) return;
  for (const entry of readdirSync(root, { withFileTypes: true })) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) {
      await sanitizeTextFiles(path, secrets);
      continue;
    }
    if (!/\.(json|log|txt|md)$/i.test(entry.name)) continue;
    const content = await readFile(path, "utf8");
    await writeFile(path, redact(content, secrets), { mode: 0o600 });
  }
}

export async function preserveFailureArtifacts(run, artifactBase = join(REPO_ROOT, "e2e-artifacts")) {
  const artifactDir = join(artifactBase, basename(run.runRoot));
  await rm(artifactDir, { recursive: true, force: true });
  await mkdir(artifactDir, { recursive: true, mode: 0o700 });
  await cp(run.runRoot, artifactDir, { recursive: true, force: true });
  for (const unsafePath of [
    "browser",
    "keychain",
    "data/ctl-endpoint.json",
    "fake-runtime.go",
    "go-overlay.json",
    ".agentre-e2e-consumed",
  ]) {
    await rm(join(artifactDir, unsafePath), { recursive: true, force: true });
  }
  await sanitizeTextFiles(artifactDir, [
    run.token,
    run.controlToken,
    run.syncIdentity?.refreshToken,
    run.remoteIdentity?.deviceToken,
  ]);
  await run.remove();
  return artifactDir;
}
