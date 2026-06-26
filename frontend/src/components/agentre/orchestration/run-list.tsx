import * as React from "react";
import { useTranslation } from "react-i18next";
import { Plus, Waypoints } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useOrchRunListStore } from "../../../stores/orch-run-list-store";
import type { AgentStatus } from "../types";

import { RunNewDialog } from "./run-new-dialog";

// Run 的客观生命周期状态映射到 StatusDot 显示状态:
// running  → running（运行中，绿点动画）
// paused   → waiting（暂停等待，黄点）
// 其余(pending/done/stopped) → idle（静止，灰点）
// Run 状态没有错误态，不创造 error 映射。
function runStatusToDot(status: string): AgentStatus {
  if (status === "running") return "running";
  if (status === "paused") return "waiting";
  return "idle";
}

// 状态点颜色 token（不依赖 StatusDot 组件，设计用纯 ellipse 7px）
function statusDotClass(status: string): string {
  const s = runStatusToDot(status);
  if (s === "running") return "bg-status-running motion-safe:animate-pulse";
  if (s === "waiting") return "bg-status-waiting";
  return "bg-status-idle";
}

// 图标容器 token: active run 用 primary-soft / primary-text；其余用 secondary / sidebar-icon
function iconBgClass(isActive: boolean): string {
  return isActive ? "bg-primary-soft" : "bg-secondary";
}
function iconColorClass(isActive: boolean): string {
  return isActive ? "text-primary-text" : "text-sidebar-icon";
}

// 状态描述文本（设计中 "3 running · 2 done" / "等待审批 · 1 ask" 等为动态数据，不翻译）
function statusMeta(status: string): string {
  if (status === "running") return "running";
  if (status === "paused") return "paused";
  if (status === "done") return "done";
  if (status === "stopped") return "stopped";
  return status;
}

export function RunList({
  activeRunId,
  onSelect,
}: {
  activeRunId?: number;
  onSelect: (runId: number) => void;
}) {
  const { t } = useTranslation();
  const runs = useOrchRunListStore((s) => s.runs);
  const [dialogOpen, setDialogOpen] = React.useState(false);

  // 组件挂载时加载 Run 列表
  React.useEffect(() => {
    void useOrchRunListStore.getState().load();
  }, []);

  if (runs.length === 0) {
    return (
      <>
        <div
          data-testid="run-onboarding-cta"
          className="flex flex-1 flex-col items-center justify-center gap-4 px-4 py-8 text-center"
        >
          <div className="flex flex-col gap-2">
            <p className="text-sm font-medium text-foreground">
              {t("orchestration.onboarding.cta")}
            </p>
            <ol className="flex flex-col gap-1 text-xs text-muted-foreground">
              <li>{t("orchestration.onboarding.step1")}</li>
              <li>{t("orchestration.onboarding.step2")}</li>
              <li>{t("orchestration.onboarding.step3")}</li>
            </ol>
          </div>
          <Button type="button" size="sm" onClick={() => setDialogOpen(true)}>
            {t("orchestration.list.newButton")}
          </Button>
        </div>
        <RunNewDialog open={dialogOpen} onOpenChange={setDialogOpen} />
      </>
    );
  }

  return (
    <>
      <div className="flex flex-col">
        {/* listHead: padding=[14,14,8,14] → pt-3.5 pr-3.5 pb-2 pl-3.5, gap-2 items-center */}
        <div
          data-testid="run-list-head"
          className="flex items-center gap-2 pb-2 pl-3.5 pr-3.5 pt-3.5"
        >
          <span className="text-[13px] font-semibold text-foreground font-sans">
            {t("orchestration.list.title")}
          </span>
          {/* spacer */}
          <span className="flex-1" />
          {/* newBtn: bg-primary rounded-md px-2 py-1, plus icon + label */}
          <button
            type="button"
            data-testid="run-new-button"
            className="flex items-center gap-1 rounded-md bg-primary px-2 py-1 text-primary-foreground transition-opacity hover:opacity-90"
            onClick={() => setDialogOpen(true)}
          >
            <Plus className="h-[11px] w-[11px]" />
            <span className="font-mono text-[10px] font-semibold">
              {t("orchestration.list.newBtnLabel")}
            </span>
          </button>
        </div>

        {/* filters: padding=[0,14,10,14], gap-6
            FUTURE: wire up filtering logic in a future slice — currently static visual chips */}
        <div
          data-testid="run-filter-chips"
          className="flex items-center gap-6 pb-[10px] pl-3.5 pr-3.5"
        >
          {/* Active chip: bg-secondary border border-border */}
          <span className="rounded-full border border-border bg-secondary px-2 py-[3px] text-[11px] font-semibold text-foreground">
            {t("orchestration.list.filterAll")}
          </span>
          <span className="rounded-full px-2 py-[3px] text-[11px] text-muted-foreground">
            {t("orchestration.list.filterRunning")}
          </span>
          <span className="rounded-full px-2 py-[3px] text-[11px] text-muted-foreground">
            {t("orchestration.list.filterDone")}
          </span>
        </div>

        {/* runs: padding=[0,8], gap-2, vertical */}
        <ul className="flex flex-col gap-2 px-2">
          {runs.map((run) => {
            const isActive = run.id === activeRunId;

            return (
              <li key={run.id}>
                <button
                  type="button"
                  data-testid={`run-row-${run.id}`}
                  data-active={isActive ? "true" : "false"}
                  className={cn(
                    "flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left transition-colors",
                    isActive ? "bg-secondary" : "hover:bg-muted/60",
                  )}
                  onClick={() => onSelect(run.id)}
                >
                  {/* icon container: 22x22 rounded-md */}
                  <span
                    className={cn(
                      "flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-md",
                      iconBgClass(isActive),
                    )}
                  >
                    <Waypoints
                      className={cn("h-3 w-3", iconColorClass(isActive))}
                    />
                  </span>

                  {/* text stack: name + status meta */}
                  <span className="flex min-w-0 flex-1 flex-col gap-px">
                    <span
                      className={cn(
                        "truncate text-[12.5px] leading-tight text-foreground",
                        isActive ? "font-semibold" : "font-medium",
                      )}
                    >
                      {run.goal}
                    </span>
                    <span className="truncate font-mono text-[9.5px] text-muted-foreground leading-tight">
                      {statusMeta(run.status)}
                    </span>
                  </span>

                  {/* status dot ellipse 7×7 */}
                  <span
                    className={cn(
                      "h-[7px] w-[7px] shrink-0 rounded-full",
                      statusDotClass(run.status),
                    )}
                  />
                </button>
              </li>
            );
          })}
        </ul>
      </div>
      <RunNewDialog open={dialogOpen} onOpenChange={setDialogOpen} />
    </>
  );
}
