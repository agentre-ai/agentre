import { cn } from "@/lib/utils";
import {
  type FlowGraph,
  isBounceSource,
  layoutFlowGraph,
  parseFlowGraph,
} from "./flow-graph";
import {
  taskMatchesNode,
  type NodeOverlay,
  type NodeStatus,
} from "./flow-overlay";

const COL_W = 118;
const ROW_H = 52;
const NODE_W = 96;
const NODE_H = 34;
const PAD = 16;

// kind → 徽章样式(二元 + 派生收口)
// 折叠 isBounceSource 逻辑避免 JSX 字符串字面量
function kindBadge(g: FlowGraph, id: string, kind: string, isSink: boolean) {
  let label: string;
  let cls: string;

  if (isSink) {
    label = "finish";
    cls = "text-status-running";
  } else if (kind === "task") {
    label = "task";
    cls = "text-primary-text";
  } else {
    label = "leader";
    cls = "text-muted-foreground";
  }

  // 追加 bounce 源标记
  if (isBounceSource(g, id)) {
    label = `${label} · fail↩`;
  }

  return { label, cls };
}

// overlayClass: 节点状态 → 卡片边框/背景色(复用 run-status banner 的既有 token)。
function overlayClass(status: NodeStatus): string {
  switch (status) {
    case "done":
      return "border-status-running bg-status-running-bg";
    case "running":
      return "border-status-waiting bg-status-waiting-bg";
    case "error":
      return "border-destructive bg-destructive-soft";
    case "neutral":
      return "opacity-60";
    default: // pending
      return "";
  }
}

export function FlowGraphView({
  graph,
  className,
  overlay,
  onNodeClick,
  selectedLabel,
}: {
  graph?: string | FlowGraph;
  className?: string;
  overlay?: Record<string, NodeOverlay>;
  // onNodeClick:点节点回调(传节点 label,用于按流程步骤筛任务)。缺省=只读不可点(向后兼容)。
  onNodeClick?: (label: string) => void;
  // selectedLabel:当前被选中的节点 label(同 label 节点一并高亮)。
  selectedLabel?: string | null;
}) {
  const g = typeof graph === "string" ? parseFlowGraph(graph) : (graph ?? null);
  if (!g) return null;
  const { placed, cols, rows } = layoutFlowGraph(g);
  const width = PAD * 2 + cols * COL_W;
  const height = PAD * 2 + (rows + 1) * ROW_H;
  const pos = new Map(
    placed.map((p) => [
      p.node.id,
      {
        x: PAD + p.col * COL_W,
        y: PAD + p.row * ROW_H,
      },
    ]),
  );
  const cx = (id: string) => (pos.get(id)?.x ?? 0) + NODE_W / 2;
  const cy = (id: string) => (pos.get(id)?.y ?? 0) + NODE_H / 2;

  return (
    <div
      className={cn(
        "relative overflow-auto rounded-lg border border-border bg-card/40",
        className,
      )}
      style={{ width: "100%", height }}
    >
      <svg width={width} height={height} className="absolute inset-0">
        {g.edges.map((e, i) => {
          const bounce = e.kind === "bounce";
          return (
            <line
              key={i}
              x1={cx(e.from)}
              y1={cy(e.from)}
              x2={cx(e.to)}
              y2={cy(e.to)}
              stroke={
                bounce ? "var(--color-status-waiting)" : "var(--color-primary)"
              }
              strokeWidth={1.5}
            />
          );
        })}
      </svg>
      {placed.map((p) => {
        const isSink = !g.edges.some(
          (e) => e.kind !== "bounce" && e.from === p.node.id,
        );
        const badge = kindBadge(g, p.node.id, p.node.kind, isSink);
        const ov = overlay?.[p.node.id];
        const clickable = !!onNodeClick;
        const selected =
          clickable && taskMatchesNode(p.node.label, selectedLabel);
        return (
          <div
            key={p.node.id}
            className={cn(
              "absolute flex flex-col justify-center rounded-md border border-border bg-card px-2 py-1",
              ov ? overlayClass(ov.status) : undefined,
              clickable && "cursor-pointer",
              selected && "ring-2 ring-primary",
            )}
            style={{
              left: pos.get(p.node.id)!.x,
              top: pos.get(p.node.id)!.y,
              width: NODE_W,
              height: NODE_H,
            }}
            title={p.node.brief}
            data-testid={`flow-node-${p.node.id}`}
            data-selected={selected ? "true" : undefined}
            role={clickable ? "button" : undefined}
            tabIndex={clickable ? 0 : undefined}
            onClick={clickable ? () => onNodeClick?.(p.node.label) : undefined}
            onKeyDown={
              clickable
                ? (e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      onNodeClick?.(p.node.label);
                    }
                  }
                : undefined
            }
          >
            <span className="truncate text-2xs font-medium text-foreground">
              {p.node.label}
            </span>
            <span className={cn("text-[9px]", badge.cls)}>{badge.label}</span>
            {ov && ov.count > 0 ? (
              <span
                data-testid={`flow-node-${p.node.id}-count`}
                className="absolute -right-1.5 -top-1.5 flex size-4 items-center justify-center rounded-full bg-foreground text-[9px] font-semibold text-background"
              >
                {ov.count}
              </span>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}
