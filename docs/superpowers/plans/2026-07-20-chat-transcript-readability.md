# 对话流可读性重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把对话流的 8 级字号阶梯压到 4 级、把 `max-w-[720px]` 魔法字面量收成 measure token、把散落的卡片样式收成一个共享原语,使长对话读起来不累。

**Architecture:** 三层。底层是 `globals.css` 里新增的 `--text-prose/aux/meta` + `--container-measure` token;中层是新的 `transcript-card.tsx` 卡片原语;上层是逐个组件替换字面量。全程由一个**排版护栏测试**兜底 —— 该测试的扫描文件清单随每个任务增长,所以每个任务都有真实的 red→green。

**Tech Stack:** React 19 + TypeScript + Tailwind CSS v4(`@theme inline` token 约定)+ Vitest + Testing Library。

## Global Constraints

- 设计依据:`docs/superpowers/specs/2026-07-20-chat-transcript-readability-design.md`。与 spec 冲突时以 spec 为准。
- **纯前端视觉层**。不改 `internal/` 任何 Go 代码,不改数据流,不改 Wails binding。
- **不做 drive-by**:不重排 import、不改无关组件、不顺手改文案、不跑格式化全量 pass(AGENTS.md 高优先级约束 4)。
- **不加新可见文案**,因此本轮不动 `i18n/locales/*`。若发现必须加文案,停下来问用户。
- **已否决,不要自作主张加回来**:栏居中、用户消息灰底块、字号/密度设置项、工具卡嵌套滚动改造(`max-h-[120px]` / `max-h-[200px]` 保持原样)。
- 分支 `develop/wyz` 是**共享分支且有并发会话**。**每次提交必须带 pathspec**(`git commit <files> -m ...`),裸 `git commit` 会把别人 staged 的改动卷进来。
- 前端测试命令:`cd frontend && pnpm test -- <path>`(focused);收尾用 `cd frontend && pnpm test` 全量。
- 类型/格式以 `tsc` 与 `pnpm lint` 为准,**不要信 LSP 内联诊断**(本仓库 LSP 诊断常 stale)。

## Token 契约(所有任务共享)

Task 1 建立,后续任务全部消费。名字刻意避开 `secondary` / `muted` 等已被 `--color-*` 占用的词。

| Tailwind 类 | 值 | 用途 |
| --- | --- | --- |
| `text-prose` | 15px / 1.7 | 对话流正文(markdown body) |
| `text-aux` | 13px / 1.65 | 工具卡正文、代码、思考正文、表格 |
| `text-meta` | 12px / 1.25rem | 时间戳、复制/编辑/重新生成、token、语言标签、状态胶囊、行号 |
| `max-w-measure` | 720px | 对话流正文栏宽 |

---

## 文件结构

**新建**

| 文件 | 职责 |
| --- | --- |
| `frontend/src/components/agentre/transcript-card.tsx` | 卡片原语:`TranscriptCard` / `TranscriptCardHeader` / `TranscriptCardBody` / `TranscriptPill`。对话流里所有卡片样式的**唯一定义处**。 |
| `frontend/src/components/agentre/__tests__/transcript-card.test.tsx` | 原语单测(唯一断言具体 class 的地方 —— 因为这里就是定义处)。 |
| `frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts` | 排版护栏:扫描对话流组件源码,命中禁用字面量即失败。 |
| `frontend/src/__tests__/design-tokens.test.ts` | 断言 `globals.css` 暴露了 token 契约。 |

**修改**(按任务分组,见下)

`globals.css` · `message-row.tsx` · `markdown-text.tsx` · `code-block.tsx` · `thinking-block.tsx` · `transcript-row-view.tsx` · `chat.tsx` · `chat-panel.tsx` · `compact-boundary-divider.tsx` · `compact-history-fold.tsx` · `auto-trigger-banner.tsx` · `canonical-tool/{raw,file-edit,file-write,agent-spawn,plan}/card.tsx` · `canonical-tool/file-edit/hunk-renderer.tsx` · `canonical-tool/{tool-permission,user-ask}/card.tsx` · `tool-approval/card.tsx` · `local-command/card.tsx`

**已知会被打破的既有测试**(这是预期的,不是意外 —— 它们断言的正是本轮要改的值):

- `__tests__/chat.test.tsx:1488` `renders the content column under max-w-[720px] inside the article` → 改断言 `max-w-measure`(Task 8)
- `__tests__/chat.test.tsx:1485` 断言 meta 容器 `text-subtle-foreground` → 改 `text-muted-foreground`(Task 8)

---

## 护栏设计说明(实现者必读)

护栏测试**不是**逐组件断言 class。它只做一件事:扫描一份显式文件清单,禁止若干字面量再出现。

关键细节:**`chat.tsx` 同时包含 transcript 和 composer**(第 646/654/714/726 行是拖放区、输入框外壳、附件缩略图 —— 属于输入框,不是对话流)。所以护栏必须**按文件裁剪生效的规则组**,不能一刀切:`chat.tsx` 跳过 `shadow` / `radius` 两组,只受字号与 measure 约束。

清单用显式数组、不用通配。新增对话流组件时必须手动加进来 —— 这是有意的,让「加进对话流」成为一个需要过目排版护栏的动作。

---

### Task 1: Design token 契约 + 护栏骨架

**Files:**
- Modify: `frontend/src/styles/globals.css`(`@theme inline` 块,约第 197–215 行)
- Create: `frontend/src/__tests__/design-tokens.test.ts`
- Create: `frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts`

**Interfaces:**
- Consumes: 无
- Produces: Tailwind 工具类 `text-prose` / `text-aux` / `text-meta` / `max-w-measure`;护栏测试的 `SCANNED` 清单(后续每个任务往里加文件)。

- [ ] **Step 1: 写失败测试 —— token 契约**

Create `frontend/src/__tests__/design-tokens.test.ts`:

```ts
import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

// 对话流排版依赖这几个 token。它们是 globals.css 与所有 transcript 组件之间的契约,
// 改名/删除会让 max-w-measure、text-prose 等工具类静默失效(Tailwind 不会报错,
// 只是不生成这个类),所以在这里锁死。
const GLOBALS_CSS = path.resolve(__dirname, "../styles/globals.css");

const REQUIRED_TOKENS: [string, string][] = [
  ["--text-prose", "0.9375rem"],
  ["--text-prose--line-height", "1.7"],
  ["--text-aux", "0.8125rem"],
  ["--text-aux--line-height", "1.65"],
  ["--text-meta", "0.75rem"],
  ["--text-meta--line-height", "1.25rem"],
  ["--container-measure", "45rem"],
];

describe("design tokens", () => {
  it("globals.css 暴露对话流排版 token", () => {
    const css = fs.readFileSync(GLOBALS_CSS, "utf8");
    const missing = REQUIRED_TOKENS.filter(
      ([name, value]) => !new RegExp(`${name}:\\s*${value}\\s*;`).test(css),
    ).map(([name]) => name);
    expect(missing).toEqual([]);
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/__tests__/design-tokens.test.ts`
Expected: FAIL —— `expected [ '--text-prose', '--text-prose--line-height', '--text-aux', '--text-aux--line-height', '--text-meta', '--text-meta--line-height', '--container-measure' ] to deeply equal []`

- [ ] **Step 3: 加 token**

在 `frontend/src/styles/globals.css` 的 `@theme inline` 块内,紧跟现有 `--text-2xs` 定义之后(约第 215 行)插入:

```css
  /* 对话流排版阶梯 —— 4 级,替代此前散落的 9/10/11/12/13/14/15/16 八级。
     命名避开 secondary / muted 等已被 --color-* 占用的词:否则 text-secondary
     会同时是字号和文字颜色,产生歧义。 */
  --text-prose: 0.9375rem;
  --text-prose--line-height: 1.7;
  --text-aux: 0.8125rem;
  --text-aux--line-height: 1.65;
  --text-meta: 0.75rem;
  --text-meta--line-height: 1.25rem;
  /* 对话流正文栏宽。此前是 9 个文件各写一遍的 max-w-[720px] 字面量。
     --container-* 是 Tailwind v4 的 max-w/w 命名空间 → 工具类 max-w-measure。 */
  --container-measure: 45rem;
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/__tests__/design-tokens.test.ts`
Expected: PASS(1 passed)

- [ ] **Step 5: 写失败测试 —— 护栏检测器自测**

Create `frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts`:

```ts
import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

// 对话流排版护栏。不逐组件断言 class(那种测试与实现同构、无信息量),
// 只禁止若干已被 token 取代的字面量在对话流链路上复活。
//
// chat.tsx 同时装着 transcript 和 composer —— 646/654/714/726 行的圆角与阴影
// 属于输入框,不归对话流卡片系统管。所以规则按文件裁剪,不一刀切。
const AGENTRE_DIR = path.resolve(__dirname, "..");

type RuleGroup = "type" | "measure" | "shadow" | "radius";

const RULES: { group: RuleGroup; pattern: RegExp; why: string }[] = [
  { group: "type", pattern: /text-\[9px\]/, why: "低于可读下限,用 text-meta" },
  { group: "type", pattern: /text-\[10px\]/, why: "低于可读下限,用 text-meta" },
  { group: "measure", pattern: /max-w-\[720px\]/, why: "用 max-w-measure" },
  { group: "measure", pattern: /max-w-\[580px\]/, why: "用 max-w-measure" },
  { group: "shadow", pattern: /shadow-sm/, why: "对话流卡片不带阴影" },
  { group: "radius", pattern: /rounded-md/, why: "对话流卡片统一 rounded-lg" },
];

// SCANNED:对话流渲染链路上的组件。新增 transcript 组件时必须加进来 ——
// 让「加进对话流」成为一个需要过目排版护栏的动作。
// skip:该文件豁免的规则组,每一项都要写清理由。
export const SCANNED: { file: string; skip?: RuleGroup[] }[] = [];

function violations(source: string, skip: RuleGroup[] = []): string[] {
  return RULES.filter(
    (r) => !skip.includes(r.group) && r.pattern.test(source),
  ).map((r) => `${r.pattern.source} —— ${r.why}`);
}

describe("transcript typography guard", () => {
  it("检测器能抓到违规源码", () => {
    expect(violations('<div className="text-[9px] rounded-md" />')).toEqual([
      "text-\\[9px\\] —— 低于可读下限,用 text-meta",
      "rounded-md —— 对话流卡片统一 rounded-lg",
    ]);
  });

  it("检测器尊重 skip", () => {
    expect(
      violations('<div className="rounded-md" />', ["radius"]),
    ).toEqual([]);
  });

  it("对话流组件不含被禁的排版字面量", () => {
    const found = SCANNED.flatMap(({ file, skip }) => {
      const source = fs.readFileSync(path.join(AGENTRE_DIR, file), "utf8");
      return violations(source, skip).map((v) => `${file}: ${v}`);
    });
    expect(found).toEqual([]);
  });
});
```

- [ ] **Step 6: 跑测试确认失败**

先临时把文件重命名成 `.bak` 之外的做法不需要 —— 直接跑:

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-typography-guard.test.ts`
Expected: 第一次跑就 PASS(3 passed)—— `SCANNED` 是空的,第三个用例平凡通过。**这是预期的**:护栏的 red 由后续每个任务往 `SCANNED` 加文件时产生。前两个用例(检测器自测)是本步真正的 red→green:在写出 `violations()` 之前它们无法通过。

若想验证检测器确实会红,临时把 `SCANNED` 改成 `[{ file: "code-block.tsx" }]` 跑一次,应看到 `max-w-[580px]` / `text-[10px]` / `rounded-md` 三条违规,然后**改回空数组**。

- [ ] **Step 7: 跑既有全量前端测试确认没打破别的**

Run: `cd frontend && pnpm test`
Expected: PASS(全绿。本步只加了 token 和新测试,没改任何组件)

- [ ] **Step 8: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/styles/globals.css \
           frontend/src/__tests__/design-tokens.test.ts \
           frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts \
  -m "💄 对话流: 加排版 token(text-prose/aux/meta + measure)与排版护栏骨架"
```

---

### Task 2: TranscriptCard 卡片原语

**Files:**
- Create: `frontend/src/components/agentre/transcript-card.tsx`
- Create: `frontend/src/components/agentre/__tests__/transcript-card.test.tsx`
- Modify: `frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts`(往 `SCANNED` 加 `transcript-card.tsx`)

**Interfaces:**
- Consumes: Task 1 的 `max-w-measure` / `text-meta`
- Produces:
  - `type TranscriptCardTone = "default" | "error" | "pending" | "done"`
  - `TranscriptCard(props: React.ComponentProps<"section"> & { tone?: TranscriptCardTone })`
  - `TranscriptCardHeader(props: React.ComponentProps<"button">)`
  - `TranscriptCardBody(props: React.ComponentProps<"div">)`
  - `TranscriptPill(props: React.ComponentProps<"span"> & { tone?: TranscriptCardTone })`

- [ ] **Step 1: 写失败测试**

Create `frontend/src/components/agentre/__tests__/transcript-card.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  TranscriptCard,
  TranscriptCardBody,
  TranscriptCardHeader,
  TranscriptPill,
} from "../transcript-card";

describe("TranscriptCard", () => {
  it("默认 tone 用中性边框 + measure 栏宽 + 无阴影", () => {
    render(<TranscriptCard data-testid="c">x</TranscriptCard>);
    const el = screen.getByTestId("c");
    expect(el.className).toContain("max-w-measure");
    expect(el.className).toContain("rounded-lg");
    expect(el.className).toContain("bg-card");
    expect(el.className).toContain("border-border");
    expect(el.className).not.toContain("shadow");
  });

  it("error tone 换成告警边框", () => {
    render(
      <TranscriptCard tone="error" data-testid="c">
        x
      </TranscriptCard>,
    );
    const el = screen.getByTestId("c");
    expect(el.className).toContain("border-status-error/40");
    expect(el.className).not.toContain("border-border");
  });

  it("调用方 className 能覆盖默认值", () => {
    render(
      <TranscriptCard className="rounded-none" data-testid="c">
        x
      </TranscriptCard>,
    );
    expect(screen.getByTestId("c").className).toContain("rounded-none");
    expect(screen.getByTestId("c").className).not.toContain("rounded-lg");
  });

  it("header 与 body 用同一套水平内边距", () => {
    render(
      <TranscriptCard>
        <TranscriptCardHeader data-testid="h">h</TranscriptCardHeader>
        <TranscriptCardBody data-testid="b">b</TranscriptCardBody>
      </TranscriptCard>,
    );
    expect(screen.getByTestId("h").className).toContain("px-3.5");
    expect(screen.getByTestId("b").className).toContain("px-3.5");
    expect(screen.getByTestId("b").className).toContain("border-t");
  });

  it("pill 用 text-meta,不再是 9px", () => {
    render(<TranscriptPill data-testid="p">完成</TranscriptPill>);
    const el = screen.getByTestId("p");
    expect(el.className).toContain("text-meta");
    expect(el.className).not.toMatch(/text-\[\d+px\]/);
  });

  it("done tone 的 pill 用 running 配色", () => {
    render(
      <TranscriptPill tone="done" data-testid="p">
        完成
      </TranscriptPill>,
    );
    expect(screen.getByTestId("p").className).toContain("bg-status-running-bg");
    expect(screen.getByTestId("p").className).toContain("text-status-running");
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-card.test.tsx`
Expected: FAIL —— `Failed to resolve import "../transcript-card"`

- [ ] **Step 3: 写实现**

Create `frontend/src/components/agentre/transcript-card.tsx`:

```tsx
import * as React from "react";

import { cn } from "@/lib/utils";

// 对话流卡片原语。此前 12 个卡片各写各的:3 种圆角、2 套阴影策略、4 种内边距,
// 相邻卡片互相打架。这里是这些样式的唯一定义处 —— 改样式只改这个文件。
export type TranscriptCardTone = "default" | "error" | "pending" | "done";

const cardToneClass: Record<TranscriptCardTone, string> = {
  default: "border-border",
  error: "border-status-error/40",
  pending: "border-primary",
  done: "border-status-running/50",
};

const pillToneClass: Record<TranscriptCardTone, string> = {
  default: "bg-muted text-muted-foreground",
  error: "bg-destructive-soft text-status-error",
  pending: "bg-status-waiting-bg text-status-waiting",
  done: "bg-status-running-bg text-status-running",
};

function TranscriptCard({
  tone = "default",
  className,
  ...props
}: React.ComponentProps<"section"> & { tone?: TranscriptCardTone }) {
  return (
    <section
      {...props}
      className={cn(
        "w-full max-w-measure overflow-hidden rounded-lg border bg-card",
        cardToneClass[tone],
        className,
      )}
    />
  );
}

function TranscriptCardHeader({
  className,
  ...props
}: React.ComponentProps<"button">) {
  return (
    <button
      type="button"
      {...props}
      className={cn(
        "flex w-full min-w-0 cursor-pointer items-center gap-2 px-3.5 py-2.5 text-left transition-colors hover:bg-muted/40",
        className,
      )}
    />
  );
}

function TranscriptCardBody({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      {...props}
      className={cn("border-t border-border px-3.5 py-3", className)}
    />
  );
}

function TranscriptPill({
  tone = "default",
  className,
  ...props
}: React.ComponentProps<"span"> & { tone?: TranscriptCardTone }) {
  return (
    <span
      {...props}
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-meta font-medium",
        pillToneClass[tone],
        className,
      )}
    />
  );
}

export {
  TranscriptCard,
  TranscriptCardBody,
  TranscriptCardHeader,
  TranscriptPill,
};
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-card.test.tsx`
Expected: PASS(6 passed)

- [ ] **Step 5: 把新文件加进护栏清单**

在 `transcript-typography-guard.test.ts` 里把 `SCANNED` 改成:

```ts
export const SCANNED: { file: string; skip?: RuleGroup[] }[] = [
  { file: "transcript-card.tsx" },
];
```

- [ ] **Step 6: 跑护栏确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-typography-guard.test.ts`
Expected: PASS(3 passed —— 新原语本身就是干净的)

- [ ] **Step 7: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/transcript-card.tsx \
           frontend/src/components/agentre/__tests__/transcript-card.test.tsx \
           frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts \
  -m "💄 对话流: 抽 TranscriptCard 卡片原语(统一圆角/阴影/内边距/栏宽)"
```

---

### Task 3: 正文层 —— message-row + markdown-text

**Files:**
- Modify: `frontend/src/components/agentre/message-row.tsx`
- Modify: `frontend/src/components/agentre/markdown-text.tsx`
- Modify: `frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts`
- Test(既有,需通过): `frontend/src/components/agentre/message-row.test.tsx`、`frontend/src/components/agentre/markdown-text.test.tsx`

**Interfaces:**
- Consumes: `text-prose` / `text-meta` / `max-w-measure`
- Produces: 正文 15/1.7 的行高**单一真源**在 `MarkdownText` 根节点。

- [ ] **Step 1: 先把两个文件加进护栏清单(制造 red)**

`SCANNED` 改成:

```ts
export const SCANNED: { file: string; skip?: RuleGroup[] }[] = [
  { file: "transcript-card.tsx" },
  { file: "message-row.tsx" },
  { file: "markdown-text.tsx" },
];
```

- [ ] **Step 2: 跑护栏确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-typography-guard.test.ts`
Expected: FAIL —— 至少包含
```
message-row.tsx: text-\[10px\] —— 低于可读下限,用 text-meta
message-row.tsx: max-w-\[720px\] —— 用 max-w-measure
markdown-text.tsx: rounded-md —— 对话流卡片统一 rounded-lg
```

- [ ] **Step 3: 改 message-row.tsx**

三处改动:

`message-row.tsx:57` 内容列 —— 换 measure token:
```
- <div className="flex min-w-0 max-w-[720px] flex-1 flex-col gap-1">
+ <div className="flex min-w-0 max-w-measure flex-1 flex-col gap-1">
```

`message-row.tsx:66-69` —— **删掉 `leading-[1.55]`**。它是死代码:`MarkdownText` 在自己根节点写了 `leading-relaxed`,后代选择器同级、后声明胜出,这条对所有文本消息从来没生效过。行高改由 `MarkdownText` 单点声明(Step 4)。
```
- <div data-selectable-text="true" className="flex flex-col gap-2 leading-[1.55]">
+ <div data-selectable-text="true" className="flex flex-col gap-2">
```

`message-row.tsx:73` footer 槽 —— 元信息层 10px mono subtle → 12px sans muted:
```
- <div className="mt-1 flex flex-wrap items-center gap-1.5 font-mono text-[10px] text-subtle-foreground">
+ <div className="mt-1 flex flex-wrap items-center gap-2 text-meta text-muted-foreground">
```

`message-row.tsx:116` 复制按钮:
```
- className="h-5 gap-1 px-1.5 text-[10px] text-muted-foreground"
+ className="h-6 gap-1 px-1.5 text-meta text-muted-foreground"
```
(`h-5` → `h-6`:12px 文字在 20px 高的按钮里会被裁到基线,必须同步抬高。)

- [ ] **Step 4: 改 markdown-text.tsx**

`markdown-text.tsx:365` 与 `:394`(`MarkdownText` 与 `StreamingMarkdown` 两个外壳,**必须同改**,否则流式与定稿态字号跳变):
```
- <div className="markdown-body break-words text-sm leading-relaxed">
+ <div className="markdown-body break-words text-prose">
```
(`text-prose` 自带 `--text-prose--line-height: 1.7`,不需要再写 `leading-*`。)

`markdown-text.tsx:182-199` 标题跟着抬一档,保持与 15px 正文的层级差:
```
- h1: "mt-3 mb-1 text-base font-semibold"      → "mt-4 mb-1.5 text-lg font-semibold"
- h2: "mt-3 mb-1 text-[15px] font-semibold"    → "mt-4 mb-1.5 text-base font-semibold"
- h3: "mt-2 mb-1 text-sm font-semibold"        → "mt-3 mb-1 text-prose font-semibold"
```

`markdown-text.tsx:241` 无 `<code>` 子节点的 `pre` 兜底分支:
```
- "my-2 overflow-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed"
+ "my-2 overflow-auto rounded-lg bg-muted p-3 font-mono text-aux"
```

`markdown-text.tsx:253` 表格:
```
- className={cn("w-full border-collapse text-xs", className)}
+ className={cn("w-full border-collapse text-aux", className)}
```

行内 `code`(`:174`)保持 `text-[0.85em]` —— 它是相对正文的比例,随 15px 一起放大,不是硬编码字号,护栏也不禁它。

- [ ] **Step 5: 跑护栏 + 两个既有测试**

Run:
```bash
cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-typography-guard.test.ts src/components/agentre/message-row.test.tsx src/components/agentre/markdown-text.test.tsx
```
Expected: PASS。

若 `message-row.test.tsx` / `markdown-text.test.tsx` 里有断言旧 class 的用例失败,**逐条看清楚再改断言** —— 断言的是本轮有意改掉的值才改,断言的是别的行为则说明改坏了,回去修实现。

- [ ] **Step 6: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/message-row.tsx \
           frontend/src/components/agentre/markdown-text.tsx \
           frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts \
  -m "💄 对话流: 正文升到 15/1.7,删掉被覆盖的死 leading,元信息层 10→12"
```

---

### Task 4: code-block.tsx

**Files:**
- Modify: `frontend/src/components/agentre/code-block.tsx`
- Modify: `frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts`

**Interfaces:**
- Consumes: `TranscriptCard`(Task 2)、`text-aux` / `text-meta` / `max-w-measure`
- Produces: 无新导出(`CodeBlock` 签名不变)

- [ ] **Step 1: 加进护栏清单制造 red**

`SCANNED` 追加 `{ file: "code-block.tsx" }`。

- [ ] **Step 2: 跑护栏确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-typography-guard.test.ts`
Expected: FAIL —— 含 `code-block.tsx: max-w-\[580px\]`、`code-block.tsx: text-\[10px\]`、`code-block.tsx: rounded-md`

- [ ] **Step 3: 改实现**

`code-block.tsx:77` 外壳换成原语。`CodeBlock` 渲染的是 `<div>`,`TranscriptCard` 渲染 `<section>` —— 语义上代码块确实是一个独立区块,换成 `section` 可接受,但**要确认调用方没有依赖 `div` 的类型**(`markdown-text.tsx:227` 只传 `className` / `language` / `children`,安全)。

```
- <div className={cn("w-full max-w-[580px] overflow-hidden rounded-md border border-border bg-secondary", className)} {...props}>
+ <TranscriptCard className={className} {...props}>
```
对应结尾 `</div>` 改 `</TranscriptCard>`,并在文件顶部加 `import { TranscriptCard } from "./transcript-card";`。

这一处同时修掉三件事:580 → measure(代码是最需要宽度的内容,此前却是全流最窄)、`bg-secondary` → `bg-card`(与所有其他卡片统一)、`rounded-md` → `rounded-lg`。

`code-block.tsx:82` 头部内边距与正文对齐(此前 `px-2.5` vs 正文 `px-3`,语言标签左偏 2px):
```
- <div className="flex items-center gap-2 border-b border-border px-2.5 py-1.5">
+ <div className="flex items-center gap-2 border-b border-border px-3.5 py-2">
```

`code-block.tsx:83` 语言标签:
```
- className="font-mono text-[10px] font-semibold text-muted-foreground"
+ className="text-meta font-semibold text-muted-foreground"
```

`code-block.tsx:91` 复制按钮:
```
- className="h-5 gap-1 px-1.5 text-[10px] text-muted-foreground"
+ className="h-6 gap-1 px-1.5 text-meta text-muted-foreground"
```

`code-block.tsx:104` 代码正文 —— 内边距与头部对齐,字号 12 → 13:
```
- className="overflow-auto px-3 py-2.5 font-mono text-xs leading-relaxed text-foreground"
+ className="overflow-auto px-3.5 py-3 font-mono text-aux text-foreground"
```

- [ ] **Step 4: 跑护栏 + 相关测试**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-typography-guard.test.ts src/components/agentre/markdown-text.test.tsx`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/code-block.tsx \
           frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts \
  -m "💄 对话流: CodeBlock 用 TranscriptCard(580→measure / bg-card / 头尾内边距对齐)"
```

---

### Task 5: thinking-block.tsx

**Files:**
- Modify: `frontend/src/components/agentre/thinking-block.tsx`
- Modify: `frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts`
- Test(既有,需通过): 搜 `thinking` 相关测试(`rg -l thinking frontend/src --glob '*.test.tsx'`)

**Interfaces:**
- Consumes: `TranscriptCard` / `TranscriptCardHeader` / `TranscriptPill`、`text-aux` / `text-meta`
- Produces: 无新导出

- [ ] **Step 1: 加进护栏清单制造 red**

`SCANNED` 追加 `{ file: "thinking-block.tsx" }`。

- [ ] **Step 2: 跑护栏确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-typography-guard.test.ts`
Expected: FAIL —— 含 `thinking-block.tsx: max-w-\[720px\]`?**不会** —— 该文件本来就没有 measure 约束(这正是 spec §3.3 要补的缺口之一)。实际红点是:该文件当前不含任何被禁字面量,护栏会**通过**。

因此本任务的 red 不来自护栏,而来自 Step 3 的行为测试。

- [ ] **Step 3: 写失败测试 —— measure 约束**

追加到 `frontend/src/components/agentre/__tests__/transcript-card.test.tsx` 末尾:

```tsx
import { ThinkingBlock } from "../thinking-block";

describe("ThinkingBlock 排版契约", () => {
  it("即使脱离 MessageRow 也受 measure 约束", () => {
    const { container } = render(
      <ThinkingBlock text="思考内容" streaming={false} />,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.className).toContain("max-w-measure");
  });
});
```

若 `ThinkingBlock` 的 props 与上面不符,**先读 `thinking-block.tsx` 的 props 类型再写**,不要猜。若它依赖 `TranscriptUIStateProvider` 等 context,按 `transcript-row-view` 既有测试的包裹方式补上。

- [ ] **Step 4: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-card.test.tsx`
Expected: FAIL —— `expected '...rounded-lg border border-border bg-card' to contain 'max-w-measure'`

- [ ] **Step 5: 改实现**

`thinking-block.tsx:108-110` 外壳换原语(顺带补上 measure):
```
- <div data-selectable-text="true" className="overflow-hidden rounded-lg border border-border bg-card">
+ <TranscriptCard data-selectable-text="true">
```
结尾 `</div>` → `</TranscriptCard>`,顶部 `import { TranscriptCard, TranscriptCardHeader, TranscriptPill } from "./transcript-card";`

`thinking-block.tsx:117-119` 头部换原语(内边距从 `px-3.5 py-2.5` 变成原语的同值,视觉不变):
```
- <button type="button" onClick={handleToggle} aria-expanded={expanded} aria-label={...} className="flex w-full cursor-pointer items-center gap-2 px-3.5 py-2.5 text-left hover:bg-muted/40">
+ <TranscriptCardHeader onClick={handleToggle} aria-expanded={expanded} aria-label={...}>
```
结尾 `</button>` → `</TranscriptCardHeader>`。

`thinking-block.tsx:137` 计时胶囊换原语:
```
- <span data-copyable-control-text="true" className="rounded bg-primary/10 px-1.5 py-0.5 font-mono text-2xs font-medium text-primary-text">
+ <TranscriptPill data-copyable-control-text="true" className="bg-primary-soft text-primary-text">
```
结尾 `</span>` → `</TranscriptPill>`。

`thinking-block.tsx:144` meta 文字:
```
- className="text-xs text-muted-foreground"
+ className="text-meta text-muted-foreground"
```

`thinking-block.tsx:175` 与 `:180`(流式 / 定稿两个分支,**必须同改**):
```
- "...px-3.5 py-3 text-xs italic leading-[1.55] text-muted-foreground"
+ "...px-3.5 py-3 text-aux italic text-muted-foreground"
```
(`text-aux` 自带 1.65 行高。`max-h-[132px] overflow-hidden` 保留 —— 那是流式截断,不是本轮否决的嵌套滚动。)

- [ ] **Step 6: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-card.test.tsx src/components/agentre/__tests__/transcript-typography-guard.test.ts`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/thinking-block.tsx \
           frontend/src/components/agentre/__tests__/transcript-card.test.tsx \
           frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts \
  -m "💄 对话流: ThinkingBlock 用 TranscriptCard,正文 12→13,补 measure 约束"
```

---

### Task 6: canonical-tool 卡片(raw / file-edit / file-write / agent-spawn / plan + hunk-renderer)

**Files:**
- Modify: `frontend/src/components/agentre/canonical-tool/raw/card.tsx`
- Modify: `frontend/src/components/agentre/canonical-tool/file-edit/card.tsx`
- Modify: `frontend/src/components/agentre/canonical-tool/file-edit/hunk-renderer.tsx`
- Modify: `frontend/src/components/agentre/canonical-tool/file-write/card.tsx`
- Modify: `frontend/src/components/agentre/canonical-tool/agent-spawn/card.tsx`
- Modify: `frontend/src/components/agentre/canonical-tool/plan/card.tsx`
- Modify: `frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts`
- Test(既有,需通过): `canonical-tool/registry.test.tsx` 及各卡片自带测试

**Interfaces:**
- Consumes: `TranscriptCard` / `TranscriptCardHeader` / `TranscriptCardBody` / `TranscriptPill`、`text-aux` / `text-meta`
- Produces: 无新导出

**注意:** 护栏清单里这些文件的路径要写成相对 `components/agentre/` 的形式,如 `canonical-tool/raw/card.tsx`。

- [ ] **Step 1: 加进护栏清单制造 red**

`SCANNED` 追加:
```ts
  { file: "canonical-tool/raw/card.tsx" },
  { file: "canonical-tool/file-edit/card.tsx" },
  { file: "canonical-tool/file-edit/hunk-renderer.tsx" },
  { file: "canonical-tool/file-write/card.tsx" },
  { file: "canonical-tool/agent-spawn/card.tsx" },
  { file: "canonical-tool/plan/card.tsx" },
```

- [ ] **Step 2: 跑护栏确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-typography-guard.test.ts`
Expected: FAIL —— 约 15 条违规,含各文件的 `text-\[9px\]` / `text-\[10px\]` / `max-w-\[720px\]` / `rounded-md`

- [ ] **Step 3: 逐文件改**

五个 card 的**共同改法**(它们的外壳字符串完全相同,`raw:110` / `file-edit:53` / `file-write:46` / `agent-spawn:207`,`plan:265` 少 font 部分):

```
- <section data-testid="..." aria-label={...} className={cn("w-full max-w-[720px] overflow-hidden rounded-md border bg-card font-mono text-xs", isError ? "border-status-error/40" : "border-border")}>
+ <TranscriptCard data-testid="..." aria-label={...} tone={isError ? "error" : "default"} className="font-mono text-aux">
```
`plan/card.tsx:265` 的 tone 映射:未决 → `"pending"`,已批准 → `"done"`,其余 `"default"`。

头部(`raw:118` 等):
```
- <button type="button" onClick={...} aria-expanded={expanded} className="flex w-full min-w-0 cursor-pointer items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-muted/40">
+ <TranscriptCardHeader onClick={...} aria-expanded={expanded}>
```

展开体(`raw:187-189`):
```
- <div data-selectable-text="true" className="flex flex-col gap-3 border-t border-border px-3 py-3">
+ <TranscriptCardBody data-selectable-text="true" className="flex flex-col gap-3">
```

**所有 `text-[9px]` 胶囊换成 `TranscriptPill`**(共 11 处):
- `raw/card.tsx:146` bg-running pill → `<TranscriptPill className="rounded-full border border-border">`(保留 pill 形状差异,tone 用默认)
- `raw/card.tsx:163` approved badge → `<TranscriptPill tone="done">`
- `raw/card.tsx:171` 状态 pill → `<TranscriptPill className={pillConfig.pillClassName}>`(保留既有 tone 映射逻辑,只换尺寸基座;**注意去掉 `tracking-[0.04em]`** —— 那是为了让 9px 大写字母不糊,12px 下不再需要,留着反而稀疏)
- `file-write/card.tsx:72,81`、`file-edit/card.tsx:90,97`、`hunk-renderer.tsx:24,29` 同法

`hunk-renderer.tsx` 的 `text-[11px]` 行号/gutter(`:20,51,65,95,98,102`)与 `text-[10px]` 增删计数(`:33,37`)→ 统一 `text-meta`。

`raw/card.tsx:270,274` 的 Section 标签:
```
- <span className="font-sans text-[11px] font-semibold tracking-wide text-muted-foreground">
+ <span className="font-sans text-meta font-semibold text-muted-foreground">
- <span className="font-mono text-[10px] text-subtle-foreground">
+ <span className="font-mono text-meta text-muted-foreground">
```

`agent-spawn/card.tsx` 的 `text-2xs`(`:251,260,274,283,373,377,488`)→ `text-meta`。

**保留不动:** `max-h-[120px]` / `max-h-[200px]` 嵌套滚动(spec §2 明确另开一轮)。

- [ ] **Step 4: 跑护栏 + canonical-tool 全部测试**

Run: `cd frontend && pnpm test -- src/components/agentre/canonical-tool src/components/agentre/__tests__/transcript-typography-guard.test.ts`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/canonical-tool \
           frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts \
  -m "💄 对话流: canonical-tool 卡片用 TranscriptCard,胶囊 9→12,正文 12→13"
```

---

### Task 7: 交互类卡片(tool-approval / tool-permission / user-ask / local-command)

**Files:**
- Modify: `frontend/src/components/agentre/tool-approval/card.tsx`
- Modify: `frontend/src/components/agentre/canonical-tool/tool-permission/card.tsx`
- Modify: `frontend/src/components/agentre/canonical-tool/user-ask/card.tsx`
- Modify: `frontend/src/components/agentre/local-command/card.tsx`
- Modify: `frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts`

**Interfaces:**
- Consumes: `TranscriptCard` / `TranscriptPill`、`text-aux` / `text-meta`
- Produces: 无新导出

这四个是 spec §1③ 里「完全没有 measure 约束 + 自带 `shadow-sm` + 用 `rounded-lg` 而非 `rounded-md`」的一组 —— 它们脱离 `MessageRow` 渲染时会撑破正文栏。

- [ ] **Step 1: 加进护栏清单制造 red**

`SCANNED` 追加:
```ts
  { file: "tool-approval/card.tsx" },
  { file: "canonical-tool/tool-permission/card.tsx" },
  { file: "canonical-tool/user-ask/card.tsx" },
  { file: "local-command/card.tsx" },
```

- [ ] **Step 2: 跑护栏确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-typography-guard.test.ts`
Expected: FAIL —— 四个文件各命中 `shadow-sm`;`tool-permission` / `user-ask` 另含 `rounded-md`

- [ ] **Step 3: 改实现**

`tool-approval/card.tsx:57`:
```
- className="rounded-md border bg-card text-card-foreground shadow-sm"
+ 换成 <TranscriptCard className="text-card-foreground">
```

`canonical-tool/tool-permission/card.tsx:123`:同上。

`canonical-tool/user-ask/card.tsx:219`:
```
- className="rounded-lg border border-border bg-card text-foreground shadow-sm outline-none"
+ 换成 <TranscriptCard className="text-foreground outline-none">
```

`local-command/card.tsx:99`(它是一个可点的行,不是可折叠卡,保留 `flex items-center` 布局,只换外壳样式):
```
- className="flex cursor-pointer items-center gap-2 rounded-lg border border-border bg-card px-3.5 py-2 text-foreground shadow-sm transition-colors hover:bg-accent/40"
+ className="flex cursor-pointer items-center gap-2 rounded-lg border border-border bg-card px-3.5 py-2.5 text-foreground transition-colors hover:bg-accent/40 w-full max-w-measure"
```

字号统一:
- `user-ask/card.tsx`:问题 `text-[15px]` → `text-prose`(同值,但走 token);选项 `text-[13px]` → `text-aux`;`text-2xs` 徽章(`:243,326,334,342,349,427`)→ `text-meta`;输入 `text-sm` 保持
- `tool-approval/card.tsx` / `tool-permission/card.tsx` 的 `text-xs` → `text-aux`
- `local-command/card.tsx` 的 `text-2xs`(`:67,125,143,158,163`)→ `text-meta`;命令 `text-xs`(`:120,150`)→ `text-aux`

- [ ] **Step 4: 跑护栏 + 相关测试**

Run: `cd frontend && pnpm test -- src/components/agentre/tool-approval src/components/agentre/local-command src/components/agentre/canonical-tool src/components/agentre/__tests__/transcript-typography-guard.test.ts`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/tool-approval \
           frontend/src/components/agentre/local-command \
           frontend/src/components/agentre/canonical-tool \
           frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts \
  -m "💄 对话流: 交互类卡片补 measure 约束,去 shadow-sm,字号并入 token"
```

---

### Task 8: 行视图与容器(transcript-row-view / chat / chat-panel)

**Files:**
- Modify: `frontend/src/components/agentre/transcript-row-view.tsx`
- Modify: `frontend/src/components/agentre/chat.tsx`
- Modify: `frontend/src/components/agentre/chat-panel.tsx`
- Modify: `frontend/src/components/agentre/__tests__/chat.test.tsx`(两条既有断言,见下)
- Modify: `frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts`

**Interfaces:**
- Consumes: `TranscriptCard`、全部 token
- Produces: 纵向节奏常量(消息间 `pb-7` / 块间 `pb-2.5`)

- [ ] **Step 1: 加进护栏清单制造 red**

`SCANNED` 追加。**注意 `chat.tsx` 必须带 skip** —— 它同时装着 composer,646/654/714/726 行的圆角与阴影属于输入框:
```ts
  { file: "transcript-row-view.tsx" },
  // chat.tsx 同时装着 transcript 和 composer:646/654/714/726 的圆角与阴影
  // 属于输入框,不归对话流卡片系统管,只受字号与 measure 约束。
  { file: "chat.tsx", skip: ["shadow", "radius"] },
```

- [ ] **Step 2: 跑护栏确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-typography-guard.test.ts`
Expected: FAIL —— 含 `transcript-row-view.tsx` 的 `text-\[10px\]` / `max-w-\[720px\]` / `rounded-md`,`chat.tsx` 的 `max-w-\[720px\]`

- [ ] **Step 3: 改 transcript-row-view.tsx**

| 行 | 改动 |
| --- | --- |
| `:109` | 用户头像 `text-[11px]` → `text-meta` |
| `:120` | 时间戳 `font-mono text-[10px] text-muted-foreground` → `text-meta text-muted-foreground` |
| `:215` | MessageMeta tooltip `text-[11px]` → `text-meta` |
| `:235` | 重新生成按钮 `text-[10px]` → `text-meta`,`h-5` → `h-6` |
| `:306` | 编辑按钮 `text-[10px]` → `text-meta`,`h-5` → `h-6` |
| `:321` | ErrorCard `max-w-[720px]` → `max-w-measure`,`rounded-md` → `rounded-lg` |
| `:327` | ErrorCard 正文 `text-xs` → `text-aux` |
| `:358` | RetryNotice `max-w-[720px]` → `max-w-measure`,`rounded-md` → `rounded-lg` |
| `:366,370,375,379` | RetryNotice 的 `text-xs` → `text-aux`,`text-[10px]` / `text-[11px]` → `text-meta` |
| `:421` | CompactingIndicator `text-xs` → `text-aux` |
| `:446` | ImageBlockView `rounded-md` → `rounded-lg` |
| `:586` | unknown-block `rounded-md` → `rounded-lg`,`text-xs` → `text-aux` |
| `:702-704` | 续行 `max-w-[720px]` → `max-w-measure` |
| `:707` | 续行 `leading-[1.55]` 删掉(与 Task 3 的主消息行一致,行高由 `MarkdownText` 单点声明) |
| `:713` | 续行 footer `font-mono text-[10px] text-subtle-foreground` → `text-meta text-muted-foreground`,`gap-1.5` → `gap-2` |

**发现但不修:** `:358` 用了 `border-status-warning/45`,但 token 表里只有 `--status-waiting`,没有 `--status-warning` —— 这个 class 很可能从来没生效过。**这是本轮范围外的既有缺陷,不要顺手改**(AGENTS.md 约束 4)。在 Task 10 的收尾报告里向用户单独提出来。

- [ ] **Step 4: 改 chat.tsx**

| 行 | 改动 |
| --- | --- |
| `:77` | `ToolCall` 外壳换 `TranscriptCard`(`max-w-[720px]` + `rounded-md` + `px-3 py-2.5` 一并被原语接管;保留 `flex flex-col gap-1.5` 作为 className) |
| `:82` | ToolCall 头部 `font-mono text-xs` → `font-mono text-aux` |
| `:83` | ToolCall 状态行 `text-[11px]` → `text-meta` |
| `:123` | `ApprovalGate` 外壳换 `TranscriptCard` |
| `:132,134` | ApprovalGate 标题/描述 `text-xs` → `text-aux` |
| `:636` | composer `px-5` → `px-7`,与 transcript 左右边缘对齐 |
| `:1382` | 纵向节奏:`"pb-5"` → `"pb-7"`(消息间 20→28),`"pb-2"` → `"pb-2.5"`(块间 8→10) |

同时把 `:1379-1380` 的注释更新成新值(注释写着 `pb-5` / `pb-2`,不改会变成假注释):
```
-  // 行间距:消息末行 pb-5(消息间距),消息内分片行 pb-2(block 间距)。padding
+  // 行间距:消息末行 pb-7(消息间距),消息内分片行 pb-2.5(block 间距)。padding
```

`chat.tsx:1390` 的注释 `不再加 max-w-4xl —— 内部 ChatMessage 已经 cap 在 720px` 改成 `…已经 cap 在 max-w-measure`。

- [ ] **Step 5: 改 chat-panel.tsx**

`:1948` transcript 容器保持 `px-7`(已与新 composer 一致),但纵向留白跟随节奏:
```
- className="min-h-0 flex-1 overflow-auto px-7 py-5"
+ className="min-h-0 flex-1 overflow-auto px-7 py-6"
```

`:1920-1921` 的注释若声称「输入框宽度 = transcript 宽度」,现在**才真的成立**了 —— 保留即可,不需要改。

- [ ] **Step 6: 更新两条既有断言**

`__tests__/chat.test.tsx:1488` 用例名与断言:
```
- it("renders the content column under max-w-[720px] inside the article", () => {
+ it("renders the content column under max-w-measure inside the article", () => {
```
用例体里 `toContain("max-w-[720px]")` → `toContain("max-w-measure")`;注释里的「统一…为 720px」改成「统一走 --container-measure token」。

`__tests__/chat.test.tsx:1485`:
```
- expect(metaContainer.className).toContain("text-subtle-foreground");
+ expect(metaContainer.className).toContain("text-muted-foreground");
```

- [ ] **Step 7: 跑护栏 + chat 测试**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__ src/components/agentre/chat-panel-scroll-state.test.ts`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/transcript-row-view.tsx \
           frontend/src/components/agentre/chat.tsx \
           frontend/src/components/agentre/chat-panel.tsx \
           frontend/src/components/agentre/__tests__/chat.test.tsx \
           frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts \
  -m "💄 对话流: 行视图/容器并入 token,节奏 20→28 / 8→10,输入框与正文对齐"
```

---

### Task 9: 全宽漏网组件(compact-boundary-divider / compact-history-fold / auto-trigger-banner)

**Files:**
- Modify: `frontend/src/components/agentre/compact-boundary-divider.tsx`
- Modify: `frontend/src/components/agentre/compact-history-fold.tsx`
- Modify: `frontend/src/components/agentre/auto-trigger-banner.tsx`
- Modify: `frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts`

**Interfaces:**
- Consumes: `max-w-measure` / `text-aux`
- Produces: 无

这三个渲染在 `MessageRow` **之外**(`transcript-row-view.tsx:577`、`chat.tsx:1396`),完全没有宽度约束 —— 宽窗口下会横跨整屏切过 720px 的正文列,是视觉上最刺眼的一处不齐。

- [ ] **Step 1: 写失败测试**

追加到 `frontend/src/components/agentre/__tests__/transcript-card.test.tsx`:

```tsx
import { CompactBoundaryDivider } from "../compact-boundary-divider";

describe("脱离 MessageRow 渲染的块也守 measure", () => {
  it("CompactBoundaryDivider 受 measure 约束", () => {
    const { container } = render(<CompactBoundaryDivider />);
    const root = container.firstElementChild as HTMLElement;
    expect(root.className).toContain("max-w-measure");
  });
});
```

若组件需要 props,**先读文件再写测试**,不要猜签名。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/transcript-card.test.tsx`
Expected: FAIL —— 断言 `max-w-measure` 不存在

- [ ] **Step 3: 改实现**

三个组件的根节点各加 `w-full max-w-measure`:
- `compact-boundary-divider.tsx:38`(根容器;`:42,60` 的 `flex-1` 分隔线在受限容器内自然收窄,不用动)
- `compact-history-fold.tsx:26`
- `auto-trigger-banner.tsx:20`

同时字号并入 token:三处 `text-xs` → `text-aux`;`compact-boundary-divider.tsx:43` 的 chip `text-xs` → `text-meta`。

- [ ] **Step 4: 加进护栏清单并跑测试**

`SCANNED` 追加:
```ts
  { file: "compact-boundary-divider.tsx" },
  { file: "compact-history-fold.tsx" },
  { file: "auto-trigger-banner.tsx" },
```

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/compact-boundary-divider.tsx \
           frontend/src/components/agentre/compact-history-fold.tsx \
           frontend/src/components/agentre/auto-trigger-banner.tsx \
           frontend/src/components/agentre/__tests__/transcript-card.test.tsx \
           frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts \
  -m "💄 对话流: 全宽漏网组件补 measure 约束"
```

---

### Task 10: 虚拟化测高校准 + 全量门禁

**Files:**
- Modify: `frontend/src/components/agentre/chat.tsx:1155-1165`(`estimateSize` 兜底 `132`)、`:1368`(`rows.length * 48`)
- 可能 Modify: `frontend/src/components/agentre/transcript-rows.ts` 里的 `estimateRowSize`

**Interfaces:**
- Consumes: 前 9 个任务的全部改动
- Produces: 无

**这是本轮唯一有真实回归风险的任务。** 正文 14→15、行高 1.625→1.7、消息间距 20→28 之后,虚拟化的行高估值系统性偏小,会导致滚动条长度跳变、滚到底部时位置漂移。

- [ ] **Step 1: 读 `estimateRowSize` 摸清估值构成**

Run: `rg -n "estimateRowSize" frontend/src/components/agentre/`

读懂它按 row 类型给出的各档估值。**不要凭感觉乘系数** —— 先算清楚:正文行高从 `14 × 1.625 = 22.75px` 变成 `15 × 1.7 = 25.5px`,单行 +12%;消息间距 +8px。

- [ ] **Step 2: 按上述比例调整估值**

文本类 row 的估值 × 1.12 并加上间距增量;兜底 `132` → `148`;`rows.length * 48` → `rows.length * 54`。

- [ ] **Step 3: 实机验证(不能只跑单测)**

```bash
cd /Users/codfrm/Code/agentre/agentre && make dev
```
打开一个有 100+ 条消息的长会话,检查:
1. 快速滚到底,滚动条不跳
2. 中途展开/折叠工具卡,上方内容不位移
3. 流式输出时视口跟随不抖

**若观察到跳动,回到 Step 2 继续调,不要跳过这步。** 这是本任务存在的唯一理由。

- [ ] **Step 4: 全量门禁**

必须看**真实 exit code**,不要用 `| tail` 吞掉(`make … | tail` 会吞 make 的退出码):

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm test;        echo "vitest exit=$?"
pnpm exec tsc -b; echo "tsc exit=$?"
pnpm lint;        echo "eslint exit=$?"
```
Expected: 三个 exit 全为 0。

- [ ] **Step 5: 后端门禁(确认没误伤)**

```bash
cd /Users/codfrm/Code/agentre/agentre && make test-backend; echo "go exit=$?"
```
Expected: exit=0(本轮不该动 Go,这步是确认没误伤)

- [ ] **Step 6: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit frontend/src/components/agentre/chat.tsx \
           frontend/src/components/agentre/transcript-rows.ts \
  -m "💄 对话流: 校准虚拟化行高估值,匹配新的正文字号与间距"
```

- [ ] **Step 7: 向用户报告**

汇报三件事:
1. 全量门禁的**真实 exit code**(不要只说「全绿」)
2. Step 3 实机验证看到的现象
3. **范围外发现,请用户定夺**:`transcript-row-view.tsx:358` 使用 `border-status-warning/45`,但 token 表只有 `--status-waiting`,没有 `--status-warning` —— 该 class 疑似从未生效。本轮按「不做 drive-by」未修。

---

## 自查记录

- **Spec 覆盖:** §3.1 字号阶梯 → Task 1(token)+ 3/4/5/6/7/8/9(逐组件);§3.2 行高单一真源 → Task 3(`message-row`)+ Task 8(续行);§3.3 版面 → Task 1(token)+ 4/5/7/9(补齐 8 个漏网组件)+ Task 8(边缘对齐、纵向节奏);§3.4 卡片原语 → Task 2 + 4/5/6/7/8;§4.1 原语单测 → Task 2;§4.2 护栏 → Task 1 建立、每个任务增量扩清单;§4.4 虚拟化估值 → Task 10。无遗漏。
- **命名一致性:** `TranscriptCard` / `TranscriptCardHeader` / `TranscriptCardBody` / `TranscriptPill` / `TranscriptCardTone` 在 Task 2 定义,Task 4–9 引用一致;token 名 `text-prose` / `text-aux` / `text-meta` / `max-w-measure` 全篇一致。
- **护栏 red 的诚实说明:** Task 5(thinking-block)与 Task 9 的 red 不来自护栏(那些文件本来就不含被禁字面量,缺的是 measure 约束),已在任务内改用行为测试制造 red,并在 Task 5 Step 2 明确写出「护栏此处会通过,这是预期的」。
