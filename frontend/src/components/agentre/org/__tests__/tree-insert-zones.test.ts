import { describe, expect, it } from "vitest";

import { buildOrgTreeLayout } from "../org-tree";
import { buildInsertZones, isZoneValidTarget } from "../tree-insert-zones";
import type { OrgAgent } from "../types";

function agent(p: Partial<OrgAgent> & { id: number }): OrgAgent {
  return {
    id: p.id,
    name: `a${p.id}`,
    description: "",
    avatarColor: "neutral",
    avatarIcon: "",
    avatarDataUrl: "",
    systemBadge: p.systemBadge ?? "",
    departmentId: p.departmentId ?? 0,
    parentAgentId: p.parentAgentId ?? 0,
    agentBackendId: 0,
    sortOrder: p.sortOrder ?? 0,
    skills: [],
  } as unknown as OrgAgent;
}

describe("buildInsertZones", () => {
  it("N 个同级 agent 产出 N+1 个插入位,orderedIds 为该组现序", () => {
    const ceo = agent({ id: 1, systemBadge: "DEFAULT" });
    const a2 = agent({ id: 2, parentAgentId: 1, sortOrder: 1 });
    const a3 = agent({ id: 3, parentAgentId: 1, sortOrder: 2 });
    const layout = buildOrgTreeLayout({
      agents: [ceo, a2, a3],
      departments: [],
      collapse: {},
    });
    const zones = buildInsertZones(layout, {
      agents: [ceo, a2, a3],
      departments: [],
      collapse: {},
    });
    const group = zones.filter(
      (z) => z.kind === "agent" && z.parentAgentId === 1,
    );
    expect(group).toHaveLength(3); // before / between / after
    expect(group.every((z) => z.orderedIds.join() === "2,3")).toBe(true);
    // x 递增
    expect(group[0].x).toBeLessThan(group[1].x);
    expect(group[1].x).toBeLessThan(group[2].x);
  });
});

describe("isZoneValidTarget", () => {
  const zone = {
    id: "insert-agent-0-1-0",
    kind: "agent" as const,
    departmentId: 0,
    parentAgentId: 1,
    parentId: 0,
    index: 0,
    orderedIds: [2, 3],
    x: 0,
    y: 0,
    height: 10,
  };
  it("同组同类型 → 合法", () => {
    expect(
      isZoneValidTarget(zone, {
        id: 2,
        kind: "agent",
        departmentId: 0,
        parentAgentId: 1,
        parentId: 0,
      }),
    ).toBe(true);
  });
  it("跨组 → 非法", () => {
    expect(
      isZoneValidTarget(zone, {
        id: 9,
        kind: "agent",
        departmentId: 5,
        parentAgentId: 0,
        parentId: 0,
      }),
    ).toBe(false);
  });
});
