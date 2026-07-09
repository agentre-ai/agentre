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
    dispatches: [],
  }),
  RunPause: vi.fn(),
  RunResume: vi.fn().mockResolvedValue(undefined),
  RunStop: vi.fn(),
  LoadChatSession: vi.fn().mockResolvedValue({ messages: [] }),
}));

// Task 4: footer 换 ChatComposer,经 useComposerSend 发送(sender-only,不订阅流)。
// mockUseComposerSend 是 spy,记录每次调用的 args,供断言 isRunning 派生是否正确。
const mockComposerSubmit = vi.fn().mockResolvedValue(null);
const mockUseComposerSend = vi.fn((_args?: unknown) => ({
  submit: mockComposerSubmit,
  sending: false,
  error: null,
  backendType: "claudecode",
  isModeSwitchable: false,
  supportsImageInput: true,
  permissionMode: { mode: "default" },
  permissionModeMeta: { order: [] },
}));
vi.mock("@/hooks/use-composer-send", () => ({
  useComposerSend: (args: unknown) => mockUseComposerSend(args),
}));

// ChatComposer stub: 渲染一个按钮，点击即触发 onSubmit({text: "to leader"})，
// 用来在不拉起真实富文本编辑器的前提下驱动 footer 提交路径。
vi.mock("../../chat", () => ({
  ChatComposer: ({
    onSubmit,
  }: {
    onSubmit?: (m: { text: string }) => void;
  }) => (
    <button
      type="button"
      data-testid="leader-composer-send"
      onClick={() => onSubmit?.({ text: "to leader" })}
    >
      send
    </button>
  ),
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
import { useSessionStatusStore } from "../../../../stores/session-status-store";
import { OrchestrationRun } from "../index";

// 构造 RunDetailDTO 的工厂函数
function makeDetail(
  overrides: Partial<{
    runStatus: string;
    runId: number;
    tasks: app.DispatchDTO[];
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
    dispatches: tasks,
  } as app.RunDetailDTO;
}

// 构造一条 leader(agentId=2)task,携带 sessionId,用于驱动 footer 渲染 ChatComposer
// 的场景(leaderSessionId 由 index.tsx 从 detail.tasks 中派生)。
function makeLeaderTask(sessionId: number): app.DispatchDTO {
  return {
    id: 5,
    runId: 1,
    agentId: 2,
    sessionId,
    parentDispatchId: 0,
    kind: "dispatch",
    status: "running",
    brief: "leader task",
    result: "",
    callSeq: 1,
    refs: "",
    createtime: Date.now(),
    updatetime: Date.now(),
  } as app.DispatchDTO;
}

beforeEach(() => {
  useOrchRunStore.getState().__reset();
  useSessionStatusStore.getState().__reset();
  vi.clearAllMocks();
});

describe("OrchestrationRun shell", () => {
  it("渲染根元素 orchestration-run", () => {
    render(<OrchestrationRun runId={1} title="测试运行" />);
    expect(screen.getByTestId("orchestration-run")).toBeInTheDocument();
  });

  it("根元素横向撑满父级主区, 避免右侧留下空白", () => {
    render(<OrchestrationRun runId={1} title="测试运行" />);
    expect(screen.getByTestId("orchestration-run")).toHaveClass(
      "min-w-0",
      "flex-1",
    );
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
          parentDispatchId: 0,
          kind: "dispatch",
          status: "running",
          brief: "任务一",
          result: "",
          callSeq: 1,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as app.DispatchDTO,
        {
          id: 2,
          runId: 1,
          agentId: 3,
          sessionId: 0,
          parentDispatchId: 1,
          kind: "dispatch",
          status: "running",
          brief: "子任务",
          result: "",
          callSeq: 2,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as app.DispatchDTO,
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

    // 切换后结构图消失，外壳的 orch-footer (speak to leader) 仍在
    expect(
      screen.queryByTestId("stub-structure-graph"),
    ).not.toBeInTheDocument();
    expect(screen.getByTestId("orch-footer")).toBeInTheDocument();
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

    // 切换后结构图消失；外壳 footer 仍在（feed 自身不再渲染独立 footer）
    expect(
      screen.queryByTestId("stub-structure-graph"),
    ).not.toBeInTheDocument();
    expect(screen.getByTestId("orch-footer")).toBeInTheDocument();
  });

  it("orch-footer 存在且渲染 ChatComposer（leader session 存在时）", () => {
    const detail = makeDetail({ runId: 1, tasks: [makeLeaderTask(500)] });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    expect(screen.getByTestId("orch-footer")).toBeInTheDocument();
    expect(screen.getByTestId("leader-composer-send")).toBeInTheDocument();
  });

  // ── Bug fix: leader footer must steer (not silently drop) while Leader is
  // mid-turn. isRunning 派生自 session-status-store,而非硬编码 false。

  it("Leader session 处于 running 态 → useComposerSend 收到 isRunning: true(忙时 steer,而非静默丢弃)", () => {
    const detail = makeDetail({ runId: 1, tasks: [makeLeaderTask(500)] });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });
    useSessionStatusStore.getState().upsert(500, {
      agentStatus: "running",
      needsAttention: false,
    });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    expect(mockUseComposerSend).toHaveBeenCalledWith(
      expect.objectContaining({ sessionId: 500, isRunning: true }),
    );
  });

  it("Leader session 处于 idle 态(或状态缺失)→ useComposerSend 收到 isRunning: false", () => {
    const detail = makeDetail({ runId: 1, tasks: [makeLeaderTask(500)] });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });
    useSessionStatusStore.getState().upsert(500, {
      agentStatus: "idle",
      needsAttention: false,
    });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    expect(mockUseComposerSend).toHaveBeenCalledWith(
      expect.objectContaining({ sessionId: 500, isRunning: false }),
    );
  });

  it("Leader session 状态未知(store 无记录)→ useComposerSend 收到 isRunning: false", () => {
    const detail = makeDetail({ runId: 1, tasks: [makeLeaderTask(500)] });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });
    // 不写入 session-status-store,模拟状态未知/尚未加载

    render(<OrchestrationRun runId={1} title="测试运行" />);

    expect(mockUseComposerSend).toHaveBeenCalledWith(
      expect.objectContaining({ sessionId: 500, isRunning: false }),
    );
  });

  // ── Regression guard: exactly ONE speak-to-Leader send control in Main ────
  // Prevents activity-feed or any view from re-introducing a second footer.

  it("graph 视图: Main 列中 leader ChatComposer 有且仅有一个", () => {
    const detail = makeDetail({ runId: 1, tasks: [makeLeaderTask(500)] });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    // Default view is graph
    expect(screen.getAllByTestId("leader-composer-send")).toHaveLength(1);
  });

  it("feed 视图: Main 列中 leader ChatComposer 有且仅有一个", () => {
    const detail = makeDetail({ runId: 1, tasks: [makeLeaderTask(500)] });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    // Switch to feed view
    fireEvent.click(screen.getByTestId("toggle-feed"));

    expect(screen.getAllByTestId("leader-composer-send")).toHaveLength(1);
  });

  it("leader session 不存在时不渲染 ChatComposer（无可发送目标）", () => {
    // detail.run.leaderAgentId=2, tasks 中无 agentId=2 的 task → no leader session
    const detail = makeDetail({ runId: 1, tasks: [] });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    expect(screen.getByTestId("orch-footer")).toBeInTheDocument();
    expect(
      screen.queryByTestId("leader-composer-send"),
    ).not.toBeInTheDocument();
    expect(screen.getByTestId("orch-footer-no-leader")).toBeInTheDocument();
  });

  it("leader session 存在时提交 footer → useComposerSend.submit 被调用, 且右栏切到 Leader ConversationPanel", async () => {
    // agentId=2 (leaderAgentId) 有 sessionId=500 的 task
    const detail = makeDetail({ runId: 1, tasks: [makeLeaderTask(500)] });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    // 初始右栏是任务板，ConversationPanel 未渲染
    expect(screen.getByTestId("board-tab-tasks")).toBeInTheDocument();
    expect(screen.queryByTestId("conversation-panel")).not.toBeInTheDocument();

    // 点击 stub ChatComposer 的发送按钮（触发 onSubmit({text:"to leader"})）
    fireEvent.click(screen.getByTestId("leader-composer-send"));

    expect(mockComposerSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ text: "to leader" }),
    );

    // submit 的 Promise resolve 后，index.tsx 应 setSelectedSessionId(leaderSessionId)
    // 切到 Leader 的 ConversationPanel（stub 渲染 conv-session-id）
    await waitFor(() => {
      expect(screen.getByTestId("conversation-panel")).toBeInTheDocument();
    });
    expect(screen.getByTestId("conv-session-id")).toHaveTextContent("500");
  });

  // ── End Task 1 RED tests ─────────────────────────────────────────────────

  // ── Task 9 RED tests: banner relocation + restyle ────────────────────────

  it("Task9: completed phase → graph-completed-banner 在 orch-main 中, 在 orch-content 之前", () => {
    const detail = makeDetail({
      runId: 1,
      runStatus: "done",
      tasks: [
        {
          id: 1,
          runId: 1,
          agentId: 2,
          sessionId: 0,
          parentDispatchId: 0,
          kind: "dispatch",
          status: "done",
          brief: "t1",
          result: "",
          callSeq: 1,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.DispatchDTO,
        {
          id: 2,
          runId: 1,
          agentId: 3,
          sessionId: 0,
          parentDispatchId: 1,
          kind: "dispatch",
          status: "done",
          brief: "t2",
          result: "",
          callSeq: 2,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.DispatchDTO,
      ],
    });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    const main = screen.getByTestId("orch-main");
    const banner = screen.getByTestId("graph-completed-banner");
    const content = screen.getByTestId("orch-content");

    // banner must be inside orch-main
    expect(main).toContainElement(banner);
    // banner appears before orch-content in DOM
    expect(
      banner.compareDocumentPosition(content) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("Task9: paused phase → graph-paused-banner 在 orch-main 中, 含 resume 动作按钮", () => {
    const detail = makeDetail({
      runId: 1,
      runStatus: "paused",
      tasks: [
        {
          id: 1,
          runId: 1,
          agentId: 2,
          sessionId: 0,
          parentDispatchId: 0,
          kind: "dispatch",
          status: "running",
          brief: "t1",
          result: "",
          callSeq: 1,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.DispatchDTO,
        {
          id: 2,
          runId: 1,
          agentId: 3,
          sessionId: 0,
          parentDispatchId: 1,
          kind: "dispatch",
          status: "running",
          brief: "t2",
          result: "",
          callSeq: 2,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.DispatchDTO,
      ],
    });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    const main = screen.getByTestId("orch-main");
    const banner = screen.getByTestId("graph-paused-banner");
    expect(main).toContainElement(banner);

    // The resume action button must be present in the banner
    const resumeBtn = banner.querySelector("[data-testid='banner-resume-btn']");
    expect(resumeBtn).toBeInTheDocument();
  });

  it("Task9: paused banner resume 按钮点击 → 调用 RunResume(runId)", async () => {
    const RunResumeMock = (await import("../../../../../wailsjs/go/app/App"))
      .RunResume as ReturnType<typeof vi.fn>;

    const detail = makeDetail({
      runId: 1,
      runStatus: "paused",
      tasks: [
        {
          id: 1,
          runId: 1,
          agentId: 2,
          sessionId: 0,
          parentDispatchId: 0,
          kind: "dispatch",
          status: "running",
          brief: "t1",
          result: "",
          callSeq: 1,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.DispatchDTO,
        {
          id: 2,
          runId: 1,
          agentId: 3,
          sessionId: 0,
          parentDispatchId: 1,
          kind: "dispatch",
          status: "running",
          brief: "t2",
          result: "",
          callSeq: 2,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.DispatchDTO,
      ],
    });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    const resumeBtn = screen.getByTestId("banner-resume-btn");
    fireEvent.click(resumeBtn);

    await waitFor(() => {
      expect(RunResumeMock).toHaveBeenCalledWith(1);
    });
  });

  it("Task9: stopped phase → graph-stopped-banner 在 orch-main 中, 无动作按钮", () => {
    const detail = makeDetail({
      runId: 1,
      runStatus: "stopped",
      tasks: [
        {
          id: 1,
          runId: 1,
          agentId: 2,
          sessionId: 0,
          parentDispatchId: 0,
          kind: "dispatch",
          status: "done",
          brief: "t1",
          result: "",
          callSeq: 1,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.DispatchDTO,
        {
          id: 2,
          runId: 1,
          agentId: 3,
          sessionId: 0,
          parentDispatchId: 1,
          kind: "dispatch",
          status: "done",
          brief: "t2",
          result: "",
          callSeq: 2,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.DispatchDTO,
      ],
    });
    useOrchRunStore.setState({ details: new Map([[1, detail]]) });

    render(<OrchestrationRun runId={1} title="测试运行" />);

    const main = screen.getByTestId("orch-main");
    const banner = screen.getByTestId("graph-stopped-banner");
    expect(main).toContainElement(banner);

    // No action button for stopped
    expect(banner.querySelector("button")).not.toBeInTheDocument();
  });

  it("Task9: deadlock → graph-deadlock-banner 在 orch-main 中, 含 intervene 按钮", () => {
    const runId = 5;
    const sessionId = 88;
    useOrchRunStore.setState({
      deadlocks: new Map([[runId, [sessionId]]]),
    });
    const detail = makeDetail({
      runId,
      runStatus: "running",
      tasks: [
        {
          id: 1,
          runId,
          agentId: 2,
          sessionId: 0,
          parentDispatchId: 0,
          kind: "dispatch",
          status: "running",
          brief: "t1",
          result: "",
          callSeq: 1,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.DispatchDTO,
        {
          id: 2,
          runId,
          agentId: 3,
          sessionId,
          parentDispatchId: 1,
          kind: "dispatch",
          status: "awaiting-user",
          brief: "t2",
          result: "",
          callSeq: 2,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.DispatchDTO,
      ],
    });
    useOrchRunStore.setState({ details: new Map([[runId, detail]]) });

    render(<OrchestrationRun runId={runId} title="测试运行" />);

    const main = screen.getByTestId("orch-main");
    const banner = screen.getByTestId("graph-deadlock-banner");
    expect(main).toContainElement(banner);

    const interveneBtn = screen.getByTestId("banner-intervene-btn");
    expect(interveneBtn).toBeInTheDocument();
  });

  it("Task9: deadlock intervene 按钮点击 → 切到 Leader ConversationPanel", () => {
    const runId = 6;
    const sessionId = 77;
    useOrchRunStore.setState({
      deadlocks: new Map([[runId, [sessionId]]]),
    });
    // Include a leader task with a valid sessionId so there is a Leader session to switch to
    const detail = makeDetail({
      runId,
      runStatus: "running",
      tasks: [
        // leader agent (agentId=2) task with sessionId → leaderSessionId resolves
        {
          id: 1,
          runId,
          agentId: 2,
          sessionId: 500,
          parentDispatchId: 0,
          kind: "dispatch",
          status: "running",
          brief: "leader",
          result: "",
          callSeq: 1,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.DispatchDTO,
        {
          id: 2,
          runId,
          agentId: 3,
          sessionId,
          parentDispatchId: 1,
          kind: "dispatch",
          status: "awaiting-user",
          brief: "t2",
          result: "",
          callSeq: 2,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as import("../../../../../wailsjs/go/models").app.DispatchDTO,
      ],
    });
    useOrchRunStore.setState({ details: new Map([[runId, detail]]) });

    render(<OrchestrationRun runId={runId} title="测试运行" />);

    const interveneBtn = screen.getByTestId("banner-intervene-btn");
    fireEvent.click(interveneBtn);

    // Right rail should switch to the Leader's ConversationPanel (sessionId=500)
    expect(screen.getByTestId("conversation-panel")).toBeInTheDocument();
    expect(screen.getByTestId("conv-session-id")).toHaveTextContent("500");
  });

  // ── End Task 9 RED tests ─────────────────────────────────────────────────

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
          parentDispatchId: 0,
          kind: "dispatch",
          status: "running",
          brief: "子任务",
          result: "",
          callSeq: 1,
          refs: "",
          createtime: Date.now(),
          updatetime: Date.now(),
        } as app.DispatchDTO,
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
