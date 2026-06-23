import { Terminal } from "lucide-react";
import React from "react";
import { useTranslation } from "react-i18next";

import type { BackgroundTask } from "./types";

// formatElapsed 将毫秒数格式化为紧凑耗时字符串（computed — 不走 t()）。
// < 60s → "12s"  |  < 60m → "3m 05s"  |  else → "1h 02m"
function formatElapsed(ms: number): string {
  const totalSec = Math.floor(ms / 1000);
  if (totalSec < 60) return `${totalSec}s`;
  const totalMin = Math.floor(totalSec / 60);
  if (totalMin < 60) {
    const remSec = totalSec % 60;
    return `${totalMin}m ${remSec.toString().padStart(2, "0")}s`;
  }
  const hours = Math.floor(totalMin / 60);
  const remMin = totalMin % 60;
  return `${hours}h ${remMin.toString().padStart(2, "0")}m`;
}

type BackgroundTasksPopoverContentProps = {
  tasks: BackgroundTask[];
  onClearCompleted?: () => void;
};

export function BackgroundTasksPopoverContent({
  tasks,
  onClearCompleted,
}: BackgroundTasksPopoverContentProps) {
  const { t } = useTranslation();

  const [now, setNow] = React.useState(() => Date.now());
  const hasLiveElapsed = tasks.some(
    (task) => task.status === "running" && task.startedAt != null,
  );
  React.useEffect(() => {
    if (!hasLiveElapsed) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [hasLiveElapsed]);

  const runningCount = tasks.filter((tk) => tk.status === "running").length;
  const hasCompleted = tasks.some(
    (tk) => tk.status === "completed" || tk.status === "failed",
  );

  return (
    <div className="flex min-w-[260px] max-w-[400px] flex-col gap-2">
      <div className="flex items-center gap-2">
        <span className="text-xs font-semibold text-foreground">
          {t("chatPanel.backgroundTasks.title")}
        </span>
        {runningCount > 0 && (
          <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-1.5 py-0.5 font-mono text-[10px] font-medium text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-400">
            <span
              className="inline-block size-1.5 rounded-full bg-emerald-500"
              aria-hidden="true"
            />
            {t("chatPanel.backgroundTasks.chip", { count: runningCount })}
          </span>
        )}
        <span className="min-w-0 flex-1" />
        {hasCompleted && onClearCompleted && (
          <button
            type="button"
            onClick={onClearCompleted}
            className="shrink-0 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
          >
            {t("chatPanel.backgroundTasks.clearCompleted")}
          </button>
        )}
      </div>
      {tasks.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          {t("chatPanel.backgroundTasks.empty")}
        </p>
      ) : (
        <ul className="flex flex-col gap-1.5">
          {tasks.map((task) => {
            let elapsedLabel: string | undefined;
            if (task.status === "running" && task.startedAt != null) {
              elapsedLabel = formatElapsed(now - task.startedAt);
            } else if (
              (task.status === "completed" || task.status === "failed") &&
              task.durationMs != null &&
              task.durationMs > 0
            ) {
              elapsedLabel = formatElapsed(task.durationMs);
            }

            return (
              <li key={task.toolUseId} className="flex items-center gap-2.5">
                <span
                  className="inline-flex size-6 shrink-0 items-center justify-center rounded-md bg-sky-50 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300"
                  aria-hidden="true"
                >
                  <Terminal className="size-3.5" />
                </span>
                <div className="min-w-0 flex-1">
                  {/* description is dynamic agent output — do NOT pass through t() */}
                  <p className="break-words text-xs leading-snug text-foreground">
                    {task.description || " "}
                  </p>
                  <div className="mt-0.5 flex items-center gap-1.5">
                    <span className="font-mono text-[10px] text-muted-foreground">
                      {t("chatPanel.backgroundTasks.bash")}
                    </span>
                    {task.taskId && (
                      <>
                        <span className="font-mono text-[10px] text-muted-foreground/50">
                          ·
                        </span>
                        <span
                          className="font-mono text-[10px] text-muted-foreground"
                          data-testid="task-id"
                        >
                          {task.taskId}
                        </span>
                      </>
                    )}
                    {elapsedLabel != null && (
                      <>
                        <span className="font-mono text-[10px] text-muted-foreground/50">
                          ·
                        </span>
                        <span
                          className="font-mono text-[10px] tabular-nums text-muted-foreground"
                          data-testid="elapsed"
                        >
                          {elapsedLabel}
                        </span>
                      </>
                    )}
                  </div>
                  {/* summary is dynamic agent text (exit-code text) — do NOT pass through t() */}
                  {task.summary && (
                    <p className="mt-0.5 break-words text-[10px] text-muted-foreground">
                      {task.summary}
                    </p>
                  )}
                </div>
                <StatusPill status={task.status} />
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

function StatusPill({ status }: { status: BackgroundTask["status"] }) {
  const { t } = useTranslation();

  if (status === "running") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-1.5 py-0.5 font-mono text-[10px] text-green-700 dark:bg-emerald-500/15 dark:text-green-400">
        <span
          className="inline-block size-1.5 rounded-full bg-green-500"
          aria-hidden="true"
        />
        {t("chatPanel.backgroundTasks.running")}
      </span>
    );
  }
  if (status === "failed") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-destructive/10 px-1.5 py-0.5 font-mono text-[10px] text-destructive">
        <span
          className="inline-block size-1.5 rounded-full bg-destructive"
          aria-hidden="true"
        />
        {t("chatPanel.backgroundTasks.failed")}
      </span>
    );
  }
  // completed
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
      <span
        className="inline-block size-1.5 rounded-full bg-muted-foreground"
        aria-hidden="true"
      />
      {t("chatPanel.backgroundTasks.completed")}
    </span>
  );
}
