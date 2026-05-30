// frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx
import * as React from "react";
import { Sparkles } from "lucide-react";
import { useTranslation } from "react-i18next";

import { ChatPanel } from "../chat-panel";
import { TerminalPanel } from "../terminal/terminal-panel";
import { reloadSidebarSources } from "@/stores/sidebar-reload";
import type { ChatTab, TabKind } from "@/stores/chat-tabs-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useChatAgentsStore } from "@/stores/chat-agents-store";

type PanelOrderState = {
  tabs: ChatTab[];
  order: string[];
};

function reconcilePanelOrder(order: string[], tabs: ChatTab[]) {
  const liveIds = new Set(tabs.map((tab) => tab.id));
  const next = order.filter((id) => liveIds.has(id));
  const knownIds = new Set(next);

  for (const tab of tabs) {
    if (knownIds.has(tab.id)) continue;
    next.push(tab.id);
    knownIds.add(tab.id);
  }

  return next;
}

function tabsByPanelOrder(tabs: ChatTab[], order: string[]) {
  const byId = new Map(tabs.map((tab) => [tab.id, tab]));
  return order
    .map((id) => byId.get(id))
    .filter((tab): tab is ChatTab => Boolean(tab));
}

export function ChatPanelHost() {
  const { t } = useTranslation();
  const tabs = useChatTabsStore((s) => s.tabs);
  const activeTabId = useChatTabsStore((s) => s.activeTabId);
  const [panelOrderState, setPanelOrderState] = React.useState<PanelOrderState>(
    () => ({
      tabs,
      order: reconcilePanelOrder([], tabs),
    }),
  );

  let panelOrder = panelOrderState.order;
  if (panelOrderState.tabs !== tabs) {
    panelOrder = reconcilePanelOrder(panelOrderState.order, tabs);
    setPanelOrderState({ tabs, order: panelOrder });
  }

  const panelTabs = React.useMemo(
    () => tabsByPanelOrder(tabs, panelOrder),
    [panelOrder, tabs],
  );

  if (tabs.length === 0) {
    return (
      <main className="flex flex-1 flex-col items-center justify-center gap-3 bg-background px-8 text-center">
        <span className="inline-flex size-14 items-center justify-center rounded-lg border border-border bg-primary-soft">
          <Sparkles className="size-6 text-primary" aria-hidden="true" />
        </span>
        <div className="text-base font-semibold">
          {t("chatTabs.empty.title")}
        </div>
        <div className="text-xs text-muted-foreground">
          {t("chatTabs.empty.description")}
        </div>
        <div className="mt-2 flex items-center gap-3 text-xs text-muted-foreground">
          <kbd className="rounded-md border border-border bg-card px-2 py-1 font-mono">
            ⌘1..⌘9
          </kbd>
          {t("chatTabs.empty.shortcuts.switch")}
          <kbd className="rounded-md border border-border bg-card px-2 py-1 font-mono">
            ⌘W
          </kbd>
          {t("chatTabs.empty.shortcuts.close")}
          <kbd className="rounded-md border border-border bg-card px-2 py-1 font-mono">
            ⌘ Click
          </kbd>
          {t("chatTabs.empty.shortcuts.openInNewTab")}
        </div>
      </main>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      {panelTabs.map((t) =>
        t.meta.kind === "terminal" ? (
          <HostedTerminalPanel
            key={t.id}
            tab={t}
            active={t.id === activeTabId}
          />
        ) : (
          <HostedPanel key={t.id} tab={t} active={t.id === activeTabId} />
        ),
      )}
    </div>
  );
}

function HostedPanel({ tab, active }: { tab: ChatTab; active: boolean }) {
  const sid = tab.meta.kind === "session" ? tab.meta.sessionId : 0;
  const isNewTab = tab.meta.kind === "new";
  const newAgentId = tab.meta.kind === "new" ? tab.meta.agentId : 0;
  const newProjectId = tab.meta.kind === "new" ? tab.meta.projectId : 0;
  const agents = useChatAgentsStore((s) => s.agents);
  const agentsLoading = useChatAgentsStore((s) => s.loading);
  const agentsError = useChatAgentsStore((s) => s.error);
  const reloadAgents = useChatAgentsStore((s) => s.reload);
  const agent = isNewTab
    ? (agents.find((a) => a.id === newAgentId) ?? null)
    : null;
  const resolveNewTab = useChatTabsStore((s) => s.resolveNewTab);
  const closeTab = useChatTabsStore((s) => s.closeTab);
  const reloadMissingAgentRef = React.useRef<number | null>(null);

  // 每次该 Tab 从隐藏切到 active(包括 tab-strip 单击、overflow menu、⌘1..⌘9、
  // cmd+click 新开后激活、closeTab 后自动激活相邻 tab),把焦点交回 TipTap 输入框。
  // ProseMirror contenteditable 支持原生 .focus(),光标停在上次位置即可,不需要
  // 强行跳到末尾。
  const wrapperRef = React.useRef<HTMLDivElement>(null);
  const prevActiveRef = React.useRef<boolean | null>(null);
  const prevNewAgentReadyRef = React.useRef(false);
  const newAgentReady = isNewTab && !!agent;
  React.useEffect(() => {
    const prev = prevActiveRef.current;
    const prevNewAgentReady = prevNewAgentReadyRef.current;
    prevActiveRef.current = active;
    prevNewAgentReadyRef.current = newAgentReady;
    if (
      !active ||
      (prev === true && (!isNewTab || prevNewAgentReady || !newAgentReady))
    ) {
      return;
    }
    const editor = wrapperRef.current?.querySelector<HTMLElement>(
      "[contenteditable='true']",
    );
    if (!editor) return;
    // 用 microtask 等 display:none → flex 切换完, Radix 菜单 / popover 关闭时
    // 的焦点夺回也已让出,再 focus 才能稳稳落到编辑器上。
    const id = window.setTimeout(() => editor.focus(), 0);
    return () => window.clearTimeout(id);
  }, [active, isNewTab, newAgentReady]);

  React.useEffect(() => {
    if (!isNewTab || agent) return;
    if (reloadMissingAgentRef.current === newAgentId) return;
    reloadMissingAgentRef.current = newAgentId;
    void reloadAgents();
  }, [agent, isNewTab, newAgentId, reloadAgents]);

  return (
    <div
      ref={wrapperRef}
      data-tab-id={tab.id}
      data-active={active}
      style={{ display: active ? "flex" : "none" }}
      className="flex h-full min-h-0 flex-1 flex-col"
    >
      {isNewTab && !agent ? (
        <MissingNewSessionAgent
          agentId={newAgentId}
          loading={agentsLoading}
          error={agentsError}
        />
      ) : (
        <ChatPanel
          active={active}
          sessionId={sid}
          newSessionAgent={isNewTab ? agent : null}
          newSessionContext={isNewTab ? { projectId: newProjectId } : undefined}
          onSessionCreated={(newSid) => resolveNewTab(tab.id, newSid)}
          onSessionDeleted={() => closeTab(tab.id)}
          onSidebarShouldReload={() => {
            // 统一信号: 让 /chat (chat-agents-store) 与 /projects
            // (project-sessions-store) 两边的 sidebar 都同步刷新。新建会话 /
            // 删除会话 / 改标题 / turn 结束等 RPC 完成都走这里, 不必等下次
            // mount。两个 store 各自 inflight dedup, 调用安全。
            reloadSidebarSources();
          }}
        />
      )}
    </div>
  );
}

function HostedTerminalPanel({
  tab,
  active,
}: {
  tab: ChatTab;
  active: boolean;
}) {
  const closeTab = useChatTabsStore((s) => s.closeTab);
  const meta = tab.meta as Extract<TabKind, { kind: "terminal" }>;
  return (
    <div
      data-tab-id={tab.id}
      data-active={active}
      style={{ display: active ? "flex" : "none" }}
      className="flex h-full min-h-0 flex-1 flex-col"
    >
      <TerminalPanel
        terminalID={meta.terminalId}
        projectId={meta.projectId}
        deviceId={meta.deviceId}
        active={active}
        onClose={() => closeTab(tab.id)}
      />
    </div>
  );
}

function MissingNewSessionAgent({
  agentId,
  loading,
  error,
}: {
  agentId: number;
  loading: boolean;
  error: string | null;
}) {
  const { t } = useTranslation();
  const title = loading
    ? t("chatTabs.missingAgent.loading")
    : error
      ? t("chatTabs.missingAgent.loadFailed")
      : t("chatTabs.missingAgent.notFound");
  const detail = error
    ? error
    : loading
      ? `Agent #${agentId}`
      : t("chatTabs.missingAgent.detail", { id: agentId });

  return (
    <main className="flex min-h-0 min-w-0 flex-1 flex-col items-center justify-center gap-2 bg-background px-8 text-center">
      <div className="text-sm font-semibold">{title}</div>
      <div className="max-w-md text-xs text-muted-foreground">{detail}</div>
    </main>
  );
}
