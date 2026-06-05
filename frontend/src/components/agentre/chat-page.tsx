import * as React from "react";
import { Check, Plus, Search, SlidersHorizontal, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useChatAgents, type ChatAgentItem } from "@/hooks/use-chat-agents";
import { useGroupList, type GroupListItem } from "@/hooks/use-group-list";
import { NEW_CHAT_INITIAL_QUERY } from "@/components/agentre/shortcuts/registry";
import {
  reasonToDisplayStatus,
  reasonToPillText,
} from "@/lib/attention-display";
import { cn } from "@/lib/utils";
import { relativeTime } from "@/lib/relative-time";
import {
  useSessionAttentionList,
  type AttentionReason,
} from "@/stores/attention-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useCommandPaletteStore } from "@/stores/command-palette-store";
import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import { AgentGroup, AgentPanelSection } from "./agent-list";
import type { AgentSession } from "./agent-list";
import { StatusDot } from "./primitives";
import { GroupNewDialog } from "./group-chat/group-new-dialog";
import { ResizableSidebar } from "./resizable-sidebar";
import { SessionsPopover } from "./sessions-popover";
import { ListChatAgentSessions } from "../../../wailsjs/go/app/App";
import type { AgentColor, AgentStatus } from "./types";

// 群 run_status → sidebar StatusDot 的 AgentStatus 收敛。后端可能下发带下划线的
// "waiting_user";"paused" 落到 idle(暂停不需要醒目色)。未知值兜底 idle。
function groupRunStatusToDotStatus(runStatus: string): AgentStatus {
  switch (runStatus) {
    case "running":
      return "running";
    case "waiting_user":
    case "waitingUser":
      return "waiting";
    case "error":
      return "error";
    default:
      return "idle";
  }
}

// ─── AgentSession builder ────────────────────────────────────────────────────

// agentSessionFromMeta: 从 meta-store 数据和 reason 投影成 AgentSession。
// 展开态常规列表（buildSessions 等价）和 attention bubble 共用。
function agentSessionFromMeta(
  sid: number,
  title: string,
  lastMessageAt: number,
  agentStatus: string,
  reason: AttentionReason | null,
  t: TFunction,
  attentionRank?: AttentionReason | "selected",
): AgentSession {
  const status = reasonToDisplayStatus(
    reason,
    (agentStatus as AgentStatus) || "idle",
  );
  const trailingLabel =
    status === "running"
      ? "running"
      : status === "waiting"
        ? (reasonToPillText(reason) ?? "")
        : status === "error"
          ? "error"
          : relativeTime(lastMessageAt);
  return {
    id: String(sid),
    status,
    title: title || t("chatPage.untitledSession"),
    trailingLabel,
    ...(attentionRank !== undefined ? { attentionRank } : {}),
  };
}

// useBuildAttentionSessions: 把 agent 的 sessionIds 投影成 sidebar attention bubble 行。
// 通过 useSessionAttentionList 过滤出真正需要冒泡的 session，并将 selected 锚点钉到末尾。
function useBuildAttentionSessions(
  agent: ChatAgentItem,
  selectedAgentId: number,
  selectedSessionId: number,
): AgentSession[] {
  const { t } = useTranslation();
  const sessionIds = React.useMemo(
    () => agent.sessionIds ?? agent.sessions.map((s) => s.id),
    [agent],
  );
  const attentionItems = useSessionAttentionList(sessionIds);
  const metas = useSessionMetaStore((s) => s.metas);
  const statuses = useSessionStatusStore((s) => s.statuses);

  return React.useMemo(() => {
    const isThisAgentSelected =
      selectedAgentId === agent.id && selectedSessionId > 0;
    const rows: AgentSession[] = [];
    const seen = new Set<number>();
    for (const { sessionId, reason } of attentionItems) {
      const meta = metas.get(sessionId);
      if (!meta) continue;
      const status = statuses.get(sessionId);
      seen.add(sessionId);
      rows.push(
        agentSessionFromMeta(
          sessionId,
          meta.title,
          meta.lastMessageAt ?? 0,
          status?.agentStatus ?? "idle",
          reason,
          t,
          reason,
        ),
      );
    }
    rows.sort((a, b) => {
      const aTs = metas.get(Number(a.id))?.lastMessageAt ?? 0;
      const bTs = metas.get(Number(b.id))?.lastMessageAt ?? 0;
      return bTs - aTs;
    });
    // selected 锚点：当前打开的会话即使不在 attention 池，也钉到末尾
    if (isThisAgentSelected && !seen.has(selectedSessionId)) {
      const meta = metas.get(selectedSessionId);
      if (meta) {
        const status = statuses.get(selectedSessionId);
        rows.push(
          agentSessionFromMeta(
            selectedSessionId,
            meta.title,
            meta.lastMessageAt ?? 0,
            status?.agentStatus ?? "idle",
            null,
            t,
            "selected",
          ),
        );
      }
    }
    return rows;
  }, [
    attentionItems,
    metas,
    statuses,
    selectedAgentId,
    selectedSessionId,
    agent.id,
    t,
  ]);
}

// buildSessions: 投影展开态侧栏常规列表（从 meta/status store 读，无需 attentionSessions）。
function useBuildSessions(agent: ChatAgentItem): AgentSession[] {
  const { t } = useTranslation();
  const metas = useSessionMetaStore((s) => s.metas);
  const statuses = useSessionStatusStore((s) => s.statuses);
  const attentionItems = useSessionAttentionList(
    React.useMemo(
      () => agent.sessionIds ?? agent.sessions.map((s) => s.id),
      [agent],
    ),
  );
  const attentionMap = React.useMemo(() => {
    const m = new Map<number, AttentionReason>();
    for (const x of attentionItems) m.set(x.sessionId, x.reason);
    return m;
  }, [attentionItems]);

  return React.useMemo(() => {
    return agent.sessions.map((s) => {
      const meta = metas.get(s.id);
      const status = statuses.get(s.id);
      const reason = attentionMap.get(s.id) ?? null;
      return agentSessionFromMeta(
        s.id,
        meta?.title ?? s.title,
        meta?.lastMessageAt ?? s.lastMessageAt,
        status?.agentStatus ?? s.status,
        reason,
        t,
      );
    });
  }, [agent.sessions, metas, statuses, attentionMap, t]);
}

// ─── AgentGroupRow ────────────────────────────────────────────────────────────
// 独立组件：每个 agent 对应一行，内部调 useBuildSessions / useBuildAttentionSessions
// hook，因此可以安全使用 React hooks（不违反 rules-of-hooks）。

type AgentGroupRowProps = {
  agent: ChatAgentItem;
  selectedAgentId: number;
  selectedSessionId: number;
  openSession: (sid: number) => void;
  openSessionInNewTab: (sid: number) => void;
  openNewSession: (projectId: number, agentId: number, title: string) => void;
  showNotChattableNotice: (name: string, hint: string) => void;
};

function AgentGroupRow({
  agent: a,
  selectedAgentId,
  selectedSessionId,
  openSession,
  openSessionInNewTab,
  openNewSession,
  showNotChattableNotice,
}: AgentGroupRowProps) {
  const { t } = useTranslation();
  const sessions = useBuildSessions(a);
  const attentionSessions = useBuildAttentionSessions(
    a,
    selectedAgentId,
    selectedSessionId,
  );
  return (
    <AgentGroup
      key={a.id}
      name={a.name}
      initials={a.name.charAt(0)}
      color={(a.avatarColor as AgentColor) || "agent-1"}
      activeCount={a.activeCount}
      pinned={a.pinned}
      persistenceKey={`agent:${a.id}`}
      sessions={sessions}
      attentionSessions={attentionSessions}
      totalSessions={a.totalSessions > 5 ? Number(a.totalSessions) : undefined}
      selectedSessionId={
        selectedSessionId ? String(selectedSessionId) : undefined
      }
      onHeaderClick={() => {
        if (!a.chattable) {
          showNotChattableNotice(
            a.name,
            a.chattableHint || t("chatPage.notChattable.defaultHint"),
          );
          return;
        }
        const first = a.sessions[0];
        if (first) openSession(first.id);
        else openNewSession(0, a.id, "");
      }}
      onNewSession={() => {
        openNewSession(0, a.id, "");
      }}
      onSessionSelect={(sid, opts) => {
        if (opts?.newTab) openSessionInNewTab(Number(sid));
        else openSession(Number(sid));
      }}
      renderSessionsPopover={(close) => (
        <SessionsPopover
          header={{
            name: a.name,
            avatarColor: a.avatarColor,
            avatarIcon: a.avatarIcon,
            avatarDataUrl: a.avatarDataUrl,
            activeCount: a.activeCount,
          }}
          loader={async ({ offset, limit }) => {
            const resp = await ListChatAgentSessions({
              agentId: a.id,
              offset,
              limit,
            } as Parameters<typeof ListChatAgentSessions>[0]);
            return {
              sessions: resp.sessions,
              total: resp.total,
              hasMore: resp.hasMore,
            };
          }}
          onClose={close}
          onSelectSession={(sid, opts) => {
            if (opts?.newTab) openSessionInNewTab(sid);
            else openSession(sid);
          }}
        />
      )}
    />
  );
}

// ─── GroupRow ────────────────────────────────────────────────────────────────
// 左侧群聊分区的一行:群标题(动态,不进 t())+ run_status 状态点。点击打开/激活
// 对应 group tab。视觉密度与 SessionRow 对齐。

type GroupRowProps = {
  group: GroupListItem;
  selected: boolean;
  onOpen: (groupId: number, title: string) => void;
};

function GroupRow({ group, selected, onOpen }: GroupRowProps) {
  const dotStatus = groupRunStatusToDotStatus(group.runStatus);
  return (
    <button
      type="button"
      aria-current={selected ? "true" : undefined}
      onClick={() => onOpen(group.id, group.title)}
      className={cn(
        "flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs outline-none transition-colors hover:bg-sidebar-active-bg focus-visible:ring-[3px] focus-visible:ring-ring/50",
        selected && "bg-primary-soft text-primary-text",
      )}
    >
      <StatusDot status={dotStatus} size="xs" />
      <span
        className={cn(
          "min-w-0 flex-1 truncate",
          selected ? "font-medium text-primary-text" : "text-foreground",
        )}
      >
        {group.title}
      </span>
    </button>
  );
}

// ─── Sidebar mixed filter ────────────────────────────────────────────────────

type ChatSidebarFilter = "all" | "groups" | "agents" | "running" | "unread";

function groupMatchesSearch(group: GroupListItem, query: string): boolean {
  if (!query) return true;
  return group.title.toLowerCase().includes(query);
}

function agentMatchesSearch(agent: ChatAgentItem, query: string): boolean {
  if (!query) return true;
  return (
    agent.name.toLowerCase().includes(query) ||
    agent.sessions.some((s) => s.title.toLowerCase().includes(query))
  );
}

function groupMatchesFilter(
  group: GroupListItem,
  filter: ChatSidebarFilter,
): boolean {
  switch (filter) {
    case "all":
    case "groups":
      return true;
    case "running":
      return group.runStatus === "running";
    case "agents":
    case "unread":
      return false;
  }
}

function agentMatchesFilter(
  agent: ChatAgentItem,
  filter: ChatSidebarFilter,
  attentionReasons: Map<number, AttentionReason>,
): boolean {
  switch (filter) {
    case "all":
    case "agents":
      return true;
    case "groups":
      return false;
    case "running":
      return (
        agent.activeCount > 0 ||
        (agent.sessionIds ?? agent.sessions.map((s) => s.id)).some(
          (sid) => attentionReasons.get(sid) === "running",
        )
      );
    case "unread":
      return (agent.sessionIds ?? agent.sessions.map((s) => s.id)).some(
        (sid) => attentionReasons.get(sid) === "unread",
      );
  }
}

// ─── Main ChatPage ───────────────────────────────────────────────────────────

function ChatPage() {
  const { t } = useTranslation();
  const { agents } = useChatAgents();
  const { groups } = useGroupList();
  const metas = useSessionMetaStore((s) => s.metas);
  // 选中态完全派生自 chat-tabs-store(single source of truth):
  // - kind:"session" → selectedSessionId = meta.sessionId,selectedAgentId 反查 agents
  //   找到拥有该 session 的 agent(用于 sidebar 高亮 + attention bubble 钉选中行)。
  // - kind:"new"     → selectedSessionId = 0,selectedAgentId = meta.agentId。
  // - 无 active tab  → 全 0,sidebar 不高亮任何 agent。
  const activeTab = useChatTabsStore((s) =>
    s.activeTabId ? (s.tabs.find((t) => t.id === s.activeTabId) ?? null) : null,
  );
  const selectedSessionId =
    activeTab?.meta.kind === "session" ? activeTab.meta.sessionId : 0;
  const selectedAgentId = React.useMemo(() => {
    if (!activeTab) return 0;
    if (activeTab.meta.kind === "new") return activeTab.meta.agentId;
    if (activeTab.meta.kind !== "session") return 0;
    const sid = activeTab.meta.sessionId;
    for (const a of agents) {
      if (a.sessions.some((s) => s.id === sid)) return a.id;
    }
    return 0;
  }, [activeTab, agents]);
  const openSession = useChatTabsStore((s) => s.openSession);
  const openSessionInNewTab = useChatTabsStore((s) => s.openSessionInNewTab);
  const openNewSession = useChatTabsStore((s) => s.openNewSession);
  const openGroup = useChatTabsStore((s) => s.openGroup);
  const openCommandPalette = useCommandPaletteStore((s) => s.openWith);
  // 当前 active tab 是某个 group → 高亮对应群行。
  const selectedGroupId =
    activeTab?.meta.kind === "group" ? activeTab.meta.groupId : 0;
  const [agentFilter, setAgentFilter] = React.useState("");
  const [sidebarFilter, setSidebarFilter] =
    React.useState<ChatSidebarFilter>("all");
  const [filterPopoverOpen, setFilterPopoverOpen] = React.useState(false);
  const [newGroupOpen, setNewGroupOpen] = React.useState(false);
  // not-chattable inline notice: 点击不可对话 agent header 时显示，3 秒后自动消失。
  const [notChattableNotice, setNotChattableNotice] = React.useState<{
    name: string;
    hint: string;
  } | null>(null);
  const notChattableTimerRef = React.useRef<ReturnType<
    typeof setTimeout
  > | null>(null);

  const showNotChattableNotice = React.useCallback(
    (name: string, hint: string) => {
      if (notChattableTimerRef.current)
        clearTimeout(notChattableTimerRef.current);
      setNotChattableNotice({ name, hint });
      notChattableTimerRef.current = setTimeout(() => {
        setNotChattableNotice(null);
        notChattableTimerRef.current = null;
      }, 3000);
    },
    [],
  );

  // 清理 timer on unmount
  React.useEffect(() => {
    return () => {
      if (notChattableTimerRef.current)
        clearTimeout(notChattableTimerRef.current);
    };
  }, []);

  // Filter
  const filterValue = agentFilter.trim().toLowerCase();
  const allSessionIds = React.useMemo(
    () => agents.flatMap((a) => a.sessionIds ?? a.sessions.map((s) => s.id)),
    [agents],
  );
  const attentionItems = useSessionAttentionList(allSessionIds);
  const attentionReasons = React.useMemo(() => {
    const m = new Map<number, AttentionReason>();
    for (const item of attentionItems) m.set(item.sessionId, item.reason);
    return m;
  }, [attentionItems]);
  const unreadCount = React.useMemo(() => {
    let count = 0;
    for (const reason of attentionReasons.values()) {
      if (reason === "unread") count += 1;
    }
    return count;
  }, [attentionReasons]);
  const visibleGroups = React.useMemo(
    () =>
      groups.filter(
        (g) =>
          groupMatchesSearch(g, filterValue) &&
          groupMatchesFilter(g, sidebarFilter),
      ),
    [groups, filterValue, sidebarFilter],
  );
  const visibleAgents = React.useMemo(
    () =>
      agents.filter(
        (a) =>
          agentMatchesSearch(a, filterValue) &&
          agentMatchesFilter(a, sidebarFilter, attentionReasons),
      ),
    [agents, filterValue, sidebarFilter, attentionReasons],
  );
  const pinned = visibleAgents.filter((a) => a.pinned);
  // 非 pinned 按最新会话时间倒序。无会话的 agent 沉到底部，保持原 DB 顺序。
  // pinned 不参与排序（用户强约束：永远最前面）。
  // 排序键取 sessionIds 在 meta-store 中的 max(lastMessageAt)，确保 turn 结束
  // 后实时反映最新活跃时间，而非快照里偶发的 sessions[0] 顺序。
  const others = React.useMemo(() => {
    const agentMaxTs = (a: ChatAgentItem): number => {
      const ids = a.sessionIds ?? a.sessions.map((s) => s.id);
      let max = 0;
      for (const sid of ids) {
        const ts = metas.get(sid)?.lastMessageAt ?? 0;
        if (ts > max) max = ts;
      }
      return max;
    };
    return visibleAgents
      .filter((a) => !a.pinned)
      .sort((a, b) => {
        const aTs = agentMaxTs(a);
        const bTs = agentMaxTs(b);
        if (aTs === bTs) return 0;
        if (aTs === 0) return 1;
        if (bTs === 0) return -1;
        return bTs - aTs;
      });
  }, [visibleAgents, metas]);

  const filterIsActive = filterValue.length > 0;
  const hasResults = visibleAgents.length > 0 || visibleGroups.length > 0;
  const filterOptions: Array<{
    value: ChatSidebarFilter;
    label: string;
    dotClassName?: string;
    badge?: number;
  }> = [
    { value: "all", label: t("chatPage.filter.options.all") },
    { value: "groups", label: t("chatPage.filter.options.groups") },
    { value: "agents", label: t("chatPage.filter.options.agents") },
    {
      value: "running",
      label: t("chatPage.filter.options.running"),
      dotClassName: "bg-status-running",
    },
    {
      value: "unread",
      label: t("chatPage.filter.options.unread"),
      dotClassName: "bg-status-waiting",
      badge: unreadCount,
    },
  ];

  const renderAgentGroup = (a: ChatAgentItem) => (
    <AgentGroupRow
      key={a.id}
      agent={a}
      selectedAgentId={selectedAgentId}
      selectedSessionId={selectedSessionId}
      openSession={openSession}
      openSessionInNewTab={openSessionInNewTab}
      openNewSession={openNewSession}
      showNotChattableNotice={showNotChattableNotice}
    />
  );

  return (
    <>
      {/* ── Left sidebar ── */}
      <ResizableSidebar persistenceKey="chat" ariaLabel={t("chatPage.sidebar")}>
        <div className="border-b border-border px-4 py-3">
          <div className="flex items-center gap-2">
            <Popover
              open={filterPopoverOpen}
              onOpenChange={setFilterPopoverOpen}
            >
              <PopoverTrigger asChild>
                <Button
                  type="button"
                  variant="outline"
                  size="icon-sm"
                  aria-label={t("chatPage.filter.open")}
                  title={t("chatPage.filter.open")}
                  className={cn(
                    "relative size-[30px] bg-sidebar",
                    sidebarFilter !== "all" && "border-ring text-primary-text",
                  )}
                >
                  <SlidersHorizontal data-icon="only" aria-hidden="true" />
                  {sidebarFilter !== "all" ? (
                    <span className="absolute right-1 top-1 size-1.5 rounded-full bg-destructive" />
                  ) : null}
                </Button>
              </PopoverTrigger>
              <PopoverContent className="w-[182px] p-1" align="start">
                <div className="flex flex-col gap-0.5">
                  {filterOptions.map((option) => (
                    <button
                      key={option.value}
                      type="button"
                      aria-pressed={sidebarFilter === option.value}
                      className={cn(
                        "flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-foreground outline-none transition-colors hover:bg-sidebar-active-bg focus-visible:ring-[3px] focus-visible:ring-ring/50",
                        sidebarFilter === option.value &&
                          "bg-sidebar-active-bg font-semibold",
                      )}
                      onClick={() => {
                        setSidebarFilter(option.value);
                        setFilterPopoverOpen(false);
                      }}
                    >
                      {option.dotClassName ? (
                        <span
                          aria-hidden="true"
                          className={cn(
                            "size-1.5 rounded-full",
                            option.dotClassName,
                          )}
                        />
                      ) : null}
                      <span className="min-w-0 flex-1 truncate">
                        {option.label}
                      </span>
                      {option.badge ? (
                        <span className="rounded-full bg-destructive px-1.5 font-mono text-2xs font-semibold text-destructive-foreground">
                          {option.badge}
                        </span>
                      ) : null}
                      {sidebarFilter === option.value ? (
                        <Check
                          className="size-3.5 text-primary-text"
                          aria-hidden="true"
                        />
                      ) : null}
                    </button>
                  ))}
                </div>
              </PopoverContent>
            </Popover>
            <div className="relative min-w-0 flex-1">
              <Search
                className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
                aria-hidden="true"
              />
              <Input
                aria-label={t("chatPage.search.aria")}
                placeholder={t("chatPage.search.placeholder")}
                className="h-[30px] bg-background pl-8 pr-7 text-xs"
                value={agentFilter}
                onChange={(event) => setAgentFilter(event.target.value)}
              />
              {agentFilter ? (
                <button
                  type="button"
                  aria-label={t("chatPage.search.clear")}
                  title={t("chatPage.search.clear")}
                  className="absolute right-1.5 top-1/2 inline-flex size-5 -translate-y-1/2 cursor-pointer items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  onClick={() => setAgentFilter("")}
                >
                  <X className="size-3" aria-hidden="true" />
                </button>
              ) : null}
            </div>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  variant="secondary"
                  size="icon-sm"
                  aria-label={t("chatPage.add.aria")}
                  title={t("chatPage.add.aria")}
                  className="size-[30px] bg-primary-soft text-primary-text hover:bg-primary-soft/80"
                >
                  <Plus data-icon="only" aria-hidden="true" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  onSelect={() => openCommandPalette(NEW_CHAT_INITIAL_QUERY)}
                >
                  {t("chatPage.add.newAgentChat")}
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setNewGroupOpen(true)}>
                  {t("chatPage.add.newGroup")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>

        {/* not-chattable inline 提示 */}
        {notChattableNotice ? (
          <div
            role="alert"
            aria-live="polite"
            className="mx-2 mt-2 rounded-md bg-muted px-3 py-2 text-xs text-muted-foreground"
          >
            <span className="font-semibold text-foreground">
              {notChattableNotice.name}
            </span>{" "}
            {t("chatPage.notChattable.message", {
              hint: notChattableNotice.hint,
            })}
          </div>
        ) : null}

        <div className="min-h-0 flex-1 overflow-auto px-2 py-3">
          {visibleGroups.length > 0 ? (
            <>
              <AgentPanelSection label={t("group.section")} />
              {visibleGroups.map((g) => (
                <GroupRow
                  key={g.id}
                  group={g}
                  selected={g.id === selectedGroupId}
                  onOpen={openGroup}
                />
              ))}
            </>
          ) : null}
          {pinned.length > 0 ? (
            <>
              <AgentPanelSection
                label={t("chatPage.sections.pinned")}
                icon="pin"
              />
              {pinned.map(renderAgentGroup)}
            </>
          ) : null}
          {others.length > 0 ? (
            <>
              <AgentPanelSection label={t("chatPage.sections.agents")} />
              {others.map(renderAgentGroup)}
            </>
          ) : null}
          {filterIsActive && !hasResults ? (
            <div className="px-2 py-6 text-center text-2xs text-muted-foreground">
              {t("chatPage.search.noMatches", {
                query: agentFilter.trim(),
              })}
            </div>
          ) : null}
        </div>
      </ResizableSidebar>
      <GroupNewDialog open={newGroupOpen} onOpenChange={setNewGroupOpen} />
    </>
  );
}

export { ChatPage };
