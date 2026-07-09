import type { app } from "../../../../wailsjs/go/models";

export type TaskLite = app.DispatchDTO;
export type NodeStatus =
  | "running"
  | "waiting"
  | "waiting-user"
  | "done"
  | "error"
  | "idle";

export interface GraphCall {
  taskId: number;
  sessionId: number;
  callSeq: number;
  brief: string;
  status: NodeStatus;
}
export interface GraphNode {
  agentId: number;
  isLeader: boolean;
  isTopLevel: boolean;
  status: NodeStatus;
  callCount: number;
  calls: GraphCall[];
}
export interface GraphEdge {
  from: number;
  to: number;
  kind: "dispatch" | "report";
}
export interface TreeStats {
  nodes: number;
  subagents: number;
  depth: number;
}

// 单个 task 状态 → NodeStatus(per-call)。
function taskStatus(status: string): NodeStatus {
  switch (status) {
    case "awaiting-user":
      return "waiting-user";
    case "error":
      return "error";
    case "running":
      return "running";
    case "awaiting-children":
      return "waiting";
    case "done":
      return "done";
    default:
      return "idle";
  }
}

// 聚合多个 per-call 状态成节点状态。优先级:
// 等待你(awaiting-user)优先于 error——等待你是唯一真正阻塞用户的状态,
// 不能被技术崩溃(会回报父 agent 重派)掩盖。
function aggregate(statuses: NodeStatus[]): NodeStatus {
  const s = new Set(statuses);
  if (s.has("waiting-user")) return "waiting-user";
  if (s.has("error")) return "error";
  if (s.has("running")) return "running";
  if (s.has("waiting")) return "waiting";
  if (statuses.length && statuses.every((x) => x === "done")) return "done";
  return "idle";
}

export function buildGraph(detail: app.RunDetailDTO): {
  nodes: GraphNode[];
  edges: GraphEdge[];
  stats: TreeStats;
} {
  const tasks = detail.dispatches ?? [];
  const leaderAgent = detail.run?.leaderAgentId;
  const taskById = new Map(tasks.map((t) => [t.id, t]));

  // 按 agent 聚合(agent-as-node):同 subagent 多调用 = 同一节点。
  const byAgent = new Map<number, TaskLite[]>();
  for (const t of tasks) {
    if (!byAgent.has(t.agentId)) byAgent.set(t.agentId, []);
    byAgent.get(t.agentId)!.push(t);
  }

  const nodes: GraphNode[] = [...byAgent.entries()].map(([agentId, ts]) => {
    const calls: GraphCall[] = ts
      .map((t) => ({
        taskId: t.id,
        sessionId: t.sessionId,
        callSeq: t.callSeq,
        brief: t.brief,
        status: taskStatus(t.status),
      }))
      .sort((a, b) => a.callSeq - b.callSeq || a.taskId - b.taskId);
    // 顶层 = 其某个 task 的父 task 由 Leader 派发(父 task.agentId === leaderAgentId)。
    // 用「父 task 的 agent」判定,不依赖 rootTaskId 是否填充,既有测试夹具(无 rootTaskId)也成立。
    const isTopLevel = ts.some(
      (t) => taskById.get(t.parentDispatchId)?.agentId === leaderAgent,
    );
    return {
      agentId,
      isLeader: agentId === leaderAgent,
      isTopLevel,
      status: aggregate(calls.map((c) => c.status)),
      callCount: calls.length,
      calls,
    };
  });

  // dispatch 边:子任务的 parent agent → 子任务 agent(去重到 agent 级)。
  const seen = new Set<string>();
  const edges: GraphEdge[] = [];
  for (const t of tasks) {
    if (!t.parentDispatchId) continue;
    const parent = taskById.get(t.parentDispatchId);
    if (!parent || parent.agentId === t.agentId) continue;
    const key = `${parent.agentId}->${t.agentId}`;
    if (seen.has(key)) continue;
    seen.add(key);
    edges.push({ from: parent.agentId, to: t.agentId, kind: "dispatch" });
  }

  // 树深:沿 parentDispatchId 最长链。
  const depthOf = (id: number, guard = 0): number => {
    const t = taskById.get(id);
    if (!t || !t.parentDispatchId || guard > 64) return 0;
    return 1 + depthOf(t.parentDispatchId, guard + 1);
  };
  const depth = tasks.reduce((m, t) => Math.max(m, depthOf(t.id)), 0);

  // subagents = 唯一子agent 节点数(排除 Leader),与 runHeader「子agent M」标签一致。
  return {
    nodes,
    edges,
    stats: {
      nodes: nodes.length,
      subagents: Math.max(0, nodes.length - 1),
      depth,
    },
  };
}

/**
 * Pure helper: returns true if the given deadlock cycle (session IDs) overlaps
 * with at least one task in the provided tasks list.
 * Used by index.tsx and optionally structure-graph.tsx to compute `hasDeadlock`.
 */
export function hasDeadlockCycle(
  cycle: number[] | undefined,
  tasks: Array<{ sessionId?: number }>,
): boolean {
  if (!cycle || cycle.length === 0) return false;
  const sessionIds = new Set<number>(cycle);
  return tasks.some((task) => task.sessionId && sessionIds.has(task.sessionId));
}

export function lifecycle(
  detail: app.RunDetailDTO,
): "empty" | "running" | "completed" | "paused" | "stopped" {
  const st = detail.run?.status;
  if (st === "done") return "completed";
  if (st === "paused") return "paused";
  if (st === "stopped") return "stopped";
  const tasks = detail.dispatches ?? [];
  if (tasks.length <= 1) return "empty";
  return "running";
}
