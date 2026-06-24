import * as React from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { useChatAgents } from "@/hooks/use-chat-agents";
import type { app } from "../../../../wailsjs/go/models";

// 任务状态 → 可读文字 key
function statusKey(status: string): string {
  switch (status) {
    case "running":
      return "orchestration.board.statusRunning";
    case "done":
      return "orchestration.board.statusDone";
    case "awaiting-user":
      return "orchestration.board.statusAwaitingUser";
    case "awaiting-children":
      return "orchestration.board.statusAwaitingChildren";
    case "error":
      return "orchestration.board.statusError";
    default:
      return "orchestration.board.statusPending";
  }
}

// 任务状态对应的颜色 class
function statusDotClass(status: string): string {
  switch (status) {
    case "running":
      return "bg-status-running";
    case "awaiting-user":
    case "awaiting-children":
      return "bg-status-waiting";
    case "done":
      return "bg-muted-foreground/40";
    case "error":
      return "bg-destructive";
    default:
      return "bg-muted-foreground/20";
  }
}

// 节点钻入面板：展示选中 agent 的任务详情
function DrilldownPanel({
  tasks,
  agentName,
}: {
  tasks: app.TaskDTO[];
  agentName: string;
}) {
  const { t } = useTranslation();
  return (
    <div
      data-testid="board-drilldown"
      className="flex flex-col gap-3 border-b border-border p-3"
    >
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-foreground">{agentName}</span>
        <span className="text-xs text-muted-foreground">
          {t("orchestration.board.drilldownTitle")}
        </span>
      </div>
      {/* TODO(plan-1b): 嵌入该会话 ChatPanel 只读 transcript + 对它说 */}
      <div className="flex flex-col gap-2">
        {tasks.map((task) => (
          <div
            key={task.id}
            className="rounded-md border border-border bg-card p-2 text-xs"
          >
            {/* 任务 brief（动态内容不走 t()） */}
            <div className="mb-1 font-medium text-foreground">
              {task.brief || `#${task.id}`}
            </div>
            <div className="flex items-center gap-1.5">
              <span
                className={cn(
                  "h-1.5 w-1.5 rounded-full",
                  statusDotClass(task.status),
                )}
              />
              <span className="text-muted-foreground">
                {t(statusKey(task.status))}
              </span>
            </div>
            {/* 结果（动态内容不走 t()） */}
            {task.result && (
              <div className="mt-1 truncate text-muted-foreground">
                {task.result}
              </div>
            )}
          </div>
        ))}
        {tasks.length === 0 && (
          <p className="text-center text-xs text-muted-foreground">
            {t("orchestration.board.drilldownEmpty")}
          </p>
        )}
      </div>
    </div>
  );
}

export function TaskBoard({
  detail,
  selectedAgentId,
  onSelectTask,
}: {
  detail: app.RunDetailDTO;
  selectedAgentId: number | null;
  onSelectTask: (agentId: number) => void;
}) {
  const { t } = useTranslation();
  const { agents } = useChatAgents();
  const [tab, setTab] = React.useState<"tasks" | "outputs">("tasks");

  const tasks = React.useMemo(() => detail.tasks ?? [], [detail.tasks]);

  // agentId → 名称查找表
  const agentNameMap = React.useMemo(() => {
    const m = new Map<number, string>();
    for (const a of agents) {
      m.set(a.id, a.name);
    }
    return m;
  }, [agents]);

  // 选中 agent 的所有任务（用于钻入面板）
  const selectedAgentTasks = React.useMemo(() => {
    if (selectedAgentId === null) return [];
    return tasks.filter((t) => t.agentId === selectedAgentId);
  }, [tasks, selectedAgentId]);

  // 选中 agent 的名称
  const selectedAgentName =
    selectedAgentId !== null
      ? (agentNameMap.get(selectedAgentId) ?? `#${selectedAgentId}`)
      : "";

  return (
    <div className="flex h-full flex-col">
      {/* Tab 分段控件 */}
      <div className="flex shrink-0 items-center gap-1 border-b border-border p-2">
        <Button
          data-testid="board-tab-tasks"
          variant={tab === "tasks" ? "default" : "ghost"}
          size="sm"
          className="h-7 flex-1 px-2 text-xs"
          onClick={() => setTab("tasks")}
        >
          {t("orchestration.board.tabTasks")}
        </Button>
        <Button
          data-testid="board-tab-outputs"
          variant={tab === "outputs" ? "default" : "ghost"}
          size="sm"
          className="h-7 flex-1 px-2 text-xs"
          onClick={() => setTab("outputs")}
        >
          {t("orchestration.board.tabOutputs")}
        </Button>
      </div>

      {/* 内容区 */}
      <div className="min-h-0 flex-1 overflow-y-auto">
        {tab === "tasks" ? (
          <>
            {/* 节点钻入面板（选中 agent 时显示） */}
            {selectedAgentId !== null && (
              <DrilldownPanel
                tasks={selectedAgentTasks}
                agentName={selectedAgentName}
              />
            )}

            {/* 任务清单 */}
            {tasks.length === 0 ? (
              <p className="p-3 text-center text-xs text-muted-foreground">
                {t("orchestration.board.tasksEmpty")}
              </p>
            ) : (
              <ul className="flex flex-col gap-0">
                {tasks.map((task, index) => {
                  const isChild = task.parentTaskId !== 0;
                  const agentName =
                    agentNameMap.get(task.agentId) ?? `#${task.agentId}`;
                  const isSelected = task.agentId === selectedAgentId;
                  const seq = task.callSeq > 0 ? task.callSeq : index + 1;
                  return (
                    <li key={task.id}>
                      <button
                        type="button"
                        data-testid={`board-task-${task.id}`}
                        onClick={() => onSelectTask(task.agentId)}
                        className={cn(
                          "flex w-full items-center gap-2 px-3 py-2 text-left text-xs transition-colors hover:bg-muted/50",
                          isChild && "pl-6",
                          isSelected && "bg-muted",
                        )}
                      >
                        {/* 序号 */}
                        <span className="shrink-0 text-muted-foreground/60">
                          #{seq}
                        </span>
                        {/* 状态点 */}
                        <span
                          className={cn(
                            "h-1.5 w-1.5 shrink-0 rounded-full",
                            statusDotClass(task.status),
                          )}
                        />
                        {/* Agent 名称 */}
                        <span className="shrink-0 font-medium text-foreground">
                          {agentName}
                        </span>
                        {/* 任务 brief（动态内容不走 t()） */}
                        {task.brief && (
                          <span className="min-w-0 flex-1 truncate text-muted-foreground">
                            {task.brief}
                          </span>
                        )}
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
          </>
        ) : (
          // 产出物 tab
          <div data-testid="board-outputs" className="flex flex-col gap-2 p-3">
            {/* TODO(plan-1b): 结构化展开 Refs */}
            {tasks.filter((t) => t.refs && t.refs.trim()).length === 0 ? (
              <p className="text-center text-xs text-muted-foreground">
                {t("orchestration.board.outputsEmpty")}
              </p>
            ) : (
              tasks
                .filter((t) => t.refs && t.refs.trim())
                .map((task) => (
                  <div
                    key={task.id}
                    className="rounded-md border border-border bg-card p-2 text-xs text-muted-foreground"
                  >
                    {/* refs 是 JSON 字符串（动态内容不走 t()） */}
                    {task.refs}
                  </div>
                ))
            )}
          </div>
        )}
      </div>
    </div>
  );
}
