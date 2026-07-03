import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TeamDepartmentPicker } from "../team-department-picker";
import type { PickerModel } from "../team-picker-data";

const dev = {
  id: 1,
  name: "王一之",
  avatarColor: "agent-1",
  backendType: "claudecode",
  departmentId: 10,
};
const dev2 = {
  id: 2,
  name: "阿则",
  avatarColor: "agent-3",
  backendType: "codex",
  departmentId: 10,
};
const prod = {
  id: 3,
  name: "见野",
  avatarColor: "agent-2",
  backendType: "claudecode",
  departmentId: 20,
};
const loose = {
  id: 4,
  name: "游侠",
  avatarColor: "agent-5",
  backendType: "codex",
  departmentId: 0,
};

const model: PickerModel = {
  all: [dev, dev2, prod, loose],
  departments: [
    {
      id: 10,
      name: "研发部",
      icon: "code",
      accentColor: "agent-1",
      agents: [dev, dev2],
    },
    {
      id: 20,
      name: "产品部",
      icon: "palette",
      accentColor: "agent-2",
      agents: [prod],
    },
  ],
  ungrouped: [loose],
};

function setup(value: number[] = []) {
  const onChange = vi.fn();
  render(
    <TeamDepartmentPicker model={model} value={value} onChange={onChange} />,
  );
  return { onChange };
}

describe("TeamDepartmentPicker", () => {
  it("左栏列出 全部/各部门/未分组", () => {
    setup();
    expect(screen.getByTestId("run-team-scope-all")).toBeInTheDocument();
    expect(screen.getByTestId("run-team-scope-10")).toBeInTheDocument();
    expect(screen.getByTestId("run-team-scope-20")).toBeInTheDocument();
    expect(screen.getByTestId("run-team-scope-ungrouped")).toBeInTheDocument();
  });

  it("默认 全部 视图展示所有可参与 agent", () => {
    setup();
    expect(screen.getByTestId("run-team-agent-1")).toBeInTheDocument();
    expect(screen.getByTestId("run-team-agent-3")).toBeInTheDocument();
    expect(screen.getByTestId("run-team-agent-4")).toBeInTheDocument();
  });

  it("点某部门 → 右栏只剩该部门成员", () => {
    setup();
    fireEvent.click(screen.getByTestId("run-team-scope-20"));
    expect(screen.getByTestId("run-team-agent-3")).toBeInTheDocument();
    expect(screen.queryByTestId("run-team-agent-1")).toBeNull();
  });

  it("勾选一个 agent → onChange 带该 id", () => {
    const { onChange } = setup([]);
    fireEvent.click(screen.getByTestId("run-team-agent-3"));
    expect(onChange).toHaveBeenCalledWith([3]);
  });

  it("已选 agent 再点 → onChange 去掉该 id", () => {
    const { onChange } = setup([3]);
    fireEvent.click(screen.getByTestId("run-team-agent-3"));
    expect(onChange).toHaveBeenCalledWith([]);
  });

  it("aria-pressed 反映选中态", () => {
    setup([3]);
    expect(screen.getByTestId("run-team-agent-3")).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByTestId("run-team-agent-1")).toHaveAttribute(
      "aria-pressed",
      "false",
    );
  });

  it("研发部『全选』→ onChange 含该部门全部成员", () => {
    const { onChange } = setup([]);
    fireEvent.click(screen.getByTestId("run-team-scope-10"));
    fireEvent.click(screen.getByTestId("run-team-select-all"));
    expect(onChange).toHaveBeenCalledWith([1, 2]);
  });

  it("已选计数 run-team-count 反映 value 长度", () => {
    setup([1, 3]);
    expect(screen.getByTestId("run-team-count").textContent).toMatch(/2/);
  });

  it("搜索过滤到跨部门扁平结果", () => {
    setup();
    fireEvent.change(screen.getByTestId("run-team-search"), {
      target: { value: "见" },
    });
    expect(screen.getByTestId("run-team-agent-3")).toBeInTheDocument();
    expect(screen.queryByTestId("run-team-agent-1")).toBeNull();
  });

  it("搜索无匹配 → 空态", () => {
    setup();
    fireEvent.change(screen.getByTestId("run-team-search"), {
      target: { value: "zzz" },
    });
    expect(screen.getByTestId("run-team-search-empty")).toBeInTheDocument();
  });

  it("有已选 → 底部汇总条出现", () => {
    setup([1]);
    expect(screen.getByTestId("run-team-summary")).toBeInTheDocument();
  });

  it("无已选 → 无汇总条", () => {
    setup([]);
    expect(screen.queryByTestId("run-team-summary")).toBeNull();
  });
});
