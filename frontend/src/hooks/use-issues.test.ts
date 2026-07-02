import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../wailsjs/go/app/App", () => ({
  IssueList: vi.fn(),
  IssueListLabels: vi.fn(),
  IssueMove: vi.fn(),
}));

import {
  IssueList,
  IssueListLabels,
  IssueMove,
} from "../../wailsjs/go/app/App";
import { useIssues } from "./use-issues";

const issueList = IssueList as ReturnType<typeof vi.fn>;
const issueListLabels = IssueListLabels as ReturnType<typeof vi.fn>;
const issueMove = IssueMove as ReturnType<typeof vi.fn>;

describe("useIssues", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    issueList.mockResolvedValue({
      issues: [
        {
          id: 1,
          title: "demo",
          state: "open",
          agentStatus: "idle",
          labels: [],
          stage: "doing",
          position: 10,
        },
      ],
      openCount: 1,
      closedCount: 0,
      stageCounts: { doing: 1 },
    });
    issueListLabels.mockResolvedValue([{ id: 1, name: "bug", tone: "bug" }]);
    issueMove.mockResolvedValue({
      id: 1,
      stage: "review",
      position: 5,
      labels: [],
    });
  });

  it("loads issues, labels and counts on mount", async () => {
    const { result } = renderHook(() =>
      useIssues({ state: "open", projectID: 0, labelIDs: [] }),
    );
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.issues).toHaveLength(1);
    expect(result.current.openCount).toBe(1);
    expect(result.current.labels[0].name).toBe("bug");
    expect(issueList).toHaveBeenCalledWith(
      expect.objectContaining({ state: "open", projectID: 0 }),
    );
  });

  it("captures errors as a string", async () => {
    issueList.mockRejectedValue(new Error("boom"));
    const { result } = renderHook(() =>
      useIssues({ state: "open", projectID: 0, labelIDs: [] }),
    );
    await waitFor(() => expect(result.current.error).toBe("boom"));
  });

  it("board 默认按 position 拉取并暴露 stageCounts", async () => {
    const { result } = renderHook(() =>
      useIssues({ state: "", projectID: 0, labelIDs: [], sort: "position" }),
    );
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(issueList).toHaveBeenCalledWith(
      expect.objectContaining({ sort: "position" }),
    );
    expect(result.current.stageCounts.doing).toBe(1);
  });

  it("moveIssue 调 IssueMove", async () => {
    const { result } = renderHook(() =>
      useIssues({ state: "", projectID: 0, labelIDs: [], sort: "position" }),
    );
    await waitFor(() => expect(result.current.loading).toBe(false));
    await result.current.moveIssue(1, "review", 0);
    expect(issueMove).toHaveBeenCalledWith({
      id: 1,
      stage: "review",
      afterID: 0,
    });
  });
});
