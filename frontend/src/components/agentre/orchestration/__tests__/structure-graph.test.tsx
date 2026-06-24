import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 注意: 测试位于 orchestration/__tests__/, 距 wailsjs 五层
// wailsjs/go/app/App → 被 chat-agents-store 间接 import，需要 mock
vi.mock("../../../../../wailsjs/go/app/App", () => ({
  ListChatAgents: vi.fn().mockResolvedValue({ agents: [] }),
  RunLoad: vi.fn().mockResolvedValue({ run: undefined, tasks: [] }),
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
  vi.clearAllMocks();
});

describe("StructureGraph", () => {
  it("lifecycle=completed (run.status=done) 时渲染 graph-completed-banner", () => {
    // 终态 done → lifecycle 返回 completed，应有 banner
    const detail = makeDetail({
      runStatus: "done",
      tasks: [makeTask(1, 2, "done"), makeTask(2, 3, "done", 1)],
    });

    render(<StructureGraph detail={detail} onSelectNode={vi.fn()} />);

    expect(screen.getByTestId("graph-completed-banner")).toBeInTheDocument();
  });

  it("store deadlocks 有 [runId → [sessionId]] 且 task 含该 sessionId → graph-deadlock-banner 可见", () => {
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

    render(<StructureGraph detail={detail} onSelectNode={vi.fn()} />);

    expect(screen.getByTestId("graph-deadlock-banner")).toBeInTheDocument();
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

    render(<StructureGraph detail={detail} onSelectNode={vi.fn()} />);

    expect(screen.getByTestId("graph-empty")).toBeInTheDocument();
  });

  it("点击节点 node-{agentId} 调 onSelectNode(agentId)", () => {
    const onSelectNode = vi.fn();
    // 两个节点：Leader(2) + Sub(3)
    const detail = makeDetail({
      runStatus: "running",
      tasks: [makeTask(1, 2, "running"), makeTask(2, 3, "running", 1)],
    });

    render(<StructureGraph detail={detail} onSelectNode={onSelectNode} />);

    // 点击子 agent 节点
    fireEvent.click(screen.getByTestId("node-3"));

    expect(onSelectNode).toHaveBeenCalledWith(3);
  });
});
