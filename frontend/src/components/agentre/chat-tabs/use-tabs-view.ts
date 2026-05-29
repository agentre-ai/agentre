// frontend/src/components/agentre/chat-tabs/use-tabs-view.ts
import * as React from "react";
import { useTranslation } from "react-i18next";

import { useProjectTree } from "@/hooks/use-project-tree";
import { reasonToPillText } from "@/lib/attention-display";
import { findProjectColorToken, projectChain } from "@/lib/project-chain";
import {
  useSessionAttentionList,
  type AttentionReason,
} from "@/stores/attention-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import type { AgentColor } from "../types";
import { agentColorOrder } from "../types";
import type { TabStatus } from "./tab";

export type TabView = {
  id: string;
  title: string;
  kind: "session" | "new" | "terminal";
  avatar: { letter: string; color: string };
  isPreview: boolean;
  isPinned: boolean;
  status: TabStatus;
  projectColor: string | null;
  worktree: boolean;
  pillText: string | null;
  sessionId: number;
  projectChain: string[] | null;
  worktreeBranch: string | null;
  lastMessageAt: number;
};

const AGENT_COLOR_SET = new Set<string>(agentColorOrder);

function tokenToCssColor(token: string | null | undefined): string | null {
  if (!token) return null;
  if (!AGENT_COLOR_SET.has(token as AgentColor)) return null;
  return `var(--${token})`;
}

function firstLetter(name: string | null | undefined): string {
  if (!name) return "?";
  const trimmed = name.trim();
  if (!trimmed) return "?";
  return Array.from(trimmed)[0] ?? "?";
}

export function useTabsView(): TabView[] {
  const { t } = useTranslation();
  const tabs = useChatTabsStore((s) => s.tabs);
  const statuses = useSessionStatusStore((s) => s.statuses);
  const metas = useSessionMetaStore((s) => s.metas);
  const { tree } = useProjectTree();

  const sessionTabIds = React.useMemo(
    () =>
      tabs
        .filter((t) => t.meta.kind === "session")
        .map(
          (t) => (t.meta as { kind: "session"; sessionId: number }).sessionId,
        ),
    [tabs],
  );
  const attentionItems = useSessionAttentionList(sessionTabIds);
  const attentionBySid = React.useMemo(() => {
    const m = new Map<number, AttentionReason>();
    for (const x of attentionItems) m.set(x.sessionId, x.reason);
    return m;
  }, [attentionItems]);

  return tabs.map((tab) => {
    const sid = tab.meta.kind === "session" ? tab.meta.sessionId : 0;
    const live = sid ? statuses.get(sid) : undefined;
    const reason = sid ? (attentionBySid.get(sid) ?? null) : null;
    const agentStatus = live?.agentStatus;
    const status: TabStatus =
      agentStatus === "running"
        ? "running"
        : agentStatus === "waiting"
          ? "waiting"
          : agentStatus === "error"
            ? "error"
            : "idle";
    const pillText =
      status === "error"
        ? t("chatTabs.status.errorLabel")
        : reasonToPillText(reason);
    const meta = sid ? (metas.get(sid) ?? null) : null;
    const avatarColor = tokenToCssColor(meta?.agentColor) ?? "#94a3b8";
    const avatarLetter = firstLetter(meta?.agentName);
    const pid = meta?.projectId ?? 0;
    const projectColor =
      pid > 0 ? tokenToCssColor(findProjectColorToken(tree, pid)) : null;
    const chain = pid > 0 ? projectChain(tree, pid) : [];
    return {
      id: tab.id,
      title: meta?.title ?? tab.title ?? t("chatTabs.fallbackSession"),
      kind: tab.meta.kind,
      avatar: { letter: avatarLetter, color: avatarColor },
      isPreview: tab.isPreview,
      isPinned: tab.isPinned,
      status,
      projectColor,
      worktree: false,
      pillText,
      sessionId: sid,
      projectChain: chain.length > 0 ? chain : null,
      worktreeBranch: null,
      lastMessageAt: meta?.lastMessageAt ?? 0,
    };
  });
}
