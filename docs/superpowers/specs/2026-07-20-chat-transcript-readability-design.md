# 对话流可读性重构 — 设计

- 日期:2026-07-20
- 分支:develop/wyz
- 范围:frontend 纯视觉层(字号阶梯 / 版面 measure / 卡片原语),不改数据流、不改后端
- Pencil 稿:`agentre.pen` → 顶层 frame「对话流可读性重构 — 现状 vs 改版」(左现状 / 右改版)

## 1. 问题

对话流「读着累」不是审美问题,是三处结构性缺陷。以下均为实测,非推断:

**① 字号阶梯塌了。** 全流用了 8 个字号:16 / 15 / 14 / 13 / 12 / 11 / 10 / 9,其中 5 级挤在 9–12px 的 3px 区间里,没有可感知的层级差,只有噪声。承载真实信息的元素恰恰落在最小档:

- 时间戳、复制、编辑、重新生成、token 计数 —— 全部 `font-mono text-[10px] text-subtle-foreground`,是全 app 最小且对比度最低的文字(`message-row.tsx:73`、`transcript-row-view.tsx:120,235,306`、`code-block.tsx:83,91`)。
- 工具卡状态胶囊(running / error / approved)—— `text-[9px]`,11 处(`raw/card.tsx:146,163,171`、`file-write/card.tsx:72,81`、`file-edit/card.tsx:90,97`、`hunk-renderer.tsx:24,29`)。

**② 行高声明重复且互相覆盖。** `MessageRow` 在内容列写了 `leading-[1.55]`(`message-row.tsx:68`),`MarkdownText` 又在自己根节点写 `leading-relaxed`(`markdown-text.tsx:365`)。后者胜出 —— **`leading-[1.55]` 对所有文本消息都是死代码**,只对非 markdown 子节点生效。全流共 5 种行高(1.0 / 1.375 / 1.4 / 1.55 / 1.625)。

**③ `max-w-[720px]` 是 9 个文件里的魔法字面量,且 8 个组件没遵守。** 没有 token、没有常量。`CodeBlock` 自己写 `max-w-[580px]` 且用 `bg-secondary`(其他卡都是 `bg-card`),造成右边缘参差;`CompactBoundaryDivider` / `AutoTriggerBanner` 渲染在 `MessageRow` 之外、完全无约束,在宽窗口下横跨整屏切过 720px 的正文列。

**④ 卡片没有共用原语。** 相邻卡片之间:3 种圆角(`rounded-md` / `rounded-lg`)、2 套阴影策略(有 / 无 `shadow-sm`)、4 种内边距(`px-2.5` / `px-3` / `px-3.5` / `px-4`)。`CodeBlock` 头部 `px-2.5` 与正文 `px-3` 不一致,语言标签比代码左偏 2px。

**⑤ transcript 与输入框左边缘对不齐。** transcript `px-7`(`chat-panel.tsx:1948`),composer `px-5`(`chat.tsx:636`),差 8px —— 尽管 `chat-panel.tsx:1920` 的注释声称「输入框宽度 = transcript 宽度」。

## 2. 已明确排除的方案

以下在设计过程中评估并被用户否决,记录以免后续重新翻案:

- **栏居中**(760px 居中于滚动区):否决,保持贴左。
- **用户消息灰底块**(`bg-muted` 包裹用户发言):否决,用户与 assistant 保持同款扁平渲染。
- **设置项**(聊天字号 / 密度可调):否决,先把默认值做对。
- **工具卡嵌套滚动**(`max-h-[120px]` / `max-h-[200px]` 劫持滚轮):**另开一轮**。它是行为改动而非排版,且去掉 max-h 会让长输出的卡撑得很长、影响虚拟化测高,需要单独想方案(如「展开全文 / 折叠」)。本轮 diff 保持纯视觉,便于复审。

## 3. 设计

### 3.1 字号阶梯 — 8 级压到 4 级

| 层级 | token | 现状 | 改版 | 用途 |
| --- | --- | --- | --- | --- |
| 正文 | `text-prose` | 14 / 1.625 | **15 / 1.7** | markdown body、用户消息 |
| 标题行 | `text-sm` | 14 | 14(不变) | 消息名字行、卡片标题、思考头 |
| 次要 | `text-aux` | 12 | **13 / 1.65** | 工具卡正文、代码块、思考正文、表格 |
| 元信息 | `text-meta` | 10 mono subtle / 9 | **12 sans muted** | 时间戳、复制/编辑/重新生成、token、语言标签、状态胶囊、行号 |

新 token 走 Tailwind v4 的 `--text-*` 约定加在 `@theme inline` 里(`--text-prose` / `--text-aux` / `--text-meta`,各带 `--text-*--line-height`)。
命名刻意避开 `secondary` / `muted` 等已被 `--color-*` 占用的词 —— 那会让 `text-secondary` 同时是字号和文字颜色,产生歧义。

markdown 标题跟着抬一档,保证与 15px 正文拉开:h1 16→**18**、h2 15→**16**、h3 14→**15**。

`text-[9px]` 与 `text-[10px]` 在 transcript 组件内**全部消失**,由护栏测试锁死(§4.2)。
元信息层同时从 `font-mono` + `subtle-foreground` 改为 `font-sans` + `muted-foreground` —— mono 在 10px 下字面宽度不齐且 `subtle-foreground` 对比度不足,两个问题叠加。

`--text-2xs`(11px)保留在 token 表中,但 transcript 内不再使用(其他页面仍在用,不动)。

### 3.2 行高单一真源

删除 `message-row.tsx:68` 的 `leading-[1.55]`(死代码)。行高只在两处声明:

- markdown 根:`leading-[1.7]`
- 次要 / mono 内容:`leading-[1.65]`

`transcript-row-view.tsx:707` 的续行 `leading-[1.55]` 同步对齐到 1.7,与主消息行一致。

### 3.3 版面

- **新增 `--measure: 720px`**,在 `@theme inline` 暴露为 `--container-measure`,得到 Tailwind 工具类 `max-w-measure`。替换全部 9 处 `max-w-[720px]` 字面量。
- **补齐 8 个未遵守 measure 的组件**:`CodeBlock`(580 → measure)、`ToolApprovalCard`、`ToolPermissionCard`、`LocalCommandCard`、`UserAskCard`、`ThinkingBlock`、`CompactBoundaryDivider`、`AutoTriggerBanner`。
  - 注:`ThinkingBlock` / `UserAskCard` / `ToolApprovalCard` 作为 `MessageRow` 子节点时已继承 720 上限,显式加 `max-w-measure` 是为了它们在 `MessageRow` 之外渲染时也成立(LSP:实现必须无条件满足契约)。
- **左右边缘对齐**:composer `px-5` → `px-7`,与 transcript 一致。
- **纵向节奏**:`chat.tsx:1379-1384` 的 `rowWrapperPad` —— 消息间 `pb-5`(20)→ `pb-7`(28),块间 `pb-2`(8)→ `pb-2.5`(10)。

### 3.4 卡片原语

新增 `components/agentre/transcript-card.tsx`,导出:

- `TranscriptCard` — 卡片外壳:`w-full max-w-measure overflow-hidden rounded-lg border bg-card`,边框色由 `tone` prop 决定(`default` / `error` / `pending` / `done`),**不带阴影**。
- `TranscriptCardHeader` — `flex w-full min-w-0 items-center gap-2 px-3.5 py-2.5 text-left`。
- `TranscriptCardBody` — `border-t border-border px-3.5 py-3`。
- `TranscriptPill` — `rounded px-1.5 py-0.5 text-meta`,配色由 `tone` 决定。

改造为使用该原语的卡片:`raw` / `file-edit` / `file-write` / `agent-spawn` / `plan` / `tool-approval` / `tool-permission` / `local-command` / `user-ask` / `thinking-block` / `code-block` / `chat.tsx` 的 `ToolCall` 与 `ApprovalGate`。

收敛结果:圆角 3 种 → **1 种**(`rounded-lg`);阴影 2 套 → **0**;内边距 4 套 → **1 套**。
`CodeBlock` 额外:`bg-secondary` → `bg-card`,头部内边距 `px-2.5` → `px-3.5` 与正文对齐。

## 4. 测试策略

纯样式改动不适合逐组件断言 class 字符串 —— 那种测试与实现同构、无信息量、且每次微调都要改。测试打在**共享原语**和**护栏**两层。

### 4.1 原语单测(`transcript-card.test.tsx`)

`TranscriptCard` / `TranscriptCardHeader` / `TranscriptCardBody` / `TranscriptPill` 的渲染测试:给定 tone,断言输出 canonical class 集合。这是唯一断言具体 class 的地方,因为它就是这些 class 的**定义处**。

### 4.2 护栏测试(`__tests__/transcript-typography-guard.test.ts`)

扫描 transcript 相关组件源码,命中以下任一模式即失败:

| 禁止模式 | 理由 |
| --- | --- |
| `text-[9px]` / `text-[10px]` | 低于可读下限 |
| `max-w-[720px]` / `max-w-[580px]` | 应使用 `max-w-measure` |
| `shadow-sm`(限 transcript 卡片文件) | 卡片不带阴影 |
| `rounded-md`(限 transcript 卡片文件) | 圆角统一 `rounded-lg` |

扫描范围以显式文件清单声明(不用通配),新增 transcript 组件时需要主动加入 —— 这是有意的:让「加进对话流」成为一个需要过目护栏的动作。

**该测试必须先红**:当前上述字面量全部在仓库里,测试跑起来就失败;实现完成后转绿。这满足 Red→Green。

### 4.3 既有测试

`markdown-text.test.tsx`、`message-row.test.tsx`、`transcript-rows.test.ts`、`chat-panel.test.tsx` 全绿。
`i18n.test.ts` 全绿(本轮不新增可见文案)。

### 4.4 已知需要复核的连带影响

- `chat.tsx:1159` 虚拟化行高估值 `132`、`chat.tsx:1368` 兜底 `rows.length * 48` —— 正文与间距变大后估值偏小。**实现时需实测调整**,否则滚动条长度跳变。这是本轮唯一有回归风险的点。

## 5. 不做的事

- 不加设置项(字号 / 密度可调)。
- 不动 `internal/` 任何 Go 代码。
- 不做 drive-by:不重排 import、不改无关组件、不顺手改文案。
- 不动 `--text-2xs` 在 transcript 之外的用法。
