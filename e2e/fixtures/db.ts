import { DatabaseSync } from "node:sqlite";
import { join } from "node:path";

import { resolveTarget } from "../lib/target.mjs";

// The isolated data dir of this checkout's `fake` target. AGENTRE_DATA_DIR is set by
// playwright.config.ts (in every process, incl. workers); the fallback resolves the same target
// rather than a hardcoded path, so the oracle can never end up reading a directory no app wrote.
// Exported so specs that need to touch the filesystem directly (e.g. seeding a real git repo
// under <dataDir>/agents/<agentID>/, mirroring internal/pkg/agentruntime/cwd.go's AgentCwd)
// share the exact same root the Go backend resolves via internal/pkg/paths.AppDataDir().
export const e2eDataDir = () =>
  process.env.AGENTRE_DATA_DIR ?? resolveTarget("fake").dataDir;

const dbPath = () => join(e2eDataDir(), "agentre.db");

// Count chat_sessions stuck in agent_status='running'. Read-only, independent of the Go service
// layer — proves the real status write hit disk. After a finished turn this must be 0.
export function runningSessionCount(): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare("SELECT COUNT(*) AS n FROM chat_sessions WHERE agent_status = 'running'")
      .get() as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

export function chatUserMessageCountContaining(text: string): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare(
        "SELECT COUNT(*) AS n FROM chat_messages WHERE role = 'user' AND blocks_json LIKE '%' || ? || '%'",
      )
      .get(text) as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

// Count chat_sessions created as one-shot subagent delegations (purpose='subagent_call').
// Read-only — proves agent_call actually spun up an isolated sub-agent session at the source of
// truth, independent of the UI (these sessions are hidden from the sidebar by nonSubagentScope).
export function subagentSessionCount(): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare("SELECT COUNT(*) AS n FROM chat_sessions WHERE purpose = 'subagent_call'")
      .get() as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

// Count departments with the given name (org_create_department oracle). Specs use a timestamped
// unique name, so baseline+1 pins the department THIS test created via the approved org write tool,
// independent of seeded departments and the rendered UI.
export function departmentCountByName(name: string): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare("SELECT COUNT(*) AS n FROM departments WHERE name = ?")
      .get(name) as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

// expiredUserAskCountContaining counts persisted chat_messages whose blocks_json carries BOTH the
// given marker (the per-test unique question text) AND "expired":true. Read-only — proves the
// turn-finalize path marked the unanswered AskUserQuestion expired and persisted it (the ask-card
// terminal-state feature), independent of the UI.
export function expiredUserAskCountContaining(marker: string): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare(
        "SELECT COUNT(*) AS n FROM chat_messages WHERE blocks_json LIKE '%' || ? || '%' AND blocks_json LIKE '%\"expired\":true%'",
      )
      .get(marker) as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

// hookCountByName counts hooks with the given (unique, timestamped) name. Read-only — proves a hook
// was created at the source of truth (via the settings page IPC binding or the approved MCP
// hook_create write tool), independent of the UI.
export function hookCountByName(name: string): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare("SELECT COUNT(*) AS n FROM hooks WHERE name = ?")
      .get(name) as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

// Count persisted assistant chat_messages whose text echoes the fake reply prefix. Read-only,
// independent of the UI — proves an agent turn's reply actually hit disk (used to corroborate
// rehydration after a reload). The fake's text lands in blocks_json, so match the raw column.
export function fakeAssistantMessageCount(): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare(
        "SELECT COUNT(*) AS n FROM chat_messages WHERE role = 'assistant' AND blocks_json LIKE '%e2e-fake-reply:%'",
      )
      .get() as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

// Look up an agent's id by its (unique) name. Read-only — lets a spec compute the same
// filesystem path the Go backend resolves for a project-less session's cwd
// (internal/pkg/agentruntime/cwd.go's AgentCwd: <AppDataDir>/agents/<agentID>/) without
// hardcoding the seeded CEO agent's autoincrement id. Returns null when no such agent exists.
export function agentIdByName(name: string): number | null {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
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
