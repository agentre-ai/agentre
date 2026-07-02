import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  RunList: vi.fn(),
  RunLoad: vi.fn(),
  ListChatAgents: vi.fn(),
}));
vi.mock("../../../../../wailsjs/go/app/App", () => appMocks);

const mockNavigate = vi.fn();
vi.mock("react-router-dom", () => ({ useNavigate: () => mockNavigate }));

import { useOrchRunListStore } from "../../../../stores/orch-run-list-store";
import { useOrchRunStore } from "../../../../stores/orch-run-store";
import { OrchestrationOverview } from "../orchestration-overview";

const now = Date.now();
const runningRun = {
  id: 1,
  goal: "支付重构",
  status: "running",
  leaderAgentId: 2,
  projectId: 0,
  flowId: 0,
  flowContent: "",
  rootTaskId: 0,
  createtime: now - 3_600_000,
  updatetime: now - 60_000,
};
const doneRun = {
  id: 2,
  goal: "登录页",
  status: "done",
  leaderAgentId: 2,
  projectId: 0,
  flowId: 0,
  flowContent: "",
  rootTaskId: 0,
  createtime: now - 7_200_000,
  updatetime: now - 120_000,
};

beforeEach(() => {
  useOrchRunListStore.getState().__reset();
  useOrchRunStore.getState().__reset();
  vi.clearAllMocks();
  appMocks.RunList.mockResolvedValue([runningRun, doneRun]);
  appMocks.ListChatAgents.mockResolvedValue({
    agents: [
      {
        id: 2,
        name: "架构师",
        avatarColor: "agent-1",
        defaultPermissionMode: "default",
      },
    ],
  });
  appMocks.RunLoad.mockResolvedValue({
    run: runningRun,
    tasks: [
      {
        id: 10,
        runId: 1,
        agentId: 2,
        sessionId: 0,
        parentTaskId: 0,
        kind: "orch",
        status: "done",
        brief: "",
        result: "",
        callSeq: 0,
        refs: "",
        createtime: now,
        updatetime: now,
      },
      {
        id: 11,
        runId: 1,
        agentId: 2,
        sessionId: 0,
        parentTaskId: 0,
        kind: "orch",
        status: "running",
        brief: "",
        result: "",
        callSeq: 1,
        refs: "",
        createtime: now,
        updatetime: now,
      },
    ],
  });
});

describe("OrchestrationOverview", () => {
  it("统计卡展示 running / 本周完成 计数", async () => {
    render(<OrchestrationOverview />);
    expect(
      await screen.findByTestId("overview-stat-running"),
    ).toHaveTextContent("1");
    expect(screen.getByTestId("overview-stat-doneWeek")).toHaveTextContent("1");
  });

  it("进行中卡片展示进度 done/total(来自懒加载 detail)", async () => {
    render(<OrchestrationOverview />);
    expect(
      await screen.findByTestId("overview-inprogress-progress-1"),
    ).toHaveTextContent("1/2");
    const bar = within(
      await screen.findByTestId("overview-inprogress-card-1"),
    ).getByRole("progressbar");
    expect(bar).toHaveAttribute("aria-valuenow", "50");
    const fill = bar.firstElementChild as HTMLElement;
    expect(fill).toHaveStyle({ width: "50%" });
  });

  it("点击进行中卡片 navigate 到该 Run", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<OrchestrationOverview />);
    await user.click(await screen.findByTestId("overview-inprogress-card-1"));
    expect(mockNavigate).toHaveBeenCalledWith("/orchestration/1");
  });

  it("最近完成列出 done 的 Run", async () => {
    render(<OrchestrationOverview />);
    expect(
      await screen.findByTestId("overview-recent-row-2"),
    ).toBeInTheDocument();
  });

  it("runs=[] 时渲染空态(仍带 overview testid)", async () => {
    appMocks.RunList.mockResolvedValue([]);
    render(<OrchestrationOverview />);
    expect(
      await screen.findByTestId("orchestration-overview"),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("overview-stat-running")).toBeNull();
  });
});
