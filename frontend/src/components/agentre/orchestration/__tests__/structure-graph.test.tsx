import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 注意: 测试位于 orchestration/__tests__/, 距 wailsjs 五层
// wailsjs/go/app/App → 被 chat-agents-store 间接 import，需要 mock
vi.mock("../../../../../wailsjs/go/app/App", () => ({
  ListChatAgents: vi.fn().mockResolvedValue({ agents: [] }),
  RunLoad: vi.fn().mockResolvedValue({ run: undefined, tasks: [] }),
  LoadChatSession: vi.fn().mockResolvedValue({
    messages: [
      {
        blocks: [
          {
            type: "tool_use",
            toolUseId: "s",
            subagent: { kind: "local_agent", status: "running" },
          },
        ],
      },
    ],
  }),
}));

// 从 hooks 层 mock useChatAgents，返回确定的 agent 列表
vi.mock("../../../../../hooks/use-chat-agents", () => ({
  useChatAgents: () => ({
    agents: [
      {
        id: 2,
        name: "Leader Agent",
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

import type { app } from "../../../../../wailsjs/go/models";
import { useOrchRunStore } from "../../../../stores/orch-run-store";
import { useOrchSubagentsStore } from "../../../../stores/orch-subagents-store";
import { StructureGraph } from "../structure-graph";

// 工厂: 构造 RunDetailDTO
function makeDetail(
  overrides: Partial<{
    runStatus: string;
    runId: number;
    tasks: app.TaskDTO[];
    leaderAgentId: number;
  }> = {},
): app.RunDetailDTO {
  const {
    runStatus = "running",
    runId = 1,
    tasks = [],
    leaderAgentId = 2,
  } = overrides;
  return {
    run: {
      id: runId,
      goal: "Test Goal",
      status: runStatus,
      leaderAgentId,
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

function makeTask(
  id: number,
  agentId: number,
  status: string,
  parentTaskId = 0,
  sessionId = 0,
): app.TaskDTO {
  return {
    id,
    runId: 1,
    agentId,
    sessionId,
    parentTaskId,
    kind: "dispatch",
    status,
    brief: `Task ${id}`,
    result: "",
    callSeq: 0,
    refs: "",
    createtime: Date.now(),
    updatetime: Date.now(),
  } as app.TaskDTO;
}

beforeEach(() => {
  useOrchRunStore.getState().__reset();
  useOrchSubagentsStore.getState().__reset();
  vi.clearAllMocks();
});

describe("StructureGraph", () => {
  it("lifecycle=completed (run.status=done) 时节点渲染(banner 已移至 index.tsx 主列)", () => {
    // 终态 done → lifecycle 返回 completed，banner 已由 index.tsx 负责渲染
    // StructureGraph 应渲染节点(不再有 completed banner)
    const detail = makeDetail({
      runStatus: "done",
      tasks: [makeTask(1, 2, "done"), makeTask(2, 3, "done", 1)],
    });

    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);

    // banner relocated to index.tsx — not here
    expect(
      screen.queryByTestId("graph-completed-banner"),
    ).not.toBeInTheDocument();
    // nodes are still rendered (phase !== empty since tasks.length > 1)
    expect(screen.getByTestId("node-2")).toBeInTheDocument();
    expect(screen.getByTestId("node-3")).toBeInTheDocument();
  });

  it("store deadlocks 有 [runId → [sessionId]] 且 task 含该 sessionId → 死锁节点有红环, banner 已移至 index.tsx", () => {
    const runId = 42;
    const sessionId = 99;
    // 注入死锁信息到 store
    useOrchRunStore.setState({
      deadlocks: new Map([[runId, [sessionId]]]),
    });

    const detail = makeDetail({
      runId,
      runStatus: "running",
      tasks: [
        // Leader task
        makeTask(1, 2, "running", 0, 0),
        // 子 agent task，sessionId 命中死锁
        makeTask(2, 3, "awaiting-user", 1, sessionId),
      ],
    });

    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);

    // banner relocated to index.tsx — not here
    expect(
      screen.queryByTestId("graph-deadlock-banner"),
    ).not.toBeInTheDocument();
    // 死锁高亮必须落在正确的节点:cycle sessionId(99)→该任务 agentId(3),
    // Leader(agentId 2)不在环上,不应被红环。验证 sessionId→agentId 映射真发生。
    expect(screen.getByTestId("node-3").className).toMatch(/ring-destructive/);
    expect(screen.getByTestId("node-2").className).not.toMatch(
      /ring-destructive/,
    );
  });

  it("empty 态（仅 Leader 根任务）渲染 graph-empty 引导文案", () => {
    // 只有 1 个任务 → lifecycle === empty
    const detail = makeDetail({
      runStatus: "running",
      tasks: [makeTask(1, 2, "running")],
    });

    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);

    expect(screen.getByTestId("graph-empty")).toBeInTheDocument();
  });

  it("点击单调用节点 → onSelectSession(该 task sessionId)", () => {
    const onSelectSession = vi.fn();
    const detail = makeDetail({
      runStatus: "running",
      tasks: [makeTask(1, 2, "running"), makeTask(2, 3, "running", 1, 555)],
    });

    render(
      <StructureGraph detail={detail} onSelectSession={onSelectSession} />,
    );

    // 点击子 agent 节点
    fireEvent.click(screen.getByTestId("node-3"));

    expect(onSelectSession).toHaveBeenCalledWith(555);
  });

  it("非顶层 subagent 多次调用 → node 显示 ×N 合并徽标, 不逐条列 call 子行", () => {
    const detail = makeDetail({
      runStatus: "running",
      tasks: [
        makeTask(1, 2, "running", 0, 0), // Leader
        makeTask(2, 3, "running", 1, 0), // 后端(顶层)
        makeTask(3, 4, "running", 2, 410), // 验签助手 #1, 父=后端(3)
        makeTask(4, 4, "done", 2, 411), // 验签助手 #2
      ],
    });

    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);

    // 合并徽标可见、文案含 ×2
    expect(screen.getByTestId("node-4-multi")).toHaveTextContent("×2");
    // 合并节点不列 per-call 子行(那是任务板的事)
    expect(screen.queryByTestId("node-4-call-3")).not.toBeInTheDocument();
    expect(screen.queryByTestId("node-4-call-4")).not.toBeInTheDocument();
  });

  it("顶层 agent 多次对话 → 分组容器, 头部 N 会话 + 每次调用一条子行", () => {
    const detail = makeDetail({
      runStatus: "running",
      tasks: [
        makeTask(1, 2, "running", 0, 0), // Leader
        makeTask(2, 3, "running", 1, 501), // 前端 会话#1, 父=Leader 根(1)
        makeTask(3, 3, "done", 1, 502), // 前端 会话#2
      ],
    });

    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);

    // 头部「2 会话」分组标记(i18n 未 mock → t() 回显 key, 用 count 插值断言)
    expect(screen.getByTestId("node-3")).toHaveTextContent("2");
    // 两条 per-call 子行
    expect(screen.getByTestId("node-3-call-2")).toBeInTheDocument();
    expect(screen.getByTestId("node-3-call-3")).toBeInTheDocument();
    // 顶层分组不显示 ×N 合并徽标(分组 ≠ 合并)
    expect(screen.queryByTestId("node-3-multi")).not.toBeInTheDocument();

    // Fix 1 guard: 分组头部的中间点分隔符必须带空格 "· N sessions",
    // 断言 textContent 含 "· 2" (middot + space + count),区分意外的 "·2"。
    // toHaveTextContent 内部对空格 normalize 使得连续空格折叠为单格,但 "· 2"
    // 与 "·2" 仍有区别: 前者在 substring 中存在 middot+space。
    const headerSpan = screen
      .getByTestId("node-3")
      .querySelector("div > span > span");
    expect(headerSpan?.textContent).toMatch(/·\s2/);
  });

  it("activeAsks 含某 asker → 其节点挂 提问·等待回复 徽标", () => {
    useOrchRunStore.setState({
      activeAsks: new Map([
        [1, [{ askId: "k", askerAgentId: 3, targetAgentId: 2 }]],
      ]),
    });
    const detail = makeDetail({
      runId: 1,
      runStatus: "running",
      tasks: [makeTask(1, 2, "running"), makeTask(2, 3, "running", 1)],
    });
    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);
    expect(screen.getByTestId("node-3-asking")).toBeInTheDocument();
  });

  it("activeAsks 为空 → 不显示 asking 徽标", () => {
    const detail = makeDetail({
      runId: 1,
      runStatus: "running",
      tasks: [makeTask(1, 2, "running"), makeTask(2, 3, "running", 1)],
    });
    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);
    expect(screen.queryByTestId("node-3-asking")).not.toBeInTheDocument();
  });

  // ── Task 9: banners relocated — StructureGraph must NOT render them ────────

  it("Task9: completed phase → StructureGraph 不再渲染 graph-completed-banner", () => {
    const detail = makeDetail({
      runStatus: "done",
      tasks: [makeTask(1, 2, "done"), makeTask(2, 3, "done", 1)],
    });
    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);
    expect(
      screen.queryByTestId("graph-completed-banner"),
    ).not.toBeInTheDocument();
  });

  it("Task9: paused phase → StructureGraph 不再渲染 graph-paused-banner", () => {
    const detail = makeDetail({
      runStatus: "paused",
      tasks: [makeTask(1, 2, "running"), makeTask(2, 3, "running", 1)],
    });
    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);
    expect(screen.queryByTestId("graph-paused-banner")).not.toBeInTheDocument();
  });

  it("Task9: stopped phase → StructureGraph 不再渲染 graph-stopped-banner", () => {
    const detail = makeDetail({
      runStatus: "stopped",
      tasks: [makeTask(1, 2, "done"), makeTask(2, 3, "done", 1)],
    });
    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);
    expect(
      screen.queryByTestId("graph-stopped-banner"),
    ).not.toBeInTheDocument();
  });

  it("Task9: deadlock → StructureGraph 不再渲染 graph-deadlock-banner", () => {
    const runId = 42;
    const sessionId = 99;
    useOrchRunStore.setState({
      deadlocks: new Map([[runId, [sessionId]]]),
    });
    const detail = makeDetail({
      runId,
      runStatus: "running",
      tasks: [
        makeTask(1, 2, "running", 0, 0),
        makeTask(2, 3, "awaiting-user", 1, sessionId),
      ],
    });
    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);
    expect(
      screen.queryByTestId("graph-deadlock-banner"),
    ).not.toBeInTheDocument();
  });

  it("Task9: node-asking 徽标仍由 StructureGraph 渲染(未移除)", () => {
    useOrchRunStore.setState({
      activeAsks: new Map([
        [1, [{ askId: "k", askerAgentId: 3, targetAgentId: 2 }]],
      ]),
    });
    const detail = makeDetail({
      runId: 1,
      runStatus: "running",
      tasks: [makeTask(1, 2, "running"), makeTask(2, 3, "running", 1)],
    });
    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);
    // asking badge still rendered by StructureGraph
    expect(screen.getByTestId("node-3-asking")).toBeInTheDocument();
  });

  // ── End Task 9 ────────────────────────────────────────────────────────────

  it("agent 的 task session 有 CLI 子代理 → 节点挂 +N 子代理 徽标", async () => {
    const detail = makeDetail({
      runStatus: "running",
      tasks: [
        makeTask(1, 2, "running", 0, 0),
        makeTask(2, 3, "running", 1, 700),
      ],
    });
    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);
    const badge = await screen.findByTestId("node-3-subagents");
    expect(badge).toHaveTextContent("1");
  });

  // ── Task 5: top-down layout ────────────────────────────────────────────────

  it("top-down: leader 节点在 children 容器之前(DOM 顺序)", () => {
    const detail = makeDetail({
      runStatus: "running",
      tasks: [
        makeTask(1, 2, "running", 0, 0), // Leader
        makeTask(2, 3, "running", 1, 500), // child
      ],
    });
    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);

    const leaderNode = screen.getByTestId("node-2");
    const childNode = screen.getByTestId("node-3");

    // Leader must appear before child in DOM order (top-down)
    expect(
      leaderNode.compareDocumentPosition(childNode) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("top-down: 有子节点时渲染 graph-bus 横向连接线", () => {
    const detail = makeDetail({
      runStatus: "running",
      tasks: [
        makeTask(1, 2, "running", 0, 0), // Leader
        makeTask(2, 3, "running", 1, 500), // child
      ],
    });
    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);
    expect(screen.getByTestId("graph-bus")).toBeInTheDocument();
  });

  it("top-down: 仅 leader 无子节点时不渲染 graph-bus", () => {
    // lifecycle=empty 时(只有1个task)显示 graph-empty,无 bus
    const detail = makeDetail({
      runStatus: "running",
      tasks: [makeTask(1, 2, "running")],
    });
    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);
    expect(screen.queryByTestId("graph-bus")).not.toBeInTheDocument();
  });

  it("top-down: leader-only 结构图(2任务但只有leader节点)→ 无 graph-bus", () => {
    // 2 tasks, both belong to the same leader agent → single node, no children
    const detail = makeDetail({
      runStatus: "running",
      tasks: [
        makeTask(1, 2, "running", 0, 0),
        makeTask(2, 2, "done", 0, 0), // same agent, still leader-only graph
      ],
    });
    render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);
    // No children nodes → no bus
    expect(screen.queryByTestId("graph-bus")).not.toBeInTheDocument();
  });

  it("top-down: 已有 testid 和 onSelectSession 行为均保留", () => {
    const onSelectSession = vi.fn();
    const detail = makeDetail({
      runStatus: "running",
      tasks: [
        makeTask(1, 2, "running", 0, 0), // Leader
        makeTask(2, 3, "running", 1, 600), // child
      ],
    });
    render(
      <StructureGraph detail={detail} onSelectSession={onSelectSession} />,
    );

    // Both node testids present
    expect(screen.getByTestId("node-2")).toBeInTheDocument();
    expect(screen.getByTestId("node-3")).toBeInTheDocument();

    // Click child node → onSelectSession called
    fireEvent.click(screen.getByTestId("node-3"));
    expect(onSelectSession).toHaveBeenCalledWith(600);
  });

  // ── FIX 1: cycle guard regression ─────────────────────────────────────────
  // Construct a cycle: agent 2 (leader) dispatches to agent 3,
  // agent 3 dispatches back to agent 2. buildGraph produces edges 2→3 and 3→2.
  // Without the visited-set guard, SubNodeColumn would recurse infinitely → stack overflow.
  // With the guard, the back-edge is suppressed and both nodes render exactly once.
  it("SubNodeColumn 有环路守卫：A→B→A 死锁环不导致爆栈，节点正常渲染", () => {
    const detail = makeDetail({
      runStatus: "running",
      tasks: [
        makeTask(1, 2, "running", 0, 0), // Leader (agent 2), parentTaskId=0 → root
        makeTask(2, 3, "running", 1, 600), // agent 3, parent=task1(agent 2) → edge 2→3
        makeTask(3, 2, "running", 2, 0), // agent 2 again, parent=task2(agent 3) → edge 3→2 (CYCLE)
      ],
    });

    // Without the cycle guard this would throw "Maximum call stack size exceeded"
    expect(() =>
      render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />),
    ).not.toThrow();

    // Both nodes must appear exactly once (guard stops re-entry)
    expect(screen.getByTestId("node-2")).toBeInTheDocument();
    expect(screen.getByTestId("node-3")).toBeInTheDocument();
  });
});
