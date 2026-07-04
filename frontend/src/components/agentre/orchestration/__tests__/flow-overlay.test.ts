import { describe, expect, it } from "vitest";

import { deriveNodeOverlay, taskMatchesNode } from "../flow-overlay";

const graph = JSON.stringify({
  version: 1,
  nodes: [
    { id: "n1", label: "Break", kind: "leader" },
    { id: "n2", label: "FE", kind: "task" },
    { id: "n3", label: "BE", kind: "task" },
    { id: "n4", label: "QA", kind: "task" },
  ],
  edges: [],
});

describe("deriveNodeOverlay", () => {
  it("leader 节点恒 neutral, 无计数", () => {
    const o = deriveNodeOverlay(graph, []);
    expect(o.n1).toEqual({ status: "neutral", count: 0 });
  });

  it("无匹配任务的 task 节点 = pending", () => {
    const o = deriveNodeOverlay(graph, []);
    expect(o.n2).toEqual({ status: "pending", count: 0 });
  });

  it("label 匹配(trim+大小写不敏感)聚合任务并计数", () => {
    const o = deriveNodeOverlay(graph, [
      { nodeRef: " fe ", status: "done" },
      { nodeRef: "FE", status: "done" },
    ]);
    expect(o.n2).toEqual({ status: "done", count: 2 });
  });

  it("优先级 error > running > done", () => {
    expect(
      deriveNodeOverlay(graph, [
        { nodeRef: "FE", status: "done" },
        { nodeRef: "FE", status: "error" },
      ]).n2.status,
    ).toBe("error");
    expect(
      deriveNodeOverlay(graph, [
        { nodeRef: "FE", status: "done" },
        { nodeRef: "FE", status: "running" },
      ]).n2.status,
    ).toBe("running");
  });

  it("awaiting-children/awaiting-user/pending/paused 都算 running(非终态)", () => {
    for (const s of [
      "awaiting-children",
      "awaiting-user",
      "pending",
      "paused",
    ]) {
      expect(
        deriveNodeOverlay(graph, [{ nodeRef: "FE", status: s }]).n2.status,
      ).toBe("running");
    }
  });

  it("全终态(含 canceled-only)→ done", () => {
    expect(
      deriveNodeOverlay(graph, [{ nodeRef: "QA", status: "canceled" }]).n4
        .status,
    ).toBe("done");
  });

  it("未打标 / 匹配不到节点的任务被忽略", () => {
    const o = deriveNodeOverlay(graph, [
      { status: "running" }, // 无 nodeRef
      { nodeRef: "不存在", status: "running" },
    ]);
    expect(o.n2.status).toBe("pending");
    expect(o.n3.status).toBe("pending");
  });

  it("非法/空 graph → 空对象", () => {
    expect(deriveNodeOverlay("", [])).toEqual({});
    expect(deriveNodeOverlay(null, [])).toEqual({});
  });
});

describe("taskMatchesNode", () => {
  it("按 label trim+小写匹配(与 deriveNodeOverlay 同口径)", () => {
    expect(taskMatchesNode("FE", "fe")).toBe(true);
    expect(taskMatchesNode("  QA ", "qa")).toBe(true);
    expect(taskMatchesNode("FE", "BE")).toBe(false);
  });

  it("空 nodeRef / 空 label → false(不误纳未打标任务)", () => {
    expect(taskMatchesNode("", "FE")).toBe(false);
    expect(taskMatchesNode(undefined, "FE")).toBe(false);
    expect(taskMatchesNode("FE", null)).toBe(false);
    expect(taskMatchesNode("FE", "")).toBe(false);
  });
});
