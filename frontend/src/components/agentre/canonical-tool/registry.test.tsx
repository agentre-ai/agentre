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

  it("routes file.write → FileWriteCard", () => {
    const block = {
      type: "tool_use",
      toolName: "Write",
      canonical: {
        kind: "file.write",
        fileWrite: { path: "/a.ts", content: "x", lines: 1, bytes: 1 },
      },
    } as unknown as ChatBlockData;
    render(<CanonicalToolRouter toolBlock={block} />);
    expect(screen.getByTestId("file-write-card")).toBeDefined();
  });

  it("routes file.edit → FileEditCard", () => {
    const block = {
      type: "tool_use",
      toolName: "Edit",
      canonical: {
        kind: "file.edit",
        fileEdit: {
          files: [
            { path: "/a.ts", kind: "modified", hunks: [], plus: 1, minus: 0 },
          ],
        },
      },
    } as unknown as ChatBlockData;
    render(<CanonicalToolRouter toolBlock={block} />);
    expect(screen.getByTestId("file-edit-card")).toBeDefined();
  });

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
