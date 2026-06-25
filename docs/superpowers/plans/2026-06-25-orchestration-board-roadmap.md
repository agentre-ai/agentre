# 编排板块实现路线图（拆解 → 逐片 plan）

> 关联设计 spec：`docs/superpowers/specs/2026-06-25-agent-orchestration-board-design.md`（10 屏设计稿 + 对齐方案）。
> 本文件是**拆解路线图**，不是实现计划本身。它把「编排板块」切成若干**独立可实现的切片**，给出依赖与顺序；每个切片再单独走 `writing-plans` 出详细 plan（Red→Green→Refactor）。

## 怎么用

- 板块太大，不做一份巨型 plan。按下表切片，**逐片** spec(可选)→plan→实现→提交。
- 切片尽量**独立可合并**：能在「现有编排surface（chat tab 里的 openRun）」上先做的，就不等 IA 重构。
- 每片标了：范围 / 后端 / 前端 / 依赖 / 体量 / 是否要单独 spec。
- 复查已确认的事实（落地时别再踩）：peer ask/reply **后端只对 idle 目标已通；对方 busy（正在跑 turn）时 `Ask()` 会 `ChatSendInFlight` 直接报错**（`chat.go:2133` `startTurn` 的 `lock.TryLock()` 守卫），「对方处理中也能 ask」这条**还没落地**，见 S3；`awaiting-user`/`ApprovalGateway` **定义了但零调用点**；Run **无 error 状态**（error 是 task 级）；`callSeq`(×N) 已实现且 task-board 已渲；任务板「5/12」当前未实现。

## 切片清单

| # | 切片 | 后端 | 前端 | 依赖 | 体量 | 单独 spec? |
|---|---|---|---|---|---|---|
| S1 | **流程库 tags/outline 展示层** | 迁移 + `workflow_entity`/`workflow_svc` 加 `tags`/`outline`（display-only，**绝不注入**） | `workflow-editor-form`(录入) / `workflow-manager-dialog`(列表 chip + 预览蓝图 band) / `run-new-dialog`(picker 行标签+步骤) | 无 | 中 | 否（板块 spec §6.1/§9 已够） |
| S2 | **Run 流程蓝图参考带** | 无（读 `run.FlowID`→流程 outline） | Run 视图顶部淡色「流程蓝图」带；与实时流解耦 | S1 | 小 | 否 |
| S3 | **peer ask/reply（busy 目标 + 前端）** | **有但偏**：`ask`（`orch_svc/mcp.go`）现用 `Send`/`startTurn`，对方 busy → `lock.TryLock()` 失败、`ChatSendInFlight` 报错。**已定方案**：`Ask()` idle 走 `Send`、**busy 时走 `Enqueue`/steer 注入对方当前 turn（复用 setter）**；需把 `Enqueue` 加进 orch `ChatGateway`+`orch_adapter`。**接受权衡**：对方本 turn 内不 reply → 提问方 4min 超时返错（已有超时分支） | `feed-data.ts` 渲染 `"ask"`/reply + 结构图「提问·等待回复」徽标 | 无 | 中 | 建议 |
| S4 | **结构图节点模型 + 视觉对齐** | 无 | **`graph-data.ts`**：边按 `parentTaskId`→父 task 节点（任意深度 / 非-Leader 派发）；**结构图把同一 subagent 的多调用合并成一个 `×N` 节点**（agent-as-node，图保持干净；§5.7a 图显示细化），**顶层 agent 多对话用 `N 会话` 分组容器**，per-call 明细（独立 session/钻入）喂**任务板**；`send` 不新增节点 + `structure-graph.tsx` 视觉（Leader 皇冠 + 自顶向下树 + `×N`/`N 会话` 归组 + 父节点 `+N 子代理` 徽标 + 阻塞红框 + 死锁徽标）。详见 spec §5.7a/§5.7b | 无 | **大** | 建议 |
| S5 | **任务板汇总计数 5/12** | 无（前端按 task 状态聚合） | task-board 头部 done/total 徽标 + agent/派发调用 分组（headline 只数 orch 派发 call） | 无 | 小 | 否 |
| S5b | **CLI 子代理：任务板折叠子行 + 图徽标（零后端）** | 无 | 前端 derive `tool_use.subagent` 帧（按 `task.SessionID`）→ 任务板父 call 下**默认折叠**只读子行（`▸ +N 子代理`，点开展开，auto-merge / 无 task 标）+ 结构图父节点 `+N 子代理` 合并徽标。详见 spec §5.7b | S4（图徽标挂 call 节点）/ S5（任务板分组） | 中 | 否（§5.7b 已够） |
| S6 | **进入会话（钻入·只读）** | 无（每 task 有 `SessionID`，读该 session 消息） | **调用节点** / 任务行 / 头像点击 → 右栏「任务板 ⇄ 会话」切换；transcript + 「对它说」。因 S4 改 call-as-node，每节点 = 唯一 session，**无需「一 agent 多会话」切换器** | S4（节点 = 调用） | 中 | 否 |
| S7 | **IA/导航重构** | 无 | `/orchestration` 路由 + `navItems` + `AppRail` 收敛；`OrchestrationPage` 壳；RunList 移出 chat tab（`chat-page.tsx` L653–669 + `chat-tabs-store` 的 `run` 变体） | 无（但 S2/S4/S5/S6 落到它里更顺） | 大 | 建议（重构面广，风险高） |
| S8 | **总览/落地页** | 无（数据来自 `orch-run-list-store`/`orch-run-store`） | `OrchestrationOverview`：统计 + 进行中卡片 + 最近完成 + 空态 | S7（页面壳） | 中 | 否 |
| S9 | **awaiting-user 审批接线** | **接线** `TaskAwaitingUser` 转换 + 调 `ApprovalGateway`（危险工具→task 阻塞→续跑） | 钻入/任务板内联「批准/拒绝」阻塞块 | 无（前端 UI 复用 S6 右栏） | 大 | **建议**（后端缺口大，独立特性） |

> **S3 注入格式（已定）**：ask 注入消息用 XML 标签包裹问题 + 带 `ask_id`，例 `<peer_ask ask_id="…" from="…">问题</peer_ask>` + 一句「调用 `reply(ask_id=…)` 回复」。取代现有 `【收到提问 ask_id=…】` 纯文本前缀；闭合标签是天然边界，busy steer 进对方当前 turn 时不被其输出污染、更易被识别并据 id 回 `reply`。

## 依赖图 / 建议顺序

```
Phase 0（独立、低风险、已设计透，可在现有 surface 先做）
  S1 流程库 tags/outline ──▶ S2 蓝图参考带
  S3 peer ask/reply 前端
  S4 结构图视觉对齐 ──┐
  S5 任务板 5/12 ─────┼─（可与 S4 合一个 plan）
  S5b CLI 子代理折叠子行 + 图徽标(零后端) ─┘（接 S4 图徽标 / S5 分组）
  S6 进入会话（只读）

Phase 1（IA 重构，foundational 但风险高）
  S7 顶级页/导航/RunList 迁出 ──▶ S8 总览页
  （S2/S4/S5/S6 最终落到 S7 的 OrchestrationPage 里）

Phase 2（更大的后端特性，可独立排期）
  S9 awaiting-user 审批接线（后端）+ 内联批准/拒绝（接 S6 右栏）
```

要点：
- **Phase 0 全部不依赖 S7**——它们改的是 `structure-graph`/`activity-feed`/`task-board`/`workflow-*`/`run-new-dialog` 等既有组件，可直接在现有 chat-tab 编排上落地、独立合并。
- **S7 是分水岭**：把编排从 chat tab 提到顶级页。它本身不加功能、但 blast radius 最大（路由 + chat-tabs 解耦）。建议**先把 Phase 0 的价值落了**，再做 S7 把它们「搬家」并补总览。
- **S9 最大且独立**：`awaiting-user`/`ApprovalGateway` 是真缺口，建议单独 spec。S6 先做**只读**钻入（不依赖 S9）；S6 的「批准/拒绝」交互在 S9 落地后接通。

## 建议的第一份 plan

**S1（流程库 tags/outline）** —— 本轮设计最透、后端+前端都自洽、零外部依赖：
- 后端 TDD：迁移（`migrationList()` 末尾 append，native SQL `ALTER TABLE workflows ADD tags/outline TEXT DEFAULT ''`，存 JSON）→ `workflow_entity` 字段 → `workflow_svc` 类型(List/Create/Update 带 `tags`/`outline`，JSON marshal) → repo(sqlmock)/svc(mockgen) 测试 → **一条断言：注入正文（`run.FlowContent`/Leader 注入）不含 tags/outline**。
- 前端：`workflow-editor-form`(tags chips + outline 有序列表录入) → `workflow-manager-dialog`(列表 chip + 预览蓝图 band) → `run-new-dialog`(picker 行标签+步骤) → i18n 加 `workflows.editor.tags/outline.*`、`workflows.preview.blueprint.*`（`workflows.*` 已大量存在）。
- 紧跟 **S2**（蓝图参考带）作为 S1 的延伸，或并入同一 plan。

## 未决 / 待确认

- S7 之前要不要保留「chat tab 深链兼容」（`kind:"run"`）一段时间？还是一步到位移除。
- S9 是否本轮排期，还是延后（drill-in 先只读）。
- 是否给 S7、S9 各补一份独立 spec（建议补）。
