import { describe, expect, it } from "vitest";

import { groupAgentsByDepartment } from "../team-picker-data";

const depts = [
  { id: 10, name: "研发部", icon: "code", accentColor: "agent-1", sortOrder: 1 },
  { id: 20, name: "产品部", icon: "palette", accentColor: "agent-2", sortOrder: 0 },
];

describe("groupAgentsByDepartment", () => {
  it("空可参与集合 → 三块皆空", () => {
    const m = groupAgentsByDepartment([], depts, []);
    expect(m.all).toEqual([]);
    expect(m.departments).toEqual([]);
    expect(m.ungrouped).toEqual([]);
  });

  it("按部门归组并带上部门元数据(icon/accentColor/name)", () => {
    const m = groupAgentsByDepartment(
      [{ id: 1, name: "A", avatarColor: "agent-1", backendType: "claudecode" }],
      depts,
      [{ id: 1, departmentId: 10 }],
    );
    expect(m.departments).toHaveLength(1);
    expect(m.departments[0]).toMatchObject({ id: 10, name: "研发部", icon: "code", accentColor: "agent-1" });
    expect(m.departments[0].agents.map((a) => a.id)).toEqual([1]);
    expect(m.departments[0].agents[0].backendType).toBe("claudecode");
    expect(m.ungrouped).toEqual([]);
  });

  it("不在 org 映射里的可参与 agent → 未分组", () => {
    const m = groupAgentsByDepartment([{ id: 9, name: "游侠" }], depts, []);
    expect(m.ungrouped.map((a) => a.id)).toEqual([9]);
    expect(m.ungrouped[0].departmentId).toBe(0);
    expect(m.departments).toEqual([]);
  });

  it("departmentId 指向不存在的部门 → 未分组", () => {
    const m = groupAgentsByDepartment([{ id: 3, name: "X" }], depts, [{ id: 3, departmentId: 999 }]);
    expect(m.ungrouped.map((a) => a.id)).toEqual([3]);
    expect(m.departments).toEqual([]);
  });

  it("部门按 sortOrder 升序;无成员的部门不出现", () => {
    const m = groupAgentsByDepartment(
      [
        { id: 1, name: "A" },
        { id: 2, name: "B" },
      ],
      depts,
      [
        { id: 1, departmentId: 10 },
        { id: 2, departmentId: 20 },
      ],
    );
    // 产品部 sortOrder 0 在前, 研发部 sortOrder 1 在后
    expect(m.departments.map((d) => d.id)).toEqual([20, 10]);
    expect(m.all.map((a) => a.id)).toEqual([1, 2]);
  });
});
