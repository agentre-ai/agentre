import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const workflowList = vi.fn();
const workflowCreate = vi.fn();
const workflowUpdate = vi.fn();
const workflowDelete = vi.fn();

vi.mock("../../../../wailsjs/go/app/App", () => ({
  WorkflowList: (...a: unknown[]) => workflowList(...a),
  WorkflowCreate: (...a: unknown[]) => workflowCreate(...a),
  WorkflowUpdate: (...a: unknown[]) => workflowUpdate(...a),
  WorkflowDelete: (...a: unknown[]) => workflowDelete(...a),
}));

import { WorkflowManagerDialog } from "./workflow-manager-dialog";
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

const items = [
  {
    id: 1,
    name: "产品开发流程",
    content: "# 产品开发流程\n\n适用:新功能完整交付。\n\n## 角色",
    groupCount: 2,
    createtime: 1700000000000,
    updatetime: 1700000000000,
  },
  {
    id: 2,
    name: "紧急修复流程",
    content: "",
    groupCount: 0,
    createtime: 1700000000000,
    updatetime: 1700000000000,
  },
];

function resetAll() {
  workflowList.mockReset().mockResolvedValue({ items });
  workflowCreate.mockReset().mockResolvedValue({ item: { id: 9 } });
  workflowUpdate.mockReset().mockResolvedValue({ item: { id: 1 } });
  workflowDelete.mockReset().mockResolvedValue({});
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

  it("新建按钮 → 编辑器 → 保存调 WorkflowCreate", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-new-button"));
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "评审流程" },
    });
    await user.click(screen.getByTestId("workflow-save-button"));
    await waitFor(() =>
      expect(workflowCreate).toHaveBeenCalledWith({
        name: "评审流程",
        content: "",
      }),
    );
    expect(workflowList.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("intent=create 打开即编辑器", async () => {
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openCreate();
    await waitFor(() =>
      expect(screen.getByRole("textbox", { name: "Name" })).toBeTruthy(),
    );
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

describe("WorkflowManagerDialog · 内联删除", () => {
  beforeEach(resetAll);

  it("删除图标 → 内联确认条(带使用中群数) → 确认调 WorkflowDelete", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-row-1"));
    await user.click(screen.getByTestId("workflow-delete-button"));
    expect(screen.getByTestId("workflow-delete-confirm")).toBeTruthy();
    expect(
      screen.getByText(
        '"产品开发流程" is used by 2 groups; after deletion they fall back to "no workflow". This cannot be undone.',
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
