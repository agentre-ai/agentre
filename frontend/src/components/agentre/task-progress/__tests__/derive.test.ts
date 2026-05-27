import { describe, expect, it } from "vitest";

import { deriveTaskProgress } from "../derive";

import type { chat_svc } from "../../../../../wailsjs/go/models";
import type { ChatBlockData } from "@/stores/chat-streams-store";

// Plan C: derive.ts 从"在前端 re-parse TaskCreate/TaskUpdate/update_plan raw
// input"重写成"读最新一条 canonical.PlanUpdate"。
// backend 已经把 TodoWrite / TaskCreate+TaskUpdate(claudecode task_aggregator
// 聚合) / update_plan(codex translator)收编到 canonical.PlanUpdate;前端只读
// 该字段,不再分支 runtime。
//
// 本测试只覆盖纯前端选择器逻辑:取最近一条 plan.update canonical 并映射成 Task[]。
// runtime 端聚合行为由 task_aggregator_test.go / codex translator_test.go 覆盖。

function mkMsg(
  blocks: ChatBlockData[],
  role: "user" | "assistant" = "assistant",
): chat_svc.ChatMessage {
  return {
    id: Math.floor(Math.random() * 1e6),
    sessionId: 1,
    role,
    blocks,
    model: "",
    promptTokens: 0,
    completionTokens: 0,
    durationMs: 0,
    errorText: "",
    seq: 0,
    createtime: Date.now(),
  } as unknown as chat_svc.ChatMessage;
}

function planBlock(
  steps: { id?: string; step: string; status: string }[],
): ChatBlockData {
  return {
    type: "tool_use",
    canonical: {
      kind: "plan.update",
      planUpdate: { steps },
    },
  } as unknown as ChatBlockData;
}

describe("deriveTaskProgress", () => {
  it("空消息 + 空 liveBlocks → 空任务列表", () => {
    expect(deriveTaskProgress([], [])).toEqual({ tasks: [] });
  });

  it("没有任何 plan.update canonical → 空任务列表", () => {
    const msg = mkMsg([
      {
        type: "tool_use",
        toolUseId: "x",
        toolName: "Bash",
        toolInput: { command: "ls" },
      } as unknown as ChatBlockData,
    ]);
    expect(deriveTaskProgress([msg], [])).toEqual({ tasks: [] });
  });

  it("最新 plan.update steps 映射到 Task[],status 映射 pending→queued, inProgress→running", () => {
    const msg = mkMsg([
      planBlock([
        { id: "a", step: "step a", status: "completed" },
        { id: "b", step: "step b", status: "inProgress" },
        { id: "c", step: "step c", status: "pending" },
      ]),
    ]);
    expect(deriveTaskProgress([msg], [])).toEqual({
      tasks: [
        { id: "a", description: "step a", status: "completed" },
        { id: "b", description: "step b", status: "running" },
        { id: "c", description: "step c", status: "queued" },
      ],
    });
  });

  it("step.id 缺省 → 用 fallback step-i 兜底", () => {
    const msg = mkMsg([
      planBlock([
        { step: "no-id-1", status: "pending" },
        { step: "no-id-2", status: "completed" },
      ]),
    ]);
    const r = deriveTaskProgress([msg], []);
    expect(r.tasks.map((t) => t.id)).toEqual(["step-0", "step-1"]);
  });

  it("canceled 状态映射为 cancelled", () => {
    const msg = mkMsg([
      planBlock([{ id: "a", step: "a", status: "canceled" }]),
    ]);
    expect(deriveTaskProgress([msg], []).tasks[0].status).toBe("cancelled");
  });

  it("未知状态字符串映射为 queued", () => {
    const msg = mkMsg([planBlock([{ id: "a", step: "a", status: "weird" }])]);
    expect(deriveTaskProgress([msg], []).tasks[0].status).toBe("queued");
  });

  it("多条 plan.update → 取最新 message 内最新的那一条", () => {
    const oldMsg = mkMsg([
      planBlock([{ id: "old", step: "old", status: "pending" }]),
    ]);
    const newMsg = mkMsg([
      planBlock([{ id: "new", step: "new", status: "completed" }]),
    ]);
    const r = deriveTaskProgress([oldMsg, newMsg], []);
    expect(r.tasks.map((t) => t.id)).toEqual(["new"]);
  });

  it("liveBlocks 比 messages 新 → 取 liveBlocks 中最新的 plan.update", () => {
    const historicalMsg = mkMsg([
      planBlock([{ id: "old", step: "old", status: "pending" }]),
    ]);
    const liveBlocks: ChatBlockData[] = [
      planBlock([{ id: "live-1", step: "live-1", status: "inProgress" }]),
    ];
    const r = deriveTaskProgress([historicalMsg], liveBlocks);
    expect(r.tasks.map((t) => t.id)).toEqual(["live-1"]);
  });

  it("plan.update 同时也可能附在 plan 类型 block 上(replay 路径)", () => {
    const msg = mkMsg([
      {
        type: "plan",
        text: "...",
        canonical: {
          kind: "plan.update",
          planUpdate: {
            steps: [{ id: "p", step: "from-plan-block", status: "completed" }],
          },
        },
      } as unknown as ChatBlockData,
    ]);
    expect(deriveTaskProgress([msg], []).tasks).toEqual([
      { id: "p", description: "from-plan-block", status: "completed" },
    ]);
  });
});
