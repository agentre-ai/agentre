import { DatabaseSync } from "node:sqlite";
import { tmpdir } from "node:os";
import { join } from "node:path";

// The e2e temp DB. AGENTRE_DATA_DIR is set by playwright.config.ts (in every process, incl.
// workers); fall back to the same deterministic path for safety.
const dbPath = () =>
  join(process.env.AGENTRE_DATA_DIR ?? join(tmpdir(), "agentre-e2e-data"), "agentre.db");

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

export type WorkflowRow = {
  id: number;
  name: string;
  content: string;
  status: number;
};

export function workflowByName(name: string): WorkflowRow | null {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare(
        "SELECT id, name, content, status FROM workflows WHERE name = ? ORDER BY id DESC LIMIT 1",
      )
      .get(name) as WorkflowRow | undefined;
    return row ?? null;
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

// orchestrationRunStatus returns the status of the latest orchestration_run, or null if none.
// Read-only — proves the orchestration engine actually advanced the Run lifecycle at the source of
// truth (DB), independent of the UI.
export function orchestrationRunStatus(): string | null {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare("SELECT status FROM orchestration_runs ORDER BY id DESC LIMIT 1")
      .get() as { status: string } | undefined;
    return row?.status ?? null;
  } finally {
    db.close();
  }
}

export type OrchTaskRow = {
  id: number;
  status: string;
  parentTaskId: number;
};

// orchTaskRows returns all orch_tasks rows (id, status, parentTaskId) ordered by id.
// Read-only — proves dispatch created sub-tasks and they reached terminal state at the source of
// truth, independent of the UI.
export function orchTaskRows(): OrchTaskRow[] {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    return db
      .prepare(
        "SELECT id, status, parent_task_id AS parentTaskId FROM orch_tasks ORDER BY id ASC",
      )
      .all() as OrchTaskRow[];
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
