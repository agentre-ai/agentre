import type { OrgTreeLayout, OrgTreeLayoutNode } from "./org-tree";
import type { OrgAgent, OrgDepartment } from "./types";

const ZONE_HALF = 10; // 命中条半宽(canvas 坐标)

export type InsertZone = {
  id: string;
  kind: "agent" | "dept";
  departmentId: number; // agent 组键
  parentAgentId: number; // agent 组键
  parentId: number; // dept 组键
  index: number; // 插入下标(0..n)
  orderedIds: number[]; // 该组现序 id
  x: number; // 命中条中心 x(canvas 坐标)
  y: number;
  height: number;
};

export type ActiveDrag = {
  id: number;
  kind: "agent" | "dept";
  departmentId: number;
  parentAgentId: number;
  parentId: number;
};

type ZoneInput = {
  agents: OrgAgent[];
  departments: OrgDepartment[];
  collapse: Record<number, boolean>;
};

export function isZoneValidTarget(
  zone: InsertZone,
  active: ActiveDrag,
): boolean {
  if (zone.kind !== active.kind) return false;
  if (zone.kind === "agent") {
    return (
      zone.departmentId === active.departmentId &&
      zone.parentAgentId === active.parentAgentId
    );
  }
  return zone.parentId === active.parentId;
}

// 从一组左右相邻(按 x 升序)的节点算出 N+1 个插入位几何。
function slotsForGroup(
  nodes: OrgTreeLayoutNode[],
): Array<{ index: number; x: number; y: number; height: number }> {
  const sorted = [...nodes].sort((a, b) => a.x - b.x);
  const y = Math.min(...sorted.map((n) => n.y));
  const height = Math.max(...sorted.map((n) => n.height));
  const out: Array<{ index: number; x: number; y: number; height: number }> =
    [];
  for (let i = 0; i <= sorted.length; i++) {
    let x: number;
    if (i === 0) {
      x = sorted[0].x - sorted[0].width / 2 - ZONE_HALF;
    } else if (i === sorted.length) {
      const last = sorted[sorted.length - 1];
      x = last.x + last.width / 2 + ZONE_HALF;
    } else {
      const left = sorted[i - 1];
      const right = sorted[i];
      x = (left.x + left.width / 2 + (right.x - right.width / 2)) / 2;
    }
    out.push({ index: i, x, y, height });
  }
  return out;
}

export function buildInsertZones(
  layout: OrgTreeLayout,
  input: ZoneInput,
): InsertZone[] {
  const nodeByKey = new Map<string, OrgTreeLayoutNode>(
    layout.nodes.map((n) => [n.key, n]),
  );
  const zones: InsertZone[] = [];

  // ── agent 组:按 (departmentId, parentAgentId) 分组(排除 CEO/系统 agent)──
  const agentGroups = new Map<string, OrgAgent[]>();
  for (const a of input.agents) {
    if (a.systemBadge === "DEFAULT") continue;
    const key = `${a.departmentId ?? 0}:${a.parentAgentId ?? 0}`;
    if (!agentGroups.has(key)) agentGroups.set(key, []);
    agentGroups.get(key)!.push(a);
  }
  for (const members of agentGroups.values()) {
    const ordered = [...members].sort(
      (a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0) || a.id - b.id,
    );
    const nodes = ordered
      .map((a) => nodeByKey.get(`agent-${a.id}`))
      .filter((n): n is OrgTreeLayoutNode => Boolean(n));
    if (nodes.length !== ordered.length || nodes.length === 0) continue; // 有成员没渲染(折叠)→ 跳过
    const departmentId = ordered[0].departmentId ?? 0;
    const parentAgentId = ordered[0].parentAgentId ?? 0;
    const orderedIds = ordered.map((a) => a.id);
    for (const slot of slotsForGroup(nodes)) {
      zones.push({
        id: `insert-agent-${departmentId}-${parentAgentId}-${slot.index}`,
        kind: "agent",
        departmentId,
        parentAgentId,
        parentId: 0,
        index: slot.index,
        orderedIds,
        x: slot.x,
        y: slot.y,
        height: slot.height,
      });
    }
  }

  // ── department 组:按 parentId 分组 ──
  const deptGroups = new Map<number, OrgDepartment[]>();
  for (const d of input.departments) {
    const key = d.parentId ?? 0;
    if (!deptGroups.has(key)) deptGroups.set(key, []);
    deptGroups.get(key)!.push(d);
  }
  for (const [parentId, members] of deptGroups.entries()) {
    // 父部门折叠时子部门不渲染 → 跳过
    if (parentId > 0 && input.collapse[parentId]) continue;
    const ordered = [...members].sort(
      (a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0) || a.id - b.id,
    );
    const nodes = ordered
      .map((d) => nodeByKey.get(`dept-${d.id}`))
      .filter((n): n is OrgTreeLayoutNode => Boolean(n));
    if (nodes.length !== ordered.length || nodes.length === 0) continue;
    const orderedIds = ordered.map((d) => d.id);
    for (const slot of slotsForGroup(nodes)) {
      zones.push({
        id: `insert-dept-${parentId}-${slot.index}`,
        kind: "dept",
        departmentId: 0,
        parentAgentId: 0,
        parentId,
        index: slot.index,
        orderedIds,
        x: slot.x,
        y: slot.y,
        height: slot.height,
      });
    }
  }

  return zones;
}
