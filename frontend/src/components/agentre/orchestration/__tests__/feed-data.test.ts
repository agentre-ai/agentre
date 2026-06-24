import { describe, it, expect } from "vitest";
import { buildFeed } from "../feed-data";
describe("feed-data", () => {
  it("dispatch + 完成报告各成一条, 按时间排", () => {
    const items = buildFeed({ run: { id: 1, leaderAgentId: 2, status: "running" }, tasks: [
      { id: 2, agentId: 3, parentTaskId: 1, kind: "dispatch", status: "done", brief: "做X", result: "已完成X", createtime: 100, updatetime: 200 },
    ] } as any);
    expect(items.map((i) => i.kind)).toContain("dispatch");
    expect(items.map((i) => i.kind)).toContain("report");
    expect(items.find((i) => i.kind === "report")!.text).toContain("已完成X");
  });
});
