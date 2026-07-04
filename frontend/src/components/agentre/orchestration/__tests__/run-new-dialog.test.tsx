import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 注意: 测试位于 orchestration/__tests__/, 比组件深一层
// 组件 import ../../../../wailsjs → 测试需要 ../../../../../wailsjs (5 层)
const appMocks = vi.hoisted(() => ({
  RunCreate: vi.fn(),
  ListChatAgents: vi.fn(),
  WorkflowList: vi.fn(),
  ProjectListTree: vi.fn(),
  LoadOrg: vi.fn(),
}));

vi.mock("../../../../../wailsjs/go/app/App", () => appMocks);

// mock react-router-dom useNavigate
const mockNavigate = vi.fn();
vi.mock("react-router-dom", () => ({
  useNavigate: () => mockNavigate,
}));

import { useWorkflowManagerStore } from "../../../../stores/workflow-manager-store";
import { RunNewDialog } from "../run-new-dialog";

function renderDialog() {
  return render(<RunNewDialog open onOpenChange={vi.fn()} />);
}

describe("RunNewDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useWorkflowManagerStore.setState({ open: false, intent: "browse" });
    appMocks.RunCreate.mockResolvedValue({ run: { id: 7 }, tasks: [] });
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        {
          id: 2,
          name: "架构师",
          avatarColor: "agent-1",
          backendType: "claudecode",
          defaultPermissionMode: "default",
        },
        {
          id: 3,
          name: "危险Agent",
          avatarColor: "agent-4",
          backendType: "codex",
          defaultPermissionMode: "bypassPermissions",
        },
      ],
    });
    appMocks.LoadOrg.mockResolvedValue({
      departments: [
        {
          id: 10,
          name: "研发部",
          icon: "code",
          accentColor: "agent-1",
          sortOrder: 0,
        },
      ],
      agents: [
        { id: 2, departmentId: 10 },
        { id: 3, departmentId: 0 },
      ],
    });
    appMocks.WorkflowList.mockResolvedValue({ items: [] });
    appMocks.ProjectListTree.mockResolvedValue([
      { project: { id: 5, name: "我的项目" }, children: [] },
    ]);
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

  it("RunCreate 成功后 navigate 到 /orchestration/:runId", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderDialog();

    fireEvent.change(await screen.findByTestId("run-goal"), {
      target: { value: "做登录页" },
    });
    await user.click(screen.getByTestId("run-leader"));
    await user.click(await screen.findByRole("option", { name: "架构师" }));

    fireEvent.click(screen.getByTestId("run-create"));
    await waitFor(() =>
      expect(mockNavigate).toHaveBeenCalledWith("/orchestration/7"),
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

  describe("可参与团队(双栏部门选择器)", () => {
    it("默认 全部 视图列出可参与 agent", async () => {
      renderDialog();
      expect(await screen.findByTestId("run-team-agent-2")).toBeInTheDocument();
      expect(screen.getByTestId("run-team-agent-3")).toBeInTheDocument();
    });

    it("勾选的 agent 进入 RunCreate.allowedAgentIds", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      renderDialog();
      fireEvent.change(await screen.findByTestId("run-goal"), {
        target: { value: "做登录页" },
      });
      await user.click(screen.getByTestId("run-leader"));
      await user.click(await screen.findByRole("option", { name: "架构师" }));
      await user.click(await screen.findByTestId("run-team-agent-3"));
      await user.click(screen.getByTestId("run-create"));
      await waitFor(() =>
        expect(appMocks.RunCreate).toHaveBeenCalledWith(
          expect.objectContaining({ allowedAgentIds: [3] }),
        ),
      );
    });

    it("已选计数随选择更新", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      renderDialog();
      const count = await screen.findByTestId("run-team-count");
      expect(count.textContent).toMatch(/0/);
      await user.click(await screen.findByTestId("run-team-agent-3"));
      await waitFor(() => expect(count.textContent).toMatch(/1/));
    });

    it("部门『全选』把该部门成员写入 allowedAgentIds", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      renderDialog();
      fireEvent.change(await screen.findByTestId("run-goal"), {
        target: { value: "x" },
      });
      await user.click(screen.getByTestId("run-leader"));
      await user.click(await screen.findByRole("option", { name: "架构师" }));
      await user.click(await screen.findByTestId("run-team-scope-10"));
      await user.click(screen.getByTestId("run-team-select-all"));
      await user.click(screen.getByTestId("run-create"));
      await waitFor(() =>
        expect(appMocks.RunCreate).toHaveBeenCalledWith(
          expect.objectContaining({ allowedAgentIds: [2] }),
        ),
      );
    });
  });

  describe("flowMode 三态按钮组", () => {
    it("起始流程 label 文案为「编排流程」(en: Orchestration flow)", async () => {
      renderDialog();
      expect(await screen.findByText("Orchestration flow")).toBeInTheDocument();
    });

    it("不再有「从零开始」段, 只有 library/adhoc 两段", async () => {
      renderDialog();
      await screen.findByTestId("run-goal");
      expect(screen.queryByTestId("run-flow-mode-none")).toBeNull();
      expect(screen.getByTestId("run-flow-mode-library")).toBeInTheDocument();
      expect(screen.getByTestId("run-flow-mode-adhoc")).toBeInTheDocument();
    });

    it("默认落在 library 并预选 isDefault 流程 → RunCreate 带该 flowId", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      appMocks.WorkflowList.mockResolvedValue({
        items: [
          {
            id: 1,
            name: "Custom",
            tags: [],
            outline: [],
            graph: "",
            isDefault: false,
          },
          {
            id: 2,
            name: "Default Orchestration Flow",
            tags: ["Default"],
            outline: [],
            graph: JSON.stringify({
              version: 1,
              nodes: [{ id: "a", label: "See", kind: "leader" }],
              edges: [],
            }),
            isDefault: true,
          },
        ],
      });
      renderDialog();
      await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
      // 预选默认流程 → 渲染其 mini-DAG(flow-node-a)
      expect(await screen.findByTestId("flow-node-a")).toBeInTheDocument();
      fireEvent.change(screen.getByTestId("run-goal"), {
        target: { value: "g" },
      });
      await user.click(screen.getByTestId("run-leader"));
      await user.click(await screen.findByRole("option", { name: "架构师" }));
      await user.click(screen.getByTestId("run-create"));
      await waitFor(() =>
        expect(appMocks.RunCreate).toHaveBeenCalledWith(
          expect.objectContaining({ flowId: 2 }),
        ),
      );
    });

    it("切到 library 模式后下拉列出流程(名称 + 标签)", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
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
      await user.click(screen.getByTestId("run-flow-mode-library"));
      await user.click(await screen.findByTestId("run-flow-select"));
      expect(
        await screen.findByRole("option", { name: /标准功能开发流/ }),
      ).toBeInTheDocument();
      expect(screen.getByText("通用")).toBeInTheDocument();
      expect(screen.getByText("研发")).toBeInTheDocument();
    });

    it("选中流程后渲染流程步骤面包屑(run-flow-outline)", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
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
      await user.click(screen.getByTestId("run-flow-mode-library"));
      await user.click(await screen.findByTestId("run-flow-select"));
      await user.click(
        await screen.findByRole("option", { name: /标准功能开发流/ }),
      );
      expect(await screen.findByTestId("run-flow-outline")).toBeInTheDocument();
      expect(screen.getByText("需求拆解")).toBeInTheDocument();
    });

    it("从下拉选中流程后, RunCreate 带上该 flowId", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      appMocks.WorkflowList.mockResolvedValue({
        items: [
          { id: 1, name: "流程A", tags: [], outline: [] },
          { id: 2, name: "流程B", tags: [], outline: [] },
        ],
      });
      renderDialog();
      await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
      await user.click(screen.getByTestId("run-flow-mode-library"));
      await user.click(await screen.findByTestId("run-flow-select"));
      await user.click(await screen.findByRole("option", { name: /流程B/ }));
      fireEvent.change(screen.getByTestId("run-goal"), {
        target: { value: "测试目标" },
      });
      await user.click(screen.getByTestId("run-leader"));
      await user.click(await screen.findByRole("option", { name: "架构师" }));
      await user.click(screen.getByTestId("run-create"));
      await waitFor(() =>
        expect(appMocks.RunCreate).toHaveBeenCalledWith(
          expect.objectContaining({ flowId: 2 }),
        ),
      );
    });

    it("点击 adhoc 按钮后显示临时流程文本区", async () => {
      renderDialog();
      await screen.findByTestId("run-goal");
      screen.getByTestId("run-flow-mode-adhoc").click();
      expect(await screen.findByTestId("run-flow-content")).toBeInTheDocument();
    });

    it("library 模式下「管理流程库」链接点击打开流程库弹窗", async () => {
      renderDialog();
      await screen.findByTestId("run-goal");
      screen.getByTestId("run-flow-mode-library").click();
      fireEvent.click(await screen.findByTestId("run-flow-manage"));
      expect(useWorkflowManagerStore.getState().open).toBe(true);
    });
  });

  describe("项目选择", () => {
    it("默认不选项目 → RunCreate 带 projectId: 0", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      renderDialog();
      fireEvent.change(await screen.findByTestId("run-goal"), {
        target: { value: "做登录页" },
      });
      await user.click(screen.getByTestId("run-leader"));
      await user.click(await screen.findByRole("option", { name: "架构师" }));
      fireEvent.click(screen.getByTestId("run-create"));
      await waitFor(() =>
        expect(appMocks.RunCreate).toHaveBeenCalledWith(
          expect.objectContaining({ projectId: 0 }),
        ),
      );
    });

    it("选中项目 → RunCreate 带该 projectId", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      renderDialog();
      await waitFor(() => expect(appMocks.ProjectListTree).toHaveBeenCalled());
      fireEvent.change(await screen.findByTestId("run-goal"), {
        target: { value: "做登录页" },
      });
      await user.click(screen.getByTestId("run-leader"));
      await user.click(await screen.findByRole("option", { name: "架构师" }));
      await user.click(screen.getByTestId("run-project"));
      await user.click(await screen.findByRole("option", { name: /我的项目/ }));
      fireEvent.click(screen.getByTestId("run-create"));
      await waitFor(() =>
        expect(appMocks.RunCreate).toHaveBeenCalledWith(
          expect.objectContaining({ projectId: 5 }),
        ),
      );
    });
  });
});
