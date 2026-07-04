# 编排流程 DAG — Phase 3a 设计（运行时进度 overlay）

> Phase 1(graph 真源 + 投影注入 + 只读 mini-DAG)、Phase 2(表单式 DAG 设计器)均已完成并推送。**Phase 3a = 把「当前 Run 的实时进度」叠加到 flow DAG 上**：Run 详情页新增一个「Flow」Tab,用带状态着色的 `FlowGraphView` 展示每个委派任务节点的 pending/running/done/error。**只读可视化,无硬门控**(硬门控是 Phase 3b,单独决定)。

## 目标(一句话)

在 Run 详情页新增「Flow」Tab,复用 Phase 1 的 `FlowGraphView` 并按当前 Run 的任务实况给「委派任务节点」着色,让用户一眼看到流程走到哪了——保持 Phase 1/2 的软约束哲学,不改变运行时行为。

## 范围与前提

- **只做 overlay(可视化),不做硬门控。** 不碰 `scheduler.go` 的 dispatch 时序,不阻塞任何 `SendAndForget`。硬门控 = Phase 3b。
- **只点亮「委派任务(task-kind)节点」。** `leader-kind` 节点(See/Break/Wrap 这类 Leader 自己内联做的推理步)不产生子任务,渲染为中性(dim)、无状态。
- **节点↔任务链路是新增的,best-effort。** 现状:`Task` 无任何指向 flow 节点的字段;Leader 靠自然语言 brief + agent 名派活,不知道自己在做哪个流程节点。Phase 3a 让 `dispatch` 工具**可选**带上节点标记,未打标的任务不归属任何节点(在 Structure/TaskBoard 里照常可见)。信任模型 = 与现有「Leader 遵循 prose 指令」一致。
- **不改 `ProjectGraph` 输出。** 采用**按 label 打标**(Leader 已在 prose 里看到步骤名),而非暴露 opaque node-id → 避免改投影散文、避免破坏 Phase 1「seed content == graph 投影」tripwire。只在 dispatch 指引里加一句「派活时把对应的流程步骤名传给 `node`」。
- **不引入新前端依赖**(沿用硬约束);复用 `flow-graph.ts` 的 `layoutFlowGraph` + `FlowGraphView`。
- **i18n**:新 Tab 名 + 空态文案走 `t(...)` 双 locale;节点 label / 任务 brief 是动态数据不入 i18n。

## 数据模型(后端,均为追加)

1. **`orchestration_runs.flow_graph`**(text,`gorm:"column:flow_graph"`)——建 Run 时把 workflow 的 `graph` JSON **快照**进来(与现有 `FlowContent` 快照并列)。给 Run 一份冻结的 DAG 结构,不受之后编辑库流程影响。
   - `CreateRun`(`orch_svc/create.go`):库模式(`FlowContent==""` && `FlowID>0`)取 content 的同一处,顺带取 `graph`。需要给 `WorkflowReader` 接口加 `FlowGraphByID(ctx, id) (string, error)`(与现有 `FlowContentByID` 并列),app 层 `orchWorkflowAdapter` 经 `workflow_repo` 实现(软删/不存在 → 空)。
   - 快照来源:直接读 `workflow_entity.Workflow.Graph`(Phase 1 已有该列)。
2. **`orch_tasks.node_ref`**(text,nullable,`gorm:"column:node_ref"`)——该委派任务对应的流程节点 label(Leader 打的标)。空串 = 未打标。

迁移:新增一条迁移追加在 `migrationList()` 末尾,`ALTER TABLE` 加这两列(native SQL DDL)。**不改**任何既有迁移。

## dispatch 打标(后端)

- **MCP `dispatch` 工具**(`orch_svc/mcp.go` `handleDispatch`)入参从 `{agent, brief, isolate}` 增一个**可选** `node string`。
- **`Dispatch`**(`orch_svc/dispatch.go`)签名增 `node string` 参数,建 `Task` 时写入 `NodeRef: node`。其余逻辑不变。
- **dispatch 指引**:在工具描述 / 编排 guidance 里加一句「若当前工作对应流程里的某个步骤,把该步骤名(如 `FE`)传给 `node`,便于进度可视化;不确定就留空」。**不强制、不影响派活**。

## DTO 暴露(后端)

- `RunItemDTO`(`app/orch.go`)增 `flowGraph string`(来自 `run.FlowGraph`)。`RunDetailDTO` 经 `run` 已带上。
- `TaskDTO`(`app/orch.go`)增 `nodeRef string`(来自 `task.NodeRef`)。
- 前端 `RunDetailDTO.run.flowGraph` + `RunDetailDTO.tasks[].nodeRef` 即 overlay 的全部输入。

## 节点状态推导(前端,纯函数)

`deriveNodeStatus(flowGraph, tasks): Map<nodeId, NodeStatus>`——纯函数,单测隔离。

- 解析 `flowGraph`(复用 `parseFlowGraph`)。按 label(trim + 大小写不敏感)把 `tasks` 里带 `nodeRef` 的任务匹配到图节点,按节点 id 分组。
- **task-kind 节点**状态,优先级 `error > running > done > pending`:
  - **error** — 任一匹配任务 `status === "error"`(`TaskError`)。
  - **running** — 任一匹配任务非终态(`pending/running/awaiting_children/awaiting_user/paused`)。
  - **done** — 有 ≥1 匹配任务且全部终态,且至少一个 `done`(`TaskDone`)。
  - **pending** — 无匹配任务。
- **leader-kind 节点** — 恒为 `neutral`(无状态)。
- 未打标 / `nodeRef` 匹配不到节点的任务 → overlay 忽略(不影响 Structure/TaskBoard)。重复 label 的多个节点 → 都点亮(罕见,可接受)。
- 返回值同时带每节点匹配到的**任务数**(给 badge)与任务引用(给 tooltip),或另出一个 `nodeTaskCount` map——实现细节,保证纯可测。

`NodeStatus` 类型:`"pending" | "running" | "done" | "error" | "neutral"`。

## 前端 UI

- **`FlowGraphView` 增可选 `statusByNode?: Map<string, NodeStatus>` prop**——按状态给节点卡着色(用 DESIGN.md 的 run-status 色板:running=amber/`--color-status-running`、done=green、error=red、pending/leader=neutral)。Phase 1 的 run-new-dialog 预览不传该 prop → 行为不变(向后兼容)。复用 `layoutFlowGraph`。
- **新「Flow」Tab**:`ToggleBar` 从 graph/feed 两标签变 Flow/Structure/Feed 三标签。**默认 Tab 不变**(仍是现有的 Structure/graph);Flow Tab **仅当 `detail.run.flowGraph` 非空时出现**(无流程 / adhoc run 不显示该 Tab)。
- **新组件 `RunFlowOverlay`**:读 `detail.run.flowGraph` + `detail.tasks` → `deriveNodeStatus` → 渲染 `FlowGraphView graph={flowGraph} statusByNode={...}`;每节点附匹配任务数 badge + tooltip(任务 brief 列表)。空/非法 graph → 空态占位。
- **实时更新免费**:现有 `orch:run:updated` 事件已触发整 `loadRun` refetch(`orch-run-store`),overlay 随之重渲——**无需新事件 / 新 store / 增量**。
- **点节点筛选 TaskBoard = 延后的 nice-to-have**,非 MVP。

## 测试

- **后端**:迁移加两列(migration 套件);`Dispatch` 写 `node_ref`(dispatch/orch_svc 单测,sqlmock/mock);`CreateRun` 快照 `flow_graph`(create_test,含 `FlowGraphByID` 走 mock 的 `WorkflowReader`);DTO 暴露 `flowGraph`/`nodeRef`(app 层)。
- **前端**:`deriveNodeStatus` 纯函数单测(优先级 error>running>done>pending、leader 中性、未打标忽略、重复 label、终态混合);`FlowGraphView` 传 `statusByNode` 渲染出对应状态色 / testid;`ToggleBar` 仅在有 `flowGraph` 时显示 Flow Tab 且渲染 overlay。i18n 覆盖 + tsc + eslint。

## 明确不做(Phase 3b+)

- **硬门控 / 状态机**(前置节点未 settle 阻止下游 dispatch)——改运行时行为,需处理标错/死锁/逃生口,与软约束哲学有张力,单独设计。
- **leader-kind 节点点亮**(需 Leader 显式报自身步骤进度)。
- **点节点筛选 / 联动 TaskBoard**、从 Run 视图编辑流程。
- **改 `ProjectGraph` 输出 / opaque node-id 打标**——本片用 label 匹配,不动投影。
- **run.flow_graph 与库流程的后续同步**——快照即冻结,不追更新。

## 交付方式

沿用 Phase 1/2 的 Subagent-Driven(每 Task 一 implementer + 逐任务两段复审 + opus 全分支终审),合入 develop/wyz。后端(迁移/entity/dispatch/DTO)+ 前端(derive/view/tab)一个内聚 plan。
