# 新建编排弹窗 · 团队按部门选择（双栏选择器）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把「新建编排」弹窗里扁平的可参与 agent chips 换成方案 B 双栏部门选择器（顶部全局搜索 + 左部门导航 + 右 agent 勾选 + 底部团队汇总）。

**Architecture:** 一个纯函数 `groupAgentsByDepartment()` 把「可参与集合」（`ListChatAgents`）与「部门元数据」（`LoadOrg`）join 成分组模型；一个受控展示组件 `TeamDepartmentPicker` 渲染双栏 UI 并只产出 `allowedAgentIds: number[]`；`run-new-dialog.tsx` 只做数据注入。**零后端改动。**

**Tech Stack:** React 19 + TypeScript + Vitest + @testing-library/react + Tailwind v4 + shadcn `@/components/ui/*` + react-i18next + Wails 绑定。

## Global Constraints

- **TDD：Red → Green → Refactor**，先写失败测试再实现（本仓库硬约束）。
- **前端 UI 文案必须走 i18n**：`react-i18next` 的 `t(...)`，同时更新 `frontend/src/i18n/locales/zh-CN/common.json` 与 `en/common.json`；`i18next/no-literal-string` 会拦截 JSX 里硬编码的中文。品牌拉丁串（如 `Claude Code`）不算中文、可直接写。
- **表单控件统一用 shadcn `@/components/ui/*`**，禁止原生 `<select>`。
- **只走 Wails 绑定**（`ListChatAgents` / `LoadOrg` / `RunCreate` 均已存在），不加 HTTP 风格 app API。
- **不动无关文件**：diff 只含本计划列出的文件；不顺手改别的编排文件、不 formatter 全量刷、不重排 import。
- **前端测试用 Vitest + `vi.mock` per-file** mock wailsjs 绑定（组件测试里 mock `../../../../../wailsjs/go/app/App`，5 层路径）。
- **共享分支 `develop/wyz`**：提交用 `git commit <pathspec>` 只带本任务文件；gitmoji 前缀；**不 push**（由用户决定）。
- **收尾门禁**：`make lint`（golangci-lint + ESLint，含 `i18next/no-literal-string` 与 tsc）+ 全量 `cd frontend && pnpm test`，看真实 exit code。
- 并发改动：同日 `2026-07-03-orchestration-project-selection-design.md` 会在同一弹窗「编排流程」后加「项目」下拉。若它已落地，保留其改动，只替换团队区；勿覆盖其 import/JSX。

---

## File Structure

- **Create** `frontend/src/components/agentre/orchestration/team-picker-data.ts` — 纯函数 `groupAgentsByDepartment` + 类型（`PickerAgent/PickerDept/PickerModel/ChatAgentLite/OrgAgentLite/OrgDeptLite`）。
- **Create** `frontend/src/components/agentre/orchestration/__tests__/team-picker-data.test.ts` — 纯函数单测。
- **Create** `frontend/src/components/agentre/orchestration/team-department-picker.tsx` — 受控双栏选择器组件。
- **Create** `frontend/src/components/agentre/orchestration/__tests__/team-department-picker.test.tsx` — 组件测试。
- **Modify** `frontend/src/i18n/locales/zh-CN/common.json` + `frontend/src/i18n/locales/en/common.json` — `orchestration.new.team*` 新键。
- **Modify** `frontend/src/components/agentre/orchestration/run-new-dialog.tsx` — 拉 `LoadOrg`、接入 `TeamDepartmentPicker`、删旧扁平团队区。
- **Modify** `frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx` — 加 `LoadOrg` mock、改团队相关用例。

---

## Task 1: 纯函数 `groupAgentsByDepartment`

**Files:**
- Create: `frontend/src/components/agentre/orchestration/team-picker-data.ts`
- Test: `frontend/src/components/agentre/orchestration/__tests__/team-picker-data.test.ts`

**Interfaces:**
- Produces:
  - `type PickerAgent = { id: number; name: string; avatarColor: string; backendType: string; departmentId: number }`
  - `type PickerDept = { id: number; name: string; icon: string; accentColor: string; agents: PickerAgent[] }`
  - `type PickerModel = { all: PickerAgent[]; departments: PickerDept[]; ungrouped: PickerAgent[] }`
  - `type ChatAgentLite = { id: number; name: string; avatarColor?: string; backendType?: string }`
  - `type OrgAgentLite = { id: number; departmentId: number }`
  - `type OrgDeptLite = { id: number; name: string; icon: string; accentColor: string; sortOrder: number }`
  - `function groupAgentsByDepartment(chatAgents: ChatAgentLite[], orgDepartments: OrgDeptLite[], orgAgents: OrgAgentLite[]): PickerModel`

- [ ] **Step 1: 写失败测试**

创建 `__tests__/team-picker-data.test.ts`：

```ts
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/team-picker-data.test.ts`
Expected: FAIL —「Failed to resolve import "../team-picker-data"」或 `groupAgentsByDepartment is not a function`。

- [ ] **Step 3: 写最小实现**

创建 `team-picker-data.ts`：

```ts
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
    departments.push({ id: d.id, name: d.name, icon: d.icon, accentColor: d.accentColor, agents });
  }
  departments.sort((a, b) => {
    const da = deptById.get(a.id)!;
    const db = deptById.get(b.id)!;
    return da.sortOrder - db.sortOrder || a.id - b.id;
  });

  return { all, departments, ungrouped };
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/team-picker-data.test.ts`
Expected: PASS（5 passed）。

- [ ] **Step 5: 提交**

```bash
git commit frontend/src/components/agentre/orchestration/team-picker-data.ts \
  frontend/src/components/agentre/orchestration/__tests__/team-picker-data.test.ts \
  -m "✨ orchestration: 团队按部门归组纯函数 groupAgentsByDepartment"
```

---

## Task 2: `TeamDepartmentPicker` 双栏组件

**Files:**
- Create: `frontend/src/components/agentre/orchestration/team-department-picker.tsx`
- Test: `frontend/src/components/agentre/orchestration/__tests__/team-department-picker.test.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`、`frontend/src/i18n/locales/en/common.json`

**Interfaces:**
- Consumes: `PickerModel`, `PickerAgent`（Task 1）。
- Produces: `function TeamDepartmentPicker(props: { model: PickerModel; value: number[]; onChange: (ids: number[]) => void }): JSX.Element`。testid：`run-team-search` / `run-team-scope-all` / `run-team-scope-<deptId>` / `run-team-scope-ungrouped` / `run-team-select-all` / `run-team-agent-<id>`（`aria-pressed`）/ `run-team-count` / `run-team-summary` / `run-team-search-empty`。

- [ ] **Step 1: 加 i18n 键（两个 locale）**

在 `zh-CN/common.json` 的 `orchestration.new` 对象里，新增（放在 `teamHint` 之后）：

```json
"teamAllAgents": "全部 Agent",
"teamUngrouped": "未分组",
"teamSelectAll": "全选",
"teamDeptMembers": "{{count}} 名成员",
"teamSummary": "本次团队",
"teamTotal": "共 {{count}} 人",
"teamSearchPlaceholder": "搜索 agent…",
"teamSearchEmpty": "无匹配 agent",
"teamSearchResults": "搜索结果",
```

在 `en/common.json` 的 `orchestration.new` 里对应新增：

```json
"teamAllAgents": "All agents",
"teamUngrouped": "Ungrouped",
"teamSelectAll": "Select all",
"teamDeptMembers": "{{count}} members",
"teamSummary": "This team",
"teamTotal": "{{count}} total",
"teamSearchPlaceholder": "Search agents…",
"teamSearchEmpty": "No matching agents",
"teamSearchResults": "Search results",
```

（保留既有 `team` / `teamSelected` / `teamHint`，不删。）

- [ ] **Step 2: 写失败测试**

创建 `__tests__/team-department-picker.test.tsx`：

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TeamDepartmentPicker } from "../team-department-picker";
import type { PickerModel } from "../team-picker-data";

const dev = { id: 1, name: "王一之", avatarColor: "agent-1", backendType: "claudecode", departmentId: 10 };
const dev2 = { id: 2, name: "阿则", avatarColor: "agent-3", backendType: "codex", departmentId: 10 };
const prod = { id: 3, name: "见野", avatarColor: "agent-2", backendType: "claudecode", departmentId: 20 };
const loose = { id: 4, name: "游侠", avatarColor: "agent-5", backendType: "codex", departmentId: 0 };

const model: PickerModel = {
  all: [dev, dev2, prod, loose],
  departments: [
    { id: 10, name: "研发部", icon: "code", accentColor: "agent-1", agents: [dev, dev2] },
    { id: 20, name: "产品部", icon: "palette", accentColor: "agent-2", agents: [prod] },
  ],
  ungrouped: [loose],
};

function setup(value: number[] = []) {
  const onChange = vi.fn();
  render(<TeamDepartmentPicker model={model} value={value} onChange={onChange} />);
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
    expect(screen.getByTestId("run-team-agent-3")).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("run-team-agent-1")).toHaveAttribute("aria-pressed", "false");
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
    fireEvent.change(screen.getByTestId("run-team-search"), { target: { value: "见" } });
    expect(screen.getByTestId("run-team-agent-3")).toBeInTheDocument();
    expect(screen.queryByTestId("run-team-agent-1")).toBeNull();
  });

  it("搜索无匹配 → 空态", () => {
    setup();
    fireEvent.change(screen.getByTestId("run-team-search"), { target: { value: "zzz" } });
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
```

- [ ] **Step 3: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/team-department-picker.test.tsx`
Expected: FAIL —「Failed to resolve import "../team-department-picker"」。

- [ ] **Step 4: 写实现**

创建 `team-department-picker.tsx`：

```tsx
import * as React from "react";
import { Check, CircleDashed, Minus, Users } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

import { iconForKey } from "../icon-registry";
import { firstLetter, tokenToCssColor } from "../session-avatar";
import type { PickerAgent, PickerModel } from "./team-picker-data";

export type TeamDepartmentPickerProps = {
  model: PickerModel;
  value: number[];
  onChange: (ids: number[]) => void;
};

type Scope = "all" | "ungrouped" | number;

// 后端类型 → 展示标签(品牌拉丁串, 不进 i18n)。未知类型原样显示。
const BACKEND_LABEL: Record<string, string> = {
  claudecode: "Claude Code",
  codex: "Codex",
  builtin: "Built-in",
};
function backendLabel(t: string): string {
  return BACKEND_LABEL[t] ?? t;
}

export function TeamDepartmentPicker({ model, value, onChange }: TeamDepartmentPickerProps) {
  const { t } = useTranslation();
  const [scope, setScope] = React.useState<Scope>("all");
  const [query, setQuery] = React.useState("");
  const selected = React.useMemo(() => new Set(value), [value]);

  const q = query.trim().toLowerCase();
  const searching = q.length > 0;

  const scopeAgents: PickerAgent[] =
    scope === "all"
      ? model.all
      : scope === "ungrouped"
        ? model.ungrouped
        : (model.departments.find((d) => d.id === scope)?.agents ?? []);
  const shown = searching
    ? model.all.filter((a) => a.name.toLowerCase().includes(q))
    : scopeAgents;

  const toggle = (id: number) => {
    const next = new Set(value);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChange([...next]);
  };

  const shownIds = shown.map((a) => a.id);
  const allShownSelected = shownIds.length > 0 && shownIds.every((id) => selected.has(id));
  const someShownSelected = shownIds.some((id) => selected.has(id));
  const selectAllState: CbState = allShownSelected ? "checked" : someShownSelected ? "partial" : "empty";
  const toggleSelectAll = () => {
    const next = new Set(value);
    if (allShownSelected) shownIds.forEach((id) => next.delete(id));
    else shownIds.forEach((id) => next.add(id));
    onChange([...next]);
  };

  const title = searching
    ? t("orchestration.new.teamSearchResults")
    : scope === "all"
      ? t("orchestration.new.teamAllAgents")
      : scope === "ungrouped"
        ? t("orchestration.new.teamUngrouped")
        : (model.departments.find((d) => d.id === scope)?.name ?? "");

  const selectedAgents = value
    .map((id) => model.all.find((a) => a.id === id))
    .filter((a): a is PickerAgent => Boolean(a));

  return (
    <div className="flex flex-col gap-2 text-xs">
      <div className="flex items-center gap-2">
        <span className="font-medium text-foreground">{t("orchestration.new.team")}</span>
        <span
          data-testid="run-team-count"
          className="ml-auto rounded-full bg-primary-soft px-2 py-0.5 text-primary-text"
        >
          {t("orchestration.new.teamSelected", { count: value.length })}
        </span>
      </div>

      <div className="overflow-hidden rounded-md border border-border bg-card">
        <div className="border-b border-border">
          <Input
            data-testid="run-team-search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("orchestration.new.teamSearchPlaceholder")}
            className="h-9 border-0 bg-transparent text-xs shadow-none focus-visible:ring-0"
          />
        </div>

        <div className="flex">
          {!searching ? (
            <div className="flex w-[168px] shrink-0 flex-col gap-0.5 border-r border-border bg-sidebar py-1">
              <NavItem
                testid="run-team-scope-all"
                active={scope === "all"}
                onClick={() => setScope("all")}
                icon={<Users className="size-3.5 text-muted-foreground" aria-hidden="true" />}
                name={t("orchestration.new.teamAllAgents")}
                count={model.all.length}
              />
              {model.departments.map((d) => {
                const Icon = iconForKey(d.icon);
                return (
                  <NavItem
                    key={d.id}
                    testid={`run-team-scope-${d.id}`}
                    active={scope === d.id}
                    onClick={() => setScope(d.id)}
                    icon={
                      <span
                        className="flex size-4 items-center justify-center rounded-sm"
                        style={{ backgroundColor: tokenToCssColor(d.accentColor) ?? "#94a3b8" }}
                      >
                        <Icon className="size-2.5 text-white" aria-hidden="true" />
                      </span>
                    }
                    name={d.name}
                    count={d.agents.length}
                  />
                );
              })}
              {model.ungrouped.length > 0 ? (
                <NavItem
                  testid="run-team-scope-ungrouped"
                  active={scope === "ungrouped"}
                  onClick={() => setScope("ungrouped")}
                  icon={<CircleDashed className="size-3.5 text-muted-foreground" aria-hidden="true" />}
                  name={t("orchestration.new.teamUngrouped")}
                  count={model.ungrouped.length}
                />
              ) : null}
            </div>
          ) : null}

          <div className="flex min-w-0 flex-1 flex-col">
            <div className="flex items-center gap-2 border-b border-border px-3 py-2">
              <span className="font-semibold text-foreground">{title}</span>
              {shown.length > 0 ? (
                <span className="text-subtle-foreground">
                  {t("orchestration.new.teamDeptMembers", { count: shown.length })}
                </span>
              ) : null}
              {shown.length > 0 ? (
                <button
                  type="button"
                  data-testid="run-team-select-all"
                  onClick={toggleSelectAll}
                  className="ml-auto flex items-center gap-1.5 text-primary-text"
                >
                  <span>{t("orchestration.new.teamSelectAll")}</span>
                  <CheckBox state={selectAllState} />
                </button>
              ) : null}
            </div>

            {shown.length === 0 ? (
              <div
                data-testid="run-team-search-empty"
                className="px-3 py-6 text-center text-muted-foreground"
              >
                {t("orchestration.new.teamSearchEmpty")}
              </div>
            ) : (
              <div className="flex flex-col">
                {shown.map((a) => {
                  const on = selected.has(a.id);
                  return (
                    <button
                      key={a.id}
                      type="button"
                      data-testid={`run-team-agent-${a.id}`}
                      aria-pressed={on}
                      onClick={() => toggle(a.id)}
                      className={cn(
                        "flex items-center gap-2.5 border-t border-border px-3 py-2 text-left first:border-t-0",
                        on ? "bg-primary-soft" : "bg-card",
                      )}
                    >
                      <CheckBox state={on ? "checked" : "empty"} />
                      <AgentDot color={a.avatarColor} name={a.name} />
                      <span className={cn("text-foreground", on ? "font-medium" : "")}>{a.name}</span>
                      {a.backendType ? (
                        <span className="ml-auto rounded-sm bg-secondary px-1.5 py-0.5 font-mono text-2xs text-muted-foreground">
                          {backendLabel(a.backendType)}
                        </span>
                      ) : null}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        </div>

        {selectedAgents.length > 0 ? (
          <div
            data-testid="run-team-summary"
            className="flex items-center gap-2 border-t border-border bg-muted px-3 py-2"
          >
            <span className="font-medium text-muted-foreground">{t("orchestration.new.teamSummary")}</span>
            <span className="flex items-center gap-1">
              {selectedAgents.slice(0, 8).map((a) => (
                <AgentDot key={a.id} color={a.avatarColor} name={a.name} small />
              ))}
              {selectedAgents.length > 8 ? (
                <span className="text-2xs text-muted-foreground">+{selectedAgents.length - 8}</span>
              ) : null}
            </span>
            <span className="ml-auto font-medium text-foreground">
              {t("orchestration.new.teamTotal", { count: selectedAgents.length })}
            </span>
          </div>
        ) : null}
      </div>

      <span className="text-2xs text-muted-foreground">{t("orchestration.new.teamHint")}</span>
    </div>
  );
}

function NavItem({
  testid,
  active,
  onClick,
  icon,
  name,
  count,
}: {
  testid: string;
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  name: string;
  count: number;
}) {
  return (
    <button
      type="button"
      data-testid={testid}
      onClick={onClick}
      className={cn(
        "mx-1 flex items-center gap-2 rounded-md px-2 py-1.5 text-left",
        active ? "bg-primary-soft font-medium text-primary-text" : "text-foreground hover:bg-accent",
      )}
    >
      {icon}
      <span className="min-w-0 flex-1 truncate">{name}</span>
      <span className="font-mono text-2xs text-subtle-foreground">{count}</span>
    </button>
  );
}

type CbState = "checked" | "partial" | "empty";
function CheckBox({ state }: { state: CbState }) {
  if (state === "empty") {
    return <span className="size-4 shrink-0 rounded-sm border-[1.5px] border-border-strong" aria-hidden="true" />;
  }
  return (
    <span
      className="flex size-4 shrink-0 items-center justify-center rounded-sm bg-primary text-white"
      aria-hidden="true"
    >
      {state === "partial" ? <Minus className="size-3" /> : <Check className="size-3" />}
    </span>
  );
}

function AgentDot({ color, name, small = false }: { color: string; name: string; small?: boolean }) {
  const bg = tokenToCssColor(color) ?? "#94a3b8";
  return (
    <span
      aria-hidden="true"
      className={cn(
        "flex shrink-0 items-center justify-center rounded-full font-semibold text-white",
        small ? "size-[18px] text-[9px]" : "size-5 text-2xs",
      )}
      style={{ backgroundColor: bg }}
    >
      {firstLetter(name)}
    </span>
  );
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/team-department-picker.test.tsx`
Expected: PASS（12 passed）。

- [ ] **Step 6: 提交**

```bash
git commit frontend/src/components/agentre/orchestration/team-department-picker.tsx \
  frontend/src/components/agentre/orchestration/__tests__/team-department-picker.test.tsx \
  frontend/src/i18n/locales/zh-CN/common.json \
  frontend/src/i18n/locales/en/common.json \
  -m "✨ orchestration: 团队双栏部门选择器组件 TeamDepartmentPicker"
```

---

## Task 3: 接入 `run-new-dialog.tsx` 并改测试

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/run-new-dialog.tsx`
- Modify: `frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`

**Interfaces:**
- Consumes: `TeamDepartmentPicker`、`groupAgentsByDepartment`、`OrgDeptLite`、`OrgAgentLite`（Task 1/2）；Wails `LoadOrg()`（返回 `{ departments, agents }`）。
- Produces: 弹窗提交仍是 `RunCreate({ ..., allowedAgentIds })`，其它字段行为不变。

- [ ] **Step 1: 先改测试到失败态**

编辑 `__tests__/run-new-dialog.test.tsx`：

(a) 在 hoisted mock 里加 `LoadOrg`：

```ts
const appMocks = vi.hoisted(() => ({
  RunCreate: vi.fn(),
  ListChatAgents: vi.fn(),
  WorkflowList: vi.fn(),
  LoadOrg: vi.fn(),
}));
```

(b) `beforeEach` 里给 `ListChatAgents` 补 `avatarColor`/`backendType`，并 mock `LoadOrg`（让 id=2 属研发部、id=3 未分组）：

```ts
appMocks.ListChatAgents.mockResolvedValue({
  agents: [
    { id: 2, name: "架构师", avatarColor: "agent-1", backendType: "claudecode", defaultPermissionMode: "default" },
    { id: 3, name: "危险Agent", avatarColor: "agent-4", backendType: "codex", defaultPermissionMode: "bypassPermissions" },
  ],
});
appMocks.LoadOrg.mockResolvedValue({
  departments: [{ id: 10, name: "研发部", icon: "code", accentColor: "agent-1", sortOrder: 0 }],
  agents: [
    { id: 2, departmentId: 10 },
    { id: 3, departmentId: 0 },
  ],
});
```

(c) 把 `describe("可参与 agent chips", ...)` 整块替换为新选择器用例（testid 从 `run-team-<id>` 改成 `run-team-agent-<id>`）：

```ts
describe("可参与团队(双栏部门选择器)", () => {
  it("默认 全部 视图列出可参与 agent", async () => {
    renderDialog();
    expect(await screen.findByTestId("run-team-agent-2")).toBeInTheDocument();
    expect(screen.getByTestId("run-team-agent-3")).toBeInTheDocument();
  });

  it("勾选的 agent 进入 RunCreate.allowedAgentIds", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderDialog();
    fireEvent.change(await screen.findByTestId("run-goal"), { target: { value: "做登录页" } });
    await user.click(screen.getByTestId("run-leader"));
    await user.click(await screen.findByRole("option", { name: "架构师" }));
    await user.click(await screen.findByTestId("run-team-agent-3"));
    await user.click(screen.getByTestId("run-create"));
    await waitFor(() =>
      expect(appMocks.RunCreate).toHaveBeenCalledWith(
        expect.objectContaining({ allowedAgentIds: [3] }),
      ),
    );
  });

  it("已选计数随选择更新", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderDialog();
    const count = await screen.findByTestId("run-team-count");
    expect(count.textContent).toMatch(/0/);
    await user.click(await screen.findByTestId("run-team-agent-3"));
    await waitFor(() => expect(count.textContent).toMatch(/1/));
  });

  it("部门『全选』把该部门成员写入 allowedAgentIds", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderDialog();
    fireEvent.change(await screen.findByTestId("run-goal"), { target: { value: "x" } });
    await user.click(screen.getByTestId("run-leader"));
    await user.click(await screen.findByRole("option", { name: "架构师" }));
    await user.click(await screen.findByTestId("run-team-scope-10"));
    await user.click(screen.getByTestId("run-team-select-all"));
    await user.click(screen.getByTestId("run-create"));
    await waitFor(() =>
      expect(appMocks.RunCreate).toHaveBeenCalledWith(
        expect.objectContaining({ allowedAgentIds: [2] }),
      ),
    );
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`
Expected: FAIL — 找不到 `run-team-agent-2` 等（组件尚未接入）。

- [ ] **Step 3: 改 `run-new-dialog.tsx`**

(a) wails 绑定 import 加 `LoadOrg`（`run-new-dialog.tsx:35-39`）：

```tsx
import {
  ListChatAgents,
  LoadOrg,
  RunCreate,
  WorkflowList,
} from "../../../../wailsjs/go/app/App";
```

(b) 顶部加组件/纯函数/类型 import（放在 `useWorkflowManagerStore` import 之后）：

```tsx
import { TeamDepartmentPicker } from "./team-department-picker";
import {
  groupAgentsByDepartment,
  type OrgAgentLite,
  type OrgDeptLite,
} from "./team-picker-data";
```

(c) `AgentItem` 本地类型加 `backendType`（`run-new-dialog.tsx:56-61`）：

```tsx
type AgentItem = {
  id: number;
  name: string;
  avatarColor: string;
  backendType: string;
  defaultPermissionMode: string;
};
```

(d) 加 org 状态（在 `const [workflows, ...]` 之后）：

```tsx
const [orgDepartments, setOrgDepartments] = React.useState<OrgDeptLite[]>([]);
const [orgAgents, setOrgAgents] = React.useState<OrgAgentLite[]>([]);
```

(e) `useEffect` 里：reset 段加 `setOrgDepartments([]); setOrgAgents([]);`；`ListChatAgents` 的 map 补 `backendType: a.backendType`（类型入参加 `backendType: string`）；并发加一段 `LoadOrg()`：

```tsx
    ListChatAgents()
      .then((resp) => {
        setAgents(
          (resp?.agents ?? []).map(
            (a: {
              id: number;
              name: string;
              avatarColor: string;
              backendType: string;
              defaultPermissionMode: string;
            }) => ({
              id: a.id,
              name: a.name,
              avatarColor: a.avatarColor,
              backendType: a.backendType,
              defaultPermissionMode: a.defaultPermissionMode,
            }),
          ),
        );
      })
      .catch(() => setAgents([]));

    LoadOrg()
      .then((resp) => {
        setOrgDepartments(
          (resp?.departments ?? []).map((d) => ({
            id: d.id,
            name: d.name,
            icon: d.icon,
            accentColor: d.accentColor,
            sortOrder: d.sortOrder,
          })),
        );
        setOrgAgents(
          (resp?.agents ?? []).map((a) => ({ id: a.id, departmentId: a.departmentId })),
        );
      })
      .catch(() => {
        setOrgDepartments([]);
        setOrgAgents([]);
      });
```

(f) 删掉 `toggleAllowed`（`run-new-dialog.tsx:150-155`，已被组件内部逻辑取代）。

(g) 在 `const selectedWorkflow = ...` 之后加分组模型 memo：

```tsx
const pickerModel = React.useMemo(
  () => groupAgentsByDepartment(agents, orgDepartments, orgAgents),
  [agents, orgDepartments, orgAgents],
);
```

(h) 把整段旧团队区（`run-new-dialog.tsx:360-407`，`{agents.length > 0 ? (...) : null}`）替换为：

```tsx
          <TeamDepartmentPicker
            model={pickerModel}
            value={allowedAgentIds}
            onChange={setAllowedAgentIds}
          />
```

- [ ] **Step 4: 跑该文件测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`
Expected: PASS（全部用例，含新团队用例 + 既有 goal/leader/flow 用例）。

- [ ] **Step 5: 全量前端门禁**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration src/__tests__/i18n.test.ts`
Expected: PASS（编排目录全绿 + i18n zh/en 覆盖新键）。

Run: `make lint`
Expected: PASS（ESLint 无 `i18next/no-literal-string` 报错、tsc 无类型错）。

Run: `cd frontend && pnpm test`
Expected: PASS（真实 exit code 0；确认没有别的用例引用旧 `run-team-<id>` testid 而挂）。

- [ ] **Step 6: 提交**

```bash
git commit frontend/src/components/agentre/orchestration/run-new-dialog.tsx \
  frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx \
  -m "✨ orchestration: 新建弹窗接入双栏部门团队选择器(替换扁平 chips)"
```

---

## Self-Review 结论

- **Spec 覆盖**：数据来源(Task 1 join `ListChatAgents`+`LoadOrg`,零后端)、双栏 UI + 顶部搜索 + 汇总(Task 2)、组件拆分 + 弹窗接入 + `allowedAgentIds` 不变(Task 3)、i18n 两 locale + 搜索空态测试(Task 2/3)——均有对应任务。
- **无占位**：所有步骤含真实代码/命令/期望输出。
- **类型一致**：`groupAgentsByDepartment` / `PickerModel` / `TeamDepartmentPicker` props / testid 命名在三个任务间一致；`OrgDeptLite`/`OrgAgentLite` 由 Task 1 定义、Task 3 复用。
- **超出 spec 的一处**：新增 `teamSearchResults` 键(搜索态右栏标题),已在 Task 2 i18n 步骤补两 locale。
