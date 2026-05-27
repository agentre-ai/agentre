import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { PlanApproveCard } from "./card";
import { useChatStreamsStore } from "@/stores/chat-streams-store";
import type { ChatBlockData } from "@/stores/chat-streams-store";

vi.mock("../../../../../wailsjs/go/app/App", () => ({
  ResolvePlanAction: vi.fn().mockResolvedValue(undefined),
}));

import { ResolvePlanAction } from "../../../../../wailsjs/go/app/App";

// PlanApproveCard 现在只看 canonical.actions[],不再读 session meta
// 的 permissionModeAtLaunch(那条规则迁到 backend handlers/plan_approve.go)。

function blockWithActions(
  actions: {
    id: string;
    kind: "approve" | "refine";
    requiresFeedback?: boolean;
  }[],
): ChatBlockData {
  return {
    type: "tool_use",
    toolName: "ExitPlanMode",
    canonical: {
      kind: "plan.approve_request",
      planApprove: {
        requestId: "req-1",
        planText: "# plan\n- step 1",
        actions,
      },
    },
  } as unknown as ChatBlockData;
}

function actionPlanBlock(): ChatBlockData {
  return {
    type: "plan",
    text: "# Plan\n- inspect\n- patch",
    canonical: {
      kind: "plan.update",
      planUpdate: {
        text: "# Plan\n- inspect\n- patch",
        actions: [
          { id: "plan.execute", kind: "approve" },
          { id: "plan.refine", kind: "refine", requiresFeedback: true },
        ],
        steps: [],
      },
    },
  } as unknown as ChatBlockData;
}

describe("PlanApproveCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useChatStreamsStore.setState({ streams: new Map() });
  });

  it("renders nothing without canonical", () => {
    const block = { type: "tool_use" } as unknown as ChatBlockData;
    const { container } = render(
      <PlanApproveCard toolBlock={block} sessionId={1} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders pending state with header copy", () => {
    const block = blockWithActions([
      { id: "plan.approve.accept_edits", kind: "approve" },
      { id: "plan.approve.manual", kind: "approve" },
      { id: "plan.refine", kind: "refine", requiresFeedback: true },
    ]);
    render(<PlanApproveCard toolBlock={block} sessionId={1} />);
    expect(screen.getByText("AI 提交了执行计划")).toBeDefined();
    expect(screen.getByText("继续规划")).toBeDefined();
  });

  it("non-bypass actions: 渲染 accept_edits + manual + refine, 无 bypass", () => {
    const block = blockWithActions([
      { id: "plan.approve.accept_edits", kind: "approve" },
      { id: "plan.approve.manual", kind: "approve" },
      { id: "plan.refine", kind: "refine", requiresFeedback: true },
    ]);
    render(<PlanApproveCard toolBlock={block} sessionId={1} />);
    expect(screen.getByText("批准并切换自动模式")).toBeDefined();
    expect(screen.getByText("批准,手动确认编辑")).toBeDefined();
    expect(screen.queryByText("批准并跳过权限确认")).toBeNull();
  });

  it("bypass actions: 渲染 bypass + manual + refine, 无 accept_edits", () => {
    const block = blockWithActions([
      { id: "plan.approve.bypass_permissions", kind: "approve" },
      { id: "plan.approve.manual", kind: "approve" },
      { id: "plan.refine", kind: "refine", requiresFeedback: true },
    ]);
    render(<PlanApproveCard toolBlock={block} sessionId={1} />);
    expect(screen.getByText("批准并跳过权限确认")).toBeDefined();
    expect(screen.getByText("批准,手动确认编辑")).toBeDefined();
    expect(screen.queryByText("批准并切换自动模式")).toBeNull();
  });

  it("点 accept_edits → ResolvePlanAction(plan.approve.accept_edits)", async () => {
    const block = blockWithActions([
      { id: "plan.approve.accept_edits", kind: "approve" },
      { id: "plan.approve.manual", kind: "approve" },
      { id: "plan.refine", kind: "refine", requiresFeedback: true },
    ]);
    render(<PlanApproveCard toolBlock={block} sessionId={1} />);
    fireEvent.click(screen.getByText("批准并切换自动模式"));
    await waitFor(() => {
      expect(ResolvePlanAction).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: 1,
          requestId: "req-1",
          actionId: "plan.approve.accept_edits",
          feedback: "",
        }),
      );
    });
  });

  it("点 manual → ResolvePlanAction(plan.approve.manual)", async () => {
    const block = blockWithActions([
      { id: "plan.approve.accept_edits", kind: "approve" },
      { id: "plan.approve.manual", kind: "approve" },
      { id: "plan.refine", kind: "refine", requiresFeedback: true },
    ]);
    render(<PlanApproveCard toolBlock={block} sessionId={1} />);
    fireEvent.click(screen.getByText("批准,手动确认编辑"));
    await waitFor(() => {
      expect(ResolvePlanAction).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: 1,
          requestId: "req-1",
          actionId: "plan.approve.manual",
        }),
      );
    });
  });

  it("点 bypass → ResolvePlanAction(plan.approve.bypass_permissions)", async () => {
    const block = blockWithActions([
      { id: "plan.approve.bypass_permissions", kind: "approve" },
      { id: "plan.approve.manual", kind: "approve" },
      { id: "plan.refine", kind: "refine", requiresFeedback: true },
    ]);
    render(<PlanApproveCard toolBlock={block} sessionId={1} />);
    fireEvent.click(screen.getByText("批准并跳过权限确认"));
    await waitFor(() => {
      expect(ResolvePlanAction).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: 1,
          requestId: "req-1",
          actionId: "plan.approve.bypass_permissions",
        }),
      );
    });
  });

  it("refine 按钮展开 feedback 并传给 ResolvePlanAction(plan.refine)", async () => {
    const block = blockWithActions([
      { id: "plan.approve.accept_edits", kind: "approve" },
      { id: "plan.approve.manual", kind: "approve" },
      { id: "plan.refine", kind: "refine", requiresFeedback: true },
    ]);
    render(<PlanApproveCard toolBlock={block} sessionId={1} />);
    fireEvent.click(screen.getByText("继续规划"));
    fireEvent.change(screen.getByPlaceholderText(/步骤 2/), {
      target: { value: "再细一些" },
    });
    fireEvent.click(screen.getByText("发送反馈并继续规划"));
    await waitFor(() => {
      expect(ResolvePlanAction).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: 1,
          requestId: "req-1",
          actionId: "plan.refine",
          feedback: "再细一些",
        }),
      );
    });
  });

  it("renders resolved-allowed banner without action buttons", () => {
    const block = {
      type: "tool_use",
      toolName: "ExitPlanMode",
      canonical: {
        kind: "plan.approve_request",
        planApprove: {
          requestId: "req-1",
          planText: "x",
          resolved: true,
          allowed: true,
        },
      },
    } as unknown as ChatBlockData;
    render(<PlanApproveCard toolBlock={block} sessionId={1} />);
    expect(screen.getByText("已批准执行计划")).toBeDefined();
    expect(screen.getByText("开始执行计划")).toBeDefined();
    expect(screen.queryByText("批准并切换自动模式")).toBeNull();
    expect(screen.queryByText("继续规划")).toBeNull();
  });

  it("renders type=plan block from canonical.plan.update actions", () => {
    render(<PlanApproveCard toolBlock={actionPlanBlock()} sessionId={1} />);
    expect(screen.getByTestId("plan-card")).toBeDefined();
    expect(screen.getByText("执行计划")).toBeDefined();
    expect(screen.getByText("继续完善")).toBeDefined();
    expect(screen.getByText("选择下一步操作,或反馈继续规划")).toBeDefined();
  });

  it("plan.execute action does not require a requestId", async () => {
    render(<PlanApproveCard toolBlock={actionPlanBlock()} sessionId={1} />);
    fireEvent.click(screen.getByText("执行计划"));
    await waitFor(() => {
      expect(ResolvePlanAction).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: 1,
          requestId: "",
          actionId: "plan.execute",
          feedback: "",
        }),
      );
    });
  });

  it("keeps request-backed approval actions enabled while the session stream is waiting", () => {
    useChatStreamsStore.getState().openStream({
      name: "chat.stream.waiting",
      sessionId: 1,
      assistantMessageId: 99,
      streamStartedAt: Date.now(),
    });
    const block = blockWithActions([
      { id: "plan.approve.accept_edits", kind: "approve" },
      { id: "plan.refine", kind: "refine", requiresFeedback: true },
    ]);

    render(<PlanApproveCard toolBlock={block} sessionId={1} />);

    const approveButton = screen
      .getByText("批准并切换自动模式")
      .closest("button") as HTMLButtonElement;
    const refineButton = screen
      .getByText("继续规划")
      .closest("button") as HTMLButtonElement;
    expect(approveButton.disabled).toBe(false);
    expect(refineButton.disabled).toBe(false);
  });

  it("disables requestless plan actions while the session has an active stream", () => {
    useChatStreamsStore.getState().openStream({
      name: "chat.stream.running",
      sessionId: 1,
      assistantMessageId: 99,
      streamStartedAt: Date.now(),
    });

    render(<PlanApproveCard toolBlock={actionPlanBlock()} sessionId={1} />);

    const executeButton = screen
      .getByText("执行计划")
      .closest("button") as HTMLButtonElement;
    expect(executeButton.disabled).toBe(true);
  });

  it("plan.execute starts the returned stream in the parent transcript", async () => {
    const onPlanActionStarted = vi.fn();
    vi.mocked(ResolvePlanAction).mockResolvedValueOnce({
      sessionId: 1,
      userMessageId: 10,
      assistantMessageId: 11,
      stream: "chat.stream.1.11",
    });

    render(
      <PlanApproveCard
        toolBlock={actionPlanBlock()}
        sessionId={1}
        onPlanActionStarted={onPlanActionStarted}
      />,
    );
    fireEvent.click(screen.getByText("执行计划"));

    await waitFor(() => {
      expect(onPlanActionStarted).toHaveBeenCalledWith(
        {
          sessionId: 1,
          userMessageId: 10,
          assistantMessageId: 11,
          stream: "chat.stream.1.11",
        },
        "Implement the plan.",
      );
    });
  });

  it("hides requestless plan actions after successful submission", async () => {
    vi.mocked(ResolvePlanAction).mockResolvedValueOnce({
      sessionId: 1,
      userMessageId: 10,
      assistantMessageId: 11,
      stream: "chat.stream.1.11",
    });

    render(<PlanApproveCard toolBlock={actionPlanBlock()} sessionId={1} />);
    fireEvent.click(screen.getByText("执行计划"));

    await waitFor(() => {
      expect(screen.queryByText("执行计划")).toBeNull();
    });
    expect(screen.getByText("已批准执行计划")).toBeDefined();
  });

  it("shows backend error detail when plan action submission rejects", async () => {
    const err = {};
    Object.defineProperty(err, "message", {
      value: "当前会话已有进行中的对话，请稍后再试",
      enumerable: false,
    });
    vi.mocked(ResolvePlanAction).mockRejectedValueOnce(err);

    render(<PlanApproveCard toolBlock={actionPlanBlock()} sessionId={1} />);
    fireEvent.click(screen.getByText("执行计划"));

    expect(
      await screen.findByText("当前会话已有进行中的对话，请稍后再试"),
    ).toBeDefined();
  });

  it("requestless plan.refine action sends feedback through ResolvePlanAction", async () => {
    render(<PlanApproveCard toolBlock={actionPlanBlock()} sessionId={1} />);
    fireEvent.click(screen.getByText("继续完善"));
    fireEvent.change(screen.getByPlaceholderText(/步骤 2/), {
      target: { value: "把测试写具体一点" },
    });
    fireEvent.click(screen.getByText("发送反馈并继续规划"));
    await waitFor(() => {
      expect(ResolvePlanAction).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: 1,
          requestId: "",
          actionId: "plan.refine",
          feedback: "把测试写具体一点",
        }),
      );
    });
  });
});
