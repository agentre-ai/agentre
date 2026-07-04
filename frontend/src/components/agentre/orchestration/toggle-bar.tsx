import { useTranslation } from "react-i18next";
import {
  GitFork,
  GitMerge,
  List,
  ListChecks,
  Users,
  Waypoints,
} from "lucide-react";
import { cn } from "@/lib/utils";

export interface ToggleBarStats {
  done: number;
  total: number;
  depth: number;
  agentCount: number;
  subCount: number;
  subagentCount: number;
}

interface ToggleBarProps {
  view: "graph" | "feed" | "flow";
  onView: (v: "graph" | "feed" | "flow") => void;
  stats: ToggleBarStats;
  showFlow?: boolean;
}

export function ToggleBar({
  view,
  onView,
  stats,
  showFlow = false,
}: ToggleBarProps) {
  const { t } = useTranslation();

  return (
    <div
      data-testid="orch-toggle"
      className="flex shrink-0 items-center gap-3 border-b border-border bg-card px-5 py-2.5"
    >
      {/* Segmented control */}
      <div className="flex items-center gap-0.5 rounded-lg bg-secondary p-0.5">
        {showFlow ? (
          <button
            type="button"
            data-testid="toggle-flow"
            onClick={() => onView("flow")}
            className={cn(
              "flex items-center gap-1.5 rounded-md px-3 py-[5px] text-xs",
              view === "flow"
                ? "border border-border bg-card font-semibold text-foreground"
                : "text-muted-foreground",
            )}
          >
            <Waypoints className="size-3 shrink-0" aria-hidden="true" />
            {t("orchestration.header.viewFlow")}
          </button>
        ) : null}
        <button
          type="button"
          data-testid="toggle-graph"
          onClick={() => onView("graph")}
          className={cn(
            "flex items-center gap-1.5 rounded-md px-3 py-[5px] text-xs",
            view === "graph"
              ? "border border-border bg-card font-semibold text-foreground"
              : "text-muted-foreground",
          )}
        >
          <GitFork className="size-3 shrink-0" aria-hidden="true" />
          {t("orchestration.header.viewGraph")}
        </button>
        <button
          type="button"
          data-testid="toggle-feed"
          onClick={() => onView("feed")}
          className={cn(
            "flex items-center gap-1.5 rounded-md px-3 py-[5px] text-xs",
            view === "feed"
              ? "border border-border bg-card font-semibold text-foreground"
              : "text-muted-foreground",
          )}
        >
          <List className="size-3 shrink-0" aria-hidden="true" />
          {t("orchestration.header.viewFeed")}
        </button>
      </div>

      {/* Spacer */}
      <div className="flex-1" />

      {/* Stat chips */}
      <div
        data-testid="toggle-stat-tasks"
        className="flex items-center gap-[5px] rounded-md bg-secondary px-2 py-[3px] font-mono text-[11px] text-muted-foreground"
      >
        <ListChecks className="size-3 shrink-0" aria-hidden="true" />
        <span>
          {stats.done}/{stats.total} {t("orchestration.toggle.tasks")}
        </span>
      </div>

      <div
        data-testid="toggle-stat-depth"
        className="flex items-center gap-[5px] rounded-md bg-secondary px-2 py-[3px] font-mono text-[11px] text-muted-foreground"
      >
        <GitFork className="size-3 shrink-0" aria-hidden="true" />
        <span>
          {t("orchestration.toggle.depth")} {stats.depth}
        </span>
      </div>

      <div
        data-testid="toggle-stat-agents"
        className="flex items-center gap-[5px] rounded-md bg-secondary px-2 py-[3px] font-mono text-[11px] text-muted-foreground"
      >
        <Users className="size-3 shrink-0" aria-hidden="true" />
        <span>
          {t("orchestration.toggle.agents", {
            agentCount: stats.agentCount,
            subCount: stats.subCount,
          })}
        </span>
      </div>

      <div
        data-testid="toggle-stat-subagents"
        className="flex items-center gap-[5px] rounded-md bg-secondary px-2 py-[3px] font-mono text-[11px] text-muted-foreground"
      >
        <GitMerge className="size-3 shrink-0" aria-hidden="true" />
        <span>
          {stats.subagentCount} {t("orchestration.toggle.subagents")}
        </span>
      </div>
    </div>
  );
}
