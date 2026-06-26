import * as React from "react";
import { useTranslation } from "react-i18next";
import { MessageSquare, SendHorizontal } from "lucide-react";
import { useChatAgents } from "@/hooks/use-chat-agents";
import { Button } from "@/components/ui/button";
import type { AgentColor } from "../types";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { RunSpeak } from "../../../../wailsjs/go/app/App";
import { RunHeader } from "./run-header";
import { StructureGraph } from "./structure-graph";
import { ActivityFeed } from "./activity-feed";
import { TaskBoard } from "./task-board";
import { ConversationPanel } from "./conversation-panel";
import { ToggleBar } from "./toggle-bar";
import { buildGraph } from "./graph-data";

export function OrchestrationRun({
  runId,
  title,
}: {
  runId: number;
  title: string;
}) {
  const { t } = useTranslation();
  const detail = useOrchRunStore((s) => s.details.get(runId));

  React.useEffect(() => {
    void useOrchRunStore.getState().loadRun(runId);
  }, [runId]);

  const [view, setView] = React.useState<"graph" | "feed">("graph");
  const [selectedSessionId, setSelectedSessionId] = React.useState<
    number | null
  >(null);

  // Compute ToggleBar stats from existing data (no additional fetches)
  const toggleStats = React.useMemo(() => {
    const tasks = detail?.tasks ?? [];
    const done = tasks.filter((t) => t.status === "done").length;
    const total = tasks.length;
    if (!detail) {
      return {
        done,
        total,
        depth: 0,
        agentCount: 0,
        subCount: 0,
        subagentCount: 0,
      };
    }
    const graph = buildGraph(detail);
    const agentCount = graph.stats.nodes;
    const subCount = graph.stats.subagents;
    // subagentCount: subagents spawned via local_agent tool (tracked per-session by useRunSubagents).
    // Not available as a simple number without store subscription in this component —
    // fall back to graph.stats.subagents (node-level subagent count) as best available approximation.
    const subagentCount = graph.stats.subagents;
    return {
      done,
      total,
      depth: graph.stats.depth,
      agentCount,
      subCount,
      subagentCount,
    };
  }, [detail]);

  // Footer speak-to-Leader state
  const [leaderMsg, setLeaderMsg] = React.useState("");

  // 切换 Run 时重置选中
  React.useEffect(() => {
    setSelectedSessionId(null);
  }, [runId]);

  // 解析选中 session 对应的 agent
  const { agents } = useChatAgents();
  const selTask = (detail?.tasks ?? []).find(
    (t) => t.sessionId === selectedSessionId,
  );
  const selAgent = agents.find((a) => a.id === selTask?.agentId);
  const selAgentName = selAgent?.name ?? (selTask ? `#${selTask.agentId}` : "");
  const selAgentColor: AgentColor =
    (selAgent?.avatarColor as AgentColor) ?? "agent-1";

  // Derive leaderSessionId from the leader agent's task in detail
  const leaderSessionId = React.useMemo(() => {
    const leaderAgentId = detail?.run?.leaderAgentId;
    if (!leaderAgentId) return null;
    const leaderTask = (detail?.tasks ?? []).find(
      (task) => task.agentId === leaderAgentId && task.sessionId > 0,
    );
    return leaderTask?.sessionId ?? null;
  }, [detail]);

  const handleLeaderSend = React.useCallback(async () => {
    if (!leaderSessionId || !leaderMsg.trim()) return;
    const msg = leaderMsg.trim();
    setLeaderMsg("");
    await RunSpeak(leaderSessionId, msg);
  }, [leaderSessionId, leaderMsg]);

  const handleLeaderKeyDown = React.useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        void handleLeaderSend();
      }
    },
    [handleLeaderSend],
  );

  return (
    <div
      data-testid="orchestration-run"
      aria-label={title}
      className="flex h-full"
    >
      {!detail || !detail.run ? (
        // 加载占位:detail 未就绪 或 run 字段缺失(可选字段)时显示,
        // 避免把 run=undefined 的半成品 detail 传给子组件。
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          {t("orchestration.loading")}
        </div>
      ) : (
        <>
          {/* 主区域：Main column — header + toggle + content + footer */}
          <div
            data-testid="orch-main"
            className="flex min-h-0 min-w-0 flex-1 flex-col bg-background"
          >
            <RunHeader detail={detail} />

            <ToggleBar view={view} onView={setView} stats={toggleStats} />

            {/* Content: graph | feed */}
            <div
              data-testid="orch-content"
              className="flex min-h-0 flex-1 flex-col bg-background overflow-auto"
            >
              {view === "graph" ? (
                <StructureGraph
                  detail={detail}
                  onSelectSession={setSelectedSessionId}
                />
              ) : (
                <ActivityFeed detail={detail} />
              )}
            </div>

            {/* Footer: speak to Leader */}
            <div
              data-testid="orch-footer"
              className="shrink-0 border-t border-border bg-card px-5 py-3 flex items-center gap-2.5"
            >
              <div className="flex flex-1 items-center gap-2 rounded-lg border border-border bg-input-bg px-3 py-[9px]">
                <MessageSquare
                  className="size-3.5 shrink-0 text-muted-foreground"
                  aria-hidden="true"
                />
                <input
                  type="text"
                  className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
                  placeholder={t("orchestration.run.speakLeaderPlaceholder")}
                  value={leaderMsg}
                  onChange={(e) => setLeaderMsg(e.target.value)}
                  onKeyDown={handleLeaderKeyDown}
                  disabled={!leaderSessionId}
                />
              </div>
              <Button
                data-testid="orch-speak-leader-send"
                size="sm"
                className="shrink-0 rounded-lg bg-primary px-4 py-[9px] text-primary-foreground"
                disabled={!leaderSessionId}
                onClick={() => void handleLeaderSend()}
              >
                <SendHorizontal className="size-3.5" aria-hidden="true" />
                <span className="font-semibold">
                  {t("orchestration.run.send")}
                </span>
              </Button>
            </div>
          </div>

          {/* 右侧：任务看板 ⇄ 会话面板 二态 */}
          {/* selectedSessionId=0 是「未启动/Leader 节点无会话」的哨兵值,falsy 均回落到看板 */}
          <aside className="w-80 shrink-0 border-l border-border bg-sidebar">
            {selectedSessionId ? (
              <ConversationPanel
                sessionId={selectedSessionId}
                agentName={selAgentName}
                agentColor={selAgentColor}
                onBack={() => setSelectedSessionId(null)}
              />
            ) : (
              <TaskBoard
                detail={detail}
                selectedSessionId={selectedSessionId}
                onSelectSession={setSelectedSessionId}
              />
            )}
          </aside>
        </>
      )}
    </div>
  );
}
