import * as React from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { useChatAgents } from "@/hooks/use-chat-agents";
import type { app } from "../../../../wailsjs/go/models";
import { useRunSubagents } from "./use-run-subagents";

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
  const subagents = useRunSubagents(detail);
  const [expandedSub, setExpandedSub] = React.useState<Set<number>>(
    () => new Set(),
  );
  const toggleSub = (taskId: number) =>
    setExpandedSub((prev) => {
      const next = new Set(prev);
      if (next.has(taskId)) {
        next.delete(taskId);
      } else {
        next.add(taskId);
      }
      return next;
    });

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
    return tasks.filter((task) => task.agentId === selectedAgentId);
  }, [tasks, selectedAgentId]);

  // 选中 agent 的名称
  const selectedAgentName =
    selectedAgentId !== null
      ? (agentNameMap.get(selectedAgentId) ?? `#${selectedAgentId}`)
      : "";

  const doneCount = React.useMemo(
    () => tasks.filter((tk) => tk.status === "done").length,
    [tasks],
  );

  // 按 agent 分组,保留 task 首次出现顺序;agent 内按 callSeq 升序。
  const agentGroups = React.useMemo(() => {
    const order: number[] = [];
    const byAgent = new Map<number, app.TaskDTO[]>();
    for (const tk of tasks) {
      if (!byAgent.has(tk.agentId)) {
        byAgent.set(tk.agentId, []);
        order.push(tk.agentId);
      }
      byAgent.get(tk.agentId)!.push(tk);
    }
    return order.map((agentId) => ({
      agentId,
      tasks: byAgent
        .get(agentId)!
        .slice()
        .sort((a, b) => a.callSeq - b.callSeq || a.id - b.id),
    }));
  }, [tasks]);

  return (
    <div className="flex h-full flex-col">
      {/* Tab 分段控件 */}
      <div className="flex shrink-0 items-center gap-1 border-b border-border p-2">
        <span
          data-testid="board-progress"
          className="mr-1 shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs font-medium text-muted-foreground tabular-nums"
        >
          {t("orchestration.board.progress", {
            done: doneCount,
            total: tasks.length,
          })}
        </span>
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
                {agentGroups.map((group) => {
                  const agentName =
                    agentNameMap.get(group.agentId) ?? `#${group.agentId}`;
                  const isSelected = group.agentId === selectedAgentId;
                  const multi = group.tasks.length >= 2;

                  const renderRow = (task: app.TaskDTO, indented: boolean) => {
                    const isChild = task.parentTaskId !== 0;
                    const seq = task.callSeq > 0 ? task.callSeq : task.id;
                    const subs = subagents.forSession(task.sessionId);
                    const open = expandedSub.has(task.id);
                    return (
                      <li key={task.id}>
                        <button
                          type="button"
                          data-testid={`board-task-${task.id}`}
                          onClick={() => onSelectTask(task.agentId)}
                          className={cn(
                            "flex w-full items-center gap-2 px-3 py-2 text-left text-xs transition-colors hover:bg-muted/50",
                            !indented && isChild && "pl-6",
                            indented && "pl-8",
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
                          {/* Agent 名称（缩进子行不重复显示） */}
                          {!indented && (
                            <span className="shrink-0 font-medium text-foreground">
                              {agentName}
                            </span>
                          )}
                          {/* 任务 brief（动态内容不走 t()） */}
                          {task.brief && (
                            <span className="min-w-0 flex-1 truncate text-muted-foreground">
                              {task.brief}
                            </span>
                          )}
                        </button>
                        {subs.length > 0 && (
                          <div className={cn(indented ? "pl-12" : "pl-8")}>
                            <button
                              type="button"
                              data-testid={`board-subagents-${task.id}`}
                              onClick={() => toggleSub(task.id)}
                              aria-expanded={open}
                              className="flex items-center gap-1 px-3 py-1 text-xs text-subtle-foreground hover:text-muted-foreground"
                            >
                              <span>{open ? "▾" : "▸"}</span>
                              <span>
                                {t("orchestration.subagent.badge", {
                                  count: subs.length,
                                })}
                              </span>
                              <span className="text-muted-foreground/60">
                                {"· "}
                                {t("orchestration.subagent.autoMerge")}
                              </span>
                            </button>
                            {open && (
                              <ul className="flex flex-col gap-0.5 pb-1">
                                {subs.map((sa, i) => (
                                  <li
                                    key={sa.toolUseId}
                                    data-testid={`board-subagent-${task.id}-${i}`}
                                    className="flex items-center gap-2 px-3 py-1 pl-8 text-xs text-muted-foreground"
                                  >
                                    <span className="truncate">
                                      {sa.role ||
                                        sa.description ||
                                        sa.toolUseId}
                                    </span>
                                  </li>
                                ))}
                              </ul>
                            )}
                          </div>
                        )}
                      </li>
                    );
                  };

                  if (!multi) {
                    return renderRow(group.tasks[0], false);
                  }
                  return (
                    <li key={`agent-${group.agentId}`}>
                      <div
                        data-testid={`board-agent-${group.agentId}`}
                        className="flex items-center gap-2 px-3 pt-2 pb-1 text-xs font-medium text-foreground"
                      >
                        <span>{agentName}</span>
                        <span className="text-muted-foreground">
                          {t("orchestration.graph.sessionsCount", {
                            count: group.tasks.length,
                          })}
                        </span>
                      </div>
                      <ul className="flex flex-col gap-0">
                        {group.tasks.map((task) => renderRow(task, true))}
                      </ul>
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
            {tasks.filter((task) => task.refs && task.refs.trim()).length ===
            0 ? (
              <p className="text-center text-xs text-muted-foreground">
                {t("orchestration.board.outputsEmpty")}
              </p>
            ) : (
              tasks
                .filter((task) => task.refs && task.refs.trim())
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
