import * as React from "react";
import { useTranslation } from "react-i18next";
import { useChatAgents } from "@/hooks/use-chat-agents";
import type { AgentColor } from "../types";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { RunFlowBlueprint } from "./run-flow-blueprint";
import { RunHeader } from "./run-header";
import { StructureGraph } from "./structure-graph";
import { ActivityFeed } from "./activity-feed";
import { TaskBoard } from "./task-board";
import { ConversationPanel } from "./conversation-panel";

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
          {/* 主区域：头部 + 中视图 */}
          <main className="flex min-w-0 flex-1 flex-col">
            <RunHeader detail={detail} view={view} onView={setView} />
            <RunFlowBlueprint flowId={detail.run.flowId ?? 0} />
            <div className="min-h-0 flex-1 overflow-auto">
              {view === "graph" ? (
                <StructureGraph
                  detail={detail}
                  onSelectSession={setSelectedSessionId}
                />
              ) : (
                <ActivityFeed detail={detail} />
              )}
            </div>
          </main>

          {/* 右侧：任务看板 ⇄ 会话面板 二态 */}
          {/* selectedSessionId=0 是「未启动/Leader 节点无会话」的哨兵值,falsy 均回落到看板 */}
          <aside className="w-80 shrink-0 border-l border-border">
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
