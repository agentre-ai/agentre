import * as React from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { relativeTime } from "@/lib/relative-time";
import { cn } from "@/lib/utils";
import { useOrchRunListStore } from "../../../stores/orch-run-list-store";
import { StatusDot } from "../primitives";
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
        {/* 顶部新建按钮 */}
        <div className="p-2">
          <Button
            type="button"
            size="sm"
            className="w-full"
            data-testid="run-new-button"
            onClick={() => setDialogOpen(true)}
          >
            {t("orchestration.list.newButton")}
          </Button>
        </div>

        {/* Run 列表 */}
        <ul className="flex flex-col gap-0.5 px-1">
          {runs.map((run) => {
            const isActive = run.id === activeRunId;
            const time = relativeTime(run.updatetime || run.createtime);

            return (
              <li key={run.id}>
                <button
                  type="button"
                  data-testid={`run-row-${run.id}`}
                  className={cn(
                    "flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-xs transition-colors hover:bg-accent",
                    isActive && "bg-accent",
                  )}
                  onClick={() => onSelect(run.id)}
                >
                  <StatusDot status={runStatusToDot(run.status)} size="xs" />
                  <span className="min-w-0 flex-1 truncate font-medium">
                    {run.goal}
                  </span>
                  {time ? (
                    <span className="shrink-0 text-2xs text-muted-foreground">
                      {time}
                    </span>
                  ) : null}
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
