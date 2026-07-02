import * as React from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Waypoints } from "lucide-react";

import { relativeTime } from "@/lib/relative-time";
import { useOrchRunListStore } from "../../../stores/orch-run-list-store";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { ListChatAgents } from "../../../../wailsjs/go/app/App";
import { firstLetter, tokenToCssColor } from "../session-avatar";
import {
  computeRunStats,
  formatDuration,
  inProgressRuns,
  recentDoneRuns,
  runProgress,
} from "./overview-data";

type AgentMeta = { id: number; name: string; avatarColor: string };

export function OrchestrationOverview() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const runs = useOrchRunListStore((s) => s.runs);
  const details = useOrchRunStore((s) => s.details);
  const loadRun = useOrchRunStore((s) => s.loadRun);
  const [agents, setAgents] = React.useState<AgentMeta[]>([]);

  React.useEffect(() => {
    void useOrchRunListStore.getState().load();
  }, []);

  React.useEffect(() => {
    ListChatAgents()
      .then((resp) =>
        setAgents(
          (resp?.agents ?? []).map(
            (a: { id: number; name: string; avatarColor: string }) => ({
              id: a.id,
              name: a.name,
              avatarColor: a.avatarColor,
            }),
          ),
        ),
      )
      .catch(() => setAgents([]));
  }, []);

  const [now, setNow] = React.useState(() => Date.now());
  React.useEffect(() => {
    setNow(Date.now());
  }, [runs]);
  const stats = React.useMemo(() => computeRunStats(runs, now), [runs, now]);
  const inProgress = React.useMemo(() => inProgressRuns(runs), [runs]);
  const recent = React.useMemo(() => recentDoneRuns(runs), [runs]);

  // 懒加载少量 running Run 的详情以显示进度(顺带暖 detail 缓存,点进去更快)。
  React.useEffect(() => {
    for (const r of inProgress) {
      if (!details.has(r.id)) void loadRun(r.id);
    }
  }, [inProgress, details, loadRun]);

  const leaderOf = React.useCallback(
    (id: number) => agents.find((a) => a.id === id),
    [agents],
  );

  if (runs.length === 0) {
    return (
      <div
        data-testid="orchestration-overview"
        className="flex flex-1 items-center justify-center p-8 text-center text-sm text-muted-foreground"
      >
        {t("orchestration.onboarding.cta")}
      </div>
    );
  }

  return (
    <div
      data-testid="orchestration-overview"
      className="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto p-6"
    >
      <h1 className="text-lg font-semibold text-foreground">
        {t("orchestration.overview.title")}
      </h1>
      <p className="mb-4 text-xs text-muted-foreground">
        {t("orchestration.overview.subtitle")}
      </p>

      <div className="mb-6 grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatCard
          testid="overview-stat-running"
          label={t("orchestration.overview.statRunning")}
          value={String(stats.running)}
        />
        <StatCard
          testid="overview-stat-waiting"
          label={t("orchestration.overview.statWaiting")}
          value={String(stats.waiting)}
        />
        <StatCard
          testid="overview-stat-doneWeek"
          label={t("orchestration.overview.statDoneThisWeek")}
          value={String(stats.doneThisWeek)}
        />
        <StatCard
          testid="overview-stat-avgDuration"
          label={t("orchestration.overview.statAvgDuration")}
          value={
            formatDuration(stats.avgDurationMs) ||
            t("orchestration.overview.noDuration")
          }
        />
      </div>

      <section className="mb-6">
        <h2 className="mb-2 text-sm font-semibold text-foreground">
          {t("orchestration.overview.inProgress")}
        </h2>
        {inProgress.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            {t("orchestration.overview.inProgressEmpty")}
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            {inProgress.map((r) => {
              const prog = runProgress(details.get(r.id)?.tasks);
              const pct =
                prog.total > 0 ? Math.round((prog.done / prog.total) * 100) : 0;
              const leader = leaderOf(r.leaderAgentId);
              return (
                <button
                  key={r.id}
                  type="button"
                  data-testid={`overview-inprogress-card-${r.id}`}
                  onClick={() => navigate(`/orchestration/${r.id}`)}
                  className="flex flex-col gap-2 rounded-lg border border-border bg-card px-3.5 py-3 text-left transition-colors hover:bg-accent/40"
                >
                  <span className="flex items-center gap-2">
                    <span
                      aria-hidden="true"
                      className="flex size-5 shrink-0 items-center justify-center rounded-full text-2xs font-semibold text-white"
                      style={{
                        backgroundColor:
                          tokenToCssColor(leader?.avatarColor ?? "") ??
                          "#94a3b8",
                      }}
                    >
                      {firstLetter(leader?.name ?? "?")}
                    </span>
                    <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-foreground">
                      {r.goal}
                    </span>
                    <span className="shrink-0 font-mono text-2xs text-muted-foreground">
                      {relativeTime(r.updatetime)}
                    </span>
                  </span>
                  <span className="flex items-center gap-2">
                    <span
                      role="progressbar"
                      aria-valuemin={0}
                      aria-valuemax={100}
                      aria-valuenow={pct}
                      aria-label={t("orchestration.overview.progress")}
                      className="h-1.5 flex-1 overflow-hidden rounded-full bg-secondary"
                    >
                      <span
                        className="block h-full rounded-full bg-status-running"
                        style={{ width: `${pct}%` }}
                      />
                    </span>
                    <span
                      data-testid={`overview-inprogress-progress-${r.id}`}
                      className="shrink-0 font-mono text-2xs text-muted-foreground"
                    >
                      {t("orchestration.overview.progress", {
                        done: prog.done,
                        total: prog.total,
                      })}
                    </span>
                  </span>
                </button>
              );
            })}
          </div>
        )}
      </section>

      <section>
        <h2 className="mb-2 text-sm font-semibold text-foreground">
          {t("orchestration.overview.recentDone")}
        </h2>
        {recent.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            {t("orchestration.overview.recentDoneEmpty")}
          </p>
        ) : (
          <ul className="flex flex-col">
            {recent.map((r) => (
              <li key={r.id}>
                <button
                  type="button"
                  data-testid={`overview-recent-row-${r.id}`}
                  onClick={() => navigate(`/orchestration/${r.id}`)}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left hover:bg-accent/40"
                >
                  <Waypoints
                    className="size-3.5 shrink-0 text-muted-foreground"
                    aria-hidden="true"
                  />
                  <span className="min-w-0 flex-1 truncate text-xs text-foreground">
                    {r.goal}
                  </span>
                  <span className="shrink-0 font-mono text-2xs text-muted-foreground">
                    {relativeTime(r.updatetime)}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

function StatCard({
  testid,
  label,
  value,
}: {
  testid: string;
  label: string;
  value: string;
}) {
  return (
    <div
      data-testid={testid}
      className="flex flex-col gap-1 rounded-lg border border-border bg-card px-3.5 py-3"
    >
      <span className="text-2xs uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <span className="text-xl font-semibold text-foreground">{value}</span>
    </div>
  );
}
