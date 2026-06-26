import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

// StructureGraph stub: 渲染两个按钮，分别触发 onSelectSession(900) 和 onSelectSession(0)
// 用于二态切换测试：session=900 打开面板，session=0（Leader/未启动节点哨兵值）保留看板
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
      <button
        type="button"
        data-testid="stub-select-session-0"
        onClick={() => onSelectSession(0)}
      >
        select session 0
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

  it("detail 存在时，点击 toggle-feed 切换到活动 feed 视图", () => {
    const detail = makeDetail({ runId: 1 });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    // 默认是 graph 视图，点击 ToggleBar 中的 toggle-feed（view-feed 已从 RunHeader 移除）
    fireEvent.click(screen.getByTestId("toggle-feed"));

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

  it("sessionId=0 (Leader/未启动节点哨兵值) 不打开 ConversationPanel，看板保持可见", () => {
    const detail = makeDetail({ runId: 1 });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    // 初始看板可见
    expect(screen.getByTestId("board-tab-tasks")).toBeInTheDocument();
    expect(screen.queryByTestId("conversation-panel")).not.toBeInTheDocument();

    // 点击 sessionId=0 的节点（Leader 根任务或未启动子任务的哨兵值）
    fireEvent.click(screen.getByTestId("stub-select-session-0"));

    // 看板依然可见，不应打开 ConversationPanel
    expect(screen.getByTestId("board-tab-tasks")).toBeInTheDocument();
    expect(screen.queryByTestId("conversation-panel")).not.toBeInTheDocument();
  });

  // ── Task 1 RED tests: 3-pane shell ──────────────────────────────────────

  it("detail 存在时渲染 orch-main, orch-toggle, orch-content 容器", () => {
    const detail = makeDetail({ runId: 1 });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    expect(screen.getByTestId("orch-main")).toBeInTheDocument();
    expect(screen.getByTestId("orch-toggle")).toBeInTheDocument();
    expect(screen.getByTestId("orch-content")).toBeInTheDocument();
  });

  it("orch-toggle 中 toggle-graph 和 toggle-feed 按钮存在", () => {
    const detail = makeDetail({ runId: 1 });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    expect(screen.getByTestId("toggle-graph")).toBeInTheDocument();
    expect(screen.getByTestId("toggle-feed")).toBeInTheDocument();
  });

  it("view 默认 graph: orch-content 显示结构图, 点击 toggle-feed 切换到 feed", () => {
    const detail = makeDetail({ runId: 1 });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    // 默认结构图可见
    expect(screen.getByTestId("stub-structure-graph")).toBeInTheDocument();

    // 点击 toggle-feed
    fireEvent.click(screen.getByTestId("toggle-feed"));

    // 切换后应显示 ActivityFeed (feed-speak-input 是 ActivityFeed 内的 testid)
    expect(screen.getByTestId("feed-speak-input")).toBeInTheDocument();
  });

  it("orch-footer 存在且包含 orch-speak-leader-send 按钮", () => {
    const detail = makeDetail({ runId: 1 });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    expect(screen.getByTestId("orch-footer")).toBeInTheDocument();
    expect(screen.getByTestId("orch-speak-leader-send")).toBeInTheDocument();
  });

  it("leader session 不存在时 orch-speak-leader-send 按钮 disabled", () => {
    // detail.run.leaderAgentId=2, tasks 中无 agentId=2 的 task → no leader session
    const detail = makeDetail({ runId: 1, tasks: [] });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    expect(screen.getByTestId("orch-speak-leader-send")).toBeDisabled();
  });

  it("leader session 存在时 orch-speak-leader-send 可用, 发送后调 RunSpeak 并清空输入", async () => {
    const RunSpeakMock = (await import("../../../../../wailsjs/go/app/App"))
      .RunSpeak as ReturnType<typeof vi.fn>;

    // agentId=2 (leaderAgentId) 有 sessionId=500 的 task
    const detail = makeDetail({
      runId: 1,
      tasks: [
        {
          id: 5,
          runId: 1,
          agentId: 2,
          sessionId: 500,
          parentTaskId: 0,
          kind: "dispatch",
          status: "running",
          brief: "leader task",
          result: "",
          callSeq: 1,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.TaskDTO,
      ],
    });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    const sendBtn = screen.getByTestId("orch-speak-leader-send");
    expect(sendBtn).not.toBeDisabled();

    // 找到 footer 内的 input 并输入消息
    const footer = screen.getByTestId("orch-footer");
    const input = footer.querySelector("input") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "调整优先级" } });

    // 点发送
    fireEvent.click(sendBtn);

    await waitFor(() => {
      expect(RunSpeakMock).toHaveBeenCalledWith(500, "调整优先级");
    });

    // 输入已清空
    expect(input.value).toBe("");
  });

  it("RunFlowBlueprint 不再渲染在 Main 列中(已移除)", () => {
    const detail = makeDetail({ runId: 1 });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    // run-flow-blueprint 是 RunFlowBlueprint 的 data-testid
    // flowId=0 时本来就 null,这里只验证 orch-main 不含它
    const main = screen.getByTestId("orch-main");
    expect(main).not.toContainElement(
      document.querySelector('[data-testid="run-flow-blueprint"]'),
    );
  });

  // ── End Task 1 RED tests ─────────────────────────────────────────────────

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
