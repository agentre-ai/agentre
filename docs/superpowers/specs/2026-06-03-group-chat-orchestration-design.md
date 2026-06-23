# 群聊 Agent 编排 — 设计文档

- 日期：2026-06-03
- 仓库：`agentre/`（Wails 桌面端）
- 状态：设计待评审（brainstorming 产物，已对照 `chat_svc` / `pkg/claudecode` / `httpgateway` / `capability` 真实代码核对全部 seam，下一步 → writing-plans）
- 2026-06-03 更新：寻址机制由"文本解析 `<mention>`"升级为 **MCP `group_send` tool**（结构化收件人），并新增**能力门控** `CapMCPTools`（MVP 仅 claudecode）。`<mention>` 标记保留，仅用于**前端展示高亮 + 点击跳转**。

## 1. 背景与目标

当前 agentre 是**严格单 agent**模型：1 个 `ChatSession` = 1 个 Agent + 1 个工作目录；一个 turn = 一次 `RunRequest` → 一次 `Runtime.Run()` → 一条事件流，经由 **svc 级共享**的 `turn.Dispatcher` 路由（一个 `chatSvc` 实例一个 dispatcher，18 个 handler，靠每个 turn 传入的 `TurnContext` 携带 sessionID 区分会话 —— 不是 per-session）。消息按 `session_id` + `seq` 排序，role 仅 `user`/`assistant`，**没有"哪个 agent 说的"概念**。

目标：新增**群聊编排**能力 —— 一个共享房间里，由一个**主持人 agent（部门负责人）**牵头，动态把其他 agent 拉进群协作；成员之间、成员与用户之间通过 **@ 寻址**收发消息；**每个 agent 只看到 @ 到自己的消息**。agent 通过一个注入的 **`group_send` MCP tool** 主动发言（不是把整段回复广播出去）。

### 已确定的交互模型（来自 brainstorming）

| 维度 | 决策 |
| --- | --- |
| 核心模型 | 主持人牵头的群聊：主持人可 recruit 新成员;成员内部调用**复用现有单 agent 对话能力**;用户/agent 通过"发消息 + @收件人"参与;**agent 只看到 @ 到自己的消息** |
| 发消息机制 | **agent 调 `group_send(body, mentions[])` MCP tool 发言**（一次调用 = 一条群消息，一个 turn 内可多次、可对不同人发不同内容）。成员的私有叙述/思考**不进群**，只有 tool 调用进群。门控:**只有声明 `CapMCPTools` 能力的 backend(MVP 仅 claudecode)可入群** |
| 驱动方式 | **自动推进、随时可插话**:被投递消息的 agent 跑一个 turn,turn 中 `group_send` 给谁就自动触发谁,链式推进;用户随时暂停/插话/停止;**无 max_rounds 防死循环**(靠 stop + 没人被 @ 的自然静默收敛) |
| 工作区 | **讨论/协调为主、少数才动手**:成员共用 project 的 cwd;并发写竞态不靠强制隔离基础设施, 而是在系统提示里**引导**要动手的成员自建 git worktree(coding agent 有 shell, 自己跑 `git worktree add`),用不用交给 agent 判断 |
| 寻址协议 | **路由**=结构化:`group_send` 的 `mentions []string`(agent)/ UI 选中的 `recipientMemberIds`(用户)。**展示**=`<mention>名字</mention>` / 内联 `@名字` 标记,前端渲染成高亮可点 chip,点击跳转到该成员会话。**收件人 = mentions 全体,都收到同一条**;一个回合可发多条 |
| 执行并发 | **并行 fan-out**(eager 路由):一条消息 @ 的独立成员**全部并发跑**(并发数受成员上限 ~8 自然约束, 无单独 `max_concurrency` cap);依赖链(A→B→C)跨波次天然顺序。共享 cwd, 要写文件的成员经系统提示**引导**自建 git worktree(agent 自行决定, 无强制隔离基础设施) |

## 2. 总体架构与分层

新增一个**自包含 domain `group_*`**（与单 agent 的 `chat_*` domain 高内聚解耦），作为**纯应用层编排器**，**架在 `chat_svc` 之上**（走 `chat_svc.Chat()` accessor，是仓库认可的跨包协作方式）。**不新增 backend**（不走 agent-backend.md 的 7 层 onboarding）—— 只给现有 `claudecode` runtime **加一个能力 + MCP tool 注入管线**。

```
internal/app/group.go                         Wails 绑定(parse → svc → return,thin)
internal/service/group_svc/                   编排引擎(并发 worker pool 调度循环 + MCP tool 入口)
internal/repository/group_repo/               group / member / message 数据访问(sqlmock 单测)
  mock_group_repo/                            mockgen 产物(供 group_svc 单测)
internal/model/entity/group_entity/           充血实体(Group / GroupMember / GroupMessage)
migrations/2026xxxxxxxx_*.go                  append 到 migrationList() 末尾
frontend/src/components/agentre/group-chat/   群聊面板(transcript / roster / composer + mention chip 渲染)
frontend/src/stores/ + hooks/                 群列表 / 群详情 / live stream

—— 对现有代码的改动（seam）：
internal/pkg/agentruntime/capability/         + CapMCPTools 常量
internal/pkg/agentruntime/runner.go           RunRequest + MCPServers 字段
internal/pkg/agentruntime/runtimes/claudecode/  Capabilities() 声明 CapMCPTools + 映射 MCPServers
pkg/claudecode/{args.go,options.go}           + --mcp-config flag + WithMcpConfig
internal/pkg/httpgateway/                     注册 /mcp/group MCP handler(group_send tool)
internal/service/chat_svc/                    ObserveTurn 观察口 + group_id 列 + EnsureGroupMemberSession
                                              + SendRequest 透传 MCPServers / SystemPromptSuffix(领域无关)
```

依赖方向：`internal/app/group.go → group_svc → group_repo → group_entity`，且 `group_svc → chat_svc`（accessor，单向，无环）。`group_svc` **不直接** `db.Ctx` 摸 `chat_*` 表 —— 一切对单 agent 会话的操作都走 `chat_svc` 的方法。MCP handler 反向调 `group_svc.IngestAgentMessage`（注入接口，避免 httpgateway 反依赖 group_svc 实现）。

### 命名

domain 包统一 `group_*`。如需避免与泛化的 "group" 撞概念，可改 `crew_*`/`squad_*` —— **待确认（见 §14）**。

## 3. 对现有代码的改动（seam）

除新增包外，开下列最小、in-scope 的口子：

### 3.1 服务端 turn 生命周期观察口（`chat_svc`）

`Send` 是异步的（`gogo.Go` 起 goroutine 跑 `runTurn`，立即返回 `{SessionID, AssistantMessageID, Stream}`），事件发往 Wails stream。`group_svc` 需要在**服务端**知道某个成员的 turn **何时结束**（释放调度槽位、判断是否静默 quiesce）。

> **注意 tool-send 架构下 `ObserveTurn` 的职责收窄**：消息内容**不再**从 turn 的最终文本解析 —— 成员发言来自 turn 进行中的 `group_send` tool 调用（§5/§6）。`ObserveTurn` **只管生命周期**：turn 结束/abort/error → 释放该成员 inflight 槽 + 踢调度器 + 判断 quiesce。

```go
// chat_svc 暴露
type TurnResult struct {
    SessionID          int64
    AssistantMessageID int64
    Aborted            bool
    Err                error
}
// 订阅指定 session 的 turn 完成(返回取消函数)
func (s) ObserveTurn(sessionID int64) (<-chan TurnResult, func())
```

**关键落点（已对照代码核对）**：

- **必须在 turn 起点订阅，不在 finalize 处订阅。** `group_svc` 应先 `ObserveTurn(sessionID)` 拿到 channel，**再**调 `Send` —— 否则 turn 跑得快、回执先于订阅到达就丢了。
- **`TurnResult` 必须在所有退出路径都回灌一次，包括早退。** 正常 finalize 是单点（`chat.go` 内 `acc.Finalize()` → 落 blocks → 定 `agentStatus` → emit `StreamDone/Error/Aborted`），但 `Send` 里有**多个早退分支会绕过它**（`chat.go` 内 `failTurn`，已核对 5 个调用点：`selectRunner` 失败、消息带不支持的图片、cwd 解析失败、builtin 历史读取失败、`runner.Run()` 失败，分别在 `chat.go:2219/2228/2234/2258/2295`）。这些路径**也必须**向 observer 推一条 `TurnResult{Err: ...}`，否则成员 turn 一早退，`group_svc` 就永远占着那个 inflight 槽、整条编排链卡住。
- 实现建议：finalize 处 publish 一次（含 aborted/stopErr），`failTurn`（`chat.go:2939`）末尾 publish 一次（含 err）；二者互斥（failTurn 后立即 return 不走 finalize）→ 保证「订阅了就一定收到恰好一条终态」。

> 验证副记：中止 turn 的既有方法叫 **`Stop(ctx, *StopRequest)`**（`chat.go:1356`），不是 `StopChatMessage` —— §7 复用它。

### 3.2 backing session 归属与列表过滤（`chat_*`）

成员的 backing session 是真实的 `chat_sessions` 行（这样白嫖 history / provider-session 连续性 / steering / 工具权限 UI / capability gating）。为了**不污染普通单 agent 会话列表**：

- 迁移给 `chat_sessions` 增列 `group_id INTEGER NOT NULL DEFAULT 0`（原生 SQL，append 到 `migrationList()` 末尾，当前最后一条是 `202605220010`）。
- **`DEFAULT 0` 不会自动把 backing session 从列表里藏掉 —— 必须在每个 list 查询显式加 `group_id = 0`。** 已核对 `chat_repo/session.go`：**正好 9 个**入口要补（5 个 list：`ListByAgent:72` / `ListByAgentPaged:88` / `ListIDsByAgents:100` / `ListAttentionByAgent:127` / `ListByProject:206`；4 个 count：`CountByAgents:142` / `CountByAgent:167` / `CountRunningByAgents:180` / `CountActiveByProject:218`）。`Find:59`（单条）不加。漏一个，群成员的 backing session 就会泄进普通单 agent 会话列表。
  - 收口建议：在 repo 层做一个 `defaultSessionScope`（`.Where("group_id = ?", 0)`），9 个查询统一 `.Scopes(defaultSessionScope)`，避免各写各的、再漏。
- 加索引：`CREATE INDEX ... ON chat_sessions(agent_id, group_id, status, last_message_at)`（现有索引 `idx_chat_sessions_agent_status_last` 在 `202605220006_chat.go`，新索引并列）。
- `chat_svc` 增一个 `EnsureGroupMemberSession(ctx, agentID, projectID, groupID) (sessionID int64, err error)`，幂等创建/返回带 `group_id` 的 backing session；`group_svc` 在 recruit 时调用，随后正常 `Send(sessionID=...)` 跑 turn。

> 这是 `chat_*` domain 自己的字段（"会话可隶属某个群"），耦合可控；`group_svc` 不碰该列。

### 3.3 能力门控 + MCP tool 注入管线（capability / agentruntime / pkg/claudecode / httpgateway / chat_svc 透传）

**目标**：给成员的 backing session 启动时注入一个 `group_send` MCP tool + 群上下文 system prompt，且这套机制**能力化、不 backend-switch**（遵循 agent-backend.md：不在 chat_svc 写 `if type==claudecode`）。

1. **新能力 `CapMCPTools`**（`capability.go` 加常量 `CapMCPTools Capability = "mcp_tools"`）。语义 = **该 runtime 接受 `RunRequest.MCPServers` 并带注入的 MCP tool 启动**（机制能力，群聊是首个消费者；未来其它注入 tool 复用同一能力）。`claudecode.Capabilities().Set[CapMCPTools]=true`；codex/builtin/piagent 不声明。matrix 测试同步（agent-backend.md §0.5 三处同步：runtime.go / capability.go / runtime_test.go）。
2. **`RunRequest` 加字段 `MCPServers []MCPServerSpec`**（`runner.go`，与 `Cwd`/`ForkAnchor`/`Compact` 并列）。claudecode runtime 在该字段非空时映射为 `--mcp-config`；空则忽略（单聊零影响）。**`RunRequest.SystemPrompt` 已存在**（`chat.go:2243` = `strings.Join(a.GetPrompt(),"\n")` → claudecode `--append-system-prompt`，已核对），群上下文拼到它后面，无需新字段。
3. **`pkg/claudecode` 加 `--mcp-config`**：`args.go` 加 `flagMcpConfig` + `spec.mcpConfig`，`options.go` 加 `WithMcpConfig(jsonOrPath string)`（claude CLI 原生兼容 JSON 字符串或文件路径，与现有 `--settings` 同形态，已核对）；`ccBuildClientOpts`（`runtimes/claudecode/session.go:177`）映射 `RunRequest.MCPServers` → mcp-config JSON，并把 `mcp__group__group_send` 加入 `--allowedTools`（自动放行，不弹审批）。
4. **MCP server 复用现有 gateway**：`httpgateway.Gateway.RegisterMCP("/mcp/group", handler)`（`gateway.go:276` 已支持 `/mcp/*` prefix 分发），暴露 tool `group_send(body string, mentions []string)`（claude 侧 = `mcp__group__group_send`）。
   - **身份/scope**：每个成员 backing session 启动时，`group_svc`（经 chat_svc 的 token 签发）拿一个绑定 `(groupID, memberID)` 的短期 gateway token（复用 `TokenRegistry`），塞进该会话 mcp-config 的 header。handler 读 token → 解出成员 → 调注入的 `group_svc.IngestAgentMessage(ctx, memberID, body, mentions)`。gateway 不反依赖 group_svc 实现（注入接口）。
5. **chat_svc 领域无关透传**：`SendRequest` 加两个**通用**可选字段 `MCPServers []MCPServerSpec` + `SystemPromptSuffix string`；`runTurn` 把 `SystemPrompt = join(a.GetPrompt()) + SystemPromptSuffix`、`RunRequest.MCPServers = req.MCPServers`。**chat_svc 不 branch on group** —— group 语义全在 group_svc 填这两个字段；普通单聊留空。
6. **LRU 复用注意**：claudecode 子进程跨 turn LRU 复用，`--mcp-config`/`--append-system-prompt` 在**首轮 spawn** 固化。成员首轮投递必须带齐这两者（group_svc 每次 Send 都带，首轮即生效）；中途名单变化不会刷新 system prompt → 靠**入群系统消息**走 in-band 投递补偿（§7）。

> 门控落点：`group_svc.AddGroupMember`/`EnsureGroupMemberSession` 加成员前，查该 agent 绑定 backend 的 `Capabilities().Has(CapMCPTools)`，false → 报 `code.GroupBackendUnsupported`。前端邀请/选主持人按 cap 过滤 agent（复用现有 `GetBackendCapabilities`，hook 不改，只改消费端）。

## 4. 数据模型

三张新表（迁移用原生 SQL，append 到 `migrationList()` 末尾，**禁改既有迁移**）。

### `groups`

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | PK | |
| `title` | text | 群名 |
| `host_agent_id` | int64 | 主持人(部门负责人) |
| `department_id` | int64 | 可招募名单的来源(0=不限/显式 allowlist) |
| `project_id` | int64 | 默认 cwd(0=free) |
| `run_status` | text | `idle`/`running`/`paused`/`waiting_user`/`error` |
| `round_count` | int | 自上次用户发言以来的 agent turn 计数(**仅 UI 展示, 无上限 / 无防死循环**) |
| `status` | int | `consts.ACTIVE` / 归档 |
| `created_at`/`updated_at` | | |

### `group_members`

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | PK | 群内稳定身份 |
| `group_id` | int64 | |
| `agent_id` | int64 | |
| `backing_session_id` | int64 | 该成员在本群的 `chat_sessions.id` |
| `role` | text | `host`/`member` |
| `status` | text | `active`/`left` |
| `joined_at` | | |

> 显示用的 name/color/icon 从 Agent 实体读取，**不冗余存**（entity 为 source of truth）。

### `group_messages`

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | PK | |
| `group_id` | int64 | |
| `seq` | int | 群内排序 |
| `sender_kind` | text | `user`/`agent`/`system` |
| `sender_member_id` | int64 | agent 发言时=member id;user/system=0 |
| `recipient_member_ids` | text(json) | 收件成员 id 列表 |
| `to_user` | bool | 是否回给用户 |
| `content` | text | 正文(始终存原文;含 `@名字` 内联标记供前端渲染 chip) |
| `source_message_id` | int64 | 派生自的 `chat_messages.id`(可溯源;user=0) |
| `created_at` | | |

**@ 过滤视图天然涌现**：成员 backing session 只会收到 @ 到它的消息（投递时只 Send 给收件成员）→ "agent 只看到 @ 到自己的消息"无需任何额外过滤逻辑。

充血实体方法示例：`Group.CanAdvance()`（仅按 `run_status` 判断是否允许推进，**无轮数上限**）、`Group.NextSeq()`、`GroupMessage.Recipients()/SetRecipients()`、`GroupMember.IsHost()`。

## 5. 编排引擎（并发 fan-out，tool 驱动）

`group_svc` 为每个活跃群跑一个调度器，核心是 **待投递队列（每成员 FIFO）+ 在跑 turn 集合**（被 @ 的成员**全部并发**，无并发 cap，受成员上限 ~8 自然约束）：

1. **投递**：一条消息要发给某 agent 收件人 → 入该成员的 pending FIFO。调度器把"有 pending 且未在跑"的成员**各起一个 turn**（跨成员并发，同成员串行）：对成员 backing session 调 `chat_svc.Send`（正文加 `(来自 X)` 自然抬头 §6，并带 §3.3 的 `MCPServers` + `SystemPromptSuffix`），并先 `ObserveTurn` 订阅其生命周期。
2. **发言**：成员 turn 进行中调 `group_send(body, mentions[])` MCP tool（**可多次**）→ `/mcp/group` handler → `group_svc.IngestAgentMessage(memberID, body, mentions)`：解析 `mentions` 名字 → member id（+ `@用户` / 招募，§6/§7）→ 落一条 `group_message`（收件人 = mentions 全体）→ emit 群事件 → agent 收件人入队 → 踢调度器（**eager 路由**：tool 一调用就路由，不等该成员 turn 结束）。
3. **生命周期**：成员 turn 结束 → `ObserveTurn` fire → 释放该成员 inflight 槽 → 踢调度器填新槽。**该成员的最终助手文本不进群**（只 `group_send` 进群）—— 私有叙述留在它自己的 backing session（可在成员会话 drill-in 视图里看）。
4. **静默(quiesce)**：队列空 + 无在跑 turn → `run_status = waiting_user`。用户随时 post；插话即追加并踢一脚调度器。

**并行 fan-out**：同一条消息 @ 的若干**独立**成员**全部并发跑**（并发数由成员上限 ~8 自然约束，无单独 `max_concurrency` cap），eager 路由 —— 谁先 `group_send` 谁先触发下游，不等慢的。**依赖链**（A `group_send` @B、B 再 @C）天然跨"波次"顺序。round_count 按**每个** turn 计数，仅用于 UI 展示活跃度（无上限，见 §7）。

**写竞态**：成员共用 cwd，多个并发 turn 同写同一目录有风险。MVP **不上**强制 worktree 隔离基础设施，而是在成员 `SystemPromptSuffix` 里**引导**：「若你要修改文件、且可能与其他成员并发，请在自己的 git worktree 里作业」。coding agent 有 shell，自行 `git worktree add`；用不用、何时用由 agent 判断 —— 编排器不管理 worktree，零新增基础设施。

`run_status` 状态机：`idle → running`（有待投递）`→ waiting_user`（静默）/ `paused` / `error`；stop → `idle`。

后台调度用 `gogo.Go`，**不透传请求 ctx 进 goroutine**（用独立 ctx）。

## 6. 寻址协议（路由结构化，展示标记化）

设计原则：**像真实群聊**。一条消息就是自然正文 + 一组收件人;被 @ 到的人都收到**同一条**;**一个回合可发多条**(每次 `group_send` 一条)。

- **路由（权威，结构化）**：
  - **agent**：调 `group_send(body string, mentions []string)`，`mentions` = 成员显示名数组（从系统提示的 roster 得知名字）。编排器 `byName` 解析成 member id 集合 = 收件人。
  - **用户**：发送框打 `@名字`（成员自动补全），前端解析成 `recipientMemberIds` 结构化数组随 `SendGroupMessage` 传后端。
  - 收件人 = mentions/recipientMemberIds 全体，都收到**同一条**。`mentions` 含 "用户"/"你" → `to_user=true`。
- **展示（友好，标记化）**：群 transcript 渲染正文时，把内联 `@名字`（或 `<mention>名字</mention>` 标记）按当前 roster **渲染成高亮、可点击的 chip**；点击跳转到该成员的 backing session 会话视图（§10 的视图 tab）。系统提示引导 agent 在 body 里也用 `@名字` 自然提及（与 `mentions` 一致），未匹配到 roster 的 `@x` 退化为普通文本。**`<mention>`/`@` 解析只服务于渲染，不参与路由。**
- **名称唯一性**：群内成员显示名唯一（招募/改名时校验），`mentions` 按名字解析;解析不到的 name → flag 忽略（或触发招募，见 §7）。
- **兜底**：`group_send` 的 `mentions` 为空 → **回给上一个发消息的人**；原文始终入库不丢。
- **静默(quiesce)**：成员 turn 跑完且全程没调 `group_send`（或只 `@用户`）→ 没有指向任何 agent 成员 → 回合结束，群转 `waiting_user`。

> 标记名 **`<mention>`**（已与用户确认）。MVP 内联 `@名字` 也可作为渲染来源。

## 7. 控制流：招募 / 终止 / 插话

- **招募**：主持人 `group_send` 的 `mentions` 里出现**在部门名单、尚未进群**的 agent 名 → `group_svc` 自动新增成员（先查 `CapMCPTools`，不支持则拒绝并 flag）、`EnsureGroupMemberSession` 起 backing session、post 一条**系统消息**"X 加入"（`sender_kind=system`，投递给全体在群成员做 in-band 名单更新），并把触发的这条消息投递给新成员。成员数上限 ~8。**MVP 仅主持人能触发自动招募**（非主持人 mention 名单外/未进群 agent → 忽略并 flag）；用户也可在 UI 手动加任意**支持 `CapMCPTools` 的** agent。
- **终止（无 max_rounds 防死循环）**：自然终止 = 没人被 @ 的静默（quiesce → `waiting_user`）或回 `@用户`；跑飞了由用户 `停止`。**不设轮数上限**（2026-06-03 据用户决定去掉）—— 互相 @ 理论上可一直跑，靠 stop + 自然静默收敛；`round_count` 仅作 UI 活跃度展示。
- **插话/暂停/停止**：用户 post = live 追加；stop 取消**所有在跑**的成员 turn（对每个在跑 session 调 `chat_svc.Stop(ctx, *StopRequest)`，`chat.go:1356`）+ 清空队列 + `run_status=idle`；pause 停止填新槽位、让在跑的 turn 自然跑完（`CanAdvance()` 在 paused 返回 false）。
- **mid-turn 插话**：若用户在某成员忙时 @ 它，MVP 在群级排队（该成员 pending FIFO）、turn 结束后下一轮投递（不强行 steer；如需即时可后续接 `EnqueueChatMessage`）。

## 8. 工具权限 / 交互事件透传

- **`group_send` 本身自动放行**（`--allowedTools` 含 `mcp__group__group_send`），不弹审批。
- 成员 turn 可能触发**其它** tool 的 `ToolPermissionRequest` / `UserAskRequest`（讨论为主时较少，但会有）。这些事件仍在 backing session 的 stream 上触发：
  - MVP 把它们作为**系统行**冒泡进群 transcript（"成员 X 请求运行 …，待你批准"）。
  - 用户在群 UI 上批准/回答 → 复用现有 `AnswerToolPermission` / `AnswerUserQuestion`（作用于该 backing session），无需新增后端方法。

## 9. Wails 绑定与事件流（`internal/app/group.go`，thin）

方法只做 parse → `group_svc.Xxx()` → return：

- `ListGroups()` / `CreateGroup(req)` / `LoadGroup(id)`（房间 + 成员 + 消息日志；成员含 `backing_session_id`，前端据此**跳转到该成员的完整单聊会话**，复用现有 chat 视图，无需新绑定）
- `SendGroupMessage(req)`（groupId, text, recipientMemberIds[], toUser；用户的收件人前端已解析成结构化数组）
- `AddGroupMember(req)`（仅可加支持 `CapMCPTools` 的 agent）/ `RemoveGroupMember(req)`
- `StopGroup(id)` / `PauseGroup(id)` / `ResumeGroup(id)`
- `RenameGroup(req)` / `ArchiveGroup(id)` / `MarkGroupRead(id)`

> `IngestAgentMessage` **不**是 Wails 绑定 —— 它是 group_svc 暴露给 httpgateway MCP handler 的服务端入口（注入接口）。

实时：群事件流 `group:event:<groupId>`，推送新消息、成员状态、run_status 变化、系统行（加入/权限请求）。前端 `EventsOn` 订阅，写入 zustand store。

## 10. 前端 UI/UX

新面板 `frontend/src/components/agentre/group-chat/`（已用可视化 companion 验证布局）。**复用 app 既有的「会话列表 ｜ 对话 ｜ 右侧信息栏」四区结构**（rail 不变；不照搬单聊的两栏）：

- **左 · 对话列表**：群聊和单 agent 会话**混排在同一列表**（顶部「群聊」分区 + 下面「AGENTS」单聊会话），群条目带 run_status 点。
- **中 · 群对话**：**视图 tab 栏**（对话区顶部，复用现有 `chat-tabs/`）—— 打开的视图作为可切换 tab：群聊本身 + 点成员 `›`/点消息里 mention chip 跳进去的成员会话，点群聊 tab 即返回；房间头（群名 + run_status pill 运行中/等待你/已暂停 + 中性回合数「已 N 轮」**无上限** + 暂停/停止）；transcript（按发送者着色的**头像 + 名字 + 流式正文**，正文里 `@名字` 渲染成**高亮可点 mention chip**（点击跳转该成员会话），定向消息下灰字"仅 X 收到"提示过滤视图；系统行居中；工具权限以审批卡内联出现）；发送框（自由文本 + 内联 `@` 自动补全，收件人前端解析成结构化 `recipientMemberIds`）。
- **右 · 群信息**：分 **成员 / 设置** 两个 tab（成员/设置小 tab 用 Button+state；仓库无 shadcn `Tabs`）。成员 tab = 主持人 + 成员（实时状态：思考中/待批准/空闲）+ "邀请成员"（**仅列支持 `CapMCPTools` 的 agent**）；设置 tab = 工作目录 / 归档群。**每个成员行可点（尾部 `›`）→ 跳转到它的完整 backing session 会话**（复用单聊视图，看全程工具调用/思考/改动；该会话以新视图 tab 打开），目标视图顶部「← 返回群聊」。

约束：所有静态文案走 `react-i18next` 的 `t(...)` + 同步 `zh-CN`/`en` common.json；表单控件用 shadcn `@/components/ui/*`，禁原生 `<select>`；agent/用户/消息**内容不翻译**。复用现有 `CanonicalToolRouter` 渲染成员工具卡。mention chip 渲染 + 点击跳转是新的小组件（带 Vitest）。

## 11. 测试策略（严格 TDD：Red → Green → Refactor）

- **能力 / runtime 单测**：`capability` matrix 测试同步 `CapMCPTools`；claudecode `Capabilities()` 声明 + RunRequest.MCPServers → mcp-config 映射的单测（不 spawn 真子进程，断言 args/options，仿 `ccBuildClientOpts` 现有测试）；`pkg/claudecode` args 加 `--mcp-config` 的单测。
- **MCP handler 单测**：`/mcp/group` handler 收到 `group_send` 调用 + 合法 token → 调 `IngestAgentMessage(memberID, body, mentions)`；非法/过期 token → 拒绝。
- **Repository 单测**：`group_repo` / member / message 一律 `testutils.Database(t)` + sqlmock，禁真库。
- **Service 单测**：mockgen 生成 `mock_group_repo`，并 mock `chat_svc` 的 `EnsureGroupMemberSession` / `Send` / `ObserveTurn` / `Stop` seam（注入窄接口 `ChatGateway`）。BDD/goconvey 覆盖：
  - happy path：用户 → 主持人 turn 内 `group_send` @成员 → 成员 turn 内 `group_send` @用户 → quiesce。
  - 路由解析：多 mentions 都收到同一条 / 名字唯一解析成 member id / mentions 空兜底到上一个发送者 / 只 @用户 → quiesce。
  - 并发 fan-out：一条消息 @ 两个成员 → 两 turn 并发发起；成员 A `group_send` @B → B 二次投递。
  - 招募流：主持人 `group_send` mention 名单内未进群 agent（且支持 CapMCPTools）→ 新成员 backing session 建立 + 系统消息;非主持人触发 → 忽略并 flag;不支持 CapMCPTools → 拒绝并 flag。
  - 门控：加不支持 `CapMCPTools` 的 agent → `code.GroupBackendUnsupported`。
  - 生命周期：成员 turn 全程没 `group_send` → 释放槽 + quiesce；turn error/abort → 释放槽 + 不路由 + run_status。
- **迁移测试**：新表 + `chat_sessions.group_id` 列的 `*_test.go`（迁移自身可起真库，属白名单例外）。
- **前端 Vitest**：群面板渲染、`@` 自动补全、**mention chip 渲染 + 点击跳转**、定向 chip 与"仅 X 收到"渲染、stream 事件 → store。
- 跑法：`make test-backend`（后端 race，排除 frontend）+ `cd frontend && pnpm test -- <file>`。

## 12. 错误码与 i18n

- 在 `internal/pkg/code/code.go` 为 group domain 分配新错误码段 **19000~19999**（已核对：18000=Project / 18100=ProjectLocation / 20300+=Server，无 19000 段，空闲）。含 `GroupNotFound` / `GroupTitleRequired` / `GroupHostRequired` / `GroupMemberNotFound` / `GroupMemberExists` / `GroupMemberLimit` / `GroupNotRecruitable` / **`GroupBackendUnsupported`**（agent 后端不支持 `CapMCPTools`）。文案补 `zh_cn.go`/`en.go`，用 `i18n.NewError(ctx, code.Xxx)`。
- 关键流程打日志：`logger.Ctx(ctx)`，message 用 `group_svc.Method:` 前缀小写，动态值走 `zap.Xxx`。

## 13. MVP 范围 / 非目标

**IN（MVP）**：房间 + 主持人；**能力门控 `CapMCPTools`（仅 claudecode 可入群）**；主持人自动招募（mention 名单内未进群 agent）+ 用户手动加成员；**`group_send` MCP tool 发言（结构化 mentions，一回合可多条）**；`<mention>`/`@` 标记的**前端高亮 chip + 点击跳转**；**并行 fan-out 自动推进（eager 路由，无并发 cap）**；用户插话/暂停/停止（**无 max_rounds 防死循环**）；成员行/mention chip 跳转到完整 backing session；群上下文 system prompt 注入（角色 + roster + tool 用法 + worktree 引导）；持久化（3 表 + chat_sessions.group_id）；群面板 UI（左对话列表混排 / 右群信息）；复用工具权限透传。

**OUT（后续）**：**codex/builtin 群成员支持**（需各自实现 `CapMCPTools` + MCP/工具注入，本期只 claudecode）;**强制 worktree/并发写隔离基础设施**（MVP 仅系统提示引导 agent 自建，编排器不管理）;DAG/工作流编辑器;跨群/嵌套群;高级环检测;remote-daemon 成员(经 `Send` 可传递跑通,但本期不专门测;MCP token/gateway 在远端的可达性另议)。

## 14. 已定默认值 & 待评审确认项

下列为我替你拍的默认，评审时可推翻：

1. **执行并行 fan-out**（eager 路由，**无 `max_concurrency` cap**，并发数受成员上限 ~8 自然约束）—— 依赖链天然顺序；共享 cwd + 引导 agent 自选 git worktree，不上强制隔离基础设施。
2. **发消息 = `group_send(body, mentions[])` MCP tool**（结构化路由）；成员私有叙述**不进群**，只 tool 调用进群；一个 turn 内可多次 `group_send`。**机制经 MCP 实现，MVP 仅 claudecode**（2026-06-03 据用户决定）。
3. **能力门控 `CapMCPTools`**（机制名，2026-06-03 据用户决定采用，非特性名 `CapGroupChat`——便于未来其它注入 tool 复用）：只有声明该能力的 backend 可入群；语义 = runtime 接受 `RunRequest.MCPServers`。group_svc + 前端按 cap 过滤。
4. **寻址路由结构化、展示标记化**：路由用 `mentions[]`/`recipientMemberIds`；`<mention>`/`@名字` 标记**仅用于前端高亮 chip + 点击跳转**（2026-06-03 据用户决定保留解析用于展示）。收件人 = mentions 全体，都收到同一条。
5. **招募 = 主持人 `group_send` mention 名单内未进群且支持 CapMCPTools 的 agent**（仅主持人可触发）；可招募名单 = 部门成员；成员上限 ~8。
6. **空 mentions 兜底 = 回上一个发送者**；turn 全程无 `group_send` / 只 @用户 = quiesce 转 `waiting_user`。
7. **无防死循环 / 无 `max_rounds`**（靠 `停止` + 自然静默收敛；`round_count` 仅 UI 展示）。**成员消息流 = 点成员行/mention chip 跳转到完整 backing session**（复用单聊视图 + 「← 返回群聊」）。
8. **工具权限/提问**冒泡为系统行，复用现有 handler 应答；`group_send` 自动放行。
9. **domain 包名 `group_*`**（vs `crew_*`/`squad_*`）—— 待确认；**能力名 `CapMCPTools`**（机制名，已定）。
10. **seam 已对照真实代码核对（2026-06-03）**：`ObserveTurn`（turn 起点订阅 + 覆盖 `failTurn` 5 处早退 + 恰好一条终态，**职责收窄为生命周期**）；`group_id` 9 个查询过滤 + 索引；`Stop` 非 `StopChatMessage`；`RunRequest.SystemPrompt` 已存在通向 `--append-system-prompt`；`--mcp-config` **不存在需新增**；gateway `RegisterMCP("/mcp/*")` 已支持；`TokenRegistry` 可做 per-session scope；`capability.go` cap 列表 + claudecode `Capabilities()` 为能力声明点。

## 15. 关键不变量自检

- 绑定层只 parse→svc→return，业务全在 `group_svc`。
- `group_svc` 只依赖 repository 接口 + `chat_svc` 窄接口 accessor；MCP handler 经注入接口反调 `IngestAgentMessage`，httpgateway 不反依赖 group_svc 实现。
- **chat_svc 不 branch on group**：只透传 `SendRequest.MCPServers`/`SystemPromptSuffix` 两个通用字段。
- 能力门控经 `CapMCPTools`，**不在 chat_svc/group_svc 写 `if type==claudecode`**（OCP）。
- 迁移 append、禁改既有；DDL 原生 SQL。
- repo 单测 sqlmock、service 单测 mockgen 注入、不接真库；claudecode/MCP seam 有针对性单测。
- 前端 i18n + shadcn；动态内容不翻译；mention chip 渲染/跳转有测试。
- diff 只含本特性 producer + 测试，无 drive-by。

## 16. 群聊上下文 / 提示词的实现（细化）

成员 = 一条隐藏的 `chat_sessions`（backing session），它的"上下文"由三部分构成，**全部复用现有单 agent 机制**，群聊只在边界注入：

1. **角色/群上下文 = system prompt 后缀**。每次投递 `chat_svc.Send` 时带 `SystemPromptSuffix`（拼到 agent 自身 persona `a.GetPrompt()` 之后 → claudecode `--append-system-prompt`）。内容由 `group_svc.buildGroupSystemPrompt(g, members, me)` 动态生成：
   - 群名 + 本成员角色（主持人/成员）；
   - **当前成员名单**（名字 + 角色）—— mention 名字的来源；
   - 协议说明：「你只会收到 @ 到你的消息；发言调 `group_send(body, mentions[])`，mentions 填成员名（@用户=回人类）；一回合可多次、可对不同人发不同内容；**不调 group_send 的内容不进群**」；
   - 主持人附加：「mention 部门内未进群的同事即可招募」；
   - worktree 引导。
2. **会话历史 = backing session 自身的 chat_messages**。每条投递以 `(来自 X)\n正文` 作为一个 **user turn** 落进该会话；成员的 `group_send` tool 调用 + 回复落为 **assistant turn**。于是成员只持有「@到自己的消息 + 自己的发言」这一**局部视图**——"只看到 @ 消息"天然成立，无需额外过滤。**成员看不到群全量 transcript**（那是 group_messages，属群视图）。
3. **工具 = 注入的 `group_send` MCP server**（per-member token 标识身份）。

**关键实现注意（评审补充）：**

- **system prompt 的"刷新"问题（LRU 复用）**：claudecode 子进程跨 turn LRU 复用，`--append-system-prompt` 仅首轮 spawn 生效。因此：① 首轮投递必带 suffix（group_svc 每次都带，首轮即固化）；② **roster 中途变化**（招募新成员）不会刷新已在跑成员的 system prompt —— 用 **in-band 系统消息**「X 加入」投递给在群成员做名单更新（成员从消息正文得知新名字即可 @）；③ 如需强一致，可在 roster 变化时**主动 evict 该成员的 LRU 会话**，下一轮重新 spawn 带新 prompt（MVP 不做，留作增强）。
- **上下文增长**：长群聊里成员 backing session 历史持续增长 → 复用 claudecode 原生 compact（OUT of MVP 自动化，但机制现成）。
- **delivery 文本**：MVP 一条投递 = 一个 turn（每成员 FIFO）；多条 pending 顺序投递（可后续批量合并为一 turn 优化）。

## 17. 评审：并发 / 安全 / 边界（2026-06-03，需在实现中落实）

- **并发写竞态（必修）**：`IngestAgentMessage` 由 MCP handler goroutine 调用，可能与调度 goroutine、同一成员 turn 的多次 `group_send` 并发。`NextSeq → Create`（seq 分配）与 `round_count++ → Update` 是 read-modify-write，并发下会重号/丢更新。**每个 group 需一把锁**串行化「解析→分配 seq→落库→入队」（可挂在 `scheduler.mu` 或单独 `ingestMu`）。
- **自我 mention 过滤（必修）**：`resolveMentionNames` 要剔除发送者自身，避免成员把消息投给自己 → 自触发 turn 自循环。
- **MCP token 生命周期（必修）**：per-member token 用加密随机；成员离群 / 群 stop / 归档时**吊销**对应 token；可加 TTL。token 仅 localhost gateway 可见，scope 到 (group, member)。
- **MCP transport（✅ 已 spike 验证通过，2026-06-03 / claude-code 2.1.161）**：`--mcp-config` 的 `type:"http"` + 自定义 header **实测可用**（`claude mcp get` 显示 `✓ Connected`，自定义 header 透传到达）。实测握手：`POST initialize`(client 报 `protocolVersion 2025-11-25`)→ 纯 `application/json` 响应即可（**无需 SSE**）；`POST notifications/initialized`→202；`POST tools/list`；`GET /mcp/`(SSE 流)→回 405 claude 容忍。身份用 `Authorization: Bearer <per-member token>` header。工具名 `mcp__group__group_send`。**stdio 回退方案不再需要**（保留为备选）。
- **用户消息无 @ 的兜底**：用户在 composer 不选收件人直接发 → **默认投给主持人**（而非 quiesce/上一个发送者）；agent 侧空 mentions 才回上一个发送者。兜底规则按 sender_kind 区分。
- **招募名单 dept=0**：`department_id=0`（不限）时**关闭自动招募**（无名单无从校验），仅允许用户在 UI 手动加成员；dept>0 才支持主持人 mention 招募。
- **名称唯一性**：招募 by-name 依赖部门内 + 群内显示名唯一；招募/加入时校验，撞名拒绝并 flag。
- **流式 UX**：群消息**整条原子出现**（`group_send` 一次给全文，不 token 流式）；成员"思考中"在其 backing session stream（群里仅显示状态 pill，正文进群要等 `group_send`）。这是 tool-send 的取舍——私有过程不进群。需在 UI 文案/预期上明确。
- **stop 与在途 tool 调用竞态**：stop 后若某成员的 `group_send` HTTP 调用仍在途，`IngestAgentMessage` 可能在 stop 后落一条消息；`kick` 的 `CanAdvance()`（idle 后 false）会阻止再起新 turn，但该消息仍入库。可接受；必要时 ingest 入口检查 run_status。
- **source_message_id 语义**：tool-send 下群消息不对应单条 chat_message（是 turn 内一次 tool 调用）；存 0 或存该 turn 的 assistant message id 作弱溯源。
