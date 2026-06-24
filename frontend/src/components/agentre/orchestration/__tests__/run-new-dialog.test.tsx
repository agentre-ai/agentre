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
});
