import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../wailsjs/go/app/App", () => ({
  IssueList: vi.fn(),
  IssueListLabels: vi.fn(),
}));

import { IssueList, IssueListLabels } from "../../wailsjs/go/app/App";
import { useIssues } from "./use-issues";

const issueList = IssueList as ReturnType<typeof vi.fn>;
const issueListLabels = IssueListLabels as ReturnType<typeof vi.fn>;

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
        },
      ],
      openCount: 1,
      closedCount: 0,
    });
    issueListLabels.mockResolvedValue([{ id: 1, name: "bug", tone: "bug" }]);
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
});
