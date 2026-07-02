import { useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { LibraryBig } from "lucide-react";
import { useOrchRunListStore } from "../../../stores/orch-run-list-store";
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";
import { OrchestrationRun } from ".";
import { RunList } from "./run-list";
import { OrchestrationOverview } from "./orchestration-overview";

export function OrchestrationPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { runId: runIdParam } = useParams();
  const parsedRunId = runIdParam ? Number(runIdParam) : null;
  // 非法 :runId(如 /orchestration/abc → NaN)归一为 null, 不外泄到 activeRunId/主区
  const runId =
    parsedRunId !== null && Number.isFinite(parsedRunId) ? parsedRunId : null;
  const runs = useOrchRunListStore((s) => s.runs);
  const goal = runId ? (runs.find((r) => r.id === runId)?.goal ?? "") : "";

  return (
    <div className="flex min-h-0 min-w-0 flex-1">
      {/* 左:Run 侧栏(仅 Run, 不混会话) */}
      <aside className="flex w-80 shrink-0 flex-col border-r border-border bg-sidebar">
        <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
          <RunList
            activeRunId={runId ?? undefined}
            onSelect={(id) => navigate(`/orchestration/${id}`)}
          />
        </div>
        <button
          type="button"
          data-testid="run-library-entry"
          onClick={() => useWorkflowManagerStore.getState().openBrowse()}
          className="flex shrink-0 items-center gap-2 border-t border-border px-3.5 py-2.5 text-left text-[12.5px] text-muted-foreground transition-colors hover:text-foreground"
        >
          <LibraryBig className="h-4 w-4" aria-hidden="true" />
          <span>{t("orchestration.list.libraryEntry")}</span>
        </button>
      </aside>

      {/* 主区:选中 Run 渲 OrchestrationRun, 否则起步态(完整总览=S8) */}
      {runId ? (
        <div className="flex min-h-0 min-w-0 flex-1">
          <OrchestrationRun runId={runId} title={goal} />
        </div>
      ) : (
        <OrchestrationOverview />
      )}
    </div>
  );
}
