import { useState } from "react";
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
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { RunSpeak } from "../../../../wailsjs/go/app/App";
import type { app } from "../../../../wailsjs/go/models";
import { useChatAgents } from "@/hooks/use-chat-agents";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { AgentAvatar } from "../primitives";
import type { AgentColor } from "../types";
import { buildFeed, type FeedItem } from "./feed-data";

// 根据 agentId 从 agents 列表中解析 agent 名称，找不到则用 #{agentId} 作为 fallback
function resolveAgentName(
  agentId: number,
  agents: ReturnType<typeof useChatAgents>["agents"],
): string {
  const agent = agents.find((a) => a.id === agentId);
  return agent?.name ?? `#${agentId}`;
}

// 根据 agentId 解析 agent 颜色
function resolveAgentColor(
  agentId: number,
  agents: ReturnType<typeof useChatAgents>["agents"],
): AgentColor {
  const agent = agents.find((a) => a.id === agentId);
  return (agent?.avatarColor as AgentColor) || "agent-1";
}

// 根据 agentId 解析 avatar 相关字段
function resolveAgentAvatar(
  agentId: number,
  agents: ReturnType<typeof useChatAgents>["agents"],
): { avatarDataUrl?: string; avatarIcon?: string } {
  const agent = agents.find((a) => a.id === agentId);
  return {
    avatarDataUrl: agent?.avatarDataUrl || undefined,
    avatarIcon: agent?.avatarIcon || undefined,
  };
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
  const [msg, setMsg] = useState("");

  // 从 store 读取死锁信息
  const deadlock = useOrchRunStore((s) =>
    detail.run?.id ? s.deadlocks.get(detail.run.id) : undefined,
  );

  // 从 store 读取该 run 的 askLog（undefined 时用稳定空数组）
  const runId = detail.run?.id;
  const askLog =
    useOrchRunStore((s) => (runId ? s.askLog.get(runId) : undefined)) ?? [];

  const tasks = detail.tasks ?? [];
  const runStatus = detail.run?.status;

  // Leader agent id（用于渲染皇冠 chip）
  const leaderAgentId = detail.run?.leaderAgentId;

  // 解析根任务会话 ID：优先匹配 rootTaskId，退而匹配 parentTaskId===0
  const rootSessionId =
    (detail.tasks ?? []).find((t) => t.id === detail.run?.rootTaskId)
      ?.sessionId ??
    (detail.tasks ?? []).find((t) => t.parentTaskId === 0)?.sessionId ??
    0;

  // 统计 awaiting-user 任务数
  const awaitingTasks = tasks.filter((t) => t.status === "awaiting-user");
  const awaitingCount = awaitingTasks.length;

  // 构造 feed 条目列表（含 ask/reply 日志）
  const feedItems = buildFeed(detail, askLog);

  // 找到当前 running 的任务中，取第一个非根任务（有 parentTaskId）的运行中任务
  // 用于 typing 行显示；若无则取所有 running 任务里第一个
  const runningTask =
    tasks.find((t) => t.status === "running" && t.parentTaskId) ??
    tasks.find((t) => t.status === "running");

  // 发送「对 Leader 说」消息，目标是根 task 的 session（非 run.id）
  async function handleSend() {
    if (!rootSessionId || !msg.trim()) return;
    try {
      await RunSpeak(rootSessionId, msg.trim());
    } catch {
      // 忽略后端拒绝（如 run 已终态），不抛出未处理异常
    }
    setMsg("");
  }

  // 顶部状态条：按优先级依次判断
  const topStrip = (() => {
    if (deadlock && deadlock.length > 0) {
      // 死锁：红条
      return (
        <div
          data-testid="feed-deadlock-banner"
          className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive"
        >
          {t("orchestration.feed.deadlock")}
        </div>
      );
    }
    if (awaitingCount > 0) {
      // 有等待审批任务：琥珀阻塞条
      return (
        <div
          data-testid="feed-blocking-bar"
          className="flex items-center justify-between rounded-md border border-status-waiting/30 bg-status-waiting-bg px-3 py-2 text-sm text-status-waiting"
        >
          <span>
            {t("orchestration.feed.blockingCount", { count: awaitingCount })}
          </span>
          <Button
            data-testid="feed-blocking-view"
            variant="outline"
            size="sm"
            className="ml-2 h-7 border-status-waiting/40 text-xs text-status-waiting hover:bg-status-waiting-bg"
            onClick={() => {
              // TODO(plan-1b): 跳转钻入该 awaiting 节点
            }}
          >
            {t("orchestration.feed.blockingView")}
          </Button>
        </div>
      );
    }
    if (runStatus === "done") {
      // 完成：绿条
      return (
        <div
          data-testid="feed-completed-banner"
          className="rounded-md border border-status-running/30 bg-status-running/10 px-3 py-2 text-sm text-status-running"
        >
          {t("orchestration.feed.completed")}
        </div>
      );
    }
    if (runStatus === "paused") {
      // 暂停条
      return (
        <div
          data-testid="feed-paused-banner"
          className="rounded-md border border-muted-foreground/30 bg-muted px-3 py-2 text-sm text-muted-foreground"
        >
          {t("orchestration.feed.paused")}
        </div>
      );
    }
    return null;
  })();

  // badge 静态标签（ask/reply 走 kindAsk/kindReply）
  function kindLabel(kind: FeedItem["kind"]): string | null {
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

  return (
    <div className="flex h-full flex-col gap-0 bg-background">
      {/* 顶部状态条 */}
      {topStrip && (
        <div className="shrink-0 border-b border-border p-3">{topStrip}</div>
      )}

      {/* 中栏：时间线 feed，可滚动 */}
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        {feedItems.length === 0 ? (
          <p className="text-center text-sm text-muted-foreground">
            {t("orchestration.feed.empty")}
          </p>
        ) : (
          <ul className="flex flex-col gap-3.5">
            {feedItems.map((item) => {
              const agentName = resolveAgentName(item.agentId, agents);
              const agentColor = resolveAgentColor(item.agentId, agents);
              const agentAvatarProps = resolveAgentAvatar(item.agentId, agents);
              const isLeader =
                leaderAgentId !== undefined && item.agentId === leaderAgentId;

              // blocked 类型且 text 为空时使用 i18n fallback 文案
              const displayText =
                item.kind === "blocked" && !item.text.trim()
                  ? t("orchestration.feed.blocked")
                  : item.text;

              const label = kindLabel(item.kind);

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
                                {" @"}
                                {resolveAgentName(item.targetAgentId, agents)}
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
            {/* Avatar */}
            <AgentAvatar
              name={resolveAgentName(runningTask.agentId, agents)}
              color={resolveAgentColor(runningTask.agentId, agents)}
              size="sm"
              className="shrink-0 rounded-full"
              {...resolveAgentAvatar(runningTask.agentId, agents)}
            />
            {/* Text + dots */}
            <div className="flex items-center gap-1.5">
              <span className="text-[12px] text-muted-foreground">
                {resolveAgentName(runningTask.agentId, agents)}{" "}
                {t("orchestration.feed.executing")}{" "}
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
          </div>
        )}
      </div>

      {/* 底部「对 Leader 说」输入条 */}
      <div className="shrink-0 border-t border-border p-3">
        <div className="flex items-end gap-2">
          <Textarea
            data-testid="feed-speak-input"
            value={msg}
            onChange={(e) => setMsg(e.target.value)}
            placeholder={t("orchestration.feed.speakPlaceholder")}
            className="min-h-[60px] flex-1 resize-none text-sm"
            onKeyDown={(e) => {
              // Ctrl+Enter 或 Cmd+Enter 发送
              if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
                e.preventDefault();
                void handleSend();
              }
            }}
          />
          <Button
            data-testid="feed-speak-send"
            size="sm"
            disabled={!msg.trim()}
            onClick={() => void handleSend()}
          >
            {t("orchestration.feed.speakSend")}
          </Button>
        </div>
      </div>
    </div>
  );
}
