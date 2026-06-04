import * as React from "react";
import { Archive, ChevronRight, UserPlus } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import { AgentAvatar } from "../primitives";

import { agentColorForMember } from "./group-transcript";

import type { app } from "../../../../wailsjs/go/models";

type GroupMemberItem = app.GroupMemberItem;

type RosterTab = "members" | "settings";

export type GroupRosterProps = {
  members: GroupMemberItem[];
  memberName: (memberId: number) => string;
  /** 工作目录是动态值（路径），原样展示，不进 t()。 */
  workdir?: string;
  onOpenMember: (member: GroupMemberItem) => void;
  onInvite: () => void;
  onArchive: () => void;
};

const statusDotClass: Record<string, string> = {
  active: "bg-status-running",
  left: "bg-status-idle",
};

function MemberRow({
  member,
  name,
  onOpen,
}: {
  member: GroupMemberItem;
  name: string;
  onOpen: (member: GroupMemberItem) => void;
}) {
  const { t } = useTranslation();
  return (
    <button
      type="button"
      onClick={() => onOpen(member)}
      className="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left hover:bg-accent"
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
          statusDotClass[member.status] ?? "bg-status-idle",
        )}
      />
      <ChevronRight
        className="size-4 text-muted-foreground"
        aria-label={t("group.roster.openMember")}
      />
    </button>
  );
}

function GroupRoster({
  members,
  memberName,
  workdir,
  onOpenMember,
  onInvite,
  onArchive,
}: GroupRosterProps) {
  const { t } = useTranslation();
  const [tab, setTab] = React.useState<RosterTab>("members");

  const coordinators = members.filter((m) => m.role === "coordinator");
  const regulars = members.filter((m) => m.role !== "coordinator");

  return (
    <aside className="flex w-64 shrink-0 flex-col border-l border-border bg-card">
      <div className="flex shrink-0 gap-1 border-b border-border p-2">
        <Button
          type="button"
          variant={tab === "members" ? "secondary" : "ghost"}
          size="sm"
          className="flex-1"
          onClick={() => setTab("members")}
        >
          {t("group.tabs.members")}
        </Button>
        <Button
          type="button"
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
          {coordinators.length > 0 ? (
            <>
              <div className="px-2 pb-1 pt-2 text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
                {t("group.roster.coordinator")}
              </div>
              {coordinators.map((m) => (
                <MemberRow
                  key={m.id}
                  member={m}
                  name={memberName(m.id)}
                  onOpen={onOpenMember}
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
                  onOpen={onOpenMember}
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
      ) : (
        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto p-4">
          <div>
            <div className="text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
              {t("group.settings.workdir")}
            </div>
            {/* 工作目录路径是动态值，原样渲染。 */}
            <div className="mt-1 break-all font-mono text-xs text-foreground">
              {workdir || "—"}
            </div>
          </div>
          <Button
            type="button"
            variant="destructive"
            size="sm"
            onClick={onArchive}
          >
            <Archive data-icon="inline-start" aria-hidden="true" />
            {t("group.settings.archive")}
          </Button>
        </div>
      )}
    </aside>
  );
}

export { GroupRoster };
