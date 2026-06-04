import * as React from "react";
import { ChevronLeft, Pause, Play, Square } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import { useChatAgents } from "../../../hooks/use-chat-agents";
import { useGroup } from "../../../hooks/use-group";
import { ChatPanel } from "../chat-panel";

import { GroupComposer, type GroupComposerSend } from "./group-composer";
import { GroupRoster } from "./group-roster";
import { GroupTranscript } from "./group-transcript";
import { MentionText } from "./mention-text";

import {
  GroupArchive,
  GroupPause,
  GroupResume,
  GroupSend,
  GroupStop,
} from "../../../../wailsjs/go/app/App";
import { app } from "../../../../wailsjs/go/models";

type GroupMemberItem = app.GroupMemberItem;

// run_status → i18n key 后缀映射。后端可能下发 "waiting_user" 之类带下划线的值，
// 这里收敛到已存在的 key；未知值兜底成 idle，绝不让 t() 落空。
const RUN_STATUS_KEY: Record<string, string> = {
  running: "running",
  waiting_user: "waitingUser",
  waitingUser: "waitingUser",
  paused: "paused",
  idle: "idle",
  error: "error",
};

// 视图：群视图（id="group"）+ 任意打开的成员视图（id=`member:<memberId>`）。
type ViewId = string;

function GroupChat({ groupId }: { groupId: number }) {
  const { t } = useTranslation();
  const { detail } = useGroup(groupId);
  const { agents } = useChatAgents();

  const group = detail?.group;
  const members = React.useMemo(() => detail?.members ?? [], [detail?.members]);
  const messages = React.useMemo(
    () => detail?.messages ?? [],
    [detail?.messages],
  );

  // 视图 tab 栏状态：本地管理，不接全局 chat-tabs-store。openMemberIds 是被打开过的
  // 成员视图（按打开顺序），activeView 决定中间区渲染群视图还是某成员的 ChatPanel。
  const [openMemberIds, setOpenMemberIds] = React.useState<number[]>([]);
  const [activeView, setActiveView] = React.useState<ViewId>("group");

  const memberById = React.useMemo(() => {
    const map = new Map<number, GroupMemberItem>();
    for (const m of members) map.set(m.id, m);
    return map;
  }, [members]);

  // memberName 解析：agent 名字理应来自 agent 详情，但本任务范围内 roster 不带名字，
  // 用稳定的 "Agent #<agentID>" 占位（动态内容，不进 t()）。E4/后续接入真实名字。
  // nameOf: agentID → 真实 agent 显示名,数据来自 chat-agents-store(useChatAgents)。
  // 找不到时回退 "Agent #<id>"(动态内容,绝不进 t())。
  const nameOf = React.useCallback(
    (agentID: number) =>
      agents.find((a) => a.id === agentID)?.name ?? `Agent #${agentID}`,
    [agents],
  );

  // memberName: roster member id → 显示名。先取成员的 agentID,再走 nameOf 拿真实名字。
  const memberName = React.useCallback(
    (memberId: number) => {
      const m = memberById.get(memberId);
      if (!m) return `#${memberId}`;
      return nameOf(m.agentID);
    },
    [memberById, nameOf],
  );

  // openMemberById: 打开某成员的会话视图 tab。是 roster 行 "›" 与 transcript 里
  // mention chip 共用的跳转入口——点 @name chip 与点成员行落到同一个成员视图。
  const openMemberById = React.useCallback((memberId: number) => {
    setOpenMemberIds((prev) =>
      prev.includes(memberId) ? prev : [...prev, memberId],
    );
    setActiveView(`member:${memberId}`);
  }, []);

  function openMember(member: GroupMemberItem) {
    openMemberById(member.id);
  }

  // mentionRoster: 把成员映射成 MentionText 需要的 { memberId, name } 形态,
  // 供 transcript 里 @mention chip 按名字命中 + 点击跳转成员会话视图。
  const mentionRoster = React.useMemo(
    () => members.map((m) => ({ memberId: m.id, name: nameOf(m.agentID) })),
    [members, nameOf],
  );

  // renderMessageBody: transcript 的 body 渲染接缝。把正文交给 MentionText 高亮
  // @mention,点击 chip 复用 openMemberById —— 与点击 roster 行的 "›" 跳到同一视图。
  const renderMessageBody = React.useCallback(
    (content: string) => (
      <MentionText
        text={content}
        roster={mentionRoster}
        onJump={openMemberById}
      />
    ),
    [mentionRoster, openMemberById],
  );

  function closeMember(memberId: number) {
    setOpenMemberIds((prev) => prev.filter((id) => id !== memberId));
    setActiveView((cur) => (cur === `member:${memberId}` ? "group" : cur));
  }

  async function handleSend(payload: GroupComposerSend) {
    if (!group) return;
    try {
      await GroupSend(
        app.GroupSendRequest.createFrom({
          groupID: group.id,
          text: payload.text,
          recipientMemberIDs: payload.recipientMemberIDs,
          toUser: false,
        }),
      );
    } catch (e: unknown) {
      console.error("[group] send failed", e);
    }
  }

  function runControl(fn: (id: number) => Promise<void>) {
    if (!group) return;
    void fn(group.id).catch((e: unknown) => {
      console.error("[group] control failed", e);
    });
  }

  if (!group) {
    return <main className="flex flex-1 bg-background" />;
  }

  const runStatusKey = RUN_STATUS_KEY[group.runStatus] ?? "idle";
  const isRunning = group.runStatus === "running";
  const isPaused = group.runStatus === "paused";

  // 群视图 tab 用静态标签 t("group.tabs.group")，避免和房间头的动态群标题重复出现
  // （动态标题只在房间头出现一次）。成员视图 tab 标题是成员名（动态），不进 t()。
  const views: { id: ViewId; title: string; dynamic: boolean }[] = [
    { id: "group", title: t("group.tabs.group"), dynamic: false },
    ...openMemberIds.map((id) => ({
      id: `member:${id}`,
      title: memberName(id),
      dynamic: true,
    })),
  ];

  const activeMemberId = activeView.startsWith("member:")
    ? Number(activeView.slice("member:".length))
    : null;
  const activeMember =
    activeMemberId != null ? memberById.get(activeMemberId) : undefined;

  return (
    <div className="flex min-h-0 min-w-0 flex-1">
      <main className="flex min-h-0 min-w-0 flex-1 flex-col bg-background">
        {/* ── 视图 tab 栏 ── */}
        <div className="flex h-[38px] shrink-0 items-stretch border-b border-border">
          {views.map((v) => {
            const isMemberView = v.id.startsWith("member:");
            return (
              <div
                key={v.id}
                role="tab"
                data-active={v.id === activeView}
                onClick={() => setActiveView(v.id)}
                className={cn(
                  "relative flex min-w-[110px] max-w-[220px] cursor-pointer items-center gap-1.5 border-r border-border px-3 text-xs",
                  v.id === activeView
                    ? "bg-background font-medium text-foreground"
                    : "text-muted-foreground hover:bg-card",
                )}
              >
                {v.id === activeView ? (
                  <span className="absolute left-0 top-0 h-[2px] w-full bg-primary" />
                ) : null}
                <span className="min-w-0 flex-1 truncate">{v.title}</span>
                {isMemberView ? (
                  <button
                    type="button"
                    aria-label={t("group.roster.backToGroup")}
                    className="inline-flex size-4 items-center justify-center rounded-sm text-muted-foreground hover:bg-accent hover:text-foreground"
                    onClick={(e) => {
                      e.stopPropagation();
                      closeMember(Number(v.id.slice("member:".length)));
                    }}
                  >
                    <ChevronLeft className="size-3" aria-hidden="true" />
                  </button>
                ) : null}
              </div>
            );
          })}
        </div>

        {activeMember ? (
          // ── 成员视图：嵌入单会话 ChatPanel ──
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex shrink-0 items-center gap-2 border-b border-border px-5 py-1.5">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setActiveView("group")}
              >
                <ChevronLeft data-icon="inline-start" aria-hidden="true" />
                {t("group.roster.backToGroup")}
              </Button>
            </div>
            <ChatPanel sessionId={activeMember.backingSessionID} />
          </div>
        ) : (
          // ── 群视图：房间头 + transcript + composer ──
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-[44px] shrink-0 items-center gap-3 border-b border-border px-5 py-1.5">
              {/* 群标题是动态内容，不进 t()。 */}
              <div
                className="min-w-0 flex-1 truncate text-sm font-semibold"
                title={group.title}
              >
                {group.title}
              </div>
              <Badge variant="secondary">
                {t(`group.runStatus.${runStatusKey}`)}
              </Badge>
              <span className="text-2xs text-muted-foreground">
                {t("group.rounds", { count: group.roundCount })}
              </span>
              {isRunning ? (
                <>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => runControl(GroupPause)}
                  >
                    <Pause data-icon="inline-start" aria-hidden="true" />
                    {t("group.controls.pause")}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => runControl(GroupStop)}
                  >
                    <Square data-icon="inline-start" aria-hidden="true" />
                    {t("group.controls.stop")}
                  </Button>
                </>
              ) : null}
              {isPaused ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => runControl(GroupResume)}
                >
                  <Play data-icon="inline-start" aria-hidden="true" />
                  {t("group.controls.resume")}
                </Button>
              ) : null}
            </div>

            <section className="min-h-0 flex-1 overflow-auto px-7 py-5">
              <GroupTranscript
                messages={messages}
                roster={members}
                memberName={memberName}
                renderBody={renderMessageBody}
              />
            </section>

            <GroupComposer
              members={members}
              memberName={memberName}
              onSend={(payload) => void handleSend(payload)}
            />
          </div>
        )}
      </main>

      <GroupRoster
        members={members}
        memberName={memberName}
        onOpenMember={openMember}
        onInvite={() => {
          // MVP：邀请 picker 留待后续接入真实 agent 选择器（Task E4/后续）。
          console.warn("[group] invite picker not wired");
        }}
        onArchive={() => void GroupArchive(group.id)}
      />
    </div>
  );
}

export { GroupChat };
