export type PickerAgent = {
  id: number;
  name: string;
  avatarColor: string;
  backendType: string;
  departmentId: number;
};

export type PickerDept = {
  id: number;
  name: string;
  icon: string;
  accentColor: string;
  agents: PickerAgent[];
};

export type PickerModel = {
  all: PickerAgent[];
  departments: PickerDept[];
  ungrouped: PickerAgent[];
};

export type ChatAgentLite = {
  id: number;
  name: string;
  avatarColor?: string;
  backendType?: string;
};

export type OrgAgentLite = { id: number; departmentId: number };

export type OrgDeptLite = {
  id: number;
  name: string;
  icon: string;
  accentColor: string;
  sortOrder: number;
};

// 把「可参与集合」(chatAgents) 按「部门元数据 + agent→部门映射」归组。
// - 资格集合永远以 chatAgents 为准(语义不变);
// - 解析不到部门(未映射 / 部门不存在)的进 ungrouped, departmentId=0;
// - departments 只含有 ≥1 名可参与 agent 的部门, 按 sortOrder 升序。
export function groupAgentsByDepartment(
  chatAgents: ChatAgentLite[],
  orgDepartments: OrgDeptLite[],
  orgAgents: OrgAgentLite[],
): PickerModel {
  const deptById = new Map<number, OrgDeptLite>();
  for (const d of orgDepartments) deptById.set(d.id, d);

  const deptByAgentId = new Map<number, number>();
  for (const a of orgAgents) deptByAgentId.set(a.id, a.departmentId);

  const all: PickerAgent[] = chatAgents.map((c) => {
    const raw = deptByAgentId.get(c.id) ?? 0;
    const departmentId = deptById.has(raw) ? raw : 0;
    return {
      id: c.id,
      name: c.name,
      avatarColor: c.avatarColor ?? "",
      backendType: c.backendType ?? "",
      departmentId,
    };
  });

  const byDept = new Map<number, PickerAgent[]>();
  const ungrouped: PickerAgent[] = [];
  for (const a of all) {
    if (a.departmentId === 0) {
      ungrouped.push(a);
      continue;
    }
    const list = byDept.get(a.departmentId) ?? [];
    list.push(a);
    byDept.set(a.departmentId, list);
  }

  const departments: PickerDept[] = [];
  for (const [deptId, agents] of byDept) {
    const d = deptById.get(deptId);
    if (!d) continue;
    departments.push({
      id: d.id,
      name: d.name,
      icon: d.icon,
      accentColor: d.accentColor,
      agents,
    });
  }
  departments.sort((a, b) => {
    const da = deptById.get(a.id)!;
    const db = deptById.get(b.id)!;
    return da.sortOrder - db.sortOrder || a.id - b.id;
  });

  return { all, departments, ungrouped };
}
