import { ClipboardList, CornerDownRight } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import { relativeTime } from "@/lib/relative-time";
import { cn } from "@/lib/utils";

import { MentionChip } from "./mention-text";

import type { app } from "../../../../wailsjs/go/models";

type GroupTaskItem = app.GroupTaskItem;

// 任务状态 → i18n key。未知值兜底 open,绝不让 t() 落空(与 RUN_STATUS_KEY 同手法)。
const STATUS_KEY: Record<string, string> = {
  open: "open",
  done: "done",
  canceled: "canceled",
};

// 状态 pill 三态配色(spec §8:进行中=amber/已完成=green/已取消=gray),暗色靠
// dark: 变体(设计稿「Group 任务卡组件 — Dark」帧)。
const STATUS_PILL_CLASS: Record<string, string> = {
  open: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  done: "bg-green-500/15 text-green-600 dark:text-green-400",
  canceled: "bg-muted text-muted-foreground",
};

export type GroupTaskCardProps = {
  /** 实时任务实体(store 的 tasks,状态随 task_updated 翻转),不是消息快照。 */
  task: GroupTaskItem;
  /** 该消息的任务事件(created/completed/canceled),决定卡体展示 brief 还是 result。 */
  taskEvent: string;
  /** 所属消息 id:卡片自带 data-message-id 供任务 tab/回指链接锚定。 */
  messageId: number;
  /** member id → 显示名(动态内容,由父层解析,不进 t())。 */
  memberName: (memberId: number) => string;
  /** @chip 点击跳成员 backing session(复用 mention chip 行为)。 */
  onJumpMember: (memberId: number) => void;
  /** 「↳ 验证 #N」回指点击,锚定父任务的派活卡。 */
  onJumpTaskNo?: (taskNo: number) => void;
};

// GroupTaskCard:任务事件消息的卡片体(taskEvent != "" 的 group_message)。纯展示,
// 不取数据 —— 任务实体/跳转都由父层注入。created/canceled 展示 brief 指向 assignee,
// completed 展示交付物 result 指向 creator(任务回到建卡人手里,spec §5)。
function GroupTaskCard({
  task,
  taskEvent,
  messageId,
  memberName,
  onJumpMember,
  onJumpTaskNo,
}: GroupTaskCardProps) {
  const { t } = useTranslation();
  const statusKey = STATUS_KEY[task.status] ?? "open";
  const isCompleted = taskEvent === "completed";
  const body = isCompleted ? task.result : task.brief;
  const targetMemberId = isCompleted
    ? task.creatorMemberID
    : task.assigneeMemberID;

  return (
    <div
      data-message-id={messageId}
      data-testid="group-task-card"
      className="min-w-64 max-w-md flex-1 basis-72 rounded-lg border border-border bg-card"
    >
      <div className="flex items-center gap-2 border-b border-border px-3 py-2">
        <ClipboardList
          className="size-3.5 shrink-0 text-muted-foreground"
          aria-hidden="true"
        />
        <span className="shrink-0 font-mono text-xs text-muted-foreground">
          #{task.taskNo}
        </span>
        {/* 标题是动态内容,原样渲染不进 t()。 */}
        <span
          className="min-w-0 flex-1 truncate text-sm font-medium"
          title={task.title}
        >
          {task.title}
        </span>
        <Badge
          variant="secondary"
          className={cn("shrink-0", STATUS_PILL_CLASS[statusKey])}
        >
          {t(`group.task.status.${statusKey}`)}
        </Badge>
      </div>
      <div className="flex flex-col gap-1.5 px-3 py-2">
        {task.parentTaskNo > 0 ? (
          <button
            type="button"
            onClick={() => onJumpTaskNo?.(task.parentTaskNo)}
            className="flex w-fit items-center gap-1 text-xs text-primary hover:underline"
          >
            <CornerDownRight className="size-3" aria-hidden="true" />
            {t("group.task.verifies", { no: task.parentTaskNo })}
          </button>
        ) : null}
        {body ? (
          <div className="whitespace-pre-wrap break-words text-sm leading-relaxed text-foreground">
            {body}
          </div>
        ) : null}
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span>
            {t(
              isCompleted ? "group.task.deliveredTo" : "group.task.assignedTo",
            )}
          </span>
          <MentionChip
            memberId={targetMemberId}
            name={memberName(targetMemberId)}
            onJump={onJumpMember}
          />
        </div>
        <div className="text-2xs text-muted-foreground">
          {memberName(task.creatorMemberID)} · {relativeTime(task.createtime)}
        </div>
      </div>
    </div>
  );
}

export { GroupTaskCard };
