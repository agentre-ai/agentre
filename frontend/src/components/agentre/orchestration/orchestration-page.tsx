import { useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useOrchRunListStore } from "../../../stores/orch-run-list-store";
import { OrchestrationRun } from ".";
import { RunList } from "./run-list";

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
      <aside className="flex w-72 shrink-0 flex-col overflow-y-auto border-r border-border bg-sidebar">
        <RunList
          activeRunId={runId ?? undefined}
          onSelect={(id) => navigate(`/orchestration/${id}`)}
        />
      </aside>

      {/* 主区:选中 Run 渲 OrchestrationRun, 否则起步态(完整总览=S8) */}
      {runId ? (
        <div className="flex min-h-0 min-w-0 flex-1">
          <OrchestrationRun runId={runId} title={goal} />
        </div>
      ) : (
        <div
          data-testid="orchestration-empty-main"
          className="flex flex-1 items-center justify-center p-8 text-center text-sm text-muted-foreground"
        >
          {t("orchestration.onboarding.cta")}
        </div>
      )}
    </div>
  );
}
