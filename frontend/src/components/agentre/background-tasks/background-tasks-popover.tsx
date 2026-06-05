import { Bot, Terminal } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { BackgroundTask } from "./types";

type BackgroundTasksPopoverContentProps = {
  tasks: BackgroundTask[];
};

export function BackgroundTasksPopoverContent({
  tasks,
}: BackgroundTasksPopoverContentProps) {
  const { t } = useTranslation();

  return (
    <div className="flex min-w-[260px] max-w-[400px] flex-col gap-2">
      <div className="text-xs font-semibold text-foreground">
        {t("chatPanel.backgroundTasks.title")}
      </div>
      {tasks.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          {t("chatPanel.backgroundTasks.empty")}
        </p>
      ) : (
        <ul className="flex flex-col gap-1.5">
          {tasks.map((task) => (
            <li key={task.toolUseId} className="flex items-start gap-2">
              <span className="mt-0.5 shrink-0 text-muted-foreground">
                {task.kind === "local_bash" ? (
                  <Terminal className="size-3.5" aria-hidden="true" />
                ) : (
                  <Bot className="size-3.5" aria-hidden="true" />
                )}
              </span>
              <div className="min-w-0 flex-1">
                {/* description is dynamic agent output — do NOT pass through t() */}
                <p className="break-words text-xs leading-snug text-foreground">
                  {task.description || " "}
                </p>
                <div className="mt-0.5 flex items-center gap-1.5">
                  <span className="font-mono text-[10px] text-muted-foreground">
                    {task.kind === "local_bash"
                      ? t("chatPanel.backgroundTasks.bash")
                      : t("chatPanel.backgroundTasks.subagent")}
                  </span>
                  <StatusPill status={task.status} />
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function StatusPill({ status }: { status: BackgroundTask["status"] }) {
  const { t } = useTranslation();

  if (status === "running") {
    return (
      <span className="inline-flex items-center gap-1 font-mono text-[10px] text-green-600 dark:text-green-400">
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
      <span className="inline-flex items-center gap-1 font-mono text-[10px] text-destructive">
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
    <span className="inline-flex items-center gap-1 font-mono text-[10px] text-muted-foreground">
      <span
        className="inline-block size-1.5 rounded-full bg-muted-foreground"
        aria-hidden="true"
      />
      {t("chatPanel.backgroundTasks.completed")}
    </span>
  );
}
