import { ChevronDown } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

import { BackgroundTasksPopoverContent } from "./background-tasks-popover";
import type { BackgroundTask } from "./types";

type BackgroundTasksChipProps = {
  tasks: BackgroundTask[];
};

export function BackgroundTasksChip({ tasks }: BackgroundTasksChipProps) {
  const { t } = useTranslation();

  const runningCount = tasks.filter((task) => task.status === "running").length;

  // Hidden when no running tasks
  if (runningCount === 0) return null;

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
          <span
            className="inline-block size-2 rounded-full bg-green-500"
            aria-hidden="true"
          />
          {t("chatPanel.backgroundTasks.chip", { count: runningCount })}
          <ChevronDown className="size-3 opacity-60" aria-hidden="true" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="p-3">
        <BackgroundTasksPopoverContent tasks={tasks} />
      </PopoverContent>
    </Popover>
  );
}
