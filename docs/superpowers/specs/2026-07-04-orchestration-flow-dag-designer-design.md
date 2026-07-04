# 编排流程 DAG 设计器 — 设计文档

> 状态：设计已定，待评审 → writing-plans
> 日期：2026-07-04
> 相关画布（`agentre.pen`）：
> - `编排流程 — DAG vs Outline`（概念对比图）
> - `编排 — 新建 Run（更新·DAG 预览）`（Phase 1 UI）
> - `编排 — DAG 流程设计器（表单+实时预览）`（Phase 2 UI）

## 1. 背景与目标

起点是一个小需求：新建编排 Run 时**删掉「从零开始」选项，增加一个默认流程、默认选中、填好提示词**（引导 Leader 先用工具查成员，再按目标拆解、分派、验证、打回、收口）。

深挖后确定把「编排流程」从**扁平文本/列表**升级为**有向无环图（DAG）**：只有 DAG 能表达编排真实的结构——**并行分叉（fan-out）、汇合（fan-in）、验证不过的打回环（back-edge）**，扁平列表会把它们压成假的线性序列。

最终目标：
1. 新建 Run 弹窗去掉「从零开始」，默认落到「从流程库」并预选一条内置**默认流程**，其正文真正注入 Leader。
2. 流程以 **DAG 为唯一真源**，注入 Leader 的散文提示词是它的**确定性投影**。
3. 提供一个**表单式 DAG 设计器**（非自由画布拖拽）：左侧表单编辑节点/边，右侧实时渲染 DAG + 实时预览投影出的提示词。

## 2. 现状与一个必须先修的缺口

- **工具集（审计确认，8+1 个）**：编排 `orchestrate` = `agent_list / dispatch / ask / reply / send / finish / report / read`；另有独立能力 `subagent` = `agent_list / agent_call`（成员可自己再委派）。其中 **`send` 就是「打回/返工」的正主**（对原任务续发，不新增节点）。
- **注入现状**：`orch_svc/turn.go` 的 `BuildTurnExtras` 只注入 `orchGuidance`（框架语，恒定）+ `run.FlowContent`（仅当根任务且非空）。`workflow.Tags/Outline` **被明确设计成只展示不注入**（`turn_test.go` 有专门断言守护）。
- **`workflows` 表**：`id, name, content, tags(JSON), outline(JSON), status, createtime, updatetime`。**无 `graph`、无 `is_default`**。`List` 按 `updatetime DESC` 排序。
- **🔴 缺口（必须先修）**：**库模式当前注入为空**。前端库模式只传 `flowId`、`flowContent` 传空；后端 `CreateRun` 原样存；`turn.go` 只读 `run.FlowContent` → 库流程的正文**从没被注入过**。没人把 `workflow.Content → run.FlowContent` 快照。**默认流程若不修这条，就是纯摆设。**

## 3. 架构决策（已锁）

| 决策 | 结论 |
|---|---|
| 约束力 | **软约束**：图/提示词是**指引**，Leader 可遵可偏（保留「自主 Leader」哲学）。**不做**硬门控（越阶/未验证拒绝 finish）——列入 Phase 3 未来项。 |
| 真源 | **DAG（graph JSON）是唯一真源**；散文提示词是它的**确定性投影**；outline 也是派生。 |
| 投影落点 | 在**后端**做 `graph → prose` 纯函数投影，Create/Update 时写入 `workflows.content`（content 变成 graph 的派生缓存 + 人可读 + 被注入的正文）。 |
| 注入 | `CreateRun` 时把 `workflow.content` **快照**进 `run.FlowContent`（修上面的缺口）。adhoc/无图的老流程：content 即用户散文，照旧注入。 |
| 设计器形态 | **表单式 + 实时 DAG 预览 + 实时提示词预览**。无自由画布拖拽、**无新依赖**（`@xyflow` 等一律不引），复用仓库既有自定义渲染风格。 |
| 语言 | 默认流程 seed 内容（name/content/tags/graph 标签）用 **English**；属于 DB 数据（同 CEO/label seed），不进 i18n。 |

## 4. 数据模型

### 4.1 `workflows` 新增两列

- `graph TEXT NOT NULL DEFAULT ''` —— 流程 DAG 的 JSON 真源（空 = adhoc/无图流程）。
- `is_default INTEGER NOT NULL DEFAULT 0` —— 内置默认流程标记（仿 agents 表 `system_badge='DEFAULT'`）。编辑/软删**不改**此列；删了默认流程 → 前端回退到不预选。

### 4.2 graph JSON schema

```jsonc
{
  "version": 1,
  "nodes": [
    { "id": "see-members", "label": "See members", "kind": "leader" },
    { "id": "frontend",    "label": "Frontend",    "kind": "task",
      "brief": "Build the UI per the spec. Acceptance: renders, states covered." }
  ],
  "edges": [
    { "from": "see-members", "to": "frontend" },
    { "from": "verify", "to": "frontend", "kind": "bounce" }
  ]
}
```

- `node.kind ∈ {"task","leader"}` —— **二元**：`task`=委派给某抽象角色（含 `brief`），`leader`=Leader 自己做的步骤（plan/join/decide/finish，无 brief）。
- `node.label` —— **既是步骤名，也是抽象角色**（模板不绑真 agent，Leader 运行时用 `agent_list` 映射到真成员，保证跨团队复用）。
- `node.brief` —— 可选，仅 `task`：任务说明 + 验收标准，投影进 dispatch brief。
- `node.sharedFiles?: bool` —— 可选：并行时是否改同一片文件（投影出 `isolate=true` 提示）。
- `edge.kind ∈ {"sequence"(默认省略), "bounce"}`。

**三个从结构推断、不建独立字段的概念：**
- **parallel** = 共享同一上游、彼此无 sequence 依赖的兄弟节点。
- **verify** = 作为某条 `bounce` 边 `from` 的 `task` 节点。
- **finish/收口** = 无出向 sequence 边的 sink 节点。

## 5. graph → prose 投影（确定性纯函数）

`ProjectGraph(graph) -> (content string, outline []string)`，后端纯函数，golden 测试：

1. 头部固定：`# <workflow name>` + 一行 `You are the Leader. Every result returns to you; you decide the next move.`
2. 按 sequence 边做**最长路径分层**（拓扑）；layer 顺序即步骤顺序。
3. 逐 layer 编号出步：
   - 单节点 · `leader`：`N. <label>`（有 brief 追加 ` — <brief>`）。
   - 单节点 · `task`：`N. Dispatch to the <label> role: <brief>`。
   - 多节点（并行组）：`N. In parallel:` + 每节点一条 `- <label> — <brief>`；若任一 `sharedFiles` → 追加 `(use isolate=true if they touch the same files)`。
   - sink 节点：措辞为 `N. <label> — finish with a summary @user.`
4. `bounce` 边：给其 `from` 节点那一步追加 ` On fail → send back to <target label> (no new node).`
5. `outline` = 各 layer 的代表 label 列表（派生，供紧凑展示；主预览是 DAG 图）。

前端**实时预览**不重写 TS 投影（防漂移），改调后端 binding `WorkflowPreviewGraph(graphJSON) -> {content, outline}`（编辑防抖后调用）。**投影只有一份实现，在后端。**

## 6. 默认流程（English seed）

`is_default=1`、`name="Default Orchestration Flow"`、`tags=["Default","General"]`。graph 即画布里那张（通用 Feature 型流程）：

`See members(leader) → Break down(leader) → [Frontend ∥ Backend](task) → Integrate(leader) → Verify(task) → Wrap up(leader/sink)`，外加 `Verify --bounce--> Frontend`。

投影出的 content（示意，最终以投影函数产物为准）：

```
# Default Orchestration Flow
You are the Leader. Every result returns to you; you decide the next move.

1. See members — call agent_list; don't assume the roster.
2. Break down — split the goal into independently deliverable subtasks.
3. In parallel:
   - Frontend — dispatch: build the UI per the spec. Acceptance: ...
   - Backend  — dispatch: build the API per the spec. Acceptance: ...
   (use isolate=true if they touch the same files)
4. Integrate — join the branch outputs.
5. Verify — dispatch review / tests. Acceptance: all pass, no regressions.
   On fail → send back to Frontend (no new node).
6. Wrap up — finish with a summary @user.
```

> **「常用例子」怎么办**：不塞进一条流程的散文（违背 graph=真源）。改为**流程库里多播几条 seed 流程**（Feature / Research & Design / Bugfix，各一张小图）来充当例子；默认（`is_default`）那条 = 通用 Feature 型。MVP 至少 seed 默认这一条，另两条为轻量后续。

## 7. 后端改动

1. **迁移** `202607040001_workflow_graph_default.go`（追加到 `migrationList()` 末尾，原生 SQL）：`ALTER TABLE workflows ADD COLUMN graph ...` + `ADD COLUMN is_default ...` + `INSERT` 默认流程一行（`is_default=1`，content 为投影产物、graph 为 JSON）。
2. **entity** `workflow_entity.Workflow`：加 `Graph string`、`IsDefault int`（+ 可能 `IsBuiltinDefault()` 充血方法）。
3. **DTO** `workflow_svc.WorkflowItem`：加 `Graph string`、`IsDefault bool`；`CreateWorkflowRequest/UpdateWorkflowRequest` 加 `Graph`。
4. **workflow_svc**：`ProjectGraph` 纯函数（新 `projection.go`）；`Create/Update` 收到非空 `graph` 时 → 投影覆写 `content` 与 `outline`（graph 为真源）。
5. **binding** `WorkflowPreviewGraph(graphJSON) -> {content, outline}`（`app/workflow.go`），供设计器实时预览。
6. **orch_svc `CreateRun` 修缺口**：`req.FlowID>0 && req.FlowContent==""` 时，经一个**窄接口** `workflowReader.Find`（DIP，`Register` 注入）读 `workflow.Content` 快照进 `run.FlowContent`。（`turn.go` 注入路径不变。）

## 8. 前端改动

### Phase 1 —— 新建 Run 弹窗（`run-new-dialog.tsx`）
- `FlowMode`：`none|library|adhoc` → **`library|adhoc`**；删 `none` 段与 `Sparkles` 图标；初始/重置 `flowMode="library"`。
- `WorkflowList` 返回后，`flowId` **预选 `isDefault` 那条**（回退：不预选）。`WorkflowOption` 加 `isDefault`、`graph`。
- 旧的扁平 outline 面包屑 → 换成**只读 mini-DAG 预览**组件 `FlowGraphView`（读 graph JSON，紧凑横向布局）。
- i18n：删 `flowNone`（zh+en）；加 `flowPreview`（"流程预览（DAG）"）。

### Phase 2 —— DAG 设计器（`WorkflowManagerDialog` 内新增编辑态）
- 复用 `FlowGraphView`（只读渲染），外面套**表单编辑器**：
  - 左：节点列表（加/删/选/拖排序，`@dnd-kit` 已在库）+ 选中节点属性表单 = **名称 · 类型(委派/Leader 二元) · 任务说明+验收(仅委派) · 上游依赖 · fail→打回目标**。
  - 右上：实时 DAG 预览（选中态与表单联动）。右下：实时提示词预览（调 `WorkflowPreviewGraph`）。
- 保存 → `WorkflowUpdate({..., graph})`；后端投影覆写 content/outline。

### 共享组件
- `FlowGraphView`（Phase 1 交付、Phase 2 复用）：入参 graph JSON → **轻量分层 DAG 布局**（最长路径分层 + 层内居中，仿 `structure-graph` 的自定义 div/SVG 风格，无新依赖），渲染节点卡（label + 二元 kind 徽章）、sequence 边（靛蓝）、bounce 边（琥珀）。流程规模小（≤~15 节点），无需 dagre/elk。

## 9. 分期（每期可评审、可合入）

- **Phase 1**：迁移 + entity/DTO + `ProjectGraph` + seed 默认流程 + `CreateRun` 快照修复 + `FlowGraphView` 只读组件 + 弹窗去 none/默认预选/mini-DAG。**交付原始需求端到端**（默认流程真注入 + DAG 预览），暂无编辑器。
- **Phase 2**：表单式 DAG 设计器（编辑 + 实时双预览）+ `WorkflowPreviewGraph` binding。**设计器本体**。
- **Phase 3（未来）**：plan-DAG × 运行时 `structure-graph` 叠加（计划 vs 实际）；可选硬门控。

## 10. 测试 / TDD 检查点

- **注入缺口回归**（先红）：建 Run 指向一条 content=X 的库流程 → 断言 `run.FlowContent==X` 且 `BuildTurnExtras` 注入含 X。**当前应失败**（证明缺口），再修 `CreateRun` 快照。
- **`ProjectGraph` golden 测试**：默认流程 graph → 预期 content/outline（覆盖并行组、bounce 追加、sink→finish 措辞）。
- **迁移测试**：seed 行存在、`is_default=1`、graph 可解析。
- **workflow_svc**：Create/Update 传 graph → content/outline 被投影覆写；DTO 带 `isDefault/graph`。
- **turn_test**：维持「tags/outline 不注入」不变量（只 content 注入）。
- **前端 vitest**：弹窗默认 `library`、预选 `isDefault`、无 `none` 段、mini-DAG 渲染；`FlowGraphView` 布局快照。

## 11. Non-goals / 已决

- **不做**硬门控 / 状态机（Phase 3 才议）。
- 设计器**不做**自由画布拖拽连线。
- 节点**不绑真 agent**（label=抽象角色）；**不设** `工具` 与 `角色/成员` 字段（工具是 Leader 机械选择，角色并入 label）。
- 不引入图库依赖。
- 老 adhoc（无 graph）流程继续按散文 content 注入，兼容不破。
