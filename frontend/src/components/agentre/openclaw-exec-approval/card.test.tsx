import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ExecApprovalData } from "@/stores/chat-streams-store";

import { ResolveExecApproval } from "../../../../wailsjs/go/app/App";
import { OpenClawExecApprovalCard } from "./card";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  ResolveExecApproval: vi.fn(),
}));

const resolveExecApproval = ResolveExecApproval as ReturnType<typeof vi.fn>;

function pending(overrides: Partial<ExecApprovalData> = {}): ExecApprovalData {
  return {
    id: "approval-1",
    commandText: "git status --short",
    commandPreview: "git status --short",
    allowedDecisions: ["allow-once", "deny"],
    host: "node",
    nodeId: "node-7",
    agentId: "coder",
    status: "pending",
    expiresAtMs: Date.now() + 60_000,
    ...overrides,
  };
}

describe("OpenClawExecApprovalCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resolveExecApproval.mockResolvedValue({
      status: "resolved",
      decision: "allow-once",
    });
  });

  it("Given a pending approval, When decisions are rendered, Then only Gateway allowedDecisions are offered", () => {
    render(<OpenClawExecApprovalCard approval={pending()} sessionId={42} />);

    expect(screen.getByText("Execution approval")).toBeDefined();
    expect(screen.getByText("git status --short")).toBeDefined();
    expect(screen.getByText("node-7")).toBeDefined();
    expect(screen.getByRole("button", { name: "Allow once" })).toBeDefined();
    expect(screen.getByRole("button", { name: "Deny" })).toBeDefined();
    expect(screen.queryByRole("button", { name: "Always allow" })).toBeNull();
  });

  it("Given allow-always is granted, When rendered, Then the optional decision is available", () => {
    render(
      <OpenClawExecApprovalCard
        approval={pending({
          allowedDecisions: ["allow-once", "allow-always", "deny"],
        })}
        sessionId={42}
      />,
    );

    expect(screen.getByRole("button", { name: "Always allow" })).toBeDefined();
  });

  it("Given a resolution is in flight, When a decision is clicked repeatedly, Then one RPC is sent and all decisions are disabled", async () => {
    let settle!: (value: { status: string; decision: string }) => void;
    resolveExecApproval.mockReturnValue(
      new Promise((resolve) => {
        settle = resolve;
      }),
    );
    const user = userEvent.setup();
    render(
      <OpenClawExecApprovalCard
        approval={pending({
          allowedDecisions: ["allow-once", "allow-always", "deny"],
        })}
        sessionId={42}
      />,
    );

    const allowOnce = screen.getByRole("button", { name: "Allow once" });
    await user.dblClick(allowOnce);

    expect(resolveExecApproval).toHaveBeenCalledTimes(1);
    expect(resolveExecApproval).toHaveBeenCalledWith({
      sessionId: 42,
      approvalId: "approval-1",
      decision: "allow-once",
    });
    for (const button of screen.getAllByRole("button")) {
      expect(button).toBeDisabled();
    }
    expect(screen.getByText("Submitting decision…")).toBeDefined();

    settle({ status: "resolved", decision: "allow-once" });
    await waitFor(() => expect(screen.getByText("Allowed once")).toBeDefined());
  });

  it("Given resolve fails, When the user retries, Then an inline error is cleared and the RPC can succeed", async () => {
    resolveExecApproval
      .mockRejectedValueOnce(new Error("gateway disconnected"))
      .mockResolvedValueOnce({ status: "resolved", decision: "deny" });
    const user = userEvent.setup();
    render(<OpenClawExecApprovalCard approval={pending()} sessionId={42} />);

    await user.click(screen.getByRole("button", { name: "Deny" }));
    expect(
      await screen.findByText("Could not submit the approval decision."),
    ).toBeDefined();
    expect(screen.getByRole("button", { name: "Deny" })).toBeEnabled();

    await user.click(screen.getByRole("button", { name: "Deny" }));
    await waitFor(() => expect(resolveExecApproval).toHaveBeenCalledTimes(2));
    expect(
      screen.queryByText("Could not submit the approval decision."),
    ).toBeNull();
    expect(screen.getByText("Denied")).toBeDefined();
  });

  it("Given an expired approval, When rendered, Then it is read-only", () => {
    render(
      <OpenClawExecApprovalCard
        approval={pending({ status: "expired" })}
        sessionId={42}
      />,
    );

    expect(screen.getByText("Expired")).toBeDefined();
    expect(screen.queryAllByRole("button")).toHaveLength(0);
  });

  it("Given an approval resolved by another client, When rendered, Then resolution metadata is shown without an exec-finished claim", () => {
    render(
      <OpenClawExecApprovalCard
        approval={pending({
          status: "resolved",
          decision: "allow-always",
          resolvedBy: "operator-device-2",
        })}
        sessionId={42}
      />,
    );

    expect(screen.getByText("Always allowed")).toBeDefined();
    expect(screen.getByText("operator-device-2")).toBeDefined();
    expect(screen.queryByText(/execution finished/i)).toBeNull();
    expect(screen.queryAllByRole("button")).toHaveLength(0);
  });
});
