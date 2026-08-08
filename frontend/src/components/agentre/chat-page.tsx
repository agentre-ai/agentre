import * as React from "react";
import {
  Check,
  Plus,
  Search,
  SlidersHorizontal,
  UserPlus,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import type { TFunction } from "i18next";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
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
import { requestNewAgentDialog } from "@/stores/new-agent-intent-store";
import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import { AgentGroup, AgentPanelSection } from "./agent-list";
import { NotChattableDialog } from "./not-chattable/not-chattable-dialog";
import type { AgentSession } from "./agent-list";
import { ResizableSidebar } from "./resizable-sidebar";
import { SessionsPopover } from "./sessions-popover";
import {
  ListChatAgentSessions,
  SetAgentPinned,
} from "../../../wailsjs/go/app/App";
import type { AgentColor, AgentStatus } from "./types";

// ─── AgentSession builder ────────────────────────────────────────────────────

// agentSessionFromMeta: 从 meta-store 数据和 reason 投影成 AgentSession。
// 展开态常规列表（buildSessions 等价）和 attention bubble 共用。
export function agentSessionFromMeta(
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
    reason === "bg_running"
      ? (reasonToPillText(reason) ?? "")
      : status === "running"
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
  openNotChattableDialog: (agent: ChatAgentItem) => void;
};

function AgentGroupRow({
  agent: a,
  selectedAgentId,
  selectedSessionId,
  openSession,
  openSessionInNewTab,
  openNewSession,
  openNotChattableDialog,
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
      blockReason={a.blockReason}
      notChattable={!a.chattable}
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
        const first = a.sessions[0];
        if (first) {
          openSession(first.id);
          return;
        }
        if (!a.chattable) {
          openNotChattableDialog(a);
          return;
        }
        openNewSession(0, a.id, "");
      }}
      onNewSession={() => {
        if (!a.chattable) {
          openNotChattableDialog(a);
          return;
        }
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

// ─── Sidebar filter ──────────────────────────────────────────────────────────

// 侧栏状态筛选(多选切换):运行中 / 未读,互相独立组合。
type ChatSidebarStatus = "running" | "unread";

// AgentRow: 侧栏列表的一行。ts = 最近活跃时间(取其会话在 meta-store 的
// max(lastMessageAt));pinned 浮顶。
type AgentRow = { ts: number; pinned: boolean; agent: ChatAgentItem };

// 活跃度倒序：ts 大的在前；ts===0（无活跃）沉到底部，保持稳定。
function agentRowByActivity(a: AgentRow, b: AgentRow): number {
  if (a.ts === b.ts) return 0;
  if (a.ts === 0) return 1;
  if (b.ts === 0) return -1;
  return b.ts - a.ts;
}

function agentMatchesSearch(agent: ChatAgentItem, query: string): boolean {
  if (!query) return true;
  return (
    agent.name.toLowerCase().includes(query) ||
    agent.sessions.some((s) => s.title.toLowerCase().includes(query))
  );
}

// 状态（多选切换）过滤：空集合=不约束（全过）；否则命中任一选中状态即过（并集语义）。
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
  const metas = useSessionMetaStore((s) => s.metas);
  // 选中态完全派生自 chat-tabs-store(single source of truth):
  // - kind:"session" → selectedSessionId = meta.sessionId,
  //   selectedAgentId 反查 agents 找到拥有该 session 的 agent(用于 sidebar 高亮 +
  //   attention bubble 钉选中行)。
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
  const openCommandPalette = useCommandPaletteStore((s) => s.openWith);
  const navigate = useNavigate();
  const [agentFilter, setAgentFilter] = React.useState("");
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
  const [notChattableAgent, setNotChattableAgent] =
    React.useState<ChatAgentItem | null>(null);

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
  const visibleAgents = React.useMemo(
    () =>
      agents.filter(
        (a) =>
          agentMatchesSearch(a, filterValue) &&
          agentMatchesStatuses(a, filterStatuses, attentionReasons),
      ),
    [agents, filterValue, filterStatuses, attentionReasons],
  );
  // 列表：agent 按最近活跃倒序;pinned（系统 agent + 用户置顶的 agent）浮顶。
  // agent 活跃度取 sessionIds 在 meta-store 的 max(lastMessageAt)，确保 turn
  // 结束后实时反映。无活跃的项 ts=0 沉到底部。
  const agentRows = React.useMemo<AgentRow[]>(() => {
    const agentMaxTs = (a: ChatAgentItem): number => {
      const ids = a.sessionIds ?? a.sessions.map((s) => s.id);
      let max = 0;
      for (const sid of ids) {
        const ts = metas.get(sid)?.lastMessageAt ?? 0;
        if (ts > max) max = ts;
      }
      return max;
    };
    return visibleAgents.map<AgentRow>((a) => ({
      ts: agentMaxTs(a),
      pinned: a.pinned,
      agent: a,
    }));
  }, [visibleAgents, metas]);
  const pinnedRows = React.useMemo(
    () => agentRows.filter((r) => r.pinned).sort(agentRowByActivity),
    [agentRows],
  );
  const otherRows = React.useMemo(
    () => agentRows.filter((r) => !r.pinned).sort(agentRowByActivity),
    [agentRows],
  );

  const filterIsActive = filterValue.length > 0;
  const filtersActive = filterStatuses.size > 0;
  const hasResults = visibleAgents.length > 0;
  // 一条扁平竖向列表（mockup §2.1：无分区标题/分隔线）；status=多选 toggle（点击增删）。
  const filterOptions: Array<{
    value: ChatSidebarStatus;
    label: string;
    dotClassName: string;
    badge?: number;
  }> = [
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

  const renderRow = (row: AgentRow) => (
    <AgentGroupRow
      key={row.agent.id}
      agent={row.agent}
      selectedAgentId={selectedAgentId}
      selectedSessionId={selectedSessionId}
      openSession={openSession}
      openSessionInNewTab={openSessionInNewTab}
      openNewSession={openNewSession}
      openNotChattableDialog={setNotChattableAgent}
    />
  );

  return (
    <>
      {notChattableAgent ? (
        <NotChattableDialog
          agent={notChattableAgent}
          open
          onOpenChange={(open) => {
            if (!open) setNotChattableAgent(null);
          }}
        />
      ) : null}

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
                    // 状态=多选(选中=在集合里)。
                    const pressed = filterStatuses.has(option.value);
                    return (
                      <button
                        key={option.value}
                        type="button"
                        aria-pressed={pressed}
                        className={cn(
                          "flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-foreground outline-none transition-colors hover:bg-sidebar-active-bg focus-visible:ring-[3px] focus-visible:ring-ring/50",
                          pressed && "bg-sidebar-active-bg font-semibold",
                        )}
                        onClick={() => {
                          // 保持下拉打开,让用户连续组合多个状态。
                          toggleStatus(option.value);
                        }}
                      >
                        <span
                          aria-hidden="true"
                          className={cn(
                            "size-1.5 rounded-full",
                            option.dotClassName,
                          )}
                        />
                        <span className="min-w-0 flex-1 truncate">
                          {option.label}
                        </span>
                        {option.badge ? (
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
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  data-testid="new-agent-item"
                  onSelect={() => {
                    requestNewAgentDialog();
                    navigate("/org");
                  }}
                >
                  <UserPlus className="size-4" aria-hidden="true" />
                  <div className="flex min-w-0 flex-col gap-0.5">
                    <span>{t("chatPage.add.newAgent")}</span>
                    <span className="text-2xs text-muted-foreground">
                      {t("chatPage.add.newAgentHint")}
                    </span>
                  </div>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>

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
    </>
  );
}

export { ChatPage };
