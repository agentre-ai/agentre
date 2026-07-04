import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

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
