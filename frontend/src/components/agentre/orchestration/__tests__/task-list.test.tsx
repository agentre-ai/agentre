import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { app } from "../../../../../wailsjs/go/models";
import { TaskList } from "../task-list";

function makeTask(
  id: number,
  overrides: Partial<app.TaskItemDTO> = {},
): app.TaskItemDTO {
  return {
    id,
    runId: 1,
    seq: id,
    text: `Task ${id}`,
    status: "pending",
    assigneeAgentId: 0,
    createtime: Date.now(),
    updatetime: Date.now(),
    ...overrides,
  } as app.TaskItemDTO;
}

function makeDetail(tasks: app.TaskItemDTO[] = []): app.RunDetailDTO {
  return {
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
    } as app.RunItemDTO,
    dispatches: [] as app.DispatchDTO[],
    tasks,
  } as app.RunDetailDTO;
}

describe("TaskList", () => {
  it("renders one row per task, showing its text", () => {
    const tasks = [
      makeTask(1, { text: "Write spec", status: "pending" }),
      makeTask(2, { text: "Implement feature", status: "in_progress" }),
      makeTask(3, { text: "Ship it", status: "done" }),
    ];
    render(<TaskList detail={makeDetail(tasks)} />);

    expect(screen.getByTestId("task-row-1")).toHaveTextContent("Write spec");
    expect(screen.getByTestId("task-row-2")).toHaveTextContent(
      "Implement feature",
    );
    expect(screen.getByTestId("task-row-3")).toHaveTextContent("Ship it");
  });

  it("shows done/total progress in the header", () => {
    const tasks = [
      makeTask(1, { status: "pending" }),
      makeTask(2, { status: "in_progress" }),
      makeTask(3, { status: "done" }),
    ];
    render(<TaskList detail={makeDetail(tasks)} />);

    expect(screen.getByTestId("task-list-progress")).toHaveTextContent("1/3");
  });

  it("renders the matching status icon testid for each row", () => {
    const tasks = [
      makeTask(1, { status: "pending" }),
      makeTask(2, { status: "in_progress" }),
      makeTask(3, { status: "done" }),
    ];
    render(<TaskList detail={makeDetail(tasks)} />);

    expect(screen.getByTestId("task-row-3")).toContainElement(
      screen.getByTestId("task-status-done"),
    );
    expect(screen.getByTestId("task-row-2")).toContainElement(
      screen.getByTestId("task-status-in_progress"),
    );
    expect(screen.getByTestId("task-row-1")).toContainElement(
      screen.getByTestId("task-status-pending"),
    );
  });

  it("shows the empty-state copy when there are no tasks", () => {
    render(<TaskList detail={makeDetail([])} />);

    expect(screen.getByTestId("task-list-empty")).toHaveTextContent(
      "No tasks yet",
    );
    expect(screen.queryByTestId(/^task-row-/)).not.toBeInTheDocument();
  });

  it("treats a null/undefined tasks field as an empty list (RunCreate can return null)", () => {
    const detail = {
      ...makeDetail([]),
      tasks: null,
    } as unknown as app.RunDetailDTO;

    render(<TaskList detail={detail} />);

    expect(screen.getByTestId("task-list-empty")).toBeInTheDocument();
  });

  it("marks the root container as selectable text (read-only, no copy button)", () => {
    render(<TaskList detail={makeDetail([makeTask(1)])} />);

    expect(screen.getByTestId("task-list")).toHaveAttribute(
      "data-selectable-text",
      "true",
    );
  });
});
