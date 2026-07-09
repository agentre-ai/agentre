import * as React from "react";
import { useTranslation } from "react-i18next";
import { Check, ChevronRight, Circle, GitMerge, Loader } from "lucide-react";
import { cn } from "@/lib/utils";
import { useChatAgents } from "@/hooks/use-chat-agents";
import {
  agentColorClassNames,
  type AgentColor,
} from "@/components/agentre/types";
import type { app } from "../../../../wailsjs/go/models";
import { useRunSubagents } from "./use-run-subagents";

// 任务状态 → lucide 图标 + 颜色 class
function StatusIcon({ status, testId }: { status: string; testId: string }) {
  if (status === "done") {
    return (
      <Check
        data-testid={testId}
        className="size-3.5 shrink-0 text-status-running"
        strokeWidth={2.5}
      />
    );
  }
  if (status === "running") {
    return (
      <Loader
        data-testid={testId}
        className="size-3.5 shrink-0 text-status-running motion-safe:animate-spin"
        strokeWidth={2.5}
      />
    );
  }
  // idle / pending / awaiting-children / awaiting-user / error / default
  return (
    <Circle
      data-testid={testId}
      className="size-3.5 shrink-0 text-status-idle"
      strokeWidth={2.5}
    />
  );
}

export function TaskBoard({
  detail,
  selectedSessionId,
  onSelectSession,
}: {
  detail: app.RunDetailDTO;
  selectedSessionId: number | null;
  onSelectSession: (sessionId: number) => void;
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

  // agentId → agent 名称 + 颜色
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

  const renderTaskRow = (task: app.TaskDTO, indented: boolean) => {
    const isChild = task.parentTaskId !== 0;
    const seq = task.callSeq > 0 ? task.callSeq : task.id;
    const subs = subagents.forSession(task.sessionId);
    const open = expandedSub.has(task.id);
    const isSelected = task.sessionId === selectedSessionId;

    // Subagent group info from the agents map (if a subagent tool_use has an agentId)
    // For task rows we don't need agent info (name is shown in group header)

    return (
      <li key={task.id}>
        <button
          type="button"
          data-testid={`board-task-${task.id}`}
          onClick={() => onSelectSession(task.sessionId)}
          className={cn(
            "flex w-full items-center gap-2 rounded-md py-[7px] pr-2 text-left transition-colors hover:bg-secondary/70",
            // Base left padding matches design padding=[7,8,7,14] → pl-[14px]
            // Non-indented child row (parentTaskId != 0 but single agent) → pl-6
            // Indented (multi-agent group) → pl-8
            !indented && isChild ? "pl-6" : !indented ? "pl-[14px]" : "pl-8",
            isSelected && "bg-secondary",
          )}
        >
          <StatusIcon
            status={task.status}
            testId={`board-task-${task.id}-status`}
          />
          <span className="shrink-0 font-mono text-[10px] text-subtle-foreground">
            #{seq}
          </span>
          {task.brief && (
            <span className="min-w-0 flex-1 truncate text-[12.5px] text-foreground">
              {task.brief}
            </span>
          )}
        </button>

        {/* Subagent rows */}
        {subs.length > 0 && (
          <div className={cn(indented ? "pl-12" : "pl-8")}>
            <button
              type="button"
              data-testid={`board-subagents-${task.id}`}
              onClick={() => toggleSub(task.id)}
              aria-expanded={open}
              className={cn(
                "flex w-full items-center gap-2 rounded-md py-[6px] pr-2 pl-[38px] text-left transition-colors hover:bg-secondary/50",
                "font-mono text-[11px] text-muted-foreground",
              )}
            >
              <ChevronRight
                className={cn(
                  "size-3 shrink-0 text-subtle-foreground transition-transform",
                  open && "rotate-90",
                )}
              />
              <GitMerge className="size-3 shrink-0 text-muted-foreground" />
              <span>
                {t("orchestration.subagent.badge", {
                  count: subs.length,
                })}
              </span>
              <span className="rounded-full bg-secondary px-[5px] py-[1px] font-mono text-[9px] text-subtle-foreground">
                {t("orchestration.subagent.autoMerge")}
              </span>
            </button>
            {open && (
              <ul className="flex flex-col gap-0.5 pb-1">
                {subs.map((sa, i) => (
                  <li
                    key={sa.toolUseId}
                    data-testid={`board-subagent-${task.id}-${i}`}
                    className="flex items-center gap-2 rounded-md py-[6px] pr-2 pl-[30px] text-[12px] text-muted-foreground"
                  >
                    <span className="truncate">
                      {sa.role || sa.description || sa.toolUseId}
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

  return (
    <div className="flex h-full flex-col bg-sidebar">
      {/* 头部: tbHead */}
      <div className="flex shrink-0 flex-col gap-2.5 border-b border-border bg-card px-[14px] py-3">
        {/* hr 行: 标题 + 间距 + 计数 */}
        <div className="flex items-center gap-2">
          <span
            data-testid="board-head-title"
            className="font-sans text-[13px] font-semibold text-foreground"
          >
            {t("orchestration.board.title")}
          </span>
          <span className="flex-1" />
          <span
            data-testid="board-progress"
            className="font-mono text-[11px] text-muted-foreground tabular-nums"
          >
            {t("orchestration.board.progress", {
              done: doneCount,
              total: tasks.length,
            })}
          </span>
        </div>

        {/* tabs: bg-secondary rounded-lg p-0.5 */}
        <div className="flex items-center gap-0.5 rounded-lg bg-secondary p-0.5">
          <button
            type="button"
            data-testid="board-tab-tasks"
            onClick={() => setTab("tasks")}
            className={cn(
              "flex-1 rounded-md px-3 py-1 text-[12px] font-normal transition-colors",
              tab === "tasks"
                ? "border border-border bg-card font-semibold text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {t("orchestration.board.tabTasks")}
          </button>
          <button
            type="button"
            data-testid="board-tab-outputs"
            onClick={() => setTab("outputs")}
            className={cn(
              "flex-1 rounded-md px-3 py-1 text-[12px] font-normal transition-colors",
              tab === "outputs"
                ? "border border-border bg-card text-foreground font-semibold"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {t("orchestration.board.tabOutputs")}
          </button>
        </div>
      </div>

      {/* 内容区: taskList */}
      <div className="min-h-0 flex-1 overflow-y-auto">
        {tab === "tasks" ? (
          <>
            {tasks.length === 0 ? (
              <p className="p-3 text-center text-xs text-muted-foreground">
                {t("orchestration.board.tasksEmpty")}
              </p>
            ) : (
              <ul className="flex flex-col gap-0.5 p-2">
                {agentGroups.map((group) => {
                  const info = agentInfoMap.get(group.agentId);
                  const agentName = info?.name ?? `#${group.agentId}`;
                  const agentColor = info?.color ?? "agent-1";
                  const multi = group.tasks.length >= 2;

                  if (!multi) {
                    return renderTaskRow(group.tasks[0], false);
                  }

                  return (
                    <li key={`agent-${group.agentId}`}>
                      {/* Agent group header */}
                      <div
                        data-testid={`board-agent-${group.agentId}`}
                        className="flex items-center gap-1.5 px-2 pb-1 pt-2 text-[11px] font-semibold text-muted-foreground"
                      >
                        <span
                          className={cn(
                            "size-1.5 shrink-0 rounded-full",
                            agentColorClassNames[agentColor],
                          )}
                        />
                        <span>{agentName}</span>
                        <span className="font-normal text-subtle-foreground">
                          {t("orchestration.graph.sessionsCount", {
                            count: group.tasks.length,
                          })}
                        </span>
                      </div>
                      <ul className="flex flex-col gap-0.5">
                        {group.tasks.map((task) => renderTaskRow(task, true))}
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
                    data-selectable-text="true"
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
