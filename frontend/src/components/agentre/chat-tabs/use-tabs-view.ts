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

import {
  avatarFromMeta,
  firstLetter,
  tokenToCssColor,
} from "../session-avatar";
import type { TabStatus } from "./tab";

export type TabView = {
  id: string;
  title: string;
  kind: "session" | "groupSession" | "new" | "terminal" | "group";
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

export function useTabsView(): TabView[] {
  const { t } = useTranslation();
  const tabs = useChatTabsStore((s) => s.tabs);
  const statuses = useSessionStatusStore((s) => s.statuses);
  const metas = useSessionMetaStore((s) => s.metas);
  const { tree } = useProjectTree();

  const sessionTabIds = React.useMemo(
    () => tabs.map((t) => sessionIdOf(t.meta)).filter((sid) => sid > 0),
    [tabs],
  );
  const attentionItems = useSessionAttentionList(sessionTabIds);
  const attentionBySid = React.useMemo(() => {
    const m = new Map<number, AttentionReason>();
    for (const x of attentionItems) m.set(x.sessionId, x.reason);
    return m;
  }, [attentionItems]);

  return tabs.map((tab) => {
    // group tab:标题由 meta 自带(openGroup 时透传),不走 session-meta 反查。
    // 群聊在 Tab 内用「群组」图标头像与普通 agent 区分(见 tab.tsx kind==="group"),
    // 这里的 letter/color 只是 TabView 必填字段的占位 fallback,不参与群聊渲染;
    // 状态恒为 idle(群运行态在面板内展示,tab strip 不重复表达)。
    if (tab.meta.kind === "group") {
      const groupTitle = tab.meta.title || t("chatTabs.fallbackSession");
      return {
        id: tab.id,
        title: groupTitle,
        kind: "group" as const,
        avatar: { letter: firstLetter(groupTitle), color: "#94a3b8" },
        isPreview: tab.isPreview,
        isPinned: tab.isPinned,
        status: "idle" as const,
        projectColor: null,
        worktree: false,
        pillText: null,
        sessionId: 0,
        projectChain: null,
        worktreeBranch: null,
        lastMessageAt: 0,
      };
    }
    const sid = sessionIdOf(tab.meta);
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
    const pid = meta?.projectId ?? 0;
    const projectColor =
      pid > 0 ? tokenToCssColor(findProjectColorToken(tree, pid)) : null;
    const chain = pid > 0 ? projectChain(tree, pid) : [];
    return {
      id: tab.id,
      title: meta?.title ?? tab.title ?? t("chatTabs.fallbackSession"),
      kind: tab.meta.kind,
      avatar: avatarFromMeta(meta),
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

function sessionIdOf(meta: { kind: string; sessionId?: number }): number {
  if (meta.kind !== "session" && meta.kind !== "groupSession") return 0;
  return meta.sessionId ?? 0;
}
