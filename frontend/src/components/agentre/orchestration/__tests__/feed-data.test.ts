import { describe, it, expect } from "vitest";
import { buildFeed } from "../feed-data";
import type { app } from "../../../../../wailsjs/go/models";
describe("feed-data", () => {
  it("dispatch + 完成报告各成一条, 按时间排", () => {
    const items = buildFeed({
      run: { id: 1, leaderAgentId: 2, status: "running" },
      tasks: [
        {
          id: 2,
          agentId: 3,
          parentTaskId: 1,
          kind: "dispatch",
          status: "done",
          brief: "做X",
          result: "已完成X",
          createtime: 100,
          updatetime: 200,
        },
      ],
    } as unknown as app.RunDetailDTO);
    expect(items.map((i) => i.kind)).toContain("dispatch");
    expect(items.map((i) => i.kind)).toContain("report");
    expect(items.find((i) => i.kind === "report")!.text).toContain("已完成X");
  });

  it("askLog 合并进 feed: ask + reply 两条按 ts 排序", () => {
    const items = buildFeed({ tasks: [] } as never, [
      {
        kind: "ask",
        askId: "k",
        agentId: 2,
        targetAgentId: 3,
        text: "鉴权?",
        ts: 10,
      },
      { kind: "reply", askId: "k", agentId: 3, text: "ok", ts: 20 },
    ]);
    expect(items.map((i) => i.kind)).toEqual(["ask", "reply"]);
    expect(items[0].text).toBe("鉴权?");
  });
});
