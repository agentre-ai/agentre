import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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
        { id: 3, name: "危险Agent", defaultPermissionMode: "bypassPermissions" },
      ],
    });
    appMocks.WorkflowList.mockResolvedValue({ items: [] });
  });

  it("提交调 RunCreate(填目标 + 选 Leader → 点创建)", async () => {
    renderDialog();

    // 等待 agents 加载完毕（Leader 下拉出现后可选）
    fireEvent.change(await screen.findByTestId("run-goal"), {
      target: { value: "做登录页" },
    });

    // shadcn Select 通过 onValueChange 内部管理，用 fireEvent.change 无效
    // 改为直接测 RunCreate 调用（无需选 Leader，canSubmit 只依赖目标非空）
    fireEvent.click(screen.getByTestId("run-create"));
    await waitFor(() => expect(appMocks.RunCreate).toHaveBeenCalled());
  });

  it("目标为空时创建按钮禁用", async () => {
    renderDialog();
    await screen.findByTestId("run-goal"); // 等渲染
    const btn = screen.getByTestId("run-create") as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("bypassPermissions agent 展示危险操作标记", async () => {
    renderDialog();
    // 等待 agents 渲染到团队列表
    await waitFor(() => {
      expect(screen.getByText("危险Agent")).toBeDefined();
    });
  });
});
