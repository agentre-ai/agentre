# `!命令`(本地命令)输出:完成后折叠 + 高度自适应

日期:2026-07-09
范围:`agentre/` 桌面端前端,仅改本地命令(`!cmd`)输出卡片的呈现;不动后端 `TerminalRunCommand` 流式链路。

## 背景 / 痛点

用户输入 `!<cmd>` 会在对话流里跑一条本地 shell 命令(不发送给 AI),
由 `LocalCommandCard`(`local-command/card.tsx`)+ 只读 xterm(`output-terminal.tsx`)渲染。

当前问题:输出框是**固定 `h-44`(176px)的近黑终端画布**,命令跑完后**原样常驻**在对话里。
一条 `git status`(两行)或空输出(`touch foo`)也会留下一大块黑洞,视觉沉重、割裂对话阅读流。

## 目标

1. 命令**完成后自动折叠**成一行摘要,黑框不再常驻;点击可重新展开看完整输出。
2. 展开(及运行中)时输出框**高度自适应内容,但封顶**:空输出不留黑洞,短输出矮框,长输出封顶可滚。
3. 不改变 ANSI 颜色保真(继续用 xterm,不退化成 `<pre>`),不改后端。

## 已确认的产品决策

- **全部一视同仁折叠**:成功 / 失败 / 已停止,只要 `status !== "running"` 都折叠成一行;错误输出不自动展开(想看点一下)。
- **摘要额外显示耗时**(不显示行数、不显示末行预览)。
- 折叠行**去掉** "本地命令" chip 与 "不发送给 AI",两者只在展开态 header 保留。
- max 高度**保持今天的 176px**(只会变矮不会变高)。

## 三态

一张 `LocalCommandCard` 有三个呈现态,由 `status` + 每条目的 `expanded` 覆盖态派生:

| 态 | 触发 | 呈现 |
| --- | --- | --- |
| **运行中** | `status === "running"` | header + 终端展开,**高度自适应**(min ~3 行,随输出增长到封顶后滚动并跟随末行)。保留 Stop / 在终端中打开 两个 action。 |
| **折叠**(完成后默认) | `status !== "running"` 且未手动展开 | **只有一行摘要**,无黑框。命令 + 状态 pill(点 + 文案 + 退出码) + 耗时 + chevron + dismiss ✕。 |
| **展开**(完成后手动点开) | 用户切换 | 完整 header + 终端,**同样高度自适应且封顶可滚**;无运行中 action。 |

折叠是**从 `status` 派生的默认**,不是命令式的"现在折叠":运行→完成时因为默认态翻转而自动折叠。

## 折叠摘要行

```
[⧉]  git status   ● 完成 · 退出码 0        1.2s   ⌄   ✕
```

- 命令:`font-mono` + `truncate` 单行省略;仍带 `data-selectable-text`。
- 状态 pill 复用现有 `STATUS_CONFIG`(点颜色 + 文案 + `showExitCode` 时的退出码)。
- **耗时**:右对齐 muted,格式 `420ms` / `1.2s` / `1m 3s`,由纯函数 `formatDuration(ms)` 产出。
- chevron 表示可展开;**点击整行切换展开/折叠**,用现有 `shouldIgnoreClickForSelection` 守卫,
  这样选中命令文本复制不会误触发折叠(遵循 select-to-copy 约定)。
- dismiss ✕ 保留(仅完成后可见,与现状一致)。

## 高度自适应(核心"去黑洞")

- 容器高度 = `clamp(usedRows, minRows, maxRows) × cellHeight + padding`。
- `maxRows` 对应当前 176px(`h-44`)——footprint **永不超过今天**,只会变矮。
- `minRows` ~3(运行中即时可见一块区域,避免闪现);折叠态根本不渲染终端。
- 高度计算抽成**纯函数** `computeTerminalHeight({ usedRows, cellHeight, minRows, maxRows, padding })`,
  可在 happy-dom 单测(该环境拿不到 xterm canvas 度量)。`cellHeight` 从 xterm 渲染维度读取,带兜底值。
- 运行中随每次 write 重算高度,超过封顶则视口固定在 `maxRows` 并跟随末行(auto-scroll to bottom)。
- **空输出**(如 `touch foo`):折叠一行已足够;若展开则渲染 muted 占位 **"无输出"**,不给空黑框。

## Store 改动(`stores/local-commands-store.ts`)

- 新增 `finishedAt`:在 `finish()` 里 `Date.now()` 打点 → `duration = finishedAt − createdAt`。
  (`createdAt` 是条目创建时刻,含极小启动开销,可接受。)
- 新增 `expanded?: boolean` 覆盖态 + `toggleExpanded(id)` action。
  派生:`isCollapsed = entry.expanded === undefined ? entry.status !== "running" : !entry.expanded`。
  存在 store(而非组件本地 state),这样卡片懒挂载/滚出视口卸载后切换态不丢。

## i18n(`zh-CN` + `en` 同步)

`localCommand` 下新增:
- `expand` / `collapse`:chevron 的 aria-label / title。
- `noOutput`:"无输出" / "No output"。

耗时数字本身是动态输出,不进 `t(...)`。

## 边界 / 边角

- **空输出**:见上,展开显示 "无输出" 占位。
- **失败/停止**:同样折叠(已确认);状态 pill 颜色区分(`failed` 红 / `stopped` 灰),退出码照显。
- **运行中→完成的滚动锚定**:折叠会缩短卡片高度。transcript 是块级虚拟化 + 跟随末行,
  折叠不应把滚动位置跳飞——实现时验证折叠/展开不破坏 follow-to-bottom;必要时避免布局位移动画。
- **长命令文本**:折叠行 `truncate`;展开 header 保持现有换行策略。

## 测试接缝(TDD)

- `formatDuration(ms)` 纯函数:`< 1s → "420ms"`、`1–60s → "1.2s"`、`≥ 60s → "1m 3s"`,含边界。
- `computeTerminalHeight(...)` 纯函数:空/短/等于封顶/超封顶 各断言。
- store:`finish()` 打 `finishedAt`;`toggleExpanded` 翻转;`isCollapsed` 派生(默认完成即折叠)。
- `card.test.tsx`:完成后默认渲染折叠摘要(含耗时、无黑框);点击展开出终端;dismiss;
  折叠行不含 "本地命令" chip / "不发送给 AI";select-to-copy 守卫不误触发折叠。
- `output-terminal.test.tsx`:空输出展开显示 "无输出";高度随 `output` 变化(以纯函数为主验证)。

## 涉及文件

- `frontend/src/components/agentre/local-command/card.tsx`
- `frontend/src/components/agentre/local-command/output-terminal.tsx`
- `frontend/src/components/agentre/local-command/format-duration.ts`(新增)
- `frontend/src/stores/local-commands-store.ts`
- `frontend/src/i18n/locales/{zh-CN,en}/common.json`
- 对应测试:`card.test.tsx` / `output-terminal.test.tsx` / `format-duration.test.ts`(新增) / `local-commands-store.test.ts`

无后端改动。
