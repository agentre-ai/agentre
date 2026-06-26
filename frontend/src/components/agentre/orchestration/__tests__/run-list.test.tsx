import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 注意: 测试位于 orchestration/__tests__/, 比组件深一层
// 组件 import ../../../../wailsjs → 测试需要 ../../../../../wailsjs (5 层)
vi.mock("react-router-dom", () => ({
  useNavigate: () => vi.fn(),
}));

vi.mock("../../../../../wailsjs/go/app/App", () => ({
  RunList: vi.fn().mockResolvedValue([]),
  ListChatAgents: vi.fn().mockResolvedValue({ agents: [] }),
  WorkflowList: vi.fn().mockResolvedValue({ items: [] }),
  RunCreate: vi.fn(),
}));

import { useOrchRunListStore } from "../../../../stores/orch-run-list-store";
import { RunList } from "../run-list";

beforeEach(() => {
  useOrchRunListStore.getState().__reset();
  vi.clearAllMocks();
});

describe("RunList", () => {
  it("runs=[] 时展示 onboarding CTA", () => {
    render(<RunList onSelect={vi.fn()} />);
    expect(screen.getByTestId("run-onboarding-cta")).toBeInTheDocument();
  });

  it("runs 非空时列出 Run 行并点击触发 onSelect", () => {
    useOrchRunListStore.setState({
      runs: [
        {
          id: 1,
          goal: "做登录页",
          status: "running",
          leaderAgentId: 0,
          projectId: 0,
          flowId: 0,
          flowContent: "",
          rootTaskId: 0,
          createtime: Date.now(),
          updatetime: Date.now(),
        },
      ],
    });

    const onSelect = vi.fn();
    render(<RunList onSelect={onSelect} />);

    expect(screen.getByText("做登录页")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("run-row-1"));
    expect(onSelect).toHaveBeenCalledWith(1);
  });

  it("点击新建编排 Run 按钮弹出 RunNewDialog（包含 run-goal 输入框）", async () => {
    useOrchRunListStore.setState({
      runs: [
        {
          id: 1,
          goal: "做登录页",
          status: "done",
          leaderAgentId: 0,
          projectId: 0,
          flowId: 0,
          flowContent: "",
          rootTaskId: 0,
          createtime: Date.now(),
          updatetime: Date.now(),
        },
      ],
    });

    render(<RunList onSelect={vi.fn()} />);

    fireEvent.click(screen.getByTestId("run-new-button"));

    expect(await screen.findByTestId("run-goal")).toBeInTheDocument();
  });
});
