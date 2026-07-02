# 编排模块补全:总览页 + 流程库入口 + 筛选 + 流程下拉 + 文案 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补齐编排(orchestration)模块四个已设计但未落地/未接线的缺口 —— 让「运行中/已完成」筛选真正生效、加一个总览落地页(统计 + 进行中卡片带进度条 + 最近完成)、补两个流程库可见入口、把「从流程库」的单选列表换成下拉框、并把「起始流程」文案改为「编排流程」。

**Architecture:** 纯前端改动,零后端/零迁移。总览页的进度条通过对少量 running Run 懒调 `RunLoad`(复用 `useOrchRunStore`,顺带暖 detail 缓存)拿 `done/total`,其余统计(运行中/等待/本周完成/平均时长)全部从 `orch-run-list-store` 已加载的 `runs` 客户端 derive。流程库入口调用既有的 `useWorkflowManagerStore.openBrowse()`(弹窗已挂在 App 根)。

**Tech Stack:** React 19 + TypeScript, Vite, Tailwind v4, shadcn `@/components/ui/*`, zustand store, react-i18next, Vitest + @testing-library/react + userEvent。

## Global Constraints

- **纯前端**:不改任何 `.go`、不加迁移、不改 wails binding、不跑 `make mock` / `wails generate`。`RunItemDTO`/`RunDetailDTO` 现有字段够用。
- **i18n**:所有新增可见文案走 `t("...")`,并**同时**更新 `frontend/src/i18n/locales/zh-CN/common.json` 与 `frontend/src/i18n/locales/en/common.json`(两个 locale 必须 key 对齐,否则 `src/__tests__/i18n.test.ts` 挂)。禁止在 JSX 里硬编码中文。
- **测试语言 = 英文**:`src/__tests__/setup.ts` 把 i18n 切到 `en`,所以测试里断言可见文案要用**英文值**或直接用 `data-testid`;不要断言中文字符串。
- **时间单位 = 毫秒**:`orch_repo/run.go` 用 `time.Now().UnixMilli()` 写 `createtime`/`updatetime`,DTO 里就是毫秒,直接喂 `relativeTime(ms)`,**不要** `* 1000`。
- **表单控件用 shadcn `@/components/ui/*`**,禁止原生 `<select>`。
- **Radix `Select` 测试**:用 `userEvent.setup({ pointerEventsCheck: 0 })` → `user.click(trigger)` → `user.click(findByRole("option", { name }))`;`fireEvent.change` 对它无效。
- **提交纪律**:`develop/wyz` 是共享并发分支,**每次提交都带 pathspec**(`git commit <files>`),绝不用裸 `git commit`。gitmoji 提交信息。
- **改动只碰本任务的 producer + 其测试**,禁止顺手 refactor / 格式化 / 改无关文件。

---

## File Structure

| 文件 | 责任 | 任务 |
| --- | --- | --- |
| `src/i18n/locales/{zh-CN,en}/common.json` | 文案 key(改 `orchestration.new.flow`,新增 `list.filterEmpty`/`list.libraryEntry`/`new.flowManage`/`overview.*`) | T1–T5 |
| `src/components/agentre/orchestration/run-list.tsx` | 侧栏筛选 chip 接线 | T1 |
| `src/components/agentre/orchestration/orchestration-page.tsx` | 壳:侧栏底部固定「流程库」入口 + 主区渲染总览 | T2, T5 |
| `src/components/agentre/orchestration/run-new-dialog.tsx` | 「管理流程库→」链接 + 流程下拉 + 文案 | T2, T3 |
| `src/components/agentre/orchestration/overview-data.ts`(新) | 纯函数:统计 / 进行中 / 最近完成 / 进度 / 时长格式化 | T4 |
| `src/components/agentre/orchestration/orchestration-overview.tsx`(新) | 总览页组件 | T5 |
| `src/components/agentre/orchestration/__tests__/*.test.tsx` | 对应测试(run-list / run-new-dialog / orchestration-page + 新增 overview-data / orchestration-overview) | T1–T5 |

---

## Task 1: 侧栏筛选 chip 接线(全部 / 运行中 / 已完成 可点)

**Files:**
- Modify: `src/components/agentre/orchestration/run-list.tsx:48-195`
- Modify: `src/i18n/locales/zh-CN/common.json`(`orchestration.list.filterEmpty`)
- Modify: `src/i18n/locales/en/common.json`(同)
- Test: `src/components/agentre/orchestration/__tests__/run-list.test.tsx`

**Interfaces:**
- Consumes: `useOrchRunListStore((s) => s.runs)`(已有);`RunItemDTO.status`(`running`/`paused`/`done`/`pending`/`stopped`)。
- Produces: 筛选 chip 带 `data-testid="run-filter-all|running|done"` + `data-active`;渲染列表由过滤后的 `shownRuns` 决定;空匹配时渲染 `data-testid="run-filter-empty"`。

- [ ] **Step 1: 写失败测试**

在 `run-list.test.tsx` 的 `describe("has-runs 路径 — 新结构", ...)` 内追加(`twoRuns` = id10 running + id11 paused,已在该 describe 顶部定义):

```tsx
it("点击「运行中」筛选后只显示 running 的 Run", () => {
  useOrchRunListStore.setState({ runs: twoRuns });
  render(<RunList onSelect={vi.fn()} />);
  fireEvent.click(screen.getByTestId("run-filter-running"));
  expect(screen.getByTestId("run-row-10")).toBeInTheDocument();
  expect(screen.queryByTestId("run-row-11")).toBeNull();
});

it("点击「已完成」筛选且无匹配时显示空匹配提示", () => {
  useOrchRunListStore.setState({ runs: twoRuns });
  render(<RunList onSelect={vi.fn()} />);
  fireEvent.click(screen.getByTestId("run-filter-done"));
  expect(screen.queryByTestId("run-row-10")).toBeNull();
  expect(screen.queryByTestId("run-row-11")).toBeNull();
  expect(screen.getByTestId("run-filter-empty")).toBeInTheDocument();
});

it("默认筛选 all,两个 Run 都显示", () => {
  useOrchRunListStore.setState({ runs: twoRuns });
  render(<RunList onSelect={vi.fn()} />);
  expect(screen.getByTestId("run-row-10")).toBeInTheDocument();
  expect(screen.getByTestId("run-row-11")).toBeInTheDocument();
});
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd frontend && pnpm test -- run-list.test.tsx`
Expected: FAIL — 找不到 `run-filter-running`(现在筛选 chip 是无 testid 的 `<span>`)。

- [ ] **Step 3: 加 i18n key(两个 locale)**

`zh-CN/common.json` 的 `orchestration.list` 里加:

```json
"filterEmpty": "没有匹配的 Run",
```

`en/common.json` 的 `orchestration.list` 里加:

```json
"filterEmpty": "No matching runs",
```

- [ ] **Step 4: 实现筛选状态 + chip 按钮 + 过滤列表**

在 `run-list.tsx` 的 `RunList` 组件里,`const [dialogOpen, setDialogOpen] = React.useState(false);` 下面加:

```tsx
const [filter, setFilter] = React.useState<"all" | "running" | "done">("all");

const shownRuns = React.useMemo(
  () =>
    runs.filter((r) =>
      filter === "all"
        ? true
        : filter === "running"
          ? r.status === "running"
          : r.status === "done",
    ),
  [runs, filter],
);
```

把原来的筛选 chip 区块(`data-testid="run-filter-chips"` 那个 `<div>` 里三个 `<span>`)整体替换为:

```tsx
<div
  data-testid="run-filter-chips"
  className="flex items-center gap-6 pb-[10px] pl-3.5 pr-3.5"
>
  {(
    [
      { key: "all", labelKey: "orchestration.list.filterAll" },
      { key: "running", labelKey: "orchestration.list.filterRunning" },
      { key: "done", labelKey: "orchestration.list.filterDone" },
    ] as const
  ).map(({ key, labelKey }) => {
    const active = filter === key;
    return (
      <button
        key={key}
        type="button"
        data-testid={`run-filter-${key}`}
        data-active={active ? "true" : "false"}
        onClick={() => setFilter(key)}
        className={cn(
          "rounded-full px-2 py-[3px] text-[11px] transition-colors",
          active
            ? "border border-border bg-secondary font-semibold text-foreground"
            : "text-muted-foreground hover:text-foreground",
        )}
      >
        {t(labelKey)}
      </button>
    );
  })}
</div>
```

把渲染列表的 `{runs.map((run) => {` 改成 `{shownRuns.map((run) => {`,并在 `</ul>` 之后加空匹配提示:

```tsx
{shownRuns.length === 0 ? (
  <p
    data-testid="run-filter-empty"
    className="px-3.5 py-6 text-center text-xs text-muted-foreground"
  >
    {t("orchestration.list.filterEmpty")}
  </p>
) : null}
```

- [ ] **Step 5: 运行,确认通过**

Run: `cd frontend && pnpm test -- run-list.test.tsx`
Expected: PASS(新 3 个 + 原有全部)。

- [ ] **Step 6: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/orchestration/run-list.tsx \
  frontend/src/components/agentre/orchestration/__tests__/run-list.test.tsx \
  frontend/src/i18n/locales/zh-CN/common.json \
  frontend/src/i18n/locales/en/common.json \
  -m "✨ orchestration: 侧栏筛选 chip 接线(全部/运行中/已完成)"
```

---

## Task 2: 流程库两个可见入口(侧栏底部固定 + 新建 Run 上下文链接)

**Files:**
- Modify: `src/components/agentre/orchestration/orchestration-page.tsx:18-42`(侧栏底部固定入口)
- Modify: `src/components/agentre/orchestration/run-new-dialog.tsx:276-281`(「管理流程库→」链接)
- Modify: `src/i18n/locales/{zh-CN,en}/common.json`(`orchestration.list.libraryEntry` + `orchestration.new.flowManage`)
- Test: `src/components/agentre/orchestration/__tests__/orchestration-page.test.tsx`、`__tests__/run-new-dialog.test.tsx`

**Interfaces:**
- Consumes: `useWorkflowManagerStore`(`{ open, openBrowse() }`,来自 `@/stores/workflow-manager-store`)。
- Produces: 侧栏底部 `data-testid="run-library-entry"`;新建 Run 弹窗 library 模式下 `data-testid="run-flow-manage"`;两者点击都置 `useWorkflowManagerStore.getState().open === true`。

- [ ] **Step 1: 写失败测试(侧栏底部入口)**

`orchestration-page.test.tsx` 顶部 import 区加:

```tsx
import { useWorkflowManagerStore } from "../../../../stores/workflow-manager-store";
```

`beforeEach` 里(`useOrchRunListStore.setState({ runs: [] });` 下一行)加:

```tsx
useWorkflowManagerStore.setState({ open: false, intent: "browse" });
```

`describe("OrchestrationPage", ...)` 里追加:

```tsx
it("侧栏底部「流程库」入口点击打开流程库弹窗", () => {
  renderAt("/orchestration");
  fireEvent.click(screen.getByTestId("run-library-entry"));
  expect(useWorkflowManagerStore.getState().open).toBe(true);
});
```

并把测试文件第 1 行 import 补上 `fireEvent`:

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd frontend && pnpm test -- orchestration-page.test.tsx`
Expected: FAIL — 找不到 `run-library-entry`。

- [ ] **Step 3: 加 i18n key(两个 locale)**

`zh-CN/common.json` → `orchestration.list` 加 `"libraryEntry": "流程库",`;`orchestration.new` 加 `"flowManage": "管理流程库",`。
`en/common.json` → `orchestration.list` 加 `"libraryEntry": "Flow Library",`;`orchestration.new` 加 `"flowManage": "Manage library",`。

- [ ] **Step 4: 实现侧栏底部固定入口**

`orchestration-page.tsx` 顶部 import 加:

```tsx
import { LibraryBig } from "lucide-react";
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";
```

把 `<aside>...</aside>` 整块替换为(把可滚动的 `RunList` 包进 `flex-1 overflow-y-auto`,底部固定「流程库」按钮):

```tsx
<aside className="flex w-80 shrink-0 flex-col border-r border-border bg-sidebar">
  <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
    <RunList
      activeRunId={runId ?? undefined}
      onSelect={(id) => navigate(`/orchestration/${id}`)}
    />
  </div>
  <button
    type="button"
    data-testid="run-library-entry"
    onClick={() => useWorkflowManagerStore.getState().openBrowse()}
    className="flex shrink-0 items-center gap-2 border-t border-border px-3.5 py-2.5 text-left text-[12.5px] text-muted-foreground transition-colors hover:text-foreground"
  >
    <LibraryBig className="h-4 w-4" aria-hidden="true" />
    <span>{t("orchestration.list.libraryEntry")}</span>
  </button>
</aside>
```

(注:原 `<aside>` 有 `overflow-y-auto`,现在移到内层 `<div>`;`<aside>` 自身改为不滚动的 flex 列容器,让底部按钮固定。)

- [ ] **Step 5: 运行,确认通过**

Run: `cd frontend && pnpm test -- orchestration-page.test.tsx`
Expected: PASS。

- [ ] **Step 6: 写失败测试(新建 Run 上下文链接)**

`run-new-dialog.test.tsx` 顶部 import 加:

```tsx
import { useWorkflowManagerStore } from "../../../../stores/workflow-manager-store";
```

`beforeEach` 里加:

```tsx
useWorkflowManagerStore.setState({ open: false, intent: "browse" });
```

在 `describe("flowMode 三态按钮组", ...)` 内追加:

```tsx
it("library 模式下「管理流程库」链接点击打开流程库弹窗", async () => {
  renderDialog();
  await screen.findByTestId("run-goal");
  screen.getByTestId("run-flow-mode-library").click();
  fireEvent.click(await screen.findByTestId("run-flow-manage"));
  expect(useWorkflowManagerStore.getState().open).toBe(true);
});
```

- [ ] **Step 7: 运行,确认失败**

Run: `cd frontend && pnpm test -- run-new-dialog.test.tsx -t "管理流程库"`
Expected: FAIL — 找不到 `run-flow-manage`。

- [ ] **Step 8: 实现「管理流程库→」链接**

`run-new-dialog.tsx` 顶部 import 加:

```tsx
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";
```

把 library 模式区块的标题行(现在是单个 `<span className="font-medium text-foreground">{t("orchestration.new.flowSelect")}</span>`)替换为:

```tsx
<span className="flex items-center gap-2">
  <span className="font-medium text-foreground">
    {t("orchestration.new.flowSelect")}
  </span>
  <button
    type="button"
    data-testid="run-flow-manage"
    onClick={() => useWorkflowManagerStore.getState().openBrowse()}
    className="ml-auto text-2xs text-primary-text hover:underline"
  >
    {t("orchestration.new.flowManage")} →
  </button>
</span>
```

- [ ] **Step 9: 运行,确认通过**

Run: `cd frontend && pnpm test -- run-new-dialog.test.tsx`
Expected: PASS。

- [ ] **Step 10: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/orchestration/orchestration-page.tsx \
  frontend/src/components/agentre/orchestration/run-new-dialog.tsx \
  frontend/src/components/agentre/orchestration/__tests__/orchestration-page.test.tsx \
  frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx \
  frontend/src/i18n/locales/zh-CN/common.json \
  frontend/src/i18n/locales/en/common.json \
  -m "✨ orchestration: 补流程库可见入口(侧栏底部固定 + 新建 Run 上下文链接)"
```

---

## Task 3: 新建 Run:「起始流程」→「编排流程」文案 + 流程库单选列表 → 下拉框

**Files:**
- Modify: `src/components/agentre/orchestration/run-new-dialog.tsx:276-328`(picker 列表 → `Select`)
- Modify: `src/i18n/locales/{zh-CN,en}/common.json`(`orchestration.new.flow` 改值)
- Test: `src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`(重写两条 picker 测试 + 加文案测试)

**Interfaces:**
- Consumes: 既有 `workflows: WorkflowOption[]`(`{ id, name, tags, outline }`)、`flowId` state、`setFlowId`、shadcn `Select`(已 import)。
- Produces: 下拉 trigger `data-testid="run-flow-select"`;每个流程一个 `SelectItem`(name + tag chip);选中流程的步骤面包屑 `data-testid="run-flow-outline"`;`flowId` 进 `RunCreate({ flowId })`。

- [ ] **Step 1: 改测试(文案 + 下拉)**

先改文案测试。`run-new-dialog.test.tsx` 的 `describe("RunNewDialog", ...)` 顶部加一条:

```tsx
it("起始流程 label 文案为「编排流程」(en: Orchestration flow)", async () => {
  renderDialog();
  expect(await screen.findByText("Orchestration flow")).toBeInTheDocument();
});
```

然后**删除**原有的两条 picker 列表测试(`"点击 library 按钮切换 flowMode 并显示流程库 picker(多标签全显)"` 与 `"点击流程行后选中该流程(单选)"`,即 `run-flow-pick-*` 那两条),替换为下拉版本:

```tsx
it("切到 library 模式后下拉列出流程(名称 + 标签)", async () => {
  const user = userEvent.setup({ pointerEventsCheck: 0 });
  appMocks.WorkflowList.mockResolvedValue({
    items: [
      { id: 1, name: "标准功能开发流", tags: ["通用", "研发"], outline: ["需求拆解", "方案设计"] },
    ],
  });
  renderDialog();
  await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
  await user.click(screen.getByTestId("run-flow-mode-library"));
  await user.click(await screen.findByTestId("run-flow-select"));
  expect(await screen.findByRole("option", { name: /标准功能开发流/ })).toBeInTheDocument();
  expect(screen.getByText("通用")).toBeInTheDocument();
  expect(screen.getByText("研发")).toBeInTheDocument();
});

it("从下拉选中流程后, RunCreate 带上该 flowId", async () => {
  const user = userEvent.setup({ pointerEventsCheck: 0 });
  appMocks.WorkflowList.mockResolvedValue({
    items: [
      { id: 1, name: "流程A", tags: [], outline: [] },
      { id: 2, name: "流程B", tags: [], outline: [] },
    ],
  });
  renderDialog();
  await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
  await user.click(screen.getByTestId("run-flow-mode-library"));
  await user.click(await screen.findByTestId("run-flow-select"));
  await user.click(await screen.findByRole("option", { name: /流程B/ }));
  fireEvent.change(screen.getByTestId("run-goal"), { target: { value: "测试目标" } });
  await user.click(screen.getByTestId("run-leader"));
  await user.click(await screen.findByRole("option", { name: "架构师" }));
  await user.click(screen.getByTestId("run-create"));
  await waitFor(() =>
    expect(appMocks.RunCreate).toHaveBeenCalledWith(
      expect.objectContaining({ flowId: 2 }),
    ),
  );
});
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd frontend && pnpm test -- run-new-dialog.test.tsx`
Expected: FAIL — 文案测试找不到 "Orchestration flow";下拉测试找不到 `run-flow-select`。

- [ ] **Step 3: 改 i18n 文案(两个 locale)**

`zh-CN/common.json` → `orchestration.new.flow`:`"起始流程"` 改为 `"编排流程"`。
`en/common.json` → `orchestration.new.flow`:`"Starting flow"` 改为 `"Orchestration flow"`。

- [ ] **Step 4: picker 列表换成 Select 下拉**

`run-new-dialog.tsx` 里,在 `canSubmit` 附近加派生的选中流程:

```tsx
const selectedWorkflow = workflows.find((w) => w.id === flowId);
```

把 `{flowMode === "library" ? ( ... ) : null}` 整块(即 `run-flow-pick-*` 那段单选列表)替换为:

```tsx
{flowMode === "library" ? (
  <div className="flex flex-col gap-1.5 text-xs">
    <span className="flex items-center gap-2">
      <span className="font-medium text-foreground">
        {t("orchestration.new.flowSelect")}
      </span>
      <button
        type="button"
        data-testid="run-flow-manage"
        onClick={() => useWorkflowManagerStore.getState().openBrowse()}
        className="ml-auto text-2xs text-primary-text hover:underline"
      >
        {t("orchestration.new.flowManage")} →
      </button>
    </span>
    <Select
      value={flowId ? String(flowId) : ""}
      onValueChange={(v) => setFlowId(Number(v))}
    >
      <SelectTrigger
        data-testid="run-flow-select"
        aria-label={t("orchestration.new.flowSelect")}
        className="h-9 text-xs"
      >
        <SelectValue placeholder={t("orchestration.new.flowSelectPlaceholder")} />
      </SelectTrigger>
      <SelectContent>
        {workflows.map((w) => (
          <SelectItem key={w.id} value={String(w.id)}>
            <span className="flex items-center gap-2">
              <span>{w.name}</span>
              {w.tags.map((tag) => (
                <span
                  key={tag}
                  className="rounded bg-accent px-1 py-0.5 text-2xs text-muted-foreground"
                >
                  {tag}
                </span>
              ))}
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
    {selectedWorkflow && selectedWorkflow.outline.length > 0 ? (
      <span
        data-testid="run-flow-outline"
        className="flex flex-wrap items-center gap-1"
      >
        {selectedWorkflow.outline.map((step, i) => (
          <React.Fragment key={`${step}-${i}`}>
            {i > 0 ? (
              <span className="text-2xs text-subtle-foreground">›</span>
            ) : null}
            <span className="rounded border border-border bg-card px-1.5 py-0.5 text-2xs text-muted-foreground">
              {step}
            </span>
          </React.Fragment>
        ))}
      </span>
    ) : null}
  </div>
) : null}
```

> 注:此块把 Task 2 的「管理流程库→」链接一并包含了(标题行右侧)。若 Task 2 已单独加过标题行,这里以本块为准整体替换,不要重复两个 `run-flow-manage`。`useWorkflowManagerStore` import 已在 Task 2 加过。

- [ ] **Step 5: 运行,确认通过**

Run: `cd frontend && pnpm test -- run-new-dialog.test.tsx`
Expected: PASS(文案 + 两条下拉 + 原有其余)。

- [ ] **Step 6: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/orchestration/run-new-dialog.tsx \
  frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx \
  frontend/src/i18n/locales/zh-CN/common.json \
  frontend/src/i18n/locales/en/common.json \
  -m "✨ orchestration: 流程选择改下拉框 + 起始流程→编排流程 文案"
```

---

## Task 4: 总览数据纯函数(统计 / 进行中 / 最近完成 / 进度 / 时长)

**Files:**
- Create: `src/components/agentre/orchestration/overview-data.ts`
- Test: `src/components/agentre/orchestration/__tests__/overview-data.test.ts`

**Interfaces:**
- Consumes: `app.RunItemDTO`(`status`/`createtime`/`updatetime`,毫秒)、`app.TaskDTO`(`status`)。
- Produces:
  - `computeRunStats(runs, now): { running, waiting, doneThisWeek, avgDurationMs }`
  - `inProgressRuns(runs): RunItemDTO[]`(status==="running",按 updatetime 倒序)
  - `recentDoneRuns(runs, limit=5): RunItemDTO[]`(status==="done",按 updatetime 倒序,取前 limit)
  - `runProgress(tasks): { done, total }`(done=status==="done" 计数,total=tasks.length —— 与 `task-board.tsx` 一致)
  - `formatDuration(ms): string`(`38m`/`2h`/`1d`;ms<=0 → `""`)

- [ ] **Step 1: 写失败测试**

Create `src/components/agentre/orchestration/__tests__/overview-data.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import {
  computeRunStats,
  formatDuration,
  inProgressRuns,
  recentDoneRuns,
  runProgress,
} from "../overview-data";

const NOW = 1_700_000_000_000;
const HOUR = 3_600_000;
const DAY = 24 * HOUR;

function run(over: Partial<{ id: number; status: string; createtime: number; updatetime: number }>) {
  return {
    id: 1, goal: "g", leaderAgentId: 0, status: "running", projectId: 0,
    flowId: 0, flowContent: "", rootTaskId: 0, createtime: NOW, updatetime: NOW,
    ...over,
  } as never;
}

describe("computeRunStats", () => {
  it("统计 running / waiting(paused) / 本周完成 / 平均时长", () => {
    const runs = [
      run({ id: 1, status: "running" }),
      run({ id: 2, status: "paused" }),
      run({ id: 3, status: "done", createtime: NOW - 2 * HOUR, updatetime: NOW - 1 * HOUR }),
      run({ id: 4, status: "done", createtime: NOW - 4 * HOUR, updatetime: NOW - 1 * HOUR }),
      // 8 天前完成 → 不计入本周完成
      run({ id: 5, status: "done", createtime: NOW - 9 * DAY, updatetime: NOW - 8 * DAY }),
    ];
    const s = computeRunStats(runs, NOW);
    expect(s.running).toBe(1);
    expect(s.waiting).toBe(1);
    expect(s.doneThisWeek).toBe(2);
    // 三个 done 的时长: 1h, 3h, 1d → 平均 (1+3+24)/3 h = 28/3 h
    expect(s.avgDurationMs).toBe(Math.round((HOUR + 3 * HOUR + DAY) / 3));
  });

  it("无 done 时平均时长为 0", () => {
    expect(computeRunStats([run({ status: "running" })], NOW).avgDurationMs).toBe(0);
  });
});

describe("inProgressRuns / recentDoneRuns", () => {
  it("inProgressRuns 只取 running 并按 updatetime 倒序", () => {
    const runs = [
      run({ id: 1, status: "running", updatetime: NOW - 3 * HOUR }),
      run({ id: 2, status: "done", updatetime: NOW }),
      run({ id: 3, status: "running", updatetime: NOW - 1 * HOUR }),
    ];
    expect(inProgressRuns(runs).map((r) => r.id)).toEqual([3, 1]);
  });

  it("recentDoneRuns 只取 done、倒序、限量", () => {
    const runs = [
      run({ id: 1, status: "done", updatetime: NOW - 3 * HOUR }),
      run({ id: 2, status: "done", updatetime: NOW - 1 * HOUR }),
      run({ id: 3, status: "running", updatetime: NOW }),
    ];
    expect(recentDoneRuns(runs, 1).map((r) => r.id)).toEqual([2]);
  });
});

describe("runProgress", () => {
  it("done = status==='done' 计数, total = 长度", () => {
    const tasks = [
      { status: "done" }, { status: "running" }, { status: "done" },
    ] as never[];
    expect(runProgress(tasks)).toEqual({ done: 2, total: 3 });
  });
  it("undefined → 0/0", () => {
    expect(runProgress(undefined)).toEqual({ done: 0, total: 0 });
  });
});

describe("formatDuration", () => {
  it("分/时/天 分段", () => {
    expect(formatDuration(38 * 60_000)).toBe("38m");
    expect(formatDuration(2 * HOUR)).toBe("2h");
    expect(formatDuration(1 * DAY)).toBe("1d");
    expect(formatDuration(0)).toBe("");
  });
});
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd frontend && pnpm test -- overview-data.test.ts`
Expected: FAIL — `Cannot find module "../overview-data"`。

- [ ] **Step 3: 实现纯函数**

Create `src/components/agentre/orchestration/overview-data.ts`:

```ts
import type { app } from "../../../../wailsjs/go/models";

export type RunStats = {
  running: number;
  waiting: number;
  doneThisWeek: number;
  avgDurationMs: number;
};

const WEEK_MS = 7 * 24 * 60 * 60 * 1000;

// 从 Run 列表客户端 derive 概要统计。createtime/updatetime 为毫秒(UnixMilli)。
export function computeRunStats(runs: app.RunItemDTO[], now: number): RunStats {
  let running = 0;
  let waiting = 0;
  let doneThisWeek = 0;
  let durSum = 0;
  let durCount = 0;
  for (const r of runs) {
    if (r.status === "running") running++;
    else if (r.status === "paused") waiting++;
    if (r.status === "done") {
      if (now - r.updatetime <= WEEK_MS) doneThisWeek++;
      const dur = r.updatetime - r.createtime;
      if (dur > 0) {
        durSum += dur;
        durCount++;
      }
    }
  }
  return {
    running,
    waiting,
    doneThisWeek,
    avgDurationMs: durCount > 0 ? Math.round(durSum / durCount) : 0,
  };
}

// 进行中(running)的 Run,按 updatetime 倒序。
export function inProgressRuns(runs: app.RunItemDTO[]): app.RunItemDTO[] {
  return runs
    .filter((r) => r.status === "running")
    .slice()
    .sort((a, b) => b.updatetime - a.updatetime);
}

// 最近完成(done)的 Run,按 updatetime 倒序,取前 limit。
export function recentDoneRuns(
  runs: app.RunItemDTO[],
  limit = 5,
): app.RunItemDTO[] {
  return runs
    .filter((r) => r.status === "done")
    .slice()
    .sort((a, b) => b.updatetime - a.updatetime)
    .slice(0, limit);
}

// Run 进度:done/total。镜像 task-board.tsx(done=status==="done" 计数,total=tasks.length)。
export function runProgress(tasks: app.TaskDTO[] | undefined): {
  done: number;
  total: number;
} {
  const t = tasks ?? [];
  return {
    done: t.filter((x) => x.status === "done").length,
    total: t.length,
  };
}

// 时长格式化:ms → "38m"/"2h"/"1d";ms<=0 → ""。
export function formatDuration(ms: number): string {
  if (ms <= 0) return "";
  const min = Math.floor(ms / 60000);
  if (min < 60) return `${min}m`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h`;
  const day = Math.floor(hr / 24);
  return `${day}d`;
}
```

- [ ] **Step 4: 运行,确认通过**

Run: `cd frontend && pnpm test -- overview-data.test.ts`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/orchestration/overview-data.ts \
  frontend/src/components/agentre/orchestration/__tests__/overview-data.test.ts \
  -m "✨ orchestration: 总览数据纯函数(统计/进行中/最近完成/进度/时长)"
```

---

## Task 5: 总览页组件 + 接入 orchestration-page

**Files:**
- Create: `src/components/agentre/orchestration/orchestration-overview.tsx`
- Create: `src/components/agentre/orchestration/__tests__/orchestration-overview.test.tsx`
- Modify: `src/components/agentre/orchestration/orchestration-page.tsx`(无选中 Run 时渲染 `<OrchestrationOverview />`)
- Modify: `src/components/agentre/orchestration/__tests__/orchestration-page.test.tsx`(把 `orchestration-empty-main` 断言改为 `orchestration-overview`)
- Modify: `src/i18n/locales/{zh-CN,en}/common.json`(`orchestration.overview.*`)

**Interfaces:**
- Consumes: `overview-data.ts`(T4 全部导出)、`useOrchRunListStore((s)=>s.runs)`+`.load()`、`useOrchRunStore((s)=>s.details)`+`.loadRun(id)`、`ListChatAgents`、`useNavigate`、`relativeTime`、`tokenToCssColor`/`firstLetter`(`../session-avatar`)。
- Produces: 组件 `OrchestrationOverview`,根 `data-testid="orchestration-overview"`;统计卡 `overview-stat-{running,waiting,doneWeek,avgDuration}`;进行中卡 `overview-inprogress-card-{id}` + 进度 `overview-inprogress-progress-{id}`;最近完成行 `overview-recent-row-{id}`。

- [ ] **Step 1: 加 i18n key(两个 locale)**

`zh-CN/common.json` → `orchestration` 下加 `overview` 块:

```json
"overview": {
  "title": "编排",
  "subtitle": "多 Agent 协作编排总览",
  "statRunning": "运行中",
  "statWaiting": "等待",
  "statDoneThisWeek": "本周完成",
  "statAvgDuration": "平均时长",
  "noDuration": "—",
  "inProgress": "进行中",
  "inProgressEmpty": "暂无进行中的 Run",
  "recentDone": "最近完成",
  "recentDoneEmpty": "暂无已完成的 Run",
  "progress": "{{done}}/{{total}}"
}
```

`en/common.json` → `orchestration` 下加:

```json
"overview": {
  "title": "Orchestration",
  "subtitle": "Multi-agent orchestration overview",
  "statRunning": "Running",
  "statWaiting": "Waiting",
  "statDoneThisWeek": "Done this week",
  "statAvgDuration": "Avg duration",
  "noDuration": "—",
  "inProgress": "In progress",
  "inProgressEmpty": "No runs in progress",
  "recentDone": "Recently completed",
  "recentDoneEmpty": "No completed runs",
  "progress": "{{done}}/{{total}}"
}
```

- [ ] **Step 2: 写失败测试**

Create `src/components/agentre/orchestration/__tests__/orchestration-overview.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  RunList: vi.fn(),
  RunLoad: vi.fn(),
  ListChatAgents: vi.fn(),
}));
vi.mock("../../../../../wailsjs/go/app/App", () => appMocks);

const mockNavigate = vi.fn();
vi.mock("react-router-dom", () => ({ useNavigate: () => mockNavigate }));

import { useOrchRunListStore } from "../../../../stores/orch-run-list-store";
import { useOrchRunStore } from "../../../../stores/orch-run-store";
import { OrchestrationOverview } from "../orchestration-overview";

const now = Date.now();
const runningRun = {
  id: 1, goal: "支付重构", status: "running", leaderAgentId: 2, projectId: 0,
  flowId: 0, flowContent: "", rootTaskId: 0, createtime: now - 3_600_000, updatetime: now - 60_000,
};
const doneRun = {
  id: 2, goal: "登录页", status: "done", leaderAgentId: 2, projectId: 0,
  flowId: 0, flowContent: "", rootTaskId: 0, createtime: now - 7_200_000, updatetime: now - 120_000,
};

beforeEach(() => {
  useOrchRunListStore.getState().__reset();
  useOrchRunStore.getState().__reset();
  vi.clearAllMocks();
  appMocks.RunList.mockResolvedValue([runningRun, doneRun]);
  appMocks.ListChatAgents.mockResolvedValue({
    agents: [{ id: 2, name: "架构师", avatarColor: "agent-1", defaultPermissionMode: "default" }],
  });
  appMocks.RunLoad.mockResolvedValue({
    run: runningRun,
    tasks: [
      { id: 10, runId: 1, agentId: 2, sessionId: 0, parentTaskId: 0, kind: "orch", status: "done", brief: "", result: "", callSeq: 0, refs: "", createtime: now, updatetime: now },
      { id: 11, runId: 1, agentId: 2, sessionId: 0, parentTaskId: 0, kind: "orch", status: "running", brief: "", result: "", callSeq: 1, refs: "", createtime: now, updatetime: now },
    ],
  });
});

describe("OrchestrationOverview", () => {
  it("统计卡展示 running / 本周完成 计数", async () => {
    render(<OrchestrationOverview />);
    expect(await screen.findByTestId("overview-stat-running")).toHaveTextContent("1");
    expect(screen.getByTestId("overview-stat-doneWeek")).toHaveTextContent("1");
  });

  it("进行中卡片展示进度 done/total(来自懒加载 detail)", async () => {
    render(<OrchestrationOverview />);
    expect(await screen.findByTestId("overview-inprogress-progress-1")).toHaveTextContent("1/2");
  });

  it("点击进行中卡片 navigate 到该 Run", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<OrchestrationOverview />);
    await user.click(await screen.findByTestId("overview-inprogress-card-1"));
    expect(mockNavigate).toHaveBeenCalledWith("/orchestration/1");
  });

  it("最近完成列出 done 的 Run", async () => {
    render(<OrchestrationOverview />);
    expect(await screen.findByTestId("overview-recent-row-2")).toBeInTheDocument();
  });

  it("runs=[] 时渲染空态(仍带 overview testid)", async () => {
    appMocks.RunList.mockResolvedValue([]);
    render(<OrchestrationOverview />);
    expect(await screen.findByTestId("orchestration-overview")).toBeInTheDocument();
    expect(screen.queryByTestId("overview-stat-running")).toBeNull();
  });
});
```

- [ ] **Step 3: 运行,确认失败**

Run: `cd frontend && pnpm test -- orchestration-overview.test.tsx`
Expected: FAIL — `Cannot find module "../orchestration-overview"`。

- [ ] **Step 4: 实现 OrchestrationOverview**

Create `src/components/agentre/orchestration/orchestration-overview.tsx`:

```tsx
import * as React from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Waypoints } from "lucide-react";

import { relativeTime } from "@/lib/relative-time";
import { useOrchRunListStore } from "../../../stores/orch-run-list-store";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { ListChatAgents } from "../../../../wailsjs/go/app/App";
import { firstLetter, tokenToCssColor } from "../session-avatar";
import {
  computeRunStats,
  formatDuration,
  inProgressRuns,
  recentDoneRuns,
  runProgress,
} from "./overview-data";

type AgentMeta = { id: number; name: string; avatarColor: string };

export function OrchestrationOverview() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const runs = useOrchRunListStore((s) => s.runs);
  const details = useOrchRunStore((s) => s.details);
  const loadRun = useOrchRunStore((s) => s.loadRun);
  const [agents, setAgents] = React.useState<AgentMeta[]>([]);

  React.useEffect(() => {
    void useOrchRunListStore.getState().load();
  }, []);

  React.useEffect(() => {
    ListChatAgents()
      .then((resp) =>
        setAgents(
          (resp?.agents ?? []).map(
            (a: { id: number; name: string; avatarColor: string }) => ({
              id: a.id,
              name: a.name,
              avatarColor: a.avatarColor,
            }),
          ),
        ),
      )
      .catch(() => setAgents([]));
  }, []);

  const stats = React.useMemo(() => computeRunStats(runs, Date.now()), [runs]);
  const inProgress = React.useMemo(() => inProgressRuns(runs), [runs]);
  const recent = React.useMemo(() => recentDoneRuns(runs), [runs]);

  // 懒加载少量 running Run 的详情以显示进度(顺带暖 detail 缓存,点进去更快)。
  React.useEffect(() => {
    for (const r of inProgress) {
      if (!details.has(r.id)) void loadRun(r.id);
    }
  }, [inProgress, details, loadRun]);

  const leaderOf = React.useCallback(
    (id: number) => agents.find((a) => a.id === id),
    [agents],
  );

  if (runs.length === 0) {
    return (
      <div
        data-testid="orchestration-overview"
        className="flex flex-1 items-center justify-center p-8 text-center text-sm text-muted-foreground"
      >
        {t("orchestration.onboarding.cta")}
      </div>
    );
  }

  return (
    <div
      data-testid="orchestration-overview"
      className="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto p-6"
    >
      <h1 className="text-lg font-semibold text-foreground">
        {t("orchestration.overview.title")}
      </h1>
      <p className="mb-4 text-xs text-muted-foreground">
        {t("orchestration.overview.subtitle")}
      </p>

      <div className="mb-6 grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatCard testid="overview-stat-running" label={t("orchestration.overview.statRunning")} value={String(stats.running)} />
        <StatCard testid="overview-stat-waiting" label={t("orchestration.overview.statWaiting")} value={String(stats.waiting)} />
        <StatCard testid="overview-stat-doneWeek" label={t("orchestration.overview.statDoneThisWeek")} value={String(stats.doneThisWeek)} />
        <StatCard testid="overview-stat-avgDuration" label={t("orchestration.overview.statAvgDuration")} value={formatDuration(stats.avgDurationMs) || t("orchestration.overview.noDuration")} />
      </div>

      <section className="mb-6">
        <h2 className="mb-2 text-sm font-semibold text-foreground">
          {t("orchestration.overview.inProgress")}
        </h2>
        {inProgress.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            {t("orchestration.overview.inProgressEmpty")}
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            {inProgress.map((r) => {
              const prog = runProgress(details.get(r.id)?.tasks);
              const pct = prog.total > 0 ? Math.round((prog.done / prog.total) * 100) : 0;
              const leader = leaderOf(r.leaderAgentId);
              return (
                <button
                  key={r.id}
                  type="button"
                  data-testid={`overview-inprogress-card-${r.id}`}
                  onClick={() => navigate(`/orchestration/${r.id}`)}
                  className="flex flex-col gap-2 rounded-lg border border-border bg-card px-3.5 py-3 text-left transition-colors hover:bg-accent/40"
                >
                  <span className="flex items-center gap-2">
                    <span
                      aria-hidden="true"
                      className="flex size-5 shrink-0 items-center justify-center rounded-full text-2xs font-semibold text-white"
                      style={{ backgroundColor: tokenToCssColor(leader?.avatarColor ?? "") ?? "#94a3b8" }}
                    >
                      {firstLetter(leader?.name ?? "?")}
                    </span>
                    <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-foreground">
                      {r.goal}
                    </span>
                    <span className="shrink-0 font-mono text-2xs text-muted-foreground">
                      {relativeTime(r.updatetime)}
                    </span>
                  </span>
                  <span className="flex items-center gap-2">
                    <span className="h-1.5 flex-1 overflow-hidden rounded-full bg-secondary">
                      <span
                        className="block h-full rounded-full bg-status-running"
                        style={{ width: `${pct}%` }}
                      />
                    </span>
                    <span
                      data-testid={`overview-inprogress-progress-${r.id}`}
                      className="shrink-0 font-mono text-2xs text-muted-foreground"
                    >
                      {t("orchestration.overview.progress", { done: prog.done, total: prog.total })}
                    </span>
                  </span>
                </button>
              );
            })}
          </div>
        )}
      </section>

      <section>
        <h2 className="mb-2 text-sm font-semibold text-foreground">
          {t("orchestration.overview.recentDone")}
        </h2>
        {recent.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            {t("orchestration.overview.recentDoneEmpty")}
          </p>
        ) : (
          <ul className="flex flex-col">
            {recent.map((r) => (
              <li key={r.id}>
                <button
                  type="button"
                  data-testid={`overview-recent-row-${r.id}`}
                  onClick={() => navigate(`/orchestration/${r.id}`)}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left hover:bg-accent/40"
                >
                  <Waypoints className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                  <span className="min-w-0 flex-1 truncate text-xs text-foreground">
                    {r.goal}
                  </span>
                  <span className="shrink-0 font-mono text-2xs text-muted-foreground">
                    {relativeTime(r.updatetime)}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

function StatCard({ testid, label, value }: { testid: string; label: string; value: string }) {
  return (
    <div
      data-testid={testid}
      className="flex flex-col gap-1 rounded-lg border border-border bg-card px-3.5 py-3"
    >
      <span className="text-2xs uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <span className="text-xl font-semibold text-foreground">{value}</span>
    </div>
  );
}
```

- [ ] **Step 5: 运行,确认通过**

Run: `cd frontend && pnpm test -- orchestration-overview.test.tsx`
Expected: PASS(5 条)。

- [ ] **Step 6: 接入 orchestration-page + 改其测试**

`orchestration-page.tsx` 顶部 import 加:

```tsx
import { OrchestrationOverview } from "./orchestration-overview";
```

把主区无选中 Run 的分支(现在的 `data-testid="orchestration-empty-main"` 那个 `<div>...{t("orchestration.onboarding.cta")}...</div>`)替换为:

```tsx
<OrchestrationOverview />
```

`orchestration-page.test.tsx`:把两处 `screen.getByTestId("orchestration-empty-main")` 改为 `screen.getByTestId("orchestration-overview")`(第 56、69 行)。这两个测试的 `beforeEach` 已 `runs: []`,总览渲染空态,`orchestration-overview` testid 仍在。

> 该测试文件的 wails mock 已含 `ListChatAgents`/`RunList`;总览空态不触发 `RunLoad`,无需补 mock。

- [ ] **Step 7: 运行,确认通过**

Run: `cd frontend && pnpm test -- orchestration-page.test.tsx orchestration-overview.test.tsx`
Expected: PASS。

- [ ] **Step 8: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/orchestration/orchestration-overview.tsx \
  frontend/src/components/agentre/orchestration/orchestration-page.tsx \
  frontend/src/components/agentre/orchestration/__tests__/orchestration-overview.test.tsx \
  frontend/src/components/agentre/orchestration/__tests__/orchestration-page.test.tsx \
  frontend/src/i18n/locales/zh-CN/common.json \
  frontend/src/i18n/locales/en/common.json \
  -m "✨ orchestration: 总览落地页(统计卡 + 进行中带进度条 + 最近完成)"
```

---

## 收尾:全量 gate(不是单测 focused)

per [[feedback_sdd_run_full_gates_at_finish]] —— focused 测试漏跨包/类型/格式问题,收尾必须跑全量并看**真 exit code**:

- [ ] **Lint(含 tsc/eslint,vitest 不查类型格式)**

Run: `cd frontend && pnpm lint`
Expected: 0 error。i18n `no-literal-string` 不报(所有可见文案已走 `t`)。

- [ ] **全量前端测试(含 i18n.test 的 locale 对齐校验)**

Run: `cd /Users/codfrm/Code/agentre/agentre && make test-frontend`
Expected: 全绿。特别确认 `src/__tests__/i18n.test.ts` 通过(证明新增 key 在 zh-CN/en 两侧对齐、且所有 `t("...")` 有对应 key)。

- [ ] **(可选)手动验证**:`make dev` 打开应用 → 左导航「编排」→ 无选中 Run 看到总览(统计卡/进行中带进度条/最近完成)→ 侧栏底部「流程库」入口打开弹窗 → 新建 Run 里「编排流程」文案 + 从流程库下拉选择 +「管理流程库→」链接 → 侧栏「运行中/已完成」筛选生效。

---

## Self-Review(plan 作者已核对)

- **Spec 覆盖**:用户 5 项诉求 → T1(筛选可点)、T5(总览页 + 入口)、T2(流程库入口)、T3(文案 编排流程 + 流程下拉)。progress bar(用户追加「要做进度条」)→ T4 `runProgress` + T5 懒加载 `RunLoad` 进度条。✓
- **占位扫描**:无 TBD/TODO,每步有完整代码/命令/期望。✓
- **类型一致**:`computeRunStats`/`inProgressRuns`/`recentDoneRuns`/`runProgress`/`formatDuration` 在 T4 定义、T5 消费,签名一致;testid 命名 T2/T3(`run-flow-manage`/`run-flow-select`)、T5(`overview-*`)前后一致。✓
- **依赖顺序**:T2 先加 `useWorkflowManagerStore` import 与标题行,T3 整体替换该块(含同一个 `run-flow-manage`,已注明「以本块为准、勿重复」)。T4 → T5 单向依赖。✓
- **风险点**:① orchestration-page `<aside>` 结构调整(overflow 内移)—— T2 已给完整替换块;② 删除 `run-flow-pick-*` 旧测试 —— T3 Step 1 明确列出要删的两条;③ `orchestration-empty-main` → `orchestration-overview` 断言迁移 —— T5 Step 6 指明行号与改法。
