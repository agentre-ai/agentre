# 组织架构拖拽排序 + 拖拽手感优化 — 设计文档

- **Date**: 2026-07-02
- **Status**: Design approved, pending implementation plan
- **Scope**: 前端 `components/agentre/org/`(`org-tree.tsx` / `org-list.tsx` / `use-org-data.ts`)+ 后端 `agent` / `department` 的 repo/service/app 三层新增 Reorder

## 背景

组织页有两个视图:

- **树形组织图** `org-tree.tsx`:@dnd-kit 画布,支持拖拽,但拖拽只做 **re-parent**(换上下级 / 换部门),`onMoveAgent` 从不传 sortOrder;而且**子节点按原始数组顺序渲染**,没有按 `sortOrder` 排序(见 `agentChildren` L393、`buildDepartmentNode` L417)。
- **列表** `org-list.tsx`:纯只读表格,只有下拉「排序方式」(hierarchy / name)+ 升降序,**完全没有拖拽**。hierarchy 模式下已经按 `sortOrder` 排同级(L56),但用户无法改这个值。

用户诉求:①「可以拖拽排序」——两个视图都要能拖拽给**同级**排序;②「拖拽感觉卡卡的」——树的拖拽手感要顺。

### 卡顿根因(已定位)

1. `AgentCard` / `DepartmentBanner` 的 className 里带 `transition-all`,拖拽时 dnd-kit 每帧写 inline `style.transform`,CSS transition 会去补间这个 transform → 节点"橡皮筋"跟手滞后。对照 `tab-strip.tsx` / `project-page.tsx` 都用 `useSortable` 托管的 `transform`+`transition`,不会有这问题。
2. 每次落点 `onMoveAgent`/`onMoveDepartment` → `use-org-data.ts` 的 `mutate()` → **整表 `LoadOrg()` 重取** → 整棵树重算布局重渲染 → 落点后闪一下 / reflow。`project-page.tsx` 的做法是**乐观本地更新**、后台持久化。

### 已有可复用资产

- **后端**:`agent_entity` / `department_entity` 都有 `SortOrder` 列;`MoveAgentRequest` / `MoveDepartmentRequest` 都有 `NewSortOrder`(但 `Move` 只写**绝对值**、不重排同级)。
- **后端排序范式**(直接照抄):`project_repo.ReorderSiblings(parentID, orderedIDs)` → `project_svc.Reorder` → `App.ProjectReorder`。一个事务里把同级 `sort_order` 密集重编号 `1..N`,逐条断言 `RowsAffected==1`(见 `internal/repository/project_repo/project.go:117`)。
- **前端拖拽排序范式**(直接照抄):`project-page.tsx:317 handleProjectDragEnd` —— 只在同父分组内排序、算 `orderedIDs`、乐观 `setTree`、后台 `ProjectReorder`、失败回滚 + 报错。卡片用 `useSortable`(`project-page.tsx:675`)。

## 目标

- 树形图:拖拽同级节点到**插入位**即可排序;拖到节点身上仍是 re-parent。拖拽跟手不卡。
- 列表:hierarchy 模式下拖拽行给同级排序。
- 顺序持久化到 `sort_order`,树与列表看到的同级顺序一致。
- 后端复用 project 的 ReorderSiblings 范式,严格 TDD。

## 非目标

- **不做 agent ↔ department 混排**:排序只在同一类型组内(同级 agent 之间、同级 department 之间)。树布局本来就是"先一排 agent、再一排 department",两者 `sort_order` 各自独立命名空间。
- 列表**不做跨父拖拽 / re-parent**(那是树的职责),name 排序模式下不给拖。
- 不改现有 re-parent 拖拽行为、不改 `Move` 的绝对 `NewSortOrder` 语义。
- 不引入 DB migration(`sort_order` 列已存在)。

## 后端设计(§1)

### repository 层

```go
// agent_repo：同级 = 相同 (departmentID, parentAgentID)
ReorderSiblings(ctx context.Context, departmentID, parentAgentID int64, orderedIDs []int64) error

// department_repo：同级 = 相同 parentID
ReorderSiblings(ctx context.Context, parentID int64, orderedIDs []int64) error
```

实现照抄 `project_repo.ReorderSiblings`:一个 `Transaction` 内 `for idx, id := range orderedIDs`,`UPDATE ... SET sort_order = idx+1, updatetime = ? WHERE id=? AND <同级键> AND status = ACTIVE`;`RowsAffected != 1` 直接返回 error(拒绝跨组/不存在的 id 混进来)。原生 SQL,与 project 一致。

### service 层

```go
// agent_svc
type ReorderAgentsRequest struct {
    DepartmentID  int64   `json:"departmentID"`
    ParentAgentID int64   `json:"parentAgentID"`
    OrderedIDs    []int64 `json:"orderedIDs"`
}
Reorder(ctx context.Context, req *ReorderAgentsRequest) error

// department_svc
type ReorderDepartmentsRequest struct {
    ParentID   int64   `json:"parentID"`
    OrderedIDs []int64 `json:"orderedIDs"`
}
Reorder(ctx context.Context, req *ReorderDepartmentsRequest) error
```

service 只做参数透传 + 调 repo(校验放 repo 的 `RowsAffected==1` 兜底即可,参考 `project_svc.Reorder`)。System/CEO agent 不会出现在任何同级组的 orderedIDs 里(它是根),天然不受影响;若混入,repo 的行数断言会拦。

### app(Wails binding)层

在组织相关 binding 文件(`internal/app/department.go` 或 agent 对应文件,与 `LoadOrg`/`MoveAgent` 同处)新增:

```go
func (a *App) ReorderAgents(req *agent_svc.ReorderAgentsRequest) error
func (a *App) ReorderDepartments(req *department_svc.ReorderDepartmentsRequest) error
```

只做 parse → `svc.Reorder`。`make generate` 刷新 `frontend/wailsjs` 绑定。

## 树形图设计(§2)

### 2.1 同级按 sortOrder 稳定排序

`agentChildren`(L393)、`buildDepartmentNode` 里的 `childAgents` / `childDepts`(L417-420)、`buildLayoutRoots` 的 `topAgents` / `topDepartments`(L438-448)统一 `.sort()`:主键 `sortOrder ?? 0`,次键 `id`。让树呈现的顺序 = 持久化顺序,也让插入位下标可预测。

### 2.2 插入位 drop-zone

`buildOrgTreeLayout` 增补输出:每个父节点、每个类型组(agent 组 / dept 组)其**有序子节点的 key + 几何**。据此在 `TreeCanvas` 里渲染插入 droppable:

- 位置:同类型相邻兄弟之间的空档中点、以及组首之前 / 组尾之后;用布局已知的 `x/width/y` 算出细长竖条区域。
- id 编码 `insert-<parentKey>-<kind>-<index>`(kind ∈ agent|dept)。
- 拖拽中:只有「被拖节点的类型 == 该 zone 的 kind」且「被拖节点当前父 == 该 zone 的父」时,zone 才是合法目标;`over` 命中时画一条高亮竖直插入线。
- 落点:`onDragEnd` 按 `over.id` 分流 —— `insert-*` → reorder;`agent-*` / `dept-*` 节点 → 走现有 `getOrgDragIntent`(re-parent)。两种意图互不冲突,对应用户选的「插入位高亮」方案。

reorder 计算:取该组当前有序 id 列表,移除被拖 id,在目标 index 处插入,得到 `orderedIDs` → 调用对应 Reorder。

> 实现提示:zone 的合法性/高亮判断需要 `DragOverlay` 之外的 active 信息。可在 `DndContext` 上加 `onDragStart` 记录 `activeKind/activeParentKey`,zone 组件据此决定是否 `disabled` 及高亮样式;或用 `useDndContext().active`。plan 阶段定细节。

### 2.3 手感修复(核心「不卡」)

- 拖拽时关掉补间:`AgentCard` / `DepartmentBanner` 的 className 加 `drag.isDragging && "transition-none"`(或把 `transition-all` 收窄成只补 `box-shadow`/`border` 等非 transform 属性)。这是「卡卡」的正解,低风险。
- 可选增强(plan 阶段权衡,非必须):`DragOverlay` 承载被拖节点,原位留占位。先做 `transition-none`,不够顺再上 overlay。

### 2.4 乐观更新

落点后先本地 patch,再持久化,再由 reload 兜底对账,消除落点闪烁:

- reorder:本地把该组 agents/departments 的 `sortOrder` 按新序改写,`setState` 立即生效。
- re-parent(现有 Move):本地改被拖项的 `departmentId/parentAgentId/sortOrder`。
- 失败回滚 + 报错(沿用 `use-org-data.ts` 的 error 通道)。

`use-org-data.ts` 需要新增:`reorderAgents` / `reorderDepartments`(调新 binding),以及一个「乐观 patch 本地 state 后再 mutate」的路径。倾向在 hook 内提供 `applyOptimistic(patchFn)`,让组件先算好新 `agents/departments` 再触发持久化;或让 reorder/move 内部先 patch 再 `mutate`。plan 阶段定 API 形状,保持 hook 单一职责。

## 列表设计(§3)

- 行改用 `useSortable`(照抄 `tab-strip.tsx` / `project-page.tsx` 的 `SortableProjectCard`):`transform`/`transition` 托管、`isDragging` 半透明。
- **仅 hierarchy 模式启用拖拽**;name 模式 `disabled`(顺序是派生的,拖了没意义)。
- 只允许**同一有效父级(effectiveParent)**的行之间排序;`onDragEnd` 若 active/over 的 effectiveParent 不同则忽略(照抄 `project-page` 的 `activeGroup.parentID !== overGroup.parentID` 守卫)。
- 持久化:把重排后的 orderedIDs 按**原始 placement 组**`(departmentId, parentAgentId)`分桶,每桶按其相对顺序调用一次 `ReorderAgents`。这样列表(按 effectiveParent 展示)与树(按 raw placement 展示)看到的相对顺序一致 —— 密集重编号得到的是单调子序列,不会打架。
- 乐观 + 失败回滚,`reorderFailed` 文案走 i18n。

### 边界:effectiveParent ≠ raw placement

列表按 `effectiveParent`(reporting.ts:显式 parent_agent_id ▸ 部门 leader 链 ▸ CEO 兜底)分组,树/后端按 raw `(departmentId, parentAgentId)` 分组。绝大多数情况两者同集合(同部门 agent 都 report 给该部门 leader)。当一个 effectiveParent 块跨多个 raw 组时,§3 的「分桶重排」保证各 raw 组内相对序被写对,列表整体顺序也稳定。**已知取舍**:跨 raw 组的两个 agent 在列表里的相对位置由各自 `sort_order` 的密集值比较决定,极端交叉场景下与用户拖动的绝对位置可能有细微偏差 —— 可接受,不额外处理。

## 测试 / i18n(§4)

### 后端(严格 TDD,先红后绿)

| 层 | 文件 | 用例 |
|---|---|---|
| repo | `agent_repo/agent_test.go`(sqlmock) | `ReorderSiblings` 按序写 `sort_order=1..N`、跨组 id 触发 `RowsAffected!=1` 报错 |
| repo | `department_repo/department_test.go`(sqlmock) | 同上,按 `parentID` |
| svc | `agent_svc/agent_test.go`(mockgen) | `Reorder` 透传到 repo;mock 期望参数正确 |
| svc | `department_svc/department_test.go`(mockgen) | 同上 |

sqlmock 期望要匹配 `UPDATE ... SET sort_order = ?, updatetime = ? WHERE id = ? AND ... AND status = ?` 的原生 SQL(参考 project_repo 的既有测试写法)。

### 前端(Vitest)

- 树:reorder 意图 helper(给定组有序 id + 被拖 id + 目标 index → orderedIDs)纯函数单测;`onDragEnd` 对 `insert-*` vs 节点 id 的分流;插入位几何计算(gap 中点)可提纯函数测。
- 列表:`onDragEnd` 的同 effectiveParent 守卫、分桶持久化;name 模式不给拖。
- 乐观更新:reorder 后本地 `agents/departments` 顺序即时变化、失败回滚。
- **注意**:`org-tree`/`org-list` 间接 import wailsjs runtime,渲染型测试需 per-file `vi.mock`(见记忆 `frontend_wails_runtime_test_mock`),跑**全量** vitest 而非 focused,避免漏改。

### i18n

新增可见文案(如 reorder 失败 toast)同时写 `zh-CN/common.json` 与 `en/common.json`;`i18next/no-literal-string` 会拦硬编码中文。

## 实现顺序(给后续 plan 用)

1. **后端 repo**:两个 `ReorderSiblings` + sqlmock 测(先红)。
2. **后端 svc**:两个 `Reorder` + request 类型 + mockgen 测。`make mock` 刷新。
3. **后端 app**:两个 binding;`make generate` 刷 wailsjs;`make test-backend` 绿。
4. **树 §2.1 + §2.3**:同级按 sortOrder 排序 + `transition-none` 手感修复(不含 reorder 也能独立验证"不卡"了)。
5. **树 §2.2 + §2.4**:插入位 drop-zone + 乐观更新 + reorder 落库。
6. **列表 §3**:`useSortable` 行 + hierarchy 门控 + 分桶持久化 + 乐观。
7. `make test-backend` + `make lint` + 全量 `pnpm test` 看**真 exit code**,收尾。

## 风险 / 边界

- **插入位几何在缩放/平移下的命中**:树有 `zoom`/`pan` transform,droppable 的命中矩形由 dnd-kit 按真实 DOM 计算,scale 下 dnd-kit 会用变换后的 rect,一般 OK;但要真机验证缩放态下插入线位置准确。plan 阶段留一个手验点。
- **PointerSensor distance 与点击/折叠按钮**:树现有 `activationConstraint: { distance: 6 }`,部门卡上的折叠按钮已 `stopPropagation`;新增 zone 不接管节点的 listeners,风险低,但要确认拖拽 6px 阈值不误吞节点的 onClick 选中。
- **乐观 patch 与 reload 竞态**:`use-org-data.ts` 的 `mutate` 在 in-flight 归零后 reload。乐观 patch 要保证 reload 回来的顺序 == 已 patch 的顺序(否则又闪)。密集重编号后两者应一致;若不一致说明持久化没落对,应暴露而非静默。
- **列表 effectiveParent 跨 raw 组**:见 §3 边界,已知取舍,不额外处理。
