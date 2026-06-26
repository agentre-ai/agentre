import { useTranslation } from "react-i18next";
import {
  Check,
  CornerDownRight,
  CornerUpLeft,
  Crown,
  MessageCircle,
  MessageSquare,
  TriangleAlert,
} from "lucide-react";
import { cn } from "@/lib/utils";
import type { app } from "../../../../wailsjs/go/models";
import { useChatAgents } from "@/hooks/use-chat-agents";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { AgentAvatar } from "../primitives";
import type { AgentColor } from "../types";
import { buildFeed, type FeedItem } from "./feed-data";

// 合并三次 agents.find() 为单次查找，返回所有渲染所需字段
function resolveAgent(
  agentId: number,
  agents: ReturnType<typeof useChatAgents>["agents"],
): {
  name: string;
  color: AgentColor;
  avatarDataUrl?: string;
  avatarIcon?: string;
} {
  const agent = agents.find((a) => a.id === agentId);
  return {
    name: agent?.name ?? `#${agentId}`,
    color: (agent?.avatarColor as AgentColor) || "agent-1",
    avatarDataUrl: agent?.avatarDataUrl || undefined,
    avatarIcon: agent?.avatarIcon || undefined,
  };
}

// badge 静态标签（ask/reply 走 kindAsk/kindReply）
function kindLabel(
  kind: FeedItem["kind"],
  t: ReturnType<typeof useTranslation>["t"],
): string | null {
  switch (kind) {
    case "dispatch":
      return t("orchestration.feed.kindDispatch");
    case "report":
      return t("orchestration.feed.kindReport");
    case "finish":
      return t("orchestration.feed.kindFinish");
    case "blocked":
      return t("orchestration.feed.kindBlocked");
    case "ask":
      return t("orchestration.feed.kindAsk");
    case "reply":
      return t("orchestration.feed.kindReply");
    default:
      return null;
  }
}

// badge 样式映射（header 行右侧小徽章）
function kindBadgeClass(kind: FeedItem["kind"]): string {
  switch (kind) {
    case "dispatch":
      return "bg-primary-soft text-primary-text";
    case "report":
    case "finish":
      return "bg-status-running-bg text-status-running";
    case "blocked":
      return "bg-destructive-soft text-destructive";
    case "ask":
      return "bg-status-waiting-bg text-status-waiting";
    case "reply":
      return "bg-status-running-bg text-status-running";
    default:
      return "bg-secondary text-muted-foreground";
  }
}

// badge 图标映射（每种 kind 对应的 lucide 图标）
function KindBadgeIcon({ kind }: { kind: FeedItem["kind"] }) {
  const cls = "size-2.5";
  switch (kind) {
    case "dispatch":
      return <CornerDownRight className={cls} />;
    case "finish":
    case "report":
      return <Check className={cls} />;
    case "ask":
      return <MessageCircle className={cls} />;
    case "reply":
      return <CornerUpLeft className={cls} />;
    case "blocked":
      return <TriangleAlert className={cls} />;
    default:
      return <MessageSquare className={cls} />;
  }
}

export function ActivityFeed({ detail }: { detail: app.RunDetailDTO }) {
  const { t } = useTranslation();
  const { agents } = useChatAgents();

  // 从 store 读取该 run 的 askLog（undefined 时用稳定空数组）
  const runId = detail.run?.id;
  const askLog =
    useOrchRunStore((s) => (runId ? s.askLog.get(runId) : undefined)) ?? [];

  const tasks = detail.tasks ?? [];

  // Leader agent id（用于渲染皇冠 chip）
  const leaderAgentId = detail.run?.leaderAgentId;

  // 构造 feed 条目列表（含 ask/reply 日志）
  const feedItems = buildFeed(detail, askLog);

  // 找到当前 running 的任务中，取第一个非根任务（有 parentTaskId）的运行中任务
  // 用于 typing 行显示；若无则取所有 running 任务里第一个
  const runningTask =
    tasks.find((t) => t.status === "running" && t.parentTaskId) ??
    tasks.find((t) => t.status === "running");

  return (
    <div className="flex h-full flex-col bg-background">
      {/* 时间线 feed，可滚动 */}
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        {feedItems.length === 0 ? (
          <p className="text-center text-sm text-muted-foreground">
            {t("orchestration.feed.empty")}
          </p>
        ) : (
          <ul className="flex flex-col gap-3.5">
            {feedItems.map((item) => {
              const agent = resolveAgent(item.agentId, agents);
              const {
                name: agentName,
                color: agentColor,
                ...agentAvatarProps
              } = agent;
              const isLeader =
                leaderAgentId !== undefined && item.agentId === leaderAgentId;

              // blocked 类型且 text 为空时使用 i18n fallback 文案
              const displayText =
                item.kind === "blocked" && !item.text.trim()
                  ? t("orchestration.feed.blocked")
                  : item.text;

              const label = kindLabel(item.kind, t);

              // ask/reply 使用专属 testid；ev 行无 testid
              const testId =
                item.kind === "ask" || item.kind === "reply"
                  ? `feed-${item.kind}-${item.id.replace(/^(ask|reply)-/, "")}`
                  : undefined;

              return (
                <li
                  key={item.id}
                  data-testid={testId}
                  className="flex items-start gap-2.5"
                >
                  {/* Avatar: 24px round */}
                  <AgentAvatar
                    name={agentName}
                    color={agentColor}
                    size="sm"
                    className="mt-0.5 shrink-0 rounded-full"
                    {...agentAvatarProps}
                  />
                  {/* Body: flex-col gap-1 */}
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    {/* Header row: name + optional crown + kind badge + spacer + timestamp */}
                    <div className="flex items-center gap-1.5">
                      <span className="text-[12.5px] font-semibold text-foreground">
                        {agentName}
                      </span>
                      {/* Crown chip: only for Leader */}
                      {isLeader && (
                        <span
                          data-testid="feed-leader-crown"
                          className="inline-flex items-center gap-[3px] rounded-full bg-primary-soft px-1.5 py-px text-[10px] font-semibold text-primary-text"
                        >
                          <Crown className="size-2.5" />
                          {t("orchestration.header.leaderLabel")}
                        </span>
                      )}
                      {/* Kind badge */}
                      {label && (
                        <span
                          className={cn(
                            "inline-flex items-center gap-1 rounded-full px-[7px] py-0.5 text-[10px] font-semibold",
                            kindBadgeClass(item.kind),
                          )}
                        >
                          <KindBadgeIcon kind={item.kind} />
                          {label}
                          {(item.kind === "ask" || item.kind === "reply") &&
                            item.targetAgentId !== undefined && (
                              <span>
                                {"@"}
                                {resolveAgent(item.targetAgentId, agents).name}
                              </span>
                            )}
                        </span>
                      )}
                      {/* flex spacer */}
                      <span className="flex-1" />
                      {/* timestamp */}
                      <span className="shrink-0 font-mono text-[10px] text-subtle-foreground">
                        {new Date(item.ts).toLocaleTimeString([], {
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
                      </span>
                    </div>
                    {/* Message text */}
                    <p className="break-words text-[12.5px] leading-[1.4] text-foreground">
                      {displayText}
                    </p>
                  </div>
                </li>
              );
            })}
          </ul>
        )}

        {/* Typing row: only rendered when there is an active running task */}
        {runningTask && (
          <div
            data-testid="feed-typing-row"
            className="mt-3.5 flex items-center gap-2.5"
          >
            {(() => {
              const ra = resolveAgent(runningTask.agentId, agents);
              return (
                <>
                  {/* Avatar */}
                  <AgentAvatar
                    name={ra.name}
                    color={ra.color}
                    size="sm"
                    className="shrink-0 rounded-full"
                    avatarDataUrl={ra.avatarDataUrl}
                    avatarIcon={ra.avatarIcon}
                  />
                  {/* Text + dots */}
                  <div className="flex items-center gap-1.5">
                    <span className="text-[12px] text-muted-foreground">
                      {ra.name} {t("orchestration.feed.executing")}{" "}
                      <span className="font-mono">
                        #{runningTask.callSeq ?? runningTask.id}
                      </span>
                      {runningTask.brief ? ` · ${runningTask.brief}` : ""}
                    </span>
                    {/* 3 animated dots */}
                    <span className="inline-flex items-center gap-0.5">
                      <span className="size-1 rounded-full bg-subtle-foreground motion-safe:animate-pulse" />
                      <span className="size-1 rounded-full bg-subtle-foreground motion-safe:animate-pulse [animation-delay:150ms]" />
                      <span className="size-1 rounded-full bg-subtle-foreground motion-safe:animate-pulse [animation-delay:300ms]" />
                    </span>
                  </div>
                </>
              );
            })()}
          </div>
        )}
      </div>
    </div>
  );
}
