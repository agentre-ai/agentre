import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 设计器 → MarkdownText → RichLink 间接 import wailsjs runtime
vi.mock("../../../../../wailsjs/runtime/runtime", async () => {
  const actual = await vi.importActual<
    typeof import("../../../../../wailsjs/runtime/runtime")
  >("../../../../../wailsjs/runtime/runtime");
  return { ...actual, BrowserOpenURL: vi.fn() };
});

const appMocks = vi.hoisted(() => ({ WorkflowPreviewGraph: vi.fn() }));
vi.mock("../../../../../wailsjs/go/app/App", () => appMocks);

import type { FlowGraph } from "../../orchestration/flow-graph";
import { emptyDraftGraph } from "../flow-graph-draft";
import { WorkflowDagDesigner } from "../workflow-dag-designer";

// 受控 harness: 持有 graph/name state, 让设计器的编辑真正回写。
function Harness() {
  const [name, setName] = React.useState("Flow");
  const [graph, setGraph] = React.useState<FlowGraph>(emptyDraftGraph());
  return (
    <WorkflowDagDesigner
      name={name}
      graph={graph}
      error={null}
      onNameChange={setName}
      onGraphChange={setGraph}
    />
  );
}

describe("WorkflowDagDesigner", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.WorkflowPreviewGraph.mockResolvedValue({
      content: "## Projected prompt",
      outline: [],
    });
  });

  it("渲染初始节点到 DAG(flow-node-n1)", () => {
    render(<Harness />);
    expect(screen.getByTestId("flow-node-n1")).toBeInTheDocument();
  });

  it("点击「添加节点」后 DAG 出现 flow-node-n2", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<Harness />);
    await user.click(screen.getByTestId("designer-add-node"));
    expect(await screen.findByTestId("flow-node-n2")).toBeInTheDocument();
  });

  it("防抖后调用 WorkflowPreviewGraph 并展示投影正文", async () => {
    render(<Harness />);
    await waitFor(() =>
      expect(appMocks.WorkflowPreviewGraph).toHaveBeenCalledWith(
        expect.objectContaining({ name: "Flow" }),
      ),
    );
    expect(await screen.findByText("Projected prompt")).toBeInTheDocument();
  });
});
