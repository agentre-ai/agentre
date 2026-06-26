import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 测试位于 orchestration/__tests__/, 距 wailsjs 五层
vi.mock("../../../../../wailsjs/go/app/App", () => ({
  RunSpeak: vi.fn().mockResolvedValue(undefined),
  ListChatAgents: vi.fn().mockResolvedValue({ agents: [] }),
  RunLoad: vi.fn().mockResolvedValue({ run: undefined, tasks: [] }),
}));

// mock useChatAgents，返回已知 agent 列表
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

import * as AppBindings from "../../../../../wailsjs/go/app/App";
import type { app } from "../../../../../wailsjs/go/models";
import { useOrchRunStore } from "../../../../stores/orch-run-store";
import { ActivityFeed } from "../activity-feed";

// 构造 RunDetailDTO 工厂
function makeDetail(
  overrides: Partial<{
    runId: number;
    runStatus: string;
    tasks: app.TaskDTO[];
    leaderAgentId: number;
    rootTaskId: number;
  }> = {},
): app.RunDetailDTO {
  const {
    runId = 1,
    runStatus = "running",
    tasks = [],
    leaderAgentId = 2,
    rootTaskId = 0,
  } = overrides;
  return {
    run: {
      id: runId,
      goal: "测试目标",
      status: runStatus,
      leaderAgentId,
      projectId: 0,
      flowId: 0,
      flowContent: "",
      rootTaskId,
      createtime: 1000,
      updatetime: 2000,
    } as app.RunItemDTO,
    tasks,
  } as app.RunDetailDTO;
}

function makeTask(
  overrides: Partial<app.TaskDTO> & { id: number },
): app.TaskDTO {
  return {
    runId: 1,
    agentId: 2,
    sessionId: 0,
    parentTaskId: 0,
    kind: "dispatch",
    status: "running",
    brief: "",
    result: "",
    callSeq: 0,
    refs: "",
    createtime: 1000,
    updatetime: 2000,
    ...overrides,
  } as app.TaskDTO;
}

beforeEach(() => {
  useOrchRunStore.getState().__reset();
  vi.clearAllMocks();
});

describe("ActivityFeed", () => {
  it("渲染 dispatch 任务的 brief 和 done 任务的 result", () => {
    // dispatch 任务：parentTaskId=1 触发 dispatch 条目
    // done 任务：status=done + result 触发 report 条目
    const tasks = [
      makeTask({
        id: 2,
        agentId: 3,
        parentTaskId: 1,
        status: "done",
        brief: "做X",
        result: "已完成X",
        createtime: 100,
        updatetime: 200,
      }),
    ];
    const detail = makeDetail({ tasks });

    render(<ActivityFeed detail={detail} />);

    // dispatch 条目文本
    expect(screen.getByText("做X")).toBeInTheDocument();
    // report 条目文本
    expect(screen.getByText("已完成X")).toBeInTheDocument();
  });

  it("在 feed-speak-input 输入文字后点击 feed-speak-send，调用 RunSpeak(根会话ID, msg)", async () => {
    // 根 task 的 sessionId=500，RunSpeak 应收到 500 而非 run.id(100)
    const rootTask = makeTask({
      id: 1,
      parentTaskId: 0,
      sessionId: 500,
      agentId: 2,
    });
    const detail = makeDetail({ runId: 100, rootTaskId: 1, tasks: [rootTask] });

    render(<ActivityFeed detail={detail} />);

    const input = screen.getByTestId("feed-speak-input");
    const sendBtn = screen.getByTestId("feed-speak-send");

    fireEvent.change(input, { target: { value: "对Leader的话" } });
    fireEvent.click(sendBtn);

    await waitFor(() => {
      expect(AppBindings.RunSpeak).toHaveBeenCalledWith(500, "对Leader的话");
    });
  });

  it("有 awaiting-user 任务时显示 feed-blocking-bar", () => {
    const tasks = [makeTask({ id: 1, agentId: 2, status: "awaiting-user" })];
    const detail = makeDetail({ tasks });

    render(<ActivityFeed detail={detail} />);

    expect(screen.getByTestId("feed-blocking-bar")).toBeInTheDocument();
  });

  it("渲染 ask/reply 条目: testid + 动态文本可见", () => {
    const detail = makeDetail({ runId: 1 });

    // 注入 askLog 到 store
    useOrchRunStore.setState((s) => {
      const log = new Map(s.askLog);
      log.set(1, [
        {
          kind: "ask",
          askId: "k",
          agentId: 2,
          targetAgentId: 3,
          text: "鉴权?",
          ts: 10,
        },
        { kind: "reply", askId: "k", agentId: 3, text: "ok", ts: 20 },
      ]);
      return { askLog: log };
    });

    render(<ActivityFeed detail={detail} />);

    // ask 条目: testid = feed-ask-k, 包含问题文本
    const askItem = screen.getByTestId("feed-ask-k");
    expect(askItem).toBeInTheDocument();
    expect(askItem).toHaveTextContent("鉴权?");

    // reply 条目: testid = feed-reply-k, 包含回答文本
    const replyItem = screen.getByTestId("feed-reply-k");
    expect(replyItem).toBeInTheDocument();
    expect(replyItem).toHaveTextContent("ok");
  });
});
