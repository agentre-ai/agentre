import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TaskProgressBar } from "../task-progress-bar";

import type { TaskProgress } from "../types";

const empty: TaskProgress = { tasks: [] };

const claude3: TaskProgress = {
  tasks: [
    { id: "t_1", description: "A", status: "completed" },
    { id: "t_2", description: "B", status: "running" },
    { id: "t_3", description: "C", status: "queued" },
  ],
};

describe("TaskProgressBar", () => {
  it("renders nothing when there are no tasks", () => {
    const { container } = render(<TaskProgressBar progress={empty} />);
    expect(container.firstChild).toBeNull();
  });

  it("shows ratio and current running task in collapsed mode", () => {
    render(<TaskProgressBar progress={claude3} />);
    expect(screen.getByText("1")).toBeInTheDocument();
    expect(screen.getByText("/")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
    expect(screen.getByText(/B/)).toBeInTheDocument();
  });

  it("toggles expanded state on header click and lists every task", () => {
    render(<TaskProgressBar progress={claude3} />);
    const header = screen.getByRole("button", { expanded: false });
    fireEvent.click(header);
    expect(screen.getByRole("button", { expanded: true })).toBeInTheDocument();
    expect(screen.getByText("A")).toBeInTheDocument();
    expect(screen.getByText("B")).toBeInTheDocument();
    expect(screen.getByText("C")).toBeInTheDocument();
    expect(screen.getByText("DONE")).toBeInTheDocument();
    expect(screen.getByText("RUNNING")).toBeInTheDocument();
    expect(screen.getByText("QUEUED")).toBeInTheDocument();
  });

  it("hides the 'current task' row when expanded", () => {
    render(<TaskProgressBar progress={claude3} />);
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    expect(screen.queryByText("当前")).not.toBeInTheDocument();
  });

  it("auto-collapses to recap label 2s after all tasks complete", () => {
    vi.useFakeTimers();
    const allDone: TaskProgress = {
      tasks: [
        { id: "t_1", description: "A", status: "completed" },
        { id: "t_2", description: "B", status: "completed" },
      ],
    };
    try {
      const { rerender } = render(<TaskProgressBar progress={claude3} />);
      fireEvent.click(screen.getByRole("button", { expanded: false }));
      expect(
        screen.getByRole("button", { expanded: true }),
      ).toBeInTheDocument();
      rerender(<TaskProgressBar progress={allDone} />);
      act(() => {
        vi.advanceTimersByTime(2000);
      });
      expect(
        screen.getByRole("button", { expanded: false }),
      ).toBeInTheDocument();
      expect(screen.getByText(/全部完成/)).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });
});
