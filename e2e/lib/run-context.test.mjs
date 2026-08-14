import assert from "node:assert/strict";
import { access, mkdir, readFile, rm, stat, symlink, writeFile } from "node:fs/promises";
import { basename, dirname, join } from "node:path";
import { EventEmitter } from "node:events";
import { test } from "node:test";

import {
  ProcessSupervisor,
  appEnvironment,
  assertRemoteDeviceCredentialPersisted,
  createRunContext,
  fakePeerEnvironment,
  playwrightEnvironment,
  preserveFailureArtifacts,
  reserveDistinctLoopbackPorts,
} from "./run-context.mjs";

test("Given two launches, when run contexts are created, then each gets private random storage, token, manifest, and loopback dynamic ports", async (t) => {
  const first = await createRunContext();
  const second = await createRunContext();
  t.after(async () => {
    await first.remove();
    await second.remove();
  });

  assert.notEqual(first.runRoot, second.runRoot);
  assert.notEqual(first.token, second.token);
  assert.notEqual(first.port, second.port);
  assert.notEqual(first.vitePort, second.vitePort);
  assert.notEqual(first.port, first.vitePort);
  assert.match(first.baseURL, /^http:\/\/127\.0\.0\.1:\d+$/);
  assert.match(first.viteURL, /^http:\/\/127\.0\.0\.1:\d+$/);

  const manifest = JSON.parse(await readFile(first.manifestPath, "utf8"));
  assert.deepEqual(manifest, {
    runRoot: first.runRoot,
    dataDir: first.dataDir,
    keychainDir: first.keychainDir,
    token: first.token,
  });
  assert.equal(first.env.AGENTRE_E2E_MANIFEST, first.manifestPath);
  assert.equal(first.env.AGENTRE_E2E_TOKEN, first.token);
  assert.equal(first.env.AGENTRE_DATA_DIR, first.dataDir);
  assert.equal(first.env.AGENTRE_KEYCHAIN_DIR, first.keychainDir);
  assert.equal(first.env.AGENTRE_PROXY_PORT, "0");
  assert.equal(first.env.AGENTRE_RUNTIME_MODE, undefined);

  if (process.platform !== "win32") {
    for (const dir of [first.runRoot, first.dataDir, first.keychainDir]) {
      assert.equal((await stat(dir)).mode & 0o077, 0, `${dir} must be private`);
    }
  }
});

test("Given the OS offers the same released port twice, when one run allocates its app and Vite ports, then the allocator retries until they are distinct", async () => {
  const offered = [41000, 41000, 41001];
  const ports = await reserveDistinctLoopbackPorts(async () => offered.shift());

  assert.deepEqual(ports, [41000, 41001]);
});

test("Given a private run and external E2E credentials in the parent, when the app is launched, then only this run's manifest, token, and storage overrides are inherited", async (t) => {
  const run = await createRunContext();
  t.after(() => run.remove());

  const env = appEnvironment(run, {
    PATH: "/test/bin",
    AGENTRE_E2E_SERVER_URL: "https://real.example.invalid",
    AGENTRE_E2E_REFRESH_TOKEN: "real-secret",
    AGENTRE_E2E_TOKEN: "stale-token",
  });

  assert.equal(env.PATH, "/test/bin");
  assert.equal(env.AGENTRE_E2E_SERVER_URL, undefined);
  assert.equal(env.AGENTRE_E2E_REFRESH_TOKEN, undefined);
  assert.equal(env.AGENTRE_E2E_TOKEN, run.token);
  assert.equal(env.AGENTRE_E2E_MANIFEST, run.manifestPath);
  assert.equal(env.AGENTRE_DATA_DIR, run.dataDir);
  assert.equal(env.AGENTRE_KEYCHAIN_DIR, run.keychainDir);
});

test("Given the remote fake needs a control secret, when process environments are built, then only the fake peer receives it", async (t) => {
  const run = await createRunContext();
  t.after(() => run.remove());

  const appEnv = appEnvironment(run, { PATH: "/test/bin" });
  const peerEnv = fakePeerEnvironment(run, { PATH: "/test/bin" });

  assert.equal(appEnv.AGENTRE_E2E_CONTROL_TOKEN, undefined);
  assert.equal(peerEnv.AGENTRE_E2E_CONTROL_TOKEN, run.controlToken);
});

test("Given a private run and a secret-bearing parent environment, when Playwright is launched, then browser state stays in the run root and the app token is not inherited", async (t) => {
  const run = await createRunContext();
  t.after(() => run.remove());

  const env = playwrightEnvironment(run, {
    PATH: "/test/bin",
    AGENTRE_ENV: "test",
    AGENTRE_PROXY_PORT: "0",
    AGENTRE_E2E_TOKEN: "must-not-reach-playwright",
    AGENTRE_E2E_REFRESH_TOKEN: "developer-refresh-token",
    AGENTRE_E2E_SERVER_URL: "https://developer-server.invalid",
  });

  assert.equal(env.PATH, "/test/bin");
  assert.equal(env.AGENTRE_DATA_DIR, run.dataDir);
  assert.equal(env.AGENTRE_E2E_BASE_URL, run.baseURL);
  assert.equal(env.AGENTRE_E2E_RUN_ID, basename(run.runRoot));
  assert.equal(env.AGENTRE_E2E_PLAYWRIGHT_DIR, run.playwrightDir);
  assert.equal(env.TMPDIR, run.browserDir);
  assert.equal(env.TMP, run.browserDir);
  assert.equal(env.TEMP, run.browserDir);
  assert.equal(env.AGENTRE_E2E_TOKEN, undefined);
  assert.equal(env.AGENTRE_E2E_REFRESH_TOKEN, undefined);
  assert.equal(env.AGENTRE_E2E_SERVER_URL, undefined);
  assert.equal(env.AGENTRE_E2E_MANIFEST, undefined);
  assert.equal(env.AGENTRE_KEYCHAIN_DIR, undefined);
  assert.equal(env.AGENTRE_ENV, undefined);
  assert.equal(env.AGENTRE_PROXY_PORT, undefined);
});

test("Given remote credential evidence context is unavailable, when persistence is checked, then the failure exposes no path or token and directs callers to make e2e", async () => {
  const secret = "unavailable-context-token-must-not-leak";
  const secretPath = join("private", secret, "keychain");

  await assert.rejects(
    assertRemoteDeviceCredentialPersisted({
      keychainDir: secretPath,
      remoteIdentity: { deviceToken: secret },
    }),
    (error) => assertSafeCredentialEvidenceError(error, secretPath, secret),
  );
});

test("Given remote credential paths cannot be canonicalized, when persistence is checked, then the failure exposes no path or token and directs callers to make e2e", async (t) => {
  const run = await createRunContext();
  t.after(() => run.remove());
  const secret = `canonicalization-token-${run.token}`;
  const missingKeychainDir = join(run.runRoot, secret, "missing-keychain");
  run.keychainDir = missingKeychainDir;
  run.remoteIdentity = { deviceToken: secret };

  await assert.rejects(
    assertRemoteDeviceCredentialPersisted(run),
    (error) => assertSafeCredentialEvidenceError(error, missingKeychainDir, secret),
  );
});

test("Given no persisted remote credential matches, when persistence is checked, then the failure exposes no token and directs callers to make e2e", async (t) => {
  const run = await createRunContext();
  t.after(() => run.remove());
  run.remoteIdentity = { deviceToken: `missing-remote-device-${run.token}` };
  await writeFile(join(run.keychainDir, "wrong-credential"), "wrong-token");

  await assert.rejects(
    assertRemoteDeviceCredentialPersisted(run),
    (error) => assertSafeCredentialEvidenceError(error, run.remoteIdentity.deviceToken),
  );
});

test("Given a generated remote credential, when runner-owned file-keychain evidence is checked, then only a matching regular file inside the run keychain passes without exposing the token", async (t) => {
  const run = await createRunContext();
  t.after(() => run.remove());
  run.remoteIdentity = { deviceToken: `remote-device-${run.token}` };

  await writeFile(join(run.runRoot, "outside-keychain"), run.remoteIdentity.deviceToken);
  await assert.rejects(
    assertRemoteDeviceCredentialPersisted(run),
    (error) => assertSafeCredentialEvidenceError(error, run.remoteIdentity.deviceToken),
  );

  await writeFile(join(run.keychainDir, "wrong-credential"), "wrong-token");
  await assert.rejects(
    assertRemoteDeviceCredentialPersisted(run),
    (error) => assertSafeCredentialEvidenceError(error, run.remoteIdentity.deviceToken),
  );

  await writeFile(join(run.keychainDir, "remote-device-credential"), run.remoteIdentity.deviceToken);
  await assert.doesNotReject(assertRemoteDeviceCredentialPersisted(run));
});

test("Given an outside keychain directory containing the generated credential, when persistence evidence is checked, then the runner fails closed without disclosing the path or token", async (t) => {
  const run = await createRunContext();
  const outsideDir = join(dirname(run.runRoot), `${basename(run.runRoot)}-outside-keychain`);
  t.after(async () => {
    await run.remove();
    await rm(outsideDir, { recursive: true, force: true });
  });
  await mkdir(outsideDir, { recursive: true, mode: 0o700 });
  run.remoteIdentity = { deviceToken: `outside-remote-device-${run.token}` };
  await writeFile(join(outsideDir, "remote-device-credential"), run.remoteIdentity.deviceToken);
  run.keychainDir = outsideDir;

  await assert.rejects(
    assertRemoteDeviceCredentialPersisted(run),
    (error) =>
      assertSafeCredentialEvidenceError(error, outsideDir, run.remoteIdentity.deviceToken),
  );
});

test("Given a run-root keychain symlink escaping to a credential outside the run, when persistence evidence is checked, then the runner fails closed without disclosing the path or token", async (t) => {
  const run = await createRunContext();
  const outsideDir = join(dirname(run.runRoot), `${basename(run.runRoot)}-symlink-target`);
  t.after(async () => {
    await run.remove();
    await rm(outsideDir, { recursive: true, force: true });
  });
  await mkdir(outsideDir, { recursive: true, mode: 0o700 });
  run.remoteIdentity = { deviceToken: `symlink-remote-device-${run.token}` };
  await writeFile(join(outsideDir, "remote-device-credential"), run.remoteIdentity.deviceToken);
  const escapeLink = join(run.runRoot, "keychain-escape");
  try {
    await symlink(outsideDir, escapeLink, "dir");
  } catch (error) {
    t.skip(`directory symlink unavailable: ${error.code ?? "unknown error"}`);
    return;
  }
  run.keychainDir = escapeLink;

  await assert.rejects(
    assertRemoteDeviceCredentialPersisted(run),
    (error) =>
      assertSafeCredentialEvidenceError(error, outsideDir, run.remoteIdentity.deviceToken),
  );
});

function assertSafeCredentialEvidenceError(error, ...sensitiveValues) {
  assert.match(error.message, /runner file keychain/);
  assert.match(error.message, /make e2e/);
  for (const sensitive of sensitiveValues) {
    if (sensitive) assert.equal(error.message.includes(sensitive), false);
  }
  return true;
}

test("Given supervised children and an unrelated process, when cleanup runs, then only recorded child trees are terminated once", async () => {
  const calls = [];
  const supervisor = new ProcessSupervisor(async (pid) => calls.push(pid));
  supervisor.track({ pid: 101 });
  supervisor.track({ pid: 202 });

  await supervisor.stopAll();
  await supervisor.stopAll();

  assert.deepEqual(calls.sort((a, b) => a - b), [101, 202]);
  assert.equal(calls.includes(999), false);
});

test("Given a supervised child that has already exited, when cleanup runs, then its recycled PID is not terminated", async () => {
  const calls = [];
  const supervisor = new ProcessSupervisor(async (pid) => calls.push(pid));
  const child = new EventEmitter();
  child.pid = 303;
  supervisor.track(child);
  child.emit("exit", 0, null);

  await supervisor.stopAll();

  assert.deepEqual(calls, []);
});

test("Given one supervised process tree cannot be terminated, when cleanup runs, then every child is attempted and the cleanup failure is reported", async () => {
  const calls = [];
  const supervisor = new ProcessSupervisor(async (pid) => {
    calls.push(pid);
    if (pid === 404) throw new Error("permission denied");
  });
  supervisor.track({ pid: 404 });
  supervisor.track({ pid: 505 });

  await assert.rejects(supervisor.stopAll(), /failed to terminate 1 supervised process tree/);
  await assert.rejects(supervisor.stopAll(), /failed to terminate 1 supervised process tree/);

  assert.deepEqual(calls.sort((a, b) => a - b), [404, 505]);
});

test("Given a failed run with known text secrets and unsafe state, when evidence is preserved, then secrets are redacted and token-bearing state and files are removed", async (t) => {
  const run = await createRunContext();
  const artifactRoot = join(dirname(run.runRoot), `${basename(run.runRoot)}-artifacts`);
  t.after(async () => {
    await run.remove();
    await rm(artifactRoot, { recursive: true, force: true });
  });
  await mkdir(run.logsDir, { recursive: true });
  run.syncIdentity = { refreshToken: "generated-sync-refresh" };
  run.remoteIdentity = { deviceToken: "generated-remote-device-token" };
  await writeFile(
    join(run.logsDir, "app.log"),
    `safe ${run.token} ${run.controlToken} ${run.syncIdentity.refreshToken} ${run.remoteIdentity.deviceToken} safe`,
  );
  await writeFile(join(run.keychainDir, "secret-entry"), "must-not-be-retained");
  await writeFile(join(run.browserDir, "browser-secret.json"), "must-not-be-retained");
  await writeFile(join(run.runRoot, "go-overlay.json"), "must-not-be-retained");
  const desktopControlToken = "d".repeat(43);
  await writeFile(
    join(run.dataDir, "ctl-endpoint.json"),
    `${JSON.stringify({ token: desktopControlToken })}\n`,
  );

  const preserved = await preserveFailureArtifacts(run, artifactRoot);
  await assert.rejects(access(run.runRoot), { code: "ENOENT" });
  const log = await readFile(join(preserved, "logs", "app.log"), "utf8");
  const manifest = await readFile(join(preserved, "manifest.json"), "utf8");
  for (const secret of [
    run.token,
    run.controlToken,
    run.syncIdentity.refreshToken,
    run.remoteIdentity.deviceToken,
  ]) {
    assert.equal(log.includes(secret), false);
  }
  assert.equal(manifest.includes(run.token), false);
  assert.match(log, /\[REDACTED\]/);
  await assert.rejects(access(join(preserved, "data", "ctl-endpoint.json")), {
    code: "ENOENT",
  });
  for (const path of [
    join(preserved, "keychain"),
    join(preserved, "browser"),
    join(preserved, "go-overlay.json"),
  ]) {
    await assert.rejects(access(path), { code: "ENOENT" });
  }
});
