# 配置引导 / 用户引导（Setup Guidance）

<!-- File: docs/specs/2026-08-08-setup-guidance.md -->

> Status: Draft
> Owner: 桌面端前端 + chat_svc 后端
> Last updated: 2026-08-08

**Objective:** 新用户首次打开 Agentre 时，能在「为什么还不能对话 → 缺什么 → 一键跳到对应配置页 → 完成后回来就能用」这条链路上被逐步引导，而不是被一句 3 秒消失的文字提示、一个空的命令面板或一个能打字却发不出去的新会话卡住。

**Hard invariant:** 单机已有全部配置（Agent 已绑后端、供应商已激活、网关已启动）的用户，本轮任何界面元素（徽标、引导弹窗、空态步骤、导航打点）都不得出现；不可对话的判定逻辑保持与现状一致（`chat_svc/chat.go:378-410` 的可对话条件不变，只把「原因」从一段中文串升级为结构化枚举）。

## Problem

1. **全新安装后没有任何可对话的 Agent，但界面不解释为什么、也不指路。** 应用只 seed 一个不带后端的「CEO 助手」（`migrations/202605220004_agents.go:60-69`），空聊天态只有「选一个 Agent 或项目下的会话开始」+ 快捷键（`chat-panel-host.tsx:65-93`），无按钮、无步骤。
2. **不可对话的 Agent 只有一条 3 秒消失的文字提示，没有按钮、不跳转。** 点 Agent 头部触发 `showNotChattableNotice`（`chat-page.tsx:247-252 / 636-649`），提示内容本身是 Go 后端硬编码中文（`chat_svc/chat.go:378-410`），前端拿不到「缺什么、该跳去哪」的语义。
3. **入口行为自相矛盾，用户会被骗进一个注定失败的输入框。** 命令面板「New chat with」只列 chattable 的 Agent（`new-chat-source.tsx:48-56`），全新安装时 CEO 直接被过滤、面板空白；而侧栏「+」却能给不可对话 Agent 开出新会话 tab，输入框完全可用（`chat-panel.tsx` 不读 chattable），用户打一大段按回车才报错。
4. **「新建 Agent」无处可去。** 全新用户想加一个助手，但「+」下拉只有「新建 Agent 会话」（开命令面板），没有任何入口引导去组织架构创建 Agent。
5. **设置侧两个空态不讲链条、不留退路。** Agent 后端空态只有一行灰字（`agent-backends.tsx:563-570`），不解释「为什么需要后端、内置后端还要供应商」；LLM 供应商空态有 CTA 但不提「配好后要回去绑后端」。
6. **组织架构里「缺配置」不预警。** Agent 详情已有后端选择器（`org/org-detail-agent.tsx:462-520`），但没绑后端/供应商未激活时没有内联提示；组织架构树空态只有文字无按钮（`org-tree.tsx:815-840`）。

## Actors and user stories

1. 作为**全新用户**，我打开应用后想立刻和 CEO 助手对话，所以我需要知道「还差什么配置、点哪里去配」，以便最小步数完成配置并开始第一次对话。
2. 作为**配置不全的用户**，我点一个还不能对话的 Agent 时，需要看到「缺什么 + 一键跳转」，而不是读一段文字后自己去找设置页。
3. 作为**想新增助手的新用户**，我希望能从对话页直接跳到组织架构创建 Agent，而不是找不到入口。
4. 作为**已配好一切的老用户**，我不希望看到任何引导元素，以免被反复打扰。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | **不可对话原因升级为结构化 `blockReason` 枚举**（后端 `chat_svc.ChatAgentItem` 新增字段），`ChattableHint` 保留为兜底展示字段。 | 前端要渲染「该跳去哪」的 CTA 按钮，必须拿到语义化原因。Rejected: 前端按 hint 文本正则匹配 — 脆、改文案即坏、违反 i18n（硬编码中文在 Go 侧）。 |
| 2 | **不可对话引导统一用弹窗**，侧栏行内只放徽标、不展开。 | 弹窗能容纳「原因 + 链条预告 + 1~2 个按钮」，可被点 Agent 头部 / 点 + / 命令面板选中 / 新会话 tab 四个入口复用。Rejected: 侧栏行内展开面板 — 更轻但易漏；只给 3 秒条加按钮 — 可发现性差。 |
| 3 | **「去配置 Agent 后端」主按钮跳组织架构页并自动选中该 Agent 详情**（深链：写入 `agentre.orgTree.selected` localStorage 后 `navigate("/org")`），弹窗保留次要按钮「去 Agent 后端设置」。 | 后端绑定发生在 Agent 上，详情面板的后端选择器（`org-detail-agent.tsx:462-520`）就在那里；设置 → Agent 后端只负责建后端，绑到谁还得回组织架构。Rejected: 只跳设置 → Agent 后端 — 多一步。 |
| 4 | **不可对话 Agent 的新会话 tab 禁用输入框**，输入框上方内联引导块（与弹窗同文案 + 按钮）。 | 杜绝「打一段才报错」。Rejected: 保留可用输入框 + 发送前报错 — 浪费用户输入，且与现状无异。 |
| 5 | **聊天页「+」下拉新增「新建 Agent」项**（与「新建 Agent 会话」以分隔线隔开），点击跳 /org 并自动弹出新建 Agent 弹窗（预选「挂到 CEO 助手下」）。命令面板同步新增 `New agent` 命令，行为一致。 | 把「建新助手」从对话入口直接送到组织架构，全新安装也能用（有 CEO 可挂、后端可选）。Rejected: 只跳到 /org 让用户自己点按钮 — 少一步引导。 |
| 6 | **空聊天态分两档**：没有任何可对话 Agent 时显示两步配置引导（配置 Agent 后端 → 配置 LLM 供应商，各带跳转按钮）；已有可对话 Agent 时保留现有占位 + 底部「N 个 Agent 未配置后端」链接。 | 全新安装的「第一步」应该是指路而不是一句空话；老用户不被打扰。 |
| 7 | **设置侧补齐链条与退路**：Agent 后端空态升级为带说明 + 「先配置 LLM 供应商」按钮；LLM 供应商空态补「LLM 供应商 → Agent 后端 → Agent 对话」链条提示；设置导航「Agent 后端 / LLM 供应商」在确实有缺口时打 warning 点；组织架构 Agent 详情未绑后端时内联黄条、选了内置后端但无可用供应商时内联「去配置 LLM 供应商」。 | 每个空态/告警都给一条可点的出路，避免卡死在某个配置页。Rejected: 只改文案不加跳转 — 仍要用户自己找路。 |
| 8 | **徽标与打点的触发条件**统一为「`blockReason` 非空 / 有缺口」，配好即消失。 | 与可对话判定一致，避免两套判定漂移。 |

## Design

### 1. 结构化 `blockReason`（后端）

`chat_svc.ChatAgentItem` 新增字段 `BlockReason string json:"blockReason"`（空串 = 可对话），取值与语义：

| blockReason | 含义 | 主按钮（跳转） |
|---|---|---|
| `no-backend` | 该 Agent 没绑后端（含 CEO） | 去组织架构绑定后端（深链到该 Agent 详情） |
| `backend-requires-provider` | 内置后端没绑/找不到绑定的供应商 | 去设置 → LLM 供应商 |
| `provider-inactive` | 后端绑的供应商存在但未激活/缺 Key | 去设置 → LLM 供应商 |
| `remote-provider-missing` | 远端 agentred 未配置该供应商 | 去设置 → 远端设备 |
| `gateway-not-running` | 本地网关未启动，CLI 后端暂不可用 | 启动本地网关（设置 → Agent 后端） |
| `remote-openclaw-unavailable` | 远端 OpenClaw 暂不可用 | 去设置 → 远端设备 |
| `unknown-backend` | 未知 Agent 后端类型 | 去设置 → Agent 后端 |

- 可对话判定逻辑**不变**；`blockReason` 与 `ChattableHint` 在同一点位设置，保持二者一致。
- `ChattableHint` 保留：弹窗里作为次要说明展示（兜底），其余消费方（命令面板行、旧逻辑）不再依赖它做跳转。
- `remote-provider-missing` 的 hint 里不再内嵌 `agentred llm add --key=%s` 命令（该命令属于远端 agentred，桌面端文案只引导去「远端设备」页）。

### 2. 不可对话引导弹窗（前端核心）

- 触发入口（四个一致）：① 点侧栏不可对话 Agent 头部；② 点该行「+」；③ 命令面板选中「需要先配置」分组的项；④ 新会话 tab 内联引导块的按钮。
- 弹窗内容：标题（如「CEO 助手暂时不能对话」）、原因描述、**链条预告**（`Agent 后端 → LLM 供应商`，缺失环节高亮）、按 `blockReason` 映射的主按钮 + 次要按钮（「去 Agent 后端设置」）+ 取消。
- `no-backend` 弹窗额外显示 info 提示「内置后端需要额外的 LLM 供应商，向导会提示你是否还需要配置」。
- 主按钮跳转后弹窗关闭；跳转目标由 D3 / 决策表给出。
- 现有的 3 秒提示条（`chat-page.tsx`）删除，替换为弹窗。

### 3. 侧栏 Agent 行徽标

- 不可对话 Agent 行常驻「未配置」徽标（status-waiting 色，文字按 `blockReason` 细分：`no-backend` →「未配置后端」，其余 →「未配置」），不随 hover 出现/消失。
- 可对话 Agent 行无徽标（现状不变）。
- 徽标不承载点击（点击整行走弹窗，见 §2）。

### 4. 新会话 tab 输入守卫

- 不可对话 Agent 的 `kind:"new"` tab：输入框上方内联引导块（徽标 + 标题 + 原因 + 主/次按钮，复用弹窗文案），`ChatComposer` 传 `disabled`，占位文案改为「请先配置 Agent 后端」。
- 可对话 Agent 的新会话 tab：现状不变（占位 + 可用输入框）。

### 5. 命令面板

- `new-chat-source` 不再只列 chattable：可对话 Agent 保持「New chat with」组；不可对话 Agent 单列「需要先配置」组（行内显示原因 + 「未配置」徽标），选中后**不建会话**、打开引导弹窗（§2）。
- 新增 `New agent` 命令（`commandPalette` 顶层），选中后跳 /org 并自动弹出新建 Agent 弹窗（§6）。
- 面板空的兜底文案保持现状（「暂无会话 · 先开一个 Agent 对话」）。

### 6. 新建 Agent 入口与组织架构深链

- 聊天页「+」下拉：`新建 Agent 会话`（保留，开命令面板）→ 分隔线 → `新建 Agent`（图标 UserPlus/Bot，跳 /org）。
- 命令面板新增 `New agent` 命令，行为同「新建 Agent」。
- 跳转行为：`navigate("/org")` + 一个「打开新建 Agent 弹窗」意图（新增轻量 intent 存储，见测试决策），组织架构页挂载后自动打开 `NewAgentDialog`，预选挂载位置为「挂到 CEO 助手下」（无部门时）；后端可暂不选（`agent_svc.Create` 允许 `agentBackendId=0`，`agent.go:392-402`）。弹窗内 info 提示「创建后先配后端再对话」。
- 若已在 /org 页，重复触发时重新打开弹窗（幂等）。

### 7. 空聊天态（组 1B / 1C）

- **无任何可对话 Agent**：主区显示两步配置引导——标题「开始前，先完成两步配置」+ 说明 + 两步卡（① 配置 Agent 后端 → 去设置 → Agent 后端；② 配置 LLM 供应商 → 去设置 → LLM 供应商）+ 底部「配置完成后回来，就可以和 CEO 助手开始对话了」。保留现有快捷键提示。
- **有可对话 Agent**：保留现有占位，底部补一行「N 个 Agent 未配置后端」徽标 + 「去组织架构配置 →」链接（跳 /org）。

### 8. 设置侧

- **设置导航打点**：`Agent 后端` / `LLM 供应商` 两个导航项，在「存在未配置后端的 Agent」/「存在绑了未激活供应商的后端」时各显示一个 warning 点（status-waiting），全部配好后消失。点不承载额外行为。
- **Agent 后端空态**（替换 `agent-backends.tsx:563-570` 单行文字）：空态卡（图标 + 标题 + 说明「每个 Agent 都要绑定一个后端才能执行任务；内置 SDK 还需要一个已激活的 LLM 供应商」）+ 「新增第一个后端」主按钮 + 「先配置 LLM 供应商」次按钮（跳 LLM 供应商页）。
- **LLM 供应商空态**（扩展现有 `ProvidersEmptyState`）：在现有按钮与「如何获取 API Key」下方补链条提示「LLM 供应商 → Agent 后端 → Agent 对话」+ 「去 Agent 后端」链接。
- **LLM 供应商页有缺口时**：页内黄条「还有 Agent 缺这个」+「去组织架构」链接。

### 9. 组织架构内联提示

- **Agent 详情未绑后端**（`org-detail-agent.tsx` 后端小节上方）：内联黄条「这个 Agent 还不能对话」+ 说明「绑定一个 Agent 后端后即可开始对话；内置后端需要额外的 LLM 供应商」。后端选择器保持原位。
- **后端选了内置但无可用供应商**：黄条变为「还需要一个 LLM 供应商」+ 内联「去配置 LLM 供应商」按钮 + 「去 Agent 后端设置」链接。
- **新建后端表单**（`agent-backends.tsx` 编辑表单）：供应商下拉为空时，下拉下方内联提示「还没有可用的 LLM 供应商」+「去配置 LLM 供应商」按钮。
- **组织架构树空态**：保留文字，新增按钮（新建部门 / 添加 Agent，走现有弹窗）。

### i18n

- 以上所有新增/变更文案（弹窗、徽标、空态、链条、导航点、按钮）一律 `t(...)`，同时落 `frontend/src/i18n/locales/{zh-CN,en}/common.json`，通过 `i18n.test.ts` 守卫。
- `blockReason` 枚举在**前端**映射为文案（前端拥有文案，后端只给枚举），不再有 Go 侧硬编码中文 UI 文案。

### 无障碍 / 可访问性

- 弹窗用现有 shadcn `Dialog`（焦点圈闭、Esc 关闭）；引导块含 `role="alert"`/`aria-live` 播报不可对话状态。
- 徽标带文字不靠颜色单表意；导航 warning 点旁有 `title` 说明。
- 键盘路径：弹窗按钮、命令面板项均可 Tab + Enter 操作。

## Out of scope

- 不改可对话判定的业务逻辑（仅增加原因枚举）。
- 不做完整「首次启动向导 / welcome wizard」（组 1B 的两步引导已够，不做多步引导页）。
- 不引入新的后端配置项；不重做 LLM 供应商 / Agent 后端表单本身。
- 不动 projects / issues / hooks 页（已有 CTA 空态）。
- 远端 agentred 侧的 `llm add` 命令交互不改，仅文案指向「远端设备」页。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| `chat_svc`（sqlmock + mockgen，`chat_svc/chat_test.go`） | `blockReason` 在每种不可对话分支（no-backend / backend-requires-provider / provider-inactive / remote-provider-missing / gateway-not-running / remote-openclaw-unavailable / unknown-backend）被正确设置，且可对话分支为空；`Chattable` 判定不变 | `TestRegisterGatewayBeforeNewChatMakesCLIBackendsChattable` 等现有 chattable 用例 |
| 前端组件测试（vitest + happy-dom，wails 绑定 mock） | 引导弹窗：四个入口均打开、按 blockReason 渲染对应主按钮、主按钮触发正确跳转；侧栏徽标只出现在不可对话行；新会话 tab 不可对话时输入框 `disabled` 且显示引导块；命令面板「需要先配置」分组、选中开弹窗不开会话；「新建 Agent」入口跳 /org 并触发打开弹窗意图 | 现有 `chat.test.tsx` / `command-palette` 测试 |
| i18n 覆盖（`i18n.test.ts` / eslint-i18n） | 新增键 en/zh-CN 双落、无硬编码中文 | 现有 i18n 守卫测试 |
| 深链意图存储 | 写意图 → 组织架构页挂载时读并打开 `NewAgentDialog`、读后清除 | 新增小单元测试 |

无法自动化的部分：深色主题下的徽标/黄条对比度、弹窗在窄窗口（860px）下的排版——由 wrap-up 时在应用里打开 `setup-guide.html` / `settings.html` 对应状态人工核对（mockup 已含深色与窄视口形态）。

## Open questions

（无 —— 批准前必须为空）
