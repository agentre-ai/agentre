import * as React from "react";
import { useTranslation } from "react-i18next";

import type { app } from "../../../../wailsjs/go/models";
import { parseFlowGraph } from "./flow-graph";
import { FlowGraphView } from "./flow-graph-view";
import { deriveNodeOverlay } from "./flow-overlay";

// RunFlowOverlay: 把当前 Run 的任务实况叠加到快照的 flow DAG 上(只读)。
export function RunFlowOverlay({ detail }: { detail: app.RunDetailDTO }) {
  const { t } = useTranslation();
  const graph = detail.run?.flowGraph ?? "";
  const rawTasks = detail.tasks;

  const overlay = React.useMemo(
    () =>
      deriveNodeOverlay(
        graph,
        (rawTasks ?? []).map((tk) => ({
          nodeRef: tk.nodeRef,
          status: tk.status,
        })),
      ),
    [graph, rawTasks],
  );

  if (!parseFlowGraph(graph)) {
    return (
      <div
        data-testid="run-flow-empty"
        className="flex flex-1 items-center justify-center p-8 text-sm text-muted-foreground"
      >
        {t("orchestration.flow.empty")}
      </div>
    );
  }

  return (
    <div data-testid="run-flow-overlay" className="p-5">
      <FlowGraphView graph={graph} overlay={overlay} />
    </div>
  );
}
