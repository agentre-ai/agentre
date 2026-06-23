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

// Count user-authored group_messages (sender_kind='user'). Read-only — proves a user post landed
// in the real group transcript at the source of truth, independent of the rendered UI.
export function groupUserMessageCount(): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare("SELECT COUNT(*) AS n FROM group_messages WHERE sender_kind = 'user'")
      .get() as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

// Count agent-authored group_messages whose text echoes the fake reply prefix (sender_kind='agent').
// Read-only — proves a member turn's reply actually bubbled into the group transcript via the real
// group_send MCP round-trip (fake → gateway /mcp/group/ → IngestAgentMessage), at the source of
// truth and independent of the rendered UI. The fake's reply text lands in the `content` column.
export function agentGroupMessageCount(): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare(
        "SELECT COUNT(*) AS n FROM group_messages WHERE sender_kind = 'agent' AND content LIKE '%e2e-fake-reply:%'",
      )
      .get() as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

// Count group_tasks rows, optionally filtered by status ('open' | 'done' | 'canceled').
// Read-only — proves the task card actually landed/flipped in the source of truth.
export function groupTaskCount(status?: "open" | "done" | "canceled"): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = (
      status
        ? db.prepare("SELECT COUNT(*) AS n FROM group_tasks WHERE status = ?").get(status)
        : db.prepare("SELECT COUNT(*) AS n FROM group_tasks").get()
    ) as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

// Count done group_tasks whose result was written by the fake's group_task_complete
// (TaskResultPrefix). Proves the completion round-trip carried the deliverable, not just
// a status flip.
export function fakeDeliveredTaskCount(): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare(
        "SELECT COUNT(*) AS n FROM group_tasks WHERE status = 'done' AND result LIKE 'e2e-fake-result:%'",
      )
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

export function groupWorkflowByTitle(title: string): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare("SELECT workflow_id AS workflowID FROM groups WHERE title = ? ORDER BY id DESC LIMIT 1")
      .get(title) as { workflowID: number } | undefined;
    return row?.workflowID ?? 0;
  } finally {
    db.close();
  }
}

export type GroupTaskRow = {
  id: number;
  groupID: number;
  taskNo: number;
  title: string;
  brief: string;
  creatorMemberID: number;
  assigneeMemberID: number;
  status: "open" | "done" | "canceled";
  result: string;
  parentTaskNo: number;
};

export function groupTasksByTitle(groupTitle: string): GroupTaskRow[] {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    return db
      .prepare(
        "SELECT t.id, t.group_id AS groupID, t.task_no AS taskNo, t.title, t.brief, " +
          "t.creator_member_id AS creatorMemberID, t.assignee_member_id AS assigneeMemberID, " +
          "t.status, t.result, t.parent_task_no AS parentTaskNo " +
          "FROM group_tasks t JOIN groups g ON g.id = t.group_id " +
          "WHERE g.title = ? ORDER BY t.task_no ASC",
      )
      .all(groupTitle) as GroupTaskRow[];
  } finally {
    db.close();
  }
}

export type GroupTaskEventRow = {
  id: number;
  taskID: number;
  taskEvent: "created" | "completed" | "canceled";
  content: string;
  recipients: number[];
  toUser: number;
};

export function groupTaskEventsByTitle(groupTitle: string): GroupTaskEventRow[] {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const rows = db
      .prepare(
        "SELECT msg.id, msg.task_id AS taskID, msg.task_event AS taskEvent, msg.content, " +
          "msg.recipient_member_ids AS recipientMemberIDs, msg.to_user AS toUser " +
          "FROM group_messages msg JOIN groups g ON g.id = msg.group_id " +
          "WHERE g.title = ? AND msg.task_event != '' ORDER BY msg.seq ASC",
      )
      .all(groupTitle) as Array<
      Omit<GroupTaskEventRow, "recipients"> & { recipientMemberIDs: string }
    >;
    return rows.map((row) => ({
      id: row.id,
      taskID: row.taskID,
      taskEvent: row.taskEvent,
      content: row.content,
      recipients: parseRecipientIDs(row.recipientMemberIDs),
      toUser: row.toUser,
    }));
  } finally {
    db.close();
  }
}

export function groupMessageCountByTitleAndTaskEvent(
  title: string,
  event: "created" | "completed" | "canceled",
): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare(
        "SELECT COUNT(*) AS n FROM group_messages msg JOIN groups g ON g.id = msg.group_id " +
          "WHERE g.title = ? AND msg.task_event = ?",
      )
      .get(title, event) as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

export function groupMemberCountByTitleAndName(title: string, name: string): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare(
        "SELECT COUNT(*) AS n FROM group_members m " +
          "JOIN groups g ON g.id = m.group_id " +
          "JOIN agents a ON a.id = m.agent_id " +
          "WHERE g.title = ? AND a.name = ? AND m.status = 'active'",
      )
      .get(title, name) as { n: number };
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

// Count groups with the given title (group_create chain oracle). Specs use a timestamped unique
// title, so baseline+1 pins the group THIS test case created, independent of seeded/other groups.
export function groupCountByTitle(title: string): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare("SELECT COUNT(*) AS n FROM groups WHERE title = ?")
      .get(title) as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

function parseRecipientIDs(raw: string): number[] {
  try {
    const ids = JSON.parse(raw) as unknown;
    return Array.isArray(ids) ? ids.filter((id): id is number => typeof id === "number") : [];
  } catch {
    return [];
  }
}

// Count active members of the group with the given title. Proves group_create resolved member
// names into real memberships (host + named members), not just an empty shell group.
export function groupMemberCountByTitle(title: string): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare(
        "SELECT COUNT(*) AS n FROM group_members m JOIN groups g ON g.id = m.group_id " +
          "WHERE g.title = ? AND m.status = 'active'",
      )
      .get(title) as { n: number };
    return row.n;
  } finally {
    db.close();
  }
}

// Count group_messages of the titled group whose content contains the given substring. Used to
// verify the system "自会话拉起" message and the brief delivery both landed in the transcript.
export function groupMessageCountByTitleAndContent(title: string, contentLike: string): number {
  const db = new DatabaseSync(dbPath(), { readOnly: true });
  try {
    db.exec("PRAGMA busy_timeout = 5000");
    const row = db
      .prepare(
        "SELECT COUNT(*) AS n FROM group_messages msg JOIN groups g ON g.id = msg.group_id " +
          "WHERE g.title = ? AND msg.content LIKE '%' || ? || '%'",
      )
      .get(title, contentLike) as { n: number };
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
