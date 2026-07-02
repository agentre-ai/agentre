import type { OrgAgent, OrgDepartment } from "./types";

/**
 * Produces a new ordered ID array after dragging `draggedId` to `insertIndex`.
 * The `orderedIds` param is the current complete sibling list in display order.
 *
 * `insertIndex` is a slot over the FULL group (0..n, meaning "before the element
 * currently at position i", or "after the last" when i === n) — this is what the
 * tree's static insert-zones emit, since they don't know which element is being
 * dragged. After the dragged element is removed, any target slot to its right
 * shifts left by one; dropping in either slot adjacent to the pick-up point is a
 * no-op.
 */
export function computeReorder(
  orderedIds: number[],
  draggedId: number,
  insertIndex: number,
): number[] {
  const from = orderedIds.indexOf(draggedId);
  if (from < 0) return orderedIds.slice();
  const without = orderedIds.filter((id) => id !== draggedId);
  const adjusted = insertIndex > from ? insertIndex - 1 : insertIndex;
  const clamped = Math.max(0, Math.min(adjusted, without.length));
  without.splice(clamped, 0, draggedId);
  return without;
}

/**
 * Applies a new sortOrder to agents that belong to the given
 * (departmentId, parentAgentId) group, based on orderedIds position.
 * Agents outside the group are returned unchanged.
 */
export function applyAgentOrder(
  agents: OrgAgent[],
  departmentId: number,
  parentAgentId: number,
  orderedIds: number[],
): OrgAgent[] {
  const positionMap = new Map<number, number>(
    orderedIds.map((id, index) => [id, index + 1]),
  );
  return agents.map((agent) => {
    const newPos = positionMap.get(agent.id);
    if (newPos === undefined) return agent;
    // Only touch agents in the matching group
    if (
      agent.departmentId !== departmentId ||
      (agent.parentAgentId ?? 0) !== parentAgentId
    ) {
      return agent;
    }
    return Object.assign(Object.create(Object.getPrototypeOf(agent) as object) as OrgAgent, agent, { sortOrder: newPos }) as OrgAgent;
  });
}

/**
 * Applies a new sortOrder to departments under the given parentId,
 * based on orderedIds position. Departments in other groups are unchanged.
 */
export function applyDepartmentOrder(
  departments: OrgDepartment[],
  parentId: number,
  orderedIds: number[],
): OrgDepartment[] {
  const positionMap = new Map<number, number>(
    orderedIds.map((id, index) => [id, index + 1]),
  );
  return departments.map((dept) => {
    const newPos = positionMap.get(dept.id);
    if (newPos === undefined) return dept;
    if (dept.parentId !== parentId) return dept;
    return Object.assign(Object.create(Object.getPrototypeOf(dept) as object) as OrgDepartment, dept, { sortOrder: newPos }) as OrgDepartment;
  });
}
