// The local-verification target contract shared by verify.mjs and drive.mjs.
// Verification always launches the formal desktop main; this module only supplies
// checkout-scoped storage, ports, the session handshake, and isolation guards.
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { connect, createServer } from "node:net";
import { homedir, tmpdir } from "node:os";
import { dirname, join, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

export class IsolationError extends Error {
  constructor(message) {
    super(message);
    this.name = "IsolationError";
  }
}

export const REPO_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
export const INSTANCE_ID = createHash("sha1").update(REPO_ROOT).digest("hex").slice(0, 8);
export const INSTANCE_ROOT = join(tmpdir(), "agentre-verify", INSTANCE_ID);

const PORT_BLOCK_BASE = 34300;
const PORT_BLOCKS = 600;
const PORTS_PER_BLOCK = 2;
const blockStart =
  PORT_BLOCK_BASE + (parseInt(INSTANCE_ID.slice(0, 4), 16) % PORT_BLOCKS) * PORTS_PER_BLOCK;

const TARGET = {
  devserverPort: blockStart,
  cdpPort: blockStart + 1,
  dataDir: join(INSTANCE_ROOT, "data"),
  keychainDir: join(INSTANCE_ROOT, "keychain"),
  browserDir: join(INSTANCE_ROOT, "browser"),
  logFile: join(INSTANCE_ROOT, "webserver.log"),
};

export function productionRoots() {
  const roots = [];
  if (process.platform === "darwin") {
    roots.push(join(homedir(), "Library", "Application Support", "agentre"));
  } else if (process.platform === "win32") {
    for (const base of [process.env.APPDATA, process.env.LOCALAPPDATA]) {
      if (base) roots.push(join(base, "agentre"));
    }
  } else {
    roots.push(join(process.env.XDG_CONFIG_HOME || join(homedir(), ".config"), "agentre"));
  }
  return roots;
}

function within(dir, root) {
  return dir === root || dir.startsWith(root + sep);
}

export function assertIsolatedDataDir(dir) {
  const candidate = typeof dir === "string" ? resolve(dir.trim() || ".") : "";
  if (candidate === TARGET.dataDir) return candidate;

  for (const root of productionRoots()) {
    if (within(candidate, root)) {
      throw new IsolationError(
        `refusing to touch the installed app's data dir: ${candidate}\n` +
          "Verification only runs against its checkout-scoped throwaway directory.",
      );
    }
    if (within(candidate, `${root}-dev`)) {
      throw new IsolationError(
        `refusing to touch your own dev data dir: ${candidate}\n` +
          "`make dev` holds your sessions and projects; verification gets its own directory.",
      );
    }
  }
  throw new IsolationError(
    `not the sanctioned verification data dir for this checkout: ${candidate || "(empty)"}\n` +
      `Allowed: ${TARGET.dataDir}`,
  );
}

export function assertSanctionedURL(target, url) {
  let parsed;
  try {
    parsed = new URL(url);
  } catch {
    throw new IsolationError(`not a URL: ${url}`);
  }
  const hostOK = parsed.hostname === "localhost" || parsed.hostname === "127.0.0.1";
  if (parsed.protocol !== "http:" || !hostOK || parsed.port !== String(target.devserverPort)) {
    throw new IsolationError(`refusing to drive ${url}: the verification target is only ${target.baseURL}`);
  }
  return parsed.toString();
}

export function resolveTarget() {
  return {
    ...TARGET,
    instanceId: INSTANCE_ID,
    baseURL: `http://localhost:${TARGET.devserverPort}`,
    dbPath: join(TARGET.dataDir, "agentre.db"),
  };
}

export function sessionPath() {
  return join(INSTANCE_ROOT, "session.json");
}

export function readSession() {
  const path = sessionPath();
  if (!existsSync(path)) return null;
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch {
    return null;
  }
}

export function writeSession(session) {
  mkdirSync(INSTANCE_ROOT, { recursive: true });
  writeFileSync(sessionPath(), `${JSON.stringify(session, null, 2)}\n`, { mode: 0o600 });
}

export function clearSession() {
  rmSync(sessionPath(), { force: true });
}

export function prepareDirs(target, { wipe }) {
  assertIsolatedDataDir(target.dataDir);
  if (wipe) {
    rmSync(target.dataDir, { recursive: true, force: true });
    rmSync(target.keychainDir, { recursive: true, force: true });
    rmSync(target.browserDir, { recursive: true, force: true });
  }
  mkdirSync(target.dataDir, { recursive: true, mode: 0o700 });
  mkdirSync(target.keychainDir, { recursive: true, mode: 0o700 });
  mkdirSync(target.browserDir, { recursive: true, mode: 0o700 });
}

export function launchEnv(target) {
  return {
    AGENTRE_DATA_DIR: target.dataDir,
    AGENTRE_ENV: "test",
    AGENTRE_KEYCHAIN_DIR: target.keychainDir,
    AGENTRE_PROXY_PORT: "0",
  };
}

export function portListening(port, host = "127.0.0.1", timeoutMs = 1500) {
  return new Promise((done) => {
    const socket = connect({ host, port });
    const finish = (ok) => {
      socket.destroy();
      done(ok);
    };
    socket.setTimeout(timeoutMs, () => finish(false));
    socket.once("connect", () => finish(true));
    socket.once("error", () => finish(false));
  });
}

export function portFree(port) {
  return new Promise((done) => {
    const server = createServer();
    server.once("error", () => done(false));
    server.listen(port, "127.0.0.1", () => server.close(() => done(true)));
  });
}

export function requireLiveSession() {
  const session = readSession();
  if (!session) {
    throw new IsolationError("no verification app is up in this checkout. Start one: make verify-up");
  }
  return session;
}
