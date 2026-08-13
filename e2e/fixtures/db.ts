import { DatabaseSync } from "node:sqlite";
import { join } from "node:path";

export const e2eDataDir = () => {
  const value = process.env.AGENTRE_DATA_DIR;
  if (!value) throw new Error("SQLite oracle must be started by the E2E runner");
  return value;
};

const databasePath = () => join(e2eDataDir(), "agentre.db");

function query<T>(sql: string, ...params: Array<string | number>): T[] {
  const db = new DatabaseSync(databasePath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    return db.prepare(sql).all(...params) as T[];
  } finally {
    db.close();
  }
}

function queryCount(sql: string, ...params: Array<string | number>): number {
  return query<{ n: number }>(sql, ...params)[0].n;
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
    "SELECT id, name, path, sync_id, sync_version, local_path_missing, status FROM projects WHERE name = ? AND status = 1",
    name,
  )[0];
}

export function outboundQueueCountForSyncID(syncID: string): number {
  return queryCount(
    "SELECT COUNT(*) AS n FROM sync_outbound_queue WHERE entity_sync_id = ?",
    syncID,
  );
}

export function runningSessionCount(): number {
  return queryCount("SELECT COUNT(*) AS n FROM chat_sessions WHERE agent_status = 'running'");
}

export function userMessageCountContaining(text: string): number {
  return queryCount(
    "SELECT COUNT(*) AS n FROM chat_messages WHERE role = 'user' AND blocks_json LIKE '%' || ? || '%'",
    text,
  );
}

export function assistantMessageCountContaining(text: string): number {
  return queryCount(
    "SELECT COUNT(*) AS n FROM chat_messages WHERE role = 'assistant' AND blocks_json LIKE '%' || ? || '%'",
    text,
  );
}

export function errorSessionCountContaining(errorText: string): number {
  return queryCount(
    "SELECT COUNT(DISTINCT s.id) AS n FROM chat_sessions s JOIN chat_messages m ON m.session_id = s.id WHERE s.agent_status = 'error' AND m.role = 'assistant' AND m.error_text LIKE '%' || ? || '%'",
    errorText,
  );
}

// Legacy helpers stay until task 5 removes the old committed harness paths.
export function fakeAssistantMessageCount(): number {
  return queryCount(
    "SELECT COUNT(*) AS n FROM chat_messages WHERE role = 'assistant' AND blocks_json LIKE '%e2e-fake-reply:%'",
  );
}

export function subagentSessionCount(): number {
  return queryCount("SELECT COUNT(*) AS n FROM chat_sessions WHERE purpose = 'subagent_call'");
}

export function departmentCountByName(name: string): number {
  return queryCount("SELECT COUNT(*) AS n FROM departments WHERE name = ?", name);
}

export function agentIdByName(name: string): number | null {
  return query<{ id: number }>("SELECT id FROM agents WHERE name = ?", name)[0]?.id ?? null;
}
