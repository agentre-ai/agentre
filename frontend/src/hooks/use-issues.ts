import { useCallback, useEffect, useState } from "react";

import { IssueList, IssueListLabels } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";

export type IssueFilter = {
  state: string; // "" = all (board); "open" / "closed" (list tabs)
  projectID: number;
  labelIDs: number[];
};

export function useIssues(filter: IssueFilter) {
  const [issues, setIssues] = useState<app.IssueItem[]>([]);
  const [labels, setLabels] = useState<app.LabelItem[]>([]);
  const [openCount, setOpenCount] = useState(0);
  const [closedCount, setClosedCount] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { state, projectID } = filter;
  const labelKey = filter.labelIDs.join(",");

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const labelIDs = labelKey ? labelKey.split(",").map(Number) : [];
      const [resp, labelList] = await Promise.all([
        IssueList({ state, projectID, labelIDs, sort: "updated" }),
        IssueListLabels(),
      ]);
      setIssues(resp?.issues ?? []);
      setOpenCount(resp?.openCount ?? 0);
      setClosedCount(resp?.closedCount ?? 0);
      setLabels(labelList ?? []);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [state, projectID, labelKey]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { issues, labels, openCount, closedCount, loading, error, reload };
}
