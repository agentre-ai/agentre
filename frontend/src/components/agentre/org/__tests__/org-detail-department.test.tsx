import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { OrgDetailDepartment } from "../org-detail-department";
import type { OrgAgent, OrgDepartment } from "../types";

const dept = (overrides: Partial<OrgDepartment> = {}): OrgDepartment =>
  ({
    id: 1,
    name: "工程部",
    description: "",
    icon: "hammer",
    accentColor: "agent-2",
    parentId: 0,
    leadAgentId: 0,
    leadAgentName: "",
    sortOrder: 0,
    directAgentCount: 0,
    subdepartmentCount: 0,
    memberCount: 0,
    createtime: 0,
    updatetime: 0,
    ...overrides,
  }) as OrgDepartment;

const agent = (overrides: Partial<OrgAgent> = {}): OrgAgent =>
  ({
    id: 2,
    name: "Boris",
    description: "后端工程师",
    avatarColor: "agent-3",
    avatarIcon: "",
    avatarDataUrl: "",
    systemBadge: "",
    departmentId: 1,
    departmentName: "开发组",
    parentAgentId: 0,
    parentAgentName: "",
    agentBackendId: 1,
    sortOrder: 0,
    prompt: [],
    skills: [],
    createtime: 0,
    updatetime: 0,
    ...overrides,
  }) as OrgAgent;

describe("OrgDetailDepartment editor layout", () => {
  it("matches the department drawer labels and source-of-truth previews", () => {
    const parent = dept({
      id: 9,
      name: "工程部",
      icon: "code-xml",
      accentColor: "agent-3",
    });
    const current = dept({
      id: 1,
      name: "开发组",
      icon: "hammer",
      accentColor: "agent-2",
      parentId: parent.id,
      leadAgentId: 2,
    });
    const lead = agent();

    render(
      <OrgDetailDepartment
        department={current}
        allDepartments={[parent, current]}
        allAgents={[lead]}
        leadCandidates={[lead]}
        onUpdate={vi.fn()}
        onMove={vi.fn()}
        onDelete={vi.fn()}
        onSelect={vi.fn()}
        onClose={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "部门图标" })).toHaveTextContent(
      "锤子",
    );
    expect(
      screen.getByRole("radiogroup", { name: "主题色" }),
    ).toBeInTheDocument();
    expect(
      screen.getAllByRole("radio", { name: /主题色 agent-/ }),
    ).toHaveLength(10);
    expect(
      screen.getByRole("combobox", { name: "dept-parent" }),
    ).toHaveTextContent("工程部");
    expect(screen.getByRole("heading", { name: "Leader" })).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "部门长" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "成员" })).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "成员速览" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("combobox", { name: "dept-lead" }),
    ).toHaveTextContent("Boris");
    expect(screen.getAllByText("后端工程师").length).toBeGreaterThan(0);
  });
});

describe("OrgDetailDepartment delete dialog", () => {
  it("submits reparent strategy by default", async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    render(
      <OrgDetailDepartment
        department={dept()}
        allDepartments={[]}
        allAgents={[]}
        leadCandidates={[]}
        onUpdate={vi.fn()}
        onMove={vi.fn()}
        onDelete={onDelete}
        onSelect={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    await userEvent.click(
      screen.getAllByRole("button", { name: /删除部门/ })[0],
    );
    await userEvent.click(screen.getByRole("button", { name: /确认删除/ }));
    expect(onDelete).toHaveBeenCalledWith({ id: 1, strategy: "reparent" });
  });

  it("submits cascade strategy when picked", async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    render(
      <OrgDetailDepartment
        department={dept()}
        allDepartments={[]}
        allAgents={[]}
        leadCandidates={[]}
        onUpdate={vi.fn()}
        onMove={vi.fn()}
        onDelete={onDelete}
        onSelect={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    await userEvent.click(
      screen.getAllByRole("button", { name: /删除部门/ })[0],
    );
    fireEvent.click(screen.getByLabelText(/递归删除/));
    await userEvent.click(screen.getByRole("button", { name: /确认删除/ }));
    expect(onDelete).toHaveBeenCalledWith({ id: 1, strategy: "cascade" });
  });
});
