import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  IssueList: vi.fn(),
  IssueListLabels: vi.fn(),
  IssueSetState: vi.fn(),
  IssueDelete: vi.fn(),
  IssueCreate: vi.fn(),
  IssueUpdate: vi.fn(),
  ProjectListTree: vi.fn(),
}));
vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

import { IssuesPage } from "../issues-page";

describe("IssuesPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.ProjectListTree.mockResolvedValue([]);
    appMocks.IssueListLabels.mockResolvedValue([
      { id: 1, name: "bug", tone: "bug" },
    ]);
    appMocks.IssueList.mockResolvedValue({
      issues: [
        {
          id: 142,
          title: "fix OAuth state loss",
          state: "open",
          agentStatus: "idle",
          updatetime: 0,
          labels: [{ id: 1, name: "bug", tone: "bug" }],
        },
      ],
      openCount: 1,
      closedCount: 0,
    });
  });

  it("renders issues from the binding", async () => {
    render(<IssuesPage />);
    expect(await screen.findByText("fix OAuth state loss")).toBeInTheDocument();
    expect(screen.getByText("#142")).toBeInTheDocument();
  });

  it("uses the row action menu label and hides empty timestamps", async () => {
    render(<IssuesPage />);
    expect(await screen.findByText("fix OAuth state loss")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "More actions" }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/1970/)).not.toBeInTheDocument();
  });

  it("shows the empty state when there are no issues", async () => {
    appMocks.IssueList.mockResolvedValue({
      issues: [],
      openCount: 0,
      closedCount: 0,
    });
    render(<IssuesPage />);
    expect(await screen.findByText("No issues yet")).toBeInTheDocument();
  });

  it("switching to the Closed tab refetches with state=closed", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<IssuesPage />);
    await screen.findByText("fix OAuth state loss");
    await user.click(screen.getByRole("button", { name: /Closed/i }));
    await waitFor(() =>
      expect(appMocks.IssueList).toHaveBeenLastCalledWith(
        expect.objectContaining({ state: "closed" }),
      ),
    );
  });
});
