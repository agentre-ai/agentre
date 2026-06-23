import * as React from "react";
import { ChevronRight, SquarePen, Trash2, UserPlus } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

import { AgentAvatar } from "../primitives";

import { GroupDeleteDialog } from "./group-delete-dialog";
import { GroupTaskList } from "./group-task-list";
import { agentColorForMember } from "./group-transcript";

import type { app } from "../../../../wailsjs/go/models";

type GroupMemberItem = app.GroupMemberItem;
type GroupTaskItem = app.GroupTaskItem;

type RosterTab = "members" | "tasks" | "settings";

export type GroupRosterProps = {
  members: GroupMemberItem[];
  memberName: (memberId: number) => string;
  /** 群绑定的项目 id（0 = 未绑定）。取代了原先未接线的「工作目录」展示。 */
  projectID?: number;
  /** 项目显示名是动态值，原样展示，不进 t()。projectID>0 但缺名时按未绑定兜底。 */
  projectName?: string;
  /** 点击项目名跳转到该项目（navigate(`/projects?focus=<id>`)）。 */
  onOpenProject?: (projectID: number) => void;
  onOpenMember: (member: GroupMemberItem) => void;
  onInvite: () => void;
  onDelete: (deleteSessions: boolean) => void;
  /** agentNameOf: 成员 → 其 Agent 全局名(群昵称编辑器展示「本名」/占位用,动态值不进 t())。 */
  agentNameOf?: (member: GroupMemberItem) => string;
  /** onSetMemberNickname: 设/清群昵称(空串=清除回落原名)。未提供则不渲染编辑入口。 */
  onSetMemberNickname?: (memberId: number, nickname: string) => void;
  /** 群任务卡(实时,LoadGroup + task_updated 驱动)。 */
  tasks: GroupTaskItem[];
  /** 任务行点击:transcript 锚定到该任务的派活卡。 */
  onAnchorTask: (task: GroupTaskItem) => void;
  /** 任务行尾 ›:按 member id 跳成员会话(复用 openMemberById)。 */
  onOpenMemberById: (memberId: number) => void;
};

// 状态点按运行态(running/idle)着色,而不是成员身份(active/left)。在跑→绿,
// 否则→灰(空串 / idle / 已离群都算不在跑)。这样 roster 才与该成员的实际状态一致。
const runStateDotClass: Record<string, string> = {
  running: "bg-status-running",
  idle: "bg-status-idle",
};

// MemberNicknamePopover 是 roster 行内的群昵称编辑器(悬停浮现 ✎)。本群有效名=昵称,
// 为空回落 Agent 全局名(realName);保存前在前端先做唯一性预校验(takenNames=同群其它成员的
// 有效名),与后端 GroupNicknameTaken 同口径——后端仍是权威守卫。
function MemberNicknamePopover({
  member,
  realName,
  takenNames,
  onSetNickname,
}: {
  member: GroupMemberItem;
  realName: string;
  takenNames: Set<string>;
  onSetNickname: (memberId: number, nickname: string) => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = React.useState(false);
  const [value, setValue] = React.useState(member.nickname ?? "");
  // 打开时同步当前昵称(成员可能被 live 事件更新),关闭后丢弃未保存草稿。
  React.useEffect(() => {
    if (open) setValue(member.nickname ?? "");
  }, [open, member.nickname]);

  const trimmed = value.trim();
  const collision = trimmed !== "" && takenNames.has(trimmed);
  const save = () => {
    if (collision) return;
    onSetNickname(member.id, trimmed);
    setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          data-testid="member-nickname-edit"
          aria-label={t("group.nickname.edit")}
          className="size-7 shrink-0 text-muted-foreground opacity-0 focus-visible:opacity-100 group-hover/row:opacity-100 data-[state=open]:opacity-100"
        >
          <SquarePen className="size-3.5" aria-hidden="true" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72">
        <div className="flex flex-col gap-3">
          <div className="text-sm font-semibold text-foreground">
            {t("group.nickname.title")}
          </div>
          {/* 身份行:Agent 全局名是动态值,原样渲染。 */}
          <div className="flex min-w-0 flex-col">
            <span className="truncate text-sm font-medium text-foreground">
              {realName}
            </span>
            <span className="text-2xs text-muted-foreground">
              {t("group.nickname.identity")}
            </span>
          </div>
          <Input
            data-testid="member-nickname-input"
            value={value}
            placeholder={realName}
            aria-invalid={collision}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") save();
            }}
          />
          {collision ? (
            <p
              data-testid="member-nickname-error"
              className="text-xs text-destructive"
            >
              {t("group.nickname.taken", { name: trimmed })}
            </p>
          ) : (
            <p className="text-xs text-muted-foreground">
              {t("group.nickname.hint", { name: realName })}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setOpen(false)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              type="button"
              size="sm"
              data-testid="member-nickname-save"
              disabled={collision}
              onClick={save}
            >
              {t("common.save")}
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function MemberRow({
  member,
  name,
  realName,
  takenNames,
  onOpen,
  onSetNickname,
}: {
  member: GroupMemberItem;
  name: string;
  realName: string;
  takenNames: Set<string>;
  onOpen: (member: GroupMemberItem) => void;
  onSetNickname?: (memberId: number, nickname: string) => void;
}) {
  const { t } = useTranslation();
  const canOpen = member.backingSessionID > 0;
  return (
    <div className="group/row flex items-center gap-1">
      <button
        type="button"
        onClick={() => onOpen(member)}
        disabled={!canOpen}
        className={cn(
          "flex min-w-0 flex-1 items-center gap-2.5 rounded-md px-2 py-1.5 text-left hover:bg-accent",
          !canOpen && "cursor-default opacity-70 hover:bg-transparent",
        )}
      >
        <AgentAvatar
          name={name}
          color={agentColorForMember(member.id)}
          size="sm"
        />
        {/* 成员名是动态内容，原样渲染。 */}
        <span className="min-w-0 flex-1 truncate text-sm text-foreground">
          {name}
        </span>
        <span
          aria-hidden="true"
          className={cn(
            "size-1.5 rounded-full",
            runStateDotClass[member.runState] ?? "bg-status-idle",
          )}
        />
        {canOpen ? (
          <ChevronRight
            className="size-4 text-muted-foreground"
            aria-label={t("group.roster.openMember")}
          />
        ) : null}
      </button>
      {onSetNickname ? (
        <MemberNicknamePopover
          member={member}
          realName={realName}
          takenNames={takenNames}
          onSetNickname={onSetNickname}
        />
      ) : null}
    </div>
  );
}

function GroupRoster({
  members,
  memberName,
  projectID,
  projectName,
  onOpenProject,
  onOpenMember,
  onInvite,
  onDelete,
  tasks,
  onAnchorTask,
  onOpenMemberById,
  agentNameOf,
  onSetMemberNickname,
}: GroupRosterProps) {
  const { t } = useTranslation();
  const [tab, setTab] = React.useState<RosterTab>("members");
  const [deleteOpen, setDeleteOpen] = React.useState(false);

  const hosts = members.filter((m) => m.role === "host");
  const regulars = members.filter((m) => m.role !== "host");

  // takenNamesFor: 某成员之外、同群其它成员的有效名集合(群昵称唯一性预校验)。
  const takenNamesFor = React.useCallback(
    (memberId: number) =>
      new Set(
        members.filter((o) => o.id !== memberId).map((o) => memberName(o.id)),
      ),
    [members, memberName],
  );
  const realNameOf = (m: GroupMemberItem) =>
    agentNameOf ? agentNameOf(m) : memberName(m.id);

  const openTaskCount = tasks.filter((tk) => tk.status === "open").length;

  return (
    <aside className="flex w-64 shrink-0 flex-col border-l border-border bg-card">
      <div className="flex shrink-0 gap-1 border-b border-border p-2">
        <Button
          type="button"
          data-testid="group-roster-members-tab"
          variant={tab === "members" ? "secondary" : "ghost"}
          size="sm"
          className="flex-1"
          onClick={() => setTab("members")}
        >
          {t("group.tabs.members")}
        </Button>
        <Button
          type="button"
          data-testid="group-roster-tasks-tab"
          variant={tab === "tasks" ? "secondary" : "ghost"}
          size="sm"
          className="flex-1"
          onClick={() => setTab("tasks")}
        >
          {t("group.tabs.tasks")}
          {openTaskCount > 0 ? (
            <Badge
              variant="secondary"
              className="ml-1 h-4 min-w-4 px-1 font-mono text-2xs"
            >
              {openTaskCount}
            </Badge>
          ) : null}
        </Button>
        <Button
          type="button"
          data-testid="group-roster-settings-tab"
          variant={tab === "settings" ? "secondary" : "ghost"}
          size="sm"
          className="flex-1"
          onClick={() => setTab("settings")}
        >
          {t("group.tabs.settings")}
        </Button>
      </div>

      {tab === "members" ? (
        <div className="flex min-h-0 flex-1 flex-col overflow-auto p-2">
          {hosts.length > 0 ? (
            <>
              <div className="px-2 pb-1 pt-2 text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
                {t("group.roster.host")}
              </div>
              {hosts.map((m) => (
                <MemberRow
                  key={m.id}
                  member={m}
                  name={memberName(m.id)}
                  realName={realNameOf(m)}
                  takenNames={takenNamesFor(m.id)}
                  onOpen={onOpenMember}
                  onSetNickname={onSetMemberNickname}
                />
              ))}
            </>
          ) : null}
          {regulars.length > 0 ? (
            <>
              <div className="px-2 pb-1 pt-3 text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
                {t("group.roster.members")}
              </div>
              {regulars.map((m) => (
                <MemberRow
                  key={m.id}
                  member={m}
                  name={memberName(m.id)}
                  realName={realNameOf(m)}
                  takenNames={takenNamesFor(m.id)}
                  onOpen={onOpenMember}
                  onSetNickname={onSetMemberNickname}
                />
              ))}
            </>
          ) : null}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="mt-3"
            onClick={onInvite}
          >
            <UserPlus data-icon="inline-start" aria-hidden="true" />
            {t("group.roster.invite")}
          </Button>
        </div>
      ) : tab === "tasks" ? (
        <GroupTaskList
          tasks={tasks}
          memberName={memberName}
          onAnchorTask={onAnchorTask}
          onOpenMember={onOpenMemberById}
        />
      ) : (
        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto p-4">
          <div>
            <div className="text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
              {t("group.settings.project")}
            </div>
            {projectID && projectID > 0 && projectName ? (
              // 项目名是动态值，原样渲染；点击跳转到该项目。
              <button
                type="button"
                onClick={() => onOpenProject?.(projectID)}
                className="mt-1 block max-w-full truncate text-left text-xs text-primary hover:underline"
                title={projectName}
              >
                {projectName}
              </button>
            ) : (
              <div className="mt-1 text-xs text-muted-foreground">
                {t("group.settings.noProject")}
              </div>
            )}
          </div>
          <Button
            type="button"
            variant="destructive"
            size="sm"
            onClick={() => setDeleteOpen(true)}
          >
            <Trash2 data-icon="inline-start" aria-hidden="true" />
            {t("group.delete.button")}
          </Button>
          <GroupDeleteDialog
            open={deleteOpen}
            onOpenChange={setDeleteOpen}
            onConfirm={onDelete}
          />
        </div>
      )}
    </aside>
  );
}

export { GroupRoster };
