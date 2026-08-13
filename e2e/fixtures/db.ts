import { DatabaseSync } from "node:sqlite";
import { join } from "node:path";

export const e2eDataDir = () => {
  const value = process.env.AGENTRE_DATA_DIR;
  if (!value) throw new Error("SQLite oracle must be started by the E2E runner");
  return value;
};

const databasePath = () => join(e2eDataDir(), "agentre.db");

function queryCount(sql: string, ...params: Array<string | number>): number {
  const db = new DatabaseSync(databasePath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db.prepare(sql).get(...params) as { n: number };
    return row.n;
  } finally {
    db.close();
  }
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
  const db = new DatabaseSync(databasePath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db.prepare("SELECT id FROM agents WHERE name = ?").get(name) as
      | { id: number }
      | undefined;
    return row?.id ?? null;
  } finally {
    db.close();
  }
}
