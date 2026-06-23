import * as React from "react";
import { useTranslation } from "react-i18next";

import { useChatAgents, type ChatAgentItem } from "@/hooks/use-chat-agents";
import i18n from "@/i18n";
import { cn } from "@/lib/utils";
import {
  readLastAgentId,
  writeLastAgentId,
} from "@/stores/last-agent-persistence";

import { AgentAvatar } from "../../primitives";
import type { AgentColor } from "../../types";
import { scoreItem } from "../score";
import type { CommandSource, OnSelectCtx } from "../types";

// 命令面板的 "New chat with <agent>" 命令源 —— 自由会话版。
// 仅在非 /projects 路由激活（项目路由用 newProjectChatSource，命令名 "New project chat with"）。
// 不读 useNewChatContextStore：即便残留 projectContext 也忽略，
// 该 source 的语义就是"不带项目作用域的新会话"。
export type NewChatItem = {
  key: string;
  agentId: number;
  agent: ChatAgentItem;
  // 自由会话版总把 agent 视为"成员"（无项目分组概念）；保留字段是为了 renderItem 同构。
  isMember: true;
};

// 排序：lastAgent → pinned → others。lastAgent 不存在（被删 / 未存）时退化为
// "pinned → others"，与历史行为一致。
export function flattenAgents(
  agents: ChatAgentItem[],
  lastAgentId: number | null = null,
): NewChatItem[] {
  const chattable = agents.filter((a) => a.chattable);
  const pinned = chattable.filter((a) => a.pinned);
  const others = chattable.filter((a) => !a.pinned);
  let ordered = [...pinned, ...others];
  if (lastAgentId != null) {
    const idx = ordered.findIndex((a) => a.id === lastAgentId);
    if (idx > 0) {
      const [last] = ordered.splice(idx, 1);
      ordered = [last, ...ordered];
    }
  }
  return ordered.map((agent) => ({
    key: `new-chat-agent-${agent.id}`,
    agentId: agent.id,
    agent,
    isMember: true,
  }));
}

function useItems(): { items: NewChatItem[]; loading: boolean } {
  const { agents, loading } = useChatAgents();
  // useState 懒初始化：同一次面板打开期间锁定值，避免 useChatAgents 更新
  // 触发"上次选过"位置漂移。下次面板打开时（组件 remount）重新读取。
  const [lastAgentId] = React.useState(() => readLastAgentId());
  const items = React.useMemo(
    () => flattenAgents(agents, lastAgentId),
    [agents, lastAgentId],
  );
  return { items, loading };
}

function actionTitle(item: NewChatItem): string {
  return i18n.t("commandPalette.newChat.itemTitle", {
    agentName: item.agent.name,
  });
}

const MULTI_TOKEN_SCORE = 25;

function getScore(query: string, item: NewChatItem): number {
  const title = actionTitle(item);
  const direct = scoreItem({
    query,
    title,
    subtitle: item.agent.name,
  });
  if (direct > 0) return direct;

  // Token-based fallback：多词查询每个 token 都需在 title 里出现（case-insensitive）。
  const tokens = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (tokens.length <= 1) return 0;
  const tl = title.toLowerCase();
  for (const tok of tokens) {
    if (!tl.includes(tok)) return 0;
  }
  return MULTI_TOKEN_SCORE;
}

function renderItem(item: NewChatItem): React.ReactNode {
  return <AgentRow item={item} />;
}

type AgentRowProps = { item: NewChatItem };

function AgentRow({ item }: AgentRowProps) {
  const { t } = useTranslation();
  const a = item.agent;
  return (
    <div
      data-testid={`agent-picker-item-${a.id}`}
      className="flex w-full items-center gap-3"
    >
      <AgentAvatar
        name={a.name}
        initials={a.name.charAt(0)}
        color={(a.avatarColor as AgentColor) || "agent-1"}
        avatarIcon={a.avatarIcon || undefined}
        avatarDataUrl={a.avatarDataUrl || undefined}
        size="md"
        className="size-7 rounded-md text-xs"
      />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div
          className="truncate text-sm text-foreground"
          title={t("commandPalette.newChat.itemTitle", { agentName: a.name })}
        >
          <span className="text-muted-foreground">
            {t("commandPalette.newChat.itemPrefix")}{" "}
          </span>
          <span className="font-medium">{a.name}</span>
        </div>
        {a.chattableHint ? (
          <div className="truncate text-2xs text-muted-foreground">
            {a.chattableHint}
          </div>
        ) : null}
      </div>
      <kbd
        className={cn(
          "rounded-sm border border-border bg-card px-1.5 py-0.5 font-mono text-2xs font-medium text-muted-foreground",
          "opacity-0 group-data-[selected=true]/cmditem:opacity-100",
        )}
        aria-hidden="true"
      >
        ↵
      </kbd>
    </div>
  );
}

export const PROJECTS_PATH_PREFIX = "/projects";

function isProjectsRoute(pathname: string): boolean {
  return (
    pathname === PROJECTS_PATH_PREFIX ||
    pathname.startsWith(`${PROJECTS_PATH_PREFIX}/`)
  );
}

function onSelect(item: NewChatItem, ctx: OnSelectCtx): void {
  ctx.close();
  writeLastAgentId(item.agentId);
  ctx.openNewSession(item.agentId);
  try {
    ctx.navigate("/chat");
  } catch (err) {
    console.warn("[command-palette] navigate('/chat') failed", err);
  }
}

export const newChatSource: CommandSource<NewChatItem> = {
  id: "new-chat",
  heading: i18n.t("commandPalette.newChat.heading"),
  modes: ["command"],
  activeFor: (ctx) => !isProjectsRoute(ctx.pathname),
  useItems,
  getScore,
  renderItem,
  onSelect,
};
