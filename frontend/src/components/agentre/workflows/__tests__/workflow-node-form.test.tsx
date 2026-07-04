import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { FlowNode } from "../../orchestration/flow-graph";
import { WorkflowNodeForm } from "../workflow-node-form";

const base: FlowNode = { id: "n2", label: "Build", kind: "task" };

function renderForm(overrides: Partial<Parameters<typeof WorkflowNodeForm>[0]> = {}) {
  const props = {
    node: base,
    index: 1,
    earlier: [{ id: "n1", label: "Plan", kind: "leader" as const }],
    others: [{ id: "n1", label: "Plan", kind: "leader" as const }],
    dependsOn: [] as string[],
    bounce: null as string | null,
    canRemove: true,
    onLabelChange: vi.fn(),
    onKindChange: vi.fn(),
    onBriefChange: vi.fn(),
    onToggleDependsOn: vi.fn(),
    onBounceChange: vi.fn(),
    onMoveUp: vi.fn(),
    onMoveDown: vi.fn(),
    onRemove: vi.fn(),
    ...overrides,
  };
  render(<WorkflowNodeForm {...props} />);
  return props;
}

describe("WorkflowNodeForm", () => {
  it("task 节点显示 brief 输入", () => {
    renderForm();
    expect(screen.getByTestId("node-n2-brief")).toBeInTheDocument();
  });

  it("leader 节点隐藏 brief 输入", () => {
    renderForm({ node: { id: "n2", label: "Wrap", kind: "leader" } });
    expect(screen.queryByTestId("node-n2-brief")).toBeNull();
  });

  it("改 label 触发 onLabelChange", () => {
    const props = renderForm();
    fireEvent.change(screen.getByTestId("node-n2-label"), {
      target: { value: "Ship" },
    });
    expect(props.onLabelChange).toHaveBeenCalledWith("Ship");
  });

  it("依赖 chip 点击触发 onToggleDependsOn", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const props = renderForm();
    await user.click(screen.getByTestId("node-n2-dep-n1"));
    expect(props.onToggleDependsOn).toHaveBeenCalledWith("n1");
  });

  it("已选依赖 chip 标记 aria-pressed", () => {
    renderForm({ dependsOn: ["n1"] });
    expect(screen.getByTestId("node-n2-dep-n1").getAttribute("aria-pressed")).toBe("true");
  });

  it("无前序节点时不渲染依赖区", () => {
    renderForm({ earlier: [] });
    expect(screen.queryByTestId("node-n2-dep-n1")).toBeNull();
  });

  it("删除禁用当 canRemove=false", () => {
    renderForm({ canRemove: false });
    expect((screen.getByTestId("node-n2-remove") as HTMLButtonElement).disabled).toBe(true);
  });
});
