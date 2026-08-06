# 会话文件面板：目录树 + 打开文件按钮

> Status: Draft
> Owner: chat experience / frontend
> Last updated: 2026-08-06

**Objective:** 把会话「文件」面板从扁平列表升级为**可折叠的目录树**，把「编辑次数」角标替换为 **+/− 行数（绿/红）** 的 diff 标注，并在**本地会话**的每个文件行上提供一个「用系统默认应用打开」的图标按钮。

**Hard invariants:**

1. **行点击跳转不回归：** 点击文件行仍跳转到该文件 `lastTurn` 对应轮次的 user 消息（现状为 `FilesView` 行点击 → `onJumpToTurn(lastTurn)` → `turnToMessageId` → `onJumpToMessage`）。打开按钮是并列的独立元素，点击它不触发跳转、也不被行跳转吞掉。
2. **远端会话绝不出现打开按钮：** 会话 `deviceID` 非空（agentred 远端）时不渲染打开按钮；本机不得对远端路径调用 `OpenPath`。`cwd` 缺失/为空时同样不渲染。
3. **+/− 行数来自 producer，不重复实现统计：** 每文件的 `plus`/`minus` 直接读 `block.canonical`（`file.edit` 的 `fileEdit.files[].plus/minus` 汇总、`file.write` 的 `fileWrite.lines` 计入 `plus`），不在前端重复解析 old/new 字符串。
4. **新增 UI 文案全部走 i18n：** 新增 key 必须同时覆盖 `frontend/src/i18n/locales/zh-CN/common.json` 与 `.../en/common.json`，不得硬编码中文；`src/__tests__/i18n.test.ts` 校验 key 覆盖。
5. **树构建是可单测的纯函数：** 层级组织逻辑位于 `derive.ts` 的 `deriveFileTree()`，不在组件内拼装。
6. 不修改 `deriveFiles()` 的路径提取逻辑（legacy 兼容）；排序键由「编辑次数」改为「+/− 行数」。

## Problem

1. **会话文件面板无法按目录浏览。** `frontend/src/components/agentre/chat-context-sidebar/views/files-view.tsx` 把 `deriveFiles()` 产出的相对路径渲染成扁平列表，且 `shortPath` 只显示路径后两段；深目录、多文件时会话看不出目录归属，也难以收拢/定位。
2. **面板无法直接打开文件。** 打开本地路径的能力 `OpenPath` 已存在（`rich-link.tsx:24,106` 用系统默认应用打开），但 sidebar 从未拿到 `session.cwd`，也没有打开入口；用户只能自己去找文件路径。
3. **编辑角标只显示次数、不显示改动量。** 现状 `FileEntry.edits` 只统计编辑工具调用次数；用户想看到每个文件的**新增/删除行数**。后端 `internal/pkg/diff` + `canonical` 已算出每文件 `plus`/`minus`（replay 时 `chat.go:809` / `event_convert.go:40` 重建 `block.canonical`），前端 `chat_svc.ChatBlock.canonical.fileEdit.files[].plus/minus` 可直接消费，无需重复统计。

## Actors and user stories

1. 作为用户，我希望会话文件按目录层级展示成树、可折叠，以便在大会话中快速定位改动文件。
2. 作为本地会话用户，我希望每个文件行有一个图标按钮，一键用系统默认应用打开文件。
3. 作为远端会话用户，我不希望看到一个必然失效的打开按钮。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | 可折叠目录树，默认全部展开，展开状态仅存组件内、不持久化 | 常见文件树交互，深路径可收拢。拒绝静态分组——240px 窄栏下深路径无法收拢；拒绝持久化——超出本任务价值 |
| 2 | 打开按钮仅本地（`deviceID` 为空）且 `cwd` 非空时渲染，点击 `OpenPath(cwd + "/" + path)` | `OpenPath` 已有先例（`rich-link`）。拒绝「始终显示 + 失败 toast」——远端必然失败、体验差；拒绝走 `remote_fs_svc` 下载/打开远端文件——需要后端配合，明显超出本任务 |
| 3 | `deriveFileTree(files)` 作为纯函数放 `derive.ts`：目录节点按名称字母序，文件节点保持 `deriveFiles()` 传入顺序（编辑次数降序、同数按 lastTurn 降序） | 层级与排序职责分离：`deriveFiles` 只负责排序、`deriveFileTree` 只负责层级，两者各自单测。拒绝在组件内建树——不可测；拒绝目录也按编辑权重排——目录位置不稳定 |
| 4 | 文件行拆成两个并列独立按钮：跳转按钮（图标+文件名+diff 角标，flex-1）与打开按钮（ExternalLink 图标） | 行是 `<button>`、内部再嵌 `<button>` 是非法 HTML；拆成并列按钮既合法又可独立聚焦。拒绝「整行点击=打开」——会与既有跳转行为冲突 |
| 5 | 打开按钮始终可见（hover 加深），而非仅 hover 行时出现 | 用户明确要「icon 按钮」，常显可发现性更好。拒绝「hover 才出现」——窄栏下不易发现 |
| 6 | 每文件 `plus`/`minus` 直接读 `block.canonical`（`file.edit` 汇总、`file.write` 计 `lines`），UI 显示 `+N`（绿 `text-status-running`）/ `−N`（红 `text-destructive`）；两者为 0 时不显示角标 | 后端 `internal/pkg/diff` 已产出并经 replay 持久化，前端只消费。拒绝前端重复解析 old/new 字符串（违反 producer 归一化）；拒绝保留编辑次数角标——用户明确要换成行数。颜色复用 `file-edit/card.tsx` 既有 diff 绿/红 |
| 7 | 排序键由「编辑次数」改为「`plus + minus` 行数，同数按 `lastTurn` 降序」 | 保持「改动最多者优先」的既有排序语义。拒绝按字母序重排——丢失改动权重信息 |

## 数据与派生

- `FileEntry` 由 `{ path, edits, reads, lastTurn }` 改为 `{ path, plus, minus, lastTurn }`（`reads` 无 UI 用途，随类型改动移除）。
- 新增纯函数 `deriveFileTree(files: FileEntry[]): FileTreeNode[]`，其中：

  ```ts
  type FileTreeNode =
    | { kind: "dir"; name: string; children: FileTreeNode[] }
    | { kind: "file"; entry: FileEntry };
  ```

- 按 `FileEntry.path` 的 `/` 分段建树；空输入返回空数组；根目录下的文件成为根级 `file` 节点。目录节点按名称字母序（`localeCompare`）排序；文件节点保持输入顺序（即 `deriveFiles()` 的排序结果），`deriveFileTree` 不重排文件。
- `deriveFiles()` 的路径/轮次提取逻辑不变（`extractToolPaths` 仍覆盖编辑+读取工具、各后端与 legacy）；`plus`/`minus` 改为直接消费 `block.canonical`：`canonical.kind === "file.edit"` 时按 `fileEdit.files[].path` 汇总该文件的 `plus`/`minus`；`canonical.kind === "file.write"` 时 `plus += fileWrite.lines`。无 `canonical` 的 legacy 块不产生 `plus`/`minus`（该文件角标不显示）。
- 排序：`(plus + minus)` 降序，同数按 `lastTurn` 降序（替代原 `edits` 排序键）。

## 树渲染与交互

- `FilesView` 接收新增 props `cwd: string` 与 `remote: boolean`（`chat-panel` 通过 `ChatContextSidebar` 传入 `cwd={session?.cwd ?? ""}`、`remote={Boolean(session?.deviceID)}`，恒有值），组件内 `useMemo(() => deriveFileTree(files), [files])` 建树（`files` prop 保持 `FileEntry[]` 不变）。
- **目录节点**：整行可点击，左侧 chevron（展开态 `ChevronDown` / 收起态 `ChevronRight`）+ 文件夹图标 + 目录名（等宽字体、truncate），点击切换该目录展开/收起；行有 hover 反馈，`aria-expanded` 表达当前状态。
- **文件节点**：与现状一致的文件图标（`FileCode`）+ 文件名（basename、等宽、truncate）+ **diff 角标**（仅当 `plus > 0` 或 `minus > 0` 时显示：`+N` 绿色 `text-status-running`、`−N` 红色 `text-destructive`，与 `canonical-tool/file-edit/card.tsx` 的 diff 颜色一致，`aria-hidden`）。行内跳转按钮点击调用 `onJumpToTurn(lastTurn)`，行为与现状完全一致。
- **默认全部展开**；展开状态是组件内 state，文件集合变化时重置为全部展开，不写入 localStorage。
- 每层目录缩进固定步长；层级过深时名称以省略号截断，不压缩缩进，不换行。
- 空文件列表沿用现有空态（`chatContext.files.empty`）。

## 打开文件按钮

- 前提（三者同时满足才渲染）：会话为本地（`remote === false`）、`cwd` 非空、`deriveFiles` 有该文件。
- 位置：文件行右侧、diff 角标之后；图标 `ExternalLink`（约 size-3，`text-muted-foreground`，hover 加深到 foreground）。
- 点击 → `OpenPath(cwd + "/" + path)`；成功无额外反馈；失败（reject）→ `toast.error` 提示新增文案 `chatContext.files.openFailed`，包含错误信息。
- 无障碍：打开按钮与跳转按钮是**两个并列的独立 `<button>`**（不嵌套），都可 Tab 聚焦、可独立点击；打开按钮带 `aria-label`（新增 `chatContext.files.openFile`，中文「打开文件」/英文「Open file」）。点击打开按钮不触发行跳转。

## 新增 i18n key

- `chatContext.files.openFile`（打开按钮 aria-label）：zh「打开文件」/ en「Open file」
- `chatContext.files.openFailed`（失败 toast，含 `{{error}}`）：zh「打开文件失败：{{error}}」/ en「Failed to open file: {{error}}」
- `chatContext.files.expandFolder` / `chatContext.files.collapseFolder`（目录 chevron aria-label）：zh「展开 {{name}}」/「收起 {{name}}」，en「Expand {{name}}」/「Collapse {{name}}」

## Out of scope

- 远端会话（`deviceID` 非空）的文件打开/下载（含 `remote_fs_svc` 集成），本轮仅隐藏打开按钮。
- 树展开状态的持久化（localStorage）与「全部收起/全部展开」工具按钮。
- 目录节点聚合显示 +/− 行数（当前仅文件行显示 diff 角标）。
- 为无 `canonical` 的 legacy 块实现前端行数统计（历史会话不显示 diff 角标）。
- 其他面板（大纲）的任何改动。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| `deriveFiles()` 的 `plus`/`minus`（`__tests__/derive.test.ts`） | `file.edit` 多 hunk 汇总、`file.write` 计 `lines`、多后端路径命中、无 `canonical` 的 legacy 块为 0、排序按 `plus+minus` | `derive.test.ts` 现有 `deriveFiles` 用例（改造 `edits` 断言为 `plus`/`minus`） |
| `deriveFileTree()`（`__tests__/derive.test.ts`） | 目录/文件层级构建、根目录文件、目录字母序、文件保持传入顺序、深路径、空输入 | 无（新增） |
| `FilesView`（`__tests__/files-view.test.tsx`） | 树渲染、目录折叠/展开、行点击跳转不回归、diff 角标 `+N`/`−N` 绿红配色、打开按钮在「本地+cwd」下渲染并调用 `OpenPath(cwd+path)`、远端或 cwd 缺失时不渲染、OpenPath reject 时 toast | `files-view.test.tsx` 现有用例 + `rich-link.test.tsx` 的 `OpenPath` mock 模式 |
| i18n key 覆盖 | 新增 4 个 key 双语言覆盖 | `src/__tests__/i18n.test.ts` |
| chat-panel 接线（`cwd`/`remote` 传入 sidebar） | 接线正确性 | chat-panel 为大型集成组件、不单测；由 `make lint` / `make test-frontend` + 运行时手动观察覆盖 |

不可自动化部分：chat-panel 到 sidebar 的单行接线由运行时观察覆盖（本地会话渲染打开按钮、远端会话不渲染）。

## Open questions

<!-- 无。决策 1-A + 2-A 已获用户确认（2026-08-06）。 -->
