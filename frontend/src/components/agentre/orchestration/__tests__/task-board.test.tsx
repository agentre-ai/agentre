import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 测试位于 orchestration/__tests__/, wailsjs 需要 5 层 ../
vi.mock("../../../../../wailsjs/go/app/App", () => ({
  RunLoad: vi.fn().mockResolvedValue({
    run: { id: 1, goal: "G", status: "running", leaderAgentId: 2 },
    tasks: [],
  }),
  RunPause: vi.fn(),
  RunResume: vi.fn(),
  RunStop: vi.fn(),
  RunSpeak: vi.fn(),
  LoadChatSession: vi.fn().mockResolvedValue({
    messages: [
      {
        blocks: [
          {
            type: "tool_use",
            toolUseId: "s1",
            subagent: {
              kind: "local_agent",
              subagentType: "用例生成器",
              status: "completed",
            },
          },
        ],
      },
    ],
  }),
}));

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
import { TaskBoard } from "../task-board";

// 构造 RunDetailDTO
function makeDetail(tasks: app.TaskDTO[] = []): app.RunDetailDTO {
  return {
    run: {
      id: 1,
      goal: "测试目标",
      status: "running",
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

function makeTask(
  id: number,
  agentId: number,
  overrides: Partial<app.TaskDTO> = {},
): app.TaskDTO {
  return {
    id,
    runId: 1,
    agentId,
    sessionId: 0,
    parentTaskId: 0,
    kind: "dispatch",
    status: "running",
    brief: `Task ${id}`,
    result: "",
    callSeq: id,
    refs: "",
    createtime: Date.now(),
    updatetime: Date.now(),
    ...overrides,
  } as app.TaskDTO;
}

beforeEach(() => {
  useOrchRunStore.getState().__reset();
  useOrchSubagentsStore.getState().__reset();
  vi.clearAllMocks();
});

describe("TaskBoard", () => {
  describe("任务看板 tab", () => {
    it("为每个任务渲染 board-task-${id} 行", () => {
      const tasks = [makeTask(1, 2), makeTask(2, 3, { parentTaskId: 1 })];
      const detail = makeDetail(tasks);

      render(
        <TaskBoard
          detail={detail}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );

      expect(screen.getByTestId("board-task-1")).toBeInTheDocument();
      expect(screen.getByTestId("board-task-2")).toBeInTheDocument();
    });

    it("点击任务行调用 onSelectSession(该 task sessionId)", () => {
      const onSelectSession = vi.fn();
      const tasks = [
        makeTask(1, 2, { sessionId: 11 }),
        makeTask(2, 3, { parentTaskId: 1, sessionId: 22 }),
      ];
      const detail = makeDetail(tasks);

      render(
        <TaskBoard
          detail={detail}
          selectedSessionId={null}
          onSelectSession={onSelectSession}
        />,
      );

      fireEvent.click(screen.getByTestId("board-task-2"));

      expect(onSelectSession).toHaveBeenCalledWith(22);
    });

    it("子任务（parentTaskId !== 0）渲染时有缩进 class", () => {
      const tasks = [makeTask(1, 2), makeTask(2, 3, { parentTaskId: 1 })];
      const detail = makeDetail(tasks);

      render(
        <TaskBoard
          detail={detail}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );

      // 子任务行有 pl-6 类（缩进）
      const childRow = screen.getByTestId("board-task-2");
      expect(childRow.className).toMatch(/pl-6/);
    });

    it("点击 board-tab-tasks 切换回任务看板", () => {
      const detail = makeDetail([makeTask(1, 2)]);

      render(
        <TaskBoard
          detail={detail}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );

      // 先切到产出物
      fireEvent.click(screen.getByTestId("board-tab-outputs"));
      expect(screen.getByTestId("board-outputs")).toBeInTheDocument();

      // 再切回任务看板
      fireEvent.click(screen.getByTestId("board-tab-tasks"));
      expect(screen.getByTestId("board-task-1")).toBeInTheDocument();
    });
  });

  describe("头部计数徽标", () => {
    it("头部 board-progress 显示 done/total(完成数/任务数)", () => {
      const tasks = [
        makeTask(1, 2, { status: "done" }),
        makeTask(2, 3, { status: "done", parentTaskId: 1 }),
        makeTask(3, 3, { status: "running", parentTaskId: 1 }),
      ];
      render(
        <TaskBoard
          detail={makeDetail(tasks)}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );
      expect(screen.getByTestId("board-progress")).toHaveTextContent("2");
      expect(screen.getByTestId("board-progress")).toHaveTextContent("3");
    });
  });

  describe("agent 分组", () => {
    it("同 agent 多次调用 → 分组头 + 每次调用一条 per-call 行", () => {
      const tasks = [
        makeTask(1, 2, { status: "running" }), // Leader 单调用
        makeTask(2, 3, {
          status: "running",
          parentTaskId: 1,
          callSeq: 1,
          sessionId: 501,
        }),
        makeTask(3, 3, {
          status: "done",
          parentTaskId: 1,
          callSeq: 2,
          sessionId: 502,
        }),
      ];
      render(
        <TaskBoard
          detail={makeDetail(tasks)}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );
      // agent 3 多调用 → 分组头 + 两条 per-call 行
      expect(screen.getByTestId("board-agent-3")).toBeInTheDocument();
      expect(screen.getByTestId("board-task-2")).toBeInTheDocument();
      expect(screen.getByTestId("board-task-3")).toBeInTheDocument();
      // agent 2 单调用 → 无分组头
      expect(screen.queryByTestId("board-agent-2")).not.toBeInTheDocument();
      expect(screen.getByTestId("board-task-1")).toBeInTheDocument();
    });

    it("有 CLI 子代理的 call 行下挂折叠 board-subagents-{taskId}, 点开展开只读子行", async () => {
      const tasks = [
        makeTask(1, 2, { status: "running" }),
        makeTask(2, 3, { status: "running", parentTaskId: 1, sessionId: 501 }),
      ];
      render(
        <TaskBoard
          detail={makeDetail(tasks)}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );
      // 懒加载完成后折叠行出现(+1 子代理)
      const toggle = await screen.findByTestId("board-subagents-2");
      expect(toggle).toHaveTextContent("1");
      // 默认折叠:子行不在
      expect(
        screen.queryByTestId("board-subagent-2-0"),
      ).not.toBeInTheDocument();
      // 点开 → 子行出现
      fireEvent.click(toggle);
      expect(screen.getByTestId("board-subagent-2-0")).toBeInTheDocument();
    });
  });

  describe("头部标题与格式", () => {
    it("渲染 board-head-title 元素", () => {
      render(
        <TaskBoard
          detail={makeDetail([])}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );
      expect(screen.getByTestId("board-head-title")).toBeInTheDocument();
      // text comes from i18n key orchestration.board.title (locale-dependent)
      expect(
        screen.getByTestId("board-head-title").textContent?.length,
      ).toBeGreaterThan(0);
    });

    it("board-progress 显示 done / total 完成 格式", () => {
      const tasks = [
        makeTask(1, 2, { status: "done" }),
        makeTask(2, 3, { status: "running" }),
      ];
      render(
        <TaskBoard
          detail={makeDetail(tasks)}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );
      const prog = screen.getByTestId("board-progress");
      expect(prog).toHaveTextContent("1");
      expect(prog).toHaveTextContent("2");
    });
  });

  describe("任务行状态图标", () => {
    it("done 任务行渲染 status-icon-done testid", () => {
      const tasks = [makeTask(1, 2, { status: "done" })];
      render(
        <TaskBoard
          detail={makeDetail(tasks)}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );
      expect(screen.getByTestId("board-task-1-status")).toBeInTheDocument();
    });

    it("running 任务行渲染 status icon testid", () => {
      const tasks = [makeTask(1, 2, { status: "running" })];
      render(
        <TaskBoard
          detail={makeDetail(tasks)}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );
      expect(screen.getByTestId("board-task-1-status")).toBeInTheDocument();
    });

    it("任务行显示 #id (任务序号)", () => {
      const tasks = [makeTask(5, 2, { callSeq: 5 })];
      render(
        <TaskBoard
          detail={makeDetail(tasks)}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );
      const row = screen.getByTestId("board-task-5");
      expect(row).toHaveTextContent("#5");
    });

    it("任务行显示 brief 标题", () => {
      const tasks = [makeTask(1, 2, { brief: "实现网关适配层" })];
      render(
        <TaskBoard
          detail={makeDetail(tasks)}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );
      expect(screen.getByTestId("board-task-1")).toHaveTextContent(
        "实现网关适配层",
      );
    });
  });

  describe("产出物 tab", () => {
    it("点击 board-tab-outputs 切换到产出物视图，渲染 board-outputs", () => {
      const detail = makeDetail();

      render(
        <TaskBoard
          detail={detail}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );

      fireEvent.click(screen.getByTestId("board-tab-outputs"));

      expect(screen.getByTestId("board-outputs")).toBeInTheDocument();
    });

    it("有 refs 的任务在产出物 tab 中显示", () => {
      const tasks = [
        makeTask(1, 2, { refs: '{"url":"https://example.com"}' }),
        makeTask(2, 3, { refs: "" }),
      ];
      const detail = makeDetail(tasks);

      render(
        <TaskBoard
          detail={detail}
          selectedSessionId={null}
          onSelectSession={vi.fn()}
        />,
      );

      fireEvent.click(screen.getByTestId("board-tab-outputs"));

      // 只有第一个任务的 refs 非空
      const outputsArea = screen.getByTestId("board-outputs");
      expect(outputsArea).toHaveTextContent('{"url":"https://example.com"}');
    });
  });
});
