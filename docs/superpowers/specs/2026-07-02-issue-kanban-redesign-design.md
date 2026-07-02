# Issue 看板重构 — Design Spec

**日期**: 2026-07-02
**仓库**: `agentre/`（Wails v2 桌面端）
**状态**: 已批准（brainstorm + Pencil 设计稿确认），待写实现计划
**前置**: [2026-06-03-issue-tracker-v1-design.md](2026-06-03-issue-tracker-v1-design.md)（v1 数据层 + List/Board mock 接真实数据，已合入）

## 背景与目标

现状：`frontend/src/components/agentre/issues-page.tsx` 已接真实数据，含 **List** 与 **Board** 两视图，但 Board 只按 `state` 客户端分成 待派发(open) / 已关闭(closed) 两列，无拖拽，卡片只能点开编辑；v1 明确把「多阶段工作流」「拖拽」「派发给 agent」都延后了。

本次目标：**把 issue 模块重构成以看板为主导的工作项系统**——看板成为默认主视图、引入真正的多阶段工作流 + 拖拽排序、并把 **指派 / 派发给 Agent** 做成一等公民（这是 agentre「multi-agent is the shape」的核心）。List 视图保留为次要视图。

## 决策记录（brainstorm 结论，已锁定）

| # | 维度 | 决策 |
| - | ---- | ---- |
| 1 | 列模型 | **手动工作流阶段**：新增 `stage` 字段，用户拖拽卡片在列间移动即改 `stage`；`agent_status` 作为卡片徽标 |
| 2 | 列集合 | **固定 4 列**：`待办(todo)` / `进行中(doing)` / `待审阅(review)` / `已完成(done)`；本轮枚举写死，可配置列延后 |
| 3 | 关闭语义 | **已完成即终态**：`stage` 成为主生命周期字段；进入 `done` 列即 `state=closed`+记 `closed_at`，移出即 reopen；不做独立归档 |
| 4 | 列表视图 | **保留为次要视图**：看板默认，顶部 列表/看板 切换，List 复用现有代码 |
| 5 | 列内排序 | **手动拖拽排序并持久化**：新增 `position REAL` 字段，分数排序（midpoint 插入 + 稀疏重排） |
| 6 | 分配 agent | **指派 + 一键派发（完整）**：指派 `assignee_agent_id`；派发起一个绑定 project+agent 的 `chat_entity.Session`，`agent_status` 接真实会话态 |

### 派发机制（已确认的默认行为）

1. **派发自动进阶**：从 `待办` 派发时卡片自动移到 `进行中`；其余阶段移动全靠手动拖拽（守住「手动阶段」心智）。
2. **点已派发卡片打开其会话**：跳到该 `session_id` 的 chat 观看/审批；未派发卡片点击开编辑弹窗。
3. **`agent_status` 回写**：被链接会话的状态变化（running/waiting/error/idle）回写到 issue 的 `agent_status`，驱动卡片徽标；含远端 `agentred` 会话。
4. **完成手动**：会话结束只更新徽标，用户手动把卡片拖到 `待审阅 / 已完成`。

## 设计稿（Pencil，`agentre.pen`，已确认）

| 帧 | 覆盖 |
| -- | ---- |
| `Issues — Kanban — Light` / `— Dark` | 看板主视图：4 阶段列、阶段形状图标、拖拽（抬起卡片 + 落位槽）、卡片(#id/标题/标签/仓库·时间/**指派 agent 头像** + 实时 `agent_status` 徽标)、已完成卡打勾+淡化 |
| `Issues — New Issue Dialog` | 新建/编辑表单：标题(必填)、描述(md)、项目、**阶段**、**指派 Agent**、标签多选；底部 `取消 / 创建 / 创建并派发`；`⌘↵` 提示 |
| `Issues — Card Menu` | 卡片快捷菜单：`派发给 Agent`(未派发领衔) / 编辑 / 标记完成 / 移到… / 删除 |
| `Issues — Empty State` | 首次无 issue 空态：图标 + CTA `新建 Issue` + `C` 快捷键提示 |

> 设计语言遵循 `docs/DESIGN.md` + `PRODUCT.md`：Linear 邻近、密集有层次、chrome 克制（扁平卡/形状阶段图标/安静内联状态/纯数字计数/仓库名 mono）。

## 数据模型

新增迁移 `migrations/202607020001_issue_kanban.go`（追加到 `migrationList()` 末尾，当前最新为 `202606250002`，**禁改既有迁移**；DDL 用原生 SQL）。

### `issues` 表新增列

| 列 | 类型 | 说明 |
| -- | ---- | ---- |
| `stage` | TEXT NOT NULL DEFAULT 'todo' | `todo` \| `doing` \| `review` \| `done`（主生命周期） |
| `position` | REAL NOT NULL DEFAULT 0 | 列内排序键（分数排序，升序=靠上） |
| `assignee_agent_id` | INTEGER NOT NULL DEFAULT 0 | 指派的 agent；0=未指派 |
| `session_id` | INTEGER NOT NULL DEFAULT 0 | 派发产生的 `chat_sessions.id`；0=未派发 |

已有列 `state` / `agent_status` / `closed_at` 保留：
- `state`（open/closed）成为 `stage` 的**派生镜像**（`closed` 当且仅当 `stage='done'`），由 entity 在 `SetStage` 时同步，供 List 视图 Open/Closed tab + 计数继续工作。
- `agent_status`（idle/running/waiting/error）此后由派发的会话回写驱动。

**回填**（同一迁移内原生 SQL）：
- `stage = CASE WHEN state='closed' THEN 'done' ELSE 'todo' END`
- `position = createtime`（按创建时间给稳定初值；升序=老的靠上）
- `assignee_agent_id = 0`、`session_id = 0`

索引：`idx_issues_board (status, stage, position)`（看板按 stage 分组 + position 排序）；保留既有 `idx_issues_state`。

> agentre 未发布，可干净迁移、无需兼容层。

## 后端分层（cago 风格）

### `internal/model/entity/issue_entity/`

`Issue` 充血实体新增字段 + 方法：
- 常量 `StageTodo/StageDoing/StageReview/StageDone`；`KnownStages` 集合。
- `Stage string` / `Position float64` / `AssigneeAgentID int64` / `SessionID int64` 字段。
- `SetStage(stage string, now int64)`：置 `stage`；若 `stage==done` → `Close(now)`（`state=closed`+`closed_at`）；若离开 `done` → `Reopen()`。**stage↔state 一处同步，不在消费端各自兜底**。
- `Assign(agentID int64)` / `LinkSession(sessionID int64)` / `IsDispatched() bool`。
- `Check(ctx)` 增加 `stage ∈ KnownStages` 校验。
- 保留 `IsOpen/IsClosed/IsActive`。

### `internal/repository/issue_repo/`

`IssueRepo` 接口新增/调整（仍统一 `db.Ctx(ctx)`，单测一律 sqlmock）：
- `List(ctx, ListFilter)`：`ListFilter` 的 `Sort` 落地——`sort="position"` → `ORDER BY stage, position ASC, id ASC`（看板）；默认 → `updatetime DESC`（列表）。
- `FindBySessionID(ctx, sessionID) (*Issue, error)`：`agent_status` 回写用。
- `Update` 覆盖新列（stage/position/assignee_agent_id/session_id）。
- `StageCounts(ctx, filter) (map[string]int64)`：各阶段计数（列头用），带 project/label 过滤。
- 邻居查询（Move 计算 position 用）：`ListStageForRank(ctx, stage, filter) ([]*Issue ordered by position)` 或复用带 `sort=position` 的 List。

### `internal/service/issue_svc/`

接口（只依赖 repo 接口 + `Default()` 单例；关键流程 `logger.Ctx(ctx)` 前缀 `issue_svc.Method:`）：

```
Create(ctx, *CreateIssueRequest) (*IssueDetail, error)   // 默认 stage=todo，position=该 stage 末尾+STEP；可带 assigneeAgentID
Update(ctx, *UpdateIssueRequest) (*IssueDetail, error)
Move(ctx, *MoveIssueRequest) (*IssueDetail, error)       // {ID, Stage, AfterID}：改 stage + 计算 position
Assign(ctx, id, agentID int64) (*IssueDetail, error)     // 仅指派，不起会话
Dispatch(ctx, *DispatchRequest) (*IssueDetail, error)    // 指派(如给) + 起会话 + 链接 + stage→doing
Stop(ctx, id int64) (*IssueDetail, error)                // 停止会话
SetState(ctx, id int64, state string) (*IssueDetail, error)  // List 视图关闭/重开：委托 SetStage(done/todo)
Delete(ctx, id int64) error
Get(ctx, id int64) (*IssueDetail, error)
List(ctx, *ListIssuesRequest) (*ListIssuesResponse, error)   // 含 stageCounts + open/closed 计数
ListLabels(ctx) ([]*issue_entity.Label, error)
SyncAgentStatus(ctx, sessionID int64, status string) error   // agent_status 回写入口
```

- **position 计算（Move）**：`AfterID=0` → 落在目标 stage 顶部（`firstPos - STEP`）；否则取 `AfterID.position` 与其下一个兄弟 `position` 的中点；落底 → `lastPos + STEP`。中点间隙过小（< ε）时对该 stage 触发一次稀疏重排（重新等距赋值）。`STEP` 用较大常量（如 65536）。
- **Dispatch 编排**：确保 `assignee_agent_id`（请求带或已存在）→ 调 `chat_svc` 创建绑定 `project_id` + agent 的 `Session`，把 issue 的 `title+body` 作为首条任务 prompt 注入 → `issue.LinkSession(sessionID)`、`SetStage(doing)`、`agent_status` 初始置 running（或待会话回写）→ `repo.Update`。后台动作用 `gogo.Go`，不透传请求 ctx。
- **`SetState` 保留**给 List 视图的关闭/重开，内部走 `entity.SetStage(done/todo)`，保证与看板一致。

### `agent_status` 回写接缝（本次最高风险点）

需求：被派发会话的 `agent_status` 变化要驱动卡片徽标。约束：保持依赖单向，`chat_svc` **不得反向 import** `issue_svc`。

**推荐设计（DIP + 观察者）**：
- 在 `chat`/session 生命周期的「`agent_status` 落库」边界，回调已注册的观察者接口 `SessionStatusObserver{ OnAgentStatusChanged(ctx, sessionID int64, status string) }`。
- `issue_svc` 实现该接口并在 bootstrap 里 `RegisterSessionStatusObserver(issue_svc.Default())`；实现内部 `FindBySessionID` → 命中则 `SyncAgentStatus`。
- 未命中（普通会话）直接 no-op。远端 `agentred` 会话的状态本就经既有回写路径落库，故观察者对本地/远端统一生效。

> 该接缝的确切位置（在哪个方法回调、是否复用现有事件总线）留给实现计划先行侦查现有 session 状态更新链路后再定；spec 只锁定「解耦观察者、issue 单向依赖」这一架构方向。

### `internal/app/issue.go`（Wails 绑定，thin：parse → svc → return）

新增/调整 camelCase DTO 方法：
```
IssueMove(req *IssueMoveRequest) (*IssueItem, error)         // {id, stage, afterID}
IssueAssign(req *IssueAssignRequest) (*IssueItem, error)     // {id, agentID}
IssueDispatch(req *IssueDispatchRequest) (*IssueItem, error) // {id, agentID?}
IssueStop(id int64) (*IssueItem, error)
```
- `IssueItem` 增 `stage / position / assigneeAgentID / sessionID`（`agentStatus` 已有）。
- `IssueListResponse` 增 `stageCounts map[string]int64`。
- 业务全在 svc，绑定层不塞逻辑。

## 前端

重写 `frontend/src/components/agentre/issues-page.tsx`（board 拆分到子组件降复杂度）：

- **默认视图 = 看板**；顶部保留 `列表 / 看板` 切换，List 视图复用现有实现。
- **看板**：4 固定阶段列（i18n 标签 + 形状图标 `circle/circle-dot/circle-dashed/circle-check-big` + 阶段强调色），客户端按 `stage` 分组、列内按 `position` 排序；列头显示 `stageCounts` + `+`（在该阶段新建，表单预置 stage）。
- **拖拽**：接 `@dnd-kit/core` + `@dnd-kit/sortable`（仓库已用于 tab-strip/project-page/org-tree）——`DndContext` + 每列 `SortableContext` + 卡片 `useSortable` + 列 droppable；跨列=改 stage，列内=改 position；`DragOverlay` 抬起态 + 落位槽；**乐观更新** 后调 `IssueMove`，失败 toast + reload 回滚。
- **卡片**：`#id`、标题、标签、仓库(项目名 mono)、相对时间、**指派 agent 头像**（agent 调色板色块+首字）、实时 `agent_status` 徽标（idle 不显示；running/waiting/error 内联点+文案）；`done` 卡打勾 + 轻微淡化。点未派发卡→编辑弹窗；点已派发卡→打开其会话。
- **卡片菜单**（复用 List 的 `RowActions` 模式，dnd-kit 下用 pointer sensor + 阻止拖拽冲突）：未派发 `派发给 Agent / 编辑 / 标记完成 / 移到… / 删除`；已派发 `查看会话 / 停止 / 重新派发 / 编辑 / 删除`。
- **新建/编辑弹窗**（shadcn `Dialog`）：字段 标题(必填)/描述(textarea·md)/项目(`ProjectListTree`)/**阶段**(shadcn `Select`)/**指派 Agent**(agent 选择器，复用 org/agent 列表)/标签(多选)。底部 `取消` + `创建`；当已指派 agent 时显 `创建并派发`（调 `IssueCreate` 后 `IssueDispatch`）。
- **agent 选择器**：列出可用 Agent（头像 + 名 + 部门），复用现有 agent 目录 hook；派发/指派与表单共用。
- **空态**：无 issue 时空态 + `新建 Issue` CTA。
- **列内空态**：某阶段 0 卡时淡落位提示。
- **加载/错误**：列/卡骨架；加载失败与 move/dispatch 失败给可恢复提示。
- **键盘**：`C` 新建；卡片 `↑↓` 选择 + `Enter` 打开（Agentre keyboard-first）。
- **i18n**：所有新增可见文案走 `t(...)`，`zh-CN` + `en` 同步（`issues.stages.{todo,doing,review,done}`、`issues.board.*`、`issues.dispatch.*`、`issues.assign.*`、空态等）；删除被取代的 `issues.columns.backlog/closed` 等旧键；表单控件一律 shadcn `@/components/ui/*`，禁原生 `<select>`；跑 `i18n.test.ts`。不翻译 agent/会话/仓库等动态内容。

## 测试（严格 TDD，Red → Green 逐层；每个 behavior spec 覆盖 happy + ≥1 边界/错误）

- **entity**：`SetStage` 各转移（todo→done 置 closed+closed_at；done→doing reopen；非法 stage 被 `Check` 拒）；`Assign`/`LinkSession`/`IsDispatched`；position 字段。
- **repo（sqlmock，无真库）**：`List` 在 `sort=position` 时按 `stage,position` 排、默认按 `updatetime`；`Update` 写全部新列；`FindBySessionID`；`StageCounts` 带过滤；迁移 ALTER+回填（迁移自带 `*_test.go`）。
- **svc（mockgen repo mock，不接 DB）**：`Move` 由 `AfterID` 邻居算中点 position + 改 stage；顶/底/中点/间隙重排各一例；`Create` 默认 stage=todo + append position；`Dispatch` 编排（建会话[mock chat 依赖]→链接→stage→doing→agent_status）；`SyncAgentStatus` 命中/未命中；`SetState` 委托 SetStage；`List` 返回 stageCounts；错误路径（未知 id / 非法 stage / 派发未指派）。
- **回写接缝**：`SessionStatusObserver` 注册后，会话状态变化触发 `issue_svc.SyncAgentStatus`（对观察者接缝写单测，mock session 侧调用）。
- **前端（Vitest，mock wailsjs：per-file `vi.mock`（importActual+override），不加全局 alias）**：看板按 stage 分 4 列 + position 排序渲染；拖拽 move 调 `IssueMove(stage, afterID)` 且乐观更新；派发流程调 `IssueDispatch`；创建并派发；卡片菜单动作；列表/看板切换；空态；agent 头像 + 徽标随 `agent_status`。
- **i18n.test** 同步（静态 key + 中英覆盖）。
- **收尾跑全量 gate**：`make test-backend` + `make lint` + 全量 `pnpm test`，看真 exit code（per-task focused 测试会漏跨包 sqlmock / 整包 goroutine-leak flake / tsc·eslint）。

## 关键不变量（强制）

- 依赖单向 `internal/app → service → repository → model/entity`；`internal/pkg` 不反向 import service/repo；**`chat_svc` 不反向 import `issue_svc`**（用观察者接口 + bootstrap 注册解耦）。
- service 只依赖 repository 接口（DIP）+ `Register/accessor` 装配。
- 迁移 append 到末尾、禁改既有；DDL 原生 SQL；回填在同迁移内。
- 关键流程打日志：`logger.Ctx(ctx)`，message 前缀 `issue_svc.Method:` 小写，动态值走 `zap.Xxx`。
- 新可见文案走 i18n；表单控件 shadcn；gitmoji commit；diff 只含 producer + 测试，无 drive-by。
- 共享分支 `develop/wyz` 上有并发会话，提交一律带 pathspec（`git commit <files>`），复审用 `git show <commit>`。

## 分阶段实现建议（供计划切片，非硬边界）

1. **数据层 + 阶段/排序**：迁移(4 列) + entity(SetStage/position) + repo(List sort=position / StageCounts / Update) + svc(Move/Create 默认) + 绑定 `IssueMove` + 单测。
2. **看板前端**：board 默认 + dnd-kit 拖拽(跨列改 stage / 列内改 position) + 乐观更新 + 卡片/列头/空态 + List 保留 + i18n。
3. **指派**：`assignee_agent_id` + `Assign` + 绑定 + 表单指派字段 + 卡片头像 + agent 选择器。
4. **派发 + 回写**：`session_id` + `Dispatch/Stop` + `chat_svc` 建会话编排 + `SessionStatusObserver` 回写接缝（先侦查现有 session 状态链路）+ 卡片实时徽标 + 点卡开会话 + 创建并派发 + 远端验证。

## 明确不做（延后，非本 spec）

指派人为「人」（仅 agent）· 评论/讨论/详情时间线 · 标签管理 UI（增删改色）· 可配置列 / WIP 限制 / 泳道 · Hook/webhook 自动建 issue（`source != manual`）· 按项目 issue 编号（继续用 db id 作 `#id`）· 看板分组切换（表单/工具栏「分组：阶段」控件本轮固定为 stage，多分组时再做，**避免只有一个选项的假控件**）。
