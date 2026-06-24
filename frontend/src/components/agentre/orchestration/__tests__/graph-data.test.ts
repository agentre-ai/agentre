import { describe, it, expect } from "vitest";
import { buildGraph, lifecycle } from "../graph-data";

const detail = (tasks: any[], runStatus = "running") => ({ run: { id: 1, leaderAgentId: 2, status: runStatus }, tasks });

describe("graph-data", () => {
  it("节点按 agent 聚合, 边来自 dispatch 父子", () => {
    const g = buildGraph(detail([
      { id: 1, agentId: 2, parentTaskId: 0, kind: "dispatch", status: "running" }, // Leader 根
      { id: 2, agentId: 3, parentTaskId: 1, kind: "dispatch", status: "running" },
      { id: 3, agentId: 3, parentTaskId: 1, kind: "dispatch", status: "done" },    // 同 agent 第二任务 → 同节点
    ]) as any);
    expect(g.nodes).toHaveLength(2);                         // agent 2 + agent 3
    const a3 = g.nodes.find((n) => n.agentId === 3)!;
    expect(a3.tasks).toHaveLength(2);                        // 卡内两任务行
    expect(a3.status).toBe("running");                       // 聚合: 有 running
    expect(g.edges).toContainEqual({ from: 2, to: 3, kind: "dispatch" });
    expect(g.stats.nodes).toBe(2);
  });
  it("awaiting-user 聚合为 waiting-user(等待你)", () => {
    const g = buildGraph(detail([{ id: 1, agentId: 2, parentTaskId: 0, status: "running" }, { id: 2, agentId: 4, parentTaskId: 1, status: "awaiting-user" }]) as any);
    expect(g.nodes.find((n) => n.agentId === 4)!.status).toBe("waiting-user");
  });
  it("lifecycle: 只有 Leader 根任务时为 empty", () => {
    expect(lifecycle(detail([{ id: 1, agentId: 2, parentTaskId: 0, status: "running" }]) as any)).toBe("empty");
    expect(lifecycle(detail([], "done") as any)).toBe("completed");
  });
});
