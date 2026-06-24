import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 注意: 测试位于 orchestration/__tests__/, 比组件深一层
// 组件 import ../../../../wailsjs → 测试需要 ../../../../../wailsjs (5 层)
vi.mock("../../../../../wailsjs/go/app/App", () => ({
  RunPause: vi.fn().mockResolvedValue(undefined),
  RunResume: vi.fn().mockResolvedValue(undefined),
  RunStop: vi.fn().mockResolvedValue(undefined),
}));

import * as AppBindings from "../../../../../wailsjs/go/app/App";
import type { app } from "../../../../../wailsjs/go/models";
import { RunHeader } from "../run-header";

// 构造 RunDetailDTO 的工厂函数
function makeDetail(
  overrides: Partial<{
    runStatus: string;
    runId: number;
    tasks: app.TaskDTO[];
  }> = {},
): app.RunDetailDTO {
  const { runStatus = "running", runId = 100, tasks = [] } = overrides;
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

function makeTask(status: string, id = 1): app.TaskDTO {
  return {
    id,
    runId: 1,
    agentId: 2,
    sessionId: 0,
    parentTaskId: 0,
    kind: "dispatch",
    status,
    brief: "",
    result: "",
    callSeq: 0,
    refs: "",
    createtime: Date.now(),
    updatetime: Date.now(),
  } as app.TaskDTO;
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("RunHeader", () => {
  it("status=running 时点击 run-pause 调 RunPause(runId)", async () => {
    const detail = makeDetail({ runStatus: "running", runId: 100 });
    render(<RunHeader detail={detail} view="graph" onView={vi.fn()} />);

    fireEvent.click(screen.getByTestId("run-pause"));

    expect(AppBindings.RunPause).toHaveBeenCalledWith(100);
  });

  it("点击 view-feed 分段控件调 onView('feed')", () => {
    const onView = vi.fn();
    const detail = makeDetail();
    render(<RunHeader detail={detail} view="graph" onView={onView} />);

    fireEvent.click(screen.getByTestId("view-feed"));

    expect(onView).toHaveBeenCalledWith("feed");
  });

  it("tasks 中含 awaiting-user 时等待你计数显示 1", () => {
    const tasks = [makeTask("awaiting-user", 1)];
    const detail = makeDetail({ tasks });
    render(<RunHeader detail={detail} view="graph" onView={vi.fn()} />);

    // 查找等待你计数 chip 中的数字 1
    const chip = screen.getByTestId("count-waiting-you");
    expect(chip).toHaveTextContent("1");
  });
});
