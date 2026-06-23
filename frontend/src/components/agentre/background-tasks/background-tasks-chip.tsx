import { ChevronDown, Terminal } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

import { BackgroundTasksPopoverContent } from "./background-tasks-popover";
import type { BackgroundTask } from "./types";

type BackgroundTasksChipProps = {
  tasks: BackgroundTask[];
  onClearCompleted?: () => void;
};

export function BackgroundTasksChip({
  tasks,
  onClearCompleted,
}: BackgroundTasksChipProps) {
  const { t } = useTranslation();

  const runningCount = tasks.filter((task) => task.status === "running").length;
  const completedCount = tasks.filter(
    (task) => task.status === "completed" || task.status === "failed",
  ).length;

  // Hidden only when there is nothing to show at all.
  if (runningCount === 0 && completedCount === 0) return null;

  const isRunning = runningCount > 0;

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="sm"
          aria-label={t("chatPanel.backgroundTasks.aria")}
          className="gap-1.5"
        >
          <Terminal
            className="size-3 text-muted-foreground"
            aria-hidden="true"
          />
          <span
            className={cn(
              "inline-block size-2 rounded-full",
              isRunning ? "bg-green-500" : "bg-muted-foreground",
            )}
            aria-hidden="true"
          />
          {isRunning
            ? t("chatPanel.backgroundTasks.chip", { count: runningCount })
            : t("chatPanel.backgroundTasks.completedChip", {
                count: completedCount,
              })}
          <ChevronDown className="size-3 opacity-60" aria-hidden="true" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="p-3">
        <BackgroundTasksPopoverContent
          tasks={tasks}
          onClearCompleted={onClearCompleted}
        />
      </PopoverContent>
    </Popover>
  );
}
