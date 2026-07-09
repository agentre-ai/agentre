# `!命令` 完成折叠 + 高度自适应 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 本地命令(`!cmd`)跑完后自动折叠成一行摘要(含耗时),展开/运行中的输出框高度随内容自适应且封顶,不再留固定 176px 黑框。

**Architecture:** 纯前端改动。两个纯函数(`formatDuration` 耗时格式化、`computeTerminalHeight` 高度计算)承载测试重量;store 增加 `finishedAt` 打点 + `expanded` 覆盖态 + `toggleExpanded` + `isCollapsed` 派生;`LocalCommandCard` 按 `isCollapsed` 渲染折叠行或展开卡;`OutputTerminal` 去掉固定高度改为内容自适应,空输出显示"无输出"占位。不动后端。

**Tech Stack:** React 19 + TypeScript + Zustand + @xterm/xterm + Vitest + Tailwind v4 + react-i18next。

## Global Constraints

- 新增可见 UI 文案必须走 i18n:`t("...")` + 同步 `frontend/src/i18n/locales/zh-CN/common.json` 和 `frontend/src/i18n/locales/en/common.json`;`i18next/no-literal-string` 会拦硬编码中文。
- 表单/交互控件用 shadcn `@/components/ui/*`(本改动复用现有 `Button`,不新增原生控件)。
- 只读文本用 `data-selectable-text="true"` + `shouldIgnoreClickForSelection` 守卫(不加复制按钮)。
- 折叠**一视同仁**:成功/失败/停止只要 `status !== "running"` 默认折叠。
- 折叠摘要只额外显示**耗时**(不显示行数/末行预览)。
- 折叠行**去掉** "本地命令" chip 与 "不发送给 AI",两者只在展开态 header 保留。
- 输出框 max 高度 ≈ 当前 176px(`h-44`),footprint 永不超过今天。
- 不改后端(`TerminalRunCommand` / `TerminalClose` 流式链路)。
- 共享分支 `develop/wyz`:每次 `git commit` **带 pathspec** 只提交本任务文件,不裸 commit(避免卷入并发会话 staged 改动)。
- 测试命令:聚焦用 `cd frontend && pnpm test -- <path>`;收尾用 `cd frontend && pnpm test` 全量 + `make lint`(前端 eslint+tsc)。

## File Structure

- `frontend/src/components/agentre/local-command/format-duration.ts` — 新增,纯函数 `formatDuration(ms)`。
- `frontend/src/components/agentre/local-command/terminal-height.ts` — 新增,纯函数 `computeTerminalHeight(...)`。
- `frontend/src/stores/local-commands-store.ts` — 加 `finishedAt` / `expanded` / `toggleExpanded` / 导出 `isCollapsed`。
- `frontend/src/components/agentre/local-command/output-terminal.tsx` — 去固定高度,内容自适应 + 空输出占位。
- `frontend/src/components/agentre/local-command/card.tsx` — 折叠行 / 展开卡两态。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` — 新增 `localCommand.expand/collapse/noOutput`。
- 测试:`format-duration.test.ts`(新) / `terminal-height.test.ts`(新) / `__tests__/local-commands-store.test.ts` / `__tests__/output-terminal.test.tsx` / `__tests__/card.test.tsx`。

---

### Task 1: `formatDuration` 纯函数

**Files:**
- Create: `frontend/src/components/agentre/local-command/format-duration.ts`
- Test: `frontend/src/components/agentre/local-command/__tests__/format-duration.test.ts`

**Interfaces:**
- Produces: `formatDuration(ms: number): string` — `<1000 → "{ms}ms"`;`1000–59999 → "{s.toFixed(1)}s"`;`≥60000 → "{m}m {s}s"`(秒用 `Math.floor` 防 60)。

- [ ] **Step 1: Write the failing test**

Create `frontend/src/components/agentre/local-command/__tests__/format-duration.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { formatDuration } from "../format-duration";

describe("formatDuration", () => {
  it("sub-second → milliseconds", () => {
    expect(formatDuration(0)).toBe("0ms");
    expect(formatDuration(420)).toBe("420ms");
    expect(formatDuration(999)).toBe("999ms");
  });
  it("seconds → one decimal", () => {
    expect(formatDuration(1000)).toBe("1.0s");
    expect(formatDuration(1200)).toBe("1.2s");
    expect(formatDuration(59900)).toBe("59.9s");
  });
  it("minutes → m s, seconds floored so never 60", () => {
    expect(formatDuration(60000)).toBe("1m 0s");
    expect(formatDuration(63000)).toBe("1m 3s");
    expect(formatDuration(119900)).toBe("1m 59s");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/local-command/__tests__/format-duration.test.ts`
Expected: FAIL — `Cannot find module '../format-duration'`.

- [ ] **Step 3: Write minimal implementation**

Create `frontend/src/components/agentre/local-command/format-duration.ts`:

```ts
// 本地命令耗时的紧凑格式化:ms / s(一位小数) / m s。秒用 floor 防止进位到 60。
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  const m = Math.floor(ms / 60000);
  const s = Math.floor((ms % 60000) / 1000);
  return `${m}m ${s}s`;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/local-command/__tests__/format-duration.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/local-command/format-duration.ts frontend/src/components/agentre/local-command/__tests__/format-duration.test.ts
git commit frontend/src/components/agentre/local-command/format-duration.ts frontend/src/components/agentre/local-command/__tests__/format-duration.test.ts -m "✨ orch: 本地命令耗时格式化 formatDuration"
```

---

### Task 2: store — `finishedAt` / `expanded` / `toggleExpanded` / `isCollapsed`

**Files:**
- Modify: `frontend/src/stores/local-commands-store.ts`
- Test: `frontend/src/stores/__tests__/local-commands-store.test.ts`

**Interfaces:**
- Consumes: 现有 `LocalCommandEntry` / `useLocalCommandsStore`。
- Produces:
  - `LocalCommandEntry` 增字段 `finishedAt?: number`、`expanded?: boolean`。
  - `finish(id, status, exitCode?)` 现在还写 `finishedAt = Date.now()`。
  - 新 action `toggleExpanded(id: string): void` — 翻转显式覆盖态(`expanded ?? (status === "running")` 取反)。
  - 新导出纯函数 `isCollapsed(entry: LocalCommandEntry): boolean` —
    `entry.expanded === undefined ? entry.status !== "running" : !entry.expanded`。

- [ ] **Step 1: Write the failing test**

Append to `frontend/src/stores/__tests__/local-commands-store.test.ts` inside the `describe`:

```ts
  it("finish stamps finishedAt", () => {
    const spy = vi.spyOn(Date, "now").mockReturnValue(5000);
    const s = useLocalCommandsStore.getState();
    s.start({ id: "f1", sessionId: 1, command: "ls", createdAt: 100 });
    s.finish("f1", "done", 0);
    expect(useLocalCommandsStore.getState().get("f1")!.finishedAt).toBe(5000);
    spy.mockRestore();
  });

  it("isCollapsed: running expanded by default, finished collapsed by default", () => {
    const s = useLocalCommandsStore.getState();
    s.start({ id: "c1", sessionId: 1, command: "ls", createdAt: 1 });
    expect(isCollapsed(useLocalCommandsStore.getState().get("c1")!)).toBe(false);
    s.finish("c1", "done", 0);
    expect(isCollapsed(useLocalCommandsStore.getState().get("c1")!)).toBe(true);
  });

  it("toggleExpanded flips the collapsed state and survives re-read", () => {
    const s = useLocalCommandsStore.getState();
    s.start({ id: "c2", sessionId: 1, command: "ls", createdAt: 1 });
    s.finish("c2", "done", 0);
    s.toggleExpanded("c2"); // collapsed(true) → expanded
    expect(isCollapsed(useLocalCommandsStore.getState().get("c2")!)).toBe(false);
    s.toggleExpanded("c2"); // → collapsed again
    expect(isCollapsed(useLocalCommandsStore.getState().get("c2")!)).toBe(true);
  });
```

Update the test file's import line to add `vi` and `isCollapsed`:

```ts
import { describe, it, expect, beforeEach, vi } from "vitest";
import { useLocalCommandsStore, isCollapsed } from "../local-commands-store";
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/stores/__tests__/local-commands-store.test.ts`
Expected: FAIL — `isCollapsed` / `toggleExpanded` not exported / not a function.

- [ ] **Step 3: Write minimal implementation**

In `frontend/src/stores/local-commands-store.ts`:

Add fields to `LocalCommandEntry` (after `output: string;`):

```ts
  finishedAt?: number;
  expanded?: boolean;
```

Add to the `State` interface (after `finish(...)` declaration):

```ts
  toggleExpanded(id: string): void;
```

Change `finish` implementation to stamp `finishedAt`:

```ts
  finish: (id, status, exitCode) =>
    set((s) => {
      const cur = s.entries[id];
      if (!cur) return s;
      return {
        entries: {
          ...s.entries,
          [id]: { ...cur, status, exitCode, finishedAt: Date.now() },
        },
      };
    }),
```

Add `toggleExpanded` implementation (after `finish`):

```ts
  toggleExpanded: (id) =>
    set((s) => {
      const cur = s.entries[id];
      if (!cur) return s;
      const collapsed = isCollapsed(cur);
      // 折叠中 → 展开(expanded=true);展开中 → 折叠(expanded=false)。
      return {
        entries: { ...s.entries, [id]: { ...cur, expanded: collapsed } },
      };
    }),
```

Add exported pure helper at the end of the file (after the `create(...)` block):

```ts
// 折叠态派生:未手动切换过时,运行中展开、完成后折叠;切换过则以显式 expanded 为准。
export function isCollapsed(entry: LocalCommandEntry): boolean {
  return entry.expanded === undefined
    ? entry.status !== "running"
    : !entry.expanded;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/stores/__tests__/local-commands-store.test.ts`
Expected: PASS (existing 4 + new 3 = 7 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/stores/local-commands-store.ts frontend/src/stores/__tests__/local-commands-store.test.ts
git commit frontend/src/stores/local-commands-store.ts frontend/src/stores/__tests__/local-commands-store.test.ts -m "✨ orch: 本地命令 store 增 finishedAt/expanded/toggleExpanded/isCollapsed"
```

---

### Task 3: `computeTerminalHeight` 纯函数

**Files:**
- Create: `frontend/src/components/agentre/local-command/terminal-height.ts`
- Test: `frontend/src/components/agentre/local-command/__tests__/terminal-height.test.ts`

**Interfaces:**
- Produces:
  - 常量 `MIN_ROWS = 3`、`MAX_ROWS = 9`、`PADDING_PX = 12`、`FALLBACK_CELL_PX = 18`(≈18px/行 × 9 行 + 12 ≈ 174px,匹配旧 `h-44`)。
  - `computeTerminalHeight(args: { contentRows: number; cellHeight: number; minRows: number; maxRows: number; paddingPx: number }): number` —
    `clamp(contentRows, minRows, maxRows) * cellHeight + paddingPx`,返回像素数。

- [ ] **Step 1: Write the failing test**

Create `frontend/src/components/agentre/local-command/__tests__/terminal-height.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { computeTerminalHeight } from "../terminal-height";

const base = { cellHeight: 18, minRows: 3, maxRows: 9, paddingPx: 12 };

describe("computeTerminalHeight", () => {
  it("empty/short output clamps up to the min rows (no big void)", () => {
    expect(computeTerminalHeight({ ...base, contentRows: 0 })).toBe(66); // 3*18+12
    expect(computeTerminalHeight({ ...base, contentRows: 2 })).toBe(66);
  });
  it("mid output fits its content", () => {
    expect(computeTerminalHeight({ ...base, contentRows: 5 })).toBe(102); // 5*18+12
  });
  it("long output caps at max rows (then xterm scrolls)", () => {
    expect(computeTerminalHeight({ ...base, contentRows: 9 })).toBe(174); // 9*18+12
    expect(computeTerminalHeight({ ...base, contentRows: 50 })).toBe(174);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/local-command/__tests__/terminal-height.test.ts`
Expected: FAIL — `Cannot find module '../terminal-height'`.

- [ ] **Step 3: Write minimal implementation**

Create `frontend/src/components/agentre/local-command/terminal-height.ts`:

```ts
// 只读输出终端的内容自适应高度。行数在 [MIN_ROWS, MAX_ROWS] 间夹取后 × 行高 + 内边距;
// 超过 MAX_ROWS 则封顶(视口固定,xterm 自身滚动)。MAX_ROWS×FALLBACK+PADDING ≈ 旧 h-44(176px)。
export const MIN_ROWS = 3;
export const MAX_ROWS = 9;
export const PADDING_PX = 12; // py-1.5 上下各 6px
export const FALLBACK_CELL_PX = 18; // fontSize 13 下的兜底行高(拿不到真实度量时用)

export function computeTerminalHeight(args: {
  contentRows: number;
  cellHeight: number;
  minRows: number;
  maxRows: number;
  paddingPx: number;
}): number {
  const rows = Math.min(
    Math.max(args.contentRows, args.minRows),
    args.maxRows,
  );
  return rows * args.cellHeight + args.paddingPx;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/local-command/__tests__/terminal-height.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/local-command/terminal-height.ts frontend/src/components/agentre/local-command/__tests__/terminal-height.test.ts
git commit frontend/src/components/agentre/local-command/terminal-height.ts frontend/src/components/agentre/local-command/__tests__/terminal-height.test.ts -m "✨ orch: 本地命令输出 内容自适应高度纯函数"
```

---

### Task 4: `OutputTerminal` — 内容自适应高度 + 空输出占位

**Files:**
- Modify: `frontend/src/components/agentre/local-command/output-terminal.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`(`localCommand.noOutput`)
- Modify: `frontend/src/i18n/locales/en/common.json`(`localCommand.noOutput`)
- Test: `frontend/src/components/agentre/local-command/__tests__/output-terminal.test.tsx`

**Interfaces:**
- Consumes: `computeTerminalHeight` / `MIN_ROWS` / `MAX_ROWS` / `PADDING_PX` / `FALLBACK_CELL_PX`(Task 3);`useLocalCommandsStore`。
- Produces: `OutputTerminal` 组件签名不变(`{ terminalId: string }`)。

- [ ] **Step 1: Write the failing test**

First add the i18n key so the test's `t("localCommand.noOutput")` resolves.

In `frontend/src/i18n/locales/en/common.json`, `localCommand` block — add after `"dismiss": "Dismiss command"` (add a comma to that line):

```json
    "dismiss": "Dismiss command",
    "noOutput": "No output",
    "expand": "Expand output",
    "collapse": "Collapse output"
```

In `frontend/src/i18n/locales/zh-CN/common.json`, `localCommand` block — mirror:

```json
    "dismiss": "移除此命令",
    "noOutput": "无输出",
    "expand": "展开输出",
    "collapse": "折叠输出"
```

Now update the xterm mock's buffer in `__tests__/output-terminal.test.tsx` (so height wiring can read `baseY`/`cursorY`) — change the `buffer` line to:

```ts
      buffer: { active: { length: 1, baseY: 0, cursorY: 0 } },
```

Add these tests inside `describe("OutputTerminal", ...)`:

```ts
  it("sizes the container to content (min rows) instead of a fixed 176px void", () => {
    delete (globalThis as { IntersectionObserver?: unknown })
      .IntersectionObserver;
    useLocalCommandsStore
      .getState()
      .start({ id: "h1", sessionId: 1, command: "echo", createdAt: 1 });
    useLocalCommandsStore.getState().appendOutput("h1", "one line\n");

    const { getByTestId } = render(<OutputTerminal terminalId="h1" />);

    // 1 content row → clamped to MIN_ROWS(3): 3*18(fallback)+12 = 66px. No h-44.
    const box = getByTestId("local-command-terminal");
    expect(box.style.height).toBe("66px");
    expect(box.className).not.toContain("h-44");
  });

  it("finished command with empty output shows a 无输出 placeholder, builds no xterm", () => {
    delete (globalThis as { IntersectionObserver?: unknown })
      .IntersectionObserver;
    useLocalCommandsStore
      .getState()
      .start({ id: "e1", sessionId: 1, command: "touch x", createdAt: 1 });
    useLocalCommandsStore.getState().finish("e1", "done", 0);

    const { getByTestId } = render(<OutputTerminal terminalId="e1" />);

    expect(getByTestId("local-command-terminal").textContent).toMatch(
      /无输出|No output/,
    );
    expect(Terminal).not.toHaveBeenCalled();
  });
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/local-command/__tests__/output-terminal.test.tsx`
Expected: FAIL — height test sees no inline `style.height` (still `h-44`); placeholder test finds empty box + a constructed Terminal.

- [ ] **Step 3: Write minimal implementation**

Rewrite `frontend/src/components/agentre/local-command/output-terminal.tsx`. Key changes: import `useTranslation` + the height helpers + a reactive empty-finished selector; early-return the "无输出" placeholder; drop `h-44` and drive inline height via `applyHeight()`.

Add imports at top (after existing imports):

```ts
import { useTranslation } from "react-i18next";
import {
  computeTerminalHeight,
  MIN_ROWS,
  MAX_ROWS,
  PADDING_PX,
  FALLBACK_CELL_PX,
} from "./terminal-height";
```

Inside the component, before the effects, add:

```ts
  const { t } = useTranslation();
  const cellHeightRef = useRef(FALLBACK_CELL_PX);
  const isEmptyFinished = useLocalCommandsStore((s) => {
    const e = s.entries[terminalId];
    return !!e && e.status !== "running" && e.output === "";
  });
```

Add an `applyHeight` helper and call it from the write effect. Replace the "seed + 增量" effect (the `useEffect` with `writeDelta`) with:

```ts
  // seed + 增量:写新增片段 + 每次按内容重算容器高度(自适应且封顶)。
  useEffect(() => {
    if (!mounted) return;
    const applyHeight = () => {
      const term = xtermRef.current;
      const el = containerRef.current;
      if (!term || !el) return;
      // 有真实布局时用它反推行高(容器高 - padding)/ 行数;happy-dom 下退回兜底常量。
      if (el.clientHeight > 0 && term.rows > 0) {
        const m = (el.clientHeight - PADDING_PX) / term.rows;
        if (Number.isFinite(m) && m > 0) cellHeightRef.current = m;
      }
      const b = term.buffer.active;
      const contentRows = b.baseY + b.cursorY + 1;
      el.style.height = `${computeTerminalHeight({
        contentRows,
        cellHeight: cellHeightRef.current,
        minRows: MIN_ROWS,
        maxRows: MAX_ROWS,
        paddingPx: PADDING_PX,
      })}px`;
      fitRef.current?.fit();
    };
    const writeDelta = () => {
      const entry = useLocalCommandsStore.getState().get(terminalId);
      const term = xtermRef.current;
      if (!entry || !term) return;
      if (entry.output.length > writtenLenRef.current) {
        term.write(entry.output.slice(writtenLenRef.current));
        writtenLenRef.current = entry.output.length;
      }
      applyHeight();
    };
    writeDelta(); // 首帧 seed + 定高。
    const unsub = useLocalCommandsStore.subscribe(writeDelta);
    return () => unsub();
  }, [mounted, terminalId]);
```

Change the return JSX. Replace the final `return (...)` with:

```tsx
  if (isEmptyFinished) {
    return (
      <div
        data-testid="local-command-terminal"
        className="bg-code-surface px-3 py-2 text-2xs text-muted-foreground"
      >
        {t("localCommand.noOutput")}
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      data-testid="local-command-terminal"
      className="w-full overflow-hidden bg-code-surface px-2 py-1.5"
    />
  );
```

(Note: `h-44` removed; height now comes from inline `style.height`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/local-command/__tests__/output-terminal.test.tsx`
Expected: PASS (existing 4 + new 2 = 6 tests). The existing "writes raw ANSI", "read-only", "streams deltas", "lazy-mounts" still pass (write path unchanged, only height added).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/local-command/output-terminal.tsx frontend/src/components/agentre/local-command/__tests__/output-terminal.test.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit frontend/src/components/agentre/local-command/output-terminal.tsx frontend/src/components/agentre/local-command/__tests__/output-terminal.test.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json -m "✨ orch: 本地命令输出 高度自适应+空输出占位"
```

---

### Task 5: `LocalCommandCard` — 折叠行 / 展开卡两态 + 耗时

**Files:**
- Modify: `frontend/src/components/agentre/local-command/card.tsx`
- Test: `frontend/src/components/agentre/local-command/__tests__/card.test.tsx`

**Interfaces:**
- Consumes: `isCollapsed`(Task 2)、`formatDuration`(Task 1)、`shouldIgnoreClickForSelection`(现有 `../../copyable-text`)、`useLocalCommandsStore.toggleExpanded`。
- Produces: `LocalCommandCard` 签名不变(`{ entryId, onOpenInTerminal }`)。

- [ ] **Step 1: Write the failing test**

Add tests inside `describe("LocalCommandCard", ...)` in `__tests__/card.test.tsx`:

```ts
  it("finished command defaults to a collapsed one-line summary (no chip / no 'not sent to AI')", () => {
    useLocalCommandsStore.getState().entries; // ensure store touched
    useLocalCommandsStore.setState({
      entries: {
        s1: {
          id: "s1",
          sessionId: 1,
          command: "git status",
          createdAt: 1000,
          finishedAt: 2200, // 1.2s
          status: "done",
          exitCode: 0,
          output: "clean\n",
        },
      },
    });
    render(<LocalCommandCard entryId="s1" onOpenInTerminal={vi.fn()} />);

    expect(screen.getByText("git status")).toBeInTheDocument();
    expect(screen.getByText("1.2s")).toBeInTheDocument();
    expect(screen.getByText(/退出码 0|Exit 0/)).toBeInTheDocument();
    // 折叠行不含 chip 与 "不发送给 AI"
    expect(screen.queryByText(/本地命令|Local command/)).toBeNull();
    expect(screen.queryByText(/不发送给 AI|Not sent to AI/)).toBeNull();
  });

  it("clicking a collapsed summary expands it to reveal the header chip", async () => {
    useLocalCommandsStore.setState({
      entries: {
        s2: {
          id: "s2",
          sessionId: 1,
          command: "ls",
          createdAt: 1000,
          finishedAt: 1500,
          status: "done",
          exitCode: 0,
          output: "a\n",
        },
      },
    });
    render(<LocalCommandCard entryId="s2" onOpenInTerminal={vi.fn()} />);
    // collapsed: no chip yet
    expect(screen.queryByText(/本地命令|Local command/)).toBeNull();

    await userEvent.click(
      screen.getByRole("button", { name: /展开输出|Expand output/ }),
    );

    // expanded header now shows chip + "not sent to AI"
    expect(screen.getByText(/本地命令|Local command/)).toBeInTheDocument();
    expect(screen.getByText(/不发送给 AI|Not sent to AI/)).toBeInTheDocument();
  });
```

`OutputTerminal` is already mocked to `() => null` at the top of this file, so expanding won't build a real xterm.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/local-command/__tests__/card.test.tsx`
Expected: FAIL — collapsed row not implemented: chip is still rendered for finished cards, no "1.2s", no expand button.

- [ ] **Step 3: Write minimal implementation**

Rewrite `frontend/src/components/agentre/local-command/card.tsx`. Add imports and split into collapsed / expanded render.

Update the imports block:

```ts
import { useTranslation } from "react-i18next";
import { SquareTerminal, X, ChevronRight, ChevronDown } from "lucide-react";

import { Button } from "@/components/ui/button";

import { TerminalClose } from "../../../../wailsjs/go/app/App";
import {
  useLocalCommandsStore,
  isCollapsed,
} from "../../../stores/local-commands-store";
import type { LocalCommandStatus } from "../../../stores/local-commands-store";
import { shouldIgnoreClickForSelection } from "../copyable-text";
import { formatDuration } from "./format-duration";
import { OutputTerminal } from "./output-terminal";
```

Keep the existing `STATUS_CONFIG` map unchanged.

Replace the component body (`export function LocalCommandCard(...) { ... }`) with:

```tsx
export function LocalCommandCard({
  entryId,
  onOpenInTerminal,
}: {
  entryId: string;
  onOpenInTerminal: (id: string) => void;
}) {
  const { t } = useTranslation();
  const entry = useLocalCommandsStore((s) => s.entries[entryId]);

  if (!entry) return null;

  const cfg = STATUS_CONFIG[entry.status];
  const isRunning = entry.status === "running";
  const showExitCode =
    entry.status !== "running" && entry.exitCode !== undefined;
  const collapsed = isCollapsed(entry);
  const duration =
    entry.finishedAt !== undefined
      ? formatDuration(entry.finishedAt - entry.createdAt)
      : null;

  const statusPill = (
    <span
      className={`flex items-center gap-1.5 rounded-sm px-1.5 py-0.5 text-2xs font-semibold tracking-wider ${cfg.pill}`}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${cfg.dot}`} />
      {t(cfg.labelKey)}
      {showExitCode && (
        <>
          <span className="opacity-50">·</span>
          {t("localCommand.exitCode", { code: entry.exitCode })}
        </>
      )}
    </span>
  );

  const dismissBtn = (
    <button
      type="button"
      aria-label={t("localCommand.dismiss")}
      title={t("localCommand.dismiss")}
      className="-mr-1 inline-flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
      onClick={(e) => {
        e.stopPropagation();
        useLocalCommandsStore.getState().remove(entryId);
      }}
    >
      <X className="size-3.5" aria-hidden="true" />
    </button>
  );

  // ── Collapsed: one-line summary (command + status + exit + duration). ──
  if (collapsed) {
    const toggle = () =>
      useLocalCommandsStore.getState().toggleExpanded(entryId);
    return (
      <div
        role="button"
        tabIndex={0}
        aria-label={t("localCommand.expand")}
        onClick={(e) => {
          if (shouldIgnoreClickForSelection(e)) return;
          toggle();
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            toggle();
          }
        }}
        className="flex cursor-pointer items-center gap-2 rounded-lg border border-border bg-card px-3.5 py-2 text-foreground shadow-sm transition-colors hover:bg-accent/40"
      >
        <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <SquareTerminal className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span
          data-selectable-text="true"
          className="min-w-0 flex-1 truncate font-mono text-xs font-semibold text-foreground"
        >
          {entry.command}
        </span>
        {duration && (
          <span className="shrink-0 text-2xs tabular-nums text-muted-foreground">
            {duration}
          </span>
        )}
        {statusPill}
        {dismissBtn}
      </div>
    );
  }

  // ── Expanded: full header + output terminal. ──
  return (
    <div className="rounded-lg border border-border bg-card text-foreground shadow-sm">
      {/* Header */}
      <div className="flex items-center gap-2 border-b border-border px-3.5 py-2.5">
        <SquareTerminal className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />

        {/* "本地命令" chip */}
        <span className="rounded-sm border border-border bg-muted px-1.5 py-0.5 text-2xs font-semibold text-muted-foreground">
          {t("localCommand.localChip")}
        </span>

        {/* Command */}
        <span
          data-selectable-text="true"
          className="font-mono text-xs font-semibold text-foreground"
        >
          {entry.command}
        </span>

        <div className="flex-1" />

        {/* Not shared with AI marker */}
        <span className="text-2xs text-muted-foreground/70">
          {t("localCommand.notSharedWithAI")}
        </span>

        {duration && (
          <span className="text-2xs tabular-nums text-muted-foreground">
            {duration}
          </span>
        )}

        {/* Status pill */}
        {statusPill}

        {/* Collapse — only once finished (running stays open to stream). */}
        {!isRunning && (
          <button
            type="button"
            aria-label={t("localCommand.collapse")}
            title={t("localCommand.collapse")}
            className="-mr-1 inline-flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            onClick={() =>
              useLocalCommandsStore.getState().toggleExpanded(entryId)
            }
          >
            <ChevronDown className="size-3.5" aria-hidden="true" />
          </button>
        )}

        {/* Dismiss — only once finished; running cards must be stopped first. */}
        {!isRunning && dismissBtn}
      </div>

      {/* Output area — read-only xterm; ANSI/OSC 交给 xterm 解释,不剥转义。 */}
      <OutputTerminal terminalId={entry.id} />

      {/* Actions — only while running */}
      {isRunning && (
        <div className="flex items-center gap-2 border-t border-border px-3.5 py-2.5">
          <div className="flex-1" />
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => void TerminalClose(entryId)}
          >
            {t("localCommand.stop")}
          </Button>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => onOpenInTerminal(entryId)}
          >
            {t("localCommand.openInTerminal")}
          </Button>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- src/components/agentre/local-command/__tests__/card.test.tsx`
Expected: PASS. Existing 4 tests still green (running expanded; finished collapsed still shows command / exit code / dismiss); new 2 tests green.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/local-command/card.tsx frontend/src/components/agentre/local-command/__tests__/card.test.tsx
git commit frontend/src/components/agentre/local-command/card.tsx frontend/src/components/agentre/local-command/__tests__/card.test.tsx -m "✨ orch: 本地命令卡 完成折叠成一行摘要(命令+状态+退出码+耗时)"
```

---

### Task 6: 全量门控 + i18n 一致性

**Files:** none (verification only)

- [ ] **Step 1: i18n 一致性测试**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS — 新增 `localCommand.expand/collapse/noOutput` 在 zh-CN 与 en 都存在且被静态 `t(...)` 引用。

- [ ] **Step 2: 全量前端测试**

Run: `cd frontend && pnpm test`
Expected: PASS,exit code 0(不要只看 tail;确认真实退出码)。

- [ ] **Step 3: lint + 类型 + 格式**

Run: `make lint`
Expected: PASS — eslint(含 `i18next/no-literal-string`)+ tsc 无错。

- [ ] **Step 4: 真机自查(可选但推荐)**

`make dev` 起应用,在会话输入框敲 `!git status`(有输出、退出 0)、`!touch /tmp/x`(空输出)、`!ls /nonexistent`(失败):确认跑完自动折叠成一行含耗时;点击展开出终端且高度贴合内容;空输出展开显示"无输出";失败折叠行状态红、退出码非 0。

- [ ] **Step 5: 收尾说明**

无需额外提交(本任务仅验证)。若前几个任务遗漏 i18n / 类型问题,回到对应任务修复并按其 pathspec 重新提交。

---

## Self-Review

**Spec coverage:**
- 完成后自动折叠成一行(命令+状态+退出码+耗时)→ Task 2(派生)+ Task 5(渲染)+ Task 1(耗时)。✅
- 高度自适应且封顶、空输出占位 → Task 3(纯函数)+ Task 4(wiring + 占位)。✅
- 一视同仁折叠 → Task 2 `isCollapsed`(仅看 `status !== "running"`)。✅
- 折叠行去 chip / 去"不发送给 AI" → Task 5 折叠分支不渲染二者,展开 header 保留。✅
- max ≈176px → Task 3 `MAX_ROWS=9`。✅
- select-to-copy 守卫 → Task 5 折叠行 `shouldIgnoreClickForSelection`。✅
- i18n 双语 → Task 4 加键,Task 6 一致性校验。✅
- 不动后端 → 全程无 Go 改动。✅

**Placeholder scan:** 无 TODO/TBD;所有 step 含完整代码或精确命令。✅

**Type consistency:** `isCollapsed`/`toggleExpanded`/`finishedAt`/`expanded`(Task 2)与 Task 5 用法一致;`computeTerminalHeight` 参数对象与 Task 3/4 一致;`formatDuration(ms:number)` 与 Task 1/5 一致。✅
