# 群聊 Agent 编排 — 设计文档

- 日期：2026-06-03
- 仓库：`agentre/`（Wails 桌面端）
- 状态：设计待评审（brainstorming 产物，已对照 `chat_svc` 真实代码核对两处 seam，下一步 → writing-plans）

## 1. 背景与目标

当前 agentre 是**严格单 agent**模型：1 个 `ChatSession` = 1 个 Agent + 1 个工作目录；一个 turn = 一次 `RunRequest` → 一次 `Runtime.Run()` → 一条事件流，经由 **svc 级共享**的 `turn.Dispatcher` 路由（一个 `chatSvc` 实例一个 dispatcher，18 个 handler，靠每个 turn 传入的 `TurnContext` 携带 sessionID 区分会话 —— 不是 per-session）。消息按 `session_id` + `seq` 排序，role 仅 `user`/`assistant`，**没有"哪个 agent 说的"概念**。

目标：新增**群聊编排**能力 —— 一个共享房间里，由一个**协调者 agent（部门 leader）**牵头，动态把其他 agent 拉进群协作；成员之间、成员与用户之间通过 **@ 寻址**收发消息；**每个 agent 只看到 @ 到自己的消息**。

### 已确定的交互模型（来自 brainstorming）

| 维度 | 决策 |
| --- | --- |
| 核心模型 | 协调者牵头的群聊：协调者可 recruit 新成员;成员内部调用**复用现有单 agent 对话能力**;用户/agent 通过"发消息 + @收件人"参与;**agent 只看到 @ 到自己的消息** |
| 驱动方式 | **自动推进、随时可插话**:被 @ 的 agent 跑一个 turn,其输出里 @ 到谁就自动触发谁,链式推进;用户随时暂停/插话/纠偏;系统自带防死循环 |
| 工作区 | **讨论/协调为主、少数才动手**:MVP **不做** worktree/并发写隔离;成员可共用 project 的 cwd |
| 寻址协议 | 一条**自然消息** + 内联 `@名字`(自动补全),序列化为 `<mention>名字</mention>`;**收件人 = 文中所有 mention,都收到同一条**;**一个回合 = 一条消息**;投递进会话用「`(来自 X)` 抬头 + 正文」自然文本,不包 XML 信封 |
| 执行并发 | MVP **串行**(一次一个 agent turn),并行 fan-out 留作后续开关 |

## 2. 总体架构与分层

新增一个**自包含 domain `group_*`**（与单 agent 的 `chat_*` domain 高内聚解耦），作为**纯应用层编排器**，**架在 `chat_svc` 之上**（走 `chat_svc.Default()` accessor，是仓库认可的跨包协作方式）。运行时层（builtin/claudecode/codex/remote）**零改动**。

```
internal/app/group.go                         Wails 绑定(parse → svc → return,thin)
internal/service/group_svc/                   编排引擎(总线调度循环)
internal/repository/group_repo/               group / member / message 数据访问(sqlmock 单测)
  mock_group_repo/                            mockgen 产物(供 group_svc 单测)
internal/model/entity/group_entity/           充血实体(Group / GroupMember / GroupMessage)
migrations/2026xxxxxxxx_group.go              append 到 migrationList() 末尾
frontend/src/components/agentre/group-chat/   群聊面板(transcript / roster / composer)
frontend/src/stores/ + hooks/                 群列表 / 群详情 / live stream
```

依赖方向：`internal/app/group.go → group_svc → group_repo → group_entity`，且 `group_svc → chat_svc`（accessor，单向，无环）。`group_svc` **不直接** `db.Ctx` 摸 `chat_*` 表 —— 一切对单 agent 会话的操作都走 `chat_svc` 的方法。

### 命名

domain 包统一 `group_*`。如需避免与泛化的 "group" 撞概念，可改 `crew_*`/`squad_*` —— **待确认（见 §14）**。

## 3. 对现有代码的改动（仅 `chat_*` 两处 seam）

除新增包外，只在 `chat_*` domain 开两个最小、in-scope 的口子：

### 3.1 服务端 turn 完成观察口（`chat_svc`）

`Send` 是异步的（`gogo.Go` 起 goroutine 跑 `runTurn`，立即返回 `{SessionID, AssistantMessageID, Stream}`），事件发往 Wails stream。`group_svc` 需要在**服务端**知道某个成员的 turn 何时结束、拿到最终助手文本。

方案：在 `chat_svc` 的 turn finalize 处增加一个**服务端观察口**（不经 Wails）：

```go
// chat_svc 暴露
type TurnResult struct {
    SessionID          int64
    AssistantMessageID int64
    Text               string // 最终助手纯文本(从 finalBlocks 提取)
    Aborted            bool
    Err                error
}
// 订阅指定 session 的 turn 完成(返回取消函数)
func (s) ObserveTurn(sessionID int64) (<-chan TurnResult, func())
```

`group_svc` 在投递前订阅对应成员 backing session 的完成事件，turn 结束后从 channel 拿 `TurnResult` 解析寻址。比 DB 轮询 / 改 Wails emitter 干净，且对 `Send` 现有签名零侵入。

**关键落点（已对照代码核对）**：

- **必须在 turn 起点订阅，不在 finalize 处订阅。** `group_svc` 应先 `ObserveTurn(sessionID)` 拿到 channel，**再**调 `Send` —— 否则 turn 跑得快、回执先于订阅到达就丢了。
- **`TurnResult` 必须在所有退出路径都回灌一次，包括早退。** 正常 finalize 是单点（`chat.go:2395-2580`：`acc.Finalize()` → 落 blocks → 定 `agentStatus` → emit `StreamDone/Error/Aborted`），但 `Send` 里有**多个早退分支会绕过它**（`chat.go` 内 `failTurn`：`selectRunner` 失败、消息带不支持的图片、cwd 解析失败、`runner.Run()` 失败）。这些路径**也必须**向 observer 推一条 `TurnResult{Err: ...}`，否则成员 turn 一早退，`group_svc` 就永远阻塞在 channel 上、整条编排链死锁。
- 实现建议：在 `runTurn`/`failTurn` 共同的收尾处（或 `Send` 的 `gogo.Go` body 的 `defer`）统一 publish 一次 `TurnResult`，保证「订阅了就一定收到恰好一条终态」。

> 验证副记：中止 turn 的既有方法叫 **`Stop(ctx, *StopRequest)`**（`chat.go:1356`），不是 `StopChatMessage` —— §7 复用它。

### 3.2 backing session 归属与列表过滤（`chat_*`）

成员的 backing session 是真实的 `chat_sessions` 行（这样白嫖 history / provider-session 连续性 / steering / 工具权限 UI / capability gating）。为了**不污染普通单 agent 会话列表**：

- 迁移给 `chat_sessions` 增列 `group_id INTEGER NOT NULL DEFAULT 0`（原生 SQL，append 到 `migrationList()` 末尾）。
- **`DEFAULT 0` 不会自动把 backing session 从列表里藏掉 —— 必须在每个 list 查询显式加 `group_id = 0`。** 已核对 `chat_repo/session.go`：当前 list 查询全部只按 `agent_id`/`project_id` + `status = ACTIVE` 过滤，共 **8+ 个入口**（`session.go:72` 起的 `ListByAgent` / `ListByAgentPaged` / `ListByProject` / `ListAttentionByAgent` / `ListIDsByAgents` 等）都要补 `AND group_id = 0`。漏一个，群成员的 backing session 就会泄进普通单 agent 会话列表。
  - 收口建议：在 repo 层把 `group_id = 0` 做成所有「默认会话列表」查询共用的一个 scope/where 片段，避免每个查询各写各的、再漏。
- 加索引覆盖新过滤维度：`CREATE INDEX ... ON chat_sessions(agent_id, group_id, status)`（list 查询主路径），群内成员枚举另走 `group_members.group_id`，不依赖此列扫表。
- `chat_svc` 增一个 `EnsureGroupMemberSession(ctx, agentID, projectID, groupID) (sessionID int64, err error)`，创建/返回带 `group_id` 的 backing session；`group_svc` 在 recruit 时调用，随后正常 `Send(sessionID=...)` 跑 turn。

> 这是 `chat_*` domain 自己的字段（"会话可隶属某个群"），耦合可控；`group_svc` 不碰该列。

## 4. 数据模型

三张新表（迁移用原生 SQL，append 到 `migrationList()` 末尾，**禁改既有迁移**）。

### `groups`

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | PK | |
| `title` | text | 群名 |
| `coordinator_agent_id` | int64 | 协调者(部门 leader) |
| `department_id` | int64 | 可招募名单的来源(0=不限/显式 allowlist) |
| `project_id` | int64 | 默认 cwd(0=free) |
| `run_status` | text | `idle`/`running`/`paused`/`waiting_user`/`error` |
| `round_count` | int | 自上次用户发言以来的 agent turn 计数 |
| `max_rounds` | int | 防死循环上限(默认 30) |
| `status` | int | `consts.ACTIVE` / 归档 |
| `created_at`/`updated_at` | | |

### `group_members`

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | PK | 群内稳定身份 |
| `group_id` | int64 | |
| `agent_id` | int64 | |
| `backing_session_id` | int64 | 该成员在本群的 `chat_sessions.id` |
| `role` | text | `coordinator`/`member` |
| `status` | text | `active`/`left` |
| `joined_at` | | |

> 显示用的 name/color/icon 从 Agent 实体读取，**不冗余存**（entity 为 source of truth）。

### `group_messages`

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | PK | |
| `group_id` | int64 | |
| `seq` | int | 群内排序 |
| `sender_kind` | text | `user`/`agent` |
| `sender_member_id` | int64 | agent 发言时=member id;user=0 |
| `recipient_member_ids` | text(json) | 收件成员 id 列表 |
| `to_user` | bool | 是否回给用户 |
| `content` | text | 正文(始终存原文,即便没解析出寻址) |
| `source_message_id` | int64 | 派生自的 `chat_messages.id`(可溯源;user=0) |
| `created_at` | | |

**@ 过滤视图天然涌现**：成员 backing session 只会收到 @ 到它的消息 → "agent 只看到 @ 到自己的消息"无需任何额外过滤逻辑。

充血实体方法示例：`Group.CanAdvance()`（round_count < max_rounds 且 run_status 允许）、`Group.NextSeq()`、`GroupMessage.Recipients()/SetRecipients()`、`GroupMember.IsCoordinator()`。

## 5. 编排引擎（串行消息总线）

`group_svc` 为每个活跃群跑一个调度器，核心是一个**待投递队列**：

1. 一条消息被 post（用户 composer，或从某成员输出解析而来），带 `recipients`。
2. 调度器取下一条 **agent 投递** → 对该成员 backing session 调 `chat_svc.Send`，正文加 `(来自 X)` 自然抬头（§6）→ `ObserveTurn` 等待 turn 完成。
3. 成员最终文本解析出内联 `<mention>` 标签 → 整条输出成为**一条** `group_message`,收件人 = 文中所有 mention → 其中的 agent 收件人入队 → 循环。无 mention 的输出走兜底（§6）。
4. 队列空 → `run_status = waiting_user`。用户随时 post；插话即追加并踢一脚调度器。

**串行（MVP）**：一次一个成员 turn（FIFO）。理由：日志确定、可读；stop/插话实现简单；防护好做。并行 fan-out（协调者同时 @ 三人征询）作为后续开关。

`run_status` 状态机：`idle → running`（有待投递）`→ waiting_user`（静默）/ `paused` / `error`；stop → `idle`。

后台调度用 `gogo.Go`，**不透传请求 ctx 进 goroutine**（用独立 ctx）。

## 6. 寻址协议（一条自然消息 + 内联 `<mention>`）

设计原则：**像真实群聊**。一条消息就是自然正文,只把 @提及 用标签包起来;被 @ 到的人都收到**同一条**;**一个回合产出一条消息**。要分别交代不同内容?下一回合再说一条 —— 本就是自动推进的聊天。

- **作者层（用户）**：发送框直接打 `@名字`（成员自动补全），前端把每个提及序列化为 `<mention>名字</mention>`，收件人 = 文中所有 mention。
- **作者层（agent）**：系统提示注入协议说明，让成员正常说话,只在提到谁时写 `<mention>名字</mention>`（或裸 `@名字`,见兜底）。**不要求 agent 产出 XML 骨架/多块**。
- **传输/会话层**：投递进成员 backing session 时用**自然文本**：开头一行 `(来自 林队)` 抬头 + 正文（正文里指向自己的 mention 保留）。作为该轮 user 输入。**不包 XML 信封**。
- **解析**：编排器从成员最终文本提取所有 `<mention>` 标签 → 对照当前 roster（+ `@用户`）匹配成 member id,作为这条消息的收件人集合。裸 `@名字` 也尽量规范化匹配。
  - **协调者 mention 了「在部门名单、但还没进群」的 agent** → 视为**招募**：自动加成员 + 起 backing session + 投递（见 §7,无需单独 `@recruit` 语法）。
- **名称唯一性**：群内成员显示名唯一（招募/改名时校验），mention 按名字解析;解析不到的 mention → 兜底 + flag。
- **兜底**：成员输出没有任何 mention（纯正文）→ **回给上一个发消息的人**；原文始终入库不丢。
- **静默(quiesce)**：输出没有指向任何 agent 成员（只 `@用户` 或无人）→ 回合结束，群转 `waiting_user`。

> 标签名 **`<mention>`**（已与用户确认）。

## 7. 控制流：招募 / 终止 / 插话

- **招募**：协调者 `<mention>` 一个**在部门名单、尚未进群**的 agent → `group_svc` 自动新增成员、`EnsureGroupMemberSession` 起 backing session、post 一条系统消息"X 加入",并把这条消息投递给它（招募与 mention 统一,无需单独语法）。成员数上限 ~8。**MVP 仅协调者能触发自动招募**（非协调者 mention 名单外/未进群 agent → 忽略并 flag）；用户也可在 UI 手动加任意 agent。
- **防死循环**：`max_rounds`（默认 30，**用户发言时清零**）→ 超限则暂停为 `error`/"已达最大轮数"并提示用户。自然终止 = 静默或回 `@用户`。
- **插话/暂停/停止**：用户 post = live 追加；stop 取消正在跑的成员 turn（复用 `chat_svc.Stop(ctx, *StopRequest)`，`chat.go:1356`）+ 清空队列 + `run_status=idle`；pause 停止新投递、让当前 turn 跑完。
- **mid-turn 插话**：若用户在某成员忙时 @ 它，MVP 在群级排队、下一轮投递（不强行 steer；如需即时可后续接 `EnqueueChatMessage`）。

## 8. 工具权限 / 交互事件透传

成员 turn 可能触发 `ToolPermissionRequest` / `UserAskRequest`（讨论为主时较少，但会有）。这些事件仍在 backing session 的 stream 上触发：

- MVP 把它们作为**系统行**冒泡进群 transcript（"成员 X 请求运行 …，待你批准"）。
- 用户在群 UI 上批准/回答 → 复用现有 `AnswerToolPermission` / `AnswerUserQuestion`（作用于该 backing session）。

## 9. Wails 绑定与事件流（`internal/app/group.go`，thin）

方法只做 parse → `group_svc.Xxx()` → return：

- `ListGroups()` / `CreateGroup(req)` / `LoadGroup(id)`（房间 + 成员 + 消息日志）
- `SendGroupMessage(req)`（groupId, text；收件人从内联 @ 解析，或显式 recipientMemberIds/toUser）
- `AddGroupMember(req)` / `RemoveGroupMember(req)`
- `StopGroup(id)` / `PauseGroup(id)` / `ResumeGroup(id)`
- `RenameGroup(req)` / `ArchiveGroup(id)` / `MarkGroupRead(id)`

实时：群事件流 `group:event:<groupId>`，推送新消息、成员状态、run_status 变化、系统行（加入/权限请求/达上限）。前端 `EventsOn` 订阅，写入 zustand store。

## 10. 前端 UI/UX

新面板 `frontend/src/components/agentre/group-chat/`（已用可视化 companion 验证布局）：

- **房间头**：群名 + run_status pill（运行中/等待你/已暂停）+ 轮次计数 + 暂停/停止。
- **左侧成员栏**：协调者 + 成员，含实时状态（思考中/待批准/空闲），"+ 邀请成员"。
- **transcript**：按发送者着色的气泡（头像=颜色块+首字），`→ @收件人` chips 表达路由，定向消息下灰字"仅 X 收到"提示过滤视图；系统行居中；工具权限以系统行内联出现。
- **发送框**：自由文本 + 内联 `@` 自动补全（无独立收件人 chip 选择器）；收件人从 @ 提及解析。

约束：所有静态文案走 `react-i18next` 的 `t(...)` + 同步 `zh-CN`/`en` common.json；表单控件用 shadcn `@/components/ui/*`，禁原生 `<select>`；agent/用户/消息**内容不翻译**。复用现有 `CanonicalToolRouter` 渲染成员工具卡。

## 11. 测试策略（严格 TDD：Red → Green → Refactor）

- **Repository 单测**：`group_repo` / member / message 一律 `testutils.Database(t)` + sqlmock，禁真库。
- **Service 单测**：mockgen 生成 `mock_group_repo`，并 mock `chat_svc` 的 `EnsureGroupMemberSession` / `Send` / `ObserveTurn` / `Stop` seam（注入接口）。BDD/goconvey 覆盖：
  - happy path：用户 → 协调者 → 成员 → 回用户。
  - 寻址解析：多 mention 都收到同一条 / 名称唯一解析 / 无 mention 兜底到上一个发送者 / 只 @用户或无人 → quiesce。
  - 招募流：协调者 mention 名单内未进群 agent → 新成员 backing session 建立 + 系统消息;非协调者触发 → 忽略并 flag。
  - 边界：`max_rounds` 超限 → 暂停 error；用户发言清零计数。
  - 错误：成员 turn 报错 / abort 的传播与 run_status。
- **迁移测试**：新表的 `*_test.go`（迁移自身可起真库，属白名单例外）。
- **前端 Vitest**：群面板渲染、`@` 自动补全、定向 chip 与"仅 X 收到"渲染、stream 事件 → store。
- 跑法：`make test-backend`（后端 race，排除 frontend）+ `cd frontend && pnpm test -- <file>`。

## 12. 错误码与 i18n

- 在 `internal/pkg/code/code.go` 为 group domain 分配新错误码段（issue 用了 18200；group 取下一个空闲段，**实现时确认**，如 18300），文案补 `zh_cn.go`/`en.go`，用 `i18n.NewError(ctx, code.Xxx)`。
- 关键流程打日志：`logger.Ctx(ctx)`，message 用 `group_svc.Method:` 前缀小写，动态值走 `zap.Xxx`。

## 13. MVP 范围 / 非目标

**IN（MVP）**：房间 + 协调者；协调者自动招募（mention 名单内未进群 agent）+ 用户手动加成员；内联 @ + `<mention>` 寻址解析（一条消息一回合，收件人都收同一条）；串行自动推进 + 防护；用户插话/暂停/停止；持久化（3 表 + chat_sessions.group_id）；群面板 UI；复用 `Send`/工具权限透传。

**OUT（后续）**：并行 fan-out;worktree/并发写隔离;builtin 真 tool 注入(parse 不稳再上);DAG/工作流编辑器;跨群/嵌套群;高级环检测;remote-daemon 成员(经 `Send` 可传递跑通,但本期不专门测).

## 14. 已定默认值 & 待评审确认项

下列为我替你拍的默认，评审时可推翻：

1. **执行串行**（非并行）—— MVP。
2. **招募 = 协调者 mention 名单内未进群的 agent**（仅协调者可触发）；可招募名单 = 部门成员；成员上限 ~8。
3. **寻址 = 一条自然消息 + 内联 `<mention>名字</mention>`**（已与用户确认标签名）；收件人 = 文中所有 mention,都收到同一条;一个回合 = 一条消息;投递用 `(来自 X)` 自然抬头,不包 XML 信封。
4. **无标签兜底 = 回上一个发送者**；无寻址 = quiesce 转 `waiting_user`。
5. **`max_rounds` 默认 30，用户发言时清零**。
6. **工具权限/提问**冒泡为系统行，复用现有 handler 应答。
7. **domain 包名 `group_*`**（vs `crew_*`/`squad_*`）。
8. **`chat_svc` 两处 seam**（`ObserveTurn` 服务端观察口 + `chat_sessions.group_id` 列 & `EnsureGroupMemberSession`）—— 唯一改动到的既有 domain。**已对照代码核对（2026-06-03）**：seam 成立，但 ① `ObserveTurn` 必须 turn 起点订阅且覆盖 `failTurn` 早退（见 §3.1）；② `group_id` 过滤需在 8+ 个 list 查询显式做 + 加索引（见 §3.2）；③ 中止方法叫 `Stop` 非 `StopChatMessage`；④ `turn.Dispatcher` 是 svc 级共享而非 per-session（措辞已修，见 §1）。

## 15. 关键不变量自检

- 绑定层只 parse→svc→return，业务全在 `group_svc`。
- `group_svc` 只依赖 repository 接口 + `chat_svc` accessor；不反向被 `pkg/` import。
- 迁移 append、禁改既有；DDL 原生 SQL。
- repo 单测 sqlmock、service 单测 mockgen 注入、不接真库。
- 前端 i18n + shadcn；动态内容不翻译。
- diff 只含本特性 producer + 测试，无 drive-by。
