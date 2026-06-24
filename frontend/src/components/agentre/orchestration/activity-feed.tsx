import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { RunSpeak } from "../../../../wailsjs/go/app/App";
import type { app } from "../../../../wailsjs/go/models";
import { useChatAgents } from "@/hooks/use-chat-agents";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { buildFeed, type FeedItem } from "./feed-data";

// 根据 agentId 从 agents 列表中解析 agent 名称，找不到则用 #{agentId} 作为 fallback
function resolveAgentName(
  agentId: number,
  agents: ReturnType<typeof useChatAgents>["agents"],
): string {
  const agent = agents.find((a) => a.id === agentId);
  return agent?.name ?? `#${agentId}`;
}

// 根据 feed item 的 kind 返回对应的颜色 class
function kindClass(kind: FeedItem["kind"]): string {
  switch (kind) {
    case "dispatch":
      return "bg-primary";
    case "report":
    case "finish":
      return "bg-status-running";
    case "blocked":
    case "ask":
      return "bg-destructive";
    default:
      return "bg-muted-foreground";
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

  const tasks = detail.tasks ?? [];
  const runStatus = detail.run?.status;

  // 解析根任务会话 ID：优先匹配 rootTaskId，退而匹配 parentTaskId===0
  const rootSessionId =
    (detail.tasks ?? []).find((t) => t.id === detail.run?.rootTaskId)
      ?.sessionId ??
    (detail.tasks ?? []).find((t) => t.parentTaskId === 0)?.sessionId ??
    0;

  // 统计 awaiting-user 任务数
  const awaitingTasks = tasks.filter((t) => t.status === "awaiting-user");
  const awaitingCount = awaitingTasks.length;

  // 构造 feed 条目列表
  const feedItems = buildFeed(detail);

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

  return (
    <div className="flex h-full flex-col gap-0">
      {/* 顶部状态条 */}
      {topStrip && (
        <div className="shrink-0 border-b border-border p-3">{topStrip}</div>
      )}

      {/* 中栏：时间线 feed，可滚动 */}
      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        {feedItems.length === 0 ? (
          <p className="text-center text-sm text-muted-foreground">
            {t("orchestration.feed.empty")}
          </p>
        ) : (
          <ul className="flex flex-col gap-2">
            {feedItems.map((item) => {
              // blocked 类型且 text 为空时使用 i18n fallback 文案
              const displayText =
                item.kind === "blocked" && !item.text.trim()
                  ? t("orchestration.feed.blocked")
                  : item.text;
              const agentName = resolveAgentName(item.agentId, agents);

              return (
                <li key={item.id} className="flex items-start gap-2 text-sm">
                  {/* 种类指示点 */}
                  <span
                    className={cn(
                      "mt-1.5 h-2 w-2 shrink-0 rounded-full",
                      kindClass(item.kind),
                    )}
                  />
                  <div className="min-w-0 flex-1">
                    {/* agent 名称 */}
                    <span className="mr-1 font-medium text-foreground">
                      {agentName}
                    </span>
                    {/* 动态内容保持原文，不走 t() */}
                    <span className="break-words text-muted-foreground">
                      {displayText}
                    </span>
                  </div>
                </li>
              );
            })}
          </ul>
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
