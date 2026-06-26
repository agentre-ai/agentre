import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 测试位于 orchestration/__tests__/, wailsjs 需要 5 层 ../
vi.mock("../../../../../wailsjs/go/app/App", () => ({
  RunLoad: vi.fn().mockResolvedValue({
    run: {
      id: 1,
      goal: "G",
      status: "running",
      leaderAgentId: 2,
      projectId: 0,
      flowId: 0,
      flowContent: "",
      rootTaskId: 0,
      createtime: Date.now(),
      updatetime: Date.now(),
    },
    tasks: [],
  }),
  RunPause: vi.fn(),
  RunResume: vi.fn(),
  RunStop: vi.fn(),
  RunSpeak: vi.fn(),
  LoadChatSession: vi.fn().mockResolvedValue({ messages: [] }),
}));

vi.mock("../../../../../hooks/use-chat-agents", () => ({
  useChatAgents: () => ({
    agents: [
      {
        id: 2,
        name: "Leader",
        avatarColor: "agent-1",
        avatarIcon: "",
        avatarDataUrl: "",
        sessionIds: [],
      },
      {
        id: 3,
        name: "Sub Agent",
        avatarColor: "agent-2",
        avatarIcon: "",
        avatarDataUrl: "",
        sessionIds: [],
      },
    ],
    loading: false,
    error: null,
    reload: vi.fn(),
  }),
}));

// StructureGraph stub: 渲染一个按钮，点击时调 onSelectSession(900)
// 用于二态切换测试：模拟用户点击节点选中 session
vi.mock("../structure-graph", () => ({
  StructureGraph: ({
    onSelectSession,
  }: {
    onSelectSession: (sessionId: number) => void;
  }) => (
    <div data-testid="stub-structure-graph">
      <button
        type="button"
        data-testid="stub-select-session-900"
        onClick={() => onSelectSession(900)}
      >
        select session 900
      </button>
    </div>
  ),
}));

// ConversationPanel stub: 轻量占位，渲染 data-testid 和返回按钮
vi.mock("../conversation-panel", () => ({
  ConversationPanel: ({
    sessionId,
    onBack,
  }: {
    sessionId: number;
    agentName: string;
    onBack: () => void;
  }) => (
    <div data-testid="conversation-panel">
      <span data-testid="conv-session-id">{sessionId}</span>
      <button type="button" data-testid="conversation-back" onClick={onBack}>
        back
      </button>
    </div>
  ),
}));

import type { app } from "../../../../../wailsjs/go/models";
import { useOrchRunStore } from "../../../../stores/orch-run-store";
import { OrchestrationRun } from "../index";

// 构造 RunDetailDTO 的工厂函数
function makeDetail(
  overrides: Partial<{
    runStatus: string;
    runId: number;
    tasks: app.TaskDTO[];
  }> = {},
): app.RunDetailDTO {
  const { runStatus = "running", runId = 1, tasks = [] } = overrides;
  return {
    run: {
      id: runId,
      goal: "测试目标 G",
      status: runStatus,
      leaderAgentId: 2,
      projectId: 0,
      flowId: 0,
      flowContent: "",
      rootTaskId: 0,
      createtime: Date.now(),
      updatetime: Date.now(),
    } as app.RunItemDTO,
    tasks,
  } as app.RunDetailDTO;
}

beforeEach(() => {
  useOrchRunStore.getState().__reset();
  vi.clearAllMocks();
});

describe("OrchestrationRun shell", () => {
  it("渲染根元素 orchestration-run", () => {
    render(<OrchestrationRun runId={1} title="测试运行" />);
    expect(screen.getByTestId("orchestration-run")).toBeInTheDocument();
  });

  it("store 中有 detail 时，默认渲染结构图（graph 视图）", () => {
    const detail = makeDetail({
      runId: 1,
      tasks: [
        {
          id: 1,
          runId: 1,
          agentId: 2,
          sessionId: 0,
          parentTaskId: 0,
          kind: "dispatch",
          status: "running",
          brief: "任务一",
          result: "",
          callSeq: 1,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as app.TaskDTO,
        {
          id: 2,
          runId: 1,
          agentId: 3,
          sessionId: 0,
          parentTaskId: 1,
          kind: "dispatch",
          status: "running",
          brief: "子任务",
          result: "",
          callSeq: 2,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as app.TaskDTO,
      ],
    });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    // RunHeader 渲染后应显示 run goal
    expect(screen.getByText("测试目标 G")).toBeInTheDocument();
  });

  it("detail 存在时，点击 view-feed 切换到活动 feed 视图", () => {
    const detail = makeDetail({ runId: 1 });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    // 默认是 graph 视图，点击 view-feed
    fireEvent.click(screen.getByTestId("view-feed"));

    // ActivityFeed 中有 feed-speak-input
    expect(screen.getByTestId("feed-speak-input")).toBeInTheDocument();
  });

  it("detail 不存在时渲染 loading 占位（根元素仍有 data-testid）", () => {
    // store 为空，detail 未加载
    render(<OrchestrationRun runId={99} title="加载中" />);

    const root = screen.getByTestId("orchestration-run");
    expect(root).toBeInTheDocument();
    // 没有 RunHeader 的内容
    expect(screen.queryByTestId("view-graph")).not.toBeInTheDocument();
  });

  it("选中 session 后右栏切到 ConversationPanel, 返回回到任务板", () => {
    // 注入含 task(agentId=3, sessionId=900) 的 detail
    const detail = makeDetail({
      runId: 1,
      tasks: [
        {
          id: 10,
          runId: 1,
          agentId: 3,
          sessionId: 900,
          parentTaskId: 0,
          kind: "dispatch",
          status: "running",
          brief: "子任务",
          result: "",
          callSeq: 1,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as app.TaskDTO,
      ],
    });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    // 初始状态: 右栏显示 TaskBoard（board-tab-tasks 可见），ConversationPanel 不可见
    expect(screen.getByTestId("board-tab-tasks")).toBeInTheDocument();
    expect(screen.queryByTestId("conversation-panel")).not.toBeInTheDocument();

    // 点击结构图节点（stub 触发 onSelectSession(900)）
    fireEvent.click(screen.getByTestId("stub-select-session-900"));

    // 切换后: ConversationPanel 出现，TaskBoard 消失
    expect(screen.getByTestId("conversation-panel")).toBeInTheDocument();
    expect(screen.getByTestId("conv-session-id")).toHaveTextContent("900");
    expect(screen.queryByTestId("board-tab-tasks")).not.toBeInTheDocument();

    // 点击返回按钮: 回到任务板
    fireEvent.click(screen.getByTestId("conversation-back"));
    expect(screen.queryByTestId("conversation-panel")).not.toBeInTheDocument();
    expect(screen.getByTestId("board-tab-tasks")).toBeInTheDocument();
  });
});
