import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

import { CanonicalToolRouter } from "./registry";
import type { ChatBlockData } from "@/stores/chat-streams-store";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  AnswerToolPermission: vi.fn().mockResolvedValue(undefined),
  AnswerUserQuestion: vi.fn().mockResolvedValue(undefined),
  ResolvePlanAction: vi.fn().mockResolvedValue(undefined),
}));

describe("CanonicalToolRouter", () => {
  it("falls back to RawToolCard when canonical is missing", () => {
    const block = {
      type: "tool_use",
      toolName: "Bash",
      toolInput: { command: "ls" },
    } as unknown as ChatBlockData;
    render(<CanonicalToolRouter toolBlock={block} />);
    expect(screen.getByTestId("raw-tool-card")).toBeDefined();
  });

  // file.write / file.edit 刻意**不注册**：它们一律折进活动块，正文由活动行的
  // 展开体直接调 content-renderer / hunk-renderer 渲染，卡壳已删。这里守的是
  // 「别有人顺手把卡壳加回注册表」——真走到路由也只该是通用回落。
  it.each(["file.write", "file.edit"])(
    "leaves %s unregistered so it can only fold into an activity block",
    (kind) => {
      const block = {
        type: "tool_use",
        toolName: "Write",
        toolInput: { file_path: "/a.ts", content: "x" },
        canonical: { kind },
      } as unknown as ChatBlockData;
      render(<CanonicalToolRouter toolBlock={block} />);
      expect(screen.getByTestId("raw-tool-card")).toBeDefined();
      expect(screen.queryByTestId("file-write-card")).toBeNull();
      expect(screen.queryByTestId("file-edit-card")).toBeNull();
    },
  );

  it("routes user.ask → UserAskCard", () => {
    const block = {
      type: "tool_use",
      canonical: {
        kind: "user.ask",
        userAsk: {
          requestId: "r1",
          questions: [
            {
              question: "?",
              header: "h",
              options: [{ label: "A", description: "" }],
            },
          ],
        },
      },
    } as unknown as ChatBlockData;
    render(<CanonicalToolRouter toolBlock={block} sessionId={1} />);
    expect(screen.getByTestId("user-ask-card")).toBeDefined();
  });

  it("falls back plan.update to RawToolCard", () => {
    const block = {
      type: "tool_use",
      toolName: "update_plan",
      toolInput: { plan: "- [ ] s" },
      canonical: {
        kind: "plan.update",
        planUpdate: {
          steps: [{ step: "s", status: "pending" }],
        },
      },
    } as unknown as ChatBlockData;
    render(<CanonicalToolRouter toolBlock={block} />);
    expect(screen.getByTestId("raw-tool-card")).toBeDefined();
    expect(screen.getByText("update_plan")).toBeDefined();
    expect(screen.queryByText("plan.update")).toBeNull();
    expect(screen.queryByTestId("plan-card")).toBeNull();
  });

  it("routes plan.approve_request → PlanApproveCard", () => {
    const block = {
      type: "tool_use",
      canonical: {
        kind: "plan.approve_request",
        planApprove: { requestId: "r", planText: "# p" },
      },
    } as unknown as ChatBlockData;
    render(<CanonicalToolRouter toolBlock={block} sessionId={1} />);
    expect(screen.getByTestId("plan-card")).toBeDefined();
  });

  it("routes agent.spawn → AgentSpawnCard", () => {
    const block = {
      type: "tool_use",
      canonical: {
        kind: "agent.spawn",
        agentSpawn: { taskId: "1" },
      },
    } as unknown as ChatBlockData;
    render(<CanonicalToolRouter toolBlock={block} />);
    expect(screen.getByTestId("agent-spawn-card")).toBeDefined();
  });

  it("falls back to RawToolCard for unknown kind", () => {
    const block = {
      type: "tool_use",
      toolName: "Custom",
      toolInput: { foo: "bar" },
      canonical: { kind: "totally.unknown" },
    } as unknown as ChatBlockData;
    render(<CanonicalToolRouter toolBlock={block} />);
    expect(screen.getByTestId("raw-tool-card")).toBeDefined();
  });
});
