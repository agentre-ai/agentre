import { describe, expect, it } from "vitest";
import { parseFlowGraph, layoutFlowGraph } from "../flow-graph";

const seed = JSON.stringify({
  version: 1,
  nodes: [
    { id: "see", label: "See", kind: "leader" },
    { id: "break", label: "Break", kind: "leader" },
    { id: "fe", label: "FE", kind: "task" },
    { id: "be", label: "BE", kind: "task" },
    { id: "wrap", label: "Wrap", kind: "leader" },
  ],
  edges: [
    { from: "see", to: "break" },
    { from: "break", to: "fe" },
    { from: "break", to: "be" },
    { from: "fe", to: "wrap" },
    { from: "be", to: "wrap" },
  ],
});

describe("flow-graph", () => {
  it("parseFlowGraph 非法/空返回 null", () => {
    expect(parseFlowGraph("")).toBeNull();
    expect(parseFlowGraph("nope")).toBeNull();
    expect(parseFlowGraph(seed)?.nodes.length).toBe(5);
  });

  it("layoutFlowGraph 按最长路径分层, 并行节点同列不同行", () => {
    const g = parseFlowGraph(seed)!;
    const { placed, cols } = layoutFlowGraph(g);
    const col = (id: string) => placed.find((p) => p.node.id === id)!.col;
    expect(col("see")).toBe(0);
    expect(col("break")).toBe(1);
    expect(col("fe")).toBe(2);
    expect(col("be")).toBe(2);
    expect(col("wrap")).toBe(3);
    // fe/be 并行 → 同列不同行
    const feRow = placed.find((p) => p.node.id === "fe")!.row;
    const beRow = placed.find((p) => p.node.id === "be")!.row;
    expect(feRow).not.toBe(beRow);
    expect(cols).toBe(4);
  });
});
