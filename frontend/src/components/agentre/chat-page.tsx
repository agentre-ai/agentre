import * as React from "react";
import { Check, Pin, Plus, Search, SlidersHorizontal, X } from "lucide-react";
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
import { useChatAgentsStore } from "@/stores/chat-agents-store";
import { useGroupList, type GroupListItem } from "@/hooks/use-group-list";
import { useGroupListStore } from "@/stores/group-list-store";
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
import { GroupAvatar, StatusDot } from "./primitives";
import { GroupNewDialog } from "./group-chat/group-new-dialog";
import { ResizableSidebar } from "./resizable-sidebar";
import { SessionsPopover } from "./sessions-popover";
import {
  GroupSetPinned,
  ListChatAgentSessions,
  SetAgentPinned,
} from "../../../wailsjs/go/app/App";
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
  groupId?: number,
  groupTitle?: string,
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
    groupId,
    groupTitle,
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
          meta.groupId,
          meta.groupTitle,
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
            meta.groupId,
            meta.groupTitle,
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
        undefined,
        meta?.groupId ?? s.groupId,
        meta?.groupTitle ?? s.groupTitle,
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
      pinToggleLabel={
        a.pinned
          ? t("chatPage.pin.unpinAria", { name: a.name })
          : t("chatPage.pin.pinAria", { name: a.name })
      }
      onTogglePin={() => {
        void (async () => {
          await SetAgentPinned({ id: a.id, pinned: !a.pinned });
          await useChatAgentsStore.getState().reload();
        })();
      }}
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
  onTogglePin: (groupId: number, pinned: boolean) => void;
};

function GroupRow({ group, selected, onOpen, onTogglePin }: GroupRowProps) {
  const { t } = useTranslation();
  const dotStatus = groupRunStatusToDotStatus(group.runStatus);
  const isWaiting = dotStatus === "waiting";
  // 群尾部标签:等待你 > 轮次(已 N 轮) > 无。颜色与 SessionRow 的 trailingLabel 对齐 ——
  // 等待走 status-waiting 琥珀,其余 muted,选中态统一 primary-text。
  const tag = isWaiting
    ? t("group.runStatus.waitingUser")
    : group.roundCount > 0
      ? t("group.rounds", { count: group.roundCount })
      : null;
  const tagColorClass = selected
    ? "text-primary-text"
    : isWaiting
      ? "text-status-waiting"
      : "text-muted-foreground";
  const pinLabel = group.pinned
    ? t("chatPage.pin.unpinAria", { name: group.title })
    : t("chatPage.pin.pinAria", { name: group.title });
  return (
    <div
      className={cn(
        "group/grouprow flex w-full items-center gap-1 rounded-md pr-1 transition-colors hover:bg-sidebar-active-bg",
        selected && "bg-primary-soft",
      )}
    >
      <button
        type="button"
        aria-current={selected ? "true" : undefined}
        onClick={() => onOpen(group.id, group.title)}
        className={cn(
          "flex min-w-0 flex-1 cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50",
          selected && "text-primary-text",
        )}
      >
        <GroupAvatar data-testid="group-avatar" />
        <span
          className={cn(
            "min-w-0 flex-1 truncate",
            selected ? "font-medium text-primary-text" : "text-foreground",
          )}
        >
          {group.title}
        </span>
        {group.pinned ? (
          <Pin
            className="size-3 -rotate-[30deg] text-primary-text"
            aria-label={t("agentList.pinned")}
          />
        ) : null}
        <StatusDot status={dotStatus} size="xs" />
        {tag ? (
          <span className={cn("shrink-0 font-mono text-2xs", tagColorClass)}>
            {tag}
          </span>
        ) : null}
      </button>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        aria-label={pinLabel}
        title={pinLabel}
        className={cn(
          "text-muted-foreground",
          group.pinned && "text-primary-text",
        )}
        onClick={() => onTogglePin(group.id, !group.pinned)}
      >
        <Pin data-icon="only" aria-hidden="true" className="-rotate-[30deg]" />
      </Button>
    </div>
  );
}

// ─── Sidebar mixed filter ────────────────────────────────────────────────────

// 侧栏筛选两维（对齐 mockup §2.1）：类型单选 + 状态多选切换，互相独立组合。
type ChatSidebarType = "all" | "groups" | "agents";
type ChatSidebarStatus = "running" | "unread";

// MixedRow: 侧栏混排列表的一行，agent 与群同列。ts = 最近活跃时间（agent 取其会话
// 在 meta-store 的 max(lastMessageAt)，群取 Updatetime）；pinned 浮顶。
type MixedRow =
  | { kind: "agent"; ts: number; pinned: boolean; agent: ChatAgentItem }
  | { kind: "group"; ts: number; pinned: boolean; group: GroupListItem };

// 活跃度倒序：ts 大的在前；ts===0（无活跃）沉到底部，保持稳定。
function mixedRowByActivity(a: MixedRow, b: MixedRow): number {
  if (a.ts === b.ts) return 0;
  if (a.ts === 0) return 1;
  if (b.ts === 0) return -1;
  return b.ts - a.ts;
}

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

// 类型（单选）过滤：群在 "agents" 下隐藏，agent 在 "groups" 下隐藏，"all" 全过。
function groupMatchesType(filter: ChatSidebarType): boolean {
  return filter !== "agents";
}

function agentMatchesType(filter: ChatSidebarType): boolean {
  return filter !== "groups";
}

// 状态（多选切换）过滤：空集合=不约束（全过）；否则命中任一选中状态即过（并集语义）。
function groupMatchesStatuses(
  group: GroupListItem,
  statuses: ReadonlySet<ChatSidebarStatus>,
): boolean {
  if (statuses.size === 0) return true;
  if (statuses.has("running") && group.runStatus === "running") return true;
  // 群「未读」= 等待用户处理（runStatus==waiting_user），与 agent 的 attention 未读对齐。
  if (statuses.has("unread") && group.runStatus === "waiting_user") return true;
  return false;
}

function agentMatchesStatuses(
  agent: ChatAgentItem,
  statuses: ReadonlySet<ChatSidebarStatus>,
  attentionReasons: Map<number, AttentionReason>,
): boolean {
  if (statuses.size === 0) return true;
  const ids = agent.sessionIds ?? agent.sessions.map((s) => s.id);
  if (
    statuses.has("running") &&
    (agent.activeCount > 0 ||
      ids.some((sid) => attentionReasons.get(sid) === "running"))
  ) {
    return true;
  }
  if (
    statuses.has("unread") &&
    ids.some((sid) => attentionReasons.get(sid) === "unread")
  ) {
    return true;
  }
  return false;
}

// ─── Main ChatPage ───────────────────────────────────────────────────────────

function ChatPage() {
  const { t } = useTranslation();
  const { agents } = useChatAgents();
  const { groups } = useGroupList();
  const metas = useSessionMetaStore((s) => s.metas);
  // 选中态完全派生自 chat-tabs-store(single source of truth):
  // - kind:"session" / "groupSession" → selectedSessionId = meta.sessionId,
  //   selectedAgentId 反查 agents 找到拥有该 session 的 agent(用于 sidebar 高亮 +
  //   attention bubble 钉选中行)。群成员 backing session 也是普通 chat_session,
  //   选中态行为必须与普通会话一致。
  // - kind:"new"     → selectedSessionId = 0,selectedAgentId = meta.agentId。
  // - 无 active tab  → 全 0,sidebar 不高亮任何 agent。
  const activeTab = useChatTabsStore((s) =>
    s.activeTabId ? (s.tabs.find((t) => t.id === s.activeTabId) ?? null) : null,
  );
  const selectedSessionId =
    activeTab?.meta.kind === "session" ||
    activeTab?.meta.kind === "groupSession"
      ? activeTab.meta.sessionId
      : 0;
  const selectedAgentId = React.useMemo(() => {
    if (!activeTab) return 0;
    if (activeTab.meta.kind === "new") return activeTab.meta.agentId;
    if (
      activeTab.meta.kind !== "session" &&
      activeTab.meta.kind !== "groupSession"
    )
      return 0;
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
  const [filterType, setFilterType] = React.useState<ChatSidebarType>("all");
  const [filterStatuses, setFilterStatuses] = React.useState<
    ReadonlySet<ChatSidebarStatus>
  >(() => new Set());
  const toggleStatus = React.useCallback((status: ChatSidebarStatus) => {
    setFilterStatuses((prev) => {
      const next = new Set(prev);
      if (next.has(status)) next.delete(status);
      else next.add(status);
      return next;
    });
  }, []);
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
          groupMatchesType(filterType) &&
          groupMatchesStatuses(g, filterStatuses),
      ),
    [groups, filterValue, filterType, filterStatuses],
  );
  const visibleAgents = React.useMemo(
    () =>
      agents.filter(
        (a) =>
          agentMatchesSearch(a, filterValue) &&
          agentMatchesType(filterType) &&
          agentMatchesStatuses(a, filterStatuses, attentionReasons),
      ),
    [agents, filterValue, filterType, filterStatuses, attentionReasons],
  );
  // 混排：agent 与群合并成一个列表，按最近活跃倒序；pinned（系统 agent + 用户置顶的
  // agent/群）浮顶。agent 活跃度取 sessionIds 在 meta-store 的 max(lastMessageAt)，
  // 确保 turn 结束后实时反映；群活跃度取 Updatetime。无活跃的项 ts=0 沉到底部。
  const mixedRows = React.useMemo<MixedRow[]>(() => {
    const agentMaxTs = (a: ChatAgentItem): number => {
      const ids = a.sessionIds ?? a.sessions.map((s) => s.id);
      let max = 0;
      for (const sid of ids) {
        const ts = metas.get(sid)?.lastMessageAt ?? 0;
        if (ts > max) max = ts;
      }
      return max;
    };
    return [
      ...visibleAgents.map<MixedRow>((a) => ({
        kind: "agent",
        ts: agentMaxTs(a),
        pinned: a.pinned,
        agent: a,
      })),
      ...visibleGroups.map<MixedRow>((g) => ({
        kind: "group",
        ts: g.updatetime,
        pinned: g.pinned,
        group: g,
      })),
    ];
  }, [visibleAgents, visibleGroups, metas]);
  const pinnedRows = React.useMemo(
    () => mixedRows.filter((r) => r.pinned).sort(mixedRowByActivity),
    [mixedRows],
  );
  const otherRows = React.useMemo(
    () => mixedRows.filter((r) => !r.pinned).sort(mixedRowByActivity),
    [mixedRows],
  );

  const filterIsActive = filterValue.length > 0;
  const filtersActive = filterType !== "all" || filterStatuses.size > 0;
  const hasResults = visibleAgents.length > 0 || visibleGroups.length > 0;
  // 一条扁平竖向列表（mockup §2.1：无分区标题/分隔线）；kind 区分交互：
  // type=单选（点击切换当前类型），status=多选 toggle（点击增删）。
  const filterOptions: Array<
    | { kind: "type"; value: ChatSidebarType; label: string }
    | {
        kind: "status";
        value: ChatSidebarStatus;
        label: string;
        dotClassName: string;
        badge?: number;
      }
  > = [
    { kind: "type", value: "all", label: t("chatPage.filter.options.all") },
    {
      kind: "type",
      value: "groups",
      label: t("chatPage.filter.options.groups"),
    },
    {
      kind: "type",
      value: "agents",
      label: t("chatPage.filter.options.agents"),
    },
    {
      kind: "status",
      value: "running",
      label: t("chatPage.filter.options.running"),
      dotClassName: "bg-status-running",
    },
    {
      kind: "status",
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

  const toggleGroupPin = (groupId: number, pinned: boolean) => {
    void (async () => {
      await GroupSetPinned(groupId, pinned);
      await useGroupListStore.getState().reload();
    })();
  };

  const renderRow = (row: MixedRow) =>
    row.kind === "agent" ? (
      renderAgentGroup(row.agent)
    ) : (
      <GroupRow
        key={`group-${row.group.id}`}
        group={row.group}
        selected={row.group.id === selectedGroupId}
        onOpen={openGroup}
        onTogglePin={toggleGroupPin}
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
                    filtersActive && "border-ring text-primary-text",
                  )}
                >
                  <SlidersHorizontal data-icon="only" aria-hidden="true" />
                  {filtersActive ? (
                    <span className="absolute right-1 top-1 size-1.5 rounded-full bg-destructive" />
                  ) : null}
                </Button>
              </PopoverTrigger>
              <PopoverContent className="w-[182px] p-1" align="start">
                <div className="flex flex-col gap-0.5">
                  {filterOptions.map((option) => {
                    // 类型=单选(选中=当前类型);状态=多选(选中=在集合里)。
                    const pressed =
                      option.kind === "type"
                        ? filterType === option.value
                        : filterStatuses.has(option.value);
                    return (
                      <button
                        key={`${option.kind}:${option.value}`}
                        type="button"
                        aria-pressed={pressed}
                        className={cn(
                          "flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-foreground outline-none transition-colors hover:bg-sidebar-active-bg focus-visible:ring-[3px] focus-visible:ring-ring/50",
                          pressed && "bg-sidebar-active-bg font-semibold",
                        )}
                        onClick={() => {
                          // 保持下拉打开,让用户连续组合类型 + 多个状态。
                          if (option.kind === "type")
                            setFilterType(option.value);
                          else toggleStatus(option.value);
                        }}
                      >
                        {option.kind === "status" ? (
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
                        {option.kind === "status" && option.badge ? (
                          <span className="rounded-full bg-destructive px-1.5 font-mono text-2xs font-semibold text-destructive-foreground">
                            {option.badge}
                          </span>
                        ) : null}
                        {pressed ? (
                          <Check
                            className="size-3.5 text-primary-text"
                            aria-hidden="true"
                          />
                        ) : null}
                      </button>
                    );
                  })}
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
                  data-testid="new-chat-button"
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
                  data-testid="new-agent-chat-item"
                  onSelect={() => openCommandPalette(NEW_CHAT_INITIAL_QUERY)}
                >
                  {t("chatPage.add.newAgentChat")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  data-testid="new-group-item"
                  onSelect={() => setNewGroupOpen(true)}
                >
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
          {pinnedRows.length > 0 ? (
            <>
              <AgentPanelSection
                label={t("chatPage.sections.pinned")}
                icon="pin"
              />
              {pinnedRows.map(renderRow)}
            </>
          ) : null}
          {otherRows.map(renderRow)}
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
