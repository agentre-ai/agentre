import * as React from "react";
import { Search, X } from "lucide-react";

import { Input } from "@/components/ui/input";
import { useChatAgents, type ChatAgentItem } from "@/hooks/use-chat-agents";
import {
  reasonToDisplayStatus,
  reasonToPillText,
} from "@/lib/attention-display";
import { relativeTime } from "@/lib/relative-time";
import {
  useSessionAttentionList,
  type AttentionReason,
} from "@/stores/attention-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import { AgentGroup, AgentPanelSection } from "./agent-list";
import type { AgentSession } from "./agent-list";
import { ResizableSidebar } from "./resizable-sidebar";
import { SessionsPopover } from "./sessions-popover";
import { ListChatAgentSessions } from "../../../wailsjs/go/app/App";
import type { AgentColor, AgentStatus } from "./types";

// ─── AgentSession builder ────────────────────────────────────────────────────

// agentSessionFromMeta: 从 meta-store 数据和 reason 投影成 AgentSession。
// 展开态常规列表（buildSessions 等价）和 attention bubble 共用。
function agentSessionFromMeta(
  sid: number,
  title: string,
  lastMessageAt: number,
  agentStatus: string,
  reason: AttentionReason | null,
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
    title: title || "(未命名会话)",
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
  ]);
}

// buildSessions: 投影展开态侧栏常规列表（从 meta/status store 读，无需 attentionSessions）。
function useBuildSessions(agent: ChatAgentItem): AgentSession[] {
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
      );
    });
  }, [agent.sessions, metas, statuses, attentionMap]);
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
            a.chattableHint || "请先在组织架构页给该 Agent 绑定一个内置后端",
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

// ─── Main ChatPage ───────────────────────────────────────────────────────────

function ChatPage() {
  const { agents } = useChatAgents();
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
    const sid = activeTab.meta.sessionId;
    for (const a of agents) {
      if (a.sessions.some((s) => s.id === sid)) return a.id;
    }
    return 0;
  }, [activeTab, agents]);
  const openSession = useChatTabsStore((s) => s.openSession);
  const openSessionInNewTab = useChatTabsStore((s) => s.openSessionInNewTab);
  const openNewSession = useChatTabsStore((s) => s.openNewSession);
  const [agentFilter, setAgentFilter] = React.useState("");
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
  const visibleAgents = filterValue
    ? agents.filter(
        (a) =>
          a.name.toLowerCase().includes(filterValue) ||
          a.sessions.some((s) => s.title.toLowerCase().includes(filterValue)),
      )
    : agents;
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
  const hasResults = visibleAgents.length > 0;

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
      <ResizableSidebar persistenceKey="chat" ariaLabel="Agent 列表">
        <div className="flex flex-col gap-2 border-b border-border px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold">Agents</span>
            <span className="font-mono text-2xs text-muted-foreground">
              {agents.length}
            </span>
            <div className="min-w-0 flex-1" />
          </div>
          <div className="relative">
            <Search
              className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
              aria-hidden="true"
            />
            <Input
              aria-label="筛选 agent 与会话"
              placeholder="搜索 Agent / 会话"
              className="h-[30px] bg-input-bg pl-8 pr-7 text-xs"
              value={agentFilter}
              onChange={(event) => setAgentFilter(event.target.value)}
            />
            {agentFilter ? (
              <button
                type="button"
                aria-label="清空筛选"
                title="清空筛选"
                className="absolute right-1.5 top-1/2 inline-flex size-5 -translate-y-1/2 cursor-pointer items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                onClick={() => setAgentFilter("")}
              >
                <X className="size-3" aria-hidden="true" />
              </button>
            ) : null}
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
            暂不可对话 — {notChattableNotice.hint}
          </div>
        ) : null}

        <div className="min-h-0 flex-1 overflow-auto px-2 py-3">
          {pinned.length > 0 ? (
            <>
              <AgentPanelSection label="PINNED" icon="pin" />
              {pinned.map(renderAgentGroup)}
            </>
          ) : null}
          {hasResults ? (
            <>
              <AgentPanelSection label="AGENTS" />
              {others.map(renderAgentGroup)}
            </>
          ) : null}
          {filterIsActive && !hasResults ? (
            <div className="px-2 py-6 text-center text-2xs text-muted-foreground">
              没有匹配 "{agentFilter.trim()}" 的 Agent 或会话
            </div>
          ) : null}
        </div>
      </ResizableSidebar>
    </>
  );
}

export { ChatPage };
