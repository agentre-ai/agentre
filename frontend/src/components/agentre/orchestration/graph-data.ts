import type { app } from "../../../wailsjs/go/models";

export type TaskLite = app.TaskDTO;
export type NodeStatus = "running" | "waiting" | "waiting-user" | "done" | "error" | "idle";
export interface GraphNode { agentId: number; tasks: TaskLite[]; status: NodeStatus; isLeader: boolean; }
export interface GraphEdge { from: number; to: number; kind: "dispatch" | "report"; }
export interface TreeStats { nodes: number; subagents: number; depth: number; }

function aggregate(tasks: TaskLite[]): NodeStatus {
  const s = new Set(tasks.map((t) => t.status));
  if (s.has("error")) return "error";
  if (s.has("awaiting-user")) return "waiting-user";
  if (s.has("running")) return "running";
  if (s.has("awaiting-children")) return "waiting";
  if (tasks.length && tasks.every((t) => t.status === "done")) return "done";
  return "idle";
}

export function buildGraph(detail: app.RunDetailDTO): { nodes: GraphNode[]; edges: GraphEdge[]; stats: TreeStats } {
  const tasks = detail.tasks ?? [];
  const leaderAgent = detail.run.leaderAgentId;
  const byAgent = new Map<number, TaskLite[]>();
  for (const t of tasks) {
    if (!byAgent.has(t.agentId)) byAgent.set(t.agentId, []);
    byAgent.get(t.agentId)!.push(t);
  }
  const nodes: GraphNode[] = [...byAgent.entries()].map(([agentId, ts]) => ({
    agentId, tasks: ts, status: aggregate(ts), isLeader: agentId === leaderAgent,
  }));
  // dispatch 边: 子任务的 parent agent → 子任务 agent(去重到 agent 级)。
  const taskById = new Map(tasks.map((t) => [t.id, t]));
  const seen = new Set<string>();
  const edges: GraphEdge[] = [];
  for (const t of tasks) {
    if (!t.parentTaskId) continue;
    const parent = taskById.get(t.parentTaskId);
    if (!parent || parent.agentId === t.agentId) continue;
    const key = `${parent.agentId}->${t.agentId}`;
    if (seen.has(key)) continue;
    seen.add(key);
    edges.push({ from: parent.agentId, to: t.agentId, kind: "dispatch" });
  }
  // 树深: 沿 parentTaskId 最长链。
  const depthOf = (id: number, guard = 0): number => {
    const t = taskById.get(id);
    if (!t || !t.parentTaskId || guard > 64) return 0;
    return 1 + depthOf(t.parentTaskId, guard + 1);
  };
  const depth = tasks.reduce((m, t) => Math.max(m, depthOf(t.id)), 0);
  return { nodes, edges, stats: { nodes: nodes.length, subagents: tasks.length, depth } };
}

export function lifecycle(detail: app.RunDetailDTO): "empty" | "running" | "completed" | "paused" | "stopped" {
  const st = detail.run.status;
  if (st === "done") return "completed";
  if (st === "paused") return "paused";
  if (st === "stopped") return "stopped";
  const tasks = detail.tasks ?? [];
  if (tasks.length <= 1) return "empty"; // 只有 Leader 根任务 → 起步态
  return "running";
}
