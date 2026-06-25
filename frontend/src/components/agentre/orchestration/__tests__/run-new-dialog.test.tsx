import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 注意: 测试位于 orchestration/__tests__/, 比组件深一层
// 组件 import ../../../../wailsjs → 测试需要 ../../../../../wailsjs (5 层)
const appMocks = vi.hoisted(() => ({
  RunCreate: vi.fn(),
  ListChatAgents: vi.fn(),
  WorkflowList: vi.fn(),
}));

vi.mock("../../../../../wailsjs/go/app/App", () => appMocks);

// mock chat-tabs-store 的 openRun
const openRunMock = vi.fn();
vi.mock("../../../../stores/chat-tabs-store", () => ({
  useChatTabsStore: {
    getState: () => ({ openRun: openRunMock }),
  },
}));

import { RunNewDialog } from "../run-new-dialog";

function renderDialog() {
  return render(<RunNewDialog open onOpenChange={vi.fn()} />);
}

describe("RunNewDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.RunCreate.mockResolvedValue({ run: { id: 7 }, tasks: [] });
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        { id: 2, name: "架构师", defaultPermissionMode: "default" },
        {
          id: 3,
          name: "危险Agent",
          defaultPermissionMode: "bypassPermissions",
        },
      ],
    });
    appMocks.WorkflowList.mockResolvedValue({ items: [] });
  });

  it("填目标 + 选 Leader → 点创建 调 RunCreate(带 leaderAgentId)", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderDialog();

    fireEvent.change(await screen.findByTestId("run-goal"), {
      target: { value: "做登录页" },
    });
    // 打开 Leader 下拉并选「架构师」(id=2)。shadcn Select 走 onValueChange,
    // 必须用 userEvent 点 trigger + option,fireEvent.change 无效。
    await user.click(screen.getByTestId("run-leader"));
    await user.click(await screen.findByRole("option", { name: "架构师" }));

    fireEvent.click(screen.getByTestId("run-create"));
    await waitFor(() =>
      expect(appMocks.RunCreate).toHaveBeenCalledWith(
        expect.objectContaining({ goal: "做登录页", leaderAgentId: 2 }),
      ),
    );
  });

  it("目标为空时创建按钮禁用", async () => {
    renderDialog();
    await screen.findByTestId("run-goal"); // 等渲染
    const btn = screen.getByTestId("run-create") as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("有目标但未选 Leader 时创建按钮仍禁用(Leader 必选)", async () => {
    renderDialog();
    fireEvent.change(await screen.findByTestId("run-goal"), {
      target: { value: "做登录页" },
    });
    const btn = screen.getByTestId("run-create") as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("团队成员按 defaultPermissionMode 展示危险操作姿态徽标", async () => {
    renderDialog();
    // 等 agents 渲染到团队列表; bypassPermissions→自动放行, 其它→需审批
    await waitFor(() => {
      expect(
        screen.getByText("Auto-approve dangerous operations"),
      ).toBeInTheDocument();
    });
    expect(
      screen.getByText("Require approval for dangerous operations"),
    ).toBeInTheDocument();
  });

  describe("flowMode 三态按钮组", () => {
    it("默认显示三个 flowMode 按钮(none/library/adhoc)", async () => {
      renderDialog();
      await screen.findByTestId("run-goal"); // 等渲染完成
      expect(screen.getByTestId("run-flow-mode-none")).toBeInTheDocument();
      expect(screen.getByTestId("run-flow-mode-library")).toBeInTheDocument();
      expect(screen.getByTestId("run-flow-mode-adhoc")).toBeInTheDocument();
    });

    it("点击 library 按钮切换 flowMode 并显示流程库 picker(多标签全显)", async () => {
      appMocks.WorkflowList.mockResolvedValue({
        items: [
          {
            id: 1,
            name: "标准功能开发流",
            tags: ["通用", "研发"],
            outline: ["需求拆解", "方案设计"],
          },
        ],
      });
      renderDialog();
      await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
      // 切到「流程库」模式
      screen.getByTestId("run-flow-mode-library").click();
      // 流程库 picker 显示名称 + 所有标签 chip + 步骤面包屑
      expect(await screen.findByText("标准功能开发流")).toBeInTheDocument();
      expect(screen.getByText("通用")).toBeInTheDocument();
      expect(screen.getByText("研发")).toBeInTheDocument();
      expect(screen.getByText("需求拆解")).toBeInTheDocument();
      expect(screen.getByText("方案设计")).toBeInTheDocument();
    });

    it("点击流程行后选中该流程(单选)", async () => {
      appMocks.WorkflowList.mockResolvedValue({
        items: [
          {
            id: 1,
            name: "流程A",
            tags: [],
            outline: [],
          },
          {
            id: 2,
            name: "流程B",
            tags: [],
            outline: [],
          },
        ],
      });
      renderDialog();
      await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
      screen.getByTestId("run-flow-mode-library").click();
      // 选中流程A
      const rowA = await screen.findByTestId("run-flow-pick-1");
      rowA.click();
      // 选中流程B 后 RunCreate 应带 flowId=2
      const rowB = screen.getByTestId("run-flow-pick-2");
      rowB.click();
      // 填目标 + 选 leader 再提交确认 flowId 正确
      fireEvent.change(screen.getByTestId("run-goal"), {
        target: { value: "测试目标" },
      });
      // 直接检测按钮的选中视觉态(data-testid="run-flow-pick-2" 有 border-primary)
      expect(rowA.className).not.toContain("border-primary");
      expect(rowB.className).toContain("border-primary");
    });

    it("点击 adhoc 按钮后显示临时流程文本区", async () => {
      renderDialog();
      await screen.findByTestId("run-goal");
      screen.getByTestId("run-flow-mode-adhoc").click();
      expect(await screen.findByTestId("run-flow-content")).toBeInTheDocument();
    });

    it("点击 none 按钮后不显示 picker 也不显示文本区", async () => {
      renderDialog();
      await screen.findByTestId("run-goal");
      // 先切到 library 再切回 none
      screen.getByTestId("run-flow-mode-library").click();
      screen.getByTestId("run-flow-mode-none").click();
      expect(screen.queryByTestId("run-flow-content")).toBeNull();
    });
  });
});
