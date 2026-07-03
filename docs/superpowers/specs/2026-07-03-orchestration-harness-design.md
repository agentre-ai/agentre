# 编排 Harness 设计 — 完成回报分层 / Leader 可观测控制 / 软检测 / 可参与 agent 生效

日期：2026-07-03 · 仓库：`agentre/`（Wails 桌面端）· 分支：`develop/wyz`

## 目标

给现有编排（`orch_svc`）加一层 **soft harness**：更好的完成/报错信号、Leader 的按需读取与轮询/纠偏工具、
后台软检测告警，以及让「可参与 agent」选择**真正生效**。核心动机有两条：

1. **削掉 Leader 上下文洪水**：当前子任务完成会把**整段** `FinalAssistantText`（或 `finish` 小结）
   强制注入 Leader 会话续轮（`complete.go:74`，且用 `【…】` 而非 XML）。Leader 无任何轮询/拉取工具，
   两次回报之间**全盲**。
2. **补齐死掉的约束**：新建编排弹窗挑的「可参与团队」（`allowedAgentIds`）从 UI 一路传到 service DTO，
   却在 `CreateRun` 被**整个丢弃**（`create.go:29-76` 从不读 `req.AllowedAgentIDs`；Run 无此列；
   `agent_list` 返回全部；`dispatch`/`ask` 不校验）——挑的团队**零效果**。

**不做硬资源上限**：保留现状「无次数/时长/成本上限、agent 自治」哲学（`turn.go:13-16` 的 guidance）。
本 harness 全是更强信号 + 拉取/轮询/纠偏 + 软检测，没有一处强制打断或封顶。「可参与 agent」是
**团队编成（用户意图）**，不是资源封顶，因此对它做硬校验与上述哲学不冲突。

## 范围决策（已确认）

- **A 触发规则**：显式 `finish`/`report`（有小结）→ 内联 XML；其余完成/报错 → 轻量 XML ping，
  Leader 用 `read(task_id)` 按需拉全文。「agent 自决是否主动告知」。
- **A 覆盖报错**：不止完成，报错等异常也走同一分层（ping + 可拉详情）。
- **B**：新增 `status()` / `read(task_id)` / `cancel(task_id)` 三工具。`read` 合并 read_result（settled）
  + peek（running）为一把。`cancel` = 软取消 + 尽力硬打断 + 级联子孙。
- **D**：只告警、不改状态（不强制打断）；**双通知**（注入 Leader + toast 用户）；一次卡住只报一次（去重）。
- **E**：可参与 agent **落地并硬生效**（持久化 + `agent_list` 过滤 + `dispatch`/`ask` 拒集外）。
- **统一 XML 信封**：所有注入 Leader 的编排内容 XML 包裹 + HTML 转义（复用 `ask.go:69` 现做法）。
- **spec 组织**：A/B/D/E 同一份 spec 一起设计；**落代码时拆独立 commit**（E 是死字段修复，A/B/D 是新特性，
  不混 diff，不破 `git bisect`）。

## 现状（关键事实，均已核对）

| 现象 | 位置 | 说明 |
|---|---|---|
| 完成强制注入整段正文 | `complete.go:57-79`（msg 格式 `:74`） | `【子任务 #N 完成 · agent#M】\n<full>`，非 XML，无分层 |
| 完成态取值 | `complete.go:34-37` | 优先 `finish` 写的 `Result`；否则退回 `FinalAssistantText` |
| `finish` 写 `Result` | `finish.go:19` | `tk.Result = summary`——**与「捕获正文」共用一列，语义被压扁** |
| 报错也上抛父 | `complete.go:82-89` `markTaskError` | 已有：技术崩溃走同一条回报路（error 传播已闭环） |
| Leader 无轮询/拉取工具 | `mcp.go:289-357` | 工具仅 `agent_list/dispatch/ask/reply/send/finish` |
| `agent_list` 返回全部 | `mcp.go:275-287` | `agents.List()` 零过滤 |
| `dispatch`/`ask` 不校验目标 | `dispatch.go:23-29` / `ask.go` | `FindByName` 接受任意 agent |
| `CreateRun` 丢弃 allowedAgentIDs | `create.go:29-76`（字段 `:19`） | Run 实体/迁移（`run.go` / `202606240001_orchestration.go`）**无此列** |
| 调度器纯事件驱动、无心跳 | `scheduler.go` | 无 ticker；`dispatch` 无超时（仅 `ask` 有 4min） |
| 取消是「软」 | `control.go:47-80` `StopRun` | 标 canceled + 停新槽；in-flight「跑完自行退出」，**不中断在跑 turn** |
| Leader 干预原语 | `control.go:83-85` `Speak` | = `SendAndForget`，任意会话续轮 |
| 注入原语（setter） | `orch_adapter.go:54-79` | `SendAndForget`（busy→`ErrSessionBusy`）/ `Enqueue` |
| 每轮注入接线 | `turn_mcp.go` / `turn_extras.go` / `turn.go:20-52` | `BuildTurnMCP`（门控 `ToolEnabled(orchestrate)`）+ `BuildTurnExtras`（guidance/flow） |

---

## 设计 A — 完成/报错回报分层 + 按需读取

### A.1 数据模型变更（唯一 schema 变更之一）

`orch_tasks` 新增一列，把「显式小结」与「捕获正文」拆开（今天两者都挤在 `Result`）：

| 列 | 含义 | 谁写 |
|---|---|---|
| `Result`（已有） | 子任务**完整最终输出**（settle 时始终落库，`read` 永远有货） | watcher 落 `FinalAssistantText` |
| `Summary`（**新**，`summary text NOT NULL DEFAULT ''`） | **显式终态小结**（仅 `finish` 写） | `finish` |

- `orch_entity.Task` 加字段 `Summary string`（`gorm:"column:summary;..."`）。
- 迁移：尾部追加 `202607030001_orch_task_summary.go`（native SQL `ALTER TABLE orch_tasks ADD COLUMN summary ...`）。
  实现时**核对当日最新迁移 ID** 再排号（`develop/wyz` 可能有并发迁移）。

### A.2 触发规则（`complete.go` 分三路）

watcher `idle` 分支改为：**始终**把 `FinalAssistantText` 落 `Result`（供 `read`），再按是否有显式小结分流。
注意 `finish` 已同步写过 `Summary`/`Status=done`，watcher 必须**以 `fresh` 为准合并**（`fresh, _ := tasks.Find(id)`），
只更新 `Result`，**不覆盖** `Summary`/`Status`：

- `fresh.Summary != ""`（agent 主动 `finish`）→ 内联
  `<task_report task_id="N" agent="X" call_seq="k" final="true">…summary(escaped)…</task_report>`
- 完成但无小结 → 轻量 ping
  `<task_done task_id="N" agent="X" call_seq="k">首行摘要…（read(task_id="N") 看全文）</task_done>`
  （首行摘要 = `Result` 首行截断，纯定位用；全文靠 `read`）
- 报错（`markTaskError`）→ ping
  `<task_error task_id="N" agent="X" reason="运行时崩溃">（read(task_id="N") 看详情；重试/换 agent/放弃该分支）</task_error>`

三种都仍走 `SendAndForget` **续 Leader 的轮**（与今天节奏一致，只是内容变瘦）；父状态翻回 running 的逻辑
（`reportToParent:66-73`）不变。所有内容/属性 HTML 转义。

### A.3 新增 `report` 工具（中途主动汇报，非收口）

- 语义：子任务运行中主动向父推一条进度/中间结论，**不改状态、不收口**。
- 实现：立即注入父会话
  `<task_report task_id="N" agent="X" final="false">…note(escaped)…</task_report>`（`SendAndForget`，busy 回退 `Enqueue`）。
- **不写 `Summary`**（`Summary` 只留给终态 `finish`），因此不会污染 A.2 的完成分流。
- 正对你说的「前提是这个 agent 没有主动告知过」——主动告知 = `finish`（终态）或 `report`（中途）。

---

## 设计 B — Leader 可观测 & 控制工具（新 MCP 工具，挂 `/mcp/orchestrate/`）

| 工具 | 参数 | 语义 |
|---|---|---|
| `status()` | 无 | 返回**本 Run 整棵任务树**快照：每个 `{task_id, agent, kind, status, brief, call_seq, 时长, blocked_on, has_summary}`。Leader 不再「两次回报之间全盲」。 |
| `read(task_id)` | `task_id` | 拉某任务**最新/最终输出**：settled → `Summary`(若有) + `Result`(全文)；running → 会话当前最新 assistant 文本。**合并 read_result + peek**。 |
| `cancel(task_id)` | `task_id` | **软取消 + 尽力硬打断**：标 `TaskCanceled` + 摘 watcher + 调 chat_svc 中断在跑 turn（若支持）+ 级联标记子孙 `canceled`。等于把 `StopRun` 的级联收窄到单任务。 |

- **归属校验**：`status`/`read`/`cancel` 都经 token `ref.sessionID` → task → `RunID` 定位；只允许操作**同 Run** 内任务
  （`read`/`cancel` 的 `task_id` 必须属于调用者所在 Run，否则报错）。`status` 默认返回整个 Run 树（Leader 是 root，正好看全量）。
- **`read` running 分支**依赖 chat_svc「取会话最新 assistant 文本」；`orch_adapter` 已有 `FinalAssistantText`（settled），
  running 需补一个薄接口 `LatestAssistantText(sessionID)`——**plan 阶段确认 chat_svc 是否已有等价能力**，无则新增。
- **`cancel` 硬打断**依赖 chat_svc「中断在跑 turn」（UI 停止按钮走的那条）——**plan 阶段确认并复用**；
  没有硬打断能力则降级为纯软取消（标记 + 摘 watcher），并在工具返回里说明「已请求取消，进行中的一轮会自行跑完」。
- `cancel` 级联：对目标 task 的所有 `dispatch` 子孙（`ParentTaskID` 链）标 `canceled`；复用/参照 `StopRun` 的级联写法。
- MCP 层：`dispatchTool`（`mcp.go:141-158`）加 `status`/`read`/`cancel` 三个 case + `orchToolSchemas()` 三条 schema。

---

## 设计 D — 软检测 / 健康（warn-only，双通知）

### D.1 机制：周期扫描（新 cron）

系统纯事件驱动、无心跳，只能靠 sweep。新增一个 cago cron（间隔约 30–60s），扫描 `RunRunning` 的活任务。
（cron 注册走 bootstrap；service 侧暴露一个 `SweepHealth(ctx, now)` 供 cron 调用 + 便于注入时钟做单测。）

### D.2 检测项（均 warn-only，绝不改 task/run 状态）

- **卡住子任务**：task 处 `running`/`awaiting-children` 且 `Updatetime` 早于阈值 `T`（默认几分钟，常量可调）。
  > `Updatetime` 只随状态变化推进——长单轮健康任务可能误报；因 warn-only，误报仅一条无害提示，可接受。
  > 精确心跳（随 turn 事件刷新 last-activity）留作后续优化，不在本期。
- **放弃型 Run**：Run `running`、根任务空闲（无活跃子孙 + 非 `awaiting-*`）却迟迟不 `done` 超过阈值 → Leader 大概率忘 `finish`。

### D.3 动作：只告警 + 双通知 + 去重

- **注入 Leader**（root 会话，`SendAndForget`）：`<health kind="stuck|abandoned" task_id="N" agent="X">…提示…</health>`
  → agent 可自纠（`cancel`/换 agent/`finish`）。卡住类注入 **Leader** 会话而非卡住的子会话（避免往可能正忙的子会话堆消息）。
- **emit `orch:run:health`**（payload `{runId, kind, taskId?, agentId?, message}`）→ 前端 `OrchNotifier` toast 给用户。
- **去重**：service 内存态 `map[runID]map[taskID]warnedKind`，一次卡住只报一次；task settle 或脱离卡住条件后清除该键。

---

## 设计 E — 可参与 agent 生效（硬）

### E.1 数据模型（唯一 schema 变更之二）

- `orchestration_runs` 新增 `allowed_agent_ids text NOT NULL DEFAULT ''`（JSON `[]int64`，**空/`[]` = 全部**，向后兼容）。
- `orch_entity.OrchestrationRun` 加字段 `AllowedAgentIDs string`（JSON）。加富方法：
  - `AllowedSet() map[int64]bool`（解析 JSON；空 → nil）
  - `IsAgentAllowed(id, leaderID int64) bool`（集合为空 → true；否则 `id ∈ set || id == leaderID`）
- 迁移：尾部追加 `202607030002_orch_run_allowed_agents.go`（native SQL ADD COLUMN）。与 A.1 迁移可两文件或合一，**核对最新 ID 排号**。

### E.2 `CreateRun` 落库

`create.go`：把 `req.AllowedAgentIDs`（去重、剔 0）JSON marshal 写入 `run.AllowedAgentIDs`。其余不变。
> Leader 与 allowedAgentIds **相互独立**（沿用 `2026-07-03…team-department-picker` spec 决策）：不自动把 Leader 并进选择集，
> 但 `IsAgentAllowed` 里 Leader 恒允许（防 Leader 自调被拒的边角）。

### E.3 强制点

- `agent_list`（`handleAgentList`）：改签名带上 `ref` → 解析 `run` → 集合非空则**只回 `allowed ∪ {leader}`**；空集合回全部（现状）。
  （`handleAgentList` 当前 `m.handleAgentList(w, r, id)` 不带 ref，需要像其它 handler 一样传 `ref`。）
- `Dispatch` / `Ask`：解析 `target` 后调 `s.assertAgentAllowed(ctx, runID, target.ID)`（新 service 私有助手，
  加载 run + `IsAgentAllowed`）。集外 → 返回 `errAgentNotAllowed`，MCP 层透传成工具错误，文案含允许清单：
  `agent "X" 不在本次可参与范围；可选：A/B/C`。
- `reply`（按 `ask_id`）/`send`（按 `task_id`）不按名解析目标，无需改。
- `subagent_svc` 的 `agent_call`/`agent_list` 是**另一条非编排路径**（一次性子调用、无 Run），**不在本期范围**。

---

## guidance 更新（`turn.go:orchGuidance`）

必须教会 agent 新模型，否则不会用新工具。在现有 guidance 基础上补：

- 子任务完成默认**只发通知**（`<task_done>`/`<task_error>`），要看输出用 `read(task_id)`；要主动汇报/收口用 `report`/`finish`（才会内联给你）。
- 用 `status()` 看全局任务树；`cancel(task_id)` 中止跑偏/卡住的子任务。
- 可参与范围可能被限定：`agent_list` 即为你可调度的全集，`dispatch`/`ask` 集外会被拒。
- 保留原「一切结果回到你、由你决定下一步、无次数/时长/成本上限」的自治语。

---

## 事件 & 前端

- 新事件 `orch:run:health`（`kind: stuck|abandoned`）：加进 `orchestration/events.ts` 的 `ORCH_EVENTS`，
  `orch-events-host.tsx` 订阅转发，`orch-notifier.tsx` 据此 toast（含「查看」跳转 Run）。
- （可选，非本期必须）`RunItemDTO`/`RunLoad` 回带 `allowedAgentIds` 供 Run 页展示「本次团队」chips；enforcement 不依赖它。

## i18n

仅**用户可见**新文案入 `t(...)` + `zh-CN`/`en` 双 locale：health toast（卡住/放弃提示 + 「查看」）。
注入 Leader 的 XML 信封与工具返回文案是 **agent 面向的动态内容/框架语，不入 i18n**（与现有 guidance/`【…】` 一致）。

## 测试计划（TDD，先红后绿）

**后端（service 单测走 mock，repo 单测走 sqlmock，均不连库）**

1. A 分流：child settle 有 `Summary` → 断言注入 `<task_report final="true">`；无 → `<task_done>` ping 且 `Result` 落库；
   `markTaskError` → `<task_error>` ping（mock `SendAndForget` 捕获注入文本断言 XML 形状 + 转义）。
2. A `report`：running 中调 → 立即注入父 `<task_report final="false">`，不改状态、不写 `Summary`。
3. A watcher 合并：`finish` 先写 `Summary`/`done` 后，watcher 只更新 `Result`、不覆盖 `Summary`/`Status`。
4. B `status`/`read`/`cancel`：handler + service（含跨 Run `task_id` 拒绝、`cancel` 级联子孙、硬打断调用/降级）。
5. D `SweepHealth(ctx, now)`：注入时钟 → 越阈值 emit `orch:run:health` + 注入 Leader 一次；去重（第二次 sweep 不重复报）；未越阈值不报。
6. E：`CreateRun` 持久化 `allowed_agent_ids`（sqlmock repo 断言列）；`agent_list` 集合非空只回 `allowed∪leader`、空回全部；
   `Dispatch`/`Ask` 集外 → `errAgentNotAllowed`、集内/Leader 放行；migration 测。
7. `i18n.test.ts`：health 新键 zh/en 覆盖。
8. **收尾**跑 `make test-backend` + `make lint` + 全量 `pnpm test`，看**真 exit code**。

## 实现切片（建议 plan 拆分，各自独立 commit）

1. **E — 可参与 agent 生效**（死字段修复，最独立，建议先做）：迁移 + 实体 + `CreateRun` + `agent_list`/`Dispatch`/`Ask` 强制 + 测试。
2. **A — 回报分层 + read 基础**：迁移（`summary`）+ `complete.go` 分流 + XML 信封 + `report` + guidance + 测试。
3. **B — 观测/控制工具**：`status`/`read`/`cancel` + chat_svc 原语（`LatestAssistantText`/turn 中断）确认与接线 + 测试。
4. **D — 软检测**：cron + `SweepHealth` + `orch:run:health` + 前端 toast + i18n + 测试。

> A 与 B 有耦合（`read` 是 A 的「拉」侧）：A 先落 `<task_done>` ping 里引用 `read(task_id)`，B 落 `read` 实现；
> 二者可同一 plan 也可前后脚，plan 阶段定。

## 非目标

- **不做硬资源上限**（次数/深度/时长/成本封顶——即当初未选的 C）。
- 不改 `subagent_svc`（非编排的一次性子调用路径）。
- 不做精确 last-activity 心跳（D 先用 `Updatetime` 近似，warn-only 可接受）。
- 不引入 HTTP 风格 app API；仅走既有 wails 绑定 + `/mcp/orchestrate/`。
- 不动 `ask`/`reply`/`send` 既有语义与 `<peer_ask>` 现名。

## 开放问题（plan 阶段确认）

- chat_svc 是否已有「取会话最新（进行中/末条）assistant 文本」能力供 `read` running 分支？无则新增薄接口。
- chat_svc 是否已有「中断在跑 turn」能力（UI 停止按钮）供 `cancel` 硬打断复用？无则 `cancel` 降级为纯软取消。
- 两处迁移当日最新 ID 排号（`develop/wyz` 并发迁移风险，见 memory `project_migration_squash_group_features`）。
