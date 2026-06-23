import * as React from "react";
import { ArrowDown, Pause, Play, Square } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useChatAgentsStore } from "@/stores/chat-agents-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useGroupListStore } from "@/stores/group-list-store";

import { useChatAgents } from "../../../hooks/use-chat-agents";
import { useGroup } from "../../../hooks/use-group";
import { useProjectList } from "../../../hooks/use-project-list";
import { useStickToBottom } from "../../../hooks/use-stick-to-bottom";

import { MarkdownText } from "../markdown-text";

import { GroupComposer, type GroupComposerSend } from "./group-composer";
import { GroupRoster } from "./group-roster";
import { GroupTranscript } from "./group-transcript";
import { MentionText, mentionMarkdownDecorator } from "./mention-text";
import { normalizeMentionMarkup } from "./mentions";

import {
  GroupDelete,
  GroupPause,
  GroupResume,
  GroupSend,
  GroupSetMemberNickname,
  GroupStop,
} from "../../../../wailsjs/go/app/App";
import { app } from "../../../../wailsjs/go/models";

type GroupMemberItem = app.GroupMemberItem;

// run_status -> i18n key suffix. The backend may send values like
// "waiting_user"; normalize them to existing locale keys.
// 这里收敛到已存在的 key；未知值兜底成 idle，绝不让 t() 落空。
const RUN_STATUS_KEY: Record<string, string> = {
  running: "running",
  waiting_user: "waitingUser",
  waitingUser: "waitingUser",
  paused: "paused",
  idle: "idle",
  error: "error",
};

function GroupChat({ groupId }: { groupId: number }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { detail } = useGroup(groupId);
  const { agents } = useChatAgents();
  const { projects } = useProjectList();
  const openGroupMemberSession = useChatTabsStore(
    (s) => s.openGroupMemberSession,
  );

  const group = detail?.group;

  // 群绑定项目 → roster 设置页展示。projectId=0(未绑定)时 projectName 为空,
  // roster 兜底成「未绑定项目」。点击跳到 /projects 并 focus 该项目。
  const projectName = React.useMemo(
    () => projects.find((p) => p.id === group?.projectId)?.name,
    [projects, group?.projectId],
  );
  const openProject = React.useCallback(
    (projectID: number) => navigate(`/projects?focus=${projectID}`),
    [navigate],
  );
  const members = React.useMemo(() => detail?.members ?? [], [detail?.members]);
  const messages = React.useMemo(
    () => detail?.messages ?? [],
    [detail?.messages],
  );

  const tasks = React.useMemo(() => detail?.tasks ?? [], [detail?.tasks]);

  // taskById:taskID → 实时任务实体,transcript 卡片按它取状态(随 task_updated 翻转)。
  const taskById = React.useMemo(() => {
    const map = new Map<number, app.GroupTaskItem>();
    for (const tk of tasks) map.set(tk.id, tk);
    return map;
  }, [tasks]);

  // createdMessageIdByTaskNo:#N → 派活卡(created 消息)的消息 id。任务 tab 点行
  // 与卡片回指链接都锚到这条消息。
  const createdMessageIdByTaskNo = React.useMemo(() => {
    const idByTaskId = new Map<number, number>();
    for (const m of messages) {
      if (m.taskEvent === "created" && !idByTaskId.has(m.taskID)) {
        idByTaskId.set(m.taskID, m.id);
      }
    }
    const map = new Map<number, number>();
    for (const tk of tasks) {
      const mid = idByTaskId.get(tk.id);
      if (mid != null) map.set(tk.taskNo, mid);
    }
    return map;
  }, [messages, tasks]);

  const {
    ref: scrollRef,
    atBottom,
    scrollToBottom,
    onScroll,
  } = useStickToBottom(messages.length);

  const memberById = React.useMemo(() => {
    const map = new Map<number, GroupMemberItem>();
    for (const m of members) map.set(m.id, m);
    return map;
  }, [members]);

  // nameOf: agentID → 真实 agent 显示名,数据来自 chat-agents-store(useChatAgents)。
  // 找不到时回退 "Agent #<id>"(动态内容,绝不进 t())。
  const nameOf = React.useCallback(
    (agentID: number) =>
      agents.find((a) => a.id === agentID)?.name ?? `Agent #${agentID}`,
    [agents],
  );

  // memberDisplayName: 成员在本群的有效显示名 —— 设了群昵称(非空白)用昵称,否则回落
  // agent 全局名。roster / 转录抬头 / @mention 命中 都经此口径(与后端 memberDisplayName 同义)。
  const memberDisplayName = React.useCallback(
    (m: GroupMemberItem) => m.nickname?.trim() || nameOf(m.agentID),
    [nameOf],
  );

  // memberName: roster member id → 有效显示名(群昵称优先,否则 agent 全局名)。
  const memberName = React.useCallback(
    (memberId: number) => {
      const m = memberById.get(memberId);
      if (!m) return `#${memberId}`;
      return memberDisplayName(m);
    },
    [memberById, memberDisplayName],
  );

  const openMemberById = React.useCallback(
    (memberId: number) => {
      const member = memberById.get(memberId);
      if (!member || member.backingSessionID <= 0) return;
      openGroupMemberSession(
        groupId,
        member.backingSessionID,
        memberName(memberId),
      );
    },
    [groupId, memberById, memberName, openGroupMemberSession],
  );

  function openMember(member: GroupMemberItem) {
    openMemberById(member.id);
  }

  // scrollToMessage:群 transcript 无虚拟化,原生 DOM 锚定即可(行/卡带 data-message-id)。
  const scrollToMessage = React.useCallback(
    (messageId: number) => {
      const el = scrollRef.current?.querySelector(
        `[data-message-id="${messageId}"]`,
      );
      el?.scrollIntoView({ block: "center" });
    },
    [scrollRef],
  );

  const anchorTaskNo = React.useCallback(
    (taskNo: number) => {
      const mid = createdMessageIdByTaskNo.get(taskNo);
      if (mid != null) scrollToMessage(mid);
    },
    [createdMessageIdByTaskNo, scrollToMessage],
  );

  // handleDelete: 删除该群(deleteSessions 决定是否一并删关联会话),然后让删除在 UI 立即可见 ——
  // reload 群列表(Group().List 过滤 status=ACTIVE → 群消失)+ reload 会话列表(deleteSessions=true
  // 时被软删的成员 backing session 从各 agent 侧栏 recent/attention/运行灯消失)+ 关闭该群标签页。
  const handleDelete = React.useCallback(
    async (deleteSessions: boolean) => {
      try {
        await GroupDelete(groupId, deleteSessions);
      } catch (e: unknown) {
        console.error("[group] delete failed", e);
        return;
      }
      await Promise.all([
        useGroupListStore.getState().reload(),
        useChatAgentsStore.getState().reload(),
      ]);
      // 关闭该群的群标签页 + 其所有成员会话(groupSession)标签页 —— 群已不存在,
      // 群上下文的 tab 一并收掉(快照后再逐个关,closeTab 会原地改 tabs)。
      const tabsStore = useChatTabsStore.getState();
      const stale = tabsStore.tabs.filter(
        (tb) =>
          (tb.meta.kind === "group" || tb.meta.kind === "groupSession") &&
          tb.meta.groupId === groupId,
      );
      for (const tb of stale) tabsStore.closeTab(tb.id);
    },
    [groupId],
  );

  // handleSetMemberNickname: 设/清某成员的群昵称(空串=清除回落原名)。后端落库后 emit
  // member_updated → use-group 事件处理 patchMember 回灌 nickname,roster/转录随之刷新。
  const handleSetMemberNickname = React.useCallback(
    (memberId: number, nickname: string) => {
      void GroupSetMemberNickname(memberId, nickname).catch((e: unknown) => {
        console.error("[group] set member nickname failed", e);
      });
    },
    [],
  );

  // mentionRoster: 把成员映射成 MentionText 需要的 { memberId, name } 形态,
  // 供 transcript 里 @mention chip 按名字命中 + 点击跳转成员会话视图。
  const mentionRoster = React.useMemo(
    () => members.map((m) => ({ memberId: m.id, name: memberDisplayName(m) })),
    [members, memberDisplayName],
  );

  // mentionDecorator: 挂在 MarkdownText 内联装饰器接缝上的 @mention chip,点击
  // 复用 openMemberById —— 与点击 roster 行的 "›" 跳到同一视图。useMemo 保持引用
  // 稳定,让 MarkdownText 的 memo 生效。
  const mentionDecorator = React.useMemo(
    () => mentionMarkdownDecorator(mentionRoster, openMemberById),
    [mentionRoster, openMemberById],
  );

  // renderMessageBody: user/agent 正文走与单聊一致的 markdown 渲染(复用 MarkdownText),
  // mention chip 经装饰器在 markdown 文本节点层面注入(代码块天然跳过)。
  // <mention> 标记必须先归一成 @NAME —— react-markdown 会丢弃 raw HTML 节点。
  const renderMessageBody = React.useCallback(
    (content: string) => (
      <MarkdownText
        text={normalizeMentionMarkup(content)}
        decorator={mentionDecorator}
      />
    ),
    [mentionDecorator],
  );

  // renderSystemBody: system 行("X 加入了群聊")是短动态文案,保持纯文本 + mention
  // 高亮,不走 markdown —— 胶囊里渲染块级 markdown 元素既无必要也破坏样式。
  const renderSystemBody = React.useCallback(
    (content: string) => (
      <MentionText
        text={content}
        roster={mentionRoster}
        onJump={openMemberById}
      />
    ),
    [mentionRoster, openMemberById],
  );

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

  return (
    <div className="flex min-h-0 min-w-0 flex-1">
      <main className="flex min-h-0 min-w-0 flex-1 flex-col bg-background">
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="flex min-h-[44px] shrink-0 flex-col justify-center gap-1 border-b border-border px-5 py-1.5">
            {/* 第一行：群标题 + 运行控制按钮 */}
            <div className="flex min-w-0 items-center gap-3">
              {/* 群标题是动态内容，不进 t()。 */}
              <div
                className="min-w-0 flex-1 truncate text-sm font-semibold"
                title={group.title}
              >
                {group.title}
              </div>
              {isRunning ? (
                <>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    data-testid="group-pause-button"
                    onClick={() => runControl(GroupPause)}
                  >
                    <Pause data-icon="inline-start" aria-hidden="true" />
                    {t("group.controls.pause")}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    data-testid="group-stop-button"
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
                  data-testid="group-resume-button"
                  onClick={() => runControl(GroupResume)}
                >
                  <Play data-icon="inline-start" aria-hidden="true" />
                  {t("group.controls.resume")}
                </Button>
              ) : null}
            </div>
            {/* 第二行：群标识符 + 运行状态 + 轮次（与普通会话的 sess-{id} 元数据行对齐） */}
            <div className="flex min-w-0 items-center gap-2">
              {/* group-{id} 是技术标识符，和 sess-{id} 一致，不进 t()。 */}
              <span className="font-mono text-2xs text-muted-foreground">
                group-{group.id}
              </span>
              <Badge variant="secondary">
                {t(`group.runStatus.${runStatusKey}`)}
              </Badge>
              <span className="text-2xs text-muted-foreground">
                {t("group.rounds", { count: group.roundCount })}
              </span>
            </div>
          </div>

          <section
            ref={scrollRef as React.Ref<HTMLElement>}
            onScroll={onScroll}
            data-testid="group-scroll"
            className="relative min-h-0 flex-1 overflow-auto px-7 py-5"
          >
            <GroupTranscript
              messages={messages}
              roster={members}
              memberName={memberName}
              renderBody={renderMessageBody}
              renderSystemBody={renderSystemBody}
              taskById={taskById}
              onJumpMember={openMemberById}
              onJumpTaskNo={anchorTaskNo}
            />
            {!atBottom ? (
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                aria-label={t("chatPanel.scroll.backToBottom")}
                title={t("chatPanel.scroll.backToBottom")}
                onClick={scrollToBottom}
                className="sticky bottom-4 z-20 ml-auto flex rounded-full bg-background shadow-md hover:shadow-lg dark:bg-background animate-in fade-in slide-in-from-bottom-1 duration-200 ease-out motion-reduce:animate-none"
              >
                <ArrowDown data-icon="only" aria-hidden="true" />
              </Button>
            ) : null}
          </section>

          <GroupComposer
            members={members}
            memberName={memberName}
            onSend={(payload) => void handleSend(payload)}
          />
        </div>
      </main>

      <GroupRoster
        members={members}
        memberName={memberName}
        projectID={group.projectId}
        projectName={projectName}
        onOpenProject={openProject}
        onOpenMember={openMember}
        onInvite={() => {
          // MVP：邀请 picker 留待后续接入真实 agent 选择器。
          console.warn("[group] invite picker not wired");
        }}
        onDelete={(deleteSessions) => void handleDelete(deleteSessions)}
        tasks={tasks}
        onAnchorTask={(tk) => anchorTaskNo(tk.taskNo)}
        onOpenMemberById={openMemberById}
        agentNameOf={(m) => nameOf(m.agentID)}
        onSetMemberNickname={handleSetMemberNickname}
      />
    </div>
  );
}

export { GroupChat };
