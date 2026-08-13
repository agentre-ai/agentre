// The verification target contract, shared by verify.mjs, drive.mjs, run-e2e.mjs and
// playwright.config.ts. It is the only place a port, data dir or keychain dir is named.
//
// Two rules hold the local-verification route up, and both are mechanical here rather than
// remembered:
//
//  1. **Never production.** A driven app runs on a throwaway dir; the installed app's root and
//     your own `make dev` root are refused by name, and so is anything else.
//  2. **Never shared between checkouts.** Every path and port is derived from *this* checkout's
//     absolute path, so two worktrees can verify at the same time without seeing each other.
//     Same checkout → same values, which is what makes `verify-up` idempotent and lets
//     `drive.mjs` find the app without being told.
//
// Two flavors:
//   fake — `wails dev -tags e2e`: the deterministic fake runtime; no CLI subprocess, no auth.
//          The committed Playwright suite runs on this target too.
//   real — `wails dev` with no tag: the real claudecode / codex runtimes, real CLI subprocesses.
//          The isolated keychain still applies (AGENTRE_KEYCHAIN_DIR is not gated on the build
//          tag — internal/bootstrap/keychain.go).
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

/** This checkout — `e2e/lib` → `e2e` → repo root. Worktrees resolve to different paths. */
export const REPO_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

/** Short, stable per-checkout id. Different worktree → different everything. */
export const INSTANCE_ID = createHash("sha1").update(REPO_ROOT).digest("hex").slice(0, 8);

export const INSTANCE_ROOT = join(tmpdir(), "agentre-verify", INSTANCE_ID);

// One 4-port block per checkout, deterministic so a second `verify-up` finds the first instead
// of starting a rival app. The range starts above every port this repo already speaks for:
// 34115 (`wails dev` default / `make dev`) and 34217 (the sibling dual-end runner, README §11).
const PORT_BLOCK_BASE = 34300;
const PORT_BLOCKS = 300;
const PORTS_PER_BLOCK = 4;
const blockStart =
  PORT_BLOCK_BASE + (parseInt(INSTANCE_ID.slice(0, 4), 16) % PORT_BLOCKS) * PORTS_PER_BLOCK;

function flavorPaths(flavor) {
  const root = join(INSTANCE_ROOT, flavor);
  return {
    dataDir: join(root, "data"),
    keychainDir: join(root, "keychain"),
    browserDir: join(root, "browser"),
    logFile: join(INSTANCE_ROOT, `${flavor}-webserver.log`),
  };
}

export const FLAVORS = {
  fake: {
    flavor: "fake",
    buildTags: ["e2e"],
    devserverPort: blockStart,
    cdpPort: blockStart + 1,
    ...flavorPaths("fake"),
  },
  real: {
    flavor: "real",
    buildTags: [],
    devserverPort: blockStart + 2,
    cdpPort: blockStart + 3,
    ...flavorPaths("real"),
  },
};

export const DEFAULT_FLAVOR = "fake";

/** The platform roots the installed app resolves to — mirrors paths.AppDataDir()'s default arm. */
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

/**
 * Throw unless `dir` is exactly one of this checkout's flavor data dirs. Allow-list rather than
 * deny-list: a new production-ish path must be added here on purpose and cannot leak in by
 * default. The installed root and the `-dev` root are named in the message because those are the
 * two a hand-written command actually lands on.
 */
export function assertIsolatedDataDir(dir) {
  const candidate = typeof dir === "string" ? resolve(dir.trim() || ".") : "";
  const sanctioned = Object.values(FLAVORS).map((f) => f.dataDir);
  if (sanctioned.includes(candidate)) return candidate;

  for (const root of productionRoots()) {
    if (within(candidate, root)) {
      throw new IsolationError(
        `refusing to touch the installed app's data dir: ${candidate}\n` +
          "Verification only ever runs against a throwaway dir — see docs/verification.md.",
      );
    }
    if (within(candidate, `${root}-dev`)) {
      throw new IsolationError(
        `refusing to touch your own dev data dir: ${candidate}\n` +
          "`make dev` holds your real sessions and projects; verification gets its own dir.",
      );
    }
  }
  throw new IsolationError(
    `not a sanctioned verification data dir for this checkout: ${candidate || "(empty)"}\n` +
      `Allowed: ${sanctioned.join(", ")}`,
  );
}

/** Throw unless `url` is served by this target's own bridge. */
export function assertSanctionedURL(target, url) {
  let parsed;
  try {
    parsed = new URL(url);
  } catch {
    throw new IsolationError(`not a URL: ${url}`);
  }
  const hostOK = parsed.hostname === "localhost" || parsed.hostname === "127.0.0.1";
  if (parsed.protocol !== "http:" || !hostOK || parsed.port !== String(target.devserverPort)) {
    throw new IsolationError(
      `refusing to drive ${url}: the ${target.flavor} target is only ${baseURL(target)}`,
    );
  }
  return parsed.toString();
}

export function baseURL(target) {
  return `http://localhost:${target.devserverPort}`;
}

export function resolveTarget(flavor = DEFAULT_FLAVOR) {
  const target = FLAVORS[flavor];
  if (!target) {
    throw new IsolationError(
      `unknown flavor: ${flavor} (known: ${Object.keys(FLAVORS).join(", ")})`,
    );
  }
  return {
    ...target,
    instanceId: INSTANCE_ID,
    baseURL: baseURL(target),
    dbPath: join(target.dataDir, "agentre.db"),
  };
}

export function sessionPath(flavor) {
  return join(INSTANCE_ROOT, `${flavor}.json`);
}

export function readSession(flavor) {
  const path = sessionPath(flavor);
  if (!existsSync(path)) return null;
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch {
    return null;
  }
}

export function writeSession(flavor, session) {
  mkdirSync(INSTANCE_ROOT, { recursive: true });
  writeFileSync(sessionPath(flavor), `${JSON.stringify(session, null, 2)}\n`);
}

export function clearSession(flavor) {
  rmSync(sessionPath(flavor), { force: true });
}

/**
 * Every flavor of THIS checkout that still has a session file. Teardown consults this before
 * reaping stray `vite` processes: the reap matches by repo path, so it cannot tell one flavor's
 * vite from another's, and killing the wrong one leaves a live app serving 502 with nothing
 * logged anywhere.
 */
export function liveSessions() {
  return Object.keys(FLAVORS)
    .map((name) => readSession(name))
    .filter(Boolean);
}

/** Reaping stray vite is only safe once no verification app is left in this checkout. */
export function shouldReapVite(sessions = liveSessions()) {
  return sessions.length === 0;
}

/**
 * A fresh (non-reuse) suite run wipes and re-seeds the data dir at start and deletes it at the
 * end — which would pull the ground out from under a `make verify-up` app on the same dir.
 * Returns the sessions that make that unsafe, so the caller can refuse instead.
 */
export function blocksFreshRun(sessions = liveSessions(), { reuse = false } = {}) {
  if (reuse) return [];
  return sessions.filter((s) => s.dataDir === FLAVORS.fake.dataDir);
}

/**
 * `wails dev` compiles into this checkout's `build/bin`, so a second flavor started here would
 * overwrite the binary the first one is running — the first app dies mid-run with nothing but a
 * "no such file or directory" line in its log. Concurrency belongs across worktrees, not within
 * one. Returns the sessions in the way.
 */
export function blocksSecondFlavor(flavor, sessions = liveSessions()) {
  return sessions.filter((s) => s.flavor !== flavor);
}

/** Create the isolated dirs a launch needs, with the keychain dir at the 0700 bootstrap requires. */
export function prepareDirs(target, { wipe }) {
  assertIsolatedDataDir(target.dataDir);
  if (wipe) {
    rmSync(target.dataDir, { recursive: true, force: true });
    rmSync(target.keychainDir, { recursive: true, force: true });
  }
  mkdirSync(target.dataDir, { recursive: true });
  mkdirSync(target.keychainDir, { recursive: true, mode: 0o700 });
  mkdirSync(target.browserDir, { recursive: true });
}

/** The env overrides that make a launched app hermetic. Every launcher uses exactly this set. */
export function launchEnv(target) {
  return {
    AGENTRE_DATA_DIR: target.dataDir,
    AGENTRE_ENV: "test",
    AGENTRE_KEYCHAIN_DIR: target.keychainDir,
    // The gateway's default 52401 is not data-dir-scoped: a running real Agentre holds it, and
    // without a free port every gateway round-trip (MCP tools, hooks) silently dies.
    AGENTRE_PROXY_PORT: "0",
  };
}

export function portListening(port, host = "127.0.0.1", timeoutMs = 1500) {
  return new Promise((res) => {
    const sock = connect({ host, port });
    const done = (ok) => {
      sock.destroy();
      res(ok);
    };
    sock.setTimeout(timeoutMs, () => done(false));
    sock.once("connect", () => done(true));
    sock.once("error", () => done(false));
  });
}

/** Whether this checkout's own port block is free to take. */
export function portFree(port) {
  return new Promise((res) => {
    const server = createServer();
    server.once("error", () => res(false));
    server.listen(port, "127.0.0.1", () => server.close(() => res(true)));
  });
}

/**
 * Resolve the live session for `flavor`, or — when no flavor is given — the single live one.
 * Refuses to guess between two live sessions: the wrong guess would drive the wrong app.
 */
export function requireLiveSession(flavor) {
  if (flavor) {
    const session = readSession(flavor);
    if (!session) {
      throw new IsolationError(
        `no ${flavor} verification app is up in this checkout. Start one: make verify-up FLAVOR=${flavor}`,
      );
    }
    return session;
  }
  const live = liveSessions();
  if (live.length === 0) {
    throw new IsolationError("no verification app is up in this checkout. Start one: make verify-up");
  }
  if (live.length > 1) {
    throw new IsolationError(
      `${live.length} verification apps are up (${live.map((s) => s.flavor).join(", ")}) — ` +
        "name one with --flavor.",
    );
  }
  return live[0];
}
