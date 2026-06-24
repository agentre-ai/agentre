import * as React from "react";
import { useTranslation } from "react-i18next";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { RunHeader } from "./run-header";
import { StructureGraph } from "./structure-graph";
import { ActivityFeed } from "./activity-feed";
import { TaskBoard } from "./task-board";

export function OrchestrationRun({
  runId,
  title,
}: {
  runId: number;
  title: string;
}) {
  const { t } = useTranslation();
  const detail = useOrchRunStore((s) => s.details.get(runId));

  React.useEffect(() => {
    void useOrchRunStore.getState().loadRun(runId);
  }, [runId]);

  const [view, setView] = React.useState<"graph" | "feed">("graph");
  const [selectedAgentId, setSelectedAgentId] = React.useState<number | null>(
    null,
  );

  return (
    <div
      data-testid="orchestration-run"
      aria-label={title}
      className="flex h-full"
    >
      {!detail || !detail.run ? (
        // 加载占位:detail 未就绪 或 run 字段缺失(可选字段)时显示,
        // 避免把 run=undefined 的半成品 detail 传给子组件。
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          {t("orchestration.loading")}
        </div>
      ) : (
        <>
          {/* 主区域：头部 + 中视图 */}
          <main className="flex min-w-0 flex-1 flex-col">
            <RunHeader detail={detail} view={view} onView={setView} />
            <div className="min-h-0 flex-1 overflow-auto">
              {view === "graph" ? (
                <StructureGraph
                  detail={detail}
                  onSelectNode={setSelectedAgentId}
                />
              ) : (
                <ActivityFeed detail={detail} />
              )}
            </div>
          </main>

          {/* 右侧：任务看板 */}
          <aside className="w-80 shrink-0 border-l border-border">
            <TaskBoard
              detail={detail}
              selectedAgentId={selectedAgentId}
              onSelectTask={(agentId) => setSelectedAgentId(agentId)}
            />
          </aside>
        </>
      )}
    </div>
  );
}
