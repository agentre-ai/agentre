import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

let items: unknown[] = [];
vi.mock("@/hooks/use-workflows", () => ({
  useWorkflows: () => ({ workflows: items }),
}));

import { RunFlowBlueprint } from "../run-flow-blueprint";

describe("RunFlowBlueprint", () => {
  it("flowId=0 → 不渲染", () => {
    items = [];
    const { container } = render(<RunFlowBlueprint flowId={0} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("找到流程 → 渲染名称 + 步骤面包屑 + 参考提示", () => {
    items = [
      {
        id: 7,
        name: "标准功能开发流",
        outline: ["需求拆解", "灰度上线"],
        tags: [],
        content: "",
        runCount: 0,
        createtime: 0,
        updatetime: 0,
      },
    ];
    render(<RunFlowBlueprint flowId={7} />);
    expect(screen.getByText("标准功能开发流")).toBeInTheDocument();
    expect(screen.getByText("需求拆解")).toBeInTheDocument();
    expect(screen.getByText("灰度上线")).toBeInTheDocument();
    expect(screen.getByTestId("run-flow-blueprint")).toBeInTheDocument();
  });

  it("flowId 找不到对应流程 → 不渲染", () => {
    items = [
      {
        id: 1,
        name: "x",
        outline: [],
        tags: [],
        content: "",
        runCount: 0,
        createtime: 0,
        updatetime: 0,
      },
    ];
    const { container } = render(<RunFlowBlueprint flowId={7} />);
    expect(container).toBeEmptyDOMElement();
  });
});
