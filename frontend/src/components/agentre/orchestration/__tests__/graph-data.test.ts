import { describe, it, expect } from "vitest";
import { buildGraph, lifecycle } from "../graph-data";
import type { app } from "../../../../../wailsjs/go/models";

const detail = (tasks: Array<Partial<app.TaskDTO>>, runStatus = "running") =>
  ({
    run: { id: 1, leaderAgentId: 2, status: runStatus },
    tasks,
  }) as unknown as app.RunDetailDTO;

describe("graph-data", () => {
  it("节点按 agent 聚合, 边来自 dispatch 父子", () => {
    const g = buildGraph(
      detail([
        {
          id: 1,
          agentId: 2,
          parentTaskId: 0,
          kind: "dispatch",
          status: "running",
        }, // Leader 根
        {
          id: 2,
          agentId: 3,
          parentTaskId: 1,
          kind: "dispatch",
          status: "running",
        },
        {
          id: 3,
          agentId: 3,
          parentTaskId: 1,
          kind: "dispatch",
          status: "done",
        }, // 同 agent 第二任务 → 同节点
      ]),
    );
    expect(g.nodes).toHaveLength(2); // agent 2 + agent 3
    const a3 = g.nodes.find((n) => n.agentId === 3)!;
    expect(a3.tasks).toHaveLength(2); // 卡内两任务行
    expect(a3.status).toBe("running"); // 聚合: 有 running
    expect(g.edges).toContainEqual({ from: 2, to: 3, kind: "dispatch" });
    expect(g.stats.nodes).toBe(2);
  });
  it("awaiting-user 聚合为 waiting-user(等待你)", () => {
    const g = buildGraph(
      detail([
        { id: 1, agentId: 2, parentTaskId: 0, status: "running" },
        { id: 2, agentId: 4, parentTaskId: 1, status: "awaiting-user" },
      ]),
    );
    expect(g.nodes.find((n) => n.agentId === 4)!.status).toBe("waiting-user");
  });
  it("lifecycle: 只有 Leader 根任务时为 empty", () => {
    expect(
      lifecycle(
        detail([{ id: 1, agentId: 2, parentTaskId: 0, status: "running" }]),
      ),
    ).toBe("empty");
    expect(lifecycle(detail([], "done"))).toBe("completed");
  });
  it("error + awaiting-user 同节点时 等待你 优先于 error(琥珀胜)", () => {
    const g = buildGraph(
      detail([
        { id: 1, agentId: 2, parentTaskId: 0, status: "running" },
        { id: 2, agentId: 5, parentTaskId: 1, status: "error" },
        { id: 3, agentId: 5, parentTaskId: 1, status: "awaiting-user" }, // 同节点同时 error + 等待你
      ]),
    );
    expect(g.nodes.find((n) => n.agentId === 5)!.status).toBe("waiting-user");
  });
  it("全 error 节点聚合为 error", () => {
    const g = buildGraph(
      detail([
        { id: 1, agentId: 2, parentTaskId: 0, status: "running" },
        { id: 2, agentId: 6, parentTaskId: 1, status: "error" },
      ]),
    );
    expect(g.nodes.find((n) => n.agentId === 6)!.status).toBe("error");
  });
  it("awaiting-children 聚合为 waiting", () => {
    const g = buildGraph(
      detail([
        { id: 1, agentId: 2, parentTaskId: 0, status: "awaiting-children" },
      ]),
    );
    expect(g.nodes.find((n) => n.agentId === 2)!.status).toBe("waiting");
  });
  it("stats.subagents 数唯一子agent 节点(排除 Leader), 非任务总数", () => {
    const g = buildGraph(
      detail([
        { id: 1, agentId: 2, parentTaskId: 0, status: "running" }, // Leader
        { id: 2, agentId: 3, parentTaskId: 1, status: "running" },
        { id: 3, agentId: 3, parentTaskId: 1, status: "done" }, // 同 agent 第二任务
        { id: 4, agentId: 7, parentTaskId: 1, status: "done" },
      ]),
    );
    expect(g.stats.nodes).toBe(3); // agent 2/3/7
    expect(g.stats.subagents).toBe(2); // 3 与 7(排除 Leader 2), 不是任务总数 4
  });
  it("stats.depth 取最长 parentTask 链", () => {
    const g = buildGraph(
      detail([
        { id: 1, agentId: 2, parentTaskId: 0, status: "running" },
        { id: 2, agentId: 3, parentTaskId: 1, status: "running" },
        { id: 3, agentId: 4, parentTaskId: 2, status: "running" }, // 深度 2
      ]),
    );
    expect(g.stats.depth).toBe(2);
  });
  it("lifecycle: paused / stopped 跟随 run 状态", () => {
    const one = [{ id: 1, agentId: 2, parentTaskId: 0, status: "running" }];
    expect(lifecycle(detail(one, "paused"))).toBe("paused");
    expect(lifecycle(detail(one, "stopped"))).toBe("stopped");
  });
  it("buildGraph 容忍 run 缺失(可选字段)不抛", () => {
    expect(() =>
      buildGraph({ tasks: [] } as unknown as app.RunDetailDTO),
    ).not.toThrow();
  });
});
