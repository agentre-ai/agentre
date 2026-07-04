import type { FlowGraph, FlowNode } from "../orchestration/flow-graph";

// nextNodeId: 找现有 n<k> 的最大 k, 返回 n<k+1>。确定性(不依赖随机/时间), 便于单测。
export function nextNodeId(g: FlowGraph): string {
  let max = 0;
  for (const n of g.nodes) {
    const m = /^n(\d+)$/.exec(n.id);
    if (m) max = Math.max(max, Number(m[1]));
  }
  return `n${max + 1}`;
}

// emptyDraftGraph: 最小可编辑图 —— 单个空白 task 节点。
export function emptyDraftGraph(): FlowGraph {
  return {
    version: 1,
    nodes: [{ id: "n1", label: "", kind: "task" }],
    edges: [],
  };
}

export function addNode(g: FlowGraph): FlowGraph {
  const id = nextNodeId(g);
  const node: FlowNode = { id, label: "", kind: "task" };
  return { ...g, nodes: [...g.nodes, node] };
}

export function updateNode(
  g: FlowGraph,
  id: string,
  patch: Partial<Pick<FlowNode, "label" | "kind" | "brief">>,
): FlowGraph {
  return {
    ...g,
    nodes: g.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n)),
  };
}

// removeNode: 删节点并连带删除所有以它为端点的边(sequence + bounce)。
export function removeNode(g: FlowGraph, id: string): FlowGraph {
  return {
    ...g,
    nodes: g.nodes.filter((n) => n.id !== id),
    edges: g.edges.filter((e) => e.from !== id && e.to !== id),
  };
}

export function moveNode(g: FlowGraph, id: string, dir: -1 | 1): FlowGraph {
  const i = g.nodes.findIndex((n) => n.id === id);
  const j = i + dir;
  if (i < 0 || j < 0 || j >= g.nodes.length) return g;
  const nodes = [...g.nodes];
  [nodes[i], nodes[j]] = [nodes[j], nodes[i]];
  return { ...g, nodes };
}

// earlierNodeIds: nodes[] 顺序中排在 id 之前的节点 —— depends-on 候选 + 天然防环。
export function earlierNodeIds(g: FlowGraph, id: string): string[] {
  const out: string[] = [];
  for (const n of g.nodes) {
    if (n.id === id) break;
    out.push(n.id);
  }
  return out;
}

// nodeDependsOn: 由 sequence 边(非 bounce)反推该节点的前驱集合。
export function nodeDependsOn(g: FlowGraph, id: string): string[] {
  return g.edges
    .filter((e) => e.kind !== "bounce" && e.to === id)
    .map((e) => e.from);
}

// setDependsOn: 用 deps 重建该节点的入向 sequence 边(替换), 不动 bounce 边与其它节点的边。
export function setDependsOn(
  g: FlowGraph,
  id: string,
  deps: string[],
): FlowGraph {
  const kept = g.edges.filter((e) => !(e.kind !== "bounce" && e.to === id));
  const added = deps.map((from) => ({ from, to: id }));
  return { ...g, edges: [...kept, ...added] };
}

// nodeBounce: 该节点的失败打回目标, 无则 null。
export function nodeBounce(g: FlowGraph, id: string): string | null {
  const e = g.edges.find((x) => x.kind === "bounce" && x.from === id);
  return e ? e.to : null;
}

// setBounce: 替换该节点唯一的 bounce 出边; target 为 null 则清除。
export function setBounce(
  g: FlowGraph,
  id: string,
  target: string | null,
): FlowGraph {
  const kept = g.edges.filter((e) => !(e.kind === "bounce" && e.from === id));
  const added: FlowGraph["edges"] = target
    ? [{ from: id, to: target, kind: "bounce" }]
    : [];
  return { ...g, edges: [...kept, ...added] };
}

export function graphToJSON(g: FlowGraph): string {
  return JSON.stringify(g);
}
