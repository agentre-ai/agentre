# Issue Tracker v1 — Design Spec

**日期**: 2026-06-03
**仓库**: `agentre/`（Wails v2 桌面端）
**状态**: 已批准，待实现

## 背景

`frontend/src/components/agentre/issues-page.tsx` 目前是一个**纯 mock** 页面：List + Board（看板）两种视图、标签、agent 头像、评论数、状态（idle/running/waiting/error）、筛选、"派发给 Agent" 按钮，全部数据来自硬编码的 `createIssueRows()` / `createBoardColumns()`，挂在 `/issues` 路由但**不接任何后端**。

mock 暗示的产品形态：一个本地工作项追踪器，issue 可**手动或由 Hook 创建**，带标签和跟进 agent，可**派发给 agent**（起一个绑定 project 的 `chat_entity.Session`），看板列跟踪该 session 的 `agent_status`。

完整功能很大。本 spec 只覆盖 **v1：本地数据层 + CRUD + 把 mock 列表/看板换成真实持久化数据**。派发、Webhook、评论、指派人等留到后续增量。

## v1 范围

**做：**

- 新建 issue 数据层：`issues` / `labels` / `issue_labels` 三张表 + entity/repo/svc/Wails 绑定。
- 手动 CRUD：创建 / 编辑 / 关闭 / 重新打开 / 删除（软删）。
- 标签：独立 `labels` 表 + 多对多关联；v1 seed mock 的 10 个标签，标签增删改 UI 延后。
- List 视图接真实数据；Open/Closed 筛选、标签筛选、最新更新排序。
- Board 视图接真实数据，客户端按 state 分组，v1 只渲染 待派发(open) 和 已关闭(closed) 两列。

**不做（明确延后到后续增量）：**

1. 派发给 agent → chat session（进行中/待审批/失败列、重新派发、`agent_status` 驱动）
2. 指派人 assignee（`issue_assignees`）
3. 评论 / 讨论 / 详情时间线
4. Webhook/Hook 创建的 issue（`source` != manual）
5. 标签管理 UI（创建/重命名/改色/删除标签）
6. author + 分派 Agent 筛选
7. 看板列间拖拽
8. 按项目的 issue 编号（v1 用数据库 id 作 `#id`）

## 决策记录（brainstorm 结论）

| 维度 | 决策 |
| ---- | ---- |
| v1 边界 | 本地数据层 + CRUD + 接真实数据，不含派发/Webhook/评论 |
| Issue↔Project | 可选归属：`project_id`，0 = 未归属；`/issues` 仍是全局视图，project 作筛选维度 |
| 状态模型 | 持久化真相只有 `state` ∈ {open, closed}；`agent_status`（默认 idle）预留给 dispatch；看板列由 (state, 派发态) 推导，v1 只有 open/closed 列有内容 |
| 标签 | 独立 `labels` 表 + `issue_labels` 关联表；v1 seed 10 个标签，标签 CRUD UI 延后 |
| 指派人 | v1 不做；assignee 本质 = 被派发的 agent session，延后到 dispatch 增量 |
| 看板分组 | 客户端按 state 分组（v1 两列），不做服务端 board 端点；dispatch 增量再评估 |

## 数据模型

新增迁移 `migrations/202605220011_issues.go`，追加到 `migrationList()` 末尾（当前最新为 `202605220010`，**禁止改动既有迁移**）。DDL 优先原生 SQL。

### 表 `issues`

| 列 | 类型 | 说明 |
| -- | ---- | ---- |
| `id` | INTEGER PK AUTOINCREMENT | |
| `project_id` | INTEGER NOT NULL DEFAULT 0 | 0 = 未归属项目 |
| `title` | TEXT NOT NULL | 必填，非空 |
| `body` | TEXT NOT NULL DEFAULT '' | markdown 正文 |
| `state` | TEXT NOT NULL DEFAULT 'open' | open \| closed（生命周期） |
| `agent_status` | TEXT NOT NULL DEFAULT 'idle' | 预留 dispatch：idle\|running\|waiting\|error |
| `source` | TEXT NOT NULL DEFAULT 'manual' | 预留：manual \| hook:github \| ... |
| `closed_at` | INTEGER NOT NULL DEFAULT 0 | 关闭时间（unix ms），open 时为 0 |
| `status` | INTEGER NOT NULL DEFAULT 1 | 软删除 consts.ACTIVE/DELETE |
| `createtime` | INTEGER NOT NULL DEFAULT 0 | unix ms |
| `updatetime` | INTEGER NOT NULL DEFAULT 0 | unix ms |

索引：`idx_issues_state (status, state, updatetime)`、`idx_issues_project (project_id, status)`。

> `status` vs `state` 两个字段是刻意的，与代码库一致（`chat_entity.Session` 同时有 `status int` 软删 + `agent_status text`）：`status` = 软删除，`state` = open/closed 生命周期，`agent_status` = 预留派发态。

### 表 `labels`

| 列 | 类型 | 说明 |
| -- | ---- | ---- |
| `id` | INTEGER PK AUTOINCREMENT | |
| `name` | TEXT NOT NULL | 标签名 |
| `tone` | TEXT NOT NULL DEFAULT '' | 固定调色板 key |
| `sort_order` | INTEGER NOT NULL DEFAULT 0 | |
| `status` | INTEGER NOT NULL DEFAULT 1 | 软删除 |
| `createtime` | INTEGER NOT NULL DEFAULT 0 | |
| `updatetime` | INTEGER NOT NULL DEFAULT 0 | |

`tone` ∈ 固定调色板 key：`auth | bug | critical | docs | feature | hook | ops | perf | refactor | ui`（与前端 `labelToneClassNames` 一致，颜色由前端设计系统统一管理，不存任意色值）。

迁移里 seed mock 用到的 10 个标签（name 即 tone）：`auth, bug, critical, docs, feature, hook, ops, perf, refactor, ui`。`sort_order` 按上述顺序。

### 表 `issue_labels`（多对多 junction）

| 列 | 类型 |
| -- | ---- |
| `issue_id` | INTEGER NOT NULL |
| `label_id` | INTEGER NOT NULL |

主键 `(issue_id, label_id)`，索引 `idx_issue_labels_label (label_id)`。

## 后端分层（cago 风格，镜像 `project` domain）

`project_repo` 在同一个包里同时暴露 `ProjectRepo` + `ProjectAgentRepo`，issue domain 照搬这个结构。

### `internal/model/entity/issue_entity/`

充血实体：

- `Issue`
  - `Check(ctx) error` — title 非空、`state` ∈ {open, closed}
  - `IsOpen() / IsClosed() bool`
  - `Close(now int64)` — `state=closed`、`closed_at=now`
  - `Reopen()` — `state=open`、`closed_at=0`
  - `IsActive() bool` — `status == consts.ACTIVE`
- `Label`
  - `Check(ctx) error` — name 非空、tone 属于已知调色板 key
  - `IsActive() bool`
- `IssueLabel` — junction 结构体

### `internal/repository/issue_repo/`

三个 interface + Register/accessor（统一 `db.Ctx(ctx)`）：

- `IssueRepo`: `Create / Update / Find / List(ctx, ListFilter) / Delete(软删) / CountByState`
  - `ListFilter{ State string; ProjectID int64; LabelIDs []int64; Sort string }`
- `LabelRepo`: `Create / Update / Find / List / Delete / FindByName`
- `IssueLabelRepo`: `SetLabels(issueID, labelIDs) / ListByIssue / ListByIssues(map) / RemoveAllByIssue`

> 单测一律 sqlmock，禁起真库。

### `internal/service/issue_svc/`

接口 + `Default()` 单例 + 私有实现；只依赖 repo 接口（DIP，便于 mockgen）：

```
type IssueSvc interface {
    Create(ctx, *CreateIssueRequest) (*IssueDetail, error)
    Update(ctx, *UpdateIssueRequest) (*IssueDetail, error)
    SetState(ctx, id int64, state string) (*IssueDetail, error) // close / reopen
    Delete(ctx, id int64) error
    Get(ctx, id int64) (*IssueDetail, error)
    List(ctx, *ListIssuesRequest) (*ListIssuesResponse, error)
    ListLabels(ctx) ([]*issue_entity.Label, error)
}
```

- `IssueDetail` = `Issue` + 已水合的 `[]Label`（后续增量再加 assignees/comments）。
- `ListIssuesResponse` = `Issues []*IssueListItem`（每项含 labels）+ `OpenCount` + `ClosedCount`。
- `CreateIssueRequest{ ProjectID, Title, Body, LabelIDs }`；`UpdateIssueRequest{ ID, Title, Body, ProjectID, LabelIDs }`。
- 创建/更新时通过 `IssueLabelRepo.SetLabels` 维护关联；`SetState` 走 entity 的 `Close/Reopen`。
- 后台任务（如有）用 `gogo.Go`，不透传请求 ctx。

### `internal/app/issue.go`（Wails 绑定，thin）

只做 parse → `issue_svc.Default().Method` → return，camelCase DTO：

```
IssueList(req *IssueListRequest) (*IssueListResponse, error)
IssueGet(id int64) (*IssueDetailResponse, error)
IssueCreate(req *IssueCreateRequest) (*IssueDetailResponse, error)
IssueUpdate(req *IssueUpdateRequest) (*IssueDetailResponse, error)
IssueSetState(req *IssueSetStateRequest) (*IssueDetailResponse, error)
IssueDelete(id int64) error
IssueListLabels() ([]*LabelItem, error)
```

> 业务一律放 svc，绑定层不可塞逻辑（否则 go test 覆盖不到）。

## 前端

重写 `frontend/src/components/agentre/issues-page.tsx`：

- 删除 `createIssueRows()` / `createBoardColumns()` 及 `issues.samples.*` i18n key（zh-CN + en 同步删，跑 `i18n.test.ts`）。
- 新增 `use-issues` hook（镜像 `use-chat-agents`），通过 wailsjs `IssueList` + `IssueListLabels` 取数。
- **创建/编辑**：shadcn `Dialog`（镜像现有 project/agent 创建弹窗），字段：title（必填）、body（textarea/markdown）、project（从 `ProjectListTree` 选，默认未归属）、labels（从 `IssueListLabels` 多选）。"新建 Issue" 开创建；点行/卡片开编辑。**v1 无独立详情页**。
- **List 视图**：真实行，状态图标由 `agent_status` 驱动（v1 全 idle），`#<id>` 用数据库 id，标签来自 junction。行内动作：关闭/重开、删除。
- **Board 视图**：客户端按 state 分组，v1 只渲染 **待派发(open)** 和 **已关闭(closed)** 两列；进行中/待审批列与卡片上的"派发/重新派发"按钮在 dispatch 增量前隐藏。
- **筛选**：接 **Open/Closed** tab（state + 实时计数）、**标签** 筛选、**最新更新** 排序。v1 隐藏 **author** 和 **分派 Agent** 两个 chip。
- **评论数**：v1 隐藏（评论延后）；头像/指派区显示空态 `+`。
- **空态**：无 issue 时显示空态 + "新建 Issue" CTA。
- 所有新增可见文案走 `react-i18next` `t(...)`，zh-CN + en 同步；表单控件用 shadcn `@/components/ui/*`，禁原生 `<select>`。

## 测试（严格 TDD，Red → Green 逐层）

- `issue_entity_test`：`Check` 校验（空 title / 非法 state / 非法 tone）、`Close/Reopen` 置 `closed_at`。
- `issue_repo_test`（sqlmock，无真库）：Create/Update/Find/List(带 filter)/Delete；label 目录 + junction `SetLabels`/`ListByIssues`。
- `issue_svc_test`（mockgen repo mock，不接 DB）：create-with-labels 编排、list 水合 + open/closed 计数、close/reopen、校验错误路径、delete。
- 前端 `issues-page.test.tsx`（Vitest，mock wailsjs）：list 从绑定渲染、open/closed + 标签筛选、创建流程调 `IssueCreate`、board 分组、空态。
- `i18n.test.ts` 同步更新。
- 迁移 seed（10 个标签）走 bootstrap 迁移路径验证。

每个 behavior spec 覆盖 happy path + 至少一个 boundary/error case。没有失败测试不写实现代码。

## 关键不变量（强制）

- 依赖单向 `internal/app → service → repository → model/entity`；`internal/pkg` 不反向 import service/repo。
- service 只依赖 repository 接口（DIP）+ `Register/accessor` 装配。
- 迁移 append 到末尾，禁改既有迁移。
- 关键流程打日志：`logger.Ctx(ctx)`，message 前缀 `issue_svc.Method:` 小写，动态值走 `zap.Xxx`。
- gitmoji commit；diff 只含 producer + 测试，无 drive-by refactor。

## 后续增量（路线图，非本 spec 范围）

1. **dispatch 增量**：issue → 派发给 agent → 起 chat session → `agent_status` 回写 → 看板补 进行中/待审批/失败 列 + 重新派发；assignee = 被派发 agent。
2. **comments 增量**：评论/讨论 + issue 详情页时间线。
3. **hook 增量**：Sentry/GitHub/邮件 Hook → 自动建 issue（`source` != manual），接已有 `hook_*` 表。
4. **label 管理增量**：标签 CRUD UI。
5. 看板拖拽、author/分派 Agent 筛选、按项目编号等增强。
