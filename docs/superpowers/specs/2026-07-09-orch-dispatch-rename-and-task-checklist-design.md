# 编排工具整理 + 执行节点改名 dispatch + 任务清单子系统

日期：2026-07-09
分支：`develop/wyz`
范围：`agentre/`（桌面端）

## 背景与动机

对当前编排（orchestration）MCP 工具集做一次整理，并厘清「任务」这个词的语义。

现状问题：

1. `orch_tasks` 表的每一行是「把一段活儿/对话**派发**给某 agent，新起一条子会话」——它是**执行树节点**，不是待办事项。但代码、工具（`task_id`）、UI 全叫「任务/task」，语义错位。
2. 编排缺少一个真正的「待办清单」——Leader 和子 agent 之间没有一块共享的、可勾选进度的协作白板。
3. `dispatch` 工具暴露的 `isolate` 参数是**死参**：schema 宣称 `true=独立 git worktree 隔离`，service 层一路透传到 `EnsureOrchSessionInput.Isolate`，但 `internal/app/orch_adapter.go` 直接丢弃（`chat_svc.EnsureSessionRequest` 无对应字段）。传 true/false 行为完全一样，误导 agent。
4. `cancel` 工具要移除。
5. `agent_list` 完全无运行态，Leader 拆活时看不出哪个 agent 正忙。
6. 右栏「任务板」（`TaskBoard`）与中间的 `StructureGraph` 派发树导航**重复**。

核心决定：**「任务/task」这个词彻底归属新的待办清单**；执行树节点全量改名为 **dispatch（派发）**。

## 目标（本次范围）

- 执行侧：`task` → `dispatch` **全量改名**（表 / 实体 / 常量 / repo / DTO / service / 工具参数 / 前端）。
- 执行侧工具集：删 `cancel`；`dispatch` 拆掉死参 `isolate`；`agent_list` 补每个 agent 的在跑（in-flight）计数；其余工具全部 `task_id` → `dispatch_id`。
- 新增待办清单子系统（`task`）：新表 + 实体 + repo + 三个 MCP 工具（`task_list` / `task_add` / `task_update`），Run 内所有 agent 读写，与派发/会话**零联动**。
- 前端：删 `TaskBoard`，右栏默认换成只读任务清单；派发导航完全交给 `StructureGraph`。

## 非目标（明确排除）

- **不**实现 `isolate` 的 worktree 隔离（本次仅从 schema/链路移除死参；将来单独做）。
- 待办清单**不**与 dispatch 联动（不因某次派发完成而自动改待办状态）。
- **不**动 `develop/wyz` 上已有的 `mcp.go`/`mcp_test.go` 未提交改动（另一会话的注入门控修复），与本任务无关。
- 不做与本任务无关的重构 / 格式化 / 改名扫荡（全量 `task→dispatch` 改名是本任务**有意**的核心，不算 drive-by）。

---

## 一、执行节点全量改名 `task` → `dispatch`

语义：一行 = 一次派发（brief 派给某 agent，绑定一条子会话，`parent_dispatch_id` 串成派发树）。

### 数据层（迁移 1，追加到 `migrations/migrations.go` 末尾）

`migration202607090001()` — 原生 SQL：

```sql
ALTER TABLE orch_tasks RENAME TO orch_dispatches;
ALTER TABLE orch_dispatches RENAME COLUMN parent_task_id TO parent_dispatch_id;
```

- SQLite 3.25+ 支持 `RENAME TO` / `RENAME COLUMN`；数据原地保留。
- Down：反向 rename。
- 迁移自带 `*_test.go`（走真实 DB，属允许例外）：断言重命名后 `orch_dispatches` 存在、`orch_tasks` 不存在、`parent_dispatch_id` 列存在。
- **次序关键**：本迁移必须在「新建 orch_tasks 清单表」（迁移 2）**之前**，先把老表腾空 `orch_tasks` 这个名字。

### 实体 / 常量（`internal/model/entity/orch_entity/`）

- `task.go` → `dispatch.go`
- `Task` struct → `Dispatch`；`TableName()` 返回 `orch_dispatches`
- `ParentTaskID` → `ParentDispatchID`（列 `parent_dispatch_id`）
- Kind 常量 `TaskKindDispatch`/`TaskKindAsk` → `DispatchKind*`
- 状态常量 `TaskPending/TaskRunning/TaskAwaitingChildren/TaskAwaitingUser/TaskDone/TaskCanceled/TaskPaused/TaskError` → `Dispatch*`
- 方法 `IsTerminal/IsWaitingUser/IsActive` 保留（挂在 `Dispatch` 上）

### 仓储（`internal/repository/orch_repo/`）

- `task.go` → `dispatch.go`；`TaskRepo` → `DispatchRepo`；accessor `Task()/RegisterTask()/NewTask()` → `Dispatch()/RegisterDispatch()/NewDispatch()`
- 方法名不变（`Create/Update/Find/FindBySession/ListByRun/CountByRunAgent`），仅签名里 `*orch_entity.Task` → `*orch_entity.Dispatch`
- **新增** `CountActiveByRunAgent(ctx, runID, agentID) (int64, error)`（仅统计非终态派发，供 `agent_list` 的在跑计数；见第三节）
- repo 单测（sqlmock）SQL 里表名 `orch_tasks` → `orch_dispatches`

### 服务（`internal/service/orch_svc/`）

- 约 200+ 处 `Task` 标识符统一改名（`s.tasks` → `s.dispatches` 等；deps/accessor 一并改）
- `status.go`：`statusRow` 的 `task_id`/`parent_task_id`/`blocked_on` 字段语义 → 派发 id；渲染函数注释「任务树」→「派发树」

### Wails DTO（`internal/app/orch.go`）

- `TaskDTO` → `DispatchDTO`；`toTaskDTO` → `toDispatchDTO`
- `RunDetailDTO.Tasks []*TaskDTO` → `Dispatches []*DispatchDTO`，json tag `tasks` → `dispatches`

### 前端

- `wailsjs` 绑定 `make generate` 重生（`TaskDTO`→`DispatchDTO`、`detail.tasks`→`detail.dispatches`）
- `StructureGraph`：`task.taskId` → `dispatch.dispatchId` 等；节点/分组文案「任务」→「派发」
- run store（`orch-run-store.ts`）字段随之改名
- i18n：`orchestration.*` 中「任务」相关文案按语义改为「派发」（保留给新清单的「任务」键见第三节）

### 工具参数改名（`mcp.go` schema + handler）

- `dispatch` 返回文案 `task_id=` → `dispatch_id=`
- `send` / `read` 入参 `task_id` → `dispatch_id`
- `status` 工具描述与输出字段：「任务树」→「派发树」，`task_id`→`dispatch_id`

---

## 二、执行侧工具集变更

| 动作 | 工具 | 说明 |
|---|---|---|
| **删** | `cancel` | 移除 schema、`dispatchTool` case、`handleCancel`、service `CancelTask`（`cancel.go`）及其测试；连带清理仅被 cancel 使用的级联逻辑 |
| **改** | `dispatch` | 移除 `isolate`：schema 属性、`handleDispatch` 入参结构、service `Dispatch` 签名、`EnsureOrchSessionInput.Isolate` 字段、`orch_adapter.go` 相关注释一并拆除 |
| **改** | `agent_list` | 每项补 `running`（在跑派发数，来自 `CountActiveByRunAgent`）；`status` 工具**保留不动** |
| 改名 | `dispatch`/`ask`/`reply`/`send`/`finish`/`report`/`read`/`status` | 涉及 `task_id` 的全部 → `dispatch_id`（见第一节） |

`agent_list` 返回项新增字段（`agentListItem`）：

```go
type agentListItem struct {
    ID          int64  `json:"id"`
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    SystemBadge string `json:"systemBadge,omitempty"`
    Running     int    `json:"running"` // 本 Run 内该 agent 当前在跑的派发数
}
```

---

## 三、新增待办清单子系统 `task`（与派发树零联动）

定位：类 TodoWrite 的待办清单，纯组织思路 + 给人看进度，**不触发任何执行**。

### 数据层（迁移 2，追加在迁移 1 之后）

`migration202607090002()` — 原生 SQL 新建 `orch_tasks`（**全新语义的同名表**）：

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | bigint PK autoInc | |
| `run_id` | bigint not null | Run 作用域 |
| `seq` | int not null default 0 | 稳定展示顺序（add 时递增） |
| `text` | text not null default '' | 一句话待办 |
| `status` | text not null default 'pending' | `pending` / `in_progress` / `done` |
| `assignee_agent_id` | bigint not null default 0 | 认领人（0=未认领） |
| `created_by_agent_id` | bigint not null default 0 | 创建者（溯源） |
| `createtime` | bigint not null default 0 | |
| `updatetime` | bigint not null default 0 | |

Down：`DROP TABLE orch_tasks`。迁移自带真实 DB 测试。

### 实体（`orch_entity`，全新 `Task`）

`task.go`（新文件，与已改名走的 `dispatch.go` 并存）：

```go
type Task struct { // 语义 = 待办清单条目（非派发节点）
    ID              int64
    RunID           int64
    Seq             int
    Text            string
    Status          string // TaskStatusPending/InProgress/Done
    AssigneeAgentID int64
    CreatedByAgentID int64
    Createtime      int64
    Updatetime      int64
}
func (*Task) TableName() string { return "orch_tasks" }
```

状态常量：`TaskStatusPending = "pending"` / `TaskStatusInProgress = "in_progress"` / `TaskStatusDone = "done"`。
富实体校验：`ValidStatus(s) bool`（拒绝非法状态）。

### 仓储（`orch_repo`，全新 `TaskRepo`）

老 `TaskRepo` 已改名 `DispatchRepo`，`TaskRepo` 名字腾出给清单：

```go
type TaskRepo interface {
    Create(ctx, *orch_entity.Task) error   // 返回后带 ID
    Update(ctx, *orch_entity.Task) error
    Find(ctx, id int64) (*orch_entity.Task, error)
    ListByRun(ctx, runID int64) ([]*orch_entity.Task, error)
    MaxSeq(ctx, runID int64) (int, error)  // add 时算下一个 seq
}
```

accessor `Task()/RegisterTask()/NewTask()`。sqlmock 单测。

### 服务（`orch_svc`，新增 `todo.go` 或 `task_list.go`）

三个方法（Run 作用域由调用者 sessionID → 派发 → RunID 推出，与现有工具一致）：

- `TaskList(ctx, sessionID) (string, error)` — 返回本 Run 全部清单条目 JSON（id/seq/text/status/assignee/…）
- `TaskAdd(ctx, sessionID, agentID, text) (int64, error)` — 新增一条（seq=MaxSeq+1，created_by=agentID，status=pending），返回 id
- `TaskUpdate(ctx, sessionID, agentID, id, status*, claim*) error` — 按 id 改状态（校验合法）/ 认领（assignee=agentID）；越 Run 操作拒绝

约束：
- 权限 = Run 内任意 agent（与 dispatch/status 同款 `sessionInRun` 判定），**不做 sole-owner 限制**（协作白板）。
- 与 dispatch 完全解耦：不读写 `orch_dispatches`，不因派发状态变化触发。

### MCP 工具（`mcp.go`）

三件套，注入链路与现有编排工具完全一致（同一 `orchToolSchemas()` + `dispatchTool` + `MintToken` 门控）：

| 工具 | 参数 | 作用 |
|---|---|---|
| `task_list` | 无 | 读本 Run 待办清单 |
| `task_add` | `text`(必填) | 加一条，返回 `task_id` |
| `task_update` | `task_id`(必填) + `status`(可选) + `claim`(可选 bool) | 改状态 / 认领；至少给一个 |

`task_add`/`task_update` 的 `agentID` 取自 HMAC token（`ref.agentID`），用于 `created_by` / `assignee`。

> 命名不撞车确认：执行侧工具用 `dispatch_id`，清单侧用 `task_id`——两者语义已彻底分离（dispatch=派发节点，task=待办）。

---

## 四、前端：右栏改为只读任务清单

现状：右栏 `<aside w-80>` 在无选中会话时回落显示 `TaskBoard`（派发导航树 + 进度 + 产出物 tab）；中间 `StructureGraph`（601 行）本就是完整可点的派发树。

变更：

- **删除** `task-board.tsx` 及其测试（导航由 `StructureGraph` 独担；产出物 tab 现基本空——挂着 `TODO(plan-1b)`——一并丢弃）。
- **新增** `task-list.tsx`（只读任务清单）：
  - 数据源：`RunDetailDTO` 新增 `Tasks []*app.TaskDTO`（清单条目，后端 `RunLoad` 一并返回）+ 编排事件驱动刷新。
  - 展示：每条 `[status 图标] text  [认领人徽标]`，顶部「N/M 完成」进度。
  - **只读**：不加复制按钮；最外层 wrapper 标 `data-selectable-text="true"` 允许选中复制（遵循既有约定）。
  - i18n：新增 `orchestration.tasks.*` 键（`title`/`empty`/`progress` 等，zh-CN + en 双份）。
- `index.tsx` 右栏回落分支：`TaskBoard` → `TaskList`。

> 注意：前端此处 `TaskDTO` 是**清单条目**新语义；派发条目的 DTO 已改名 `DispatchDTO`。二者不再同名。

---

## 五、数据流

```
Leader/子 agent 会话
   │  (MCP over HTTP, Bearer=MintToken(agentID,sessionID))
   ▼
orchMCP.dispatchTool ──► 派发侧: dispatch/ask/reply/send/finish/report/read/status/agent_list
   │                          └─► orch_svc ──► orch_repo.DispatchRepo ──► orch_dispatches
   └──────────────────────► 清单侧: task_list/task_add/task_update
                              └─► orch_svc ──► orch_repo.TaskRepo ──► orch_tasks(新)

前端 Run 详情:
  StructureGraph  ← detail.dispatches (派发树导航)
  TaskList (右栏) ← detail.tasks      (待办清单, 只读)
```

## 六、错误处理

- 工具入参缺失 → JSON-RPC `-32602`（沿用现有约定）。
- `task_update` 非法 status → 富实体 `ValidStatus` 拒绝，返回 `-32602`。
- 越 Run 操作（task 不属于调用者所在 Run）→ `-32000` 拒绝。
- 迁移 1 若目标 SQLite 不支持 `RENAME COLUMN`（<3.25，理论上不会）→ 回退「建新表 + copy + drop」模式；迁移测试会先暴露。

## 七、测试策略（TDD：Red → Green → Refactor）

- **迁移**：真实 DB `*_test.go`——迁移 1 断言 rename 生效；迁移 2 断言新 `orch_tasks` 结构 + 与 `orch_dispatches` 并存。
- **repo**：sqlmock——`DispatchRepo` 改名后 SQL 表名；新 `TaskRepo` 的 CRUD/ListByRun/MaxSeq。
- **service**：mockgen 注入 repo mock——`TaskAdd/List/Update` 的 happy path + 越 Run 拒绝 + 非法 status；`agent_list` 的 `running` 计数（active-only）；`dispatch` 去掉 isolate 后签名。
- **mcp handler**：三个新工具的 tools/list schema 出现、tools/call 分发、门控（token/`sessionInRun`）；`cancel` 已从 schema 与分发移除。
- **前端**：`task-list.test.tsx`（渲染/进度/空态/可选中）；`index.test.tsx` 右栏回落改为 TaskList；删除 `task-board.test.tsx`；StructureGraph 测试随字段改名更新；i18n 覆盖测试（`i18n.test.ts`）。
- **收尾闸门**：`make test-backend` + `make lint`（含 `gofmt -l`）+ 全量 `pnpm test`（看真 exit code）。

## 八、迁移次序与风险

1. 追加迁移 1（rename orch_tasks→orch_dispatches）→ 迁移 2（create 新 orch_tasks）。**顺序不可颠倒**。
2. 全量改名是大 diff，但属本任务有意范围；分批提交（gitmoji），每次 `git commit <files>` 带 pathspec，避免卷入 `develop/wyz` 上并发会话 staged 的 mcp.go/mcp_test.go。
3. `make generate` 重生 wailsjs 后前端才编译得过；worktree 场景注意 `GOWORK=off` 与占位 `frontend/dist`（见既有约定）。

## 九、交付顺序建议（供 writing-plans 细化）

1. 迁移 1 + 实体/repo/service/DTO/工具 的 `task→dispatch` 改名（一个可编译的绿点）。
2. 删 `cancel` + 拆 `isolate` + `agent_list` 补 running。
3. 迁移 2 + 清单实体/repo/service/三工具（后端闭环）。
4. 前端：`make generate` + 删 TaskBoard + 新 TaskList + StructureGraph 字段改名 + i18n。
