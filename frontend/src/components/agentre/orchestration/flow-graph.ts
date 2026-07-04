export type FlowKind = "task" | "leader";
export interface FlowNode {
  id: string;
  label: string;
  kind: FlowKind;
  brief?: string;
  sharedFiles?: boolean;
}
export interface FlowEdge {
  from: string;
  to: string;
  kind?: "bounce";
}
export interface FlowGraph {
  version: number;
  nodes: FlowNode[];
  edges: FlowEdge[];
}

export function parseFlowGraph(json?: string): FlowGraph | null {
  if (!json || !json.trim()) return null;
  try {
    const g = JSON.parse(json) as FlowGraph;
    if (!g || !Array.isArray(g.nodes) || g.nodes.length === 0) return null;
    if (!Array.isArray(g.edges)) g.edges = [];
    return g;
  } catch {
    return null;
  }
}

export interface Placed {
  node: FlowNode;
  col: number;
  row: number;
}

// layoutFlowGraph: col = 最长路径层(仅 sequence 边), row = 层内出现序。
export function layoutFlowGraph(g: FlowGraph): {
  placed: Placed[];
  cols: number;
  rows: number;
} {
  const preds = new Map<string, string[]>();
  for (const e of g.edges) {
    if (e.kind === "bounce") continue;
    preds.set(e.to, [...(preds.get(e.to) ?? []), e.from]);
  }
  const layer = new Map<string, number>();
  const depth = (id: string, guard = 0): number => {
    const cached = layer.get(id);
    if (cached !== undefined) return cached;
    let best = 0;
    if (guard < 256) {
      for (const p of preds.get(id) ?? [])
        best = Math.max(best, depth(p, guard + 1) + 1);
    }
    layer.set(id, best);
    return best;
  };
  const rowCounter = new Map<number, number>();
  const placed: Placed[] = g.nodes.map((node) => {
    const col = depth(node.id);
    const row = rowCounter.get(col) ?? 0;
    rowCounter.set(col, row + 1);
    return {
      node,
      col,
      row,
    };
  });
  const cols = placed.reduce((m, p) => Math.max(m, p.col + 1), 0);
  const rows = [...rowCounter.values()].reduce((m, r) => Math.max(m, r), 0);
  return { placed, cols, rows };
}

export function isBounceSource(g: FlowGraph, id: string): boolean {
  return g.edges.some((e) => e.kind === "bounce" && e.from === id);
}
