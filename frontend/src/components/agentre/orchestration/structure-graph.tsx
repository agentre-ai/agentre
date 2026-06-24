import * as React from "react";
import { useTranslation } from "react-i18next";
import { Crown } from "lucide-react";
import { cn } from "@/lib/utils";

import type { app } from "../../../../wailsjs/go/models";
import { useChatAgents } from "@/hooks/use-chat-agents";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { AgentAvatar, StatusDot } from "../primitives";
import type { AgentColor, AgentStatus } from "../types";
import { buildGraph, lifecycle } from "./graph-data";
import type { GraphNode, NodeStatus } from "./graph-data";

// NodeStatus → 边框样式
function nodeBorderClass(s: NodeStatus): string {
  switch (s) {
    case "running":
      return "border-status-running";
    case "waiting-user":
      return "border-status-waiting";
    case "error":
      return "border-destructive";
    case "done":
      return "border-status-running/40";
    default:
      return "border-border";
  }
}

// 单个 agent 节点卡片
function NodeCard({
  node,
  agentName,
  agentColor,
  agentAvatarIcon,
  agentAvatarDataUrl,
  hasDeadlock,
  leaderLabel,
  onClick,
}: {
  node: GraphNode;
  agentName: string;
  agentColor: AgentColor;
  agentAvatarIcon?: string;
  agentAvatarDataUrl?: string;
  hasDeadlock: boolean;
  leaderLabel: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      data-testid={`node-${node.agentId}`}
      onClick={onClick}
      className={cn(
        "flex w-52 cursor-pointer flex-col gap-2 rounded-lg border-2 bg-card p-3 text-left shadow-sm transition-shadow hover:shadow-md",
        hasDeadlock
          ? "border-destructive ring-2 ring-destructive/40"
          : nodeBorderClass(node.status),
      )}
    >
      {/* 头部: 头像 + 名称 + 皇冠 */}
      <div className="flex items-center gap-2">
        <AgentAvatar
          name={agentName}
          color={agentColor}
          size="sm"
          avatarIcon={agentAvatarIcon}
          avatarDataUrl={agentAvatarDataUrl}
        />
        <span className="flex-1 truncate text-sm font-medium text-foreground">
          {agentName}
        </span>
        {node.isLeader && (
          <Crown
            aria-label={leaderLabel}
            className="size-3.5 shrink-0 text-status-waiting"
          />
        )}
      </div>

      {/* 任务行列表 */}
      {node.tasks.length > 0 && (
        <ul className="flex flex-col gap-1">
          {node.tasks.map((task) => (
            <li key={task.id} className="flex items-center gap-1.5">
              <StatusDot
                status={
                  task.status === "awaiting-user" ||
                  task.status === "awaiting-children"
                    ? "waiting"
                    : (task.status as AgentStatus) in
                        { running: 1, waiting: 1, idle: 1, error: 1 }
                      ? (task.status as AgentStatus)
                      : "idle"
                }
                size="xs"
              />
              <span className="truncate text-xs text-muted-foreground">
                {task.brief || `#${task.id}`}
              </span>
            </li>
          ))}
        </ul>
      )}
    </button>
  );
}

// 简单的树形层级布局: 根据 BFS 深度把节点分组到列
function computeDepths(
  nodes: GraphNode[],
  edges: { from: number; to: number }[],
): Map<number, number> {
  // 查找根节点（isLeader 或无入边）
  const inbound = new Set(edges.map((e) => e.to));
  const depths = new Map<number, number>();
  const children = new Map<number, number[]>();

  for (const e of edges) {
    if (!children.has(e.from)) children.set(e.from, []);
    children.get(e.from)!.push(e.to);
  }

  const queue: Array<{ agentId: number; depth: number }> = [];

  // 根节点从深度 0 开始
  for (const node of nodes) {
    if (!inbound.has(node.agentId) || node.isLeader) {
      queue.push({ agentId: node.agentId, depth: 0 });
      depths.set(node.agentId, 0);
    }
  }

  // BFS
  while (queue.length > 0) {
    const { agentId, depth } = queue.shift()!;
    for (const childId of children.get(agentId) ?? []) {
      if (!depths.has(childId)) {
        depths.set(childId, depth + 1);
        queue.push({ agentId: childId, depth: depth + 1 });
      }
    }
  }

  // 未被 BFS 覆盖的节点兜底
  for (const node of nodes) {
    if (!depths.has(node.agentId)) {
      depths.set(node.agentId, 0);
    }
  }

  return depths;
}

// 树形层级节点区域
function NodeTree({
  nodes,
  edges,
  deadlockAgentIds,
  agentMap,
  leaderLabel,
  onSelectNode,
}: {
  nodes: GraphNode[];
  edges: { from: number; to: number }[];
  deadlockAgentIds: Set<number>;
  agentMap: Map<
    number,
    {
      name: string;
      color: AgentColor;
      avatarIcon?: string;
      avatarDataUrl?: string;
    }
  >;
  leaderLabel: string;
  onSelectNode: (agentId: number) => void;
}) {
  const depths = computeDepths(nodes, edges);

  // 按深度分组
  const byDepth = new Map<number, GraphNode[]>();
  for (const node of nodes) {
    const d = depths.get(node.agentId) ?? 0;
    if (!byDepth.has(d)) byDepth.set(d, []);
    byDepth.get(d)!.push(node);
  }

  // 兜底 0:byDepth 为空时 Math.max(...[]) 会得 -Infinity,加 0 锚定不变量
  const maxDepth = Math.max(...[...byDepth.keys()], 0);

  return (
    // TODO(plan-1b): 大图时加缩放/适应/折叠/小地图工具条
    <div className="flex flex-row items-start gap-8 overflow-x-auto p-4">
      {Array.from({ length: maxDepth + 1 }, (_, d) => (
        <div key={d} className="flex flex-col gap-4">
          {/* 深度 d > 0 时展示缩进分隔线提示层级关系 */}
          {(byDepth.get(d) ?? []).map((node) => {
            const agentInfo = agentMap.get(node.agentId);
            const agentName = agentInfo?.name ?? `#${node.agentId}`;
            const agentColor: AgentColor = agentInfo?.color ?? "agent-1";
            return (
              <NodeCard
                key={node.agentId}
                node={node}
                agentName={agentName}
                agentColor={agentColor}
                agentAvatarIcon={agentInfo?.avatarIcon}
                agentAvatarDataUrl={agentInfo?.avatarDataUrl}
                hasDeadlock={deadlockAgentIds.has(node.agentId)}
                leaderLabel={leaderLabel}
                onClick={() => onSelectNode(node.agentId)}
              />
            );
          })}
        </div>
      ))}
    </div>
  );
}

export function StructureGraph({
  detail,
  onSelectNode,
}: {
  detail: app.RunDetailDTO;
  onSelectNode: (agentId: number) => void;
}) {
  const { t } = useTranslation();
  const { agents } = useChatAgents();
  const { nodes, edges } = buildGraph(detail);
  const phase = lifecycle(detail);

  // agentId → {name, color} 查找表
  const agentMap = React.useMemo(() => {
    const m = new Map<
      number,
      {
        name: string;
        color: AgentColor;
        avatarIcon?: string;
        avatarDataUrl?: string;
      }
    >();
    for (const a of agents) {
      m.set(a.id, {
        name: a.name,
        color: (a.avatarColor as AgentColor) || "agent-1",
        avatarIcon: a.avatarIcon,
        avatarDataUrl: a.avatarDataUrl,
      });
    }
    return m;
  }, [agents]);

  // 死锁环: 从 store 读，映射 sessionId → agentId
  const runId = detail.run?.id;
  const cycle = useOrchRunStore((s) =>
    runId !== undefined ? s.deadlocks.get(runId) : undefined,
  );

  const deadlockAgentIds = React.useMemo(() => {
    if (!cycle || cycle.length === 0) return new Set<number>();
    const sessionIds = new Set<number>(cycle);
    const result = new Set<number>();
    for (const task of detail.tasks ?? []) {
      if (task.sessionId && sessionIds.has(task.sessionId)) {
        result.add(task.agentId);
      }
    }
    return result;
  }, [cycle, detail.tasks]);

  const hasDeadlock = deadlockAgentIds.size > 0;

  // 节点是否应该被暗化（paused 态）
  const dimmed = phase === "paused";

  return (
    <div className="flex flex-col gap-0">
      {/* 死锁横幅（优先级最高，任何 phase 下只要有死锁就显示） */}
      {hasDeadlock && (
        <div
          data-testid="graph-deadlock-banner"
          className="flex items-center gap-2 border-b border-destructive/30 bg-destructive-soft px-4 py-2 text-sm font-medium text-destructive"
        >
          <span>{t("orchestration.graph.deadlock")}</span>
        </div>
      )}

      {/* 完成横幅 */}
      {phase === "completed" && (
        <div
          data-testid="graph-completed-banner"
          className="flex items-center gap-2 border-b border-status-running/30 bg-status-running-bg px-4 py-2 text-sm font-medium text-status-running"
        >
          <span>{t("orchestration.graph.completedBanner")}</span>
        </div>
      )}

      {/* 暂停横幅 */}
      {phase === "paused" && (
        <div
          data-testid="graph-paused-banner"
          className="flex items-center gap-2 border-b border-status-waiting/30 bg-status-waiting-bg px-4 py-2 text-sm font-medium text-status-waiting"
        >
          <span>{t("orchestration.graph.pausedBanner")}</span>
        </div>
      )}

      {/* 停止横幅 */}
      {phase === "stopped" && (
        <div
          data-testid="graph-stopped-banner"
          className="flex items-center gap-2 border-b border-border bg-muted px-4 py-2 text-sm font-medium text-muted-foreground"
        >
          <span>{t("orchestration.graph.stoppedBanner")}</span>
        </div>
      )}

      {/* 主体区域 */}
      {phase === "empty" ? (
        // 起步态：居中引导文案
        <div
          data-testid="graph-empty"
          className="flex flex-col items-center justify-center gap-3 py-20 text-center text-muted-foreground"
        >
          <span className="text-sm">{t("orchestration.graph.empty")}</span>
        </div>
      ) : (
        // 有节点：树形布局
        <div className={cn(dimmed && "opacity-50")}>
          <NodeTree
            nodes={nodes}
            edges={edges}
            deadlockAgentIds={deadlockAgentIds}
            agentMap={agentMap}
            leaderLabel={t("orchestration.graph.leaderCrown")}
            onSelectNode={onSelectNode}
          />
        </div>
      )}
    </div>
  );
}
