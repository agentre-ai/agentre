# 会话文件预览：右侧预览面板（Markdown + Monaco 只读 + 图片 + Git 对比）

> Status: Draft
> Owner: chat experience / frontend + workspace backend
> Last updated: 2026-08-07

**Objective:** 在会话「文件」面板的三个模式（变动 / 目录 / Git）为 markdown、代码 / 文本与图片文件提供**应用内预览**：点击文件行的「预览」按钮，在右侧一个**可拖拽调宽**的预览面板里渲染内容——markdown 有三档视图（渲染 / 文本 / 双栏），代码 / 文本走 **Monaco 只读**（从 Git / 变动模式打开时直接显示与 git HEAD 的对比），图片走 `<img>` 渲染；读取是会话级、带 cwd 边界、本机与远端（agentred）行为一致；本会话轮次结束时自动刷新已打开的文件；面板入场是克制的右滑动画（design.md §8）。

> 本规格建立在 [`2026-08-07-session-files-three-modes.md`](2026-08-07-session-files-three-modes.md) 之上（其「目录 / Git」两模式与 `workspace_fs_svc` 会话路由是本轮的前置）。该轮已合并进 main（`742cd705` #29），前置满足，本轮可实施。

**Hard invariants:**

1. **行点击行为零回归：** 「变动」模式行点击跳转对应轮次、「目录」模式目录行点击展开 / 收起、「Git」模式行点击（本地）用系统应用打开——全部保持现状。「预览」是**并列的独立按钮**，点击它不触发行点击、也不被行点击吞掉。
2. **路径永不以前端绝对路径入参。** 预览读取一律会话级：前端只传 `sessionID` + 相对会话 cwd 的 `relPath`，后端解析并做 cwd 边界拦截（复用 `ErrPathRefused`）；任何越出 cwd 的解析结果都不返回内容。
3. **本机 / 远端同一语义。** `deviceID == 0` 本机执行，`deviceID != 0` 经租约走新增 `workspacefs.*` RPC；远端 agentred 不认识方法族时给出可区分的「daemon 过旧」态（复用 `WorkspaceFsDaemonOutdated`）。
4. **文本与图片有界传输、其余二进制不传内容。** 文本 > 1 MiB、图片 > 10 MiB 判「过大」；文本判二进制（首块含 NUL 且非图片）不返回正文。图片是唯一允许传输的二进制——按扩展名 allowlist 判定、以 base64 + MIME 返回（SVG 亦经 `<img>` 渲染，img 上下文不执行脚本）。
5. **对比只读、只对比当前文件、以 git HEAD 为基准，且只在从 Git / 变动模式打开代码 / 文本时直接展示。** 对比经只读 git 子进程取 HEAD 版本，不写 git 状态、不改文件；非 git 仓库 / 文件无 HEAD 版本分别给明确态。
6. **新增 UI 文案全部走 i18n**，`zh-CN` 与 `en` 双语言，`src/__tests__/i18n.test.ts` 守卫；不硬编码中文。
7. **Monaco 本地打包、只读、懒加载**，不引 CDN loader（桌面端离线 + `//go:embed`，`@monaco-editor/react` 的默认 jsdelivr loader 不可用）。
8. **动画克制且 reduced-motion 安全**：遵循 `docs/design.md` §8/§10——微交互 150ms `ease-out`、面板入场 200ms 纯右滑（无淡入）、用 `tw-animate-css` / Radix `data-state`、不引 Framer Motion，**每个动画都带 `motion-reduce:`（或 `motion-safe:`）修饰**。

## Problem

1. **会话文件面板无法在应用内查看文件内容。** 「变动」模式行只有「跳转轮次」与「用系统应用打开」（`files-view.tsx` 的 `ExternalLink` 按钮）；用户想快速确认 agent 改动 / 引用的 markdown、代码、图片内容，只能跳到外部编辑器，上下文被切断。
2. **后端缺「读文件内容」能力。** `workspace_fs_svc`（three-modes）只有列目录 / git 变动 / 分支清单，没有 session 级、带 cwd 边界的读内容方法（含按 git ref 读）；本机与远端（agentred）两侧都没有。
3. **文件内容会随轮次推进而变化。** agent 每一轮可能改写同一文件；预览若不随轮次结束刷新，就会一直显示过期内容。用户明确要求「文件变动了也要更新」。
4. **markdown 的「看效果」与「看源码」需要并存。** 渲染视图适合确认排版，但核对 agent 改了什么、复制原文时需要原始 markdown 源码；两者应能并排或一键切换。
5. **用户想知道「这个文件改了什么」。** 单看当前内容回答不了「agent 的改动 vs 提交基线」；需要把工作区文件与 git HEAD 版本并排对比（Monaco diff editor 天然支持）。
6. **预览需要动画反馈，但必须克制。** 面板开合、文件切换如果没有过渡会显得生硬；过度动画又违背本项目「motion restrained」原则（design.md §8）。用户明确要「面板入场右侧滑入、不需要淡入」。

## Actors and user stories

1. 作为**聊天用户**，我想在应用内预览 agent 提到 / 改动的 markdown 文件（GFM 渲染，且可在渲染 / 文本 / 双栏间切换），不必跳出到系统编辑器。
2. 作为**聊天用户**，我想只读预览代码 / 文本文件（语法高亮），并对比它与 git HEAD 的差异（工作区未提交改动）。
3. 作为**聊天用户**，我想直接预览 agent 生成 / 改动的图片（含 SVG），确认产物。
4. 作为**远端会话用户**，我想在 agentred 远端会话里同样能预览与对比文件，行为与本机一致。
5. 作为**用户**，我不希望预览显示过期内容：agent 一轮跑完改了文件，预览应自动刷新。
6. 作为**对动效敏感的用户**，我希望预览面板的动画克制，并在系统开启「减弱动态效果」时停用。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | **右侧独立可调宽预览面板**，位于文件侧栏右侧（最右一栏），打开占位、关闭释放 | 用户裁决（2026-08-07）。拒绝模态弹窗——盖住对话、无法边看对话边切换文件、后续 Monaco 塞进模态很别扭；拒绝侧栏内就地展开——默认 240px 太窄，markdown 表格 / 代码块几乎没法看 |
| 2 | **「预览」是各文件行并列的独立图标按钮**，不动行点击 | 行点击语义各模式不同且部分是硬不变量（变动=跳转轮次、目录=展开、Git=打开）。拒绝整行点击=预览——会破坏既有行为 |
| 3 | **本轮同时支持 markdown（GFM）+ 代码 / 文本（Monaco 只读）+ 图片（`<img>`）+ 对比（Monaco diff vs HEAD）** | 用户裁决（2026-08-07）：「本轮就引入 Monaco」「图片也加一下预览」「还可以对比模式」。拒绝「本轮只做 markdown」 |
| 4 | **内容读取按会话（`sessionID` + relPath）由后端解析 cwd，本机 / 远端路由复用 `workspace_fs_svc` 的 `SessionWorkspaceResolver`** | 沿用 three-modes 决策 2 的安全姿态：前端不持有 cwd 解析规则、路径不可被伪造。拒绝前端传绝对路径——把边界校验复制到前端且路径可注入 |
| 5 | **二进制 / 超大 / 图片类型用返回的标志与字段表达，不新增错误码** | 与 git changes 的 `ChangeView.Binary` 同构；图片单独用 MIME + base64 承载。拒绝为这些常态另开错误码段（错误码只留给真正不可恢复的异常） |
| 6 | **Markdown 复用 `MarkdownText`**（`markdown-text.tsx`：`remark-gfm` + `rehype-highlight` + URL 白名单 + 代码块），不新起渲染栈 | 渲染能力已存在且经过消息流长期验证。拒绝再引一套 markdown 渲染器——重复且易出现渲染差异 |
| 7 | **markdown 三档视图：渲染 / 文本 / 双栏**（一条分段控件）；文本档与双栏的源码侧走 Monaco 只读路径；markdown 不提供对比档 | 用户裁决（2026-08-07）：markdown 的文本与渲染**都展示出来**（双栏并排），并保留单档切换；对比只给代码 / 文本。拒绝只做切换（无法并排对照）、拒绝双栏用两个普通 `<pre>`（与代码渲染路径不一致）、拒绝给 markdown 加 git 对比（已有双栏对照，diff 价值低） |
| 8 | **Monaco 本地打包、只读、动态 import 懒加载，不引 CDN loader** | 桌面端离线 + `//go:embed` 决定；`@monaco-editor/react` 默认从 jsdelivr 拉取 Monaco，不可用。集成细节（裸 `monaco-editor` + Vite `?worker` vs `@monaco-editor/react` + `loader.config` 指向本地实例；diff editor 复用同一 Monaco 实例）由计划阶段 spike 定 |
| 9 | **代码 / 文本无「对比」档位；从 Git / 变动模式打开代码 / 文本时直接显示对比**（Monaco diff editor，左 HEAD 版本 / 右工作区），目录模式打开只显示内容；未跟踪文件左列为空（全部新增）；非 git 仓库 → 明确空态 | 用户裁决（2026-08-07 二次确认）：去掉对比档，首视图由入口模式决定——目录打开=内容、Git / 变动打开=diff；markdown 从任何模式打开都是内容三档，不做对比。three-modes 已具备只读 git 能力，`git show HEAD:<path>` + Monaco diff editor 现成。拒绝会话内快照对比（要每轮存文件快照，重得多）、拒绝两文件对比（需文件选择器 UI，与本轮单文件预览心智不符）、拒绝在面板内留手动「内容 / 对比」切换（模式已表达意图） |
| 10 | **图片按扩展名 allowlist 判定并渲染**（png / jpg / jpeg / gif / webp / avif / bmp / ico / svg），SVG 经 `<img>` 渲染 | 图片是本轮唯一的二进制例外。拒绝按 content-type 探测判定（扩展名即可、与前端入口 allowlist 一致）；SVG 若当 HTML / React 渲染会执行脚本，`<img>` 上下文天然不执行，安全 |
| 11 | **预览随本会话轮次结束自动重读刷新**（`useSessionStatus(sessionId).doneTick` 变化时重取）；打开时总是重新读取 | 用户明确要求「文件变动也要更新」；与目录 / Git 模式的自动重拉同一机制（three-modes 决策 13）。拒绝文件系统 watcher / 轮询——过度，且轮次结束已覆盖绝大多数「agent 改了文件」的场景 |
| 12 | **选中的预览文件、视图档位按会话存储**；切换会话关闭面板、清空选择、档位回默认；面板可拖拽调宽、独立 persistenceKey、带关闭按钮 | 复用 `ResizableSidebar`（`chat-context-sidebar/index.tsx` 同款，edge="left" 可拖拽 + localStorage 记忆）。拒绝跨会话共享预览位置——换会话后文件上下文已无意义 |
| 13 | **错误态复用 `workspace_fs_svc` 既有错误码映射**（`NoCwd` / `PathRefused` / `ReadFailed` / `DeviceOffline` / `DaemonOutdated`，20800 段）+ binary / tooLarge 标志 + 对比的 notARepo / 无 HEAD 空态 | 三模式已把错误翻译铺好，预览面板照搬即可，不重复实现。拒绝为预览另起一套错误翻译 |
| 14 | **动画遵循 design.md §8**：面板开合 200ms `ease-out` **纯右滑（无淡入）**（`animate-in slide-in-from-right` + `motion-reduce:animate-none`）、正文切换 / 刷新淡入 150ms、按钮 hover 用 `transition-colors`，全部 CSS、不引 Framer Motion | 用户裁决（2026-08-07）：「面板入场右侧划入就行，不需要淡入」；design.md §8 定义 restrained motion 约定。拒绝淡入（用户明确否）、拒绝重动画 / Framer Motion |

## 预览面板与入口

- **「预览」按钮**出现在「变动 / 目录 / Git」三模式的文件行上：markdown 文件（`.md` / `.markdown`，不含 `.mdx`——MDX 含 JSX，GFM 渲染会碎）、**文本 / 代码 allowlist** 内扩展名的文件、**图片扩展名**（见「图片预览」）。其余（音视频、压缩包、PDF、可执行文件等）不出现预览按钮，行保持现状。
- 「变动」模式的路径可能来自工具调用的绝对路径：**只有解析后落在会话 cwd 内的文件行才出现预览按钮**；越出 cwd 的不提供预览。「目录 / Git」模式的路径本就相对 cwd，直接可用。
- 按钮是行右侧与「用系统应用打开」（`ExternalLink`）**并列**的独立图标按钮（`Eye` 图标，与打开的 external-link 明显区分），带 `aria-label`；点击它打开或切换到右侧预览面板并选中该文件，当前被预览文件的按钮高亮。点击不触发行点击既有行为。
- 按钮出现条件不依赖「本机 / 远端」：远端会话同样出现（内容经 RPC 读取）。
- **首视图由入口模式决定**：目录模式打开 → 内容（markdown 三档 / 代码文本文本档 / 图片）；Git / 变动模式打开 → 代码 / 文本直接显示与 HEAD 的对比（diff），markdown / 图片仍显示内容。打开另一模式的行会按新模式重设首视图；面板内无「内容 / 对比」手动切换（决策 9）。
- 无 cwd（自由会话 / 远端未配路径）时，文件面板本就出对应空态，无文件行可点，故预览按钮随行消失。

## 视图档位（按文件类型）

面板头部一条分段控件，档位随文件类型变化：

| 文件类型 | 档位 | 默认 |
|---|---|---|
| markdown（`.md` / `.markdown`） | 渲染 · 文本 · 双栏 | 渲染 |
| 代码 / 文本 | （无分段控件）目录打开=内容、Git / 变动打开=对比 | 由入口模式决定 |
| 图片 | （无分段控件，只有图片） | — |

- **渲染**：`MarkdownText`（GFM + 高亮 + URL 白名单），与聊天消息外观一致。
- **文本**：原始内容，Monaco 只读（markdown 源码无 GFM 渲染；代码 / 文本带语言高亮，未知扩展名纯文本）。
- **双栏**（仅 markdown）：左文本（Monaco 只读源码）+ 右渲染（GFM），两栏各自独立滚动，中间分隔线。
- **对比**（仅代码 / 文本，且仅当从 Git / 变动模式打开时）：Monaco diff editor（见下节）；目录模式打开不显示。markdown 不提供对比——它已有「双栏」（源码 + 渲染效果）这种对照，git diff 对 markdown 只是给「文本」档加高亮底色，价值低。
- 档位（markdown 三档）是面板状态：切文件保留，关面板 / 切会话回到默认档；代码 / 文本的首视图由入口模式决定，无档位可切。

## 内容读取（后端契约）

- 新增会话级方法：
  - `ReadFile(ctx, sessionID, relPath)` → 工作区文件内容 `{ content, contentType, binary, tooLarge }`。
  - `GitFileContent(ctx, sessionID, relPath)` → 同一文件在 **HEAD** 的版本（供对比档左列）；文件未跟踪 / 不在 HEAD → 返回空基线的标志，非 git 仓库 → `notARepo`。
- 两者第一个入参都是 `sessionID`，不是路径（沿用 three-modes 决策 2）；路径只以相对 cwd 的 `relPath` 出现。
- 后端沿既有 `SessionWorkspaceResolver` 解析 `{deviceID, cwd}`；`cwd` 为空 → `WorkspaceFsNoCwd`；`sessionID <= 0` → `InvalidParameter`。
- `deviceID == 0`：在本机叶子包解析并读取；`deviceID != 0`：经 `remote_device_svc` 租约调新增 `workspacefs.readFile` / `workspacefs.gitFileContent` RPC（daemon 侧是同一份叶子实现）。
- **边界**：`relPath` 在 cwd 内解析；越界（`..`、绝对路径、逃逸的符号链接）→ 复用 `ErrPathRefused`（→ `WorkspaceFsPathRefused`）。符号链接跟随解析后再校验仍落在 cwd 内，越界拒绝。git 读取按 cwd 在仓库内的前缀换算路径（three-modes 已处理 cwd 子目录的 git 路径语义）。
- **文本**（markdown / 代码 / 文本）：返回 UTF-8 正文（`content`）。
- **图片**：按扩展名 allowlist 判定，返回 base64 编码的 `content` + MIME `contentType`（png→`image/png`、svg→`image/svg+xml` 等）。这是唯一允许传输的二进制。
- **其余二进制**（含 NUL 且非图片）→ `binary` 标志，不返回正文。
- **超大**：文本 > 1 MiB、图片 > 10 MiB → `tooLarge` 标志，不返回正文（文本阈值与 git changes 的 `maxUntrackedTextSize` 一致；图片阈值单列，计划阶段可按实际照片大小微调）。
- **对比**：`GitFileContent` 走只读 git 子进程（`git show HEAD:<path>` 之类）；文件不在 HEAD（未跟踪）→ 空基线（对比档左列为空、全部新增）；非 git 仓库 → `notARepo`；超出 cwd / 仓库边界 → `ErrPathRefused`。
- **daemon 过旧**：远端返回 `-32601`（方法不存在）→ `WorkspaceFsDaemonOutdated`，与三模式同一个翻译路径。
- 读取是纯读，不写本地库、不改任何文件、不改 git 状态；正文与对比内容不进日志。

## 渲染与对比

- **渲染档**：`MarkdownText`（GFM 表格 / 删除线 / 自动链接、代码块语法高亮、URL 白名单安全）。
- **文本档**：Monaco 只读（`readOnly`），markdown 源码 / 代码 / 文本，已知语言语法高亮、未知按纯文本。
- **双栏档**：左 = 文本档（Monaco 只读源码），右 = 渲染档（GFM），两栏独立滚动。
- **对比**（仅代码 / 文本，从 Git / 变动模式打开时）：Monaco **diff editor**（`monaco.editor.createDiffEditor`，只读），左 = HEAD 版本、右 = 工作区，增删行底色区分；未跟踪文件左列为空（全部新增）；只对比当前这一个文件。顶部条显示「对比 · HEAD → 工作区」与增删图例。目录模式打开不显示对比。
- **图片**：`<img src="data:<contentType>;base64,…">` 渲染，棋盘格底衬托透明图（png），居中、等比缩放至面板宽，超长超宽可滚动；SVG 同样经 `<img>`（img 上下文不执行脚本，防 XSS）。
- 面板正文区域按内容类型与档位渲染，内容更新（切换文件 / 轮次刷新 / 切档位）时重渲染。

## 刷新（文件变动更新）

- 打开文件、或切换选中文件时，总是重新读取（不读缓存）。Git / 变动模式打开的文件重读后重算对比（工作区变化反映到 diff）。
- **本会话轮次结束**（`doneTick` 自增：done / error / aborted / closed / steer_consumed）时，若面板仍打开且指向某文件，自动重新读取并刷新渲染（刷新后正文淡入 150ms）。刷新时机与目录 / Git 模式一致；流式进行中不逐 chunk 刷新。
- 轮次结束重读发现文件已被删除 / 不可读 → 面板进入对应错误态，不保留旧内容。

## 动画（design.md §8）

- **面板开合**：打开时从右滑入（**纯 `translateX`，无淡入**，200ms `ease-out`，`animate-in slide-in-from-right`），关闭滑出；`motion-reduce:animate-none`。
- **正文切换**：切换文件、轮次刷新、切档位后正文淡入（150ms）。
- **按钮反馈**：预览 / 打开 / 关闭按钮 hover 用 `transition-colors duration-150`（既有点击反馈样式延续）。
- 全部 CSS 动画（`tw-animate-css` + Radix `data-state`），不引 Framer Motion；**每个动画带 `motion-reduce:` / `motion-safe:` 修饰**（design.md §10 硬约定）。

## 失败与恢复

面板内各错误态（复用既有错误码 → 文案，i18n 双语）：

- **路径越界**（`WorkspaceFsPathRefused`）——「路径超出会话工作目录」。
- **无工作目录**（`WorkspaceFsNoCwd`）。
- **读取失败**（`WorkspaceFsReadFailed`）。
- **远端离线**（`WorkspaceFsDeviceOffline`）。
- **远端 agentred 过旧**（`WorkspaceFsDaemonOutdated`）——「请升级 daemon」。
- **二进制 / 超大**（标志）——「该文件无法预览（二进制 / 过大）」，文本与图片各自的阈值在提示中说明。
- **对比 · 非 git 仓库**（从 Git / 变动模式打开代码 / 文本时）——「没有可对比的 git 基准」空态。
- 错误态提供「重试」动作；加载 / 空态 / 错误文案复用 three-modes 合并后目录与 Git 模式共用的 `panel-feedback.tsx`（`PanelSkeleton` / `PanelNotice` / `errorText`），不另起一套。

## 状态与布局

- 预览面板是 `chat-panel` 主区 flex 行的**最右一栏**（`[transcript + composer] [ChatContextSidebar] [FilePreviewPanel]`）；仅在选中了可预览文件时渲染，关闭后释放宽度。
- **可拖拽调宽**（复用 `ResizableSidebar`，`edge="left"` 手柄在左边缘——4px 命中区 + 1px 视觉条），独立 `persistenceKey`，默认宽度约 440px（mockup 已按 markdown 表格 / 代码块 / 图片 / 双栏 / 对比的可读性定），宽度记忆不随会话切换丢失。
- 面板头部显示：文件类型图标、文件名（含所在目录前缀）、按文件类型的视图分段（markdown 3 档 / 代码文本无分段 / 图片无）、关闭按钮；`aria-label` 表达面板用途、档位与关闭动作。
- 选中的预览文件与视图档位按会话存储；切换会话时关闭面板、清空选择、档位回默认。

## 安全、隐私、兼容性与可访问性

- **路径入参安全**：前端永不传绝对路径；cwd 边界由后端强制执行（硬不变量 2）。会话 cwd 之外的任何文件都无法经本接口读到。
- **内容不进日志**：读取的文件正文（含图片 base64）与对比内容遵循既有日志红线，不写入日志。
- **SVG 防脚本**：SVG 一律经 `<img>` 渲染，不当作 HTML / React 节点注入，脚本不执行。
- **无写路径**：本接口与对比都只读，无任何写文件 / 改 git 的能力。
- **兼容性**：旧 agentred（无 `workspacefs.*` 方法族）→ `DaemonOutdated` 明确态，不影响其余功能；本机会话不受影响。
- **可访问性**：预览 / 关闭 / 档位切换按钮与面板都有文本标签（不靠颜色）；错误态有文字；图片带 `alt`（文件名）；对比档增删有文字标签与图例（不靠颜色单表）；面板内容可随 Tab 聚焦进入；动画有 `motion-reduce:` 兜底。
- **依赖体积**：Monaco 是重依赖，动态 import + 本地 worker 打包，不进初始包；作为本次新增依赖在 review 时确认其产物体积可接受。

## Out of scope

- **Monaco 编辑 / 保存**：本轮是只读预览，无写回能力。
- **非图片富媒体预览**：音频、视频、PDF 等。
- **对比的基线选择**：对比档固定以 HEAD 为基准，不做分支 / 基线选择器（Git 模式已有基线选择，本轮不复用）。
- **markdown 的 git 对比**：markdown 只有渲染 / 文本 / 双栏三档，从任何模式打开都不显示对比（见决策 7 / 9）。
- **会话内快照对比**（当前 vs 会话开始 / 上一轮）：需要每轮存文件快照，本轮不做。
- **文件内容实时轮询 / 文件系统 watcher**：仅轮次结束刷新。
- **allowlist 之外的文件**（音视频、压缩包、PDF、可执行文件等）：不出预览按钮；后续再放宽。
- **其他界面的预览**（项目页、会话外）：仅「文件」面板三模式。
- **three-modes 合并之前**不实施（本规格的前置）。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| 叶子包 `ReadFile`（本机侧） | 文本正常读、越界（`..` / 绝对路径 / 逃逸符号链接）拒绝、文本 NUL 判 binary、文本 >1MiB 判 tooLarge 不整读、图片扩展名 → base64 + MIME（含 svg）、图片 >10MiB 判 tooLarge、非文件 / 目录拒绝、cwd 为空 | `internal/pkg/workspacefs/*_test.go`（three-modes 的 git_changes 测试同构：真 tmpdir + 不连库） |
| 叶子包 `GitFileContent`（本机侧） | HEAD 版本读取、未跟踪 → 空基线、非 git 仓库 → notARepo、cwd 子目录的路径换算、越界拒绝 | 同上 + `git_state_test.go` 的 `runGit` helper（three-modes） |
| wire 双向翻译 + daemon handler | `workspacefs.readFile` / `workspacefs.gitFileContent` 方法名 / 请求响应类型、sentinel ↔ JSON-RPC 码、未鉴权被拒、端到端真目录一跳 | `internal/pkg/workspacefs/wire/*_test.go`、`internal/daemon/workspacefs/*_test.go`、`internal/daemon/integration_test.go`（three-modes） |
| `workspace_fs_svc` 路由 | `deviceID=0` 读真 tmpdir（远端 mock 零 EXPECT）、`deviceID≠0` 发对应 RPC、错误映射（越界 / daemon 过旧 / 离线 / notARepo） | `internal/service/workspace_fs_svc/*_test.go`（three-modes，sqlmock + mockgen，不连库） |
| 前端预览按钮出现条件 | 三模式 markdown / 代码 / 文本 / 图片行出按钮、二进制扩展名不出、cwd 外绝对路径不出、远端行出按钮 | `files-view.test.tsx` / `directory-view` / `git-view` 既有测试扩展 |
| 面板打开 / 切换 / 关闭 / 档位 | 点按钮打开并选中、点另一行切换内容、关闭释放宽度、切换会话清空、档位按文件类型（markdown 3 档 / 代码文本无分段 / 图片无）、切文件保留档位 / 关面板回默认、首视图由入口模式决定（目录→内容、Git / 变动→diff） | 无（新增，`FilePreviewPanel` 组件测试） |
| 渲染分流 | `.md` 走 MarkdownText（GFM 表格可渲染）、文本档显示原始源码、双栏左右并存、代码走 Monaco（只读、有语言）、未知扩展名纯文本、图片 `<img>` 用 data URL + alt | `markdown-text.test.tsx` 既有；Monaco 用 mock（happy-dom 不跑真实 Monaco，计划阶段定 mock 形态） |
| 对比（入口模式决定） | 从 Git / 变动模式打开代码 / 文本 → 调 `GitFileContent` 显示 diff（HEAD / 工作区）、未跟踪空左列、notARepo 空态；目录模式打开 → 只显示内容不调 `GitFileContent`；markdown 任何模式都不显示对比 | Monaco mock + `git_state_test.go` 范式 |
| doneTick 刷新 | 打开时首次读取、`doneTick` 变化重读并替换内容、切会话不串 | 目录模式 `doneTick` 自动重拉的既有测试形态 |
| 错误态 | binary / tooLarge / 越界 / 离线 / daemon 过旧 / notARepo 各文案与重试 | `workspace_fs_svc` 错误映射测试 + 目录 / Git 模式空态测试 |
| 动画 reduced-motion | 每个动画带 `motion-reduce:` 修饰（审查 + 运行期观察）；面板入场为纯右滑无淡入 | design.md §10 的既有约定（lint 层面可查 `motion-reduce` 缺失由 review 覆盖） |
| i18n key 覆盖 | 新增 key 双语言 | `src/__tests__/i18n.test.ts` |

不可自动化部分：Monaco 真实语法高亮与 diff editor 的视觉效果、图片渲染的观感、动画节奏、面板拖拽手感、预览面板与三模式并存的整体布局，由运行时手动观察覆盖；`chat-panel` 的三栏接线不做单测（大型集成组件，同三模式既有约定）。

## Open questions

<!-- 无。设计已获用户确认（2026-08-07）：决策 1=A 右侧面板、决策 2=A 独立预览按钮、决策 3=A 本轮引入 Monaco + 图片 + 对比（代码/文本）、决策 4=A 本机+远端、决策 5=A 建立在 three-modes 合并后、对比基准=A git HEAD、动画=面板入场仅右滑无淡入、markdown=三档（渲染/文本/双栏，无对比）、面板可拖拽调宽。追加（2026-08-07 二次确认）：去掉对比档——代码/文本首视图由入口模式决定（目录→内容、Git/变动→diff），markdown 任何模式都是内容三档，面板内无手动内容/对比切换。 -->
