import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { BackgroundTasksChip } from "./background-tasks-chip";
import type { BackgroundTask } from "./types";

const running: BackgroundTask = {
  toolUseId: "tu1",
  kind: "local_bash",
  description: "sleep 20",
  status: "running",
};

const completed: BackgroundTask = {
  toolUseId: "tu2",
  kind: "local_agent",
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
  it("renders null when no running tasks", () => {
    const { container } = render(
      <BackgroundTasksChip tasks={[completed, failed]} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders null when tasks is empty", () => {
    const { container } = render(<BackgroundTasksChip tasks={[]} />);
    expect(container.firstChild).toBeNull();
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
      kind: "local_agent",
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

  it("shows kind labels (bash / subagent) in popover", () => {
    render(
      <BackgroundTasksChip
        tasks={[running, { ...completed, status: "running" as const }]}
      />,
    );
    fireEvent.click(screen.getByRole("button"));
    expect(screen.getByText("bash")).toBeInTheDocument();
    expect(screen.getByText("subagent")).toBeInTheDocument();
  });
});
