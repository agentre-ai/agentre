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
          selectedAgentId={null}
          onSelectTask={vi.fn()}
        />,
      );

      expect(screen.getByTestId("board-task-1")).toBeInTheDocument();
      expect(screen.getByTestId("board-task-2")).toBeInTheDocument();
    });

    it("点击任务行调用 onSelectTask(agentId)", () => {
      const onSelectTask = vi.fn();
      const tasks = [makeTask(1, 2), makeTask(2, 3, { parentTaskId: 1 })];
      const detail = makeDetail(tasks);

      render(
        <TaskBoard
          detail={detail}
          selectedAgentId={null}
          onSelectTask={onSelectTask}
        />,
      );

      fireEvent.click(screen.getByTestId("board-task-2"));

      expect(onSelectTask).toHaveBeenCalledWith(3);
    });

    it("selectedAgentId 非 null 时渲染 board-drilldown 钻入面板", () => {
      const tasks = [makeTask(1, 2)];
      const detail = makeDetail(tasks);

      render(
        <TaskBoard
          detail={detail}
          selectedAgentId={2}
          onSelectTask={vi.fn()}
        />,
      );

      expect(screen.getByTestId("board-drilldown")).toBeInTheDocument();
    });

    it("子任务（parentTaskId !== 0）渲染时有缩进 class", () => {
      const tasks = [makeTask(1, 2), makeTask(2, 3, { parentTaskId: 1 })];
      const detail = makeDetail(tasks);

      render(
        <TaskBoard
          detail={detail}
          selectedAgentId={null}
          onSelectTask={vi.fn()}
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
          selectedAgentId={null}
          onSelectTask={vi.fn()}
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

  describe("产出物 tab", () => {
    it("点击 board-tab-outputs 切换到产出物视图，渲染 board-outputs", () => {
      const detail = makeDetail();

      render(
        <TaskBoard
          detail={detail}
          selectedAgentId={null}
          onSelectTask={vi.fn()}
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
          selectedAgentId={null}
          onSelectTask={vi.fn()}
        />,
      );

      fireEvent.click(screen.getByTestId("board-tab-outputs"));

      // 只有第一个任务的 refs 非空
      const outputsArea = screen.getByTestId("board-outputs");
      expect(outputsArea).toHaveTextContent('{"url":"https://example.com"}');
    });
  });
});
