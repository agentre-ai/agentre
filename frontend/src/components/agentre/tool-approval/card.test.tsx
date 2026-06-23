import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, it, expect, vi } from "vitest";

import { ToolApprovalCard } from "./card";
import type { ToolApprovalData } from "@/stores/chat-streams-store";
import { AnswerToolApproval } from "../../../../wailsjs/go/app/App";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  AnswerToolApproval: vi.fn().mockResolvedValue(undefined),
}));

// group_create 批准落地后要刷新侧栏群列表;mock 掉 store 只断言 reload 被调。
const mockGroupListReload = vi.hoisted(() =>
  vi.fn().mockResolvedValue(undefined),
);
vi.mock("@/stores/group-list-store", () => ({
  useGroupListStore: {
    getState: () => ({ reload: mockGroupListReload }),
  },
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

  describe("workflow", () => {
    const workflowCreatePending = (
      overrides: Partial<ToolApprovalData> = {},
    ): ToolApprovalData => ({
      toolKey: "workflow",
      requestId: "wf-1",
      toolName: "workflow_create",
      toolInput: { name: "评审流程" },
      status: "pending",
      ...overrides,
    });

    it("routes workflow_create answers through the unified AnswerToolApproval", async () => {
      const user = userEvent.setup();
      render(
        <ToolApprovalCard approval={workflowCreatePending()} sessionId={42} />,
      );
      expect(screen.getByText("Create workflow")).toBeDefined();
      await user.click(screen.getByText("Approve"));
      await waitFor(() => {
        expect(AnswerToolApproval).toHaveBeenCalledTimes(1);
      });
      expect(AnswerToolApproval).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: 42,
          requestId: "wf-1",
          allow: true,
        }),
      );
    });
  });

  describe("group_create", () => {
    const groupCreatePending = (
      overrides: Partial<ToolApprovalData> = {},
    ): ToolApprovalData => ({
      toolKey: "group_create",
      requestId: "gc-1",
      toolName: "group_create",
      toolInput: {
        title: "新功能开发组",
        memberNames: ["开发"],
        brief: "按设计稿重构",
      },
      status: "pending",
      ...overrides,
    });

    it("routes group_create answers through the unified AnswerToolApproval", async () => {
      const user = userEvent.setup();
      render(
        <ToolApprovalCard approval={groupCreatePending()} sessionId={42} />,
      );
      await user.click(screen.getByText("Approve"));
      await waitFor(() => {
        expect(AnswerToolApproval).toHaveBeenCalledTimes(1);
      });
      expect(AnswerToolApproval).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: 42,
          requestId: "gc-1",
          allow: true,
        }),
      );
    });

    it("reloads the group list when a group_create approval resolves approved", () => {
      render(
        <ToolApprovalCard
          approval={groupCreatePending({
            status: "approved",
            result: "group created: id=12 title=新功能开发组",
          })}
          sessionId={42}
        />,
      );
      expect(mockGroupListReload).toHaveBeenCalled();
    });

    it("does not reload the group list while pending or for non-group_create approvals", () => {
      render(
        <ToolApprovalCard approval={groupCreatePending()} sessionId={42} />,
      );
      render(
        <ToolApprovalCard
          approval={pending({ status: "approved", result: "done" })}
          sessionId={42}
        />,
      );
      expect(mockGroupListReload).not.toHaveBeenCalled();
    });

    it("shows the i18n label for group_create", () => {
      render(
        <ToolApprovalCard approval={groupCreatePending()} sessionId={42} />,
      );
      expect(screen.getByText("Create group chat")).toBeDefined();
    });
  });
});
