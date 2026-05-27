import type { AgentStatus } from "@/stores/types";

type StatusBarSession = {
  id?: number;
  lastMessageAt?: number;
  lastReadAt?: number;
  needsAttention?: boolean;
  status?: string;
};

export type StatusBarAgent = {
  sessions?: StatusBarSession[];
};

export type StatusBarSessionStatus = {
  agentStatus: AgentStatus;
  needsAttention: boolean;
};

export type StatusBarSessionMeta = {
  lastMessageAt?: number;
  lastReadAt?: number;
};

export type AppStatusBarState = {
  agentSummary: string;
  attentionSummary: string | null;
  indicatorStatus: AgentStatus;
};

function normalizeAgentStatus(status: string | undefined): AgentStatus {
  switch (status) {
    case "running":
    case "waiting":
    case "error":
    case "idle":
      return status;
    default:
      return "idle";
  }
}

function formatPlural(
  count: number,
  singular: string,
  plural = `${singular}s`,
) {
  return `${count} ${count === 1 ? singular : plural}`;
}

export function deriveAppStatusBarState(
  agents: readonly StatusBarAgent[],
  statuses: ReadonlyMap<number, StatusBarSessionStatus>,
  metas: ReadonlyMap<number, StatusBarSessionMeta>,
  readOverrides: ReadonlyMap<number, number>,
): AppStatusBarState {
  const sessionsById = new Map<number, StatusBarSession>();

  for (const agent of agents) {
    for (const session of agent.sessions ?? []) {
      const sessionId = session.id ?? 0;
      if (sessionId > 0) {
        sessionsById.set(sessionId, session);
      }
    }
  }

  let runningCount = 0;
  let approvalCount = 0;
  let unreadCount = 0;

  for (const [sessionId, session] of sessionsById) {
    const liveStatus = statuses.get(sessionId);
    const meta = metas.get(sessionId);
    const agentStatus =
      liveStatus?.agentStatus ?? normalizeAgentStatus(session.status);
    const needsAttention =
      liveStatus?.needsAttention ?? session.needsAttention ?? false;
    const lastMessageAt = meta?.lastMessageAt ?? session.lastMessageAt ?? 0;
    const lastReadAt = Math.max(
      meta?.lastReadAt ?? session.lastReadAt ?? 0,
      readOverrides.get(sessionId) ?? 0,
    );
    const isWaitingForUser = needsAttention || agentStatus === "waiting";

    if (agentStatus === "running") {
      runningCount += 1;
    }

    if (isWaitingForUser) {
      approvalCount += 1;
      continue;
    }

    if (agentStatus !== "running" && lastMessageAt > lastReadAt) {
      unreadCount += 1;
    }
  }

  const attentionParts: string[] = [];
  if (approvalCount > 0) {
    attentionParts.push(formatPlural(approvalCount, "approval"));
  }
  if (unreadCount > 0) {
    attentionParts.push(`${unreadCount} unread`);
  }

  return {
    agentSummary: `${formatPlural(agents.length, "agent")} · ${runningCount} running`,
    attentionSummary:
      attentionParts.length > 0 ? attentionParts.join(" · ") : null,
    indicatorStatus:
      approvalCount > 0 || unreadCount > 0
        ? "waiting"
        : runningCount > 0
          ? "running"
          : "idle",
  };
}
