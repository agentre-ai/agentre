import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const previewGraph = vi.fn();
vi.mock("../../../../wailsjs/go/app/App", () => ({
  WorkflowPreviewGraph: (...a: unknown[]) => previewGraph(...a),
}));

import { WorkflowDagDesigner } from "../workflow-dag-designer";
import type { FlowGraph } from "../../orchestration/flow-graph";

const graph: FlowGraph = {
  version: 1,
  nodes: [{ id: "n1", label: "拆解", kind: "leader" }],
  edges: [],
};

function setup(template = "{{ DAGPrompt }}") {
  const onTemplateChange = vi.fn();
  const onTemplateError = vi.fn();
  render(
    <WorkflowDagDesigner
      name="F"
      graph={graph}
      template={template}
      error={null}
      onNameChange={() => {}}
      onGraphChange={() => {}}
      onTemplateChange={onTemplateChange}
      onTemplateError={onTemplateError}
    />,
  );
  return { onTemplateChange, onTemplateError };
}

describe("WorkflowDagDesigner 模板 pane", () => {
  beforeEach(() => {
    previewGraph
      .mockReset()
      .mockResolvedValue({ content: "# F\nRENDERED", outline: [], error: "" });
  });

  it("编辑态渲染可编辑模板文本域", () => {
    setup("hello");
    expect(screen.getByTestId("designer-template-input")).toHaveValue("hello");
  });

  it("改文本域回调 onTemplateChange", () => {
    const { onTemplateChange } = setup("a");
    fireEvent.change(screen.getByTestId("designer-template-input"), {
      target: { value: "ab" },
    });
    expect(onTemplateChange).toHaveBeenCalledWith("ab");
  });

  it("插入按钮把 {{ DAGPrompt }} 拼进模板", () => {
    const { onTemplateChange } = setup("x");
    fireEvent.click(screen.getByTestId("designer-insert-token"));
    expect(onTemplateChange).toHaveBeenCalledWith(
      expect.stringContaining("{{ DAGPrompt }}"),
    );
  });

  it("切到预览显示渲染 content", async () => {
    setup("{{ DAGPrompt }}");
    fireEvent.click(screen.getByTestId("designer-tab-preview"));
    await waitFor(() =>
      expect(screen.getByTestId("designer-prompt-preview")).toHaveTextContent(
        "RENDERED",
      ),
    );
  });

  it("预览态渲染容器可选中复制(data-selectable-text)", async () => {
    setup("{{ DAGPrompt }}");
    fireEvent.click(screen.getByTestId("designer-tab-preview"));
    await waitFor(() =>
      expect(screen.getByTestId("designer-prompt-preview")).toHaveTextContent(
        "RENDERED",
      ),
    );
    expect(screen.getByTestId("designer-prompt-preview")).toHaveAttribute(
      "data-selectable-text",
      "true",
    );
  });

  it("预览报错→显示错误并回调 onTemplateError(true)", async () => {
    previewGraph.mockResolvedValue({
      content: "",
      outline: [],
      error: 'function "DAGPromt" not defined',
    });
    const { onTemplateError } = setup("{{ DAGPromt }}");
    await act(async () => {
      await Promise.resolve();
    });
    await waitFor(() => expect(onTemplateError).toHaveBeenCalledWith(true));
    fireEvent.click(screen.getByTestId("designer-tab-preview"));
    await waitFor(() =>
      expect(screen.getByTestId("designer-template-error")).toHaveTextContent(
        "DAGPromt",
      ),
    );
  });
});
