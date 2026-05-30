import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  buildOrgTreeLayout,
  getOrgDragIntent,
  isAncestor,
  OrgTree,
} from "../org-tree";
import type { OrgAgent, OrgDepartment } from "../types";

const mk = (id: number, parentId: number): OrgDepartment =>
  ({ id, parentId }) as OrgDepartment;

const agent = (overrides: Partial<OrgAgent> = {}): OrgAgent =>
  ({
    id: 100,
    name: "CEO 助手",
    description: "默认入口",
    avatarColor: "agent-1",
    avatarDataUrl: "",
    systemBadge: "DEFAULT",
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

describe("isAncestor", () => {
  const tree: OrgDepartment[] = [mk(1, 0), mk(2, 1), mk(3, 2), mk(4, 0)];

  it("returns true when moving 1 under its own descendant", () => {
    expect(isAncestor(tree, 1, 3)).toBe(true);
  });

  it("returns true when moving 1 under direct child 2", () => {
    expect(isAncestor(tree, 1, 2)).toBe(true);
  });

  it("returns false when moving 4 under 1", () => {
    expect(isAncestor(tree, 4, 1)).toBe(false);
  });

  it("returns false when target is top level", () => {
    expect(isAncestor(tree, 1, 0)).toBe(false);
  });
});

describe("getOrgDragIntent", () => {
  const tree: OrgDepartment[] = [mk(1, 0), mk(2, 1), mk(3, 2), mk(4, 0)];
  const agents: OrgAgent[] = [
    agent({ id: 9, systemBadge: "", parentAgentId: 0 }),
    agent({ id: 10, systemBadge: "", parentAgentId: 0 }),
    agent({ id: 11, systemBadge: "", parentAgentId: 9 }),
  ];

  it("allows agents to move into departments", () => {
    expect(getOrgDragIntent(tree, agents, "agent-9", "dept-1")).toEqual({
      kind: "agent",
      id: 9,
      departmentId: 1,
      parentAgentId: 0,
    });
  });

  it("allows agents to move under another agent", () => {
    expect(getOrgDragIntent(tree, agents, "agent-9", "agent-10")).toEqual({
      kind: "agent",
      id: 9,
      departmentId: 0,
      parentAgentId: 10,
    });
  });

  it("does not move agents to the root", () => {
    expect(getOrgDragIntent(tree, agents, "agent-9", "dept-root")).toBeNull();
  });

  it("blocks moving an agent under its own descendant", () => {
    expect(getOrgDragIntent(tree, agents, "agent-9", "agent-11")).toBeNull();
  });

  it("allows departments to move to the root by dropping on the CEO assistant", () => {
    expect(
      getOrgDragIntent(tree, [agent({ id: 1 })], "dept-2", "agent-1"),
    ).toEqual({
      kind: "department",
      id: 2,
      parentId: 0,
    });
  });

  it("does not move departments by dropping on normal agents", () => {
    expect(
      getOrgDragIntent(
        tree,
        [agent({ id: 1, systemBadge: "" })],
        "dept-2",
        "agent-1",
      ),
    ).toBeNull();
  });

  it("blocks moving a department under its own descendant", () => {
    expect(getOrgDragIntent(tree, agents, "dept-1", "dept-3")).toBeNull();
  });
});

describe("OrgTree empty departments", () => {
  it("shows the first-department placeholder under the CEO card", () => {
    render(
      <OrgTree
        departments={[]}
        agents={[agent()]}
        selected={null}
        collapse={{}}
        zoom={1}
        pan={{ x: 0, y: 0 }}
        onSelect={vi.fn()}
        onToggleCollapse={vi.fn()}
        onMoveAgent={vi.fn()}
        onMoveDepartment={vi.fn()}
      />,
    );

    expect(screen.getByText(/No departments created/)).toBeInTheDocument();
  });

  it("does not render a separate top-level department drop zone", () => {
    render(
      <OrgTree
        departments={[]}
        agents={[agent()]}
        selected={null}
        collapse={{}}
        zoom={1}
        pan={{ x: 0, y: 0 }}
        onSelect={vi.fn()}
        onToggleCollapse={vi.fn()}
        onMoveAgent={vi.fn()}
        onMoveDepartment={vi.fn()}
      />,
    );

    expect(screen.queryByText(/公司顶层/)).not.toBeInTheDocument();
  });

  it("scales only the tree content instead of the full-width pan layer", () => {
    const { container } = render(
      <OrgTree
        departments={[]}
        agents={[agent()]}
        selected={null}
        collapse={{}}
        zoom={0.6}
        pan={{ x: 12, y: 8 }}
        onSelect={vi.fn()}
        onToggleCollapse={vi.fn()}
        onMoveAgent={vi.fn()}
        onMoveDepartment={vi.fn()}
      />,
    );

    const panLayer = container.querySelector<HTMLElement>(
      '[data-slot="org-tree-pan"]',
    );
    const scaleLayer = container.querySelector<HTMLElement>(
      '[data-slot="org-tree"]',
    );

    expect(panLayer?.style.transform).toBe("translate3d(12px, 8px, 0)");
    expect(panLayer?.style.transform).not.toContain("scale");
    expect(scaleLayer?.style.transform).toBe("scale(0.6)");
    expect(scaleLayer?.style.transformOrigin).toBe("top center");
  });

  it("uses mouse wheel movement for vertical canvas panning", () => {
    const onPanChange = vi.fn();
    const onZoomChange = vi.fn();
    const { container } = render(
      <OrgTree
        departments={[]}
        agents={[agent()]}
        selected={null}
        collapse={{}}
        zoom={1}
        pan={{ x: 12, y: 8 }}
        onSelect={vi.fn()}
        onToggleCollapse={vi.fn()}
        onMoveAgent={vi.fn()}
        onMoveDepartment={vi.fn()}
        onPanChange={onPanChange}
        onZoomChange={onZoomChange}
      />,
    );

    const viewport = container.querySelector<HTMLElement>(
      '[data-slot="org-tree-viewport"]',
    );

    expect(viewport).toBeInTheDocument();
    fireEvent.wheel(viewport!, { deltaY: 120 });

    expect(onPanChange).toHaveBeenCalledWith({ x: 12, y: -112 });
    expect(onZoomChange).not.toHaveBeenCalled();
  });

  it("uses shift wheel movement for horizontal canvas panning", () => {
    const onPanChange = vi.fn();
    const onZoomChange = vi.fn();
    const { container } = render(
      <OrgTree
        departments={[]}
        agents={[agent()]}
        selected={null}
        collapse={{}}
        zoom={1}
        pan={{ x: 12, y: 8 }}
        onSelect={vi.fn()}
        onToggleCollapse={vi.fn()}
        onMoveAgent={vi.fn()}
        onMoveDepartment={vi.fn()}
        onPanChange={onPanChange}
        onZoomChange={onZoomChange}
      />,
    );

    const viewport = container.querySelector<HTMLElement>(
      '[data-slot="org-tree-viewport"]',
    );

    expect(viewport).toBeInTheDocument();
    const event = new WheelEvent("wheel", {
      bubbles: true,
      cancelable: true,
      deltaY: 120,
    });
    Object.defineProperty(event, "shiftKey", { value: true });
    fireEvent(viewport!, event);

    expect(onPanChange).toHaveBeenCalledWith({ x: -108, y: 8 });
    expect(onZoomChange).not.toHaveBeenCalled();
  });

  it("uses horizontal wheel movement for horizontal canvas panning", () => {
    const onPanChange = vi.fn();
    const onZoomChange = vi.fn();
    const { container } = render(
      <OrgTree
        departments={[]}
        agents={[agent()]}
        selected={null}
        collapse={{}}
        zoom={1}
        pan={{ x: 12, y: 8 }}
        onSelect={vi.fn()}
        onToggleCollapse={vi.fn()}
        onMoveAgent={vi.fn()}
        onMoveDepartment={vi.fn()}
        onPanChange={onPanChange}
        onZoomChange={onZoomChange}
      />,
    );

    const viewport = container.querySelector<HTMLElement>(
      '[data-slot="org-tree-viewport"]',
    );

    expect(viewport).toBeInTheDocument();
    fireEvent.wheel(viewport!, { deltaX: 80, deltaY: 0 });

    expect(onPanChange).toHaveBeenCalledWith({ x: -68, y: 8 });
    expect(onZoomChange).not.toHaveBeenCalled();
  });
});

describe("OrgTree branch connectors", () => {
  it("draws connector paths from parent center to each child center", () => {
    const departments = [
      {
        ...mk(1, 0),
        name: "工程部",
        icon: "hammer",
        accentColor: "agent-2",
        leadAgentName: "",
        memberCount: 1,
      } as OrgDepartment,
    ];
    const agents = [
      agent({ id: 1, systemBadge: "DEFAULT" }),
      agent({ id: 2, name: "Eva", parentAgentId: 1, systemBadge: "" }),
    ];

    const layout = buildOrgTreeLayout({ agents, collapse: {}, departments });
    const ceo = layout.nodes.find((node) => node.key === "agent-1");
    const childEdges = layout.edges.filter(
      (edge) => edge.fromKey === "agent-1",
    );

    expect(ceo).toBeDefined();
    expect(childEdges).toHaveLength(2);
    expect(childEdges.map((edge) => edge.startX)).toEqual([ceo?.x, ceo?.x]);
    expect(childEdges.map((edge) => edge.childKey).sort()).toEqual([
      "agent-2",
      "dept-1",
    ]);
  });

  it("packs direct siblings by their visible row contours", () => {
    const departments = [
      { ...mk(1, 0), name: "工程部" } as OrgDepartment,
      { ...mk(2, 0), name: "产品部" } as OrgDepartment,
    ];
    const agents = [
      agent({ id: 1, systemBadge: "DEFAULT" }),
      agent({ id: 2, name: "平台", parentAgentId: 1, systemBadge: "" }),
      agent({ id: 3, name: "前端", parentAgentId: 2, systemBadge: "" }),
      agent({ id: 4, name: "后端", parentAgentId: 2, systemBadge: "" }),
      agent({ id: 5, name: "测试", parentAgentId: 2, systemBadge: "" }),
    ];

    const layout = buildOrgTreeLayout({ agents, collapse: {}, departments });
    const platform = layout.nodes.find((node) => node.key === "agent-2");
    const engineering = layout.nodes.find((node) => node.key === "dept-1");

    expect(platform).toBeDefined();
    expect(engineering).toBeDefined();
    expect(Math.abs((engineering?.x ?? 0) - (platform?.x ?? 0))).toBeLessThan(
      270,
    );
  });
});
