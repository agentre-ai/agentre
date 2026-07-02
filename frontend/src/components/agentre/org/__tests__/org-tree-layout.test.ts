import { describe, expect, it } from "vitest";

import { buildOrgTreeLayout } from "../org-tree";
import type { OrgAgent, OrgDepartment } from "../types";

const mkAgent = (overrides: Partial<OrgAgent>): OrgAgent =>
  ({
    id: 0,
    name: "Agent",
    description: "",
    avatarColor: "agent-1",
    avatarDataUrl: "",
    avatarIcon: "",
    systemBadge: "",
    departmentId: 0,
    departmentName: "",
    parentAgentId: 0,
    parentAgentName: "",
    agentBackendId: 0,
    sortOrder: 0,
    prompt: [],
    skills: [],
    createtime: 0,
    updatetime: 0,
    ...overrides,
  }) as OrgAgent;

describe("buildOrgTreeLayout — sortOrder stable ordering", () => {
  it("places the agent with sortOrder=1 to the left of sortOrder=2 under CEO", () => {
    // CEO is the DEFAULT badge agent
    const ceo = mkAgent({ id: 1, systemBadge: "DEFAULT", sortOrder: 0 });
    // Two sub-agents: array order has sortOrder=2 first, sortOrder=1 second
    const agentSortOrder2 = mkAgent({
      id: 2,
      name: "Agent SortOrder 2",
      systemBadge: "",
      parentAgentId: 1,
      sortOrder: 2,
    });
    const agentSortOrder1 = mkAgent({
      id: 3,
      name: "Agent SortOrder 1",
      systemBadge: "",
      parentAgentId: 1,
      sortOrder: 1,
    });

    const agents: OrgAgent[] = [ceo, agentSortOrder2, agentSortOrder1];
    const departments: OrgDepartment[] = [];

    const layout = buildOrgTreeLayout({ agents, collapse: {}, departments });

    const node2 = layout.nodes.find((n) => n.key === "agent-2");
    const node3 = layout.nodes.find((n) => n.key === "agent-3");

    expect(node2).toBeDefined();
    expect(node3).toBeDefined();

    // sortOrder=1 (agent-3) should appear to the LEFT of sortOrder=2 (agent-2)
    expect(node3?.x ?? Infinity).toBeLessThan(node2?.x ?? -Infinity);
  });
});
