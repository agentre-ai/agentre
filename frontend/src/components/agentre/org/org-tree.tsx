/* eslint-disable react-refresh/only-export-components */
import * as React from "react";
import { ChevronDown, ChevronUp, Puzzle, Wrench } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import { sortableKeyboardCoordinates } from "@dnd-kit/sortable";

import { cn } from "@/lib/utils";

import { AgentAvatar } from "../primitives";
import { agentColorClassNames, type AgentColor } from "../types";
import {
  iconForKey,
  safeAgentColor,
  type OrgAgent,
  type OrgDepartment,
  type OrgSelection,
} from "./types";

export type OrgTreeProps = {
  departments: OrgDepartment[];
  agents: OrgAgent[];
  selected: OrgSelection;
  collapse: Record<number, boolean>;
  zoom: number;
  pan: { x: number; y: number };
  onSelect: (sel: OrgSelection) => void;
  onToggleCollapse: (departmentId: number) => void;
  onPanChange?: (pan: { x: number; y: number }) => void;
  onZoomChange?: (zoom: number) => void;
  onMoveAgent: (
    agentId: number,
    placement: { departmentId: number; parentAgentId: number },
  ) => void;
  onMoveDepartment: (departmentId: number, parentId: number) => void;
};

type DragId = `agent-${number}` | `dept-${number}`;
type DropId = `dept-${number}`;

const TREE_HORIZONTAL_GAP = 28;
const TREE_LEVEL_STEP = 148;
const TREE_CANVAS_PADDING = 16;
const AGENT_NODE_WIDTH = 180;
const AGENT_NODE_HEIGHT = 96;
const DEPARTMENT_NODE_WIDTH = 280;
const DEPARTMENT_NODE_HEIGHT = 48;
const EMPTY_NODE_WIDTH = 280;
const EMPTY_NODE_HEIGHT = 68;

const agentBorderClassNames: Record<AgentColor, string> = {
  "agent-1": "border-agent-1",
  "agent-2": "border-agent-2",
  "agent-3": "border-agent-3",
  "agent-4": "border-agent-4",
  "agent-5": "border-agent-5",
  "agent-6": "border-agent-6",
  "agent-7": "border-agent-7",
  "agent-8": "border-agent-8",
  "agent-9": "border-agent-9",
  "agent-10": "border-agent-10",
  neutral: "border-border",
};

function parseId(id: string): { kind: "agent" | "dept"; id: number } | null {
  if (id.startsWith("agent-")) {
    const parsed = Number(id.slice(6));
    return Number.isFinite(parsed) ? { kind: "agent", id: parsed } : null;
  }
  if (id.startsWith("dept-")) {
    const parsed = Number(id.slice(5));
    return Number.isFinite(parsed) ? { kind: "dept", id: parsed } : null;
  }
  return null;
}

export function isAncestor(
  departments: OrgDepartment[],
  fromID: number,
  toID: number,
): boolean {
  const byId = new Map(departments.map((d) => [d.id, d]));
  let cur: number | undefined = toID;
  while (cur && cur > 0) {
    if (cur === fromID) return true;
    cur = byId.get(cur)?.parentId;
  }
  return false;
}

export function isAgentAncestor(
  agents: OrgAgent[],
  fromID: number,
  toID: number,
): boolean {
  const byId = new Map(agents.map((a) => [a.id, a]));
  let cur: number | undefined = toID;
  while (cur && cur > 0) {
    if (cur === fromID) return true;
    cur = byId.get(cur)?.parentAgentId ?? 0;
  }
  return false;
}

export type OrgDragIntent =
  | {
      kind: "agent";
      id: number;
      departmentId: number;
      parentAgentId: number;
    }
  | { kind: "department"; id: number; parentId: number };

export function getOrgDragIntent(
  departments: OrgDepartment[],
  agents: OrgAgent[],
  activeId: string,
  overId: string,
): OrgDragIntent | null {
  const from = parseId(activeId);
  const to = parseId(overId);
  if (!from || !to) return null;

  if (from.kind === "agent") {
    if (to.kind === "dept") {
      return {
        kind: "agent",
        id: from.id,
        departmentId: to.id,
        parentAgentId: 0,
      };
    }
    if (to.kind === "agent") {
      if (to.id === from.id) return null;
      if (isAgentAncestor(agents, from.id, to.id)) return null;
      return {
        kind: "agent",
        id: from.id,
        departmentId: 0,
        parentAgentId: to.id,
      };
    }
    return null;
  }

  if (from.kind === "dept") {
    const newParentID =
      to.kind === "dept"
        ? to.id
        : agents.find((a) => a.id === to.id)?.systemBadge === "DEFAULT"
          ? 0
          : null;
    if (newParentID === null) return null;
    if (newParentID === from.id) return null;
    if (isAncestor(departments, from.id, newParentID)) return null;
    return { kind: "department", id: from.id, parentId: newParentID };
  }

  return null;
}

function dragStyle(transform: { x: number; y: number } | null) {
  if (!transform) return undefined;
  return {
    transform: `translate3d(${Math.round(transform.x)}px, ${Math.round(
      transform.y,
    )}px, 0)`,
  };
}

type TreeNode =
  | { key: string; kind: "agent"; id: number; agent: OrgAgent }
  | { key: string; kind: "dept"; id: number; dept: OrgDepartment }
  | { key: string; kind: "empty"; id: 0 };

type TreeNodeWithChildren = TreeNode & {
  children: TreeNodeWithChildren[];
};

type DraftLayout = {
  children: Array<{
    draft: DraftLayout;
    x: number;
  }>;
  contours: Map<number, { left: number; right: number }>;
  height: number;
  node: TreeNodeWithChildren;
  width: number;
};

export type OrgTreeLayoutNode = TreeNode & {
  height: number;
  width: number;
  x: number;
  y: number;
};

export type OrgTreeLayoutEdge = {
  childKey: string;
  endX: number;
  endY: number;
  fromKey: string;
  midY: number;
  startX: number;
  startY: number;
};

export type OrgTreeLayout = {
  edges: OrgTreeLayoutEdge[];
  height: number;
  nodes: OrgTreeLayoutNode[];
  width: number;
};

type OrgTreeLayoutInput = Pick<
  OrgTreeProps,
  "agents" | "collapse" | "departments"
>;

function nodeWidth(node: TreeNode): number {
  if (node.kind === "agent") return AGENT_NODE_WIDTH;
  if (node.kind === "empty") return EMPTY_NODE_WIDTH;
  return DEPARTMENT_NODE_WIDTH;
}

function nodeHeight(node: TreeNode): number {
  if (node.kind === "agent") return AGENT_NODE_HEIGHT;
  if (node.kind === "empty") return EMPTY_NODE_HEIGHT;
  return DEPARTMENT_NODE_HEIGHT;
}

function mergeContour(
  contours: Map<number, { left: number; right: number }>,
  level: number,
  left: number,
  right: number,
) {
  const existing = contours.get(level);
  contours.set(
    level,
    existing
      ? {
          left: Math.min(existing.left, left),
          right: Math.max(existing.right, right),
        }
      : { left, right },
  );
}

function contoursWithOffset(
  draft: DraftLayout,
  offsetX: number,
  levelOffset = 0,
): Map<number, { left: number; right: number }> {
  const out = new Map<number, { left: number; right: number }>();
  draft.contours.forEach((contour, level) => {
    out.set(level + levelOffset, {
      left: contour.left + offsetX,
      right: contour.right + offsetX,
    });
  });
  return out;
}

function packDrafts(drafts: DraftLayout[]) {
  const placed: Array<{ draft: DraftLayout; x: number }> = [];
  const merged = new Map<number, { left: number; right: number }>();

  drafts.forEach((draft) => {
    let x = 0;
    if (placed.length > 0) {
      draft.contours.forEach((contour, level) => {
        const previous = merged.get(level);
        if (!previous) return;
        x = Math.max(x, previous.right + TREE_HORIZONTAL_GAP - contour.left);
      });
    }

    placed.push({ draft, x });
    contoursWithOffset(draft, x).forEach((contour, level) => {
      mergeContour(merged, level, contour.left, contour.right);
    });
  });

  if (placed.length === 0) return placed;

  const first = placed[0];
  const last = placed[placed.length - 1];
  const centerShift = -(first.x + last.x) / 2;

  return placed.map((item) => ({ ...item, x: item.x + centerShift }));
}

function layoutDraft(node: TreeNodeWithChildren): DraftLayout {
  const width = nodeWidth(node);
  const height = nodeHeight(node);
  const childDrafts = node.children.map(layoutDraft);
  const children = packDrafts(childDrafts);
  const contours = new Map<number, { left: number; right: number }>();

  mergeContour(contours, 0, -width / 2, width / 2);
  children.forEach((child) => {
    contoursWithOffset(child.draft, child.x, 1).forEach((contour, level) => {
      mergeContour(contours, level, contour.left, contour.right);
    });
  });

  return {
    children,
    contours,
    height,
    node,
    width,
  };
}

function collectDraft(
  draft: DraftLayout,
  x: number,
  depth: number,
  nodes: OrgTreeLayoutNode[],
  edges: OrgTreeLayoutEdge[],
) {
  const y = depth * TREE_LEVEL_STEP;
  const current: OrgTreeLayoutNode = {
    ...draft.node,
    height: draft.height,
    width: draft.width,
    x,
    y,
  };
  nodes.push(current);

  draft.children.forEach((child) => {
    const childX = x + child.x;
    const childY = (depth + 1) * TREE_LEVEL_STEP;
    const childNode = child.draft.node;
    const startY = y + draft.height;
    const endY = childY;
    const midY = startY + (endY - startY) / 2;

    edges.push({
      childKey: childNode.key,
      endX: childX,
      endY,
      fromKey: draft.node.key,
      midY,
      startX: x,
      startY,
    });
    collectDraft(child.draft, childX, depth + 1, nodes, edges);
  });
}

function shiftLayout(
  nodes: OrgTreeLayoutNode[],
  edges: OrgTreeLayoutEdge[],
): OrgTreeLayout {
  if (nodes.length === 0) return { edges: [], height: 0, nodes: [], width: 0 };

  const minX = Math.min(...nodes.map((node) => node.x - node.width / 2));
  const maxX = Math.max(...nodes.map((node) => node.x + node.width / 2));
  const maxY = Math.max(...nodes.map((node) => node.y + node.height));
  const shiftX = TREE_CANVAS_PADDING - minX;

  return {
    edges: edges.map((edge) => ({
      ...edge,
      endX: edge.endX + shiftX,
      startX: edge.startX + shiftX,
    })),
    height: maxY + TREE_CANVAS_PADDING,
    nodes: nodes.map((node) => ({ ...node, x: node.x + shiftX })),
    width: maxX - minX + TREE_CANVAS_PADDING * 2,
  };
}

function agentChildren(agent: OrgAgent, all: OrgTreeLayoutInput) {
  return all.agents
    .filter((a) => (a.parentAgentId ?? 0) === agent.id && a.id !== agent.id)
    .map((a) => buildAgentNode(a, all));
}

function buildAgentNode(
  agent: OrgAgent,
  all: OrgTreeLayoutInput,
): TreeNodeWithChildren {
  return {
    agent,
    children: agentChildren(agent, all),
    id: agent.id,
    key: `agent-${agent.id}`,
    kind: "agent",
  };
}

function buildDepartmentNode(
  dept: OrgDepartment,
  all: OrgTreeLayoutInput,
): TreeNodeWithChildren {
  const collapsed = !!all.collapse[dept.id];
  const childAgents = all.agents.filter(
    (a) => a.departmentId === dept.id && (a.parentAgentId ?? 0) === 0,
  );
  const childDepts = all.departments.filter((d) => d.parentId === dept.id);

  return {
    children: collapsed
      ? []
      : [
          ...childAgents.map((agent) => buildAgentNode(agent, all)),
          ...childDepts.map((childDept) => buildDepartmentNode(childDept, all)),
        ],
    dept,
    id: dept.id,
    key: `dept-${dept.id}`,
    kind: "dept",
  };
}

function buildLayoutRoots(all: OrgTreeLayoutInput): TreeNodeWithChildren[] {
  const ceo = all.agents.find((a) => a.systemBadge === "DEFAULT") ?? null;
  const topDepartments = all.departments.filter((d) => d.parentId === 0);
  const topAgents = all.agents.filter(
    (a) =>
      a.systemBadge !== "DEFAULT" &&
      ((a.parentAgentId ?? 0) === ceo?.id ||
        (a.departmentId === 0 && (a.parentAgentId ?? 0) === 0)),
  );
  const rootChildren: TreeNodeWithChildren[] = [
    ...topAgents.map((agent) => buildAgentNode(agent, all)),
    ...topDepartments.map((dept) => buildDepartmentNode(dept, all)),
  ];

  if (ceo) {
    return [
      {
        agent: ceo,
        children:
          rootChildren.length > 0
            ? rootChildren
            : [{ children: [], id: 0, key: "empty-0", kind: "empty" }],
        id: ceo.id,
        key: `agent-${ceo.id}`,
        kind: "agent",
      },
    ];
  }

  return rootChildren;
}

export function buildOrgTreeLayout(all: OrgTreeLayoutInput): OrgTreeLayout {
  const roots = buildLayoutRoots(all);
  const drafts = roots.map(layoutDraft);
  const placed = packDrafts(drafts);
  const nodes: OrgTreeLayoutNode[] = [];
  const edges: OrgTreeLayoutEdge[] = [];

  placed.forEach((root) => {
    collectDraft(root.draft, root.x, 0, nodes, edges);
  });

  return shiftLayout(nodes, edges);
}

export function OrgTree(props: OrgTreeProps) {
  const [panning, setPanning] = React.useState<{
    pointerId: number;
    startX: number;
    startY: number;
    originX: number;
    originY: number;
  } | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const handleDragEnd = React.useCallback(
    (e: DragEndEvent) => {
      if (!e.over) return;
      const intent = getOrgDragIntent(
        props.departments,
        props.agents,
        String(e.active.id),
        String(e.over.id),
      );
      if (!intent) return;
      if (intent.kind === "agent") {
        props.onMoveAgent(intent.id, {
          departmentId: intent.departmentId,
          parentAgentId: intent.parentAgentId,
        });
        return;
      }
      props.onMoveDepartment(intent.id, intent.parentId);
    },
    [props],
  );

  const layout = React.useMemo(
    () =>
      buildOrgTreeLayout({
        agents: props.agents,
        collapse: props.collapse,
        departments: props.departments,
      }),
    [props.agents, props.collapse, props.departments],
  );

  const handlePointerDown = React.useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      if (e.button !== 0 || !props.onPanChange) return;
      const target = e.target as HTMLElement;
      if (
        target.closest(
          '[data-org-node="true"], button, input, select, textarea, [role="button"]',
        )
      ) {
        return;
      }
      e.currentTarget.setPointerCapture(e.pointerId);
      setPanning({
        pointerId: e.pointerId,
        startX: e.clientX,
        startY: e.clientY,
        originX: props.pan.x,
        originY: props.pan.y,
      });
    },
    [props],
  );

  const handlePointerMove = React.useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      if (!panning || !props.onPanChange) return;
      props.onPanChange({
        x: panning.originX + e.clientX - panning.startX,
        y: panning.originY + e.clientY - panning.startY,
      });
    },
    [panning, props],
  );

  const stopPanning = React.useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      if (!panning) return;
      if (e.currentTarget.hasPointerCapture(panning.pointerId)) {
        e.currentTarget.releasePointerCapture(panning.pointerId);
      }
      setPanning(null);
    },
    [panning],
  );

  const handleWheel = React.useCallback(
    (e: React.WheelEvent<HTMLDivElement>) => {
      if (e.ctrlKey || e.metaKey) {
        if (!props.onZoomChange) return;
        e.preventDefault();
        props.onZoomChange(props.zoom + (e.deltaY > 0 ? -0.1 : 0.1));
        return;
      }

      if (!props.onPanChange) return;
      const horizontalDelta =
        e.deltaX !== 0 ? e.deltaX : e.shiftKey ? e.deltaY : 0;
      const verticalDelta = e.shiftKey ? 0 : e.deltaY;
      if (horizontalDelta === 0 && verticalDelta === 0) return;
      e.preventDefault();
      props.onPanChange({
        x: props.pan.x - horizontalDelta,
        y: props.pan.y - verticalDelta,
      });
    },
    [props],
  );

  return (
    <DndContext sensors={sensors} onDragEnd={handleDragEnd}>
      <div
        data-slot="org-tree-viewport"
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={stopPanning}
        onPointerCancel={stopPanning}
        onWheel={handleWheel}
        className={cn(
          "h-full min-w-0 flex-1 overflow-hidden bg-muted/40 p-7",
          panning ? "cursor-grabbing select-none" : "cursor-grab",
        )}
      >
        <div
          data-slot="org-tree-pan"
          style={{
            transform: `translate3d(${props.pan.x}px, ${props.pan.y}px, 0)`,
            transformOrigin: "top left",
          }}
          className="flex min-h-full min-w-full flex-col items-start"
        >
          <div
            data-slot="org-tree"
            style={{
              transform: `scale(${props.zoom})`,
              transformOrigin: "top center",
            }}
            className="inline-flex flex-col items-center gap-4"
          >
            <TreeCanvas all={props} layout={layout} />
          </div>
        </div>
      </div>
    </DndContext>
  );
}

function TreeCanvas({
  all,
  layout,
}: {
  all: OrgTreeProps;
  layout: OrgTreeLayout;
}) {
  return (
    <div
      className="relative shrink-0"
      data-slot="org-tree-canvas"
      style={{ height: layout.height, width: layout.width }}
    >
      <svg
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 overflow-visible"
        data-slot="org-tree-connectors"
        height={layout.height}
        width={layout.width}
      >
        {layout.edges.map((edge) => (
          <path
            key={`${edge.fromKey}-${edge.childKey}`}
            d={`M ${edge.startX} ${edge.startY} V ${edge.midY} H ${edge.endX} V ${edge.endY}`}
            fill="none"
            stroke="var(--border-strong)"
            strokeLinecap="square"
            strokeWidth="2"
          />
        ))}
      </svg>
      {layout.nodes.map((node) => (
        <div
          key={node.key}
          className="absolute flex items-start justify-center"
          data-slot="org-tree-node"
          style={{
            height: node.height,
            left: node.x - node.width / 2,
            top: node.y,
            width: node.width,
          }}
        >
          <TreeLayoutNode all={all} node={node} />
        </div>
      ))}
    </div>
  );
}

function TreeLayoutNode({
  all,
  node,
}: {
  all: OrgTreeProps;
  node: OrgTreeLayoutNode;
}) {
  if (node.kind === "agent") {
    return <AgentCard agent={node.agent} all={all} accented={false} />;
  }
  if (node.kind === "empty") {
    return <EmptyOrgPlaceholder />;
  }
  return <DepartmentBanner dept={node.dept} all={all} />;
}

function EmptyOrgPlaceholder() {
  const { t } = useTranslation();

  return (
    <div
      data-slot="org-empty-placeholder"
      className="h-[68px] w-[280px] rounded-lg border border-dashed border-border-strong bg-card/80 px-4 py-3 text-center shadow-xs"
    >
      <div className="text-xs font-semibold text-foreground">
        {t("org.tree.empty.title")}
      </div>
      <div className="mt-1 max-w-[260px] text-2xs leading-5 text-muted-foreground">
        {t("org.tree.empty.description")}
      </div>
    </div>
  );
}

function DepartmentBanner({
  dept,
  all,
}: {
  dept: OrgDepartment;
  all: OrgTreeProps;
}) {
  const { t } = useTranslation();
  const dragId: DragId = `dept-${dept.id}`;
  const dropId: DropId = `dept-${dept.id}`;
  const drag = useDraggable({ id: dragId });
  const drop = useDroppable({ id: dropId });
  const setRefs = React.useCallback(
    (node: HTMLDivElement | null) => {
      drag.setNodeRef(node);
      drop.setNodeRef(node);
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [drag.setNodeRef, drop.setNodeRef],
  );

  const accent = safeAgentColor(dept.accentColor);
  const collapsed = !!all.collapse[dept.id];
  const selected =
    all.selected?.kind === "department" && all.selected.id === dept.id;
  const iconNode = React.createElement(iconForKey(dept.icon), {
    className: "size-3.5",
    "aria-hidden": true,
  });
  const chevronNode = React.createElement(collapsed ? ChevronDown : ChevronUp, {
    className: "size-4",
    "aria-hidden": true,
  });

  return (
    <div
      ref={setRefs}
      {...drag.listeners}
      {...drag.attributes}
      style={dragStyle(drag.transform)}
      role="button"
      tabIndex={0}
      onClick={() => all.onSelect({ kind: "department", id: dept.id })}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          all.onSelect({ kind: "department", id: dept.id });
        }
      }}
      data-slot="org-dept-banner"
      data-org-node="true"
      data-dept-id={dept.id}
      className={cn(
        "group inline-flex cursor-pointer items-center gap-2.5 rounded-full border-2 bg-card text-left shadow-xs outline-none transition-all",
        "hover:shadow-md focus-visible:ring-[3px] focus-visible:ring-ring/50",
        "h-[48px] w-[280px] px-3",
        selected
          ? "border-primary bg-primary-soft shadow-md shadow-primary/10"
          : agentBorderClassNames[accent],
        drop.isOver && "ring-2 ring-primary/60",
        drag.isDragging && "relative z-20 opacity-80 shadow-lg",
      )}
    >
      <span
        className={cn(
          "inline-flex shrink-0 size-6 items-center justify-center rounded-md text-white",
          agentColorClassNames[accent],
        )}
      >
        {iconNode}
      </span>
      <span className="min-w-0 flex flex-1 flex-col leading-tight">
        <span className="flex items-center gap-2">
          <span className="font-semibold truncate text-xs">{dept.name}</span>
          {dept.leadAgentName && (
            <span className="inline-flex min-w-0 max-w-[170px] items-center gap-1 rounded-sm bg-secondary px-1.5 py-0.5 font-mono text-2xs font-semibold">
              <span
                aria-hidden="true"
                className={cn(
                  "inline-block size-3.5 shrink-0 rounded-full",
                  agentColorClassNames[accent],
                )}
              />
              <span className="truncate">
                {t("org.tree.lead", { name: dept.leadAgentName })}
              </span>
            </span>
          )}
        </span>
        <span className="font-mono text-2xs text-muted-foreground">
          {t("org.tree.memberCount", { count: dept.memberCount })}
          {collapsed && t("org.tree.collapsedSuffix")}
        </span>
      </span>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          all.onToggleCollapse(dept.id);
        }}
        onPointerDown={(e) => e.stopPropagation()}
        className="ml-auto inline-flex size-[22px] shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent"
        aria-label={
          collapsed
            ? t("org.tree.expand", { name: dept.name })
            : t("org.tree.collapse", { name: dept.name })
        }
      >
        {chevronNode}
      </button>
    </div>
  );
}

function AgentCard({
  agent,
  all,
  accented,
}: {
  agent: OrgAgent;
  all: OrgTreeProps;
  accented: boolean;
}) {
  const { t } = useTranslation();
  const dragId: DragId = `agent-${agent.id}`;
  const dropId: DragId = `agent-${agent.id}`;
  const drag = useDraggable({
    id: dragId,
    disabled: agent.systemBadge === "DEFAULT",
  });
  const drop = useDroppable({ id: dropId });
  const setRefs = React.useCallback(
    (node: HTMLButtonElement | null) => {
      drag.setNodeRef(node);
      drop.setNodeRef(node);
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [drag.setNodeRef, drop.setNodeRef],
  );
  const selected =
    all.selected?.kind === "agent" && all.selected.id === agent.id;
  const color = safeAgentColor(agent.avatarColor);
  const leadOfDept = all.departments.find((d) => d.leadAgentId === agent.id);
  const leadAccent = leadOfDept ? safeAgentColor(leadOfDept.accentColor) : null;

  return (
    <button
      ref={setRefs}
      {...drag.listeners}
      {...drag.attributes}
      style={dragStyle(drag.transform)}
      type="button"
      role="treeitem"
      aria-selected={selected}
      aria-label={`${agent.name}${agent.description ? `，${agent.description}` : ""}`}
      onClick={() => all.onSelect({ kind: "agent", id: agent.id })}
      data-slot="org-agent-card"
      data-org-node="true"
      data-agent-id={agent.id}
      className={cn(
        "flex h-24 w-[180px] shrink-0 cursor-pointer flex-col gap-2 overflow-hidden rounded-lg border bg-card px-3 py-2.5 text-left shadow-xs outline-none transition-all",
        "hover:shadow-md focus-visible:ring-[3px] focus-visible:ring-ring/50",
        selected
          ? "border-primary border-2 bg-primary-soft shadow-md shadow-primary/10"
          : leadAccent
            ? `border-2 ${agentBorderClassNames[leadAccent]}`
            : accented
              ? `border-2 ${agentBorderClassNames[color]}`
              : "border-border",
        drop.isOver && "ring-2 ring-primary/60",
        drag.isDragging && "relative z-20 opacity-80 shadow-lg",
      )}
    >
      <span className="flex min-w-0 items-center gap-2.5">
        <AgentAvatar
          name={agent.name}
          color={color}
          size="md"
          className="shrink-0"
          avatarDataUrl={agent.avatarDataUrl}
          avatarIcon={agent.avatarIcon}
        />
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="truncate text-sm font-semibold">{agent.name}</span>
          </span>
          <span className="mt-0.5 flex min-w-0 items-center gap-1">
            {leadOfDept && leadAccent && (
              <span className="shrink-0 inline-flex items-center gap-0.5 rounded-sm bg-secondary px-1 py-0 font-mono text-2xs text-foreground">
                <span
                  aria-hidden="true"
                  className={cn(
                    "inline-block size-2 rounded-full",
                    agentColorClassNames[leadAccent],
                  )}
                />
                {t("org.department.leadBadge")}
              </span>
            )}
            <span className="truncate text-2xs text-muted-foreground">
              {agent.description}
            </span>
          </span>
        </span>
      </span>
      <span className="mt-auto flex min-w-0 items-center gap-1 overflow-hidden">
        <BackendChip agent={agent} />
      </span>
    </button>
  );
}

function BackendChip({ agent }: { agent: OrgAgent }) {
  const label = agent.backend?.name ?? "—";
  const Icon = agent.backend?.type === "claude-code" ? Wrench : Puzzle;
  return (
    <span className="inline-flex min-w-0 max-w-[140px] items-center gap-1 overflow-hidden rounded-sm bg-secondary px-1 py-1 font-mono text-2xs text-secondary-foreground">
      <Icon
        className="size-2.5 shrink-0 text-muted-foreground"
        aria-hidden="true"
      />
      <span className="truncate">{label}</span>
    </span>
  );
}
