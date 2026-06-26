import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Waypoints,
  Target,
  Pause,
  Square,
  Ellipsis,
  Circle,
} from "lucide-react";
import { buildGraph } from "./graph-data";
import { useChatAgents } from "@/hooks/use-chat-agents";
import { RunPause, RunResume, RunStop } from "../../../../wailsjs/go/app/App";
import type { app } from "../../../../wailsjs/go/models";

// 软阈值预警常量（超过时展示非阻塞黄条，不自动封顶）
const SOFT_SUBAGENTS = 12; // 子agent数量软上限
const SOFT_DEPTH = 5; // 树深度软上限

// 判断 Run 是否处于终态（done/stopped）
function isTerminal(status: string | undefined): boolean {
  return status === "done" || status === "stopped";
}

// 状态胶囊配置
const PHASE_CONFIG: Record<
  string,
  { labelKey: string; bgClass: string; textClass: string; dotClass: string }
> = {
  running: {
    labelKey: "orchestration.header.running",
    bgClass: "bg-status-running-bg",
    textClass: "text-status-running",
    dotClass: "text-status-running",
  },
  paused: {
    labelKey: "orchestration.header.paused",
    bgClass: "bg-status-waiting-bg",
    textClass: "text-status-waiting",
    dotClass: "text-status-waiting",
  },
  done: {
    labelKey: "orchestration.header.completed",
    bgClass: "bg-status-running-bg",
    textClass: "text-status-running",
    dotClass: "text-status-running",
  },
  stopped: {
    labelKey: "orchestration.header.stopped",
    bgClass: "bg-muted",
    textClass: "text-muted-foreground",
    dotClass: "text-muted-foreground",
  },
  pending: {
    labelKey: "orchestration.header.pending",
    bgClass: "bg-muted",
    textClass: "text-muted-foreground",
    dotClass: "text-muted-foreground",
  },
};

export function RunHeader({ detail }: { detail: app.RunDetailDTO }) {
  const { t } = useTranslation();
  const { agents } = useChatAgents();
  const { stats } = buildGraph(detail);

  const runStatus = detail.run?.status;
  const runId = detail.run?.id;
  const goal = detail.run?.goal ?? "";

  // 软阈值预警
  const showSoftWarning =
    stats.subagents > SOFT_SUBAGENTS || stats.depth > SOFT_DEPTH;

  // 状态胶囊配置
  const phaseConfig =
    PHASE_CONFIG[runStatus ?? "pending"] ?? PHASE_CONFIG["pending"];

  // Leader name for subline
  const leaderAgentId = detail.run?.leaderAgentId;
  const leaderAgent = agents.find((a) => a.id === leaderAgentId);
  const leaderName =
    leaderAgent?.name ?? (leaderAgentId ? `#${leaderAgentId}` : "");

  return (
    <div
      data-testid="run-header"
      className="flex flex-col gap-2 border-b border-border bg-card px-5 py-3.5"
    >
      {/* Topline: icon badge + title + status pill + spacer + controls */}
      <div className="flex items-center gap-2.5">
        {/* Icon badge */}
        <div
          data-testid="run-header-icon"
          className="flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-lg bg-primary-soft"
        >
          <Waypoints className="size-4 text-primary-text" aria-hidden="true" />
        </div>

        {/* Title: goal · #runId */}
        <h2
          data-testid="run-header-title"
          className="truncate text-base font-bold text-foreground"
        >
          {goal}
          {runId !== undefined && (
            <span className="ml-1.5 font-normal text-muted-foreground">
              · #{runId}
            </span>
          )}
        </h2>

        {/* Status pill */}
        <div
          data-testid="run-header-status-pill"
          className={cn(
            "flex shrink-0 items-center gap-1.5 rounded-full px-2 py-[3px]",
            phaseConfig.bgClass,
          )}
        >
          <Circle
            className={cn("size-1.5 fill-current", phaseConfig.dotClass)}
            aria-hidden="true"
          />
          <span className={cn("text-2xs font-semibold", phaseConfig.textClass)}>
            {t(phaseConfig.labelKey)}
          </span>
        </div>

        {/* Spacer */}
        <div className="flex-1" />

        {/* Controls (only when runId is defined) */}
        {runId !== undefined && (
          <div className="flex items-center gap-2">
            {/* Pause / Resume button — phase-gated */}
            {runStatus === "running" && (
              <Button
                data-testid="run-pause"
                variant="outline"
                size="sm"
                className="flex items-center gap-1.5 rounded-md border border-border bg-card px-2.5 py-1.5 text-xs font-normal"
                onClick={() => {
                  void RunPause(runId).catch(() => {});
                }}
              >
                <Pause className="size-3 shrink-0" aria-hidden="true" />
                {t("orchestration.header.pause")}
              </Button>
            )}
            {runStatus === "paused" && (
              <Button
                data-testid="run-resume"
                variant="outline"
                size="sm"
                className="flex items-center gap-1.5 rounded-md border border-border bg-card px-2.5 py-1.5 text-xs font-normal"
                onClick={() => {
                  void RunResume(runId).catch(() => {});
                }}
              >
                <Pause className="size-3 shrink-0" aria-hidden="true" />
                {t("orchestration.header.resume")}
              </Button>
            )}

            {/* Stop button */}
            <Button
              data-testid="run-stop"
              variant="outline"
              size="sm"
              disabled={isTerminal(runStatus)}
              className="flex items-center gap-1.5 rounded-md border border-border bg-card px-2.5 py-1.5 text-xs font-normal text-destructive hover:text-destructive"
              onClick={() => {
                void RunStop(runId).catch(() => {});
              }}
            >
              <Square className="size-3 shrink-0" aria-hidden="true" />
              {t("orchestration.header.stop")}
            </Button>

            {/* More button */}
            <button
              type="button"
              data-testid="run-header-more"
              className="flex h-7 w-[30px] items-center justify-center rounded-md border border-border bg-card text-muted-foreground hover:text-foreground"
              aria-label={t("orchestration.header.more")}
            >
              <Ellipsis className="size-3.5" aria-hidden="true" />
            </button>
          </div>
        )}
      </div>

      {/* Subline: target icon + goal · Leader name */}
      <div
        data-testid="run-header-subline"
        className="flex min-w-0 items-center gap-1.5"
      >
        <Target
          className="size-3 shrink-0 text-muted-foreground"
          aria-hidden="true"
        />
        <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
          {goal}
          {leaderName && (
            <>
              {" "}
              · {t("orchestration.header.leaderLabel")} {leaderName}
            </>
          )}
        </span>
      </div>

      {/* 软阈值预警黄条（非阻塞，仅提示） */}
      {showSoftWarning && (
        <div
          data-testid="run-header-soft-warning"
          className="rounded-md border border-status-waiting/30 bg-status-waiting-bg px-3 py-2 text-xs text-status-waiting"
        >
          {t("orchestration.header.softWarning")}
        </div>
      )}
    </div>
  );
}
