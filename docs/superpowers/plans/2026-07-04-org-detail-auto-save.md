# 组织架构详情表单自动保存 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 去掉组织架构部门/智能体详情表单的「取消 / 保存」按钮,改为任何字段有变动就自动保存(文本防抖+失焦即存,离散选择即时存),并用一条自动保存状态条替代页脚。

**Architecture:** 新建一个可复用的 `useAutoSave<T>` hook 承载防抖 / 即时 / flush / 状态机 / 有效性守卫 / 重试逻辑,由两个详情表单共用;再抽一个 `AutoSaveStatus` 小组件渲染状态条。两个表单把逐字段 `useState` 换成一次 `useAutoSave` + 解构 `values`(读取点不变,只改 setter 调用点为 `patch`)。结构性/独立 mutation(部门换父级 `onMove`、智能体头像上传/删除)走 `wrap(...)` 喂给同一状态机。

**Tech Stack:** React 19 + TypeScript,vitest + @testing-library/react(renderHook + fake timers),react-i18next,shadcn `@/components/ui/*`。

## Global Constraints

- **TDD 严格 Red→Green→Refactor**:先写失败测试并跑到「因正确原因失败」,再实现。
- **只改本任务相关文件**:diff 仅含新 hook + 状态条 + 两个详情表单接线 + 各自测试 + 两个 i18n key;**不做**无关重构 / rename / 格式化扫 / import 重排。
- **前端表单控件统一用 shadcn `@/components/ui/*`**;禁止原生 `<select>`。
- **新可见 UI 文案必须走 i18n**:`react-i18next` 的 `t(...)`,同步更新 `frontend/src/i18n/locales/zh-CN/common.json` 与 `en/common.json`;`i18next/no-literal-string` 会拦截 JSX 里的硬编码中文。
- **不动后端与 `use-org-data.ts`**:baseline 刷新依赖 `mutate` 现有的「in-flight 归零后 reload」行为,保持不变。
- 前端根目录:`/Users/codfrm/Code/agentre/agentre/frontend`。共享分支 `develop/wyz` 有并发会话:**提交一律带 pathspec**(`git commit <files> -m ...`),不要裸 `git commit`。
- gitmoji 提交风格。

---

### Task 1: `useAutoSave<T>` hook

**Files:**
- Create: `frontend/src/components/agentre/org/use-auto-save.ts`
- Test: `frontend/src/components/agentre/org/__tests__/use-auto-save.test.tsx`

**Interfaces:**
- Consumes: 无(叶子 hook,仅依赖 React)。
- Produces:
  - `type AutoSaveStatus = "idle" | "saving" | "saved" | "error"`
  - `function useAutoSave<T extends object>(opts: { initial: T; save: (values: T) => Promise<unknown>; debounceMs?: number; isValid?: (values: T) => boolean }): { values: T; patch: (partial: Partial<T>, opts?: { immediate?: boolean }) => void; flush: () => void; wrap: <R>(fn: () => Promise<R>) => Promise<R | null>; status: AutoSaveStatus; pendingInvalid: boolean; retry: () => void }`

- [ ] **Step 1: 写失败测试**

创建 `frontend/src/components/agentre/org/__tests__/use-auto-save.test.tsx`:

```tsx
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useAutoSave } from "../use-auto-save";

type Form = { name: string; color: string };
const initial: Form = { name: "A", color: "red" };

describe("useAutoSave", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("immediate patch saves once with merged values", async () => {
    const save = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() => useAutoSave({ initial, save }));

    act(() => result.current.patch({ color: "blue" }, { immediate: true }));

    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith({ name: "A", color: "blue" });
    expect(result.current.values).toEqual({ name: "A", color: "blue" });
    await act(async () => {});
    expect(result.current.status).toBe("saved");
  });

  it("debounced patches coalesce into one save with the latest value", () => {
    const save = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() =>
      useAutoSave({ initial, save, debounceMs: 600 }),
    );

    act(() => result.current.patch({ name: "AB" }));
    act(() => result.current.patch({ name: "ABC" }));
    expect(save).not.toHaveBeenCalled();

    act(() => vi.advanceTimersByTime(600));
    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith({ name: "ABC", color: "red" });
  });

  it("flush runs a pending debounced save immediately", () => {
    const save = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() => useAutoSave({ initial, save }));

    act(() => result.current.patch({ name: "AB" }));
    act(() => result.current.flush());
    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith({ name: "AB", color: "red" });
  });

  it("holds save when invalid, then saves once valid again", () => {
    const save = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() =>
      useAutoSave({
        initial,
        save,
        isValid: (v) => v.name.trim() !== "",
      }),
    );

    act(() => result.current.patch({ name: "" }, { immediate: true }));
    expect(save).not.toHaveBeenCalled();
    expect(result.current.pendingInvalid).toBe(true);

    act(() => result.current.patch({ name: "B" }, { immediate: true }));
    expect(save).toHaveBeenCalledTimes(1);
    expect(result.current.pendingInvalid).toBe(false);
  });

  it("sets error on rejection and retry re-runs the save", async () => {
    const save = vi
      .fn()
      .mockRejectedValueOnce(new Error("boom"))
      .mockResolvedValueOnce(undefined);
    const { result } = renderHook(() => useAutoSave({ initial, save }));

    await act(async () => {
      result.current.patch({ color: "blue" }, { immediate: true });
    });
    expect(result.current.status).toBe("error");

    await act(async () => {
      result.current.retry();
    });
    expect(save).toHaveBeenCalledTimes(2);
    expect(result.current.status).toBe("saved");
  });

  it("wrap drives status and returns the fn result", async () => {
    const save = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() => useAutoSave({ initial, save }));

    let ret: string | null = "x";
    await act(async () => {
      ret = await result.current.wrap(async () => "moved");
    });
    expect(ret).toBe("moved");
    expect(result.current.status).toBe("saved");
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/use-auto-save.test.tsx`
Expected: FAIL —— `Cannot find module '../use-auto-save'`。

- [ ] **Step 3: 实现 hook**

创建 `frontend/src/components/agentre/org/use-auto-save.ts`:

```ts
import * as React from "react";

export type AutoSaveStatus = "idle" | "saving" | "saved" | "error";

export interface UseAutoSaveOptions<T> {
  initial: T;
  save: (values: T) => Promise<unknown>;
  debounceMs?: number;
  isValid?: (values: T) => boolean;
}

export interface UseAutoSaveResult<T> {
  values: T;
  patch: (partial: Partial<T>, opts?: { immediate?: boolean }) => void;
  flush: () => void;
  wrap: <R>(fn: () => Promise<R>) => Promise<R | null>;
  status: AutoSaveStatus;
  pendingInvalid: boolean;
  retry: () => void;
}

export function useAutoSave<T extends object>(
  opts: UseAutoSaveOptions<T>,
): UseAutoSaveResult<T> {
  const debounceMs = opts.debounceMs ?? 600;

  const [values, setValues] = React.useState<T>(opts.initial);
  const [status, setStatus] = React.useState<AutoSaveStatus>("idle");
  const [pendingInvalid, setPendingInvalid] = React.useState(false);

  // refs 让所有回调保持稳定身份,并且保存时读到最新值/最新 save/isValid。
  const valuesRef = React.useRef(values);
  valuesRef.current = values;
  const saveRef = React.useRef(opts.save);
  saveRef.current = opts.save;
  const isValidRef = React.useRef(opts.isValid);
  isValidRef.current = opts.isValid;
  const timerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastActionRef = React.useRef<(() => Promise<unknown>) | null>(null);

  const clearTimer = React.useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const run = React.useCallback(async (action: () => Promise<unknown>) => {
    lastActionRef.current = action;
    setStatus("saving");
    try {
      await action();
      setStatus("saved");
    } catch {
      setStatus("error");
    }
  }, []);

  const saveNow = React.useCallback(() => {
    clearTimer();
    const snapshot = valuesRef.current;
    void run(() => saveRef.current(snapshot));
  }, [clearTimer, run]);

  const patch = React.useCallback(
    (partial: Partial<T>, patchOpts?: { immediate?: boolean }) => {
      const next = { ...valuesRef.current, ...partial };
      valuesRef.current = next;
      setValues(next);

      const isValid = isValidRef.current;
      if (isValid && !isValid(next)) {
        clearTimer();
        setPendingInvalid(true);
        return;
      }
      setPendingInvalid(false);

      if (patchOpts?.immediate) {
        saveNow();
      } else {
        clearTimer();
        timerRef.current = setTimeout(saveNow, debounceMs);
      }
    },
    [clearTimer, debounceMs, saveNow],
  );

  const flush = React.useCallback(() => {
    if (timerRef.current !== null) {
      saveNow();
    }
  }, [saveNow]);

  const wrap = React.useCallback(
    async <R>(fn: () => Promise<R>): Promise<R | null> => {
      let result: R | null = null;
      await run(async () => {
        result = await fn();
      });
      return result;
    },
    [run],
  );

  const retry = React.useCallback(() => {
    const action = lastActionRef.current;
    if (action) void run(action);
  }, [run]);

  React.useEffect(() => () => clearTimer(), [clearTimer]);

  return { values, patch, flush, wrap, status, pendingInvalid, retry };
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/use-auto-save.test.tsx`
Expected: PASS(6 个用例全绿)。

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/org/use-auto-save.ts frontend/src/components/agentre/org/__tests__/use-auto-save.test.tsx
git commit frontend/src/components/agentre/org/use-auto-save.ts frontend/src/components/agentre/org/__tests__/use-auto-save.test.tsx -m "✨ org: useAutoSave hook(防抖/即时/flush/有效性守卫/重试/wrap)"
```

---

### Task 2: `AutoSaveStatus` 状态条 + i18n key

**Files:**
- Create: `frontend/src/components/agentre/org/auto-save-status.tsx`
- Test: `frontend/src/components/agentre/org/__tests__/auto-save-status.test.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`(`common` 块内)
- Modify: `frontend/src/i18n/locales/en/common.json`(`common` 块内)

**Interfaces:**
- Consumes: `AutoSaveStatus` 类型(来自 Task 1 的 `./use-auto-save`)。
- Produces: `function AutoSaveStatus(props: { status: AutoSaveStatus; pendingInvalid: boolean; onRetry: () => void }): JSX.Element`

- [ ] **Step 1: 加 i18n key(两个 locale 的 `common` 块)**

在 `frontend/src/i18n/locales/zh-CN/common.json` 的 `"common"` 对象里,`"reject": "拒绝",` 之后插入 `"retry": "重试",`;`"saved": "已保存",` 之后插入 `"saveFailed": "保存失败",`:

```json
    "reject": "拒绝",
    "retry": "重试",
    "save": "保存",
    "saved": "已保存",
    "saveFailed": "保存失败",
    "saving": "保存中...",
```

在 `frontend/src/i18n/locales/en/common.json` 的 `"common"` 对象里对应插入:

```json
    "reject": "Reject",
    "retry": "Retry",
    "save": "Save",
    "saved": "Saved",
    "saveFailed": "Save failed",
    "saving": "Saving...",
```

- [ ] **Step 2: 写失败测试**

创建 `frontend/src/components/agentre/org/__tests__/auto-save-status.test.tsx`:

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AutoSaveStatus } from "../auto-save-status";

describe("AutoSaveStatus", () => {
  it("shows saved by default and no retry button", () => {
    render(
      <AutoSaveStatus status="saved" pendingInvalid={false} onRetry={vi.fn()} />,
    );
    expect(screen.getByText("已保存")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("shows saving label while saving", () => {
    render(
      <AutoSaveStatus status="saving" pendingInvalid={false} onRetry={vi.fn()} />,
    );
    expect(screen.getByText("保存中...")).toBeInTheDocument();
  });

  it("shows unsaved label when a change is held invalid", () => {
    render(
      <AutoSaveStatus status="idle" pendingInvalid onRetry={vi.fn()} />,
    );
    expect(screen.getByText("未保存的修改")).toBeInTheDocument();
  });

  it("shows retry button on error and calls onRetry", () => {
    const onRetry = vi.fn();
    render(
      <AutoSaveStatus status="error" pendingInvalid={false} onRetry={onRetry} />,
    );
    expect(screen.getByText("保存失败")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});
```

> 说明:仓库的 vitest i18n 默认加载 zh-CN,故断言中文文案(与既有 org 组件测试一致)。

- [ ] **Step 3: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/auto-save-status.test.tsx`
Expected: FAIL —— `Cannot find module '../auto-save-status'`。

- [ ] **Step 4: 实现组件**

创建 `frontend/src/components/agentre/org/auto-save-status.tsx`:

```tsx
import { History } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";

import type { AutoSaveStatus as Status } from "./use-auto-save";

export function AutoSaveStatus({
  status,
  pendingInvalid,
  onRetry,
}: {
  status: Status;
  pendingInvalid: boolean;
  onRetry: () => void;
}) {
  const { t } = useTranslation();
  const label =
    status === "error"
      ? t("common.saveFailed")
      : status === "saving"
        ? t("common.saving")
        : pendingInvalid
          ? t("common.unsavedChanges")
          : t("common.saved");

  return (
    <footer className="flex items-center gap-2 border-t border-border bg-secondary/40 px-5 py-3">
      <span className="flex flex-1 items-center gap-1.5 font-mono text-2xs text-muted-foreground">
        <History className="size-3" aria-hidden="true" />
        {label}
      </span>
      {status === "error" && (
        <Button variant="outline" size="sm" onClick={onRetry}>
          {t("common.retry")}
        </Button>
      )}
    </footer>
  );
}
```

- [ ] **Step 5: 跑测试确认通过 + i18n 覆盖测试**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/auto-save-status.test.tsx src/__tests__/i18n.test.ts`
Expected: PASS(状态条 4 用例 + i18n 静态 key/覆盖测试全绿)。

- [ ] **Step 6: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/org/auto-save-status.tsx frontend/src/components/agentre/org/__tests__/auto-save-status.test.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit frontend/src/components/agentre/org/auto-save-status.tsx frontend/src/components/agentre/org/__tests__/auto-save-status.test.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json -m "✨ org: AutoSaveStatus 状态条 + common.saveFailed/retry i18n"
```

---

### Task 3: 部门详情接入自动保存

**Files:**
- Modify: `frontend/src/components/agentre/org/org-detail-department.tsx`
- Test: `frontend/src/components/agentre/org/__tests__/org-detail-department.test.tsx`

**Interfaces:**
- Consumes: `useAutoSave`(Task 1)、`AutoSaveStatus`(Task 2)。
- Produces: 无新增导出(组件对外 props 不变)。

改造要点(在 `OrgDetailDepartment` 组件内):

1. **删除**逐字段 `useState`(name/description/icon/accentColor/leadAgentId)、`dirty` 计算(第 91-97 行)、`handleSave`(第 99-115 行)。**保留** `parentId` 的 `useState`(它不进 payload)、`deletePromptOpen`、`strategy`。
2. **新增** `useAutoSave` 并解构 `values`,读取点不变:

```tsx
const {
  values,
  patch,
  flush,
  wrap,
  status,
  pendingInvalid,
  retry,
} = useAutoSave({
  initial: {
    name: props.department.name,
    description: props.department.description,
    icon: props.department.icon || "puzzle",
    accentColor: safeAgentColor(props.department.accentColor),
    leadAgentId: props.department.leadAgentId,
  },
  isValid: (v) => v.name.trim() !== "",
  save: (v) =>
    props.onUpdate({
      id: props.department.id,
      name: v.name,
      description: v.description,
      icon: v.icon,
      accentColor: v.accentColor,
      leadAgentId: v.leadAgentId,
    }),
});
const { name, description, icon, accentColor, leadAgentId } = values;
```

3. **setter 映射**(其余读取点如 `iconNode`/`selectedLead`/previews 全部不变):
   - name Input:`onChange={(e) => patch({ name: e.target.value })}` + `onBlur={flush}`
   - description Input:`onChange={(e) => patch({ description: e.target.value })}` + `onBlur={flush}`
   - IconPicker:`onChange={(v) => patch({ icon: v }, { immediate: true })}`
   - 颜色 radio 按钮:`onClick={() => patch({ accentColor: c }, { immediate: true })}`
   - 负责人 Select:`onValueChange={(v) => patch({ leadAgentId: Number(v) }, { immediate: true })}`
   - 父部门 Select:`onValueChange={(v) => { const p = Number(v); setParentId(p); void wrap(() => props.onMove({ id: props.department.id, newParentId: p, newSortOrder: 0 })); }}`
4. **页脚**:把第 515-526 行的 `<footer>...取消/保存...</footer>` 整块替换为
   `<AutoSaveStatus status={status} pendingInvalid={pendingInvalid} onRetry={retry} />`
5. **清理**:移除不再使用的 `History` 导入(状态条自带);`handleSave` 删除后确认没有悬空引用。`onClose` 仍由 header 的 X 使用,保留。

- [ ] **Step 1: 写失败测试(先改测试,红)**

在 `org-detail-department.test.tsx` 增补一个 describe(现有布局/删除用例保留;若有断言「保存/取消按钮存在」需一并更新为「不存在」):

```tsx
describe("OrgDetailDepartment auto-save", () => {
  const baseProps = (onUpdate = vi.fn().mockResolvedValue(undefined)) => ({
    department: dept({ id: 1, name: "开发组", parentId: 0, leadAgentId: 0 }),
    allDepartments: [dept({ id: 1, name: "开发组" })],
    allAgents: [],
    leadCandidates: [],
    onUpdate,
    onMove: vi.fn().mockResolvedValue(undefined),
    onDelete: vi.fn().mockResolvedValue(undefined),
    onSelect: vi.fn(),
    onClose: vi.fn(),
  });

  it("removes the cancel and save footer buttons", () => {
    render(<OrgDetailDepartment {...baseProps()} />);
    expect(screen.queryByRole("button", { name: "保存" })).toBeNull();
    expect(screen.queryByRole("button", { name: "取消" })).toBeNull();
  });

  it("saves the name on blur", () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    render(<OrgDetailDepartment {...baseProps(onUpdate)} />);
    const input = screen.getByLabelText("部门名称");
    fireEvent.change(input, { target: { value: "开发二组" } });
    fireEvent.blur(input);
    expect(onUpdate).toHaveBeenCalledWith(
      expect.objectContaining({ id: 1, name: "开发二组" }),
    );
  });

  it("saves immediately when a theme color is picked", () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    render(<OrgDetailDepartment {...baseProps(onUpdate)} />);
    fireEvent.click(
      screen.getByRole("radio", { name: /agent-5/ }),
    );
    expect(onUpdate).toHaveBeenCalledWith(
      expect.objectContaining({ accentColor: "agent-5" }),
    );
  });
});
```

> `部门名称` = `org.department.name` 的 zh-CN 文案;`org.department.themeColorNamed` 会把 `{{color}}` 渲进 radio 的 aria-label(如 `agent-5`),故用正则匹配。运行前先确认这两个 key 的实际中文,必要时对齐断言。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-detail-department.test.tsx`
Expected: FAIL —— 「保存/取消按钮仍存在」或 `onUpdate` 未被调用(此时组件还是旧的按钮式保存)。

- [ ] **Step 3: 按上面「改造要点」实现组件改造**

按本任务开头 1-5 条修改 `org-detail-department.tsx`。关键片段示例:

```tsx
// 顶部导入
import { useAutoSave } from "./use-auto-save";
import { AutoSaveStatus } from "./auto-save-status";

// name Input
<Input
  value={name}
  onChange={(e) => patch({ name: e.target.value })}
  onBlur={flush}
  aria-label={t("org.department.name")}
/>

// 颜色 radio
onClick={() => patch({ accentColor: c }, { immediate: true })}

// 父部门 Select
onValueChange={(v) => {
  const p = Number(v);
  setParentId(p);
  void wrap(() =>
    props.onMove({ id: props.department.id, newParentId: p, newSortOrder: 0 }),
  );
}}

// 页脚
<AutoSaveStatus
  status={status}
  pendingInvalid={pendingInvalid}
  onRetry={retry}
/>
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-detail-department.test.tsx`
Expected: PASS(含新 auto-save 用例 + 原布局/删除用例)。

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/org/org-detail-department.tsx frontend/src/components/agentre/org/__tests__/org-detail-department.test.tsx
git commit frontend/src/components/agentre/org/org-detail-department.tsx frontend/src/components/agentre/org/__tests__/org-detail-department.test.tsx -m "✨ org: 部门详情接入自动保存(去取消/保存,防抖+失焦存,父级 onMove 走 wrap)"
```

---

### Task 4: 智能体详情接入自动保存

**Files:**
- Modify: `frontend/src/components/agentre/org/org-detail-agent.tsx`
- Test: `frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx`

**Interfaces:**
- Consumes: `useAutoSave`(Task 1)、`AutoSaveStatus`(Task 2)。
- Produces: 无新增导出。

改造要点(在 `OrgDetailAgent` 组件内):

1. **删除**逐字段 `useState`(name/description/avatarColor/avatarIcon/backendId/prompt/skills/tools,第 93-120 行)与 `handleSave`(第 142-156 行)。保留 `deletePromptOpen`/`skillPickerOpen`/`toolPickerOpen` 等 UI 局部态。
2. **新增** `useAutoSave` 并解构 `values`(读取点不变):

```tsx
const {
  values,
  patch,
  flush,
  wrap,
  status,
  pendingInvalid,
  retry,
} = useAutoSave({
  initial: {
    name: props.agent.name,
    description: props.agent.description,
    avatarColor: safeAgentColor(props.agent.avatarColor),
    avatarIcon: props.agent.avatarIcon || "",
    backendId: props.agent.agentBackendId,
    prompt: (props.agent.prompt ?? []).join("\n"),
    skills: (props.agent.skills ?? []).map((s) => ({ ...s })),
    tools: ((): department_svc.AgentToolDTO[] => {
      const cur = new Map(
        (props.agent.tools ?? []).map((tl) => [tl.key, tl.enabled]),
      );
      return (props.availableTools ?? []).map((key) => ({
        key,
        enabled: cur.get(key) ?? false,
      }));
    })(),
  },
  isValid: (v) => v.name.trim() !== "",
  save: (v) =>
    props.onUpdate(
      agent_svc.UpdateAgentRequest.createFrom({
        id: props.agent.id,
        name: v.name,
        description: v.description,
        avatarColor: v.avatarColor,
        avatarIcon: v.avatarIcon,
        agentBackendId: v.backendId,
        prompt: v.prompt.split("\n").filter((s) => s.trim() !== ""),
        skills: v.skills,
        tools: v.tools,
      }),
    ),
});
const { name, description, avatarColor, avatarIcon, backendId, prompt, skills, tools } =
  values;
```

3. **setter 映射**(其余读取点如 `selectedBackend`/`toolChips`/`skillChips`/`promptCharCount` 全部不变):
   - name Input(第 357-359):`onChange={(e) => patch({ name: e.target.value })}` + `onBlur={flush}`
   - description Input(第 372-374):`onChange={(e) => patch({ description: e.target.value })}` + `onBlur={flush}`
   - prompt Textarea(第 497-499):`onChange={(e) => patch({ prompt: e.target.value })}` + `onBlur={flush}`
   - 头像 IconPicker `onChangeIcon`(第 282/388):`(v) => patch({ avatarIcon: v }, { immediate: true })`
   - 头像颜色 `onClick`(第 423):`() => patch({ avatarColor: c }, { immediate: true })`
   - backend Select(第 441):`onValueChange={(v) => patch({ backendId: Number(v) }, { immediate: true })}`
   - `toggleToolGrant`(第 188-191)→ `patch({ tools: tools.map((tl) => (tl.key === key ? { ...tl, enabled: !tl.enabled } : tl)) }, { immediate: true })`
   - `removeTool`(第 192-195)→ `patch({ tools: tools.map((tl) => (tl.key === key ? { ...tl, enabled: false } : tl)) }, { immediate: true })`
   - `setSkillState`(第 217-225)→ 用当前 `skills` 计算 `next` 后 `patch({ skills: next }, { immediate: true })`:
     ```tsx
     const setSkillState = (id: string, next: TriState) => {
       const rest = skills.filter((s) => s.id !== id);
       const nextSkills =
         next === "inherit"
           ? rest
           : [
               ...rest,
               department_svc.AgentSkillDTO.createFrom({
                 id,
                 enabled: next === "on",
               }),
             ];
       patch({ skills: nextSkills }, { immediate: true });
     };
     ```
   - `removeSkillOverride`(第 268-269)→ `patch({ skills: skills.filter((s) => s.id !== id) }, { immediate: true })`
4. **头像上传/删除**(第 158-170 的 `handleUploadFile` / `handleDeleteAvatar`)改走 `wrap`,喂同一状态条:
   ```tsx
   await wrap(() => props.onUploadAvatar({ id: props.agent.id, dataUrl }));
   // ...
   await wrap(() => props.onDeleteAvatar({ id: props.agent.id }));
   ```
5. **页脚**:把第 607-622 行的 `<footer>...取消/保存...</footer>` 整块替换为
   `<AutoSaveStatus status={status} pendingInvalid={pendingInvalid} onRetry={retry} />`。
6. **清理**:移除因 `handleSave` 删除而不再使用的导入/变量(如 `History` 若原本在此文件导入)。`onClose` 仍由 header X 使用,保留。

- [ ] **Step 1: 写失败测试(先改测试,红)**

在 `org-detail-agent.test.tsx` 增补(现有用例保留;若有「保存/取消按钮存在」断言改为「不存在」)。先读文件头部已有的 props 工厂/mock 约定并复用,示例:

```tsx
describe("OrgDetailAgent auto-save", () => {
  it("removes the cancel and save footer buttons", () => {
    // 复用文件已有的 renderAgentDetail / props 工厂
    renderAgentDetail();
    expect(screen.queryByRole("button", { name: "保存" })).toBeNull();
    expect(screen.queryByRole("button", { name: "取消" })).toBeNull();
  });

  it("saves the name on blur", () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    renderAgentDetail({ onUpdate });
    const input = screen.getByLabelText(/名称/);
    fireEvent.change(input, { target: { value: "Boris 二号" } });
    fireEvent.blur(input);
    expect(onUpdate).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Boris 二号" }),
    );
  });
});
```

> 若该测试文件当前没有可复用的 `renderAgentDetail` / props 工厂,则按文件已有的 `render(<OrgDetailAgent .../>)` 调用方式补齐必要 props(参考文件顶部)。`名称` 用 `org.agent.*` 的实际 zh-CN 文案,`getByLabelText` 用正则容错。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-detail-agent.test.tsx`
Expected: FAIL —— 按钮仍在 / `onUpdate` 未按 blur 触发。

- [ ] **Step 3: 按「改造要点」实现组件改造**

按本任务开头 1-6 条修改 `org-detail-agent.tsx`,顶部加:

```tsx
import { useAutoSave } from "./use-auto-save";
import { AutoSaveStatus } from "./auto-save-status";
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-detail-agent.test.tsx`
Expected: PASS(新 auto-save 用例 + 原有用例)。

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/org/org-detail-agent.tsx frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx
git commit frontend/src/components/agentre/org/org-detail-agent.tsx frontend/src/components/agentre/org/__tests__/org-detail-agent.test.tsx -m "✨ org: 智能体详情接入自动保存(去取消/保存,文本防抖+失焦,头像上传走 wrap)"
```

---

### Task 5: 全量 gate(类型 / lint / 全量测试)

**Files:** 无新增(收尾验证)。

> 依据仓库经验:per-file focused 测试会漏跨文件类型/eslint/i18n 覆盖问题;收尾必须跑全量并看真 exit code。

- [ ] **Step 1: 类型检查**

Run: `cd frontend && pnpm exec tsc --noEmit`
Expected: 无错误退出(exit 0)。如报未使用导入(如 `History`),按报错清理对应文件后重跑。

- [ ] **Step 2: ESLint(含 i18next/no-literal-string)**

Run: `cd /Users/codfrm/Code/agentre/agentre && make lint`
Expected: 前端 ESLint 通过(无硬编码中文、无未用变量);golangci 无本次相关改动。

- [ ] **Step 3: 全量前端测试**

Run: `cd frontend && pnpm test`
Expected: 全量 vitest 全绿(尤其 `i18n.test.ts`、`eslint-i18n.test.ts` 与三个 org 测试文件)。查看真实 exit code,不要用会吞退出码的管道。

- [ ] **Step 4: 若前面步骤有修复,补一次提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
# 仅在 Step 1-3 产生了额外改动时执行,带 pathspec 提交具体文件
git commit <changed-files> -m "🎨 org: 自动保存收尾(类型/lint/i18n 全量 gate 修复)"
```

---

## Self-Review

**Spec coverage:**
- 范围=两个表单 → Task 3(部门)+ Task 4(智能体)。✅
- 文本防抖 600ms + 失焦立即存 → `useAutoSave` debounce + `flush`(Task 1),两个表单 name/description(/prompt)`onChange`+`onBlur`(Task 3/4)。✅
- 离散选择即时存 → `patch(..., { immediate:true })`(icon/color/select/skills/tools)。✅
- 名称为空暂不保存 → `isValid`(Task 1)+ 两表单传 `isValid: name 非空`。✅
- 状态条(保存中/已保存/未保存/保存失败[重试])→ `AutoSaveStatus`(Task 2)。✅
- 部门父级走 onMove、智能体头像走独立 mutation → `wrap`(Task 1)+ 接线(Task 3/4)。✅
- 新增 i18n `common.saveFailed`/`common.retry`(zh+en)→ Task 2 Step 1。✅
- 不动后端 / `use-org-data` / 删除弹窗 / 拖拽 → 未列入任何 Modify。✅

**Placeholder scan:** 无 TBD/TODO;每个代码步骤含完整代码;测试步骤含真实断言。✅

**Type consistency:** `useAutoSave` 返回 `{ values, patch, flush, wrap, status, pendingInvalid, retry }` 在 Task 1 定义,Task 2/3/4 一致引用;`AutoSaveStatus` props `{ status, pendingInvalid, onRetry }` 三处一致;`patch(partial, { immediate })` 签名各调用点一致。✅
