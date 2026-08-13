import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { existsSync, rmSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

import {
  VERIFY_VIEWPORT,
  applyVerificationViewport,
  verificationBrowserArgs,
} from "./browser.mjs";
import {
  INSTANCE_ID,
  INSTANCE_ROOT,
  IsolationError,
  REPO_ROOT,
  assertIsolatedDataDir,
  assertSanctionedURL,
  productionRoots,
  launchEnv,
  prepareDirs,
  resolveTarget,
  sessionPath,
} from "./target.mjs";

test("Given formal verification, when its target is resolved, then storage and ports belong only to this checkout", () => {
  const target = resolveTarget();
  for (const dir of [target.dataDir, target.keychainDir, target.browserDir]) {
    assert.ok(dir.startsWith(INSTANCE_ROOT), `${dir} must live under ${INSTANCE_ROOT}`);
  }
  assert.ok(INSTANCE_ROOT.startsWith(tmpdir()));
  assert.doesNotThrow(() => assertIsolatedDataDir(target.dataDir));
  assert.notEqual(target.devserverPort, 34115);
  assert.notEqual(target.cdpPort, 34115);
  assert.notEqual(target.devserverPort, target.cdpPort);
});

test("Given different worktrees, when target identities are derived, then they cannot share state", () => {
  assert.equal(INSTANCE_ID, createHash("sha1").update(REPO_ROOT).digest("hex").slice(0, 8));
  const other = createHash("sha1").update(`${REPO_ROOT}-worktree`).digest("hex").slice(0, 8);
  assert.notEqual(other, INSTANCE_ID);
  assert.ok(sessionPath().startsWith(INSTANCE_ROOT));
});

test("Given protected app roots, when verification resolves a data directory, then production, development, and arbitrary paths are rejected", () => {
  for (const root of productionRoots()) {
    assert.throws(() => assertIsolatedDataDir(root), IsolationError);
    assert.throws(() => assertIsolatedDataDir(join(root, "agentre.db")), IsolationError);
    assert.throws(() => assertIsolatedDataDir(`${root}-dev`), IsolationError);
  }
  assert.throws(() => assertIsolatedDataDir(join(homedir(), "somewhere-else")), IsolationError);
  assert.throws(() => assertIsolatedDataDir(""), IsolationError);
});

test("Given a live verification target, when a URL is checked, then only its own loopback bridge is accepted", () => {
  const target = resolveTarget();
  assert.doesNotThrow(() => assertSanctionedURL(target, `${target.baseURL}/`));
  assert.doesNotThrow(() =>
    assertSanctionedURL(target, `http://127.0.0.1:${target.devserverPort}/settings`),
  );
  assert.throws(() => assertSanctionedURL(target, "http://localhost:34115/"), IsolationError);
  assert.throws(() => assertSanctionedURL(target, "https://agentre.ai/"), IsolationError);
  assert.throws(() => assertSanctionedURL(target, "file:///etc/passwd"), IsolationError);
});

test("Given formal verification with stale E2E variables and missing isolated storage, when launch input is prepared, then private directories are created and E2E composition variables are absent", () => {
  const target = resolveTarget();
  for (const path of [target.dataDir, target.keychainDir, target.browserDir]) {
    rmSync(path, { recursive: true, force: true });
  }

  prepareDirs(target, { wipe: true });
  const env = launchEnv(target, {
    PATH: "/test/bin",
    AGENTRE_E2E_MANIFEST: "/tmp/stale-manifest",
    AGENTRE_E2E_REFRESH_TOKEN: "developer-secret",
  });

  assert.equal(env.PATH, "/test/bin");
  assert.equal(env.AGENTRE_DATA_DIR, target.dataDir);
  assert.equal(env.AGENTRE_KEYCHAIN_DIR, target.keychainDir);
  assert.equal(env.AGENTRE_E2E_MANIFEST, undefined);
  assert.equal(env.AGENTRE_E2E_REFRESH_TOKEN, undefined);
  for (const path of [target.dataDir, target.keychainDir, target.browserDir]) {
    assert.equal(existsSync(path), true);
  }
});

test("Given formal verification, when its browser and driven page are prepared, then evidence uses the established 1440x900 viewport in headless and headed modes", async () => {
  const calls = [];
  await applyVerificationViewport({
    setViewportSize(size) {
      calls.push(size);
    },
  });

  assert.deepEqual(VERIFY_VIEWPORT, { width: 1440, height: 900 });
  assert.deepEqual(calls, [{ width: 1440, height: 900 }]);

  const headless = verificationBrowserArgs({
    cdpPort: 34301,
    browserDir: "/tmp/agentre-browser",
    headless: true,
  });
  assert.ok(headless.includes("--window-size=1440,900"));
  assert.ok(headless.includes("--headless=new"));

  const headed = verificationBrowserArgs({
    cdpPort: 34301,
    browserDir: "/tmp/agentre-browser",
    headless: false,
  });
  assert.ok(headed.includes("--window-size=1440,900"));
  assert.ok(!headed.includes("--headless=new"));
});
