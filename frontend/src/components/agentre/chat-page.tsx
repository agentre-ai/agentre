import * as React from "react";
import { Plus, Search, UserPlus, X } from "lucide-react";
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
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import { reloadSidebarSources } from "@/stores/sidebar-reload";
import { requestNewAgentDialog } from "@/stores/new-agent-intent-store";
import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import { AgentGroup, AgentPanelSection } from "./agent-list";
import { NotChattableDialog } from "./not-chattable/not-chattable-dialog";
import type { AgentSession } from "./agent-list";
import { ResizableSidebar } from "./resizable-sidebar";
import { SessionsPopover } from "./sessions-popover";
import {
  DeleteChatSession,
  ListChatAgentSessions,
  RenameChatSession,
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
  // 会话行右键菜单 handler（可选）→ 透传给 SessionGroup / SessionRow。
  onRenameSession: (sessionId: number, title: string) => void;
  onDeleteSession: (sessionId: number) => void;
};

function AgentGroupRow({
  agent: a,
  selectedAgentId,
  selectedSessionId,
  openSession,
  openSessionInNewTab,
  openNewSession,
  openNotChattableDialog,
  onRenameSession,
  onDeleteSession,
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
      onOpenInNewTab={openSessionInNewTab}
      onRenameSession={onRenameSession}
      onDeleteSession={onDeleteSession}
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
  const { agents, loading } = useChatAgents();
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
  // 状态筛选 chips：单选。空集合 = 全部；非空时集合里就是当前唯一选中的状态。
  const [filterStatuses, setFilterStatuses] = React.useState<
    ReadonlySet<ChatSidebarStatus>
  >(() => new Set());
  const activeStatusFilter = React.useMemo<ChatSidebarStatus | null>(() => {
    if (filterStatuses.size !== 1) return null;
    return [...filterStatuses][0];
  }, [filterStatuses]);
  const selectStatusFilter = React.useCallback(
    (status: ChatSidebarStatus | null) => {
      setFilterStatuses(status ? new Set([status]) : new Set());
    },
    [],
  );
  const toggleStatusFilter = React.useCallback((status: ChatSidebarStatus) => {
    // 单选：再次点击已选中的 chip → 清空（回到「全部」）。
    setFilterStatuses((prev) => {
      if (prev.has(status) && prev.size === 1) return new Set();
      return new Set([status]);
    });
  }, []);
  // 会话行右键菜单的改名 / 删除对话框状态（镜像 chat-panel.tsx 的模式）。
  const [pendingRename, setPendingRename] = React.useState<{
    id: number;
    draft: string;
  } | null>(null);
  const [pendingDeleteId, setPendingDeleteId] = React.useState<number | null>(
    null,
  );
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
  // 列表：agent 按最近活跃倒序；pinned（DB 置顶的 agent）浮顶。
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
  const hasResults = visibleAgents.length > 0;

  // ── 会话行右键菜单：改名 / 删除 ──────────────────────────────────────────
  // 对话框 state 放在 ChatPage（镜像 chat-panel.tsx：pendingRename / pendingDeleteId）。
  const handleRenameSession = React.useCallback(
    (sessionId: number, title: string) => {
      setPendingRename({ id: sessionId, draft: title });
    },
    [],
  );
  const handleDeleteSession = React.useCallback((sessionId: number) => {
    setPendingDeleteId(sessionId);
  }, []);

  async function confirmRename() {
    if (!pendingRename) return;
    const next = pendingRename.draft.trim();
    if (!next) {
      setPendingRename(null);
      return;
    }
    const id = pendingRename.id;
    setPendingRename(null);
    await RenameChatSession({ sessionId: id, title: next });
    reloadSidebarSources();
  }

  async function confirmDelete() {
    const id = pendingDeleteId;
    if (id == null) return;
    setPendingDeleteId(null);
    await DeleteChatSession({ sessionId: id });
    // 与 ChatPanel 删除后关闭对应 tab 的行为保持一致。
    const openTabIds = useChatTabsStore
      .getState()
      .tabs.filter(
        (tab) => tab.meta.kind === "session" && tab.meta.sessionId === id,
      )
      .map((tab) => tab.id);
    for (const tabId of openTabIds) {
      useChatTabsStore.getState().closeTab(tabId);
    }
    reloadSidebarSources();
  }

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
      onRenameSession={handleRenameSession}
      onDeleteSession={handleDeleteSession}
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

      {/* 会话行右键菜单的删除确认对话框（镜像 chat-panel.tsx） */}
      <Dialog
        open={pendingDeleteId !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDeleteId(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("chatPanel.deleteDialog.title")}</DialogTitle>
          </DialogHeader>
          <DialogBody>
            <p className="text-sm text-muted-foreground">
              {t("chatPanel.deleteDialog.description")}
            </p>
          </DialogBody>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPendingDeleteId(null)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={() => void confirmDelete()}
            >
              {t("common.delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {/* 会话行右键菜单的改名对话框（镜像 chat-panel.tsx） */}
      <Dialog
        open={pendingRename !== null}
        onOpenChange={(open) => {
          if (!open) setPendingRename(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("chatPanel.renameDialog.title")}</DialogTitle>
          </DialogHeader>
          <DialogBody>
            <form
              id="sidebar-rename-session-form"
              onSubmit={(e) => {
                e.preventDefault();
                void confirmRename();
              }}
            >
              <Input
                autoFocus
                value={pendingRename?.draft ?? ""}
                onChange={(e) =>
                  setPendingRename((prev) =>
                    prev ? { ...prev, draft: e.target.value } : prev,
                  )
                }
                placeholder={t("chatPanel.renameDialog.placeholder")}
                aria-label={t("chatPanel.renameDialog.nameAria")}
              />
            </form>
          </DialogBody>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPendingRename(null)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              type="submit"
              form="sidebar-rename-session-form"
              size="sm"
              disabled={
                !pendingRename || pendingRename.draft.trim().length === 0
              }
            >
              {t("common.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ── Left sidebar ── */}
      <ResizableSidebar persistenceKey="chat" ariaLabel={t("chatPage.sidebar")}>
        <div className="border-b border-border px-4 py-3">
          <div className="flex items-center gap-2">
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
          {/* 状态筛选 chips：全部 / 运行中 / 未读 N —— 单选，替换原 SlidersHorizontal 下拉 */}
          <div className="mt-2 flex items-center gap-1.5">
            <button
              type="button"
              data-testid="filter-chip-all"
              aria-pressed={activeStatusFilter === null}
              className={cn(
                "inline-flex cursor-pointer items-center gap-1 rounded-full px-2.5 py-1 text-2xs outline-none transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50",
                activeStatusFilter === null
                  ? "bg-primary-soft font-medium text-primary-text"
                  : "bg-sidebar-active-bg text-muted-foreground hover:text-foreground",
              )}
              onClick={() => selectStatusFilter(null)}
            >
              {t("chatPage.filter.chips.all")}
            </button>
            <button
              type="button"
              data-testid="filter-chip-running"
              aria-pressed={activeStatusFilter === "running"}
              className={cn(
                "inline-flex cursor-pointer items-center gap-1 rounded-full px-2.5 py-1 text-2xs outline-none transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50",
                activeStatusFilter === "running"
                  ? "bg-primary-soft font-medium text-primary-text"
                  : "bg-sidebar-active-bg text-muted-foreground hover:text-foreground",
              )}
              onClick={() => toggleStatusFilter("running")}
            >
              {t("chatPage.filter.chips.running")}
            </button>
            <button
              type="button"
              data-testid="filter-chip-unread"
              aria-pressed={activeStatusFilter === "unread"}
              className={cn(
                "inline-flex cursor-pointer items-center gap-1 rounded-full px-2.5 py-1 text-2xs outline-none transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50",
                activeStatusFilter === "unread"
                  ? "bg-primary-soft font-medium text-primary-text"
                  : "bg-sidebar-active-bg text-muted-foreground hover:text-foreground",
              )}
              onClick={() => toggleStatusFilter("unread")}
            >
              {t("chatPage.filter.chips.unread")}
              {unreadCount > 0 ? (
                <span className="rounded-full bg-destructive px-1.5 font-mono text-2xs font-semibold text-destructive-foreground">
                  {unreadCount}
                </span>
              ) : null}
            </button>
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
          {agents.length === 0 && !loading ? (
            <div className="flex flex-col items-center gap-2 px-4 py-10 text-center">
              <div className="text-sm font-semibold text-foreground">
                {t("chatPage.empty.title")}
              </div>
              <div className="text-xs text-muted-foreground">
                {t("chatPage.empty.description")}
              </div>
              <Button
                type="button"
                size="sm"
                onClick={() => {
                  requestNewAgentDialog();
                  navigate("/org");
                }}
              >
                {t("chatPage.empty.cta")}
              </Button>
            </div>
          ) : filterIsActive && !hasResults ? (
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
