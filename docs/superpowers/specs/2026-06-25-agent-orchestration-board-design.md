# Agent 编排独立板块 — 设计稿 + 前端对齐方案

> 状态：设计稿已完成（`agentre.pen`，Light + Dark 双版），本文为对齐方案（spec）。
> 范围：**只画设计稿 + 出对齐方案**，本轮不改前端代码。
> 关联：`2026-06-23-agent-orchestration-design.md`（编排功能本体）、`2026-06-11-group-task-orchestration-design.md`、`2026-06-13-workflow-library-relocation-and-agent-tool-design.md`。

## 1. 目标

把「Agent 编排」从**内嵌在 Chat 页**升级为一个**顶级导航板块**：有独立入口、独立页面、独立的 Run 侧栏，并把分散、不一致的编排 UI/UX 统一到一套设计语言。同时让设计稿与前端实现、以及设计稿内部彼此**对齐**。

## 2. 现状诊断

### 2.1 前端（实现）

编排目前是 **Chat 页的一个子区域**，没有独立路由：

- `App.tsx` 路由（L875–884）只有 `/chat /projects /issues /hooks /org /settings`，**无 `/orchestration`**。
- 左侧导航栏 `navItems[]`（`App.tsx` L67–99，`<aside>` 渲染于 L785–812，图标用 **`@iconify-icons/tabler`**）= Chat / Projects / Issues / Org / Hooks，**无编排项**。
- 编排入口内嵌在 `chat-page.tsx` 侧栏顶部（L653–669）：`AgentPanelSection label={t("orchestration.section")}` + `<RunList>`。
- Run 以 **tab** 形式打开：`chat-tabs-store.ts` 的 `TabKind` 含 `{kind:"run",runId,title}`（L14）、`openRun()`（L128–145）；`chat-panel-host.tsx` 在 `kind==="run"` 时渲染 `<HostedOrchestrationRun>`→`<OrchestrationRun>`（L105–110、L259–277）。
- 组件目录 `components/agentre/orchestration/`：`index.tsx`（graph/feed 切换，L23–54）、`run-header.tsx`（结构图/活动流 segmented toggle 已实现，L71–96）、`structure-graph.tsx`、`activity-feed.tsx`、`task-board.tsx`、`run-list.tsx`、`run-new-dialog.tsx`。
- 事件/状态：`orch-events-host.tsx`、`orch-notifier.tsx`、`stores/orch-run-store.ts`、`stores/orch-run-list-store.ts`。

> 结论：**功能基本齐全**（双视图、任务板、创建弹窗、事件流都在），缺的是「独立板块」这层 IA：没有顶级入口/路由，Run 与 agent 会话挤在同一套 tab + 侧栏里。

### 2.2 设计稿（`agentre.pen`）

编排相关旧屏散落、且不一致：

- 导航 rail 有**三套并存**：Chat 页 `AppRail`（message-circle/circle-dot/network/settings/moon，是较规范的一套）、Org 页 rail（layout-list/git-fork/webhook…）、旧编排屏 rail（waypoints/folder-tree/users）。编排还没进规范 rail。
- 旧编排屏（`编排 Run — 活动流` 等）侧栏把「Run + 会话」混装；空状态屏 `bOsgi` 已尝试「独立编排板块」但未贯彻。
- **只画过活动流，从未画过结构图（graph）主视图**；无 Light 版；无统一「总览/首页」。

## 3. 设计交付物（已落在 `agentre.pen`）

新板块集中放在画布右侧的「编排板块」区域（原点约 `x≈34176, y≈760`，Light 在左列、Dark 在右列）。旧编排屏**保留不动**作参考。共 10 屏 × Light/Dark = 20 帧：

| # | 屏 | Light | Dark | 说明 |
|---|---|---|---|---|
| 1 | 编排 — 总览 | `K0q5q` | `Zk6xc` | 落地页：标题+概要统计(运行中/等待/本周完成/平均时长) + 进行中 Run 卡片(进度条+Agent 头像+状态) + 最近完成列表。**左 RunSidebar 底部固定「流程库」入口**（route 图标 +「浏览/新建/编辑流程」→ 打开流程库弹窗），补齐此前缺失的可见入口 |
| 2 | Run — 结构图 | `iBqBl` | `KsHC5` | **新**主视图：自顶向下树（Leader 皇冠 → Agent → subagent）。**后端工程师派生 3 个 subagent**(验签助手 ×2 / 迁移助手 / 限流助手)、测试派生 1 个(用例生成器)——演示「一个 agent 调用多次 / 多个 subagent」；前端节点带「向 后端 提问·等待回复」徽标演示 peer ask |
| 3 | Run — 活动流 | `Z2P0Vn` | `o6UQQ` | 事件时间线（派发/汇报/提问/完成/思考 + Leader 皇冠 + 正在执行指示器）；含 **agent↔agent peer ask/reply** 事件（前端 提问 @后端 → 后端 回复 @前端）+ 右侧任务板。**视图切换栏下方有一条「流程蓝图」参考带**（套用的流程名 + 步骤面包屑 + 「仅参考·不约束执行」），淡色、与下方彩色实时流明显区分 —— 详见 §6.1「蓝图 vs 执行」 |
| 4 | Run — 暂停·错误 | `kx9LR` | `rtrNS` | 活动流 + 红色错误横幅 + 状态「已暂停」+「继续」控制 |
| 5 | Run — ask·死锁 | `RLgFO` | `XoZpZ` | 结构图 + 死锁横幅 + 互等两节点红框 + 状态「等待介入」 |
| 6 | 新建 Run 弹窗 | `Y8hJ5` | `GCJVD` | 目标/Leader/起始流程/可参与 agent + 创建并启动（**已去掉「关键步骤需批准」「允许自动派生 subagent」两项设置**） |
| 7 | 新建 Run · 从流程库 | `wst5z` | `a5uhM` | 起始流程切到「从流程库选择」的状态：流程单选列表（每项=名称 + **标签 chip** + **步骤面包屑**）+ Leader + 套用流程并启动。标签/步骤是给人一眼挑流程的展示层（来自结构化字段，见 §6.1），**不注入 AI**。右上「管理流程库 →」上下文链接 → 打开流程库弹窗 |
| 8 | 流程库弹窗 | `DEpdt` | `VSE2d` | 真实 `workflow-manager-dialog` 的两栏壳：左列表（名称 + **标签 chip** + 使用中 Run 数 + 一行摘要 + 更新于）+ 右预览。**预览顶部新增「蓝图」glance band**（标签 chips + 步骤 outline 面包屑 + 「仅供一眼读懂·不约束 AI」提示），其下是 **Markdown 正文**（AI 实际读取，`## 步骤` 是正文细节）+ 编辑/删除 |
| 9 | 流程编辑器 | `yl83F` | `kRxup` | manager 的编辑态：名称 + **标签 chips**（增删）+ **步骤(概览) 有序列表**（grip 排序/增删，每行 标注「仅展示·不约束 AI」）+ **流程正文(Markdown) · AI 实际读取**（含「插入骨架模板」）+ ⌘+Enter 保存。对齐 `workflow-editor-form` 并新增 tags/outline 录入 |
| 10 | Run — 进入会话(钻入) | `x4ZEP` | `RUK6J` | 点 agent 节点→蓝色选中环，右侧任务板切换成**该 agent 的会话**(transcript + 内联审批 批准/拒绝 + 「对它说」输入 + 返回任务板)。这就是「进入实际对话」的入口 |

任务/计数已对齐为 **12 任务 · 5 完成**：任务板按 agent 分组并把 subagent 作为缩进子组列出（验签助手带「×2 次调用」徽标）；总览卡、Run 头部、任务板头部统一为 5/12。

构建要点（供后续维护者理解设计稿）：

- 全部用 `$变量` 上色 + 屏帧 `theme:{mode:light|dark}`；Dark 版是 Light 版的 `Copy` + 翻 `theme`。
- 壳（topbar/rail/RunSidebar/statusbar）为**内联**构建（非组件实例）。原因见 §7「设计稿坑」。
- Run 模板（活动流屏 `Z2P0Vn`）内同时含 `graphView`(默认隐藏) 与 `feedView`；结构图/暂停/死锁/钻入屏都是 `Copy` 它 + `descendants` 开关视图/横幅/节点高亮；钻入屏用 `descendants` 把 `TaskBoard` 整体 **type-替换**成会话面板。**改 Run 内容需「改模板 → 删旧变体 → 重新派生」**(故变体/Dark frame id 经多次重新派生已变 —— **以 frame name `编排 — … · Dark` 搜索为准**，表中 Dark id 为最近一次派生值、可能已过期；Light id 稳定)。另：画布原点也被移动过（协作编辑），按 name 定位、勿硬编码坐标。

## 4. 新 IA / 导航

- 在规范左侧 rail 增加一项 **编排（图标：拓扑/路由型）**，建议排在 Chat 之后：`Chat · 编排 · Projects · Issues · Org · Hooks ·(spacer)· Theme · Settings`。
  - 设计稿用 lucide `waypoints`；前端图标库是 **tabler**，对应可用 `topology-star-3` / `sitemap` / `binary-tree-2` / `route`（择一，建议 `topology-star-3`）。
- 新路由 `/orchestration`，可带子路由 `/orchestration/:runId`（深链到具体 Run）。
- 板块壳：`AppTopBar(面包屑 AgentRe / 编排 [/ Run 名]) + [Rail 48 ┃ RunSidebar 320 ┃ 主区 ┃ 任务板 320] + AppStatusBar`。
- **RunSidebar 仅 Run**：标题「编排 Runs」+「新建」+ 过滤(全部/运行中/已完成) + Run 列表(状态点) + 空状态。**不再混入会话列表**。
- 总览(无选中 Run)与 Run 详情(选中)走 master-detail：选中 Run 即在主区内联渲染结构图/活动流 + 右侧任务板（不再新开 chat tab）。

## 5. 前端对齐方案（后续实现清单，本轮不做）

按改动从小到大：

1. **加路由 + 导航项**：`App.tsx` `navItems[]` 增 `{path:"/orchestration", labelKey:"nav.orchestration", icon:<tabler 拓扑图标>}`（置于 chat 之后）；`<Routes>` 增 `<Route path="/orchestration" element={<OrchestrationPage/>} />`。
2. **新建 `OrchestrationPage`**（`components/agentre/orchestration/orchestration-page.tsx`）：承载板块壳 = 左 RunSidebar + 主区(总览 或 选中 Run 的 `<OrchestrationRun>`) + 右任务板。无选中 Run 时渲染新的 `<OrchestrationOverview>`。
3. **RunSidebar（仅 Run）**：把 `run-list.tsx` 升级/包一层为带「编排 Runs」标题 +「新建」+ 过滤段（全部/运行中/已完成）+ 空状态的侧栏；**仅 Run，不含会话**。
4. **总览/落地页 `OrchestrationOverview`**（新组件）：概要统计 + 进行中 Run 卡片(进度/头像/状态) + 最近完成列表 + 空状态。数据来自 `orch-run-list-store` / `orch-run-store`。
5. **Run 从 chat tab 迁出**：`chat-page.tsx` L653–669 移除编排 `AgentPanelSection + RunList`；`OrchestrationPage` 内联渲染 Run，**不再走** `chat-tabs-store.openRun` / `kind:"run"` tab。`TabKind` 的 `run` 变体与 `chat-panel-host` 的 `HostedOrchestrationRun` 分支可删（或仅保留深链兼容）。
6. **复用既有视图**：`index.tsx` 的 graph/feed 切换、`structure-graph.tsx`、`activity-feed.tsx`、`task-board.tsx`、`run-header.tsx`、`run-new-dialog.tsx` 基本可直接复用；按设计稿核对视觉(状态色/徽标/横幅/任务板分组/Leader 皇冠/typing 指示)。
7. **结构图视觉对齐**：设计稿给出了此前缺失的结构图样式（Leader 皇冠 + 自顶向下树 + 阻塞红框 + 死锁徽标），据此校准 `structure-graph.tsx`。
8. **导航 rail 收敛（设计系统修复）**：把分散的三套 rail 统一为一个共享 `AppRail` 组件（Chat/编排/Projects/Issues/Org/Hooks/Theme/Settings），各页只切换 active 项。
9. **进入会话（节点钻入）**：结构图节点 / 任务板行 / 活动流头像都应可点击 → 选中(蓝环) + 右侧从「任务板」切到该 agent 的**会话面板**(transcript + 内联审批 批准/拒绝 + 「对它说」输入 + 返回任务板)。这是用户反馈「没看见点击进入实际对话的入口」的直接补齐 —— 复用既有 chat transcript + ask-card 组件，承载在 Run 的右栏（任务板 ⇄ 会话 二态切换）。
10. **去掉两项 Run 创建设置**：`run-new-dialog.tsx` 不再有「关键步骤需我批准」「允许 Leader 自动派生 subagent」开关 —— **派生 subagent 默认允许**；审批走**各 agent 后端权限的 awaiting-user 阻塞**（危险操作内联批准/拒绝，见 `2026-06-23-agent-orchestration-design.md`），不是 Run 级开关。**注意**：`TaskAwaitingUser`/`ApprovalGateway` 后端尚未接线（见 §9），落地需补转换逻辑。
11. **agent↔agent ask/reply（部分已存在，busy 目标需补后端 + 前端）**：`orch_svc` 已有 `ask`/`reply` MCP 工具（`mcp.go`），任意 agent 问任意 agent、阻塞等回复（≤4min）、接死锁检测 —— **但只对 idle 目标有效**：`Ask()` 走 `Send`/`startTurn`，对方 busy 时 `lock.TryLock()` 失败、返回 `ChatSendInFlight`。你要的「**对方处理中也能 ask**」**没落地**，且实现**没复用 setter**（用的是新 turn，不是 `Enqueue`/steer）。落地 = ① 后端：busy 时改走 `Enqueue`/steer 投递；② 前端：`feed-data.ts` 渲染 ask/reply + 结构图徽标；③ **注入格式：把提问用 XML 标签包裹并带 `ask_id`**（`<peer_ask ask_id="…" from="…">问题</peer_ask>` + 一句「调用 `reply(ask_id=…)` 回复」），取代现有 `【收到提问 ask_id=…】` 纯文本前缀 —— 闭合标签是天然边界，steer 进对方当前 turn 时不被其输出污染、更易被识别为同伴提问并据 id 回 `reply`。详见 §9 与 roadmap S3。
12. **「从流程库选择」+ 标签/步骤展示层**：`run-new-dialog.tsx` 已有 3 态 Select（无/流程库/临时）+ 按名称的流程下拉。设计稿把它升级为「名称 + **标签 chip** + **步骤面包屑**」的单选列表 —— 标签/步骤是**给人一眼挑流程的展示层**，来自新增的 display-only 字段 `tags`/`outline`（§6.1），**绝不注入 Leader**（注入仍只 `content`）。落地改动面见 §9（含小迁移）。
13. **流程库可见入口（补缺）**：现状流程库**只能从命令面板打开**（`command-palette/sources/workflow-actions-source.tsx` 的「打开流程库/新建流程」→ `useWorkflowManagerStore.openBrowse()/openCreate()`），无任何可见按钮 —— 发现性缺口。设计稿补两个可见入口：① **RunSidebar 底部固定「流程库」行**（持久入口，编排页常驻）→ `openBrowse()`；② **新建 Run·从流程库 picker 右上「管理流程库 →」**（上下文入口，挑流程时可跳去管理）→ `openBrowse()`。命令面板入口保留。

## 6. 状态与空态覆盖（对齐 Web App 设计准则）

- 总览：有数据(卡片+列表) / 空(无 Run，引导新建)。
- Run：运行中(活动流/结构图) / 暂停·错误 / ask·死锁(阻塞) / 完成(历史)。
- 创建弹窗：表单态(目标/Leader/起始流程/可参与 agent)；两态 —— 「从零开始」与「从流程库选择」(流程单选：名称 + 标签 chip + 步骤面包屑)。
- Run 右栏：任务板态 ⇄ 会话(钻入)态；会话态含 transcript + awaiting-user 内联审批 + 「对它说」。**注意**：awaiting-user 审批后端尚未接线，见 §9。
- 流程库：三态 —— 浏览态(左列表 + 右**蓝图 glance band** + Markdown 正文预览 + 编辑/删除) / 编辑态(名称 + 标签 + 步骤 outline + Markdown 正文) / 从库选择(picker)。三态均已画。
- Run 视图：活动流/结构图顶部统一带一条**流程蓝图参考带**（套用流程时显示；未套流程则隐藏），见 §6.1。

## 6.1 流程蓝图 vs 实际执行（保留「给人看的标签/步骤」的设计原则）

把两件事**显式分开**，是这套编排能同时「让人一眼读懂」又「不把 AI 锁进流水线」的关键：

| | **流程蓝图（给人看的"菜谱"）** | **实际执行（AI 真正在跑的）** |
|---|---|---|
| 是什么 | 这条 SOP 打算怎么走：标签 + 步骤面包屑 + 角色 | Leader/agent/subagent 的真实结构图 + 任务板 |
| 来源 | 人写的流程（静态） | AI dispatch/ask/report 产生（动态：`callSeq`/任务状态/边） |
| 性质 | 意图、提示，**不锁死 AI** | 事实，可能与蓝图不同 |
| 在 UI | 流程库的标签/步骤、Run 顶部淡色「流程蓝图」参考带 | 结构图 + 任务板（彩色、agent 着色、实时） |

**数据模型（设计提案，需小迁移）**：在 `workflow` 上增两个**仅展示、绝不注入 Leader** 的结构化字段 —— `tags []string`（短分类标签：通用/修复/重构…）+ `outline []string`（有序步骤名，给人看的骨架）。AI 注入的仍只有 `content`（自由 Markdown SOP）。编辑器里 tags 以 chips 录入、outline 以可增删排序的有序列表录入，每处都标注「仅展示·不约束 AI」。

**与执行解耦**：Run 套用某流程时，把该流程的 `outline` 作为 Run 顶部一条**淡色「流程蓝图」参考带**（流程名 + 步骤面包屑 + 「仅参考·不约束执行」），视觉上 muted/参考、与下方彩色实时流明确区分；**不**把真实任务往步骤上做强映射（不假装"第 3 步进行中"）。结构图/任务板永远是唯一的执行真相。

## 7. 设计系统一致性

- **Token**：全部复用既有变量，无需新增 —— `primary/primary-soft/primary-text`、`status-running/waiting/error/idle`(含 `-bg`)、`agent-1..10`、`rail/sidebar/sidebar-active-bg`、`card/border/border-strong/muted-foreground/subtle-foreground`、`radius-sm/md/lg/xl`，字体 `font-sans`(Geist)/`font-mono`(JetBrains Mono)。
- **状态色映射**：运行=running(绿)、等待/ask=waiting(琥珀)、错误/阻塞=error(红)、完成/空闲=idle(灰)。
- **Agent 调色板**：结构图节点 / 头像按 `agent-1..10` 着色，保持与 Org 一致。

### 设计稿坑（务必知道）

- **本会话内**：通过 `Insert` 新建的节点，其子内容在 MCP 截图/`snapshot_layout` 里**渲染为空白**；而 `Copy` 出来的节点能正常渲染。故所有验证都靠「`Copy` 到 Dark 版后截图」完成；Light 原件结构与 Dark 版一致，实时画布正常（旧屏 `Iphsb` 等 Light 屏渲染正常可证）。这是工具同步问题，不是设计问题。
- **不要跨主题用组件实例**：ref 实例在 light 屏渲染空白（既有文件正是因此手动复制 dark 屏，如 `Qs5Ff`）。本板块壳因此走内联 + Copy 翻主题。

## 8. i18n

新增可见文案走 `t(...)`，同步 `zh-CN` 与 `en` 的 `common.json`（现有 `orchestration.*` 已有 section/viewGraph/viewFeed/new 等）。新增建议键：`nav.orchestration`、`orchestration.overview.*`（title/subtitle/stats.*/inProgress/recentDone/empty）、`orchestration.sidebar.*`（title/new/filter.all|running|done）、`orchestration.run.*`（pause/resume/stop/target/intervene）、`orchestration.banner.deadlock|error`。**流程库文案无需新增** —— 已有完整 `workflows.*`（title/subtitle/new/empty/runCount/preview.*/editor.*/manager.* 等）与 `orchestration.new.flow*`（flowNone/flowLibrary/flowAdhoc/flowSelect…）。动态内容(agent/任务/消息)不翻译。

## 9. 取舍 / 未覆盖

- 旧编排屏（`VfK55`/`bOsgi`/`SSum1`/`fcbwn`/`HPdcC`/`VcUcz`/`XMieU`/`kGaru`/`H1rked`）**保留**作参考，被本板块取代；前端落地后可清理设计稿旧屏。
- **流程库能力已确认存在**：`workflow` 域完整落地 —— `workflow_entity` / `_repo` / `_svc`（List/Create/Update/Delete）+ `workflows` 表 + 前端 `workflow-manager-dialog`/`workflow-editor-form` + `run-new-dialog` 集成。**「从流程库套用为编排起始」也已通**：`run` 有 `FlowID` + `FlowContent`（创建时快照正文），`orch_svc/create.go` 持久化，Run 启动注入 Leader，进行中 Run 下一轮取最新正文。
- **⚠ 标签/步骤是设计提案，需小改动（落地前确认）**：现有 `workflow` **只有 `name` + `content`（自由 Markdown）**。为支持「给人一眼读懂」的标签/步骤（§6.1），需新增两个**仅展示、绝不注入 Leader** 的字段 `tags []string` + `outline []string`。改动面：① 迁移在 `workflows` 末尾 append `tags`/`outline` 列（native SQL，prefer JSON text 列）；② `workflow_entity`/`workflow_svc` 类型 + List/Create/Update 带上；③ 前端 `workflow-editor-form`（tags chips + outline 有序列表录入）、`workflow-manager-dialog`（列表标签 chip + 预览蓝图 band）、`run-new-dialog`（picker 行带标签+步骤）。**注入链路不动**（仍只 `content`）。若决定不加这两个字段，则退回「标签/步骤从 `content` 的 `## 步骤` 解析显示」的轻量方案（脆弱，不推荐）。
- **❗ agent↔agent peer ask/reply —— 部分已存在，但与原需求不一致（再次复查纠正）**：`orch_svc` 有 `ask`/`reply` MCP 工具 + `Ask()`（任意 agent 问任意 agent、阻塞等回复 ≤4min、`recordAskWait` 接死锁检测）。**但 `Ask()` 经 `SendAndForget`→`chat_svc.Send`→`startTurn`，而 `startTurn` 用 `lock.TryLock()`(`chat.go:2133`)，对方 busy（正在跑 turn）时直接返回 `ChatSendInFlight` 报错**。后果:① **「对方处理中也能 ask」没落地**（只 idle 目标可问）；② 实现**没复用 setter**（用新 `Send` turn，不是 `Enqueue`/steer）。所以前一版「后端齐了只差前端／不需要新后端」**是错的**:busy 目标要补后端。**busy 投递已定（用户拍板）：只 steer 进对方当前 turn**——`Ask()` idle 走 `Send`、busy 时走 `Enqueue`/steer 注入对方在跑的 turn（复用 setter），需把 `Enqueue` 加进 orch `ChatGateway`+`orch_adapter`。**接受权衡**：对方本 turn 内不调 `reply` → 提问方 4min 超时返错（已有超时分支，不新增机制）。见 roadmap S3。
- **❗ awaiting-user 内联审批后端未接线**：`TaskAwaitingUser` 常量 + `ApprovalGateway` 接口已定义但**零调用点**。钻入屏 / Run 右栏画的「批准/拒绝」阻塞目前**没有后端落地**，需补 `orch_svc` 的状态转换 + 调用 ApprovalGateway。
- **❗ Run 没有 "error" 状态**：Run 状态枚举只有 `pending/running/paused/done/stopped`；error 是 **task 级**（技术崩溃 `TaskError`）。「暂停·错误」屏的"错误"应表述为「某任务 error」，不是 Run 状态。**task 级 pause 也未用**（只有 Run 级 pause）。
- **✅ 复查确认可直接用的**：`callSeq`（×N 次调用）已实现且 `task-board.tsx` 已渲染（`#{seq}`），结构图边按 `parentTaskId` 连（`buildGraph`，同 agent 对去重）—— 「验签助手 ×2」是真数据；每个 task 有独立 `SessionID`（migration 加 `chat_sessions.run_id`）→「进入会话」钻入可行（读该 session 消息），需前端做「任务板⇄会话」切换。
- **任务板「5/12 done/total 计数」当前未实现**（现只逐条 task 状态）；设计稿的汇总计数是增项。
- **流程蓝图参考带只画在活动流（`Z2P0Vn`/`o6UQQ`，Light+Dark）**作演示；结构图/暂停/死锁/钻入等变体补该带是机械工作（改模板 → 删旧变体 → 重派生），本轮未做。
- **旧编排屏 = 2026-06-23 那批 14 个 dark 帧**（精确清单：`XMieU` 创建编排 Run 弹窗、`VfK55` 活动流、`OUC2c` 结构图、`LRVP7` 完成·审计、`QXzs0` 空·起步、`bOsgi` 无 Run 首次态、`VcUcz` ask 死锁、`VKELc` 节点钻入、`HPdcC` 暂停·错误、`pT2H4` 活动流·空起步、`fcbwn` 活动流·ask 死锁、`bj9u2` 活动流·完成审计、`SSum1` 活动流·暂停错误、`NmXoA` 结构图·大图），被本 10 屏板块取代。另有 2026-06-11 群聊批：`QTo6w` Group Chat v2、`kJa6T` 新建群聊弹窗、`DVZLR` 任务卡编排、`ovPMb` 任务卡组件 Dark、`hsqJ0` Workflows 流程库、`kGaru` 流程编辑弹窗（编排「取代群聊」后也属旧稿）。**以上 20 帧已于 2026-06-25 全部删除**（用户确认「旧编排 + 旧群聊/流程库都删」）；复用组件（`d4EDNs`/`ohDjP`/`cdydZ` 等）保留未动。设计稿现仅余 Agent Chat、Issues、Org、Hooks、通知、Popover 等 + 本编排新板块 20 帧。
- 真机 GUI 验证、远端(agentred)编排入口未涉及。
- 本轮**不动前端代码**，仅产出设计稿 + 本方案。
