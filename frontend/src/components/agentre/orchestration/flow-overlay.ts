import { parseFlowGraph, type FlowGraph } from "./flow-graph";

export type NodeStatus = "pending" | "running" | "done" | "error" | "neutral";

export interface NodeOverlay {
  status: NodeStatus;
  count: number;
}

// OverlayTask: deriveNodeOverlay 的最小任务输入(与 wails 生成类型解耦, 便于纯单测)。
export interface OverlayTask {
  nodeRef?: string;
  status: string;
}

// 非终态任务状态(点亮为 running)。终态 = done / canceled / error。
const NON_TERMINAL = new Set([
  "pending",
  "running",
  "awaiting-children",
  "awaiting-user",
  "paused",
]);

const norm = (s: string) => s.trim().toLowerCase();

// taskMatchesNode: 任务是否属于某流程节点(按 label trim+小写,与 deriveNodeOverlay 同口径)。
// 空 nodeRef(未打标)/ 空 label → false,避免筛选时误纳未打标任务。
export function taskMatchesNode(
  nodeRef: string | null | undefined,
  nodeLabel: string | null | undefined,
): boolean {
  if (!nodeRef || !nodeLabel) return false;
  return norm(nodeRef) === norm(nodeLabel);
}

// statusOf: task-kind 节点按匹配任务聚合状态。优先级 error > running > done > pending。
// 全终态(含 canceled-only)统一记 done(已了结)——总函数, 覆盖 spec 的边界。
function statusOf(tasks: OverlayTask[]): NodeStatus {
  if (tasks.length === 0) return "pending";
  if (tasks.some((t) => t.status === "error")) return "error";
  if (tasks.some((t) => NON_TERMINAL.has(t.status))) return "running";
  return "done";
}

// deriveNodeOverlay: 按 label(trim+小写)把带 nodeRef 的任务匹配到图节点, 算出每节点状态+计数。
// leader-kind 节点恒 neutral;未打标 / 匹配不到的任务忽略;非法/空 graph → {}。
export function deriveNodeOverlay(
  graph: FlowGraph | string | null | undefined,
  tasks: OverlayTask[],
): Record<string, NodeOverlay> {
  const g = typeof graph === "string" ? parseFlowGraph(graph) : (graph ?? null);
  const out: Record<string, NodeOverlay> = {};
  if (!g) return out;

  const byLabel = new Map<string, OverlayTask[]>();
  for (const tk of tasks) {
    const ref = tk.nodeRef?.trim();
    if (!ref) continue;
    const k = norm(ref);
    byLabel.set(k, [...(byLabel.get(k) ?? []), tk]);
  }

  for (const node of g.nodes) {
    if (node.kind === "leader") {
      out[node.id] = { status: "neutral", count: 0 };
      continue;
    }
    const matched = byLabel.get(norm(node.label)) ?? [];
    out[node.id] = { status: statusOf(matched), count: matched.length };
  }
  return out;
}
