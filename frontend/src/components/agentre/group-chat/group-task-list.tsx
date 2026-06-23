import { Check, ChevronRight, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { relativeTime } from "@/lib/relative-time";

import { AgentAvatar } from "../primitives";

import { agentColorForMember } from "./group-transcript";

import type { app } from "../../../../wailsjs/go/models";

type GroupTaskItem = app.GroupTaskItem;

export type GroupTaskListProps = {
  tasks: GroupTaskItem[];
  /** member id → 显示名(动态内容,父层解析,不进 t())。 */
  memberName: (memberId: number) => string;
  /** 点行:transcript 锚定到该任务的派活卡。 */
  onAnchorTask: (task: GroupTaskItem) => void;
  /** 行尾 ›:跳 assignee 的成员会话。 */
  onOpenMember: (memberId: number) => void;
};

// 状态图标:open=amber 点(进行中),done=绿勾,canceled=灰叉 —— 与卡片 pill 同色系。
function statusIcon(status: string) {
  if (status === "done") {
    return (
      <Check
        className="size-3.5 shrink-0 text-green-600 dark:text-green-400"
        aria-hidden="true"
      />
    );
  }
  if (status === "canceled") {
    return (
      <X
        className="size-3.5 shrink-0 text-muted-foreground"
        aria-hidden="true"
      />
    );
  }
  return (
    <span
      aria-hidden="true"
      className="mx-1 size-1.5 shrink-0 rounded-full bg-amber-500"
    />
  );
}

function TaskRow({
  task,
  memberName,
  onAnchorTask,
  onOpenMember,
}: {
  task: GroupTaskItem;
  memberName: (memberId: number) => string;
  onAnchorTask: (task: GroupTaskItem) => void;
  onOpenMember: (memberId: number) => void;
}) {
  const { t } = useTranslation();
  const assigneeName = memberName(task.assigneeMemberID);
  // 主体与行尾是两个并排 button(交互元素不嵌套):点主体锚定,点 › 跳会话。
  return (
    <div className="flex items-center gap-1">
      <button
        type="button"
        data-testid={`group-task-row-${task.taskNo}`}
        data-status={task.status}
        onClick={() => onAnchorTask(task)}
        className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-accent"
      >
        {statusIcon(task.status)}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="shrink-0 font-mono text-2xs text-muted-foreground">
              #{task.taskNo}
            </span>
            {/* 标题/成员名是动态内容,原样渲染。 */}
            <span className="min-w-0 flex-1 truncate text-sm text-foreground">
              {task.title}
            </span>
          </div>
          <div className="truncate text-2xs text-muted-foreground">
            {assigneeName}
            {task.parentTaskNo > 0 ? ` · ↳#${task.parentTaskNo}` : ""}
            {` · ${relativeTime(task.createtime)}`}
          </div>
        </div>
        <AgentAvatar
          name={assigneeName}
          color={agentColorForMember(task.assigneeMemberID)}
          size="sm"
        />
      </button>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label={`${t("group.task.openAssignee")} ${assigneeName}`}
        onClick={() => onOpenMember(task.assigneeMemberID)}
      >
        <ChevronRight data-icon="only" aria-hidden="true" />
      </Button>
    </div>
  );
}

// GroupTaskList:roster「任务」tab 的内容。「进行中」置顶、已结束分组在下,
// 组内按 #N 升序(与 transcript 卡片故事线一致)。
function GroupTaskList({
  tasks,
  memberName,
  onAnchorTask,
  onOpenMember,
}: GroupTaskListProps) {
  const { t } = useTranslation();
  const open = tasks
    .filter((tk) => tk.status === "open")
    .sort((a, b) => a.taskNo - b.taskNo);
  const closed = tasks
    .filter((tk) => tk.status !== "open")
    .sort((a, b) => a.taskNo - b.taskNo);

  if (tasks.length === 0) {
    return (
      <div className="p-4 text-center text-xs text-muted-foreground">
        {t("group.task.empty")}
      </div>
    );
  }

  return (
    <div
      className="flex min-h-0 flex-1 flex-col overflow-auto p-2"
      data-testid="group-task-list"
    >
      {open.length > 0 ? (
        <>
          <div className="px-2 pb-1 pt-2 text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
            {t("group.task.sectionOpen")}
          </div>
          {open.map((tk) => (
            <TaskRow
              key={tk.id}
              task={tk}
              memberName={memberName}
              onAnchorTask={onAnchorTask}
              onOpenMember={onOpenMember}
            />
          ))}
        </>
      ) : null}
      {closed.length > 0 ? (
        <>
          <div className="px-2 pb-1 pt-3 text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
            {t("group.task.sectionClosed")}
          </div>
          {closed.map((tk) => (
            <TaskRow
              key={tk.id}
              task={tk}
              memberName={memberName}
              onAnchorTask={onAnchorTask}
              onOpenMember={onOpenMember}
            />
          ))}
        </>
      ) : null}
    </div>
  );
}

export { GroupTaskList };
