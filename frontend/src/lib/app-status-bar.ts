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
  agentCount: number;
  runningCount: number;
  approvalIds: number[];
  unreadIds: number[];
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
  const approvalIds: number[] = [];
  const unreadIds: number[] = [];

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
      approvalIds.push(sessionId);
      continue;
    }

    if (agentStatus !== "running" && lastMessageAt > lastReadAt) {
      unreadIds.push(sessionId);
    }
  }

  return {
    agentCount: agents.length,
    runningCount,
    approvalIds,
    unreadIds,
    indicatorStatus:
      approvalIds.length > 0 || unreadIds.length > 0
        ? "waiting"
        : runningCount > 0
          ? "running"
          : "idle",
  };
}
