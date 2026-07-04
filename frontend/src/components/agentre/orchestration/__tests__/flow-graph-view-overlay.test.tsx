import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { FlowGraphView } from "../flow-graph-view";

const graph = JSON.stringify({
  version: 1,
  nodes: [
    { id: "n1", label: "FE", kind: "task" },
    { id: "n2", label: "BE", kind: "task" },
  ],
  edges: [],
});

describe("FlowGraphView overlay", () => {
  it("不传 overlay → 无状态色 class(向后兼容)", () => {
    render(<FlowGraphView graph={graph} />);
    const node = screen.getByTestId("flow-node-n1");
    expect(node.className).not.toContain("border-status-running");
    expect(node.className).not.toContain("border-destructive");
  });

  it("done 节点着绿(status-running token)", () => {
    render(
      <FlowGraphView
        graph={graph}
        overlay={{ n1: { status: "done", count: 2 } }}
      />,
    );
    expect(screen.getByTestId("flow-node-n1").className).toContain(
      "border-status-running",
    );
  });

  it("error 节点着红", () => {
    render(
      <FlowGraphView
        graph={graph}
        overlay={{ n1: { status: "error", count: 1 } }}
      />,
    );
    expect(screen.getByTestId("flow-node-n1").className).toContain(
      "border-destructive",
    );
  });

  it("count>0 渲染计数徽章", () => {
    render(
      <FlowGraphView
        graph={graph}
        overlay={{ n1: { status: "running", count: 3 } }}
      />,
    );
    expect(screen.getByTestId("flow-node-n1-count").textContent).toBe("3");
  });

  it("count=0 不渲染徽章", () => {
    render(
      <FlowGraphView
        graph={graph}
        overlay={{ n1: { status: "pending", count: 0 } }}
      />,
    );
    expect(screen.queryByTestId("flow-node-n1-count")).toBeNull();
  });
});

describe("FlowGraphView 节点点击/高亮", () => {
  it("不传 onNodeClick → 节点无 role=button(向后兼容,只读)", () => {
    render(<FlowGraphView graph={graph} />);
    expect(screen.getByTestId("flow-node-n1").getAttribute("role")).toBeNull();
  });

  it("传 onNodeClick → 点节点回调该节点 label", () => {
    const onNodeClick = vi.fn();
    render(<FlowGraphView graph={graph} onNodeClick={onNodeClick} />);
    fireEvent.click(screen.getByTestId("flow-node-n2"));
    expect(onNodeClick).toHaveBeenCalledWith("BE");
  });

  it("selectedLabel 匹配的节点高亮(data-selected + ring)", () => {
    const onNodeClick = vi.fn();
    render(
      <FlowGraphView
        graph={graph}
        onNodeClick={onNodeClick}
        selectedLabel="fe"
      />,
    );
    const n1 = screen.getByTestId("flow-node-n1");
    const n2 = screen.getByTestId("flow-node-n2");
    expect(n1.getAttribute("data-selected")).toBe("true");
    expect(n1.className).toContain("ring-2");
    expect(n2.getAttribute("data-selected")).toBeNull();
  });
});
