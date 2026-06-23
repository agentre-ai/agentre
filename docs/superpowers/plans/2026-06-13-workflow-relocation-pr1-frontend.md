# 流程库重定位 PR1 · 前端管理弹窗实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把「流程库」从左侧 rail 一级槽位 + 整页路由 `/workflows`，重做成一个全局**管理弹窗**（列表 + 预览 + 内联编辑 + 内联删除确认），从命令面板与建群弹窗两个入口打开；删除 rail 项使其从 6 项降为 5 项。

**Architecture:** 新增一个 UI-only 的 zustand store（`useWorkflowManagerStore`）控制弹窗开关与进入意图；新增 `WorkflowManagerDialog`，复用既有 `useWorkflows` hook + `MarkdownText`，把原 `workflows-page` 的列表/预览折叠进弹窗，并把原独立的编辑/删除弹窗改为**右栏内联模式切换**（编辑器替换预览、删除就地确认，弹窗嵌套深度恒 ≤ 2）。编辑表单从 `workflow-edit-dialog.tsx` 抽成受控的 `WorkflowEditorForm`。命令面板加一个 action source 暴露「打开流程库 / 新建流程」两条命令；建群弹窗在「协作流程」label 行加「管理流程」link 并在弹窗关闭后刷新下拉。`App.tsx` 删除 nav 项/路由/面包屑并在根挂载弹窗。

**Tech Stack:** React 19 + TypeScript, Tailwind v4, shadcn `@/components/ui/*`, zustand, react-i18next, lucide-react, Vitest + Testing Library。

设计稿（锁定，仅 pencil MCP 看）：`~/Desktop/agentry.pen` 帧 `流程库管理弹窗 · 浏览预览`(`mM5u0`) / `· 内联编辑`(`ZboYf`) / `· 内联删除确认`(`m4HBy4`) / `入口① 建群弹窗`(`Ae4I5`) / `入口② 命令面板`(`AUa8B`)。Spec：`docs/superpowers/specs/2026-06-13-workflow-library-relocation-and-agent-tool-design.md`（Part A）。

---

## 背景与既有事实（实现者必读，避免踩坑）

### 既有代码（复用，不重写）

- **`frontend/src/hooks/use-workflows.ts`** 已提供全部数据逻辑，签名稳定：
  ```ts
  const { workflows, loading, error, reload, create, update, remove } = useWorkflows();
  // workflows: WorkflowItem[] = { id, name, content, groupCount, createtime, updatetime }[]
  // create(name, content): Promise<void>  —— 失败抛出（供表单内联展示）
  // update(id, name, content): Promise<void> —— 失败抛出
  // remove(id): Promise<void> —— 失败不抛，落到 hook 的 error
  // reload(): Promise<void>
  ```
  hook 内部已 mount 即 `reload()`，每次写操作后自动 `reload()`。
- **`frontend/src/components/agentre/markdown-text.tsx`** 的 `MarkdownText`（`<MarkdownText text={...} />`）渲染 SOP 正文。
- **shadcn `@/components/ui/dialog`** 导出 `Dialog`/`DialogContent`/`DialogTitle`/`DialogDescription`（命令面板 `command-palette.tsx` 用的就是它们 + `showCloseButton={false}` + 自定义内容）。本弹窗同款用法。
- **`@/components/ui/button`**、**`@/components/ui/input`**、**`@/components/ui/textarea`**、**`@/lib/utils` 的 `cn`**。

### 待移除/改造

- `frontend/src/App.tsx`
  - import `routeIcon`（line 20）、import `WorkflowsPage`（line 37）。
  - `navItems` 中 `/workflows` 项（line 94-98）。
  - `pageBreadcrumbKeys["/workflows"]`（line 118）。
  - 路由 `<Route path="/workflows" element={<WorkflowsPage />} />`（line 886）。
- `frontend/src/components/agentre/index.ts:28` `export { WorkflowsPage } ...`。
- `frontend/src/components/agentre/workflows/workflows-page.tsx`（删）+ `workflows-page.test.tsx`（删）。
- `frontend/src/components/agentre/workflows/workflow-edit-dialog.tsx`（表单抽出后删）+ `workflow-edit-dialog.test.tsx`（重定向到 `workflow-editor-form.test.tsx`）。
- `frontend/src/components/agentre/workflows/workflow-delete-dialog.tsx`（删；删除改内联确认）+ 若有 `workflow-delete-dialog.test.tsx`（删）。

> 唯一引用 `/workflows` 路由/`nav-workflows` 的提交代码是 `App.tsx`；提交的 e2e 套件无引用（仅 gitignore 的 `e2e/scratch/` 用过 `nav-workflows`，不计）。旧 lastPath=`/workflows` 由 `App.tsx` 现有 `<Route path="*" element={<Navigate to="/chat" replace />} />` 兜底，安全。

### 命令面板扩展点

- `frontend/src/components/agentre/command-palette/command-palette.tsx:57` 的 `SOURCES` 数组是加新源的唯一入口（注释明确支持「导航/动作」源）。
- 源契约 `CommandSource<T>`（`command-palette/types.ts`）：`{ id, heading, modes, activeFor?, useItems, getScore, renderItem, onSelect }`。`modes:["command"]` = `>` 前缀命令模式。`onSelect(item, ctx)` 的 `ctx` 有 `close()`/`navigate`/`openSession`/`openNewSession`/`pathname`，**没有**打开本弹窗的能力 —— 本源 `onSelect` 直接 `useWorkflowManagerStore.getState().openBrowse()/openCreate()` + `ctx.close()`（store 是全局 UI store，直接 getState 调用是允许的；ctx 模式只针对 router/session）。
- `getScore(query, item)` 返回 0=不命中、>0=排序权重；可用 `command-palette/score.ts` 的 `scoreItem`。`renderItem` 渲染单行。

### i18n（`src/__tests__/i18n.test.ts` 强约束）

- zh-CN 与 en `common.json` **key 完全对齐**（多/少一个都 fail）。
- 源码每个**静态** `t("a.b.c")` 的 key 必须在两个 locale 存在；**禁止**硬编码中文（含 JSX 文本与 `aria-label`/`title`/`placeholder` 等可见属性）。
- 现有 `workflows.*` 键（`title`/`subtitle`/`new`/`empty`/`groupCount`/`groupCount_one`/`updatedAt`/`edit`/`delete`/`preview.*`/`editor.*`/`deleteConfirm.*`）**复用**。
- 删 `nav.workflows`（zh:line 1416 区块内 / en 对应），因为 navItem 与面包屑都移除，无 `t("nav.workflows")` 残留。

### 测试与构建约束（先读再动手）

- **vitest 不需要真实 `wailsjs/`**：`frontend/vite.config.ts` 的 `test.alias` 把 `wailsjs/go/app/App` → `src/__tests__/mocks/wailsApp.ts`。但既有 `workflows-page.test.tsx` 用的是 **per-file `vi.mock("../../../../wailsjs/go/app/App", ...)`** 直接桩掉四个 binding（见该文件 line 10-15）。本计划的弹窗测试**沿用同款 per-file `vi.mock`**（最稳，避免依赖全局 alias 的导出齐全）。
- 跑前端测试：`cd frontend && pnpm test -- <file>`（**不要** `make test-frontend`，它会强制先 `generate`）。
- 需要 `tsc -b` / `eslint` / 全量校验时（涉及 `wailsjs/go/models` 类型）：在本 worktree 跑 `GOWORK=off make generate` 先生成 `wailsjs/`（gitignore 产物，不提交；`frontend/dist/.gitkeep` 占位已在）。见项目记忆 `project_agentre_worktree_build_gotchas`。
- **mock 规则**（项目记忆 `reference_frontend_wails_runtime_test_mock`）：`wailsjs/go/*` 用 per-file `vi.mock` 或全局 alias 均可；**不要**给它加全局 vite alias 之外的破坏；本 PR 不碰 `wailsjs/runtime/runtime`，故无需 runtime mock。判断「哪些测试要补」跑全量 vitest，不要只跑 focused。

### 文件结构（先看清边界）

**新增：**
- `frontend/src/stores/workflow-manager-store.ts` —— UI-only 开关 store（`open` + `intent` + `openBrowse/openCreate/close`）。
- `frontend/src/components/agentre/workflows/workflow-editor-form.tsx` —— 受控编辑表单（name + content textarea + 插入模板 link + error），browse 弹窗内联编辑态复用。
- `frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx` —— 管理弹窗（列表 + 预览 + 内联编辑/删除三态）。
- `frontend/src/components/agentre/command-palette/sources/workflow-actions-source.tsx` —— 命令面板「打开流程库 / 新建流程」action 源。

**修改：**
- `frontend/src/components/agentre/command-palette/command-palette.tsx` —— `SOURCES` 加 `workflowActionsSource`。
- `frontend/src/components/agentre/group-chat/group-new-dialog.tsx` —— 加「管理流程」link + 抽 `loadWorkflows` + 弹窗关闭刷新。
- `frontend/src/App.tsx` —— 删 nav/route/breadcrumb/import；根挂 `<WorkflowManagerDialog/>`。
- `frontend/src/components/agentre/index.ts` —— 删 `WorkflowsPage` 导出；加 `WorkflowManagerDialog`。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` —— 删 `nav.workflows`；加 `workflows.manager.*`、`workflows.actions.*`、`commandPalette.workflows.*`、`group.new.manageWorkflows`。

**删除：**
- `workflows-page.tsx` + `workflows-page.test.tsx`、`workflow-edit-dialog.tsx`、`workflow-delete-dialog.tsx`（及其 test）。

---

## Task 1: 管理弹窗开关 store

**Files:**
- Create: `frontend/src/stores/workflow-manager-store.ts`
- Test: `frontend/src/stores/workflow-manager-store.test.ts`

- [ ] **Step 1: 写失败测试**

```ts
// frontend/src/stores/workflow-manager-store.test.ts
import { beforeEach, describe, expect, it } from "vitest";

import { useWorkflowManagerStore } from "./workflow-manager-store";

describe("useWorkflowManagerStore", () => {
  beforeEach(() => {
    useWorkflowManagerStore.setState({ open: false, intent: "browse" });
  });

  it("openBrowse 打开且 intent=browse", () => {
    useWorkflowManagerStore.getState().openBrowse();
    expect(useWorkflowManagerStore.getState().open).toBe(true);
    expect(useWorkflowManagerStore.getState().intent).toBe("browse");
  });

  it("openCreate 打开且 intent=create", () => {
    useWorkflowManagerStore.getState().openCreate();
    expect(useWorkflowManagerStore.getState().open).toBe(true);
    expect(useWorkflowManagerStore.getState().intent).toBe("create");
  });

  it("close 关闭并把 intent 复位为 browse", () => {
    useWorkflowManagerStore.getState().openCreate();
    useWorkflowManagerStore.getState().close();
    expect(useWorkflowManagerStore.getState().open).toBe(false);
    expect(useWorkflowManagerStore.getState().intent).toBe("browse");
  });
});
```

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/stores/workflow-manager-store.test.ts`
Expected: FAIL（`Cannot find module './workflow-manager-store'`）

- [ ] **Step 3: 实现 store**

```ts
// frontend/src/stores/workflow-manager-store.ts
import { create } from "zustand";

// 进入意图:browse=打开后停在浏览/预览态;create=打开后直接进空白编辑器。
// 内部的 view/editor/delete-confirm 细分态是 WorkflowManagerDialog 的本地状态,
// store 只负责"开/关 + 初次进入意图"。
type WorkflowManagerIntent = "browse" | "create";

type State = {
  open: boolean;
  intent: WorkflowManagerIntent;
};

type Actions = {
  openBrowse: () => void;
  openCreate: () => void;
  close: () => void;
};

// UI-only store(无数据):流程数据仍由 useWorkflows 拥有。放 store 是为了让命令面板
// source / 建群弹窗 link 等无父子关系的入口都能打开同一个根挂载的弹窗,免 prop drilling。
export const useWorkflowManagerStore = create<State & Actions>((set) => ({
  open: false,
  intent: "browse",
  openBrowse: () => set({ open: true, intent: "browse" }),
  openCreate: () => set({ open: true, intent: "create" }),
  close: () => set({ open: false, intent: "browse" }),
}));
```

- [ ] **Step 4: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/stores/workflow-manager-store.test.ts`
Expected: PASS（3 passed）

- [ ] **Step 5: 提交**

```bash
git add frontend/src/stores/workflow-manager-store.ts frontend/src/stores/workflow-manager-store.test.ts
git commit -m "✨ workflow: 管理弹窗开关 store(open/intent)"
```

---

## Task 2: 抽出受控编辑表单 `WorkflowEditorForm`

把 `workflow-edit-dialog.tsx` 的表单主体抽成受控组件，供管理弹窗内联编辑态复用，并把原编辑弹窗的「插入模板」行为测试迁移过来。

**Files:**
- Create: `frontend/src/components/agentre/workflows/workflow-editor-form.tsx`
- Create: `frontend/src/components/agentre/workflows/workflow-editor-form.test.tsx`

- [ ] **Step 1: 写失败测试**

```tsx
// frontend/src/components/agentre/workflows/workflow-editor-form.test.tsx
import { fireEvent, render, screen } from "@testing-library/react";
import * as React from "react";
import { describe, expect, it } from "vitest";

import { WorkflowEditorForm } from "./workflow-editor-form";

function Harness({ initialContent = "" }: { initialContent?: string }) {
  const [name, setName] = React.useState("");
  const [content, setContent] = React.useState(initialContent);
  return (
    <WorkflowEditorForm
      name={name}
      content={content}
      error={null}
      onNameChange={setName}
      onContentChange={setContent}
    />
  );
}

describe("WorkflowEditorForm", () => {
  it("编辑名称/正文回写", () => {
    render(<Harness />);
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "评审流程" },
    });
    expect(
      (screen.getByRole("textbox", { name: "Name" }) as HTMLInputElement).value,
    ).toBe("评审流程");
  });

  it("空正文点插入模板:写入模板(英文 locale 模板以 # 开头)", () => {
    render(<Harness />);
    fireEvent.click(
      screen.getByRole("button", { name: "Insert skeleton template" }),
    );
    const ta = screen.getByRole("textbox", {
      name: "Workflow body (Markdown)",
    }) as HTMLTextAreaElement;
    expect(ta.value.startsWith("#")).toBe(true);
  });

  it("非空正文插入模板:追加不覆盖", () => {
    render(<Harness initialContent="已有内容" />);
    fireEvent.click(
      screen.getByRole("button", { name: "Insert skeleton template" }),
    );
    const ta = screen.getByRole("textbox", {
      name: "Workflow body (Markdown)",
    }) as HTMLTextAreaElement;
    expect(ta.value.startsWith("已有内容")).toBe(true);
    expect(ta.value.length).toBeGreaterThan("已有内容".length);
  });

  it("error 非空时渲染错误条", () => {
    render(
      <WorkflowEditorForm
        name="x"
        content=""
        error="boom"
        onNameChange={() => {}}
        onContentChange={() => {}}
      />,
    );
    expect(screen.getByText("boom")).toBeTruthy();
  });
});
```

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/workflow-editor-form.test.tsx`
Expected: FAIL（`Cannot find module './workflow-editor-form'`）

- [ ] **Step 3: 实现受控表单**

```tsx
// frontend/src/components/agentre/workflows/workflow-editor-form.tsx
import * as React from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

export type WorkflowEditorFormProps = {
  name: string;
  content: string;
  error: string | null;
  onNameChange: (v: string) => void;
  onContentChange: (v: string) => void;
};

// 受控编辑表单:名称 + 正文(Markdown) + 「插入骨架模板」。
// 不含提交按钮/弹窗壳 —— 提交与开关由宿主(管理弹窗右栏)统一管理。
export function WorkflowEditorForm({
  name,
  content,
  error,
  onNameChange,
  onContentChange,
}: WorkflowEditorFormProps) {
  const { t } = useTranslation();

  // 骨架模板:正文非空时追加到末尾不覆盖。
  const insertTemplate = () => {
    const tpl = t("workflows.editor.template");
    onContentChange(content.trim() ? `${content.trimEnd()}\n\n${tpl}` : tpl);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3.5">
      <label className="flex flex-col gap-1.5 text-xs">
        <span className="font-medium text-foreground">
          {t("workflows.editor.name")}
          <span className="ml-0.5 text-destructive">*</span>
        </span>
        <Input
          data-testid="workflow-name-input"
          aria-label={t("workflows.editor.name")}
          value={name}
          onChange={(e) => onNameChange(e.target.value)}
          placeholder={t("workflows.editor.namePlaceholder")}
          className="h-9 text-xs"
        />
      </label>
      <div className="flex min-h-0 flex-1 flex-col gap-1.5 text-xs">
        <span className="flex items-center justify-between font-medium text-foreground">
          <span>{t("workflows.editor.content")}</span>
          <Button
            type="button"
            variant="link"
            size="sm"
            data-testid="workflow-insert-template-button"
            className="h-auto p-0 text-2xs"
            onClick={insertTemplate}
          >
            {t("workflows.editor.insertTemplate")}
          </Button>
        </span>
        <Textarea
          data-testid="workflow-content-input"
          aria-label={t("workflows.editor.content")}
          value={content}
          onChange={(e) => onContentChange(e.target.value)}
          className="min-h-0 flex-1 resize-none font-mono text-xs"
        />
      </div>
      {error ? (
        <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
          {error}
        </div>
      ) : null}
    </div>
  );
}
```

> 注意 `aria-label` 文案来自 `t("workflows.editor.name")`（en=`Name`）与 `t("workflows.editor.content")`（en=`Workflow body (Markdown)`）；插入按钮 `t("workflows.editor.insertTemplate")`（en=`Insert skeleton template`）。测试断言用的是这些 en 值（i18n 默认/测试 locale 为 en，与既有 `workflows-page.test.tsx` 一致）。若 en 现值不同，以 en `common.json` 实际值为准对齐测试断言。

- [ ] **Step 4: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/workflow-editor-form.test.tsx`
Expected: PASS（4 passed）

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/workflows/workflow-editor-form.tsx frontend/src/components/agentre/workflows/workflow-editor-form.test.tsx
git commit -m "✨ workflow: 抽出受控 WorkflowEditorForm(供管理弹窗内联编辑复用)"
```

---

## Task 3: i18n —— 新增管理弹窗/入口键 + 删 nav.workflows

先把所有要用到的 i18n 键补齐（zh-CN + en 对齐），后续组件 task 才能引用且不被 `i18n.test.ts` 拦。

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`
- Test: `frontend/src/__tests__/i18n.test.ts`（既有,不改,跑通即可）

- [ ] **Step 1: zh-CN 加键**

在 `workflows` 对象内（`deleteConfirm` 之后、对象闭合 `}` 之前）追加：

```json
    "manager": {
      "searchPlaceholder": "搜索流程名称或正文",
      "metaLive": "{{count}} 个群使用中 · 更新于 {{time}} · 修改即时生效",
      "metaLive_one": "{{count}} 个群使用中 · 更新于 {{time}} · 修改即时生效",
      "metaUnused": "未使用 · 更新于 {{time}} · 修改即时生效",
      "saveHint": "⌘ + Enter 保存 · Esc 取消",
      "deleteHint": "删除后不可恢复",
      "newWorkflowName": "未命名流程"
    },
    "actions": {
      "openLibrary": "打开流程库",
      "openLibraryHint": "浏览 / 新建 / 编辑跨群协作流程",
      "newWorkflow": "新建流程",
      "newWorkflowHint": "直接打开空白流程编辑器"
    }
```

在 `commandPalette` 对象内追加一组（与现有 `newChat` 等并列）：

```json
    "workflows": {
      "heading": "操作"
    }
```

在 `group.new` 对象内（已有 `workflow`/`workflowHint`/`workflowNone` 等）追加：

```json
      "manageWorkflows": "管理流程"
```

删除 `nav` 对象内的 `"workflows": "流程"` 一行（zh-CN）。

- [ ] **Step 2: en 加同样的键(英文文案)**

`workflows.manager`:
```json
    "manager": {
      "searchPlaceholder": "Search workflow name or body",
      "metaLive": "Used by {{count}} groups · updated {{time}} · changes apply immediately",
      "metaLive_one": "Used by {{count}} group · updated {{time}} · changes apply immediately",
      "metaUnused": "Unused · updated {{time}} · changes apply immediately",
      "saveHint": "⌘ + Enter to save · Esc to cancel",
      "deleteHint": "This cannot be undone",
      "newWorkflowName": "Untitled workflow"
    },
    "actions": {
      "openLibrary": "Open workflow library",
      "openLibraryHint": "Browse / create / edit cross-group workflows",
      "newWorkflow": "New workflow",
      "newWorkflowHint": "Open a blank workflow editor"
    }
```
`commandPalette.workflows`:
```json
    "workflows": {
      "heading": "Actions"
    }
```
`group.new.manageWorkflows`: `"Manage workflows"`。删除 en `nav.workflows`。

- [ ] **Step 3: 跑 i18n 测试**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS（zh/en key 对齐;此时尚无源码引用新键,但已存在的 key 不会 fail。删 `nav.workflows` 后**仍无** `t("nav.workflows")` 引用 —— 因为 App.tsx 改在 Task 7,这里先删 locale 会让 App.tsx 现存的 `t(item.labelKey)`(动态 key)在运行时缺失但**静态扫描不报**。为避免运行期空文案窗口,本步只加键、**先不删** `nav.workflows`;删除放到 Task 7 与 App.tsx 改动同一步)。

> 修正:Step 1/Step 2 **暂不删** `nav.workflows`,只新增键。`nav.workflows` 的删除挪到 Task 7 Step 与 navItem 移除同批提交,保证「删 locale 键」与「删用它的代码」原子。

- [ ] **Step 4: 提交**

```bash
git add frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "🌐 workflow: 加管理弹窗/命令面板/建群入口 i18n 键"
```

---

## Task 4: 管理弹窗骨架 —— 列表 + 预览(浏览态)

先实现 store 驱动开关 + 左列表 + 右预览(view 态) + 空态。编辑/删除态在 Task 5/6 叠加。

**Files:**
- Create: `frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx`
- Test: `frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx`

- [ ] **Step 1: 写失败测试(浏览态)**

```tsx
// frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const workflowList = vi.fn();
const workflowCreate = vi.fn();
const workflowUpdate = vi.fn();
const workflowDelete = vi.fn();

vi.mock("../../../../wailsjs/go/app/App", () => ({
  WorkflowList: (...a: unknown[]) => workflowList(...a),
  WorkflowCreate: (...a: unknown[]) => workflowCreate(...a),
  WorkflowUpdate: (...a: unknown[]) => workflowUpdate(...a),
  WorkflowDelete: (...a: unknown[]) => workflowDelete(...a),
}));

import { WorkflowManagerDialog } from "./workflow-manager-dialog";
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

const items = [
  {
    id: 1,
    name: "产品开发流程",
    content: "# 产品开发流程\n\n适用:新功能完整交付。\n\n## 角色",
    groupCount: 2,
    createtime: 1700000000000,
    updatetime: 1700000000000,
  },
  {
    id: 2,
    name: "紧急修复流程",
    content: "",
    groupCount: 0,
    createtime: 1700000000000,
    updatetime: 1700000000000,
  },
];

describe("WorkflowManagerDialog · 浏览态", () => {
  beforeEach(() => {
    workflowList.mockReset().mockResolvedValue({ items });
    workflowCreate.mockReset().mockResolvedValue({ item: { id: 9 } });
    workflowUpdate.mockReset().mockResolvedValue({ item: { id: 1 } });
    workflowDelete.mockReset().mockResolvedValue({});
    useWorkflowManagerStore.setState({ open: false, intent: "browse" });
  });

  it("open=false 不渲染内容", () => {
    render(<WorkflowManagerDialog />);
    expect(screen.queryByTestId("workflow-manager")).toBeNull();
  });

  it("openBrowse 后渲染列表行 + 选中后右栏预览正文", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    // 预览空态
    expect(screen.getByText("Select a workflow to preview")).toBeTruthy();
    await user.click(screen.getByTestId("workflow-row-1"));
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { level: 1, name: "产品开发流程" }),
      ).toBeTruthy(),
    );
  });

  it("空列表显示空态", async () => {
    workflowList.mockResolvedValue({ items: [] });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() =>
      expect(
        screen.getByText('No workflows yet — click "New workflow" to start'),
      ).toBeTruthy(),
    );
  });
});
```

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/workflow-manager-dialog.test.tsx`
Expected: FAIL（`Cannot find module './workflow-manager-dialog'`）

- [ ] **Step 3: 实现弹窗(浏览态 + 编辑/删除态占位)**

> 一次性写下含编辑/删除态的完整组件（Task 5/6 的测试直接复用本组件）。本步先保证浏览态测试过。

```tsx
// frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx
import * as React from "react";
import { Check, Pencil, Plus, Route, Search, Trash2, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useWorkflows, type WorkflowItem } from "@/hooks/use-workflows";
import { cn } from "@/lib/utils";
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

import { MarkdownText } from "../markdown-text";
import { WorkflowEditorForm } from "./workflow-editor-form";

// 摘要首行:跳过空行与 markdown 标题行,取第一行正文。
function firstSummaryLine(content: string): string {
  for (const raw of content.split("\n")) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    return line;
  }
  return "";
}

type DetailMode = "view" | "editor";

export function WorkflowManagerDialog() {
  const open = useWorkflowManagerStore((s) => s.open);
  const intent = useWorkflowManagerStore((s) => s.intent);
  const close = useWorkflowManagerStore((s) => s.close);
  if (!open) return null;
  return <WorkflowManagerBody intent={intent} onClose={close} />;
}

function WorkflowManagerBody({
  intent,
  onClose,
}: {
  intent: "browse" | "create";
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { workflows, loading, error, create, update, remove } = useWorkflows();

  const [selectedId, setSelectedId] = React.useState(0);
  const [mode, setMode] = React.useState<DetailMode>("view");
  // editingId: 0 = 新建;>0 = 编辑该流程
  const [editingId, setEditingId] = React.useState(0);
  const [query, setQuery] = React.useState("");
  const [confirmingDelete, setConfirmingDelete] = React.useState(false);
  // 编辑器本地草稿
  const [draftName, setDraftName] = React.useState("");
  const [draftContent, setDraftContent] = React.useState("");
  const [formError, setFormError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);

  // intent=create:打开即进空白编辑器(只跑一次)。
  const started = React.useRef(false);
  React.useEffect(() => {
    if (started.current) return;
    started.current = true;
    if (intent === "create") {
      setMode("editor");
      setEditingId(0);
      setDraftName("");
      setDraftContent("");
      setFormError(null);
    }
  }, [intent]);

  const selected = workflows.find((w) => w.id === selectedId) ?? null;

  const filtered = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return workflows;
    return workflows.filter(
      (w) =>
        w.name.toLowerCase().includes(q) ||
        w.content.toLowerCase().includes(q),
    );
  }, [workflows, query]);

  const openCreate = () => {
    setMode("editor");
    setEditingId(0);
    setDraftName("");
    setDraftContent("");
    setFormError(null);
    setConfirmingDelete(false);
  };
  const openEdit = (w: WorkflowItem) => {
    setMode("editor");
    setEditingId(w.id);
    setDraftName(w.name);
    setDraftContent(w.content);
    setFormError(null);
    setConfirmingDelete(false);
  };
  const cancelEdit = () => {
    setMode("view");
    setFormError(null);
  };

  const canSave = draftName.trim().length > 0 && !submitting;
  const submit = async () => {
    if (!canSave) return;
    setFormError(null);
    setSubmitting(true);
    try {
      if (editingId > 0) {
        await update(editingId, draftName.trim(), draftContent);
        setSelectedId(editingId);
      } else {
        await create(draftName.trim(), draftContent);
        // 新建无回传 id;reload 后留在浏览态,选择清空(列表已含新行)。
        setSelectedId(0);
      }
      setMode("view");
    } catch (err) {
      setFormError(String(err));
    } finally {
      setSubmitting(false);
    }
  };

  const confirmDelete = async () => {
    if (!selected) return;
    await remove(selected.id);
    setConfirmingDelete(false);
    setSelectedId(0);
    setMode("view");
  };

  const onEditorKeyDown = (e: React.KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      void submit();
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        showCloseButton={false}
        data-testid="workflow-manager"
        className={cn(
          "flex h-[640px] max-h-[88vh] w-[920px] max-w-[94vw] flex-col gap-0 overflow-hidden p-0",
        )}
      >
        <DialogTitle className="sr-only">{t("workflows.title")}</DialogTitle>
        <DialogDescription className="sr-only">
          {t("workflows.subtitle")}
        </DialogDescription>

        {/* Header */}
        <header className="flex shrink-0 items-center gap-3 border-b border-border bg-muted/40 px-5 py-3.5">
          <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary-soft">
            <Route className="size-4 text-primary-text" aria-hidden="true" />
          </span>
          <div className="flex min-w-0 flex-col">
            <h1 className="text-sm font-semibold text-foreground">
              {t("workflows.title")}
            </h1>
            <p className="truncate text-2xs text-muted-foreground">
              {t("workflows.subtitle")}
            </p>
          </div>
          <div className="flex-1" />
          <Button
            type="button"
            size="sm"
            data-testid="workflow-new-button"
            onClick={openCreate}
          >
            <Plus className="size-3.5" aria-hidden="true" />
            {t("workflows.new")}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={t("common.close")}
            onClick={onClose}
          >
            <X className="size-3.5" aria-hidden="true" />
          </Button>
        </header>

        {/* Body */}
        <div className="flex min-h-0 flex-1">
          {/* List column */}
          <aside className="flex w-[300px] shrink-0 flex-col border-r border-border bg-muted/20">
            <div className="border-b border-border p-2.5">
              <div className="flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1.5">
                <Search
                  className="size-3.5 shrink-0 text-muted-foreground"
                  aria-hidden="true"
                />
                <Input
                  aria-label={t("workflows.manager.searchPlaceholder")}
                  placeholder={t("workflows.manager.searchPlaceholder")}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  className="h-auto border-0 bg-transparent p-0 text-xs shadow-none focus-visible:ring-0"
                />
              </div>
            </div>
            <div className="flex-1 overflow-y-auto">
              {error ? (
                <div className="px-4 py-2 text-2xs text-destructive">
                  {error}
                </div>
              ) : null}
              {filtered.length === 0 && !loading && !error ? (
                <div className="px-4 py-8 text-center text-xs text-muted-foreground">
                  {t("workflows.empty")}
                </div>
              ) : null}
              {filtered.map((w) => {
                const summary = firstSummaryLine(w.content);
                const active = selectedId === w.id;
                return (
                  <button
                    key={w.id}
                    type="button"
                    data-testid={`workflow-row-${w.id}`}
                    aria-current={active ? "true" : undefined}
                    onClick={() => {
                      setSelectedId(w.id);
                      setMode("view");
                      setConfirmingDelete(false);
                    }}
                    className={cn(
                      "flex w-full cursor-pointer flex-col gap-1 border-b border-border px-4 py-2.5 text-left",
                      active
                        ? "border-l-[3px] border-l-primary bg-primary-soft"
                        : "hover:bg-accent/50",
                    )}
                  >
                    <div className="flex w-full items-center gap-2">
                      <span className="min-w-0 flex-1 truncate text-xs font-medium text-foreground">
                        {w.name}
                      </span>
                      {w.groupCount > 0 ? (
                        <span className="shrink-0 rounded-full bg-accent px-1.5 py-0.5 text-2xs text-muted-foreground">
                          {t("workflows.groupCount", { count: w.groupCount })}
                        </span>
                      ) : null}
                    </div>
                    {summary ? (
                      <span className="w-full truncate text-2xs text-muted-foreground">
                        {summary}
                      </span>
                    ) : null}
                    <span className="text-2xs text-muted-foreground">
                      {t("workflows.updatedAt", {
                        time: new Date(w.updatetime).toLocaleDateString(),
                      })}
                    </span>
                  </button>
                );
              })}
            </div>
          </aside>

          {/* Detail pane */}
          <section className="flex min-w-0 flex-1 flex-col bg-muted/10">
            {mode === "editor" ? (
              <EditorPane
                editing={editingId > 0}
                name={draftName}
                content={draftContent}
                error={formError}
                canSave={canSave}
                onNameChange={setDraftName}
                onContentChange={setDraftContent}
                onCancel={cancelEdit}
                onSave={() => void submit()}
                onKeyDown={onEditorKeyDown}
              />
            ) : selected ? (
              <ViewPane
                workflow={selected}
                confirmingDelete={confirmingDelete}
                onEdit={() => openEdit(selected)}
                onAskDelete={() => setConfirmingDelete(true)}
                onCancelDelete={() => setConfirmingDelete(false)}
                onConfirmDelete={() => void confirmDelete()}
              />
            ) : (
              <div className="flex flex-1 items-center justify-center text-xs text-muted-foreground">
                {t("workflows.preview.empty")}
              </div>
            )}
          </section>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ViewPane({
  workflow,
  confirmingDelete,
  onEdit,
  onAskDelete,
  onCancelDelete,
  onConfirmDelete,
}: {
  workflow: WorkflowItem;
  confirmingDelete: boolean;
  onEdit: () => void;
  onAskDelete: () => void;
  onCancelDelete: () => void;
  onConfirmDelete: () => void;
}) {
  const { t } = useTranslation();
  const meta =
    workflow.groupCount > 0
      ? t("workflows.manager.metaLive", {
          count: workflow.groupCount,
          time: new Date(workflow.updatetime).toLocaleDateString(),
        })
      : t("workflows.manager.metaUnused", {
          time: new Date(workflow.updatetime).toLocaleDateString(),
        });
  return (
    <>
      <header className="flex flex-col gap-1 border-b border-border px-5 py-3">
        <div className="flex items-center gap-2.5">
          <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary-soft">
            <Route className="size-4 text-primary-text" aria-hidden="true" />
          </span>
          <h2 className="text-sm font-semibold text-foreground">
            {workflow.name}
          </h2>
        </div>
        <p className="text-2xs text-muted-foreground">{meta}</p>
      </header>
      <div className="flex-1 overflow-y-auto px-5 py-4">
        <MarkdownText text={workflow.content} />
      </div>
      {confirmingDelete ? (
        <DeleteConfirmBar
          workflow={workflow}
          onCancel={onCancelDelete}
          onConfirm={onConfirmDelete}
        />
      ) : (
        <footer className="flex items-center gap-2 border-t border-border px-5 py-3">
          <Button
            type="button"
            size="sm"
            className="flex-1"
            data-testid="workflow-edit-button"
            onClick={onEdit}
          >
            <Pencil className="size-3.5" aria-hidden="true" />
            {t("workflows.edit")}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="icon-sm"
            data-testid="workflow-delete-button"
            aria-label={t("workflows.delete")}
            onClick={onAskDelete}
          >
            <Trash2 className="size-3.5" aria-hidden="true" />
          </Button>
        </footer>
      )}
    </>
  );
}

function DeleteConfirmBar({
  workflow,
  onCancel,
  onConfirm,
}: {
  workflow: WorkflowItem;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useTranslation();
  const desc =
    workflow.groupCount > 0
      ? t("workflows.deleteConfirm.desc", {
          name: workflow.name,
          count: workflow.groupCount,
        })
      : t("workflows.deleteConfirm.descUnused", { name: workflow.name });
  return (
    <div
      data-testid="workflow-delete-confirm"
      className="flex flex-col gap-3 border-t border-destructive/40 bg-destructive-soft px-5 py-3.5"
    >
      <div className="flex items-start gap-2.5">
        <Trash2
          className="mt-0.5 size-4 shrink-0 text-destructive"
          aria-hidden="true"
        />
        <div className="flex flex-col gap-0.5">
          <span className="text-xs font-semibold text-destructive">
            {t("workflows.deleteConfirm.title")}
          </span>
          <span className="text-2xs leading-relaxed text-muted-foreground">
            {desc}
          </span>
        </div>
      </div>
      <div className="flex items-center justify-end gap-2">
        <Button type="button" variant="outline" size="sm" onClick={onCancel}>
          {t("common.cancel")}
        </Button>
        <Button
          type="button"
          variant="destructive"
          size="sm"
          data-testid="workflow-delete-confirm-button"
          onClick={onConfirm}
        >
          {t("workflows.deleteConfirm.confirm")}
        </Button>
      </div>
    </div>
  );
}

function EditorPane({
  editing,
  name,
  content,
  error,
  canSave,
  onNameChange,
  onContentChange,
  onCancel,
  onSave,
  onKeyDown,
}: {
  editing: boolean;
  name: string;
  content: string;
  error: string | null;
  canSave: boolean;
  onNameChange: (v: string) => void;
  onContentChange: (v: string) => void;
  onCancel: () => void;
  onSave: () => void;
  onKeyDown: (e: React.KeyboardEvent) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-0 flex-1 flex-col" onKeyDown={onKeyDown}>
      <header className="flex items-center gap-2.5 border-b border-border px-5 py-3">
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary-soft">
          <Pencil className="size-4 text-primary-text" aria-hidden="true" />
        </span>
        <h2 className="text-sm font-semibold text-foreground">
          {editing
            ? t("workflows.editor.editTitle")
            : t("workflows.editor.createTitle")}
        </h2>
      </header>
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-5 py-4">
        <WorkflowEditorForm
          name={name}
          content={content}
          error={error}
          onNameChange={onNameChange}
          onContentChange={onContentChange}
        />
      </div>
      <footer className="flex items-center gap-2 border-t border-border px-5 py-3">
        <span className="text-2xs text-muted-foreground">
          {t("workflows.manager.saveHint")}
        </span>
        <div className="flex-1" />
        <Button type="button" variant="outline" size="sm" onClick={onCancel}>
          {t("common.cancel")}
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={!canSave}
          data-testid="workflow-save-button"
          onClick={onSave}
        >
          <Check className="size-3.5" aria-hidden="true" />
          {t("workflows.editor.save")}
        </Button>
      </footer>
    </div>
  );
}
```

> 若 `@/components/ui/button` 无 `size="icon-sm"`/`icon-xs` 变体，用既有最接近的（如 `size="icon"` + className 调尺寸）；以 `button.tsx` 实际 variants 为准。`bg-primary-soft`/`text-primary-text`/`bg-destructive-soft` 是项目既有 token（见 `workflows-page`/`group-new-dialog` 用法）。

- [ ] **Step 4: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/workflow-manager-dialog.test.tsx`
Expected: PASS（浏览态 3 用例）

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx
git commit -m "✨ workflow: 管理弹窗骨架(列表+预览浏览态)"
```

---

## Task 5: 管理弹窗 —— 内联编辑/新建

组件已含编辑态（Task 4），本 task 补行为测试并验证新建/编辑保存路径。

**Files:**
- Modify: `frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx`

- [ ] **Step 1: 追加测试**

```tsx
describe("WorkflowManagerDialog · 内联编辑", () => {
  beforeEach(() => {
    workflowList.mockReset().mockResolvedValue({ items });
    workflowCreate.mockReset().mockResolvedValue({ item: { id: 9 } });
    workflowUpdate.mockReset().mockResolvedValue({ item: { id: 1 } });
    workflowDelete.mockReset().mockResolvedValue({});
    useWorkflowManagerStore.setState({ open: false, intent: "browse" });
  });

  it("新建按钮 → 编辑器 → 保存调 WorkflowCreate + 回浏览态", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-new-button"));
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "评审流程" },
    });
    await user.click(screen.getByTestId("workflow-save-button"));
    await waitFor(() =>
      expect(workflowCreate).toHaveBeenCalledWith({
        name: "评审流程",
        content: "",
      }),
    );
    expect(workflowList.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("intent=create 打开即编辑器", async () => {
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openCreate();
    await waitFor(() =>
      expect(screen.getByRole("textbox", { name: "Name" })).toBeTruthy(),
    );
  });

  it("选中后点编辑 → 预填名称 → 保存调 WorkflowUpdate", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-row-1"));
    await user.click(screen.getByTestId("workflow-edit-button"));
    const nameInput = screen.getByRole("textbox", {
      name: "Name",
    }) as HTMLInputElement;
    expect(nameInput.value).toBe("产品开发流程");
    fireEvent.change(nameInput, { target: { value: "产品开发流程 v2" } });
    await user.click(screen.getByTestId("workflow-save-button"));
    await waitFor(() =>
      expect(workflowUpdate).toHaveBeenCalledWith(
        expect.objectContaining({ id: 1, name: "产品开发流程 v2" }),
      ),
    );
  });
});
```

> 顶部 import 补 `fireEvent`：`import { fireEvent, render, screen, waitFor } from "@testing-library/react";`

- [ ] **Step 2: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/workflow-manager-dialog.test.tsx`
Expected: PASS（编辑态 3 用例 + 之前的浏览态）

- [ ] **Step 3: 提交**

```bash
git add frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx
git commit -m "✅ workflow: 管理弹窗内联编辑/新建行为测试"
```

---

## Task 6: 管理弹窗 —— 内联删除确认

**Files:**
- Modify: `frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx`

- [ ] **Step 1: 追加测试**

```tsx
describe("WorkflowManagerDialog · 内联删除", () => {
  beforeEach(() => {
    workflowList.mockReset().mockResolvedValue({ items });
    workflowDelete.mockReset().mockResolvedValue({});
    useWorkflowManagerStore.setState({ open: false, intent: "browse" });
  });

  it("删除图标 → 内联确认条(带使用中群数) → 确认调 WorkflowDelete", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-row-1"));
    await user.click(screen.getByTestId("workflow-delete-button"));
    // 内联确认条出现(不弹新窗),含使用中群数文案
    expect(screen.getByTestId("workflow-delete-confirm")).toBeTruthy();
    expect(
      screen.getByText(
        '"产品开发流程" is used by 2 groups; after deletion they fall back to "no workflow". This cannot be undone.',
      ),
    ).toBeTruthy();
    await user.click(screen.getByTestId("workflow-delete-confirm-button"));
    await waitFor(() => expect(workflowDelete).toHaveBeenCalledWith({ id: 1 }));
  });

  it("删除后回预览空态", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowManagerDialog />);
    useWorkflowManagerStore.getState().openBrowse();
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByTestId("workflow-row-1"));
    workflowList.mockResolvedValue({ items: [items[1]] });
    await user.click(screen.getByTestId("workflow-delete-button"));
    await user.click(screen.getByTestId("workflow-delete-confirm-button"));
    await waitFor(() =>
      expect(screen.getByText("Select a workflow to preview")).toBeTruthy(),
    );
  });
});
```

> 删除确认文案断言以 en `workflows.deleteConfirm.desc`（`{{count}}`复数形）实际值为准；若 en 现值与断言串不一致，对齐 en `common.json` 实际文案。

- [ ] **Step 2: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/workflow-manager-dialog.test.tsx`
Expected: PASS（删除 2 用例 + 之前全部）

- [ ] **Step 3: 提交**

```bash
git add frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx
git commit -m "✅ workflow: 管理弹窗内联删除确认测试"
```

---

## Task 7: 移除 rail/route/页面 + 根挂载弹窗 + index 导出切换 + 删 nav.workflows

**Files:**
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/components/agentre/index.ts`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`、`frontend/src/i18n/locales/en/common.json`
- Delete: `frontend/src/components/agentre/workflows/workflows-page.tsx`、`workflows-page.test.tsx`、`workflow-edit-dialog.tsx`、`workflow-edit-dialog.test.tsx`、`workflow-delete-dialog.tsx`（若存在 `workflow-delete-dialog.test.tsx` 一并删）

- [ ] **Step 1: index.ts 导出切换**

`frontend/src/components/agentre/index.ts`：
- 删 `export { WorkflowsPage } from "./workflows/workflows-page";`（line 28）。
- 加 `export { WorkflowManagerDialog } from "./workflows/workflow-manager-dialog";`。

- [ ] **Step 2: App.tsx 改动**

- 删 import：`import routeIcon from "@iconify-icons/tabler/route-2";`（line 20）。
- `@/components/agentre` 的具名 import 里把 `WorkflowsPage` 换成 `WorkflowManagerDialog`（line 37）。
- 删 `navItems` 中 `/workflows` 项（line 94-98 整块）。
- 删 `pageBreadcrumbKeys["/workflows"]: "nav.workflows"`（line 118）。
- 删路由 `<Route path="/workflows" element={<WorkflowsPage />} />`（line 886）。
- 在根挂载弹窗:与 `<QuitConfirmDialog />` 同级(line 878 附近,`<Routes>` 之前)加一行：
  ```tsx
  <WorkflowManagerDialog />
  ```
  （`WorkflowManagerDialog` open=false 时返回 null,常驻无开销;由 store 驱动。）

- [ ] **Step 3: 删 nav.workflows i18n 键**

zh-CN `nav` 对象删 `"workflows": "流程"`；en `nav` 对象删对应行。（与代码移除同批,保证原子。）

- [ ] **Step 4: 删旧文件**

```bash
git rm frontend/src/components/agentre/workflows/workflows-page.tsx \
       frontend/src/components/agentre/workflows/workflows-page.test.tsx \
       frontend/src/components/agentre/workflows/workflow-edit-dialog.tsx \
       frontend/src/components/agentre/workflows/workflow-edit-dialog.test.tsx \
       frontend/src/components/agentre/workflows/workflow-delete-dialog.tsx
# 若存在: git rm frontend/src/components/agentre/workflows/workflow-delete-dialog.test.tsx
```

- [ ] **Step 5: 验证 —— i18n + tsc + 全量 vitest**

Run（i18n 与单测无需真实 wailsjs）：
```bash
cd frontend && pnpm test -- src/__tests__/i18n.test.ts
cd frontend && pnpm test
```
Expected: i18n PASS（无 `nav.workflows` 残留引用,zh/en 对齐）；全量 vitest 全绿（确认删旧文件后无悬空 import；`App.test` 若断言过 `nav-workflows` 需同步删该断言 —— 跑全量发现）。

Run（类型/lint，需先生成 wailsjs）：
```bash
cd .. && GOWORK=off make generate && cd frontend && pnpm exec tsc -b && pnpm lint
```
Expected: tsc 0 错（无 `WorkflowsPage`/`routeIcon` 悬空引用）、eslint 0（含 i18n no-literal-string）。

- [ ] **Step 6: 提交**

```bash
git add frontend/src/App.tsx frontend/src/components/agentre/index.ts \
        frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git add -A frontend/src/components/agentre/workflows/
git commit -m "♻️ workflow: 流程库移出 rail/路由,根挂管理弹窗,删整页与独立编辑/删除弹窗"
```

---

## Task 8: 命令面板入口（打开流程库 / 新建流程）

**Files:**
- Create: `frontend/src/components/agentre/command-palette/sources/workflow-actions-source.tsx`
- Create: `frontend/src/components/agentre/command-palette/sources/workflow-actions-source.test.tsx`
- Modify: `frontend/src/components/agentre/command-palette/command-palette.tsx`

- [ ] **Step 1: 写失败测试**

```tsx
// frontend/src/components/agentre/command-palette/sources/workflow-actions-source.test.tsx
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

import { workflowActionsSource } from "./workflow-actions-source";
import type { OnSelectCtx } from "../types";

function ctx(): OnSelectCtx {
  return {
    navigate: vi.fn() as unknown as OnSelectCtx["navigate"],
    close: vi.fn(),
    openSession: vi.fn(),
    openNewSession: vi.fn(),
    pathname: "/chat",
  };
}

describe("workflowActionsSource", () => {
  beforeEach(() => {
    useWorkflowManagerStore.setState({ open: false, intent: "browse" });
  });

  it("命令模式下两条命令(open / new)", () => {
    expect(workflowActionsSource.modes).toContain("command");
    const { items } = workflowActionsSource.useItems();
    expect(items.map((i) => i.key)).toEqual([
      "workflow-open-library",
      "workflow-new",
    ]);
  });

  it("open 命令:close + openBrowse", () => {
    const c = ctx();
    const open = workflowActionsSource
      .useItems()
      .items.find((i) => i.key === "workflow-open-library")!;
    workflowActionsSource.onSelect(open, c);
    expect(c.close).toHaveBeenCalled();
    expect(useWorkflowManagerStore.getState().open).toBe(true);
    expect(useWorkflowManagerStore.getState().intent).toBe("browse");
  });

  it("new 命令:close + openCreate", () => {
    const c = ctx();
    const item = workflowActionsSource
      .useItems()
      .items.find((i) => i.key === "workflow-new")!;
    workflowActionsSource.onSelect(item, c);
    expect(c.close).toHaveBeenCalled();
    expect(useWorkflowManagerStore.getState().intent).toBe("create");
  });

  it("getScore:标题命中 query 返回正分,不命中 0", () => {
    const item = workflowActionsSource
      .useItems()
      .items.find((i) => i.key === "workflow-open-library")!;
    expect(workflowActionsSource.getScore("流程", item)).toBeGreaterThan(0);
    expect(workflowActionsSource.getScore("zzzzz", item)).toBe(0);
  });
});
```

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/components/agentre/command-palette/sources/workflow-actions-source.test.tsx`
Expected: FAIL（`Cannot find module './workflow-actions-source'`）

- [ ] **Step 3: 实现 source**

```tsx
// frontend/src/components/agentre/command-palette/sources/workflow-actions-source.tsx
import * as React from "react";
import { Plus, Route } from "lucide-react";
import { useTranslation } from "react-i18next";

import i18n from "@/i18n";
import { cn } from "@/lib/utils";
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

import { scoreItem } from "../score";
import type { CommandSource, OnSelectCtx } from "../types";

type WorkflowActionKey = "workflow-open-library" | "workflow-new";

export type WorkflowActionItem = {
  key: WorkflowActionKey;
  titleKey: string;
  hintKey: string;
  icon: "route" | "plus";
};

const ACTIONS: WorkflowActionItem[] = [
  {
    key: "workflow-open-library",
    titleKey: "workflows.actions.openLibrary",
    hintKey: "workflows.actions.openLibraryHint",
    icon: "route",
  },
  {
    key: "workflow-new",
    titleKey: "workflows.actions.newWorkflow",
    hintKey: "workflows.actions.newWorkflowHint",
    icon: "plus",
  },
];

function useItems(): { items: WorkflowActionItem[]; loading: boolean } {
  return { items: ACTIONS, loading: false };
}

function getScore(query: string, item: WorkflowActionItem): number {
  return scoreItem({
    query,
    title: i18n.t(item.titleKey),
    subtitle: i18n.t(item.hintKey),
  });
}

function ActionRow({ item }: { item: WorkflowActionItem }) {
  const { t } = useTranslation();
  const Icon = item.icon === "plus" ? Plus : Route;
  return (
    <div className="flex w-full items-center gap-3">
      <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary-soft">
        <Icon className="size-4 text-primary-text" aria-hidden="true" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-sm font-medium text-foreground">
          {t(item.titleKey)}
        </span>
        <span className="truncate text-2xs text-muted-foreground">
          {t(item.hintKey)}
        </span>
      </div>
      <kbd
        className={cn(
          "rounded-sm border border-border bg-card px-1.5 py-0.5 font-mono text-2xs font-medium text-muted-foreground",
          "opacity-0 group-data-[selected=true]/cmditem:opacity-100",
        )}
        aria-hidden="true"
      >
        ↵
      </kbd>
    </div>
  );
}

function onSelect(item: WorkflowActionItem, ctx: OnSelectCtx): void {
  ctx.close();
  const store = useWorkflowManagerStore.getState();
  if (item.key === "workflow-new") store.openCreate();
  else store.openBrowse();
}

export const workflowActionsSource: CommandSource<WorkflowActionItem> = {
  id: "workflow-actions",
  heading: i18n.t("commandPalette.workflows.heading"),
  modes: ["command"],
  useItems,
  getScore,
  renderItem: (item) => <ActionRow item={item} />,
  onSelect,
};
```

> `scoreItem` 的入参形状以 `command-palette/score.ts` 实际导出为准（new-chat-source 用的是 `scoreItem({ query, title, subtitle })`）。若签名不同，按其签名调整。

- [ ] **Step 4: 注册进 SOURCES**

`command-palette.tsx`：import 后把 `workflowActionsSource` 加进 `SOURCES` 数组：
```tsx
import { workflowActionsSource } from "./sources/workflow-actions-source";
// ...
const SOURCES: CommandSource<any>[] = [
  chatSessionsSource,
  newChatSource,
  newProjectChatSource,
  workflowActionsSource,
];
```

- [ ] **Step 5: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/components/agentre/command-palette/sources/workflow-actions-source.test.tsx`
Expected: PASS（4 用例）

- [ ] **Step 6: 提交**

```bash
git add frontend/src/components/agentre/command-palette/sources/workflow-actions-source.tsx \
        frontend/src/components/agentre/command-palette/sources/workflow-actions-source.test.tsx \
        frontend/src/components/agentre/command-palette/command-palette.tsx
git commit -m "✨ workflow: 命令面板加「打开流程库 / 新建流程」命令"
```

---

## Task 9: 建群弹窗加「管理流程」入口 + 关闭刷新下拉

**Files:**
- Modify: `frontend/src/components/agentre/group-chat/group-new-dialog.tsx`
- Modify/Create: `frontend/src/components/agentre/group-chat/group-new-dialog.test.tsx`

- [ ] **Step 1: 写失败测试**

在 `group-new-dialog.test.tsx`（既有；若需新建则建）追加。该文件已 mock `wailsjs/go/app/App`（含 `WorkflowList`）。新增：

```tsx
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

it("「管理流程」link 打开管理弹窗(openBrowse)", async () => {
  const user = userEvent.setup({ pointerEventsCheck: 0 });
  useWorkflowManagerStore.setState({ open: false, intent: "browse" });
  render(<GroupNewDialog open onOpenChange={() => {}} />);
  await user.click(screen.getByTestId("group-manage-workflows"));
  expect(useWorkflowManagerStore.getState().open).toBe(true);
  expect(useWorkflowManagerStore.getState().intent).toBe("browse");
});
```

> 若既有 `group-new-dialog.test.tsx` 的 mock 未含 `WorkflowList`，补上（`WorkflowList: vi.fn().mockResolvedValue({ items: [] })`）。

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/components/agentre/group-chat/group-new-dialog.test.tsx`
Expected: FAIL（找不到 `group-manage-workflows` testid）

- [ ] **Step 3: 实现**

`group-new-dialog.tsx`：

1. 顶部 import 加：
   ```tsx
   import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";
   ```
2. 把第 76-85 行的 `WorkflowList().then(...)` 抽成可复用回调（放在 component 内、effect 之上）：
   ```tsx
   const loadWorkflows = React.useCallback(() => {
     WorkflowList()
       .then((resp) =>
         setWorkflowOptions(
           (resp?.items ?? []).map((i: { id: number; name: string }) => ({
             id: i.id,
             name: i.name,
           })),
         ),
       )
       .catch(() => setWorkflowOptions([]));
   }, []);
   ```
   并把 open-effect 里的 `WorkflowList().then(...)` 替换为 `loadWorkflows();`。
3. 订阅管理弹窗开关，在它从 open→close 时刷新下拉（新建的流程立即可选）：
   ```tsx
   const wfManagerOpen = useWorkflowManagerStore((s) => s.open);
   const prevWfOpen = React.useRef(wfManagerOpen);
   React.useEffect(() => {
     if (prevWfOpen.current && !wfManagerOpen && open) loadWorkflows();
     prevWfOpen.current = wfManagerOpen;
   }, [wfManagerOpen, open, loadWorkflows]);
   ```
4. 在「协作流程」label 行（`group-new-dialog.tsx:191-194` 的 `<span>{t("group.new.workflow")}</span>`）改为带「管理流程」link 的行：
   ```tsx
   <span className="flex items-center justify-between font-medium text-foreground">
     <span>{t("group.new.workflow")}</span>
     <Button
       type="button"
       variant="link"
       size="sm"
       data-testid="group-manage-workflows"
       className="h-auto p-0 text-2xs"
       onClick={() => useWorkflowManagerStore.getState().openBrowse()}
     >
       {t("group.new.manageWorkflows")}
     </Button>
   </span>
   ```

- [ ] **Step 4: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/components/agentre/group-chat/group-new-dialog.test.tsx`
Expected: PASS（新增用例 + 原有用例）

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/group-chat/group-new-dialog.tsx \
        frontend/src/components/agentre/group-chat/group-new-dialog.test.tsx
git commit -m "✨ workflow: 建群弹窗加「管理流程」入口,弹窗关闭刷新下拉"
```

---

## Task 10: 全量校验

**Files:** 无（验证）

- [ ] **Step 1: 全量 vitest**

Run: `cd frontend && pnpm test`
Expected: 全绿。重点确认无悬空 import（旧文件已删）、无组件因新键缺失报错。

- [ ] **Step 2: i18n 测试**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS（zh/en 键对齐;新键全覆盖;无 `nav.workflows` 残留;无硬编码中文）。

- [ ] **Step 3: 类型 + lint（需真实 wailsjs）**

Run:
```bash
cd /Users/codfrm/Code/agentre/agentre && GOWORK=off make generate
cd frontend && pnpm exec tsc -b && pnpm lint
```
Expected: tsc 0 错、eslint 0。

- [ ] **Step 4: 人工冒烟（可选,按惯例）**

`make dev` 启动，验证：① rail 只剩 5 项、无「流程」；② ⌘P 输入 `>流程` 出「打开流程库/新建流程」，回车开弹窗；③ 弹窗内新建/编辑/内联删除确认；④ 建群弹窗「管理流程」link 开弹窗、关后下拉含新建流程；⑤ 旧 `/workflows`（若 localStorage 残留）启动回落 `/chat`。

- [ ] **Step 5: 收尾提交（若冒烟无改动则跳过）**

```bash
git add -A && git commit -m "✅ workflow: PR1 全量校验通过"
```

---

## Self-Review（已对照 spec Part A）

- **rail 槽位释放**：Task 7 删 navItem/route/breadcrumb/import ✓
- **管理弹窗三态**：Task 4(浏览/预览) + Task 5(内联编辑) + Task 6(内联删除确认) ✓，弹窗深度 ≤2（内联切换不叠窗）✓
- **两入口**：Task 8(命令面板两命令) + Task 9(建群 link + 关后刷新) ✓
- **复用**：`useWorkflows`/`MarkdownText` 复用；编辑表单抽 `WorkflowEditorForm`（Task 2）✓
- **i18n**：Task 3 加键 + Task 7 删 `nav.workflows`（与代码同批）✓
- **测试**：每新单元先 red 后 green；删旧 `workflows-page.test` 同步（Task 7）✓
- **类型一致**：store 方法 `openBrowse/openCreate/close` 在 Task 1/4/8/9 一致；testid（`workflow-manager`/`workflow-row-{id}`/`workflow-new-button`/`workflow-edit-button`/`workflow-delete-button`/`workflow-delete-confirm(-button)`/`workflow-save-button`/`group-manage-workflows`）跨 task 一致 ✓
- **构建坑**：worktree 需 `GOWORK=off make generate` + `frontend/dist` 占位（已记于约束）✓
