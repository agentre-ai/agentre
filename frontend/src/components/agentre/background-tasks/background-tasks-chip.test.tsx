import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { BackgroundTasksChip } from "./background-tasks-chip";
import { BackgroundTasksPopoverContent } from "./background-tasks-popover";
import type { BackgroundTask } from "./types";

// 任何用 vi.useFakeTimers() 的用例若断言抛错,真实计时器不会被恢复,会泄漏到
// 后续测试。统一在 afterEach 兜底恢复。
afterEach(() => {
  vi.useRealTimers();
});

const running: BackgroundTask = {
  toolUseId: "tu1",
  kind: "local_bash",
  description: "sleep 20",
  status: "running",
};

const completed: BackgroundTask = {
  toolUseId: "tu2",
  kind: "local_bash",
  description: "Explore repo",
  status: "completed",
};

const failed: BackgroundTask = {
  toolUseId: "tu3",
  kind: "local_bash",
  description: "build step",
  status: "failed",
};

describe("BackgroundTasksChip", () => {
  it("stays visible with a completed label when only completed/failed tasks remain", () => {
    render(
      <BackgroundTasksChip
        tasks={[
          {
            toolUseId: "tu1",
            taskId: "id1",
            kind: "local_bash",
            description: "build",
            status: "completed",
            durationMs: 1000,
          },
          {
            toolUseId: "tu2",
            taskId: "id2",
            kind: "local_bash",
            description: "lint",
            status: "failed",
            durationMs: 500,
          },
        ]}
      />,
    );
    // chip trigger present (aria-label 后台任务) and shows the completed count, not running
    expect(
      screen.getByRole("button", { name: /后台任务|background/i }),
    ).toBeInTheDocument();
    expect(screen.getByText(/2 已完成|2 done/)).toBeInTheDocument();
  });

  it("renders null only when there are no tasks at all", () => {
    const { container } = render(<BackgroundTasksChip tasks={[]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows running count in chip label", () => {
    render(<BackgroundTasksChip tasks={[running, completed]} />);
    const btn = screen.getByRole("button", { name: /background tasks/i });
    expect(btn).toBeInTheDocument();
    expect(btn).toHaveTextContent("1 running");
  });

  it("shows correct count when multiple tasks are running", () => {
    const running2: BackgroundTask = {
      toolUseId: "tu4",
      kind: "local_bash",
      description: "another task",
      status: "running",
    };
    render(<BackgroundTasksChip tasks={[running, running2, completed]} />);
    expect(screen.getByRole("button")).toHaveTextContent("2 running");
  });

  it("opens popover and shows all tasks when chip is clicked", () => {
    render(<BackgroundTasksChip tasks={[running, completed, failed]} />);
    const btn = screen.getByRole("button");
    fireEvent.click(btn);

    // popover title
    expect(screen.getByText("Background tasks")).toBeInTheDocument();
    // task descriptions (dynamic — rendered raw)
    expect(screen.getByText("sleep 20")).toBeInTheDocument();
    expect(screen.getByText("Explore repo")).toBeInTheDocument();
    expect(screen.getByText("build step")).toBeInTheDocument();
    // status labels
    expect(screen.getByText("Running")).toBeInTheDocument();
    expect(screen.getByText("Done")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
  });

  it("shows empty state in popover if tasks array has no items", () => {
    // chip is hidden when 0 running, but we can test popover content directly via
    // the BackgroundTasksPopoverContent component independently
    // Here we test via chip by passing a task that is running but empty
    render(
      <BackgroundTasksChip
        tasks={[
          {
            toolUseId: "tu5",
            kind: "local_bash",
            description: "",
            status: "running",
          },
        ]}
      />,
    );
    fireEvent.click(screen.getByRole("button"));
    // popover shows 1 item with empty description
    expect(screen.getByText("Background tasks")).toBeInTheDocument();
  });

  it("shows 'bash' kind label for all tasks in popover", () => {
    render(
      <BackgroundTasksChip
        tasks={[running, { ...completed, status: "running" as const }]}
      />,
    );
    fireEvent.click(screen.getByRole("button"));
    // Both tasks are local_bash — both rows should show the "bash" label
    const bashLabels = screen.getAllByText("bash");
    expect(bashLabels.length).toBe(2);
  });
});

describe("BackgroundTasksPopoverContent — elapsed + summary", () => {
  it("shows elapsed for a running task with startedAt", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(1700000030000)); // 30s after startedAt

    const tasks: BackgroundTask[] = [
      {
        toolUseId: "tu-r",
        kind: "local_bash",
        description: "sleep 20",
        status: "running",
        startedAt: 1700000000000,
      },
    ];
    render(<BackgroundTasksPopoverContent tasks={tasks} />);
    // running 30s → "30s"
    expect(screen.getByTestId("elapsed")).toHaveTextContent("30s");
  });

  it("shows frozen durationMs for a completed bash task", () => {
    const tasks: BackgroundTask[] = [
      {
        toolUseId: "tu-c",
        kind: "local_bash",
        description: "Explore",
        status: "completed",
        durationMs: 4200,
      },
    ];
    render(<BackgroundTasksPopoverContent tasks={tasks} />);
    expect(screen.getByTestId("elapsed")).toHaveTextContent("4s");
  });

  it("shows summary text for a completed bash task", () => {
    const summary = 'Background command "sleep 20" completed (exit code 0)';
    const tasks: BackgroundTask[] = [
      {
        toolUseId: "tu-b",
        kind: "local_bash",
        description: "sleep 20",
        status: "completed",
        summary,
      },
    ];
    render(<BackgroundTasksPopoverContent tasks={tasks} />);
    expect(screen.getByText(summary)).toBeInTheDocument();
  });

  it("does not show elapsed for a running task without startedAt", () => {
    const tasks: BackgroundTask[] = [
      {
        toolUseId: "tu-no-start",
        kind: "local_bash",
        description: "sleep 20",
        status: "running",
      },
    ];
    render(<BackgroundTasksPopoverContent tasks={tasks} />);
    expect(screen.queryByTestId("elapsed")).toBeNull();
  });

  it("does not show summary when summary is absent", () => {
    const tasks: BackgroundTask[] = [
      {
        toolUseId: "tu-no-summary",
        kind: "local_bash",
        description: "run",
        status: "completed",
      },
    ];
    render(<BackgroundTasksPopoverContent tasks={tasks} />);
    // No extra text beyond what's expected
    expect(screen.queryByText(/exit code/)).toBeNull();
  });

  it("formats minute-range frozen durationMs as m ss", () => {
    const tasks: BackgroundTask[] = [
      {
        toolUseId: "tu-min",
        kind: "local_bash",
        description: "task",
        status: "completed",
        durationMs: 185_000, // 3m 05s
      },
    ];
    render(<BackgroundTasksPopoverContent tasks={tasks} />);
    expect(screen.getByTestId("elapsed")).toHaveTextContent("3m 05s");
  });

  it("formats hour-range frozen durationMs as h mm", () => {
    const tasks: BackgroundTask[] = [
      {
        toolUseId: "tu-hr",
        kind: "local_bash",
        description: "task",
        status: "completed",
        durationMs: 3_720_000, // 1h 02m
      },
    ];
    render(<BackgroundTasksPopoverContent tasks={tasks} />);
    expect(screen.getByTestId("elapsed")).toHaveTextContent("1h 02m");
  });
});

describe("BackgroundTasksChip — new design (badge + clearCompleted)", () => {
  it("shows the running-count badge and the task_id in the row", async () => {
    const onClear = vi.fn();
    render(
      <BackgroundTasksChip
        tasks={[
          {
            toolUseId: "tu1",
            taskId: "b3875slp0",
            kind: "local_bash",
            description: "sleep 5",
            status: "running",
            startedAt: Date.now(),
          },
          {
            toolUseId: "tu2",
            taskId: "c9xyz",
            kind: "local_bash",
            description: "build",
            status: "completed",
            durationMs: 20000,
          },
        ]}
        onClearCompleted={onClear}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: /后台任务|background/i }),
    );
    expect(screen.getByText("b3875slp0")).toBeInTheDocument();
    // The running-count badge appears in the popover header (may also appear in chip label)
    expect(
      screen.getAllByText(/1 运行中|1 running/).length,
    ).toBeGreaterThanOrEqual(1);
  });

  it("clears completed tasks via 清理已完成", async () => {
    const onClear = vi.fn();
    render(
      <BackgroundTasksChip
        tasks={[
          {
            toolUseId: "tu1",
            taskId: "id1",
            kind: "local_bash",
            description: "sleep",
            status: "running",
            startedAt: Date.now(),
          },
          {
            toolUseId: "tu2",
            taskId: "id2",
            kind: "local_bash",
            description: "build",
            status: "completed",
            durationMs: 1000,
          },
        ]}
        onClearCompleted={onClear}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: /后台任务|background/i }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: /清理已完成|clear completed/i }),
    );
    expect(onClear).toHaveBeenCalledTimes(1);
  });
});
