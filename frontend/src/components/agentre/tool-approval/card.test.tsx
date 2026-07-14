import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, it, expect, vi } from "vitest";

import { ToolApprovalCard } from "./card";
import type { ToolApprovalData } from "@/stores/chat-streams-store";
import { AnswerToolApproval } from "../../../../wailsjs/go/app/App";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  AnswerToolApproval: vi.fn().mockResolvedValue(undefined),
}));

describe("ToolApprovalCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  const pending = (
    overrides: Partial<ToolApprovalData> = {},
  ): ToolApprovalData => ({
    toolKey: "org",
    requestId: "org-1",
    toolName: "org_create_department",
    toolInput: { name: "研发部", parentId: 1 },
    status: "pending",
    ...overrides,
  });

  it("renders the tool label, the input payload and approve/reject buttons when pending", () => {
    render(<ToolApprovalCard approval={pending()} sessionId={42} />);
    // tools.org_create_department → "Create department" (setup forces en locale)
    expect(screen.getByText("Create department")).toBeDefined();
    // 入参 JSON 原样渲染(动态内容不翻译)
    expect(screen.getByText(/研发部/)).toBeDefined();
    expect(screen.getByText("Approve")).toBeDefined();
    expect(screen.getByText("Reject")).toBeDefined();
  });

  it("calls AnswerToolApproval with allow:true when approve is clicked", async () => {
    const user = userEvent.setup();
    render(<ToolApprovalCard approval={pending()} sessionId={42} />);
    await user.click(screen.getByText("Approve"));
    await waitFor(() => {
      expect(AnswerToolApproval).toHaveBeenCalledTimes(1);
    });
    expect(AnswerToolApproval).toHaveBeenCalledWith(
      expect.objectContaining({
        sessionId: 42,
        requestId: "org-1",
        allow: true,
      }),
    );
  });

  it("calls AnswerToolApproval with allow:false when reject is clicked", async () => {
    const user = userEvent.setup();
    render(<ToolApprovalCard approval={pending()} sessionId={42} />);
    await user.click(screen.getByText("Reject"));
    await waitFor(() => {
      expect(AnswerToolApproval).toHaveBeenCalledTimes(1);
    });
    expect(AnswerToolApproval).toHaveBeenCalledWith(
      expect.objectContaining({
        sessionId: 42,
        requestId: "org-1",
        allow: false,
      }),
    );
  });

  it("renders a read-only status badge with no buttons once denied", () => {
    render(
      <ToolApprovalCard
        approval={pending({
          status: "denied",
          result: "用户拒绝了删除操作",
        })}
        sessionId={42}
      />,
    );
    expect(screen.getByText("Rejected")).toBeDefined();
    expect(screen.getByText("用户拒绝了删除操作")).toBeDefined();
    expect(screen.queryByText("Approve")).toBeNull();
    expect(screen.queryByText("Reject")).toBeNull();
  });

  it("renders an approved badge with the result text", () => {
    render(
      <ToolApprovalCard
        approval={pending({
          status: "approved",
          result: "已创建部门 研发部",
        })}
        sessionId={42}
      />,
    );
    expect(screen.getByText("Approved")).toBeDefined();
    expect(screen.getByText("已创建部门 研发部")).toBeDefined();
    expect(screen.queryByText("Approve")).toBeNull();
  });

  it("renders an expired badge for status=expired", () => {
    render(
      <ToolApprovalCard
        approval={pending({ status: "expired" })}
        sessionId={42}
      />,
    );
    expect(screen.getByText("Expired")).toBeDefined();
    expect(screen.queryByText("Approve")).toBeNull();
  });
});
