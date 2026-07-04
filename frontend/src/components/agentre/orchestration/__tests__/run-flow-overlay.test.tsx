import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { app } from "../../../../../wailsjs/go/models";
import { RunFlowOverlay } from "../run-flow-overlay";

const graph = JSON.stringify({
  version: 1,
  nodes: [
    { id: "n1", label: "FE", kind: "task" },
    { id: "n2", label: "BE", kind: "task" },
  ],
  edges: [{ from: "n1", to: "n2" }],
});

function detail(over: Partial<app.RunDetailDTO> = {}): app.RunDetailDTO {
  return {
    run: { id: 1, flowGraph: graph },
    tasks: [{ id: 9, nodeRef: "FE", status: "done" }],
    ...over,
  } as unknown as app.RunDetailDTO;
}

describe("RunFlowOverlay", () => {
  it("渲染 flow DAG 节点(来自 run.flowGraph 快照)", () => {
    render(<RunFlowOverlay detail={detail()} />);
    expect(screen.getByTestId("run-flow-overlay")).toBeInTheDocument();
    expect(screen.getByTestId("flow-node-n1")).toBeInTheDocument();
  });

  it("按任务实况给节点着色(FE done → 绿)", () => {
    render(<RunFlowOverlay detail={detail()} />);
    expect(screen.getByTestId("flow-node-n1").className).toContain(
      "border-status-running",
    );
  });

  it("无 flowGraph → 空态", () => {
    render(
      <RunFlowOverlay
        detail={detail({ run: { id: 1, flowGraph: "" } as app.RunItemDTO })}
      />,
    );
    expect(screen.getByTestId("run-flow-empty")).toBeInTheDocument();
  });
});
