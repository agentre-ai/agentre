# Tab 拖拽排序 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 chat tab 列表从"系统三区自动排序"切换到"用户手动拖拽排序",保留 attention 首次进入时一次性置顶到 pinned 前缀之后的提示行为。

**Architecture:** `selectSortedTabs` 删除,`tabs` 数组顺序即显示顺序。`moveTab` action 落地拖拽结果并 normalize pinned 状态(拖出 pinned 前缀自动 unpin)。`useAttentionBump` hook 监听 attention 状态边沿,边沿触发时调 `bumpToAfterPinned` 把 tab 搬到 pinned 前缀之后。TabStrip 接入 `@dnd-kit/sortable` 提供拖拽 UI。

**Tech Stack:** React 19 + TypeScript + Zustand + `@dnd-kit/core` + `@dnd-kit/sortable`(都已安装) + Vitest + @testing-library/react。

**Spec:** `docs/superpowers/specs/2026-05-27-tab-drag-reorder-design.md`

---

## 文件结构总览

| 文件 | 操作 | 责任 |
|---|---|---|
| `frontend/src/stores/chat-tabs-store.ts` | 修改 | 加 `moveTab` / `bumpToAfterPinned`;`togglePin` pin 时自动归位 |
| `frontend/src/stores/__tests__/chat-tabs-store.test.ts` | 修改 | 加上述 3 个新行为的用例 |
| `frontend/src/stores/chat-tabs-store-selectors.ts` | 删除 | 整文件不再需要 |
| `frontend/src/stores/__tests__/chat-tabs-store-selectors.test.ts` | 删除 | 同上 |
| `frontend/src/components/agentre/chat-tabs/use-tabs-view.ts` | 修改 | 直接 map `tabs` 数组,删 `zone` 字段、`TabZone` 类型导出 |
| `frontend/src/components/agentre/chat-tabs/use-attention-bump.ts` | 新建 | attention 边沿 → `bumpToAfterPinned` |
| `frontend/src/components/agentre/chat-tabs/__tests__/use-attention-bump.test.ts` | 新建 | 边沿触发、持续不重复、true→false→true 重新触发 |
| `frontend/src/components/agentre/chat-tabs/tab-strip.tsx` | 修改 | 接入 `DndContext` + `SortableContext`,删 zone-divider 渲染 |
| `frontend/src/components/agentre/chat-tabs/__tests__/tab-strip.test.tsx` | 修改 | 删 zone-divider 断言;加键盘拖拽用例 |

后端 / Wails / SQL / Go 完全无影响。

---

## Task 1: store 新增 `moveTab` action(纯 reorder,不动 isPinned)

**Files:**
- Modify: `frontend/src/stores/chat-tabs-store.ts`
- Test: `frontend/src/stores/__tests__/chat-tabs-store.test.ts`

- [ ] **Step 1: 写失败测试**

在 `chat-tabs-store.test.ts` 末尾追加:

```ts
describe("chat-tabs-store · moveTab", () => {
  beforeEach(() => {
    useChatTabsStore.setState({ tabs: [], activeTabId: null });
    let nid = 1;
    __setNextIdFactoryForTesting(() => `t${nid++}`);
    __setNowForTesting(() => 1000);
  });

  it("把 index 0 的 tab 移到 index 2, 数组顺序更新, isPinned 不变", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    useChatTabsStore.getState().openSessionInNewTab(2);
    useChatTabsStore.getState().openSessionInNewTab(3);
    useChatTabsStore.getState().moveTab(0, 2);
    expect(
      useChatTabsStore.getState().tabs.map((t) => (t.meta as { sessionId: number }).sessionId),
    ).toEqual([2, 3, 1]);
  });

  it("from === to 时无副作用, 不改 state 引用", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    useChatTabsStore.getState().openSessionInNewTab(2);
    const before = useChatTabsStore.getState().tabs;
    useChatTabsStore.getState().moveTab(1, 1);
    expect(useChatTabsStore.getState().tabs).toBe(before);
  });

  it("越界 from/to 不抛, state 不变", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    const before = useChatTabsStore.getState().tabs;
    useChatTabsStore.getState().moveTab(5, 0);
    useChatTabsStore.getState().moveTab(0, 5);
    expect(useChatTabsStore.getState().tabs).toBe(before);
  });
});
```

- [ ] **Step 2: 跑测试看红**

```bash
cd frontend && pnpm test -- src/stores/__tests__/chat-tabs-store.test.ts
```

Expected: 3 个 `moveTab` 用例 FAIL,报 `moveTab is not a function`。

- [ ] **Step 3: 实现 `moveTab`**

在 `chat-tabs-store.ts` 的 `Actions` 类型里加:

```ts
moveTab: (fromIndex: number, toIndex: number) => void;
```

在 `useChatTabsStore` 的 actions 实现里加(`setActive` 之后插入):

```ts
moveTab: (fromIndex, toIndex) =>
  set((state) => {
    const len = state.tabs.length;
    if (
      fromIndex < 0 ||
      fromIndex >= len ||
      toIndex < 0 ||
      toIndex >= len ||
      fromIndex === toIndex
    ) {
      return state;
    }
    const tabs = [...state.tabs];
    const [moved] = tabs.splice(fromIndex, 1);
    tabs.splice(toIndex, 0, moved);

    // normalize: 如果被搬的 tab 原本 isPinned 但搬到 pinned 前缀之外, 取消 pin。
    let lastPinnedPrefixIndex = -1;
    for (let i = 0; i < tabs.length; i++) {
      if (tabs[i].isPinned) lastPinnedPrefixIndex = i;
      else break;
    }
    const finalIdx = tabs.indexOf(moved);
    if (moved.isPinned && finalIdx > lastPinnedPrefixIndex) {
      tabs[finalIdx] = { ...moved, isPinned: false, pinAt: 0 };
    }
    return { tabs };
  }),
```

- [ ] **Step 4: 跑测试看绿**

```bash
cd frontend && pnpm test -- src/stores/__tests__/chat-tabs-store.test.ts -t "moveTab"
```

Expected: 3 个用例都 PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/stores/chat-tabs-store.ts frontend/src/stores/__tests__/chat-tabs-store.test.ts
git commit -m "✨ feat(chat-tabs): add moveTab action for drag reorder"
```

---

## Task 2: `moveTab` normalize —— 拖出 pinned 前缀自动 unpin

**Files:**
- Test: `frontend/src/stores/__tests__/chat-tabs-store.test.ts`

(实现已经在 Task 1 一并完成,这一步只补 normalize 的专项用例。)

- [ ] **Step 1: 写失败测试**

在 `describe("chat-tabs-store · moveTab", ...)` 里追加:

```ts
it("拖动 pinned tab 到 pinned 前缀之外, isPinned 自动变 false 且 pinAt 清零", () => {
  // tabs = [P1, P2, X, Y, Z],把 P2 拖到 index 4
  useChatTabsStore.getState().openSessionInNewTab(1);
  useChatTabsStore.getState().openSessionInNewTab(2);
  useChatTabsStore.getState().openSessionInNewTab(3);
  useChatTabsStore.getState().openSessionInNewTab(4);
  useChatTabsStore.getState().openSessionInNewTab(5);
  const t1Id = useChatTabsStore.getState().tabs[0].id;
  const t2Id = useChatTabsStore.getState().tabs[1].id;
  useChatTabsStore.getState().togglePin(t1Id);
  useChatTabsStore.getState().togglePin(t2Id);

  // 此时 tabs = [P1, P2, X, Y, Z], pinned 前缀末端 = 1。
  // 拖 P2 (index 1) 到 index 4。
  useChatTabsStore.getState().moveTab(1, 4);
  const tabs = useChatTabsStore.getState().tabs;
  const moved = tabs[4];
  expect((moved.meta as { sessionId: number }).sessionId).toBe(2);
  expect(moved.isPinned).toBe(false);
  expect(moved.pinAt).toBe(0);
  // P1 仍然在 index 0, 仍 pinned
  expect(tabs[0].isPinned).toBe(true);
});

it("拖动 pinned tab 在 pinned 前缀内换位, 保持 pinned", () => {
  useChatTabsStore.getState().openSessionInNewTab(1);
  useChatTabsStore.getState().openSessionInNewTab(2);
  useChatTabsStore.getState().openSessionInNewTab(3);
  const t1Id = useChatTabsStore.getState().tabs[0].id;
  const t2Id = useChatTabsStore.getState().tabs[1].id;
  const t3Id = useChatTabsStore.getState().tabs[2].id;
  useChatTabsStore.getState().togglePin(t1Id);
  useChatTabsStore.getState().togglePin(t2Id);
  useChatTabsStore.getState().togglePin(t3Id);
  // tabs = [P1, P2, P3], 全 pinned。拖 P1 → index 2。
  useChatTabsStore.getState().moveTab(0, 2);
  const tabs = useChatTabsStore.getState().tabs;
  expect(tabs.map((t) => t.isPinned)).toEqual([true, true, true]);
  expect((tabs[2].meta as { sessionId: number }).sessionId).toBe(1);
});

it("拖动非 pinned tab 进入 pinned 前缀, 不自动 pin", () => {
  useChatTabsStore.getState().openSessionInNewTab(1);
  useChatTabsStore.getState().openSessionInNewTab(2);
  useChatTabsStore.getState().openSessionInNewTab(3);
  const t1Id = useChatTabsStore.getState().tabs[0].id;
  useChatTabsStore.getState().togglePin(t1Id);
  // tabs = [P1, X2, X3]。拖 X3 (index 2) 到 index 0。
  useChatTabsStore.getState().moveTab(2, 0);
  const tabs = useChatTabsStore.getState().tabs;
  expect(tabs[0].isPinned).toBe(false);
  expect((tabs[0].meta as { sessionId: number }).sessionId).toBe(3);
});
```

- [ ] **Step 2: 跑测试看绿**

```bash
cd frontend && pnpm test -- src/stores/__tests__/chat-tabs-store.test.ts -t "moveTab"
```

Expected: 6 个用例(原 3 + 新 3)都 PASS。Task 1 实现已经覆盖 normalize 逻辑,这里只是补 spec。

- [ ] **Step 3: 提交**

```bash
git add frontend/src/stores/__tests__/chat-tabs-store.test.ts
git commit -m "✅ test(chat-tabs): cover moveTab pin/unpin normalization"
```

---

## Task 3: store 新增 `bumpToAfterPinned`(系统行为,不动 isPinned)

**Files:**
- Modify: `frontend/src/stores/chat-tabs-store.ts`
- Test: `frontend/src/stores/__tests__/chat-tabs-store.test.ts`

- [ ] **Step 1: 写失败测试**

在 `chat-tabs-store.test.ts` 末尾追加:

```ts
describe("chat-tabs-store · bumpToAfterPinned", () => {
  beforeEach(() => {
    useChatTabsStore.setState({ tabs: [], activeTabId: null });
    let nid = 1;
    __setNextIdFactoryForTesting(() => `t${nid++}`);
    __setNowForTesting(() => 1000);
  });

  it("把指定 tab 搬到 lastPinnedPrefixIndex + 1, 不动 isPinned", () => {
    // tabs = [P1, X2, X3, X4]
    useChatTabsStore.getState().openSessionInNewTab(1);
    useChatTabsStore.getState().openSessionInNewTab(2);
    useChatTabsStore.getState().openSessionInNewTab(3);
    useChatTabsStore.getState().openSessionInNewTab(4);
    const t1Id = useChatTabsStore.getState().tabs[0].id;
    const t4Id = useChatTabsStore.getState().tabs[3].id;
    useChatTabsStore.getState().togglePin(t1Id);
    // bump X4 (index 3) → 应该到 index 1
    useChatTabsStore.getState().bumpToAfterPinned(t4Id);
    const tabs = useChatTabsStore.getState().tabs;
    expect((tabs[1].meta as { sessionId: number }).sessionId).toBe(4);
    expect(tabs[1].isPinned).toBe(false);
  });

  it("没有 pinned 时, 搬到 index 0", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    useChatTabsStore.getState().openSessionInNewTab(2);
    useChatTabsStore.getState().openSessionInNewTab(3);
    const t3Id = useChatTabsStore.getState().tabs[2].id;
    useChatTabsStore.getState().bumpToAfterPinned(t3Id);
    expect(
      (useChatTabsStore.getState().tabs[0].meta as { sessionId: number }).sessionId,
    ).toBe(3);
  });

  it("tab 已在目标位置时无副作用", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    useChatTabsStore.getState().openSessionInNewTab(2);
    const t1Id = useChatTabsStore.getState().tabs[0].id;
    const before = useChatTabsStore.getState().tabs;
    useChatTabsStore.getState().bumpToAfterPinned(t1Id);
    expect(useChatTabsStore.getState().tabs).toBe(before);
  });

  it("未知 id 不抛, state 不变", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    const before = useChatTabsStore.getState().tabs;
    useChatTabsStore.getState().bumpToAfterPinned("nope");
    expect(useChatTabsStore.getState().tabs).toBe(before);
  });
});
```

- [ ] **Step 2: 跑测试看红**

```bash
cd frontend && pnpm test -- src/stores/__tests__/chat-tabs-store.test.ts -t "bumpToAfterPinned"
```

Expected: 4 用例都 FAIL,报 `bumpToAfterPinned is not a function`。

- [ ] **Step 3: 实现 `bumpToAfterPinned`**

`chat-tabs-store.ts` 的 `Actions` 加:

```ts
bumpToAfterPinned: (id: string) => void;
```

在 actions 实现里(`moveTab` 之后)加:

```ts
bumpToAfterPinned: (id) =>
  set((state) => {
    const idx = state.tabs.findIndex((t) => t.id === id);
    if (idx < 0) return state;
    let lastPinnedPrefixIndex = -1;
    for (let i = 0; i < state.tabs.length; i++) {
      if (state.tabs[i].isPinned) lastPinnedPrefixIndex = i;
      else break;
    }
    const target = lastPinnedPrefixIndex + 1;
    if (idx === target) return state;
    const tabs = [...state.tabs];
    const [moved] = tabs.splice(idx, 1);
    // 注意: 如果 idx < target, splice 后 target 要 -1
    const insertAt = idx < target ? target - 1 : target;
    tabs.splice(insertAt, 0, moved);
    return { tabs };
  }),
```

- [ ] **Step 4: 跑测试看绿**

```bash
cd frontend && pnpm test -- src/stores/__tests__/chat-tabs-store.test.ts -t "bumpToAfterPinned"
```

Expected: 4 用例都 PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/stores/chat-tabs-store.ts frontend/src/stores/__tests__/chat-tabs-store.test.ts
git commit -m "✨ feat(chat-tabs): add bumpToAfterPinned for attention surfacing"
```

---

## Task 4: `togglePin` 在 pin 时自动归位到 pinned 前缀末端

**Files:**
- Modify: `frontend/src/stores/chat-tabs-store.ts`
- Test: `frontend/src/stores/__tests__/chat-tabs-store.test.ts`

之所以加这个,是因为删了 `selectSortedTabs` 之后,如果 togglePin 不归位,会出现"用户右键置顶,tab 还在原地"——视觉上 pinned 跑到了中间,违反"pinned 是连续前缀"的约定。

- [ ] **Step 1: 写失败测试**

在 `chat-tabs-store.test.ts` 找到 `describe("chat-tabs-store · togglePin", ...)`(没有就在 moveTab describe 之后建一个);加用例:

```ts
describe("chat-tabs-store · togglePin", () => {
  beforeEach(() => {
    useChatTabsStore.setState({ tabs: [], activeTabId: null });
    let nid = 1;
    __setNextIdFactoryForTesting(() => `t${nid++}`);
    __setNowForTesting(() => 1000);
  });

  it("pin 一个中间位置的 tab, 自动搬到 pinned 前缀末端", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    useChatTabsStore.getState().openSessionInNewTab(2);
    useChatTabsStore.getState().openSessionInNewTab(3);
    const t1Id = useChatTabsStore.getState().tabs[0].id;
    const t3Id = useChatTabsStore.getState().tabs[2].id;
    useChatTabsStore.getState().togglePin(t1Id);
    // tabs = [P1, X2, X3]. Pin t3 → 应到 index 1, tabs = [P1, P3, X2]
    useChatTabsStore.getState().togglePin(t3Id);
    const tabs = useChatTabsStore.getState().tabs;
    expect(tabs.map((t) => (t.meta as { sessionId: number }).sessionId)).toEqual([1, 3, 2]);
    expect(tabs.map((t) => t.isPinned)).toEqual([true, true, false]);
  });

  it("unpin 时位置不动", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    useChatTabsStore.getState().openSessionInNewTab(2);
    const t1Id = useChatTabsStore.getState().tabs[0].id;
    useChatTabsStore.getState().togglePin(t1Id);
    useChatTabsStore.getState().togglePin(t1Id); // unpin
    const tabs = useChatTabsStore.getState().tabs;
    // 原位置 index 0, unpin 后还是 index 0
    expect((tabs[0].meta as { sessionId: number }).sessionId).toBe(1);
    expect(tabs[0].isPinned).toBe(false);
  });
});
```

- [ ] **Step 2: 跑测试看红**

```bash
cd frontend && pnpm test -- src/stores/__tests__/chat-tabs-store.test.ts -t "togglePin"
```

Expected: 第 1 个新用例 FAIL("auto-relocate" 还没实现);第 2 个可能已经 PASS。

- [ ] **Step 3: 改实现**

`chat-tabs-store.ts` 找到 `togglePin: (id) => set((state) => { ... })`,改成:

```ts
togglePin: (id) =>
  set((state) => {
    const idx = state.tabs.findIndex((t) => t.id === id);
    if (idx < 0) return state;
    const cur = state.tabs[idx];
    if (cur.isPinned) {
      // unpin: 位置不动
      const tabs = [...state.tabs];
      tabs[idx] = { ...cur, isPinned: false, pinAt: 0 };
      return { tabs };
    }
    // pin: 搬到 pinned 前缀末端
    let lastPinnedPrefixIndex = -1;
    for (let i = 0; i < state.tabs.length; i++) {
      if (state.tabs[i].isPinned) lastPinnedPrefixIndex = i;
      else break;
    }
    const pinned: ChatTab = {
      ...cur,
      isPinned: true,
      pinAt: now(),
      isPreview: false,
    };
    const target = lastPinnedPrefixIndex + 1;
    const tabs = [...state.tabs];
    tabs.splice(idx, 1);
    const insertAt = idx < target ? target - 1 : target;
    tabs.splice(insertAt, 0, pinned);
    return { tabs };
  }),
```

- [ ] **Step 4: 跑测试看绿**

```bash
cd frontend && pnpm test -- src/stores/__tests__/chat-tabs-store.test.ts
```

Expected: 整个文件所有用例 PASS。(注意:原来 `togglePin` 的旧用例不应破坏,需要确认。如果旧 `togglePin` 用例预期"位置不变",会被这一步打破——必要时同步更新旧用例。)

- [ ] **Step 5: 提交**

```bash
git add frontend/src/stores/chat-tabs-store.ts frontend/src/stores/__tests__/chat-tabs-store.test.ts
git commit -m "✨ feat(chat-tabs): relocate tab to pinned prefix end on pin"
```

---

## Task 5: 删除 `selectSortedTabs` 及其测试,改 `useTabsView` 直接读 `tabs`

**Files:**
- Delete: `frontend/src/stores/chat-tabs-store-selectors.ts`
- Delete: `frontend/src/stores/__tests__/chat-tabs-store-selectors.test.ts`
- Modify: `frontend/src/components/agentre/chat-tabs/use-tabs-view.ts`

- [ ] **Step 1: 改 `use-tabs-view.ts`**

把 `use-tabs-view.ts` 改成:

```ts
// frontend/src/components/agentre/chat-tabs/use-tabs-view.ts
import * as React from "react";

import { useProjectTree } from "@/hooks/use-project-tree";
import { reasonToPillText } from "@/lib/attention-display";
import { findProjectColorToken, projectChain } from "@/lib/project-chain";
import {
  useSessionAttentionList,
  type AttentionReason,
} from "@/stores/attention-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import type { AgentColor } from "../types";
import { agentColorOrder } from "../types";
import type { TabStatus } from "./tab";

export type TabView = {
  id: string;
  title: string;
  avatar: { letter: string; color: string };
  isPreview: boolean;
  isPinned: boolean;
  status: TabStatus;
  projectColor: string | null;
  worktree: boolean;
  pillText: string | null;
  sessionId: number;
  projectChain: string[] | null;
  worktreeBranch: string | null;
  lastMessageAt: number;
};

const AGENT_COLOR_SET = new Set<string>(agentColorOrder);

function tokenToCssColor(token: string | null | undefined): string | null {
  if (!token) return null;
  if (!AGENT_COLOR_SET.has(token as AgentColor)) return null;
  return `var(--${token})`;
}

function firstLetter(name: string | null | undefined): string {
  if (!name) return "?";
  const trimmed = name.trim();
  if (!trimmed) return "?";
  return Array.from(trimmed)[0] ?? "?";
}

export function useTabsView(): TabView[] {
  const tabs = useChatTabsStore((s) => s.tabs);
  const statuses = useSessionStatusStore((s) => s.statuses);
  const metas = useSessionMetaStore((s) => s.metas);
  const { tree } = useProjectTree();

  const sessionTabIds = React.useMemo(
    () =>
      tabs
        .filter((t) => t.meta.kind === "session")
        .map(
          (t) => (t.meta as { kind: "session"; sessionId: number }).sessionId,
        ),
    [tabs],
  );
  const attentionItems = useSessionAttentionList(sessionTabIds);
  const attentionBySid = React.useMemo(() => {
    const m = new Map<number, AttentionReason>();
    for (const x of attentionItems) m.set(x.sessionId, x.reason);
    return m;
  }, [attentionItems]);

  return tabs.map((t) => {
    const sid = t.meta.kind === "session" ? t.meta.sessionId : 0;
    const live = sid ? statuses.get(sid) : undefined;
    const reason = sid ? (attentionBySid.get(sid) ?? null) : null;
    const agentStatus = live?.agentStatus;
    const status: TabStatus =
      agentStatus === "running"
        ? "running"
        : agentStatus === "waiting"
          ? "waiting"
          : agentStatus === "error"
            ? "error"
            : "idle";
    const pillText = reasonToPillText(reason);
    const meta = sid ? (metas.get(sid) ?? null) : null;
    const avatarColor = tokenToCssColor(meta?.agentColor) ?? "#94a3b8";
    const avatarLetter = firstLetter(meta?.agentName);
    const pid = meta?.projectId ?? 0;
    const projectColor =
      pid > 0 ? tokenToCssColor(findProjectColorToken(tree, pid)) : null;
    const chain = pid > 0 ? projectChain(tree, pid) : [];
    return {
      id: t.id,
      title: meta?.title ?? t.title ?? "(会话)",
      avatar: { letter: avatarLetter, color: avatarColor },
      isPreview: t.isPreview,
      isPinned: t.isPinned,
      status,
      projectColor,
      worktree: false,
      pillText,
      sessionId: sid,
      projectChain: chain.length > 0 ? chain : null,
      worktreeBranch: null,
      lastMessageAt: meta?.lastMessageAt ?? 0,
    };
  });
}
```

变化:删除 `TabZone` 类型导出、`zone` 字段、`selectSortedTabs` 调用、`attentionTabIds`(Task 6 在 hook 里另算)。

- [ ] **Step 2: 删除 selector 文件 + 测试**

```bash
rm frontend/src/stores/chat-tabs-store-selectors.ts
rm frontend/src/stores/__tests__/chat-tabs-store-selectors.test.ts
```

- [ ] **Step 3: 跑 typecheck + test**

```bash
cd frontend && pnpm test
```

Expected: 现在会有几处编译错误——`tab-strip.tsx` 引用了 `TabZone` / `zone` / 旧 sortedTabs;`tab-strip.test.tsx` 仍然测 zone-divider。这些会在 Task 7 / Task 8 修。本步先把 store / view 自己改完跑通,失败先标记继续。如果别的文件引用了 `selectSortedTabs` / `TabZone`(grep 确认),也在这步顺带删 import。

```bash
cd /Users/codfrm/Code/agentre/agentre && /Users/codfrm/Code/agentre/agentre/...  # 用 grep tool 查残留
```

(用 Grep tool 搜 `selectSortedTabs`、`TabZone`,确认只剩 `tab-strip.tsx`/`tab-strip.test.tsx` 两处残留 —— 它们会在后续 task 处理。)

- [ ] **Step 4: 提交**

```bash
git add frontend/src/components/agentre/chat-tabs/use-tabs-view.ts
git rm frontend/src/stores/chat-tabs-store-selectors.ts frontend/src/stores/__tests__/chat-tabs-store-selectors.test.ts
git commit -m "♻️ refactor(chat-tabs): drop zone-based sort, use array order"
```

---

## Task 6: 新建 `useAttentionBump` hook

**Files:**
- Create: `frontend/src/components/agentre/chat-tabs/use-attention-bump.ts`
- Test: `frontend/src/components/agentre/chat-tabs/__tests__/use-attention-bump.test.ts`

- [ ] **Step 1: 写失败测试**

新建 `frontend/src/components/agentre/chat-tabs/__tests__/use-attention-bump.test.ts`:

```ts
import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useChatTabsStore } from "@/stores/chat-tabs-store";

import { useAttentionBump } from "../use-attention-bump";

describe("useAttentionBump", () => {
  beforeEach(() => {
    useChatTabsStore.setState({ tabs: [], activeTabId: null });
  });

  it("tab 从非 attention → attention 时调一次 bumpToAfterPinned", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    useChatTabsStore.getState().openSessionInNewTab(2);
    const t2Id = useChatTabsStore.getState().tabs[1].id;

    const spy = vi.spyOn(useChatTabsStore.getState(), "bumpToAfterPinned");
    const { rerender } = renderHook(
      ({ ids }: { ids: Set<string> }) => useAttentionBump(ids),
      { initialProps: { ids: new Set<string>() } },
    );
    rerender({ ids: new Set([t2Id]) });
    expect(spy).toHaveBeenCalledWith(t2Id);
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it("attention 持续 true 不重复触发", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    const t1Id = useChatTabsStore.getState().tabs[0].id;
    const spy = vi.spyOn(useChatTabsStore.getState(), "bumpToAfterPinned");
    const { rerender } = renderHook(
      ({ ids }: { ids: Set<string> }) => useAttentionBump(ids),
      { initialProps: { ids: new Set([t1Id]) } },
    );
    rerender({ ids: new Set([t1Id]) });
    rerender({ ids: new Set([t1Id]) });
    expect(spy).toHaveBeenCalledTimes(1); // 只在首次 mount 触发
  });

  it("true → false → true 时再次触发", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    const t1Id = useChatTabsStore.getState().tabs[0].id;
    const spy = vi.spyOn(useChatTabsStore.getState(), "bumpToAfterPinned");
    const { rerender } = renderHook(
      ({ ids }: { ids: Set<string> }) => useAttentionBump(ids),
      { initialProps: { ids: new Set([t1Id]) } },
    );
    rerender({ ids: new Set<string>() });
    rerender({ ids: new Set([t1Id]) });
    expect(spy).toHaveBeenCalledTimes(2);
  });
});
```

- [ ] **Step 2: 跑测试看红**

```bash
cd frontend && pnpm test -- src/components/agentre/chat-tabs/__tests__/use-attention-bump.test.ts
```

Expected: FAIL,模块不存在。

- [ ] **Step 3: 实现 hook**

新建 `frontend/src/components/agentre/chat-tabs/use-attention-bump.ts`:

```ts
import * as React from "react";

import { useChatTabsStore } from "@/stores/chat-tabs-store";

// useAttentionBump: 监听 attentionTabIds 集合的边沿变化。
// 任一 id 从"上一帧不在集合"变为"这一帧在集合",调一次 bumpToAfterPinned
// 把它搬到 pinned 前缀之后。持续在集合中不重复触发;离开再回来重新触发。
export function useAttentionBump(attentionTabIds: Set<string>): void {
  const prev = React.useRef<Set<string>>(new Set());
  React.useEffect(() => {
    const bump = useChatTabsStore.getState().bumpToAfterPinned;
    for (const id of attentionTabIds) {
      if (!prev.current.has(id)) bump(id);
    }
    prev.current = new Set(attentionTabIds);
  }, [attentionTabIds]);
}
```

- [ ] **Step 4: 跑测试看绿**

```bash
cd frontend && pnpm test -- src/components/agentre/chat-tabs/__tests__/use-attention-bump.test.ts
```

Expected: 3 用例都 PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/chat-tabs/use-attention-bump.ts frontend/src/components/agentre/chat-tabs/__tests__/use-attention-bump.test.ts
git commit -m "✨ feat(chat-tabs): add useAttentionBump edge-triggered hook"
```

---

## Task 7: TabStrip 接入 DnD + 接入 `useAttentionBump`,删 zone-divider

**Files:**
- Modify: `frontend/src/components/agentre/chat-tabs/tab-strip.tsx`

- [ ] **Step 1: 改 tab-strip.tsx**

完整重写为:

```tsx
// frontend/src/components/agentre/chat-tabs/tab-strip.tsx
import * as React from "react";
import { Pin, PinOff, X, XCircle, ArrowRightFromLine } from "lucide-react";

import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  horizontalListSortingStrategy,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";

import {
  useSessionAttentionList,
} from "@/stores/attention-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";

import { Tab } from "./tab";
import { TabOverflowMenu } from "./tab-overflow-menu";
import { TabTooltip } from "./tab-tooltip";
import { useAttentionBump } from "./use-attention-bump";
import { useTabsView, type TabView } from "./use-tabs-view";

export function TabStrip() {
  const sortedTabs = useTabsView();
  const tabs = useChatTabsStore((s) => s.tabs);
  const activeTabId = useChatTabsStore((s) => s.activeTabId);
  const setActive = useChatTabsStore((s) => s.setActive);
  const closeTab = useChatTabsStore((s) => s.closeTab);
  const closeOthers = useChatTabsStore((s) => s.closeOthers);
  const closeTabsToRight = useChatTabsStore((s) => s.closeTabsToRight);
  const promoteCurrent = useChatTabsStore((s) => s.promoteCurrent);
  const togglePin = useChatTabsStore((s) => s.togglePin);
  const moveTab = useChatTabsStore((s) => s.moveTab);

  // attention bump: 只对 session tab 计算
  const sessionTabIds = React.useMemo(
    () =>
      tabs
        .filter((t) => t.meta.kind === "session")
        .map(
          (t) => (t.meta as { kind: "session"; sessionId: number }).sessionId,
        ),
    [tabs],
  );
  const attentionItems = useSessionAttentionList(sessionTabIds);
  const attentionTabIds = React.useMemo(() => {
    const ids = new Set<string>();
    const bySid = new Map<number, true>();
    for (const x of attentionItems) bySid.set(x.sessionId, true);
    for (const t of tabs) {
      if (t.meta.kind !== "session") continue;
      if (bySid.has(t.meta.sessionId)) ids.add(t.id);
    }
    return ids;
  }, [attentionItems, tabs]);
  useAttentionBump(attentionTabIds);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  function onDragEnd(e: DragEndEvent) {
    const { active, over } = e;
    if (!over || active.id === over.id) return;
    const from = tabs.findIndex((t) => t.id === String(active.id));
    const to = tabs.findIndex((t) => t.id === String(over.id));
    if (from < 0 || to < 0) return;
    moveTab(from, to);
  }

  return (
    <div
      role="tablist"
      className="flex h-[38px] shrink-0 items-center overflow-hidden border-b border-border bg-secondary"
    >
      <div className="flex h-full min-h-0 min-w-0 flex-1 items-center overflow-x-auto overflow-y-hidden">
        <DndContext sensors={sensors} onDragEnd={onDragEnd}>
          <SortableContext
            items={sortedTabs.map((t) => t.id)}
            strategy={horizontalListSortingStrategy}
          >
            {sortedTabs.map((t, idx) => {
              const isLast = idx === sortedTabs.length - 1;
              return (
                <TabTooltip
                  key={t.id}
                  title={t.title}
                  projectChain={t.projectChain}
                  projectColor={t.projectColor}
                  status={t.status}
                  sessionId={t.sessionId}
                  worktreeBranch={t.worktreeBranch}
                  keyboardIndex={idx < 9 ? idx + 1 : null}
                  lastMessageAt={t.lastMessageAt}
                >
                  <SortableTab
                    tab={t}
                    active={t.id === activeTabId}
                    isLast={isLast}
                    onActivate={() => setActive(t.id)}
                    onClose={() => closeTab(t.id)}
                    onDoublePromote={() => {
                      setActive(t.id);
                      promoteCurrent();
                    }}
                    onTogglePin={() => togglePin(t.id)}
                    onCloseOthers={() => closeOthers(t.id)}
                    onCloseToRight={() => closeTabsToRight(t.id)}
                  />
                </TabTooltip>
              );
            })}
          </SortableContext>
        </DndContext>
      </div>
      {sortedTabs.length > 0 ? (
        <div className="flex h-full items-center gap-1 border-l border-border px-1">
          <TabOverflowMenu />
        </div>
      ) : null}
    </div>
  );
}

type SortableTabProps = {
  tab: TabView;
  active: boolean;
  isLast: boolean;
  onActivate: () => void;
  onClose: () => void;
  onDoublePromote: () => void;
  onTogglePin: () => void;
  onCloseOthers: () => void;
  onCloseToRight: () => void;
} & Omit<React.HTMLAttributes<HTMLSpanElement>, "children">;

const SortableTab = React.forwardRef<HTMLSpanElement, SortableTabProps>(
  function SortableTab(
    {
      tab,
      active,
      isLast,
      onActivate,
      onClose,
      onDoublePromote,
      onTogglePin,
      onCloseOthers,
      onCloseToRight,
      ...rest
    },
    forwardedRef,
  ) {
    const {
      attributes,
      listeners,
      setNodeRef,
      transform,
      transition,
      isDragging,
    } = useSortable({ id: tab.id });

    const style: React.CSSProperties = {
      transform: CSS.Transform.toString(transform),
      transition,
      opacity: isDragging ? 0.5 : undefined,
    };

    // 把外面 forwardedRef(Tooltip 给的) 和 dnd-kit 的 setNodeRef 合并
    const setRef = React.useCallback(
      (node: HTMLSpanElement | null) => {
        setNodeRef(node);
        if (typeof forwardedRef === "function") forwardedRef(node);
        else if (forwardedRef) forwardedRef.current = node;
      },
      [setNodeRef, forwardedRef],
    );

    return (
      <ContextMenu>
        <ContextMenuTrigger
          ref={setRef}
          className="inline-flex h-full min-w-0 flex-shrink"
          style={style}
          {...attributes}
          {...listeners}
          {...rest}
        >
          <Tab
            title={tab.title}
            avatar={tab.avatar}
            active={active}
            isPreview={tab.isPreview}
            isPinned={tab.isPinned}
            status={tab.status}
            projectColor={tab.projectColor}
            worktree={tab.worktree}
            pillText={tab.pillText}
            onActivate={onActivate}
            onClose={onClose}
            onDoublePromote={onDoublePromote}
          />
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onSelect={onTogglePin}>
            {tab.isPinned ? <PinOff /> : <Pin />}
            <span>{tab.isPinned ? "取消置顶" : "置顶"}</span>
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem onSelect={onClose}>
            <X />
            <span>关闭</span>
          </ContextMenuItem>
          <ContextMenuItem onSelect={onCloseOthers}>
            <XCircle />
            <span>关闭其他</span>
          </ContextMenuItem>
          <ContextMenuItem onSelect={onCloseToRight} disabled={isLast}>
            <ArrowRightFromLine />
            <span>关闭右侧</span>
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
    );
  },
);
```

变化说明:
- 删除 `TabZone` import 和 `tab-zone-divider` 渲染。
- 新增 `DndContext` + `SortableContext` + `SortableTab` 包装。
- `useAttentionBump` 接入 attention 边沿。
- `PointerSensor` 用 `activationConstraint: { distance: 4 }` 避开 click/contextmenu。
- 注意根 `<div role="tablist">` 的 className 现在保留原 `h-[38px]` + 新增 `shrink-0`/`overflow-hidden` —— 与现有 `tab-strip.test.tsx` 里 "保持固定高度只横向滚" 的断言相容(原测试断言 `shrink-0 overflow-hidden`,看 `tab-strip.test.tsx:43-49`)。

- [ ] **Step 2: 跑 tab-strip 测试**

```bash
cd frontend && pnpm test -- src/components/agentre/chat-tabs/__tests__/tab-strip.test.tsx
```

Expected: `Pinned ↔ Idle 之间渲染 1 根 zone 分隔符` 用例 FAIL(我们删了 zone-divider)。其他用例理论上 PASS。下一个 task 处理。

- [ ] **Step 3: 提交**

```bash
git add frontend/src/components/agentre/chat-tabs/tab-strip.tsx
git commit -m "✨ feat(tab-strip): integrate dnd-kit sortable + attention bump"
```

---

## Task 8: 更新 `tab-strip.test.tsx`(删 zone-divider 用例 + 加键盘拖拽用例)

**Files:**
- Modify: `frontend/src/components/agentre/chat-tabs/__tests__/tab-strip.test.tsx`

- [ ] **Step 1: 删 zone-divider 用例**

打开 `tab-strip.test.tsx`,删掉这个 `it`(120–132 行):

```ts
it("Pinned ↔ Idle 之间渲染 1 根 zone 分隔符", () => { ... });
```

- [ ] **Step 2: 加键盘拖拽用例**

在 describe 块末尾追加(在 `mkMeta` 工厂函数定义之前):

```ts
it("键盘拖拽 (Tab+Space+Right+Space) 把第 1 个 tab 拖到第 2 位", async () => {
  const user = userEvent.setup();
  useChatTabsStore.getState().openSessionInNewTab(1);
  useChatTabsStore.getState().openSessionInNewTab(2);
  const firstId = useChatTabsStore.getState().tabs[0].id;
  const secondId = useChatTabsStore.getState().tabs[1].id;
  render(<TabStrip />);

  // 找到第 1 个 tab 的 SortableTab wrapper(ContextMenuTrigger span,也是 dnd-kit
  // 拿键盘 listener 的元素)。它就是 tab 的 parentElement。
  const firstTab = screen.getAllByRole("tab")[0];
  const draggable = firstTab.parentElement!;
  draggable.focus();
  await user.keyboard("[Space]");
  await user.keyboard("[ArrowRight]");
  await user.keyboard("[Space]");

  const ids = useChatTabsStore.getState().tabs.map((t) => t.id);
  expect(ids).toEqual([secondId, firstId]);
});

it("拖拽后 store.moveTab 持久化新顺序到 localStorage", async () => {
  const user = userEvent.setup();
  useChatTabsStore.getState().openSessionInNewTab(1);
  useChatTabsStore.getState().openSessionInNewTab(2);
  render(<TabStrip />);

  const firstTab = screen.getAllByRole("tab")[0];
  const draggable = firstTab.parentElement!;
  draggable.focus();
  await user.keyboard("[Space]");
  await user.keyboard("[ArrowRight]");
  await user.keyboard("[Space]");

  // 持久化是 debounce 150ms 的,这里直接读 store 即可
  expect(
    useChatTabsStore
      .getState()
      .tabs.map((t) => (t.meta as { sessionId: number }).sessionId),
  ).toEqual([2, 1]);
});
```

如果第 1 个键盘拖拽用例在 jsdom 下不稳定(`@dnd-kit` 的 KeyboardSensor 需要 element 有 `getBoundingClientRect`,jsdom 默认返回 0),用 `getBoundingClientRect` mock 兜底:

```ts
beforeEach(() => {
  // jsdom 不实现 layout, dnd-kit 的 KeyboardSensor 需要 rect 才能 announce
  Element.prototype.getBoundingClientRect = vi.fn(() => ({
    x: 0, y: 0, width: 120, height: 38, top: 0, left: 0, right: 120,
    bottom: 38, toJSON: () => ({}),
  })) as never;
});
```

(放在原有 `beforeEach` 之后或合并进去。)

- [ ] **Step 3: 跑测试看绿**

```bash
cd frontend && pnpm test -- src/components/agentre/chat-tabs/__tests__/tab-strip.test.tsx
```

Expected: 全部 PASS。如果键盘拖拽用例仍 flaky,降级:直接 `useChatTabsStore.getState().moveTab(0, 1)` 验证 UI 跟随 store(这个降级的代价是没真正验证 DnD wiring;此时把"接 dnd-kit"的回归责任靠 manual verify Task 10 兜底)。

- [ ] **Step 4: 跑整个前端测试套**

```bash
cd frontend && pnpm test
```

Expected: 全绿。如果 `tab-overflow-menu.test.tsx` 或别的引用了 `TabZone`/`zone` 字段的地方还有残留,这一步会暴露 —— 修掉再继续。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/chat-tabs/__tests__/tab-strip.test.tsx
git commit -m "✅ test(tab-strip): cover keyboard drag reorder, drop zone-divider"
```

---

## Task 9: 跑 lint + full test gate

**Files:** 全部

- [ ] **Step 1: lint**

```bash
cd /Users/codfrm/Code/agentre/agentre && make lint
```

Expected: 通过。注意 `make lint` 会跑 `wails generate module` 先 —— 我们没动 Wails binding,所以这步是 no-op。

- [ ] **Step 2: 全量 test**

```bash
cd /Users/codfrm/Code/agentre/agentre && make test
```

Expected: backend + frontend 全绿。

- [ ] **Step 3: 残留 grep**

用 Grep tool 在 `frontend/src` 下搜:
- `selectSortedTabs` —— 应该 0 命中
- `TabZone` —— 应该 0 命中
- `tab-zone-divider` —— 应该 0 命中
- `\bzone\b` (限于 chat-tabs/) —— 应该 0 命中

如果还有命中(尤其是注释里),清掉。

- [ ] **Step 4: 提交(如有清理)**

```bash
git add -p   # 选择性添加任何残留清理
git commit -m "🔥 chore(chat-tabs): drop remaining zone references"
```

---

## Task 10: Manual verify(浏览器实操)

**Files:** 无代码改动,只验证。

- [ ] **Step 1: 起 dev**

```bash
make dev
```

- [ ] **Step 2: 走一遍 golden path**

逐项手动验证(打开 Agentre 主窗口,进入聊天页):

1. **基础拖拽**:开 3 个 tab,把第 1 个拖到第 3 位,顺序立即更新。
2. **拖完刷新**:刷新窗口(或重启 dev),新顺序仍在(localStorage 持久化生效)。
3. **拖出 pinned 自动 unpin**:置顶一个 tab,把它拖到非 pinned 区域,pin 图标消失,右键菜单变回"置顶"。
4. **拖进 pinned 不自动 pin**:置顶一个 tab(它在 index 0),把另一个非 pinned tab 拖到 index 0,它**不应**自动获得 pin 图标。
5. **togglePin 自动归位**:在中间位置右键置顶一个 tab,它跳到当前 pinned 前缀的末端。
6. **attention bump**:在另一个 tab(非 active)收到新消息时,该 tab 自动跳到 pinned 之后、其他非 pinned 之前——**仅一次**。然后手动把它拖到末尾;再来一条消息时,attention 状态从 true 维持 true,**不应**再次跳。
7. **attention re-bump**:active 切换到该 tab → attention 清空(变 false)→ 切走 → 来新消息(true)→ 该 tab **应该再次** bump。
8. **dblclick promote**:双击 preview tab(italic),应正常 promote(不 italic)。验证 PointerSensor 没吞 dblclick。
9. **右键菜单**:在任意 tab 上右键,菜单照常弹出,各项行为正常。
10. **⌘1..9 切 tab**:⌘1 / ⌘2 ... 仍按当前显示顺序切 tab。
11. **横向滚动**:开 ~10+ tab 触发横向溢出。拖最右一个 tab 向左移动,tab-strip 区域应自动滚动(dnd-kit autoScroll)。

- [ ] **Step 3: 标记结果**

把 Step 2 每条的实操结果(✓/✗)记下来。任一 ✗ → 回到对应 Task 修复。全 ✓ 才算完成。

- [ ] **Step 4: 最终提交(如无修复)**

无需提交;若 Step 3 中有发现的小问题修复,正常 add + commit 即可。

---

## Self-Review Notes

- **Spec coverage**:§1(model)→ T1+T3+T5;§2(normalize)→ T1+T2;§3(attention bump)→ T3+T6;§4(DnD UI)→ T7+T8;§5(测试/持久化)→ T2+T4+T6+T8+T9。全覆盖。
- **Risk 未明确处理**:spec 提到"bump 与拖拽并发"——`useAttentionBump` 没显式跳过 isDragging 期。判断这是低发概率事件(用户拖拽时刚好新消息到达),且 `bump` 走 store action,UI 由 store 重渲染时 dnd-kit 会自然 reconcile。如 Task 10 manual verify 中发现拖拽中 bump 导致 jump,加 `isDraggingRef` 跳过 + drag end 后 diff 补 bump。先发布后观察。
- **PointerSensor distance=4 与 dblclick**:Task 7 实现里没专门处理。Task 10 Step 2-8 显式验证。dnd-kit 的 PointerSensor 在 distance 未达成时不会吞事件,所以 click/dblclick 应正常分发;但 listeners spread 到 ContextMenuTrigger 上可能有意外。如出错,把 `{...listeners}` 改成只 spread 到 Tab 内部的拖拽 handle(如 title span),避开 close button。
