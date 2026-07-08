# 砍掉编排流程的「步骤 / DAG」设计

- 日期:2026-07-08
- 分支:develop/wyz
- 状态:已批准,待写实现计划

## 背景

编排「流程库」当前把一条流程建模为「DAG 图 + 有序步骤(outline)+ Go text/template 模板」。经代码走查确认(见 `internal/service/workflow_svc/`、`internal/service/orch_svc/`):

- **DAG / 步骤在运行时没有任何硬约束**。没有「前置未完成阻断下游 dispatch」的门控,没有按拓扑自动派活的调度器;派活 100% 由 Leader 通过 MCP 工具自主驱动。
- DAG(`workflows.graph`)只被投影成散文(`ProjectGraph`)喂进提示词,或经模板渲染;原始 graph 快照进 `run.FlowGraph` 仅供前端可视化 overlay。
- dispatch 的 `node` 参数、`tasks.node_ref` 只用于进度可视化打标——工具描述里明写「不影响派活」。

结论:步骤/DAG 是「给 Leader 读的 SOP + 给人看的进度图」,不是执行引擎。本次将其全部移除,流程库退化为纯文本提示词库。

## 目标与边界

流程库从「DAG + 步骤 + 模板」退化为**纯文本提示词库**:一条流程 = `name + content(Markdown 正文) + tags`。运行时行为不变——正文经 `run.FlowContent` 注入 Leader(现状保留)。

### 保留(不动)

- `workflows.content` / `name` / `tags` / `status`
- `run.FlowID` / `run.FlowContent`(散文注入)
- `workflow_svc.FlowContentByID`
- `orch_svc.turn.go` 的 `BuildTurnExtras`(把 FlowContent 注入 Leader system-prompt)
- `StructureGraph`(实时任务执行树,建自 tasks/subagents,与 flow DAG 无关)
- `TaskBoard`(去掉节点筛选后保留)
- `run-new-dialog` 选流程
- `orchGuidance` 编排框架语

### 删除(一切「步骤/DAG」机制)

见下方后端/前端/数据库清单。

## 后端改动

| 位置 | 动作 |
|---|---|
| `internal/model/entity/workflow_entity/workflow.go` | 删字段 `Graph` `Template` `Outline` |
| `internal/model/entity/orch_entity/run.go` | 删字段 `FlowGraph` |
| `internal/model/entity/orch_entity/task.go` | 删字段 `NodeRef` |
| `internal/service/workflow_svc/projection.go` | 整文件删(ProjectGraph / FlowNode / FlowEdge / FlowGraph / ParseFlowGraph) |
| `internal/service/workflow_svc/render.go` | 整文件删(RenderTemplate / DefaultTemplate({{DAGPrompt}}) / RenderWorkflowContent) |
| `internal/service/workflow_svc/workflow.go` | 删 `applyTemplate`;Create/Update 直存 `content`;`toItem` / 请求体去 graph/template/outline |
| `internal/service/workflow_svc/types.go` | `WorkflowItem` / 请求响应结构去 graph/template/outline |
| `internal/service/workflow_svc/deps.go` + `orch_svc/deps.go` | 删 `FlowGraphByID` 接口 |
| `internal/app/orch_adapter.go` | 删 `FlowGraphByID` 适配实现 |
| `internal/service/orch_svc/create.go` | 删 graph 快照分支(只留 FlowContent 快照) |
| `internal/service/orch_svc/mcp.go` | dispatch 删 `node` 入参解析 + 工具 schema 里 `node` 描述;`Dispatch` 调用去 node 实参 |
| `internal/service/orch_svc/orch.go`(或定义处)`Dispatch` 签名 | 去掉 `node string` 形参 |
| `internal/service/orch_svc/status.go` | status 响应去 `Node`(来自 `t.NodeRef`) |
| `internal/app/workflow.go` | 删 `WorkflowPreviewGraph` 绑定 + `WorkflowPreviewRequest` / `WorkflowPreviewResponse` |
| `internal/app/orch.go` DTO | 去 `FlowGraph`、task DTO 的 `NodeRef` |

> `make generate` 刷新 `frontend/wailsjs/` 绑定(删了 Preview 绑定与 DTO 字段后)。

## 数据库(追加迁移,不改旧迁移)

新迁移 `202607080002_drop_flow_dag_steps`(实现时取 `migrationList()` 末尾的下一个可用 ID,防并发会话撞号;单文件多 DDL;SQLite `DROP COLUMN` 已被 `202607080001` 用过,可用):

- `workflows`:DROP `graph`、`template`、`outline`
- `orchestration_runs`:DROP `flow_graph`
- `tasks`:DROP `node_ref`

追加到 `migrations/migrations.go` 的 `migrationList()` 末尾。实体字段与列 DROP **同批落地**(避免 GORM select 到已删列——`is_default` 那次的教训)。已有行 `content` 已是渲染后正文,无需数据迁移。

## 前端改动

### 整文件删除(含各自 `__tests__`)

- `components/agentre/workflows/workflow-dag-designer.tsx`
- `components/agentre/workflows/workflow-node-form.tsx`
- `components/agentre/workflows/flow-graph-draft.ts`
- `components/agentre/orchestration/flow-graph.ts`
- `components/agentre/orchestration/flow-graph-view.tsx`
- `components/agentre/orchestration/flow-overlay.ts`
- `components/agentre/orchestration/run-flow-overlay.tsx`
- `components/agentre/orchestration/run-flow-blueprint.tsx`

### 修改

- `workflows/workflow-editor-form.tsx` → 只剩 名称 + 标签(chips) + 正文(Markdown textarea);删「步骤(outline)」编辑区与相关 props/回调。
- `workflows/workflow-manager-dialog.tsx` → 去 DAG 预览 / 步骤展示。
- `orchestration/index.tsx` → 移除 `RunFlowOverlay` 渲染与相关 import/state(`selectedLabel` 等)。
- `orchestration/task-board.tsx` → 去掉 `selectedLabel` / 节点筛选逻辑。
- `orchestration/run-new-dialog.tsx` → 若展示 outline / DAG 预览,一并去。
- `orchestration/graph-data.ts` → 若含 flow-graph 相关工具函数则清理(buildGraph 本身建自 tasks,保留)。
- i18n:`i18n/locales/zh-CN/common.json` + `en/common.json` 删 `workflows.editor.outline`、模板 / DAG 相关 key。

## 测试策略(TDD Red → Green)

- **删除**:projection / render / dag-designer / flow-graph / overlay / node-form 相关 `*_test.{go,tsx,ts}`。
- **改**:`migrations/202607080001_workflow_default_presets_test.go` retarget 成只验 4 内置流程的 `content` / `tags` 存活,不再断言 graph/template/outline。
- **改**:`workflow_svc` / `workflow_repo` 的 Save / List / toItem 单测(列集变化,sqlmock 语句同步)。
- **改**:`orch_svc` create / mcp / status 单测(去 graph 快照 / node 参数 / Node 响应)。
- **新增**:新迁移配 `*_test.go` —— 断言 DROP 后 `workflows.graph/template/outline`、`orchestration_runs.flow_graph`、`tasks.node_ref` 列不存在;`content` 数据保留;4 内置流程仍在。
- **收尾**:跑 `make test-backend` + `make lint` + 全量 `cd frontend && pnpm test`,看真 exit code(`make ... | tail` 会吞退出码);补跑 `gofmt -l`。

## 已定判断点(评审已确认)

1. **4 个内置流程保留,正文逐字不改**。prose 里的「## Flow / 1. / 2.」是提示词文本,不属于要删的步骤机制。
2. **运行详情页不新增「本次流程」只读面板**。删掉 DAG overlay 后流程正文在 run 里不再展示(仍可在流程库看)。YAGNI。
3. **StructureGraph(实时任务执行树)保留**。建自 tasks/subagents,与 flow DAG 无关。

## 提交纪律

- 共享分支 `develop/wyz` 有并发会话,提交一律带 pathspec(`git commit <files>`),不裸 `git commit`。
- gitmoji 提交;后端 golangci-lint v2 全绿;前端 i18n key 覆盖测试通过。
- 不夹带无关重构 / 格式化 / import 重排。
