# 新建编排弹窗 — 团队按部门选择（双栏选择器）设计

日期：2026-07-03 · 仓库：`agentre/`（Wails 桌面端）· 分支：`develop/wyz`

## 目标

优化「新建编排」弹窗（`run-new-dialog.tsx`）里的**「可参与团队」选择器**，让用户能**按部门**挑选参与
agent，并整体提升弹窗 UI/UX。当前「可参与团队」是一整片扁平的 agent 药丸 chips（`ListChatAgents`
返回的全部 agent 平铺），agent 一多就难找、无法按组织结构快速取用。

方向已确认为 **方案 B — 左右双栏选择器**（Pencil 三方案对比后用户选定）。

## 范围决策（已确认）

- **只改「可参与团队」选择器**。`目标 / Leader / 编排流程` 三个字段保持不变（Leader 仍是扁平下拉）。
- **交互模型 = 双栏**：左侧部门列表（导航/筛选），右侧该部门的 agent 勾选，底部实时「本次团队」汇总。
- **空 = 不限制**：沿用现状语义——`allowedAgentIds` 为空表示不限制可参与 agent。汇总条为空即代表全员可参与。
- **Leader 不自动并入团队**：与现状一致，`leaderAgentId` 与 `allowedAgentIds` 相互独立。
- **无需后端改动**（见「数据来源」）。

## 现状

- 前端：`frontend/src/components/agentre/orchestration/run-new-dialog.tsx`
  - 表单状态：`goal / leaderId / flowMode / flowId / flowContent / allowedAgentIds`。
  - 团队区（第 360–407 行）：`agents.map` 平铺身份色药丸 chips，`toggleAllowed` 增删 id。
  - `submit()` 把 `allowedAgentIds: number[]` 传给 `RunCreate`（团队区只产出这个数组）。
  - 数据来自 `ListChatAgents()` → `ChatAgentItem`（含 `id/name/avatarColor/defaultPermissionMode`）。
- 后端：`ChatAgentItem`（`internal/service/chat_svc/types.go`）**不含**部门字段。
- 部门是**真实数据**：`LoadOrg()`（`app.App`）返回 `department_svc` 的
  - `departments: DepartmentItem[]`（`id/name/icon/accentColor/parentId/leadAgentId/leadAgentName/sortOrder/directAgentCount/subdepartmentCount/memberCount`）
  - `agents: AgentItem[]`（含 `departmentId/departmentName/avatarColor/sortOrder` 等）
  - 现由组织架构页 `org/use-org-data.ts` 消费。
- **并发改动提示**：同日 spec `2026-07-03-orchestration-project-selection-design.md` 会在同一弹窗
  「编排流程」之后**新增一个「项目」下拉**。两处改动相互独立、可叠加：最终字段顺序为
  `目标 → Leader → 编排流程 →（项目）→ 可参与团队`。实现时以对方已落地为准，避免 import/JSX 冲突。

## 设计：双栏部门选择器

替换团队区（`run-new-dialog.tsx` 第 360–407 行那段）为一个独立组件
`TeamDepartmentPicker`，弹窗只负责把它接进表单（`value={allowedAgentIds}` / `onChange`）。

### 布局（对齐 Pencil 方案 B，画布 screen `aVYSk`）

```
可参与团队                                      [已选 N 人]   ← 标签行 + 计数 pill
┌───────────────────────────────────────────────────────┐
│ 🔍 搜索 agent…                                     ⌘K  │  ← 顶部全局搜索（跨部门）
├───────────────────────────────────────────────────────┤
│ 部门            │  研发部 · 4 名成员            [全选] ☑ │  ← 右栏头：当前部门 + 全选
│ ─────────────── │ ───────────────────────────────────── │
│ 👥 全部 Agent 16│  ☑ ● 王一之            Claude Code    │
│▎💻 研发部     4 │  ☑ ● 阿则              Codex          │  ← 左栏选中项高亮(primary-soft
│ 🎨 产品设计部 2 │  ☐ ● 小满              Claude Code    │     + 左强调条)；右栏 agent 行
│ 🧪 测试部     3 │  ☑ ● 沉舟              Claude Code    │     = 复选框 + 身份色点 + 名 + 后端 tag
│ ◌ 未分组      2 │                                       │
├───────────────────────────────────────────────────────┤
│ 本次团队  ●●●●●                              共 N 人    │  ← 汇总条：跨部门已选头像 + 总数
└───────────────────────────────────────────────────────┘
留空 = 不限制可参与 Agent                                    ← footer hint 保留
```

### 顶部搜索（全局）

- 选择器框顶部固定一条**全局搜索**（放大镜图标 + 「搜索 agent…」占位 + `⌘K` 聚焦提示），横跨双栏。
- 输入时**右栏切换为跨部门的扁平搜索结果**（匹配名称的可参与 agent，不再受左栏当前部门约束）；
  左栏可弱化/标注「搜索结果」态。清空输入即恢复默认双栏视图。
- 搜索只影响**展示与定位**，不改变已选集合；结果里的勾选/取消照常写回全局选择态。

### 左栏（部门导航）

- 顶部固定伪项 **「全部 Agent」**（图标 `users`）：右栏展示全部可参与 agent（不分部门）。
- 之后按 `sortOrder` 列出**含 ≥1 名可参与 agent 的部门**：部门色块图标（`icon` + `accentColor`）+ 名称 + 该部门可参与 agent 数。
- 末尾伪项 **「未分组」**（图标 `circle-dashed`，仅当存在 `departmentId` 无法解析到部门的可参与 agent 时出现）。
- 选中项样式：`primary-soft` 底 + 左 2px `primary` 强调条 + 文案 `primary-text`。左栏只做**导航/筛选**，不承载选择态。
- 一级扁平即可（`parentId` 层级本期不做嵌套缩进；子部门先与其它部门同级平铺，`sortOrder` 排序）。

### 右栏（agent 勾选）

- 头行：当前部门名 + 「M 名成员」+ 右侧**「全选」**（勾选/半选/未选三态，作用于**当前部门**当前展示的 agent）。
- agent 行：`复选框 + 身份色圆点(首字) + 名称 + 后端 tag`（`Claude Code`/`Codex`，来自 `backendType`）。已选行 `primary-soft` 底。
- 选择态是**全局**的（切换左栏部门不丢已选）；「全部 Agent」视图下勾选/取消同步影响其部门归属视图。

### 底部汇总条

- 左「本次团队」+ 跨部门**已选头像堆**（身份色点，超出显示 `+K`）+ 右「共 N 人」。
- 汇总为 0 时整条隐藏（或显示占位「未指定，默认全员可参与」）；标签行计数 pill 与之联动。

## 数据来源（无需改后端）

弹窗打开时**并发**拉两份现有数据，在前端做纯函数 join：

- `ListChatAgents()` → **决定「哪些 agent 可参与」**（沿用现状的资格集合，语义不变）。
- `LoadOrg()` → 提供**部门元数据**（`icon/accentColor/name/sortOrder`）与 **agent→部门映射**（`AgentItem.id → departmentId/departmentName`）。

纯函数 `groupAgentsByDepartment(chatAgents, orgDepartments, orgAgents)` → 结构：

```ts
type PickerDept = { id: number; name: string; icon: string; accentColor: string; agents: PickerAgent[] };
type PickerModel = { all: PickerAgent[]; departments: PickerDept[]; ungrouped: PickerAgent[] };
```

- 只保留在 `ListChatAgents` 里的 agent（资格集合）；用 orgAgents 的 `departmentId` 归组；
  解析不到部门的进 `ungrouped`；`departments` 仅含非空组，按 `sortOrder` 排序。
- 该函数放在 `orchestration/team-picker-data.ts`，**纯函数、单测覆盖**（空列表 / 无部门 / agent 不在资格集合 / 部门无成员 等边界）。

> 可选优化（非本期必须）：给 `ChatAgentItem` 增补 `departmentId/departmentName`，省掉第二次
> `LoadOrg` 调用。默认走「两次调用 + 前端 join」，改动最小、零后端风险；若后续觉得多一次
> `LoadOrg` 太重再做该优化。

## 组件拆分

- 新增 `orchestration/team-department-picker.tsx`：受控组件，`props = { agents(chat), org, value, onChange }`，
  内含左右双栏 + 汇总条；无业务副作用（不自己拉数据，数据由弹窗注入，便于测试）。
- 新增 `orchestration/team-picker-data.ts`：`groupAgentsByDepartment` 纯函数。
- `run-new-dialog.tsx`：`useEffect` 里在 `ListChatAgents` 基础上并发 `LoadOrg()`；把结果传给
  `TeamDepartmentPicker`；删除原扁平团队区；`allowedAgentIds` 状态与 `submit()` **不变**。

## i18n

新增键（`zh-CN` + `en` 同步，`orchestration.new.*` 命名空间）：
`teamAllAgents`（全部 Agent）、`teamUngrouped`（未分组）、`teamSelectAll`（全选）、
`teamDeptMembers`（`{{count}} 名成员`）、`teamSummary`（本次团队）、`teamTotal`（`共 {{count}} 人`）、
`teamSearchPlaceholder`（搜索 agent…）、`teamSearchEmpty`（无匹配 agent）。沿用现有 `team / teamSelected / teamHint`。

## 测试计划（TDD）

1. `team-picker-data.test.ts`：`groupAgentsByDepartment` 边界（先红后绿）。
2. `team-department-picker.test.tsx`：渲染分组、点左栏切部门、勾选/全选、汇总计数、**搜索过滤（跨部门扁平结果 + 无匹配空态）**、空态。
3. `run-new-dialog.test.tsx`（已存在）：更新为新选择器仍能产出 `allowedAgentIds` 并提交；
   `ListChatAgents`/`LoadOrg` 的 wails mock。
4. `i18n.test.ts`：新键 zh/en 覆盖。
5. 收尾跑 `make test-backend`（应无后端 diff）+ `make lint` + 全量 `pnpm test`。

## 非目标

- 不改 Leader、目标、编排流程；不动后端 `RunCreate` / `ChatAgentItem`（除可选优化）。
- 不做部门多级嵌套缩进、不做部门内拖拽、不做「按部门为单位」整队语义（那是方案 C）。
- 不引入 HTTP 风格 app API；仅走既有 wails 绑定。

## Pencil 参考

`agentre.pen` 顶层：**`方案 B 双栏 = aVYSk`（本设计，含顶部全局搜索）**。
对比稿方案 A / C 已按用户要求从画布删除，落地以 B 为准。
