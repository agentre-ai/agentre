import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";

import { AgentSpawnCard } from "./card";
import type { ChatBlockData } from "@/stores/chat-streams-store";

describe("AgentSpawnCard", () => {
  it("renders nothing without canonical", () => {
    const block = { type: "tool_use" } as unknown as ChatBlockData;
    const { container } = render(<AgentSpawnCard toolBlock={block} />);
    expect(container.firstChild).toBeNull();
  });

  it("renders Agent header with description + type", () => {
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          subagentType: "code-reviewer",
          taskDescription: "review PR",
          toolUses: 3,
          totalTokens: 1200,
          status: "running",
        },
      },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    expect(screen.getByText("Agent")).toBeDefined();
    expect(screen.getByText("review PR")).toBeDefined();
    expect(screen.getByText("code-reviewer")).toBeDefined();
    expect(screen.getByText(/3 tools/)).toBeDefined();
    expect(screen.getByText(/1\.2K tok/)).toBeDefined();
  });

  it("overlays runtime state from block.subagent onto canonical.agentSpawn", () => {
    // canonical.agentSpawn 由 translator 算静态字段(description/subagentType/prompt),
    // 运行时态(toolUses/totalTokens/durationMs/status)来自 SubagentStarted/Progress/
    // Done 事件经 mergeSubagentMeta 合并到 block.subagent;readSpawn 必须把 block.subagent
    // overlay 上去,否则 header chip 永远显示 0 工具 / 无 token。
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskDescription: "review PR",
          subagentType: "code-reviewer",
          prompt: "please review",
        },
      },
      subagent: {
        toolUses: 5,
        totalTokens: 2400,
        status: "completed",
        durationMs: 12000,
      },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    expect(screen.getByText(/5 tools/)).toBeDefined();
    expect(screen.getByText(/2\.4K tok/)).toBeDefined();
    expect(screen.getByText(/DONE/)).toBeDefined();
  });

  it("shows DONE when completed", () => {
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          status: "completed",
        },
      },
    } as unknown as ChatBlockData;
    const result = {
      type: "tool_result",
      text: "summary text",
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} resultBlock={result} />);
    expect(screen.getByText("DONE")).toBeDefined();
  });

  it("shows STOPPED label when cancelled, not RUNNING/spin", () => {
    // 用户在 turn 内点 Stop 时,chat_svc 把仍 running 的 SubagentStateBlock 改成
    // "canceled" 落 DB。前端必须识别这个值否则 narrowSpawnStatus 会 drop 掉它,
    // 回退到 base.status="running",卡片继续 spin → bug。
    const block = {
      type: "tool_use",
      toolName: "Task",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: {
          taskId: "1",
          taskDescription: "long running task",
          status: "running",
        },
      },
      subagent: {
        status: "canceled",
        durationMs: 4200,
      },
    } as unknown as ChatBlockData;
    render(<AgentSpawnCard toolBlock={block} />);
    expect(screen.getByText(/STOPPED/)).toBeDefined();
    expect(screen.queryByText(/RUNNING/)).toBeNull();
  });
});
