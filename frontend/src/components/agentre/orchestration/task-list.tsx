import * as React from "react";
import { useTranslation } from "react-i18next";
import { Check, Circle, Loader } from "lucide-react";
import { cn } from "@/lib/utils";
import { useChatAgents } from "@/hooks/use-chat-agents";
import {
  agentColorClassNames,
  type AgentColor,
} from "@/components/agentre/types";
import type { app } from "../../../../wailsjs/go/models";

function StatusIcon({ status }: { status: string }) {
  if (status === "done")
    return (
      <Check
        data-testid={`task-status-${status}`}
        className="size-3.5 shrink-0 text-status-running"
        strokeWidth={2.5}
      />
    );
  if (status === "in_progress")
    return (
      <Loader
        data-testid={`task-status-${status}`}
        className="size-3.5 shrink-0 text-status-running motion-safe:animate-spin"
        strokeWidth={2.5}
      />
    );
  return (
    <Circle
      data-testid={`task-status-${status}`}
      className="size-3.5 shrink-0 text-status-idle"
      strokeWidth={2.5}
    />
  );
}

export function TaskList({ detail }: { detail: app.RunDetailDTO }) {
  const { t } = useTranslation();
  const { agents } = useChatAgents();
  const tasks = React.useMemo(() => detail.tasks ?? [], [detail.tasks]);
  const doneCount = React.useMemo(
    () => tasks.filter((tk) => tk.status === "done").length,
    [tasks],
  );

  // assigneeAgentId → 认领人名字 + 颜色（0 = 未认领,不显示徽标）。
  const agentInfoMap = React.useMemo(() => {
    const m = new Map<number, { name: string; color: AgentColor }>();
    for (const a of agents) {
      m.set(a.id, {
        name: a.name,
        color: (a.avatarColor as AgentColor) || "agent-1",
      });
    }
    return m;
  }, [agents]);

  return (
    <div
      data-testid="task-list"
      className="flex h-full flex-col bg-sidebar"
      data-selectable-text="true"
    >
      <div className="flex shrink-0 items-center gap-2 border-b border-border bg-card px-[14px] py-3">
        <span className="font-sans text-[13px] font-semibold text-foreground">
          {t("orchestration.tasks.title")}
        </span>
        <span className="flex-1" />
        <span
          data-testid="task-list-progress"
          className="font-mono text-[11px] text-muted-foreground tabular-nums"
        >
          {t("orchestration.tasks.progress", {
            done: doneCount,
            total: tasks.length,
          })}
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {tasks.length === 0 ? (
          <p
            data-testid="task-list-empty"
            className="p-3 text-center text-xs text-muted-foreground"
          >
            {t("orchestration.tasks.empty")}
          </p>
        ) : (
          <ul className="flex flex-col gap-0.5 p-2">
            {tasks.map((task) => (
              <li
                key={task.id}
                data-testid={`task-row-${task.id}`}
                className="flex items-center gap-2 rounded-md py-[7px] pl-[14px] pr-2"
              >
                <StatusIcon status={task.status} />
                <span
                  className={cn(
                    "min-w-0 flex-1 truncate text-[12.5px]",
                    task.status === "done"
                      ? "text-muted-foreground line-through"
                      : "text-foreground",
                  )}
                >
                  {task.text}
                </span>
                {task.assigneeAgentId !== 0 &&
                  (() => {
                    const info = agentInfoMap.get(task.assigneeAgentId);
                    return (
                      <span
                        data-testid={`task-assignee-${task.id}`}
                        className="flex shrink-0 items-center gap-1 text-[11px] text-muted-foreground"
                      >
                        <span
                          className={cn(
                            "size-1.5 shrink-0 rounded-full",
                            agentColorClassNames[info?.color ?? "agent-1"],
                          )}
                        />
                        <span className="max-w-[80px] truncate">
                          {info?.name ?? `#${task.assigneeAgentId}`}
                        </span>
                      </span>
                    );
                  })()}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
