import { describe, it, expect } from "vitest";
import { deriveSubagents } from "../subagent-data";
import type { chat_svc } from "../../../../../wailsjs/go/models";

const msg = (blocks: unknown[]): chat_svc.ChatMessage =>
  ({ blocks }) as unknown as chat_svc.ChatMessage;

describe("deriveSubagents", () => {
  it("只收 kind==='local_agent' 的 tool_use.subagent, 排除 local_bash", () => {
    const subs = deriveSubagents([
      msg([
        {
          type: "tool_use",
          toolUseId: "a",
          subagent: {
            kind: "local_agent",
            subagentType: "用例生成器",
            taskDescription: "写用例",
            status: "completed",
          },
        },
        {
          type: "tool_use",
          toolUseId: "b",
          subagent: { kind: "local_bash", status: "running" },
        }, // 后台 bash, 排除
        { type: "text", text: "hi" }, // 非 tool_use
      ]),
    ]);
    expect(subs).toHaveLength(1);
    expect(subs[0].toolUseId).toBe("a");
    expect(subs[0].role).toBe("用例生成器");
    expect(subs[0].status).toBe("completed");
  });

  it("按 toolUseId dedupe(后出现覆盖)", () => {
    const subs = deriveSubagents([
      msg([
        {
          type: "tool_use",
          toolUseId: "x",
          subagent: { kind: "local_agent", status: "running" },
        },
      ]),
      msg([
        {
          type: "tool_use",
          toolUseId: "x",
          subagent: { kind: "local_agent", status: "completed" },
        },
      ]),
    ]);
    expect(subs).toHaveLength(1);
    expect(subs[0].status).toBe("completed");
  });

  it("canceled 归 failed; 缺 status 归 running", () => {
    const subs = deriveSubagents([
      msg([
        {
          type: "tool_use",
          toolUseId: "c",
          subagent: { kind: "local_agent", status: "canceled" },
        },
        { type: "tool_use", toolUseId: "d", subagent: { kind: "local_agent" } },
      ]),
    ]);
    expect(subs.find((s) => s.toolUseId === "c")!.status).toBe("failed");
    expect(subs.find((s) => s.toolUseId === "d")!.status).toBe("running");
  });
});
