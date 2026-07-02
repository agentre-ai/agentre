import { useCallback, useEffect, useState } from "react";

import { IssueList, IssueListLabels, IssueMove } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";

export type IssueFilter = {
  state: string; // "" = all (board); "open" / "closed" (list tabs)
  projectID: number;
  labelIDs: number[];
  sort?: "position" | "updated";
};

export function useIssues(filter: IssueFilter) {
  const [issues, setIssues] = useState<app.IssueItem[]>([]);
  const [labels, setLabels] = useState<app.LabelItem[]>([]);
  const [openCount, setOpenCount] = useState(0);
  const [closedCount, setClosedCount] = useState(0);
  const [stageCounts, setStageCounts] = useState<Record<string, number>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { state, projectID } = filter;
  const sort = filter.sort ?? "updated";
  const labelKey = filter.labelIDs.join(",");

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const labelIDs = labelKey ? labelKey.split(",").map(Number) : [];
      const [resp, labelList] = await Promise.all([
        IssueList({ state, projectID, labelIDs, sort }),
        IssueListLabels(),
      ]);
      setIssues(resp?.issues ?? []);
      setOpenCount(resp?.openCount ?? 0);
      setClosedCount(resp?.closedCount ?? 0);
      setStageCounts(resp?.stageCounts ?? {});
      setLabels(labelList ?? []);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [state, projectID, sort, labelKey]);

  const moveIssue = useCallback(
    async (id: number, stage: string, afterID: number) => {
      await IssueMove({ id, stage, afterID });
    },
    [],
  );

  useEffect(() => {
    void reload();
  }, [reload]);

  return {
    issues, labels, openCount, closedCount, stageCounts,
    loading, error, reload, moveIssue,
  };
}
