// Guard tests for the verification target contract. These pin the two rules the whole
// local-verification route rests on: a driven app never resolves to production state, and two
// checkouts never resolve to the same state.
//
// Run: cd e2e && pnpm run test:guards   (also run by run-e2e.mjs before any app is launched)
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

import {
  FLAVORS,
  INSTANCE_ID,
  INSTANCE_ROOT,
  IsolationError,
  REPO_ROOT,
  assertIsolatedDataDir,
  assertSanctionedURL,
  blocksFreshRun,
  blocksSecondFlavor,
  productionRoots,
  resolveTarget,
  sessionPath,
  shouldReapVite,
} from "./target.mjs";

test("every flavor resolves to an isolated temp dir under this checkout's own root", () => {
  for (const name of Object.keys(FLAVORS)) {
    const target = resolveTarget(name);
    assert.equal(target.flavor, name);
    for (const dir of [target.dataDir, target.keychainDir, target.browserDir]) {
      assert.ok(dir.startsWith(INSTANCE_ROOT), `${name}: ${dir} must live under ${INSTANCE_ROOT}`);
    }
    assert.ok(INSTANCE_ROOT.startsWith(tmpdir()));
    assert.doesNotThrow(() => assertIsolatedDataDir(target.dataDir));
  }
});

test("the instance id is derived from this checkout's path, so worktrees never share state", () => {
  // A worktree is a different absolute path, so it lands on a different id — and therefore a
  // different data dir, keychain, log, session file and port block.
  assert.equal(INSTANCE_ID, createHash("sha1").update(REPO_ROOT).digest("hex").slice(0, 8));
  assert.equal(INSTANCE_ID.length, 8);
  assert.ok(INSTANCE_ROOT.endsWith(join("agentre-verify", INSTANCE_ID)));
  const other = createHash("sha1").update(`${REPO_ROOT}-worktree`).digest("hex").slice(0, 8);
  assert.notEqual(other, INSTANCE_ID);
});

test("ports are unique per flavor and clear of the ports this repo already speaks for", () => {
  const ports = Object.values(FLAVORS).flatMap((f) => [f.devserverPort, f.cdpPort]);
  assert.equal(new Set(ports).size, ports.length, "flavor ports must be unique");
  for (const port of ports) {
    // 34115 = `wails dev` default / `make dev`; 34217 = the sibling agentre-server dual-end
    // runner (e2e/README.md §11). Landing on either would drive the wrong app.
    assert.notEqual(port, 34115);
    assert.notEqual(port, 34217);
    assert.ok(port >= 34300 && port < 35500, `${port} outside the reserved verification block`);
  }
});

test("the real flavor drops the e2e build tag and keeps its own dirs and ports", () => {
  const real = resolveTarget("real");
  const fake = resolveTarget("fake");
  assert.deepEqual(real.buildTags, []);
  assert.deepEqual(fake.buildTags, ["e2e"]);
  assert.notEqual(real.dataDir, fake.dataDir);
  assert.notEqual(real.keychainDir, fake.keychainDir);
  assert.notEqual(real.devserverPort, fake.devserverPort);
});

test("an unknown flavor is rejected instead of silently defaulting", () => {
  assert.throws(() => resolveTarget("prod"), /unknown flavor/i);
});

test("assertIsolatedDataDir rejects the production data root and anything inside it", () => {
  for (const root of productionRoots()) {
    assert.throws(() => assertIsolatedDataDir(root), IsolationError);
    assert.throws(() => assertIsolatedDataDir(join(root, "agentre.db")), IsolationError);
  }
});

test("assertIsolatedDataDir rejects the developer's own dev data dir", () => {
  // `make dev` writes <config>/agentre-dev: isolated from the installed app, but it holds the
  // developer's real sessions and projects. A verification run gets its own throwaway dir.
  for (const root of productionRoots()) {
    assert.throws(() => assertIsolatedDataDir(`${root}-dev`), IsolationError);
  }
});

test("assertIsolatedDataDir rejects any path that is not a flavor dir of THIS checkout", () => {
  assert.throws(() => assertIsolatedDataDir(join(homedir(), "somewhere-else")), IsolationError);
  assert.throws(() => assertIsolatedDataDir(""), IsolationError);
  // Another checkout's dir is off limits too: that app is not the one this process may touch.
  const otherInstance = join(tmpdir(), "agentre-verify", "deadbeef", "fake", "data");
  assert.throws(() => assertIsolatedDataDir(otherInstance), IsolationError);
});

test("assertSanctionedURL only allows the flavor's own bridge origin", () => {
  const target = resolveTarget("fake");
  assert.doesNotThrow(() => assertSanctionedURL(target, `${target.baseURL}/`));
  assert.doesNotThrow(() =>
    assertSanctionedURL(target, `http://127.0.0.1:${target.devserverPort}/settings`),
  );
  assert.throws(() => assertSanctionedURL(target, "http://localhost:34115/"), IsolationError);
  // The sibling flavor is a different app with different state: driving it by accident is the
  // whole failure this guard exists for.
  assert.throws(
    () => assertSanctionedURL(target, `http://localhost:${FLAVORS.real.devserverPort}/`),
    IsolationError,
  );
  assert.throws(() => assertSanctionedURL(target, "https://agentre.ai/"), IsolationError);
  assert.throws(() => assertSanctionedURL(target, "file:///etc/passwd"), IsolationError);
});

test("stray vite is only reaped once no verification app is left in this checkout", () => {
  // The reap matches vite by repo path and cannot tell one flavor's dev server from another's:
  // reaping while a second app is up leaves it listening but serving 502, with nothing logged.
  assert.equal(shouldReapVite([]), true);
  assert.equal(shouldReapVite([{ flavor: "fake" }]), false);
  assert.equal(shouldReapVite([{ flavor: "fake" }, { flavor: "real" }]), false);
});

test("a fresh suite run is refused while an app lives on the same data dir", () => {
  const fake = { flavor: "fake", dataDir: FLAVORS.fake.dataDir };
  const real = { flavor: "real", dataDir: FLAVORS.real.dataDir };
  assert.deepEqual(blocksFreshRun([], {}), []);
  assert.deepEqual(blocksFreshRun([fake], {}), [fake]);
  assert.deepEqual(blocksFreshRun([real], {}), [], "another dir is not in the way");
  assert.deepEqual(blocksFreshRun([fake], { reuse: true }), [], "reuse mode never wipes");
});

test("a second flavor in the same checkout is refused — they share build/bin", () => {
  // `wails dev` compiles into this checkout's build/bin; starting the second flavor overwrites
  // the binary the first is running and kills it. Run the second flavor in another worktree.
  const fake = { flavor: "fake" };
  assert.deepEqual(blocksSecondFlavor("fake", []), []);
  assert.deepEqual(blocksSecondFlavor("fake", [fake]), [], "the same flavor is a restart, not a rival");
  assert.deepEqual(blocksSecondFlavor("real", [fake]), [fake]);
});

test("each flavor's session handshake file is its own, under this checkout's root", () => {
  assert.notEqual(sessionPath("fake"), sessionPath("real"));
  assert.ok(sessionPath("fake").startsWith(INSTANCE_ROOT));
});
