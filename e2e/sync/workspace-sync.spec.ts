// Local end-to-end for workspace multi-device sync
// (docs/specs/2026-08-07-workspace-sync.md, test seam 「本地端到端」).
//
// Cast:
//   - one REAL desktop app (`wails dev -tags e2e`), seeded as a logged-in device
//   - one real `agentre-server` process on the developer's PostgreSQL + Redis
//   - two Go simulated peers (agentre-server/cmd/synce2e): a linux peer that has
//     paired the build box, and a Windows peer that reports `C:\…` local paths
//
// The specs run in order and build on each other's rows: `alpha` is created in
// the first one and deleted in the last.
import { expect, test, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  activeProjectCountBySyncID,
  agentCountBySyncID,
  agentredFingerprint,
  backendCountBySyncID,
  cutNetwork,
  deferredInboundKinds,
  deviceIDs,
  execTargetCountBySyncID,
  outboundQueueCount,
  peer,
  projectBySyncID,
  projectByName,
  projectLocationsByProjectID,
  restoreNetwork,
  serverLocalPaths,
  waitFor,
  windowsPeer,
} from "../fixtures/sync";

test.describe.configure({ mode: "serial" });

const stamp = Date.now().toString(36);
const ALPHA = `sync-alpha-${stamp}`;
const BETA = `sync-beta-${stamp}`;
const alphaPath = join(tmpdir(), `agentre-e2e-sync-${ALPHA}`);
const betaPath = join(tmpdir(), `agentre-e2e-sync-${BETA}`);

let alphaSyncID = "";
let alphaProjectID = 0;

// The peer-authored org rows of ⑥, shared by the two specs that check them from
// each end.
const AGENT_SYNC_ID = `peer-agent-${stamp}`;
const REMOTE_BACKEND_SYNC_ID = `peer-backend-remote-${stamp}`;
const LOCAL_BACKEND_SYNC_ID = `peer-backend-local-${stamp}`;
const FIRST_TARGET_SYNC_ID = `peer-target-0-${stamp}`;
const SECOND_TARGET_SYNC_ID = `peer-target-1-${stamp}`;

// A synced object's payload as the peer received it.
const nameOf = (payload: Record<string, unknown>) => String(payload.name ?? "");

// ── driving the real desktop ────────────────────────────────────────────────

async function createProjectViaUI(page: Page, name: string, path: string) {
  mkdirSync(path, { recursive: true });
  await page.getByTestId("nav-projects").click();
  await page.getByRole("button", { name: "New Project" }).first().click();
  const dialog = page.getByRole("dialog");
  await dialog.getByPlaceholder("/Users/you/Code/your-repo").fill(path);
  await dialog.getByPlaceholder("Agentre").fill(name);
  await dialog.getByRole("button", { name: "Create Project" }).click();
  await expect(dialog).toBeHidden();
}

// renameProject / deleteProject go through the app's real Wails bindings rather
// than the settings drawer: the binding layer, services, repositories, SQLite and
// the sync engine are all the real thing — only the form pixels are skipped.
async function renameProject(page: Page, id: number, name: string) {
  await page.evaluate(
    ([projectID, newName]) =>
      (window as never as WailsWindow).go.app.App.ProjectUpdate({
        id: projectID as number,
        name: newName as string,
        icon: "",
        color: "",
        description: "",
      }),
    [id, name] as const,
  );
}

async function deleteProject(page: Page, id: number) {
  await page.evaluate(
    (projectID) => (window as never as WailsWindow).go.app.App.ProjectDelete(projectID),
    id,
  );
}

async function syncNow(page: Page) {
  await page.evaluate(() => (window as never as WailsWindow).go.app.App.SyncNow());
}

async function syncStatus(page: Page) {
  return page.evaluate(() => (window as never as WailsWindow).go.app.App.SyncStatus());
}

type WailsWindow = {
  go: {
    app: {
      App: {
        ProjectUpdate(req: Record<string, unknown>): Promise<unknown>;
        ProjectDelete(id: number): Promise<unknown>;
        SyncNow(): Promise<unknown>;
        SyncStatus(): Promise<{ Enabled: boolean; AccountID: number; LastError: string }>;
      };
    };
  };
};

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("nav-projects")).toBeVisible();
});

// ── ① one edit crosses to the other end ─────────────────────────────────────

test("an edit made on the real desktop reaches the peer through the real server", async ({
  page,
}) => {
  // Precondition: the seeded credential really works — ServerBoot traded the
  // refresh token for an access token over real HTTP.
  const status = await syncStatus(page);
  expect(status.Enabled, "the desktop must come up logged in").toBe(true);
  expect(status.AccountID).toBeGreaterThan(0);

  await createProjectViaUI(page, ALPHA, alphaPath);
  await expect(page.getByText(ALPHA, { exact: true }).first()).toBeVisible();

  const local = projectByName(ALPHA);
  expect(local, "the project must exist in the desktop's own DB").toBeTruthy();
  expect(local!.sync_id).not.toBe("");
  alphaSyncID = local!.sync_id;
  alphaProjectID = local!.id;

  const remote = await waitFor(`the peer to receive ${ALPHA}`, () => {
    peer().sync();
    return peer().object(alphaSyncID);
  });
  expect(nameOf(remote.payload)).toBe(ALPHA);
  // R9 / 决策 6: the local path is reported, never synced — it must not ride along.
  expect(Object.keys(remote.payload)).not.toContain("path");
  expect(JSON.stringify(remote.payload)).not.toContain(alphaPath);
});

// ── ② concurrent edits of the same row converge ─────────────────────────────

test("two ends editing the same row concurrently converge on the later arrival", async ({
  page,
}) => {
  const base = peer().object(alphaSyncID)!.version;
  const desktopName = `${ALPHA}-from-desktop`;
  const peerName = `${ALPHA}-from-peer`;

  // Both ends edit from the same base version, unaware of each other. The desktop
  // arrives first; the peer's push is judged a conflict (R4a) yet still wins,
  // because 「同步版本号较大者胜」is decided by arrival at the server (R4).
  await renameProject(page, alphaProjectID, desktopName);
  const accepted = await waitFor("the desktop's rename to reach the server", () => {
    peer().sync();
    const obj = peer().object(alphaSyncID);
    return obj && nameOf(obj.payload) === desktopName ? obj : undefined;
  });
  expect(accepted.version).toBeGreaterThan(base);

  const pushed = peer().push({
    kind: "project",
    syncID: alphaSyncID,
    baseVersion: base,
    payload: { name: peerName, icon: "", color: "", description: "", sort_order: 0 },
  });
  const result = pushed.results[0];
  expect(result.status, "a stale base version must be reported as a conflict").toBe("conflict");
  expect(result.overwritten_version).toBe(accepted.version);
  expect(result.overwritten_device_id).toBe(deviceIDs().desktop);

  // Convergence: the desktop pulls the peer's version and both ends agree.
  const converged = await waitFor("the desktop to converge on the peer's version", async () => {
    await syncNow(page);
    const row = projectBySyncID(alphaSyncID);
    return row && row.name === peerName ? row : undefined;
  });
  expect(converged.name).toBe(peerName);
  expect(nameOf(peer().object(alphaSyncID)!.payload)).toBe(peerName);
  expect(converged.sync_version).toBe(peer().object(alphaSyncID)!.version);
});

// ── ⑥ the receiving end resolves the target list by its OWN pairing state ───

test("an Agent's ordered exec targets resolve differently per end's pairing state", async () => {
  const fingerprint = agentredFingerprint();
  const backendPayload = (name: string) => ({
    type: "claudecode",
    name,
    provider_key: "",
    cli_path: "",
    model_routes: "",
    sandbox: "",
    approval: "",
    env_json: "",
    reasoning_effort: "",
    default_permission_mode: "",
    default_model: "",
    openclaw_gateway_url: "",
    openclaw_agent_id: "",
    openclaw_default_model: "",
    openclaw_session_mode: "",
  });

  peer().push({
    kind: "agent",
    syncID: AGENT_SYNC_ID,
    payload: {
      name: `Peer Agent ${stamp}`,
      description: "",
      avatar_color: "blue",
      avatar_icon: "bot",
      system_badge: "",
      sort_order: 0,
      prompt_json: "",
      tools_json: "",
      pinned: false,
    },
  });
  // The first backend lives on an agentred the peer has paired and the desktop
  // has not — that asymmetry is the whole point of R2b.
  peer().push({
    kind: "agent_backend",
    syncID: REMOTE_BACKEND_SYNC_ID,
    fingerprint,
    payload: backendPayload(`Build Box ${stamp}`),
  });
  peer().push({
    kind: "agent_backend",
    syncID: LOCAL_BACKEND_SYNC_ID,
    payload: backendPayload(`This Machine ${stamp}`),
  });
  peer().push({
    kind: "agent_exec_target",
    syncID: FIRST_TARGET_SYNC_ID,
    payload: {
      agent_sync_id: AGENT_SYNC_ID,
      backend_sync_id: REMOTE_BACKEND_SYNC_ID,
      sort_order: 0,
      skills_json: "",
    },
  });
  peer().push({
    kind: "agent_exec_target",
    syncID: SECOND_TARGET_SYNC_ID,
    payload: {
      agent_sync_id: AGENT_SYNC_ID,
      backend_sync_id: LOCAL_BACKEND_SYNC_ID,
      sort_order: 1,
      skills_json: "",
    },
  });
  // R2b's other half: a project path that lives on that same unpaired agentred.
  peer().push({
    kind: "project_location",
    syncID: `peer-location-${stamp}`,
    projectSyncID: alphaSyncID,
    fingerprint,
    payload: { path: "/srv/build/alpha" },
  });

  // Paired end: takes the first target. Unpaired end: skips it (R15).
  const paired = peer().execTargets(AGENT_SYNC_ID, [fingerprint]);
  expect(paired.picked?.sync_id).toBe(FIRST_TARGET_SYNC_ID);
  expect(paired.picked?.fingerprint).toBe(fingerprint);

  const unpaired = peer().execTargets(AGENT_SYNC_ID, []);
  expect(unpaired.picked?.sync_id).toBe(SECOND_TARGET_SYNC_ID);
  expect(unpaired.targets[0].available).toBe(false);
  expect(unpaired.targets[0].reason).toBe("unpaired");
});

test("the real desktop, which paired nothing, defers the agentred-bound rows", async ({ page }) => {
  await waitFor("the desktop to land the peer's Agent", async () => {
    await syncNow(page);
    return agentCountBySyncID(AGENT_SYNC_ID) === 1 || undefined;
  });

  // The local-machine backend + its target land; the agentred-bound backend and
  // the target that points at it stay parked, because this desktop has no pairing
  // row for that fingerprint (R2b).
  expect(backendCountBySyncID(LOCAL_BACKEND_SYNC_ID)).toBe(1);
  expect(execTargetCountBySyncID(SECOND_TARGET_SYNC_ID)).toBe(1);
  expect(backendCountBySyncID(REMOTE_BACKEND_SYNC_ID)).toBe(0);
  expect(execTargetCountBySyncID(FIRST_TARGET_SYNC_ID)).toBe(0);
  const deferred = deferredInboundKinds().map((row) => row.entity_sync_id);
  expect(deferred).toContain(REMOTE_BACKEND_SYNC_ID);
  expect(deferred).toContain(FIRST_TARGET_SYNC_ID);

  // The path on that unpaired machine is kept but not resolved to a local
  // pairing row — it does not become "a path on this desktop" (决策 26).
  const locations = projectLocationsByProjectID(alphaProjectID);
  const onBuildBox = locations.find((row) => row.daemon_fingerprint === agentredFingerprint());
  expect(onBuildBox, "the location row itself is kept").toBeTruthy();
  expect(onBuildBox!.device_id, "but it resolves to no local agentred").toBe("");
});

// ── ⑤ the desktop's local-path snapshot matches the server's list ───────────

test("the desktop's local paths show up on the server exactly as they are locally", async () => {
  const local = projectBySyncID(alphaSyncID)!;
  expect(local.path).toBe(alphaPath);

  // R16 rides the 30 s ticker only — a local edit deliberately does NOT trigger it.
  const mine = await waitFor(
    "the desktop's local-path snapshot to reach the server",
    () => {
      const rows = serverLocalPaths().filter((row) => row.device_id === deviceIDs().desktop);
      return rows.length ? rows : undefined;
    },
    { timeoutMs: 90_000, intervalMs: 2_000 },
  );
  const alphaRow = mine.find((row) => row.project_sync_id === alphaSyncID);
  expect(alphaRow, "the server list must contain the project this desktop configured").toBeTruthy();
  expect(alphaRow!.path).toBe(local.path);
});

// ── ⑦ a Windows peer's C:\ paths stay in its own namespace ──────────────────

test("a Windows peer reporting C:\\ paths does not cross into this machine's list", async () => {
  const windowsPath = `C:\\Users\\e2e\\${ALPHA}`;
  windowsPeer().reportLocalPaths([{ project_sync_id: alphaSyncID, path: windowsPath }]);

  const rows = await waitFor("the Windows peer's report to land", () => {
    const all = serverLocalPaths();
    return all.some((row) => row.device_id === deviceIDs().peerwin) ? all : undefined;
  });
  const win = rows.filter((row) => row.device_id === deviceIDs().peerwin);
  const mine = rows.filter((row) => row.device_id === deviceIDs().desktop);
  expect(win).toEqual([
    { device_id: deviceIDs().peerwin, project_sync_id: alphaSyncID, path: windowsPath },
  ]);
  expect(mine.map((row) => row.path)).toContain(alphaPath);
  expect(mine.map((row) => row.path)).not.toContain(windowsPath);

  // R17: reporting writes nothing back into the desktop, so its own path is untouched.
  expect(projectBySyncID(alphaSyncID)!.path).toBe(alphaPath);
});

// ── ④ an edit made while the server is unreachable is pushed after reconnect ─

test("an edit made while the server is unreachable is pushed once it returns", async ({ page }) => {
  cutNetwork();
  try {
    await createProjectViaUI(page, BETA, betaPath);
    const local = await waitFor("beta to exist locally", () => projectByName(BETA));
    expect(local.sync_id).not.toBe("");

    // R8: the local write succeeded regardless; R7: it is queued, not lost.
    await syncNow(page).catch(() => undefined);
    expect(outboundQueueCount()).toBeGreaterThan(0);
    peer().sync();
    expect(
      peer().object(local.sync_id),
      "nothing can reach the peer while the server is unreachable",
    ).toBeUndefined();

    restoreNetwork();
    const arrived = await waitFor("beta to reach the peer after reconnect", async () => {
      await syncNow(page).catch(() => undefined);
      peer().sync();
      return peer().object(local.sync_id);
    });
    expect(nameOf(arrived.payload)).toBe(BETA);
  } finally {
    restoreNetwork();
  }
});

// ── ③ a delete is not resurrected by an end that still holds the old copy ────

test("a deletion made elsewhere lands here and survives a stale end pushing the old copy back", async ({
  page,
}) => {
  const doomedSyncID = `peer-doomed-${stamp}`;
  const doomedName = `sync-doomed-${stamp}`;
  const projectPayload = (name: string) => ({
    name,
    icon: "",
    color: "",
    description: "",
    sort_order: 0,
  });

  peer().push({ kind: "project", syncID: doomedSyncID, payload: projectPayload(doomedName) });
  await waitFor("the doomed project to land on the desktop", async () => {
    await syncNow(page);
    return activeProjectCountBySyncID(doomedSyncID) === 1 || undefined;
  });

  peer().push({ kind: "project", syncID: doomedSyncID, deleted: true });
  await waitFor("the deletion to land on the desktop", async () => {
    await syncNow(page);
    return activeProjectCountBySyncID(doomedSyncID) === 0 || undefined;
  });

  // A stale end pushes its old copy back — exactly the resurrection R6 forbids.
  const pushed = peer().push({
    kind: "project",
    syncID: doomedSyncID,
    payload: projectPayload(`${doomedName}-resurrected`),
  });
  expect(pushed.results[0].status).toBe("rejected");
  expect(pushed.results[0].reason).toBe("deleted");

  await syncNow(page);
  expect(activeProjectCountBySyncID(doomedSyncID)).toBe(0);
  expect(projectByName(`${doomedName}-resurrected`)).toBeUndefined();
});

// KNOWN PRODUCT DEFECT — this spec is marked `test.fail()` on purpose and is NOT
// evidence that the behaviour works.
//
// A delete made on a desktop never leaves it. `sync_svc.buildPushItem` builds a
// tombstone with a nil payload (uplink.go: 「墓碑不带正文」), `server_svc`'s
// syncPushReqItem.Payload carries no `omitempty`, so the wire body is
// `"payload": null` — and the server's guard
// (agentre-server sync_entity.ValidatePayload) accepts an EMPTY payload but
// rejects `null` as「not a json object」, failing the whole batch with code 30501.
// Reproduced standalone: `synce2e peer push --kind project --sync-id X --deleted
// --payload null` → `code 30501`.
//
// Two consequences, both worse than a missing delete: the tombstone never
// reaches any other end (R6), and because pushBatch returns an error nothing is
// dequeued — one delete wedges that desktop's outbound queue, and every later
// upload with it (R3/R7).
//
// One line on either side fixes it (`omitempty`, or treating `null` as empty in
// the guard); it is deliberately NOT fixed here, because this task may not touch
// product code. When it is fixed this spec starts passing, and `test.fail()`
// turns that into a build failure telling you to drop the annotation.
test("a delete made on the real desktop reaches the peer", async ({ page }) => {
  test.fail();
  await deleteProject(page, alphaProjectID);

  const tombstone = await waitFor(
    "the desktop's tombstone to reach the peer",
    () => {
      peer().sync();
      const obj = peer().object(alphaSyncID);
      return obj && obj.deleted ? obj : undefined;
    },
    { timeoutMs: 20_000 },
  );
  expect(tombstone.deleted).toBe(true);
});
