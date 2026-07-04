import { describe, expect, it } from "vitest";

import {
  addNode,
  earlierNodeIds,
  emptyDraftGraph,
  graphToJSON,
  moveNode,
  nextNodeId,
  nodeBounce,
  nodeDependsOn,
  removeNode,
  setBounce,
  setDependsOn,
  updateNode,
} from "../flow-graph-draft";

describe("flow-graph-draft", () => {
  it("emptyDraftGraph 是单个空白 task 节点", () => {
    const g = emptyDraftGraph();
    expect(g.version).toBe(1);
    expect(g.nodes).toEqual([{ id: "n1", label: "", kind: "task" }]);
    expect(g.edges).toEqual([]);
  });

  it("nextNodeId 取现有 n<k> 最大值 + 1", () => {
    expect(nextNodeId(emptyDraftGraph())).toBe("n2");
    const g = {
      version: 1,
      nodes: [{ id: "n5", label: "x", kind: "task" as const }],
      edges: [],
    };
    expect(nextNodeId(g)).toBe("n6");
  });

  it("addNode 追加一个新 id 的空白 task 节点", () => {
    const g = addNode(emptyDraftGraph());
    expect(g.nodes.map((n) => n.id)).toEqual(["n1", "n2"]);
    expect(g.nodes[1]).toEqual({ id: "n2", label: "", kind: "task" });
  });

  it("updateNode 只改目标节点的 label/kind/brief", () => {
    const g = updateNode(emptyDraftGraph(), "n1", {
      label: "See",
      kind: "leader",
    });
    expect(g.nodes[0]).toMatchObject({
      id: "n1",
      label: "See",
      kind: "leader",
    });
  });

  it("removeNode 删节点并连带删除其所有边", () => {
    let g = addNode(emptyDraftGraph()); // n1, n2
    g = setDependsOn(g, "n2", ["n1"]); // edge n1->n2
    g = removeNode(g, "n1");
    expect(g.nodes.map((n) => n.id)).toEqual(["n2"]);
    expect(g.edges).toEqual([]);
  });

  it("moveNode 在 nodes[] 里换位, 越界不动", () => {
    let g = addNode(emptyDraftGraph()); // n1, n2
    g = moveNode(g, "n2", -1);
    expect(g.nodes.map((n) => n.id)).toEqual(["n2", "n1"]);
    expect(moveNode(g, "n2", -1).nodes.map((n) => n.id)).toEqual(["n2", "n1"]);
  });

  it("moveNode 重排后剔除变「向后」的 sequence 依赖边, 保留 bounce", () => {
    let g = addNode(emptyDraftGraph()); // n1, n2
    g = setDependsOn(g, "n2", ["n1"]); // n1->n2 sequence(n2 依赖 n1)
    g = setBounce(g, "n2", "n1"); // n2->n1 bounce
    // 把 n2 移到 n1 之前 → n1->n2 变向后 → 剔除该依赖; bounce 是回退语义, 保留
    g = moveNode(g, "n2", -1);
    expect(g.nodes.map((n) => n.id)).toEqual(["n2", "n1"]);
    expect(nodeDependsOn(g, "n2")).toEqual([]);
    expect(nodeBounce(g, "n2")).toBe("n1");
  });

  it("removeNode 连带删除该节点的 bounce 边", () => {
    let g = addNode(emptyDraftGraph()); // n1, n2
    g = setBounce(g, "n2", "n1"); // n2->n1 bounce
    g = removeNode(g, "n1"); // 删 bounce 目标端点 → 边也删
    expect(g.edges).toEqual([]);
  });

  it("updateNode 未知 id 原样返回(no-op)", () => {
    const g = emptyDraftGraph();
    expect(updateNode(g, "nope", { label: "X" })).toEqual(g);
  });

  it("earlierNodeIds 只返回 nodes[] 里排在目标之前的节点", () => {
    const g = addNode(addNode(emptyDraftGraph())); // n1, n2, n3
    expect(earlierNodeIds(g, "n1")).toEqual([]);
    expect(earlierNodeIds(g, "n3")).toEqual(["n1", "n2"]);
  });

  it("setDependsOn/nodeDependsOn 往返: 依赖生成 sequence 边", () => {
    let g = addNode(addNode(emptyDraftGraph())); // n1, n2, n3
    g = setDependsOn(g, "n3", ["n1", "n2"]);
    expect(nodeDependsOn(g, "n3").sort()).toEqual(["n1", "n2"]);
    expect(g.edges).toEqual([
      { from: "n1", to: "n3" },
      { from: "n2", to: "n3" },
    ]);
    // 再次 setDependsOn 替换该节点入边, 不累加
    g = setDependsOn(g, "n3", ["n2"]);
    expect(nodeDependsOn(g, "n3")).toEqual(["n2"]);
  });

  it("setBounce/nodeBounce: 唯一 bounce 出边, null 清除", () => {
    let g = addNode(emptyDraftGraph()); // n1, n2
    g = setBounce(g, "n2", "n1");
    expect(nodeBounce(g, "n2")).toBe("n1");
    expect(g.edges).toContainEqual({ from: "n2", to: "n1", kind: "bounce" });
    g = setBounce(g, "n2", null);
    expect(nodeBounce(g, "n2")).toBeNull();
    expect(g.edges.some((e) => e.kind === "bounce")).toBe(false);
  });

  it("setDependsOn 不误删 bounce 边; setBounce 不误删 sequence 边", () => {
    let g = addNode(emptyDraftGraph()); // n1, n2
    g = setDependsOn(g, "n2", ["n1"]); // n1->n2 sequence
    g = setBounce(g, "n2", "n1"); // n2->n1 bounce
    g = setDependsOn(g, "n2", []); // 清 n2 入向 sequence
    expect(nodeBounce(g, "n2")).toBe("n1"); // bounce 仍在
    expect(nodeDependsOn(g, "n2")).toEqual([]);
  });

  it("graphToJSON 输出可被 JSON.parse", () => {
    const g = emptyDraftGraph();
    expect(JSON.parse(graphToJSON(g))).toEqual(g);
  });
});
