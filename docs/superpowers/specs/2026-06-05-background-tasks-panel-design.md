# 后台任务面板(会话头部胶囊 + 弹层)设计

**日期**: 2026-06-05
**状态**: 设计完成,待 review
**作者**: Claude(结对 with 王一之)
**相关**: [[2026-06-04-claudecode-background-task-autonomous-turn-design]](自主续轮捕获,本案复用其会话级旁路 + 完成信号)

## 背景

用户在会话里跑 `run_in_background` 的 bash 任务(`sleep 20`、`npm run dev &` 之类)后,**无法在 UI 看到「有多少个 / 什么任务在后台运行」**。当前仅有的零散信号:启动那条消息的 tool_result 文本(含 task ID + output 路径)、内联的 `subagent_state` running 卡片(分散在 transcript 里、按工具调用)、以及完成后冒出的自主续轮 assistant 消息。没有任何**聚合的、可一眼看到的「正在进行的任务」视图**。

### 已确认的事实(真实 CLI 2.1.162 capture + 代码 sweep)

- CLI 把后台任务与 subagent 都建模成同一套 **task** 概念,经同一组帧下发:`system{subtype:"task_started"|"task_updated"|"task_progress"|"task_notification"}`。区分维度是 `task_type`:
  - `local_bash` —— `run_in_background` 的 bash 任务(`task_notification` 带 `output_file`,无 `subagent_type`)。
  - `local_agent` —— subagent(Task 工具,带 `subagent_type` ∈ general-purpose / Explore / Plan / …,无 `output_file`)。
- subagent(`local_agent`)**在父轮内同步跑**(父 agent 等它,`task_notification` 在 active turn 内到达,带 `subagent_type`),没有 `run_in_background` 的「自主续轮」语义。
- 两者今天**都已生成 `subagent_state` 块**(`internal/service/chat_svc/blocks/subagent_state.go`,`status:"running"`,`parent_tool_call_id` = 触发它的工具 tool_use_id),已落库 + 流式给前端。sess-429 实测有此块。
- **缺口/缺陷**:① `subagent_state` 块没有 `kind` / `description` 字段,前端无法区分 bash vs subagent、无法显示任务名;② **后台 bash 的块永远停在 `running`** —— 它的完成 `task_notification` 被自主续轮当作起始标记消费掉、不再下发(见相关 spec),`task_updated{patch.status:"completed"}` 又在空闲时被 `isNonTurnFrame` 丢弃,所以没有「完成」信号回流到块。

## 目标 / 非目标

**目标**
- 会话头部一个**胶囊**(chip)显示当前**运行中任务数**(常显、可一眼看到),点开是一个**弹层**(popover)列出本会话的后台任务。
- 每个任务条目展示:**类型图标**(bash=terminal / subagent=bot)、**描述**、**耗时**、**状态**(运行中 / 已完成 / 失败)、**完成摘要**(退出码)。
- 统一覆盖 `local_bash` + `local_agent` 两类任务。
- 顺带修掉「后台 bash 块永远 running」的次生缺陷(本案的完成信号天然带来)。

**非目标**
- ❌ 查看 output(不读 `tasks/<id>.output`、不加新 Wails binding)。
- ❌ 停止 / kill 运行中的后台任务。
- ❌ 新 DB 表 / schema 迁移(复用既有 `subagent_state` 块,纯 JSON 字段增补)。
- ❌ codex / builtin backend(无等价 task 协议,不声明、不渲染)。
- ❌ 全局跨会话托盘(本案是**会话级**;全局视图列为显式 follow-up)。

## 已确认的设计决策

1. **放置 = 会话头部胶囊 + 弹层**(用户在 pencil 效果图 A/B/C 中选 A)。胶囊在 Chat `Header`(`quFOg`)右侧,弹层向下展开。pencil 稿:`agentry.pen` 帧「Agent Chat — 后台任务 · A 头部胶囊弹层」+ 复用组件 `TaskRow`(`PQd1v`)。
2. **交互 = 只读**(用户选「最轻」)。不看 output、不控制。bash 输出仍可在对话里(工具结果 / 自主轮)滚。
3. **数据来源 = 前端从 `subagent_state` 块 derive**(轻方案),而非另起一套并行 task-tracking 流。理由:这些块今天已流式给前端,只差 `kind`/`description` 与完成翻转两处;复用既有块基础设施 + 既有自主续轮机器,新增面最小。
4. **完成信号搭既有自主续轮事件的车**:后台 bash 完成 ⇔ 自主续轮触发(因果同源)。把完成任务的 `tool_use_id`/`status`/`summary` 挂到 `AutoTurn`,经既有会话级旁路 `chat:autonomous:<sessionID>`(`StreamAutonomousStarted`)带给前端 → 翻转该任务为完成。active-turn 内完成的任务走既有 `SubagentDone` 路径,本就会翻转。
5. **live / ephemeral,不持久化聚合态**。后台任务活在 CLI 子进程里;agentre 重启 / 会话 evict → 子进程死 → 后台任务也死,故重启后无「运行中」任务。面板纯反映当前子进程的 live 状态,耗时前端本地 tick。

> **被否的替代方案(记录用)**:独立 `BackgroundTaskSource` 前向子接口 + `CapBackgroundTasks` 能力 + chat_svc watcher + remote wire,完整镜像自主续轮设计。更解耦(不依赖 `subagent_state`)、天生 remote-ready,但对一个只读胶囊是大量 plumbing。本案范围内不值得;若将来要全局/远端面板再升级到它。

## 架构总览

```
turn1: Bash(run_in_background:true)
  → CLI system{task_started, task_type:"local_bash", description, tool_use_id}
  → SubagentMeta{TaskType, TaskDescription, …} (pkg/claudecode)
  → SubagentStarted{Info{Kind:"local_bash", Description, …}} (agentruntime translator)
  → subagent_state 块 {kind:local_bash, description, status:running} 落库 + 流式
  → 前端 deriveBackgroundTasks → 头部胶囊「1 运行中」+ 弹层条目
…后台任务完成…
  → CLI system{task_notification, status:completed, summary, tool_use_id} (空闲到达)
  → pkg/claudecode 判后台型 → 起自主续轮,**同时把 {tool_use_id,status,summary} 记到 AutoTurn.CompletedTask**
  → chat_svc driveAutonomousTurn:落自主 assistant 轮 **+** 在 StreamAutonomousStarted payload 带 CompletedTask
  → 前端:既插入自主轮消息,**又**把该 tool_use_id 的任务标记 completed(灰 + 退出码)

subagent(local_agent):task_started→running(turn 内);SubagentDone→completed(既有路径,无需新增)
```

## 组件 ①:`pkg/claudecode` —— 补 `task_type` + 透传完成信息

- `stream.go` `rawFrame`:补 `task_type`(三类 task 帧共通)字段解析;确认 `task_notification` 的 `tool_use_id` / `summary` / `status` 已可取(`output_file` 已有)。
- `event.go` `SubagentMeta`:加 `TaskType string`(← `task_started.task_type`,值 `local_bash` / `local_agent`)。`parseSystemTask`(`session.go`)填充。
- `autoturn.go` `AutoTurn`:加 `CompletedTask *CompletedBackgroundTask`(`{ToolUseID, TaskID, Status, Summary}`)。`currentTurn` 在 `isBackgroundTaskNotification` 命中、新建自主轮时,从该 `rawFrame` 抽出这些字段塞进 `AutoTurn`。
- **不改**自主续轮的既有路由/并发约束(本案只在「起自主轮」处顺手记录完成任务,不动 readLoop 时序)。

## 组件 ②:`agentruntime`(claudecode runtime + translator)

- `SubagentInfo`:加 `Kind string`(← `SubagentMeta.TaskType`)。`subagentInfoFromMeta` 透传。`event_wire.go` 编解码补字段(向后兼容:旧帧无 `kind` → 空)。
- `AutonomousTurn`:加 `CompletedTask`(镜像 `claudecode.AutoTurn.CompletedTask`),claudecode runtime 的 `AutonomousTurns` 桥接时透传。
- 无新能力 / 新前向接口(复用既有 `AutonomousTurnSource`)。

## 组件 ③:`chat_svc`

- `subagent_state` 块(`blocks/subagent_state.go`):加 `Kind string`(`local_bash`/`local_agent`)+ `Description string`(JSON 字段,`omitempty`,无迁移)。写块处(handlers 里处理 `SubagentStarted`/`Progress`/`Done` 的地方)填充。
- `driveAutonomousTurn`(`autonomous_turn.go`):当 `at.CompletedTask != nil` 时,在 emit 的 `StreamAutonomousStarted` payload 里带上 `{tool_use_id, status, summary}`(新增可选字段,`types.go` / `emitter.go` 对应结构补字段),供前端**即时**翻转。**串行/并发约束不变**(仍不持 chat 会话锁 drain)。
- **持久化完成态(核心,非选做)**:仅靠上面的内存事件,`reloadSession`(StreamDone 后频繁触发)会从 DB 重新 derive —— 若 bash 块仍 `running`,面板会**错误地重新显示运行中**。故完成态必须落库。两种落法,计划期二选一钉死:
  - **(a) 翻转原块(推荐)**:把 `parent_tool_call_id == tool_use_id` 的 `subagent_state` 块 `running→completed` 落库。单一真相源,**顺带修掉内联卡片永远 running 的次生缺陷**。代价:bash 块在**更早的消息**里(turn1 启动、后续自主轮才完成),需要 chat_svc 支持**跨消息更新某条历史消息的块**(需确认是否已有机制,无则新增一个定向 update)。
  - **(b) 追加引用(append-only)**:在自主轮 assistant 消息上记 `triggered_by_completed_tool_use_id`,前端 derive 交叉引用标记完成。无需跨消息更新,但内联卡片仍停 running、derive 略复杂。
  落库失败仅 log,不阻断(对齐既有失败路径)。

## 组件 ④:前端

- `deriveBackgroundTasks(messages, liveBlocks)` 纯函数(镜像既有 `task-progress/derive.ts` 的 `deriveTaskProgress`):扫会话消息 + 当前 live 块里的 `subagent_state`,产出 `BackgroundTask[]`(`{toolUseId, kind, description, status, startedAt}`)。`startedAt` 取所属消息/块时间,**耗时前端本地 tick**(setInterval)。
- 完成翻转:ChatPanel 既有的 `chat:autonomous:<sessionID>` 常驻订阅里,收到带 `CompletedTask` 的 `StreamAutonomousStarted` → 把对应 toolUseId 的任务标记 completed(本地态;若组件④的选做落库了,reload 也一致)。
- 组件:`BackgroundTasksChip`(头部,显示运行中计数 + 绿点;0 运行中时:无运行任务则隐藏胶囊,或仅在有「最近完成」时显示一个中性态——实现期定)+ `BackgroundTasksPopover`(列表:运行中在上、最近完成在下,可「清理已完成」)。挂在 Chat `Header`。
- i18n:`chatPanel.backgroundTasks.{chip,title,running,completed,failed,empty,clearDone,…}`,zh-CN + en 双份;过 `i18next/no-literal-string` 与 `i18n.test.ts`。
- 复用 shadcn `@/components/ui/*`(Popover 等),不加原生控件。

## 数据 / 状态语义

- **状态三态**:`running`(task_started 后、未完成)、`completed`(`task_notification.status=="completed"` 或退出码 0)、`failed`(`status=="failed"` 或退出码≠0)。摘要文本来自 `task_notification.summary`(形如 `Background command "…" completed (exit code 0)`)。
- **胶囊计数** = `running` 计数(bash + subagent 合计)。
- **耗时**:`running` 实时 tick(now − startedAt);`completed/failed` 定格(用 `task_updated.patch.end_time` 或完成时刻 − startedAt)。

## 边界 / 错误处理

- 后台任务完成发生在 **active user turn 内**(非空闲)→ `task_notification` 路由进 active turn → 既有 `SubagentDone` 翻转,**不**走自主轮路径(无 `CompletedTask`)。两条完成路径并存、互斥(按 active==nil 与否)。
- 一次空闲多任务同时完成 → CLI 串行 emit 多个 `task_notification`、多个自主轮,各带各自 `CompletedTask`,逐一翻转(rare;spec 记录,实现按 FIFO 自然处理)。
- 自主续轮内部出错 / 落库失败 → 沿用既有路径(回 idle、log),**不影响**任务翻转信息已带出。
- 子进程中途死亡 → 会话 evict,前端面板随会话态清空(live 语义)。

## 测试策略(TDD,先 Red)

| 测试 | 位置 | 验证 |
|---|---|---|
| `task_type` 解析 | `pkg/claudecode/session_test.go` | `task_started{task_type:"local_bash"/"local_agent"}` → `SubagentMeta.TaskType` 正确;`task_notification` → `AutoTurn.CompletedTask{ToolUseID,Status,Summary}` 填充。 |
| 翻译透传 | `internal/pkg/agentruntime/runtimes/claudecode/*_test.go` | `SubagentMeta.TaskType` → `SubagentInfo.Kind`;`AutoTurn.CompletedTask` → `AutonomousTurn.CompletedTask`;wire round-trip(`kind` 向后兼容)。 |
| 块字段 + 翻转 | `chat_svc` 单测(mock runtime) | `SubagentStarted{Kind,Description}` 落 `subagent_state{kind,description,status:running}`;`driveAutonomousTurn` 带 `CompletedTask` → emit 的 `StreamAutonomousStarted` 携带完成信息(+ 选做:对应块翻 completed)。 |
| derive 纯函数 | `frontend` Vitest | `deriveBackgroundTasks`:running 计数、bash vs subagent 分类、completed 翻转、隐藏/清理逻辑。 |
| 组件 | `frontend` Vitest | `BackgroundTasksChip`/`BackgroundTasksPopover` 渲染三态 + 计数 + 空态;i18n key 覆盖。 |

repo 单测一律 `testutils.Database(t)` + sqlmock;service 单测 mockgen 注入。

## 回滚 / 兼容

agentre 未发布,无需兼容层 / 迁移回填。`subagent_state` 新增 `kind`/`description` 为 `omitempty` JSON 字段,旧块(无该字段)前端按未知/缺省渲染,不破坏。新增能力面为零(复用 `AutonomousTurnSource`),不声明能力的 backend 不受影响。**无 DB schema 改动**。

## 实现期待定细节(计划/TDD 钉死,非架构阻塞)

1. 0 运行中时胶囊的确切行为(完全隐藏 vs 显示「最近完成 N」中性态 + 自动清理时机)。
2. `subagent_state` 块的精确写入点(`SubagentStarted` handler)与 `Kind`/`Description` 的来源帧字段(两份真实 capture:`local_bash` 与 `local_agent` 各一)。
3. 选做的内联卡片翻转是否纳入本次(推荐纳入,因完成信息已在手)。
4. remote(agentred)是否本次顺带(默认**不**;`CompletedTask` 已在 `AutonomousTurn` 上,remote 转发自主轮时大概率免费带出,留作 follow-up 验证)。
