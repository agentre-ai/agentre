import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ToggleBar } from "../toggle-bar";

// i18n mock
vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, unknown>) => {
      const map: Record<string, string> = {
        "orchestration.header.viewGraph": "结构图",
        "orchestration.header.viewFeed": "活动流",
        "orchestration.toggle.tasks": "任务",
        "orchestration.toggle.depth": "深度",
        "orchestration.toggle.subagents": "子代理",
      };
      if (key === "orchestration.toggle.agents" && opts) {
        return `${opts.agentCount} agent · ${opts.subCount} sub`;
      }
      return map[key] ?? key;
    },
    i18n: { language: "zh-CN" },
  }),
}));

const defaultStats = {
  done: 5,
  total: 12,
  depth: 3,
  agentCount: 4,
  subCount: 3,
  subagentCount: 11,
};

describe("ToggleBar", () => {
  it("渲染 orch-toggle 根容器", () => {
    render(<ToggleBar view="graph" onView={vi.fn()} stats={defaultStats} />);
    expect(screen.getByTestId("orch-toggle")).toBeInTheDocument();
  });

  it("渲染 toggle-graph 和 toggle-feed 两个分段", () => {
    render(<ToggleBar view="graph" onView={vi.fn()} stats={defaultStats} />);
    expect(screen.getByTestId("toggle-graph")).toBeInTheDocument();
    expect(screen.getByTestId("toggle-feed")).toBeInTheDocument();
  });

  it("view='graph' 时 toggle-graph 呈激活样式(有 bg-card class)", () => {
    render(<ToggleBar view="graph" onView={vi.fn()} stats={defaultStats} />);
    const graphSeg = screen.getByTestId("toggle-graph");
    expect(graphSeg.className).toMatch(/bg-card/);
  });

  it("view='feed' 时 toggle-feed 呈激活样式(有 bg-card class)", () => {
    render(<ToggleBar view="feed" onView={vi.fn()} stats={defaultStats} />);
    const feedSeg = screen.getByTestId("toggle-feed");
    expect(feedSeg.className).toMatch(/bg-card/);
  });

  it("点击 toggle-feed 调用 onView('feed')", () => {
    const onView = vi.fn();
    render(<ToggleBar view="graph" onView={onView} stats={defaultStats} />);
    fireEvent.click(screen.getByTestId("toggle-feed"));
    expect(onView).toHaveBeenCalledWith("feed");
  });

  it("点击 toggle-graph 调用 onView('graph')", () => {
    const onView = vi.fn();
    render(<ToggleBar view="feed" onView={onView} stats={defaultStats} />);
    fireEvent.click(screen.getByTestId("toggle-graph"));
    expect(onView).toHaveBeenCalledWith("graph");
  });

  it("渲染 4 个 stat chip testid", () => {
    render(<ToggleBar view="graph" onView={vi.fn()} stats={defaultStats} />);
    expect(screen.getByTestId("toggle-stat-tasks")).toBeInTheDocument();
    expect(screen.getByTestId("toggle-stat-depth")).toBeInTheDocument();
    expect(screen.getByTestId("toggle-stat-agents")).toBeInTheDocument();
    expect(screen.getByTestId("toggle-stat-subagents")).toBeInTheDocument();
  });

  it("toggle-stat-tasks 显示 done/total", () => {
    render(<ToggleBar view="graph" onView={vi.fn()} stats={defaultStats} />);
    expect(screen.getByTestId("toggle-stat-tasks")).toHaveTextContent("5/12");
  });

  it("toggle-stat-depth 显示深度值", () => {
    render(<ToggleBar view="graph" onView={vi.fn()} stats={defaultStats} />);
    expect(screen.getByTestId("toggle-stat-depth")).toHaveTextContent("3");
  });

  it("toggle-stat-agents 显示 agentCount 和 subCount", () => {
    render(<ToggleBar view="graph" onView={vi.fn()} stats={defaultStats} />);
    const chip = screen.getByTestId("toggle-stat-agents");
    expect(chip).toHaveTextContent("4");
    expect(chip).toHaveTextContent("3");
  });

  it("toggle-stat-subagents 显示 subagentCount", () => {
    render(<ToggleBar view="graph" onView={vi.fn()} stats={defaultStats} />);
    expect(screen.getByTestId("toggle-stat-subagents")).toHaveTextContent("11");
  });

  it("传入不同 stats 时正确显示数值", () => {
    render(
      <ToggleBar
        view="graph"
        onView={vi.fn()}
        stats={{
          done: 0,
          total: 7,
          depth: 1,
          agentCount: 2,
          subCount: 1,
          subagentCount: 0,
        }}
      />,
    );
    expect(screen.getByTestId("toggle-stat-tasks")).toHaveTextContent("0/7");
    expect(screen.getByTestId("toggle-stat-depth")).toHaveTextContent("1");
    expect(screen.getByTestId("toggle-stat-subagents")).toHaveTextContent("0");
  });
});
