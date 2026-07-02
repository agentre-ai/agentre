import * as React from "react";
import { useTranslation } from "react-i18next";
import {
  CircleCheck,
  MessageSquare,
  Pause,
  SendHorizontal,
  Square,
  TriangleAlert,
} from "lucide-react";
import { useChatAgents } from "@/hooks/use-chat-agents";
import { Button } from "@/components/ui/button";
import type { AgentColor } from "../types";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { RunResume, RunSpeak } from "../../../../wailsjs/go/app/App";
import { RunHeader } from "./run-header";
import { StructureGraph } from "./structure-graph";
import { ActivityFeed } from "./activity-feed";
import { TaskBoard } from "./task-board";
import { ConversationPanel } from "./conversation-panel";
import { ToggleBar } from "./toggle-bar";
import { useRunSubagents } from "./use-run-subagents";
import type { app } from "../../../../wailsjs/go/models";
import { buildGraph, hasDeadlockCycle, lifecycle } from "./graph-data";

// 稳定空 detail:detail 未就绪时给 useRunSubagents 一个恒定身份的占位,避免每渲染触发懒加载 effect。
const EMPTY_RUN_DETAIL = { tasks: [] } as unknown as app.RunDetailDTO;

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

  // 子代理(spawned local_agent)计数:复用 useRunSubagents 懒加载缓存(任务板同源,按 session 缓存)
  const subagents = useRunSubagents(detail ?? EMPTY_RUN_DETAIL);

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
    // subagentCount:真正 spawned 的子代理(local_agent tool use)总数,跨所有 task session 求和
    // (与 chip3 的「sub」=图节点级子代理计数语义不同,设计稿两者本就是不同数字)。
    const subagentCount = tasks.reduce(
      (n, task) =>
        n + (task.sessionId ? subagents.forSession(task.sessionId).length : 0),
      0,
    );
    return {
      done,
      total,
      depth: graph.stats.depth,
      agentCount,
      subCount,
      subagentCount,
    };
  }, [detail, subagents]);

  // Footer speak-to-Leader state
  const [leaderMsg, setLeaderMsg] = React.useState("");

  // Ref for footer input — used by deadlock "intervene" banner action to focus it
  const footerInputRef = React.useRef<HTMLInputElement>(null);

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

  // Phase + deadlock detection for banners (shared logic with structure-graph)
  const phase = detail ? lifecycle(detail) : null;
  const cycle = useOrchRunStore((s) =>
    runId !== undefined ? s.deadlocks.get(runId) : undefined,
  );
  const hasDeadlock = React.useMemo(
    () => hasDeadlockCycle(cycle, detail?.tasks ?? []),
    [cycle, detail],
  );

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
      className="flex h-full min-w-0 flex-1"
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

            {/* Phase banners: between ToggleBar and content, visible in both views */}
            {detail && (
              <>
                {/* Deadlock banner — highest priority, any phase */}
                {hasDeadlock && (
                  <div
                    data-testid="graph-deadlock-banner"
                    className="flex items-center gap-2.5 border-l-[3px] border-destructive bg-destructive-soft px-5 py-3 text-destructive"
                  >
                    <TriangleAlert
                      className="size-4 shrink-0"
                      aria-hidden="true"
                    />
                    <div className="flex flex-1 flex-col gap-px">
                      <span className="text-[12.5px] font-semibold">
                        {t("orchestration.graph.deadlock")}
                      </span>
                      <span className="text-[11px]">
                        {t("orchestration.graph.deadlockBannerDetail")}
                      </span>
                    </div>
                    <Button
                      data-testid="banner-intervene-btn"
                      variant="destructive"
                      size="sm"
                      className="shrink-0 rounded-md px-3 py-1.5 text-xs font-semibold"
                      onClick={() => footerInputRef.current?.focus()}
                    >
                      {t("orchestration.graph.interveneBtn")}
                    </Button>
                  </div>
                )}

                {/* Completed banner */}
                {!hasDeadlock && phase === "completed" && (
                  <div
                    data-testid="graph-completed-banner"
                    className="flex items-center gap-2.5 border-l-[3px] border-status-running bg-status-running-bg px-5 py-3 text-status-running"
                  >
                    <CircleCheck
                      className="size-4 shrink-0"
                      aria-hidden="true"
                    />
                    <div className="flex flex-1 flex-col gap-px">
                      <span className="text-[12.5px] font-semibold">
                        {t("orchestration.graph.completedBanner")}
                      </span>
                      <span className="text-[11px]">
                        {t("orchestration.graph.completedBannerDetail")}
                      </span>
                    </div>
                  </div>
                )}

                {/* Paused banner */}
                {!hasDeadlock && phase === "paused" && (
                  <div
                    data-testid="graph-paused-banner"
                    className="flex items-center gap-2.5 border-l-[3px] border-status-waiting bg-status-waiting-bg px-5 py-3 text-status-waiting"
                  >
                    <Pause className="size-4 shrink-0" aria-hidden="true" />
                    <div className="flex flex-1 flex-col gap-px">
                      <span className="text-[12.5px] font-semibold">
                        {t("orchestration.graph.pausedBanner")}
                      </span>
                      <span className="text-[11px]">
                        {t("orchestration.graph.pausedBannerDetail")}
                      </span>
                    </div>
                    <Button
                      data-testid="banner-resume-btn"
                      size="sm"
                      className="shrink-0 rounded-md border border-status-waiting bg-status-waiting-bg px-3 py-1.5 text-xs font-semibold text-status-waiting hover:bg-status-waiting-bg/80"
                      onClick={() => void RunResume(runId).catch(() => {})}
                    >
                      {t("orchestration.graph.resumeBtn")}
                    </Button>
                  </div>
                )}

                {/* Stopped banner */}
                {!hasDeadlock && phase === "stopped" && (
                  <div
                    data-testid="graph-stopped-banner"
                    className="flex items-center gap-2.5 border-l-[3px] border-border bg-muted px-5 py-3 text-muted-foreground"
                  >
                    <Square className="size-4 shrink-0" aria-hidden="true" />
                    <div className="flex flex-1 flex-col gap-px">
                      <span className="text-[12.5px] font-semibold">
                        {t("orchestration.graph.stoppedBanner")}
                      </span>
                      <span className="text-[11px]">
                        {t("orchestration.graph.stoppedBannerDetail")}
                      </span>
                    </div>
                  </div>
                )}
              </>
            )}

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
                  ref={footerInputRef}
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
                runId={runId}
                agentId={selTask?.agentId}
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
