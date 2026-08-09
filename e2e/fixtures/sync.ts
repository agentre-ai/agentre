// Fixtures for the local end-to-end sync suite (e2e/sync/).
//
// Three independent observers, none of which goes through the app's own service
// layer:
//   - `desktopDb`  — read-only node:sqlite against the desktop's temp agentre.db
//   - `peer`       — the Go simulated peer (agentre-server/cmd/synce2e), a second
//                    client of the same /v1/sync/* endpoints
//   - `serverLocalPaths` — the server's PostgreSQL, read through the same tool
//
// Everything the runner computed (binary path, DSN, run id, device ids, the
// offline flag file) arrives through env; a missing value is a hard failure, not
// a skip.
import { execFileSync } from "node:child_process";
import { existsSync, rmSync, writeFileSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";
import { tmpdir } from "node:os";
import { join } from "node:path";

function env(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(
      `${name} is not set — run this suite through 'make e2e-sync' (e2e/run-e2e-sync.mjs), ` +
        "which starts the real server, seeds the account and exports the harness env.",
    );
  }
  return value;
}

// ── the desktop's own database ──────────────────────────────────────────────

const dbPath = () =>
  join(process.env.AGENTRE_DATA_DIR ?? join(tmpdir(), "agentre-e2e-sync-data"), "agentre.db");

function query<T>(sql: string, ...params: unknown[]): T[] {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    return db.prepare(sql).all(...(params as never[])) as T[];
  } finally {
    db.close();
  }
}

export type DesktopProject = {
  id: number;
  name: string;
  path: string;
  sync_id: string;
  sync_version: number;
  local_path_missing: number;
  status: number;
};

export function projectByName(name: string): DesktopProject | undefined {
  return query<DesktopProject>(
    "SELECT id, name, path, sync_id, sync_version, local_path_missing, status FROM projects WHERE name = ?",
    name,
  )[0];
}

export function projectBySyncID(syncID: string): DesktopProject | undefined {
  return query<DesktopProject>(
    "SELECT id, name, path, sync_id, sync_version, local_path_missing, status FROM projects WHERE sync_id = ?",
    syncID,
  )[0];
}

// activeProjectCountBySyncID counts LIVE projects (soft-delete sets status <> 1).
// A resurrected tombstone would show up here as a 0 → 1 flip.
export function activeProjectCountBySyncID(syncID: string): number {
  return query<{ n: number }>(
    "SELECT COUNT(*) AS n FROM projects WHERE sync_id = ? AND status = 1",
    syncID,
  )[0].n;
}

export function agentCountBySyncID(syncID: string): number {
  return query<{ n: number }>("SELECT COUNT(*) AS n FROM agents WHERE sync_id = ?", syncID)[0].n;
}

export function backendCountBySyncID(syncID: string): number {
  return query<{ n: number }>(
    "SELECT COUNT(*) AS n FROM agent_backends WHERE sync_id = ?",
    syncID,
  )[0].n;
}

export function execTargetCountBySyncID(syncID: string): number {
  return query<{ n: number }>(
    "SELECT COUNT(*) AS n FROM agent_exec_targets WHERE sync_id = ?",
    syncID,
  )[0].n;
}

// deferredInboundKinds lists the rows parked by R2a/R2b because a reference has
// not arrived (or, for an agentred backend, because this desktop has no pairing
// row for that fingerprint).
export function deferredInboundKinds(): { entity_type: string; entity_sync_id: string }[] {
  return query<{ entity_type: string; entity_sync_id: string }>(
    "SELECT entity_type, entity_sync_id FROM sync_inbound_queue ORDER BY id",
  );
}

export function projectLocationsByProjectID(
  projectID: number,
): { sync_id: string; device_id: string; daemon_fingerprint: string; path: string }[] {
  return query(
    "SELECT sync_id, device_id, daemon_fingerprint, path FROM project_locations WHERE project_id = ? AND status = 1",
    projectID,
  );
}

export function outboundQueueCount(): number {
  return query<{ n: number }>("SELECT COUNT(*) AS n FROM sync_outbound_queue")[0].n;
}

// ── the simulated peer + the server's PostgreSQL ────────────────────────────

function runTool(args: string[]): unknown {
  const out = execFileSync(env("SYNCE2E_BIN"), args, { encoding: "utf8", timeout: 60_000 });
  return JSON.parse(out) as unknown;
}

export type PushResult = {
  status: string;
  version: number;
  reason?: string;
  overwritten_version?: number;
  overwritten_device_id?: number;
};

export type PeerObject = {
  kind: string;
  sync_id: string;
  project_sync_id?: string;
  agentred_fingerprint?: string;
  payload: Record<string, unknown>;
  version: number;
  source_device_id: number;
  deleted: boolean;
};

export type ExecTargetView = {
  sync_id: string;
  sort_order: number;
  backend_sync_id: string;
  fingerprint: string;
  available: boolean;
  reason?: string;
};

// A peer handle. `state` is the JSON file that carries its cursor, its replica
// and its rotated refresh token between one-shot invocations.
export class Peer {
  constructor(
    readonly name: string,
    private readonly state: string,
  ) {}

  sync(): { applied: number; cursor: number; objects: number } {
    return runTool(["peer", "sync", "--state", this.state]) as never;
  }

  dump(): { cursor: number; paired: string[]; items: PeerObject[] } {
    return runTool(["peer", "dump", "--state", this.state]) as never;
  }

  object(syncID: string): PeerObject | undefined {
    return this.dump().items.find((item) => item.sync_id === syncID);
  }

  objectsOfKind(kind: string): PeerObject[] {
    return this.dump().items.filter((item) => item.kind === kind && !item.deleted);
  }

  push(opts: {
    kind: string;
    syncID?: string;
    payload?: Record<string, unknown>;
    baseVersion?: number;
    deleted?: boolean;
    projectSyncID?: string;
    fingerprint?: string;
  }): { sync_id: string; base_version: number; results: PushResult[] } {
    const args = ["peer", "push", "--state", this.state, "--kind", opts.kind];
    if (opts.syncID) args.push("--sync-id", opts.syncID);
    if (opts.payload) args.push("--payload", JSON.stringify(opts.payload));
    if (opts.baseVersion !== undefined) args.push("--base-version", String(opts.baseVersion));
    if (opts.deleted) args.push("--deleted");
    if (opts.projectSyncID) args.push("--project-sync-id", opts.projectSyncID);
    if (opts.fingerprint) args.push("--fingerprint", opts.fingerprint);
    return runTool(args) as never;
  }

  reportLocalPaths(items: { project_sync_id: string; path: string }[]): void {
    runTool([
      "peer",
      "report-local-paths",
      "--state",
      this.state,
      "--items",
      JSON.stringify(items),
    ]);
  }

  // execTargets applies R15's ordered pick with R2b's pairing rule, using the
  // given set of paired agentred fingerprints instead of the peer's own.
  execTargets(agentSyncID: string, paired: string[]): { picked?: ExecTargetView; targets: ExecTargetView[] } {
    return runTool([
      "peer",
      "exec-targets",
      "--state",
      this.state,
      "--agent-sync-id",
      agentSyncID,
      "--paired",
      paired.join(","),
    ]) as never;
  }
}

export const peer = () => new Peer("peer", env("SYNCE2E_PEER_STATE"));
export const windowsPeer = () => new Peer("peerwin", env("SYNCE2E_PEERWIN_STATE"));

export const deviceIDs = () => ({
  desktop: Number(env("AGENTRE_E2E_DEVICE_ID")),
  peer: Number(env("SYNCE2E_PEER_DEVICE_ID")),
  peerwin: Number(env("SYNCE2E_PEERWIN_DEVICE_ID")),
});

export const agentredFingerprint = () => env("SYNCE2E_AGENTRED_FINGERPRINT");

// serverLocalPaths reads the account's device_local_paths straight out of
// PostgreSQL — the R16 / R18 oracle, per device.
export function serverLocalPaths(): { device_id: number; project_sync_id: string; path: string }[] {
  const res = runTool([
    "local-paths",
    "--dsn",
    env("SYNCE2E_DSN"),
    "--run-id",
    env("SYNCE2E_RUN_ID"),
  ]) as { items: { device_id: number; project_sync_id: string; path: string }[] };
  return res.items;
}

// ── the network cut used by R7 ──────────────────────────────────────────────
//
// The desktop talks to the server through a proxy owned by the runner; dropping
// this flag file makes that proxy destroy every connection, which is what "the
// server is unreachable" looks like to the desktop.
export function cutNetwork(): void {
  writeFileSync(env("SYNCE2E_OFFLINE_FLAG"), "offline");
}

export function restoreNetwork(): void {
  const flag = env("SYNCE2E_OFFLINE_FLAG");
  if (existsSync(flag)) rmSync(flag, { force: true });
}

// ── polling ─────────────────────────────────────────────────────────────────

export async function waitFor<T>(
  what: string,
  probe: () => T | undefined | false | null | Promise<T | undefined | false | null>,
  opts: { timeoutMs?: number; intervalMs?: number } = {},
): Promise<T> {
  const timeout = opts.timeoutMs ?? 45_000;
  const interval = opts.intervalMs ?? 1_000;
  const deadline = Date.now() + timeout;
  let last: unknown;
  for (;;) {
    try {
      // Awaited on purpose: an async probe returns a Promise, which is always
      // truthy — checking it unawaited would make every wait succeed instantly.
      const value = await probe();
      if (value) return value as T;
      last = value;
    } catch (err) {
      last = err;
    }
    if (Date.now() >= deadline) {
      throw new Error(`timed out after ${timeout}ms waiting for ${what} (last: ${String(last)})`);
    }
    await new Promise((resolve) => setTimeout(resolve, interval));
  }
}
