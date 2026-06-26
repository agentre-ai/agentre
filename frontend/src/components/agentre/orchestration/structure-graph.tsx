import * as React from "react";
import { useTranslation } from "react-i18next";
import { Crown, GitMerge, ListChecks } from "lucide-react";
import { cn } from "@/lib/utils";

import type { app } from "../../../../wailsjs/go/models";
import { useChatAgents } from "@/hooks/use-chat-agents";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { AgentAvatar, StatusDot } from "../primitives";
import type { AgentColor, AgentStatus } from "../types";
import { buildGraph, lifecycle } from "./graph-data";
import type { GraphCall, GraphNode, NodeStatus } from "./graph-data";
import { useRunSubagents } from "./use-run-subagents";

// module-level map: NodeStatus → AgentStatus (allocation-free, rebuilt once)
const CALL_DOT: Record<NodeStatus, AgentStatus> = {
  running: "running",
  error: "error",
  idle: "idle",
  done: "idle",
  "waiting-user": "waiting",
  waiting: "waiting",
};

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

// 单个 agent 节点卡片:三态
//  ① callCount<=1            → 单行 brief(现状)
//  ② !isTopLevel && >=2      → 合并 ×N 徽标, 不列子行(子代理保持图干净)
//  ③ isTopLevel && >=2       → 分组容器: 头部「N 会话」+ 每次调用一条可点击子行
function NodeCard({
  node,
  agentName,
  agentColor,
  agentAvatarIcon,
  agentAvatarDataUrl,
  hasDeadlock,
  leaderLabel,
  subagentCount,
  isAsking,
  isLeaderStyle,
  depth,
  onSelectSession,
}: {
  node: GraphNode;
  agentName: string;
  agentColor: AgentColor;
  agentAvatarIcon?: string;
  agentAvatarDataUrl?: string;
  hasDeadlock: boolean;
  leaderLabel: string;
  subagentCount: number;
  isAsking: boolean;
  isLeaderStyle?: boolean;
  /** Depth in the tree: 0 = leader, 1 = direct child, 2+ = grandchild */
  depth?: number;
  onSelectSession: (sessionId: number) => void;
}) {
  const { t } = useTranslation();
  const isMerged = !node.isLeader && !node.isTopLevel && node.callCount >= 2;
  const isGroup = node.isTopLevel && node.callCount >= 2;

  const callDot = (status: GraphCall["status"]) => CALL_DOT[status];

  // 整卡点击:取首个 call 的 sessionId(单调用 / 合并节点均取第一个)
  const handleCardClick = () => {
    if (node.calls[0]) {
      onSelectSession(node.calls[0].sessionId);
    }
  };

  const handleCardKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      handleCardClick();
    }
  };

  return (
    // root div role="button" 以允许内部嵌套 <button>(顶层分组子行)
    <div
      role="button"
      tabIndex={0}
      data-testid={`node-${node.agentId}`}
      onClick={handleCardClick}
      onKeyDown={handleCardKeyDown}
      className={cn(
        "flex cursor-pointer flex-col gap-2 rounded-xl bg-card p-3 text-left shadow-sm transition-shadow hover:shadow-md focus-visible:ring-2 focus-visible:ring-ring",
        isLeaderStyle
          ? cn(
              "w-[212px] border-[1.5px]",
              hasDeadlock
                ? "border-destructive ring-2 ring-destructive/40"
                : "border-primary",
            )
          : cn(
              // FIX 2: 1px border (design strokeWidth:1); FIX 3: depth-aware width
              (depth ?? 1) >= 2 ? "w-[116px]" : "w-[152px]",
              "border",
              hasDeadlock
                ? "border-destructive ring-2 ring-destructive/40"
                : nodeBorderClass(node.status),
            ),
      )}
    >
      {/* 头部: 头像 + 名称/角色 + 状态点 */}
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
          {isGroup && (
            <span className="ml-1 text-xs font-normal text-muted-foreground">
              {"· "}
              {t("orchestration.graph.sessionsCount", {
                count: node.callCount,
              })}
            </span>
          )}
        </span>
        {isMerged && (
          <span
            data-testid={`node-${node.agentId}-multi`}
            className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs font-medium text-muted-foreground"
          >
            {t("orchestration.graph.callCount", { count: node.callCount })}
          </span>
        )}
        {node.isLeader && (
          <Crown
            aria-label={leaderLabel}
            className="size-3.5 shrink-0 text-primary-text"
          />
        )}
        {subagentCount > 0 && (
          <span
            data-testid={`node-${node.agentId}-subagents`}
            className="flex shrink-0 items-center gap-1 rounded-md bg-secondary px-1.5 py-0.5 text-xs text-muted-foreground"
          >
            <GitMerge className="size-2.5" />
            {t("orchestration.subagent.badge", { count: subagentCount })}
          </span>
        )}
        {isAsking && (
          <span
            data-testid={`node-${node.agentId}-asking`}
            className="shrink-0 rounded bg-status-waiting-bg px-1.5 py-0.5 text-xs text-status-waiting"
          >
            {t("orchestration.graph.askWaiting")}
          </span>
        )}
        {/* Status dot — always visible on right */}
        <StatusDot status={CALL_DOT[node.status]} size="xs" />
      </div>

      {/* Leader 元信息行: 任务计数 chip + spacer + 状态 chip */}
      {isLeaderStyle && node.calls.length > 0 && (
        <div className="flex items-center gap-1.5">
          <span className="flex items-center gap-1 rounded-md bg-secondary px-2 py-0.5 text-2xs text-muted-foreground">
            <ListChecks className="size-2.5 shrink-0" aria-hidden />
            <span className="font-mono">
              {node.calls.length}/{node.callCount}
            </span>
          </span>
          <span className="flex-1" />
          <span className="rounded-full bg-status-running-bg px-2 py-0.5 text-2xs font-semibold text-status-running">
            {t(
              `orchestration.header.${node.status === "running" ? "running" : node.status === "done" ? "completed" : "pending"}`,
            )}
          </span>
        </div>
      )}

      {/* 分组容器:每次调用一条可点击子行(钻入会话) */}
      {!isLeaderStyle &&
        (isGroup ? (
          <ul className="flex flex-col gap-1">
            {node.calls.map((call, i) => (
              <li key={call.taskId} className="flex items-center gap-1.5">
                <button
                  type="button"
                  data-testid={`node-${node.agentId}-call-${call.taskId}`}
                  className="flex min-w-0 flex-1 items-center gap-1.5"
                  onClick={(e) => {
                    e.stopPropagation();
                    onSelectSession(call.sessionId);
                  }}
                >
                  <StatusDot status={callDot(call.status)} size="xs" />
                  <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                    {t("orchestration.graph.callLabel", {
                      seq: call.callSeq || i + 1,
                    })}
                  </span>
                  <span className="truncate text-xs text-muted-foreground">
                    {call.brief || `#${call.taskId}`}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        ) : (
          // 单次调用:单行 brief(合并 ×N 节点不列子行)
          !isMerged &&
          node.calls.length > 0 && (
            <ul className="flex flex-col gap-1">
              {node.calls.map((call) => (
                <li key={call.taskId} className="flex items-center gap-1.5">
                  <StatusDot status={callDot(call.status)} size="xs" />
                  <span className="truncate text-xs text-muted-foreground">
                    {call.brief || `#${call.taskId}`}
                  </span>
                </li>
              ))}
            </ul>
          )
        ))}
    </div>
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

// 递归渲染一个节点及其子树(depth ≥ 1)
// 布局: vl(竖线) → nodeCard, 如有子节点继续 vl + subbus + subcols
function SubNodeColumn({
  node,
  depth,
  visited,
  childrenMap,
  deadlockAgentIds,
  agentMap,
  leaderLabel,
  countForAgent,
  askingAgentIds,
  onSelectSession,
}: {
  node: GraphNode;
  /** Depth of this node in the tree (1 = direct child of leader, 2+ = grandchild) */
  depth: number;
  /** FIX 1: visited set for cycle guard — prevents infinite recursion on A→B→A cycles */
  visited: ReadonlySet<number>;
  childrenMap: Map<number, GraphNode[]>;
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
  countForAgent: (agentId: number) => number;
  askingAgentIds: Set<number>;
  onSelectSession: (sessionId: number) => void;
}) {
  // FIX 1: cycle guard — if this node is already in the ancestor chain, stop recursing
  if (visited.has(node.agentId)) return null;

  const agentInfo = agentMap.get(node.agentId);
  const agentName = agentInfo?.name ?? `#${node.agentId}`;
  const agentColor: AgentColor = agentInfo?.color ?? "agent-1";
  const grandchildren = childrenMap.get(node.agentId) ?? [];

  // New visited set for children: add current node to prevent re-entry
  const nextVisited = new Set([...visited, node.agentId]);

  return (
    <div className="flex flex-col items-center">
      {/* 竖向连接线: vl */}
      <div className="h-3.5 w-0.5 bg-border-strong" />
      {/* 节点卡片 — FIX 3: pass depth for width selection */}
      <NodeCard
        node={node}
        agentName={agentName}
        agentColor={agentColor}
        agentAvatarIcon={agentInfo?.avatarIcon}
        agentAvatarDataUrl={agentInfo?.avatarDataUrl}
        hasDeadlock={deadlockAgentIds.has(node.agentId)}
        leaderLabel={leaderLabel}
        subagentCount={countForAgent(node.agentId)}
        isAsking={askingAgentIds.has(node.agentId)}
        depth={depth}
        onSelectSession={onSelectSession}
      />
      {/* 深层子节点: vl + subbus + subcols */}
      {grandchildren.length > 0 && (
        <>
          <div className="h-3.5 w-0.5 bg-border-strong" />
          {grandchildren.length > 1 && (
            <div className="h-0.5 w-full bg-border-strong" />
          )}
          <div className="flex flex-row items-start justify-center gap-2.5">
            {grandchildren.map((gc) => (
              <SubNodeColumn
                key={gc.agentId}
                node={gc}
                depth={depth + 1}
                visited={nextVisited}
                childrenMap={childrenMap}
                deadlockAgentIds={deadlockAgentIds}
                agentMap={agentMap}
                leaderLabel={leaderLabel}
                countForAgent={countForAgent}
                askingAgentIds={askingAgentIds}
                onSelectSession={onSelectSession}
              />
            ))}
          </div>
        </>
      )}
    </div>
  );
}

// Top-down 树形布局:
//   leader node (top center)
//     ↓ vl
//   bus (horizontal line spanning children row)
//     ↓ columns of depth-1 children (each column may have deeper subtrees)
function NodeTree({
  nodes,
  edges,
  deadlockAgentIds,
  agentMap,
  leaderLabel,
  countForAgent,
  askingAgentIds,
  onSelectSession,
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
  countForAgent: (agentId: number) => number;
  askingAgentIds: Set<number>;
  onSelectSession: (sessionId: number) => void;
}) {
  const depths = computeDepths(nodes, edges);

  // Build children map: parentId → [child nodes] for the SubNodeColumn recursion
  const nodeById = new Map(nodes.map((n) => [n.agentId, n]));
  const childrenMap = new Map<number, GraphNode[]>();
  for (const e of edges) {
    if (!childrenMap.has(e.from)) childrenMap.set(e.from, []);
    const child = nodeById.get(e.to);
    if (child) childrenMap.get(e.from)!.push(child);
  }

  const leaderNode = nodes.find((n) => n.isLeader) ?? nodes[0];
  // depth-1 direct children of the leader
  const depth1Children = leaderNode
    ? (childrenMap.get(leaderNode.agentId) ?? [])
    : [];

  // Any nodes not reachable from leader via edges (depth computed but not connected)
  // We still show them by falling back to all non-leader nodes at depth ≥ 1
  const byDepth = new Map<number, GraphNode[]>();
  for (const node of nodes) {
    const d = depths.get(node.agentId) ?? 0;
    if (!byDepth.has(d)) byDepth.set(d, []);
    byDepth.get(d)!.push(node);
  }

  // Collect all non-leader depth-1 nodes including those not in childrenMap
  const connectedChildIds = new Set(depth1Children.map((n) => n.agentId));
  const orphanChildren = (byDepth.get(1) ?? []).filter(
    (n) => !connectedChildIds.has(n.agentId),
  );
  const allDepth1 = [...depth1Children, ...orphanChildren];

  const hasChildren = allDepth1.length > 0;

  const leaderInfo = leaderNode ? agentMap.get(leaderNode.agentId) : undefined;
  const leaderName = leaderInfo?.name ?? `#${leaderNode?.agentId ?? "?"}`;
  const leaderColor: AgentColor = leaderInfo?.color ?? "agent-1";

  return (
    <div className="flex flex-col items-center overflow-auto p-[30px_20px]">
      {/* Leader node at top center */}
      {leaderNode && (
        <NodeCard
          node={leaderNode}
          agentName={leaderName}
          agentColor={leaderColor}
          agentAvatarIcon={leaderInfo?.avatarIcon}
          agentAvatarDataUrl={leaderInfo?.avatarDataUrl}
          hasDeadlock={deadlockAgentIds.has(leaderNode.agentId)}
          leaderLabel={leaderLabel}
          subagentCount={countForAgent(leaderNode.agentId)}
          isAsking={askingAgentIds.has(leaderNode.agentId)}
          isLeaderStyle={true}
          onSelectSession={onSelectSession}
        />
      )}

      {/* Only render connector + children if there are child nodes */}
      {hasChildren && (
        <>
          {/* Vertical connector: vl */}
          <div className="h-[22px] w-0.5 bg-border-strong" />

          {/* Horizontal bus spanning the children columns */}
          <div
            data-testid="graph-bus"
            className="h-0.5 w-full bg-border-strong"
          />

          {/* Children columns */}
          <div className="flex flex-row items-start justify-center gap-5">
            {allDepth1.map((node) => (
              <SubNodeColumn
                key={node.agentId}
                node={node}
                depth={1}
                visited={
                  // FIX 1: seed visited with leader + all already-rendered depth-0 ids
                  new Set((byDepth.get(0) ?? []).map((n) => n.agentId))
                }
                childrenMap={childrenMap}
                deadlockAgentIds={deadlockAgentIds}
                agentMap={agentMap}
                leaderLabel={leaderLabel}
                countForAgent={countForAgent}
                askingAgentIds={askingAgentIds}
                onSelectSession={onSelectSession}
              />
            ))}
          </div>
        </>
      )}
    </div>
  );
}

export function StructureGraph({
  detail,
  onSelectSession,
}: {
  detail: app.RunDetailDTO;
  onSelectSession: (sessionId: number) => void;
}) {
  const { t } = useTranslation();
  const { agents } = useChatAgents();
  const { nodes, edges } = buildGraph(detail);
  const phase = lifecycle(detail);
  const subagents = useRunSubagents(detail);

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

  // 死锁环 + 提问中: 从 store 读
  const runId = detail.run?.id;
  const cycle = useOrchRunStore((s) =>
    runId !== undefined ? s.deadlocks.get(runId) : undefined,
  );
  const activeAsks = useOrchRunStore((s) =>
    runId !== undefined ? s.activeAsks.get(runId) : undefined,
  );
  const askingAgentIds = React.useMemo(
    () => new Set((activeAsks ?? []).map((a) => a.askerAgentId)),
    [activeAsks],
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
        // 有节点：top-down 树形布局
        <div className={cn(dimmed && "opacity-50")}>
          <NodeTree
            nodes={nodes}
            edges={edges}
            deadlockAgentIds={deadlockAgentIds}
            agentMap={agentMap}
            leaderLabel={t("orchestration.graph.leaderCrown")}
            countForAgent={subagents.countForAgent}
            askingAgentIds={askingAgentIds}
            onSelectSession={onSelectSession}
          />
        </div>
      )}
    </div>
  );
}
