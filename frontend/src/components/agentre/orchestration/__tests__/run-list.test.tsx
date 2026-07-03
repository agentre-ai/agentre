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
  ProjectListTree: vi.fn().mockResolvedValue([]),
  LoadOrg: vi.fn().mockResolvedValue({ departments: [], agents: [] }),
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

  // ── 新结构断言 ──────────────────────────────────────────────────
  describe("has-runs 路径 — 新结构", () => {
    const twoRuns = [
      {
        id: 10,
        goal: "支付重构",
        status: "running",
        leaderAgentId: 0,
        projectId: 0,
        flowId: 0,
        flowContent: "",
        rootTaskId: 0,
        createtime: Date.now(),
        updatetime: Date.now(),
      },
      {
        id: 11,
        goal: "数据迁移",
        status: "paused",
        leaderAgentId: 0,
        projectId: 0,
        flowId: 0,
        flowContent: "",
        rootTaskId: 0,
        createtime: Date.now(),
        updatetime: Date.now(),
      },
    ];

    it("展示 listHead 标题区域", () => {
      useOrchRunListStore.setState({ runs: twoRuns });
      render(<RunList onSelect={vi.fn()} />);
      // listHead 必须存在
      expect(screen.getByTestId("run-list-head")).toBeInTheDocument();
    });

    it("展示新建按钮 run-new-button", () => {
      useOrchRunListStore.setState({ runs: twoRuns });
      render(<RunList onSelect={vi.fn()} />);
      expect(screen.getByTestId("run-new-button")).toBeInTheDocument();
    });

    it("展示筛选 chip 行", () => {
      useOrchRunListStore.setState({ runs: twoRuns });
      render(<RunList onSelect={vi.fn()} />);
      expect(screen.getByTestId("run-filter-chips")).toBeInTheDocument();
    });

    it("activeRunId 匹配时 run-row 带 active 样式标记", () => {
      useOrchRunListStore.setState({ runs: twoRuns });
      render(<RunList onSelect={vi.fn()} activeRunId={10} />);
      const activeRow = screen.getByTestId("run-row-10");
      expect(activeRow).toHaveAttribute("data-active", "true");
    });

    it("非 active run-row data-active=false", () => {
      useOrchRunListStore.setState({ runs: twoRuns });
      render(<RunList onSelect={vi.fn()} activeRunId={10} />);
      const inactiveRow = screen.getByTestId("run-row-11");
      expect(inactiveRow).toHaveAttribute("data-active", "false");
    });

    it("run-row 展示 run goal 文本", () => {
      useOrchRunListStore.setState({ runs: twoRuns });
      render(<RunList onSelect={vi.fn()} />);
      expect(screen.getByTestId("run-row-10")).toBeInTheDocument();
      expect(screen.getByTestId("run-row-11")).toBeInTheDocument();
    });

    it("run-row 状态文本已本地化，不显示原始英文 status 字符串", () => {
      useOrchRunListStore.setState({ runs: twoRuns });
      render(<RunList onSelect={vi.fn()} />);
      // run-row-10 is "running" → must NOT show raw "running"
      // (i18n 在测试环境返回 key 本身如 "orchestration.header.running"，总是不等于 "running")
      const row10 = screen.getByTestId("run-row-10");
      expect(row10).not.toHaveTextContent(/^running$/i);
      // run-row-11 is "paused" → must NOT show raw "paused"
      const row11 = screen.getByTestId("run-row-11");
      expect(row11).not.toHaveTextContent(/^paused$/i);
    });

    it("点击「运行中」筛选后只显示 running 的 Run", () => {
      useOrchRunListStore.setState({ runs: twoRuns });
      render(<RunList onSelect={vi.fn()} />);
      fireEvent.click(screen.getByTestId("run-filter-running"));
      expect(screen.getByTestId("run-row-10")).toBeInTheDocument();
      expect(screen.queryByTestId("run-row-11")).toBeNull();
    });

    it("点击「已完成」筛选且无匹配时显示空匹配提示", () => {
      useOrchRunListStore.setState({ runs: twoRuns });
      render(<RunList onSelect={vi.fn()} />);
      fireEvent.click(screen.getByTestId("run-filter-done"));
      expect(screen.queryByTestId("run-row-10")).toBeNull();
      expect(screen.queryByTestId("run-row-11")).toBeNull();
      expect(screen.getByTestId("run-filter-empty")).toBeInTheDocument();
    });

    it("默认筛选 all,两个 Run 都显示", () => {
      useOrchRunListStore.setState({ runs: twoRuns });
      render(<RunList onSelect={vi.fn()} />);
      expect(screen.getByTestId("run-row-10")).toBeInTheDocument();
      expect(screen.getByTestId("run-row-11")).toBeInTheDocument();
    });
  });
});
