import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { buildGraph } from "./graph-data";
import { RunPause, RunResume, RunStop } from "../../../../wailsjs/go/app/App";
import type { app } from "../../../../wailsjs/go/models";

// 软阈值预警常量（超过时展示非阻塞黄条，不自动封顶）
const SOFT_SUBAGENTS = 12; // 子agent数量软上限
const SOFT_DEPTH = 5; // 树深度软上限

// 判断 Run 是否处于终态（done/stopped）
function isTerminal(status: string | undefined): boolean {
  return status === "done" || status === "stopped";
}

export function RunHeader({
  detail,
  view,
  onView,
}: {
  detail: app.RunDetailDTO;
  view: "graph" | "feed";
  onView: (v: "graph" | "feed") => void;
}) {
  const { t } = useTranslation();
  const { stats } = buildGraph(detail);
  const tasks = detail.tasks ?? [];

  // 按状态统计任务数
  const countRunning = tasks.filter((task) => task.status === "running").length;
  const countWaitingYou = tasks.filter(
    (task) => task.status === "awaiting-user",
  ).length;
  const countDone = tasks.filter((task) => task.status === "done").length;

  // 运行状态文本映射
  const runStatus = detail.run?.status;
  const statusTextKey = (() => {
    switch (runStatus) {
      case "running":
        return "orchestration.header.running";
      case "done":
        return "orchestration.header.completed";
      case "paused":
        return "orchestration.header.paused";
      case "stopped":
        return "orchestration.header.stopped";
      case "pending":
        return "orchestration.header.pending";
      default:
        return "orchestration.header.pending";
    }
  })();

  // 软阈值预警：子agent数或树深超过软上限时显示非阻塞黄条
  const showSoftWarning =
    stats.subagents > SOFT_SUBAGENTS || stats.depth > SOFT_DEPTH;

  const runId = detail.run?.id;

  return (
    <div className="flex flex-col gap-3 border-b border-border p-4">
      {/* 第一行：标题 + 视图切换分段控件 */}
      <div className="flex items-center justify-between gap-3">
        <h2 className="truncate text-base font-semibold text-foreground">
          {detail.run?.goal}
        </h2>

        {/* 视图分段控件：两个 Button 组合成 segmented control */}
        <div className="flex items-center rounded-md border border-border bg-muted p-0.5">
          <Button
            data-testid="view-graph"
            variant={view === "graph" ? "default" : "ghost"}
            size="sm"
            className={cn(
              "h-7 rounded-sm px-3 text-xs",
              view !== "graph" && "text-muted-foreground hover:text-foreground",
            )}
            onClick={() => onView("graph")}
          >
            {t("orchestration.header.viewGraph")}
          </Button>
          <Button
            data-testid="view-feed"
            variant={view === "feed" ? "default" : "ghost"}
            size="sm"
            className={cn(
              "h-7 rounded-sm px-3 text-xs",
              view !== "feed" && "text-muted-foreground hover:text-foreground",
            )}
            onClick={() => onView("feed")}
          >
            {t("orchestration.header.viewFeed")}
          </Button>
        </div>
      </div>

      {/* 第二行：状态文本 + 客观计数 chips */}
      <div className="flex flex-wrap items-center gap-2">
        {/* 运行状态文本 */}
        <span className="text-sm text-muted-foreground">
          {t(statusTextKey)}
        </span>

        <span className="text-muted-foreground/40">·</span>

        {/* 运行中计数 chip */}
        <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
          <span>{t("orchestration.header.countRunning")}</span>
          <span className="font-medium text-foreground">{countRunning}</span>
        </span>

        {/* 等待你计数 chip：> 0 时琥珀高亮 */}
        <span
          data-testid="count-waiting-you"
          className={cn(
            "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs",
            countWaitingYou > 0
              ? "bg-status-waiting-bg text-status-waiting"
              : "bg-muted text-muted-foreground",
          )}
        >
          <span>{t("orchestration.header.countWaitingYou")}</span>
          <span
            className={cn(
              "font-medium",
              countWaitingYou > 0 ? "text-status-waiting" : "text-foreground",
            )}
          >
            {countWaitingYou}
          </span>
        </span>

        {/* 已完成计数 chip */}
        <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
          <span>{t("orchestration.header.countDone")}</span>
          <span className="font-medium text-foreground">{countDone}</span>
        </span>
      </div>

      {/* 第三行：树规模 + token/时长占位 */}
      <div className="flex flex-wrap items-center gap-4 text-xs text-muted-foreground">
        <span>
          {t("orchestration.header.treeSize", {
            nodes: stats.nodes,
            subagents: stats.subagents,
            depth: stats.depth,
          })}
        </span>

        {/* token 占位（后端暂未统计） */}
        <span>
          {t("orchestration.header.tokens")}
          <span className="ml-1 text-foreground/40">—</span>
        </span>

        {/* 时长占位（后端暂未统计） */}
        <span>
          {t("orchestration.header.duration")}
          <span className="ml-1 text-foreground/40">—</span>
        </span>
      </div>

      {/* 第四行：干预控件（暂停/继续 + 硬停止），guard detail.run?.id */}
      {runId !== undefined && (
        <div className="flex items-center gap-2">
          {/* 暂停/继续按钮：按运行状态切换 */}
          {runStatus === "running" && (
            <Button
              data-testid="run-pause"
              variant="outline"
              size="sm"
              onClick={() => RunPause(runId)}
            >
              {t("orchestration.header.pause")}
            </Button>
          )}
          {runStatus === "paused" && (
            <Button
              data-testid="run-resume"
              variant="outline"
              size="sm"
              onClick={() => RunResume(runId)}
            >
              {t("orchestration.header.resume")}
            </Button>
          )}

          {/* 硬停止按钮：终态时禁用 */}
          <Button
            data-testid="run-stop"
            variant="destructive"
            size="sm"
            disabled={isTerminal(runStatus)}
            onClick={() => RunStop(runId)}
          >
            {t("orchestration.header.stop")}
          </Button>
        </div>
      )}

      {/* 软阈值预警黄条（非阻塞，仅提示） */}
      {showSoftWarning && (
        <div className="rounded-md border border-status-waiting/30 bg-status-waiting-bg px-3 py-2 text-xs text-status-waiting">
          {t("orchestration.header.softWarning")}
        </div>
      )}
    </div>
  );
}
