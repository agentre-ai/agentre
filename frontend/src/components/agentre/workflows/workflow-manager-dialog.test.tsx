import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const workflowList = vi.fn();
const workflowCreate = vi.fn();
const workflowUpdate = vi.fn();
const workflowDelete = vi.fn();
const workflowPreviewGraph = vi.fn();

vi.mock("../../../../wailsjs/go/app/App", () => ({
  WorkflowList: (...a: unknown[]) => workflowList(...a),
  WorkflowCreate: (...a: unknown[]) => workflowCreate(...a),
  WorkflowUpdate: (...a: unknown[]) => workflowUpdate(...a),
  WorkflowDelete: (...a: unknown[]) => workflowDelete(...a),
  WorkflowPreviewGraph: (...a: unknown[]) => workflowPreviewGraph(...a),
}));

import { WorkflowManagerDialog } from "./workflow-manager-dialog";
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

const items = [
  {
    id: 1,
    name: "产品开发流程",
    content: "# 产品开发流程\n\n适用:新功能完整交付。\n\n## 角色",
    runCount: 2,
    createtime: 1700000000000,
    updatetime: 1700000000000,
  },
  {
    id: 2,
    name: "紧急修复流程",
    content: "",
    runCount: 0,
    createtime: 1700000000000,
    updatetime: 1700000000000,
  },
];

function resetAll() {
  workflowList.mockReset().mockResolvedValue({ items });
  workflowCreate.mockReset().mockResolvedValue({ item: { id: 9 } });
  workflowUpdate.mockReset().mockResolvedValue({ item: { id: 1 } });
  workflowDelete.mockReset().mockResolvedValue({});
  workflowPreviewGraph
    .mockReset()
    .mockResolvedValue({ content: "", outline: [] });
  useWorkflowManagerStore.setState({ open: false, intent: "browse" });
}

describe("WorkflowManagerDialog · 浏览态", () => {
  beforeEach(resetAll);

  it("open=false 不渲染内容", () => {
    render(<WorkflowManagerDialog />);
    expect(screen.queryByTestId("workflow-manager")).toBeNull();
  });

  it("openBrowse 渲染列表行 + 选中后右栏预览正文", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    expect(screen.getByText("Select a workflow to preview")).toBeTruthy();
    await user.click(screen.getByTestId("workflow-row-1"));
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { level: 1, name: "产品开发流程" }),
      ).toBeTruthy(),
    );
  });

  it("空列表显示空态", async () => {
    workflowList.mockResolvedValue({ items: [] });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() =>
      expect(
        screen.getByText('No workflows yet — click "New workflow" to start'),
      ).toBeTruthy(),
    );
  });
});

describe("WorkflowManagerDialog · 内联编辑", () => {
  beforeEach(resetAll);

  it("新建按钮 → DAG 设计器 → 保存调 WorkflowCreate(带 graph)", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-new-button"));
    // 新建默认进设计器: 名称 + 首个节点 label(满足保存条件)
    fireEvent.change(await screen.findByTestId("workflow-name-input"), {
      target: { value: "评审流程" },
    });
    fireEvent.change(screen.getByTestId("node-n1-label"), {
      target: { value: "启动评审" },
    });
    await user.click(screen.getByTestId("workflow-save-button"));
    await waitFor(() =>
      expect(workflowCreate).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "评审流程",
          template: "{{ DAGPrompt }}",
          graph: expect.stringContaining("启动评审"),
        }),
      ),
    );
    expect(workflowList.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("intent=create 打开即进 DAG 设计器(名称 + add-node)", async () => {
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openCreate();
    await waitFor(() =>
      expect(screen.getByRole("textbox", { name: "Name" })).toBeTruthy(),
    );
    expect(screen.getByTestId("designer-add-node")).toBeInTheDocument();
  });

  it("选中后点编辑 → 预填名称 → 保存调 WorkflowUpdate", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-row-1"));
    await user.click(screen.getByTestId("workflow-edit-button"));
    const nameInput = screen.getByRole("textbox", {
      name: "Name",
    }) as HTMLInputElement;
    expect(nameInput.value).toBe("产品开发流程");
    fireEvent.change(nameInput, { target: { value: "产品开发流程 v2" } });
    await user.click(screen.getByTestId("workflow-save-button"));
    await waitFor(() =>
      expect(workflowUpdate).toHaveBeenCalledWith(
        expect.objectContaining({ id: 1, name: "产品开发流程 v2" }),
      ),
    );
  });

  it("编辑态按 Esc 回到浏览态,不关弹窗", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-row-1"));
    await user.click(screen.getByTestId("workflow-edit-button"));
    expect(screen.getByRole("textbox", { name: "Name" })).toBeTruthy();
    await user.keyboard("{Escape}");
    // 弹窗仍在(没关),且回到浏览态(编辑按钮回来、编辑器消失)
    expect(screen.getByTestId("workflow-manager")).toBeTruthy();
    await waitFor(() =>
      expect(screen.getByTestId("workflow-edit-button")).toBeTruthy(),
    );
    expect(screen.queryByRole("textbox", { name: "Name" })).toBeNull();
  });
});

const itemsWithTagsOutline = [
  {
    id: 3,
    name: "标准功能开发流",
    content: "# 标准功能开发流",
    tags: ["通用"],
    outline: ["需求拆解", "方案设计"],
    runCount: 0,
    createtime: 1700000000000,
    updatetime: 1700000000000,
  },
];

describe("WorkflowManagerDialog · tags/outline", () => {
  beforeEach(() => {
    workflowList.mockReset().mockResolvedValue({ items: itemsWithTagsOutline });
    workflowCreate.mockReset().mockResolvedValue({ item: { id: 9 } });
    workflowUpdate.mockReset().mockResolvedValue({ item: { id: 3 } });
    workflowDelete.mockReset().mockResolvedValue({});
    workflowPreviewGraph
      .mockReset()
      .mockResolvedValue({ content: "", outline: [] });
    useWorkflowManagerStore.setState({ open: false, intent: "browse" });
  });

  it("预览态渲染蓝图 band(标签 + 步骤面包屑)", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() =>
      expect(screen.getByText("标准功能开发流")).toBeTruthy(),
    );
    await user.click(screen.getByTestId("workflow-row-3"));
    expect(screen.getByTestId("workflow-blueprint-band")).toBeInTheDocument();
    expect(screen.getByText("需求拆解")).toBeInTheDocument();
    expect(screen.getByText("方案设计")).toBeInTheDocument();
  });

  it("编辑保存时把 tags/outline 一并提交给 update", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() =>
      expect(screen.getByText("标准功能开发流")).toBeTruthy(),
    );
    await user.click(screen.getByTestId("workflow-row-3"));
    await user.click(screen.getByTestId("workflow-edit-button"));
    // seed tags/outline already loaded from the item; add one more step
    const stepInput = screen.getByTestId("workflow-outline-input");
    fireEvent.change(stepInput, { target: { value: "测试验收" } });
    fireEvent.keyDown(stepInput, { key: "Enter" });
    await user.click(screen.getByTestId("workflow-save-button"));
    await waitFor(() =>
      expect(workflowUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 3,
          tags: ["通用"],
          outline: ["需求拆解", "方案设计", "测试验收"],
        }),
      ),
    );
  });
});

describe("WorkflowManagerDialog · 内联删除", () => {
  beforeEach(resetAll);

  it("删除图标 → 内联确认条(带使用中 Run 数) → 确认调 WorkflowDelete", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-row-1"));
    await user.click(screen.getByTestId("workflow-delete-button"));
    expect(screen.getByTestId("workflow-delete-confirm")).toBeTruthy();
    expect(
      screen.getByText(
        '"产品开发流程" is used by 2 runs; after deletion they fall back to "no workflow". This cannot be undone.',
      ),
    ).toBeTruthy();
    await user.click(screen.getByTestId("workflow-delete-confirm-button"));
    await waitFor(() => expect(workflowDelete).toHaveBeenCalledWith({ id: 1 }));
  });

  it("删除后回预览空态", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-row-1"));
    workflowList.mockResolvedValue({ items: [items[1]] });
    await user.click(screen.getByTestId("workflow-delete-button"));
    await user.click(screen.getByTestId("workflow-delete-confirm-button"));
    await waitFor(() =>
      expect(screen.getByText("Select a workflow to preview")).toBeTruthy(),
    );
  });

  it("删除失败:保留选中(不跳空态),列表区显示错误", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    workflowDelete.mockRejectedValue(new Error("boom"));
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-row-1"));
    await user.click(screen.getByTestId("workflow-delete-button"));
    await user.click(screen.getByTestId("workflow-delete-confirm-button"));
    // 失败:错误展示 + 仍在预览态(ViewPane h2 标题在),不回空态
    await waitFor(() => expect(screen.getByText("boom")).toBeTruthy());
    expect(
      screen.getByRole("heading", { level: 2, name: "产品开发流程" }),
    ).toBeTruthy();
    expect(screen.queryByText("Select a workflow to preview")).toBeNull();
  });
});

describe("WorkflowManagerDialog · DAG 设计器", () => {
  beforeEach(resetAll);

  it("新建流程直接进 DAG 设计器", async () => {
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openCreate();
    expect(await screen.findByTestId("designer-add-node")).toBeInTheDocument();
  });

  it("编辑 legacy 无 graph 流程 → 自由文本 + 转 DAG 入口, 点转换进设计器", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-row-1"));
    await user.click(screen.getByTestId("workflow-edit-button"));
    expect(screen.getByTestId("workflow-content-input")).toBeInTheDocument();
    expect(screen.getByTestId("workflow-convert-dag")).toBeInTheDocument();
    await user.click(screen.getByTestId("workflow-convert-dag"));
    expect(await screen.findByTestId("designer-add-node")).toBeInTheDocument();
  });

  it("编辑有 graph 的流程 → 设计器载入其节点(flow-node-n1)", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    workflowList.mockResolvedValue({
      items: [
        {
          id: 5,
          name: "DAG 流程",
          content: "",
          runCount: 0,
          createtime: 1700000000000,
          updatetime: 1700000000000,
          graph: JSON.stringify({
            version: 1,
            nodes: [{ id: "n1", label: "Plan", kind: "leader" }],
            edges: [],
          }),
        },
      ],
    });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("DAG 流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-row-5"));
    await user.click(screen.getByTestId("workflow-edit-button"));
    expect(await screen.findByTestId("flow-node-n1")).toBeInTheDocument();
  });

  it("模板报错(预览返回 error)→ 保存按钮置灰", async () => {
    workflowPreviewGraph.mockReset().mockResolvedValue({
      content: "",
      outline: [],
      error: 'function "DAGPromt" not defined',
    });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openCreate();
    fireEvent.change(await screen.findByTestId("workflow-name-input"), {
      target: { value: "报错流程" },
    });
    fireEvent.change(screen.getByTestId("node-n1-label"), {
      target: { value: "步骤一" },
    });
    // 名称+节点都有效,唯一能禁用保存的就是 templateError(来自 250ms 防抖预览)
    await waitFor(() =>
      expect(screen.getByTestId("workflow-save-button")).toBeDisabled(),
    );
  });
});
