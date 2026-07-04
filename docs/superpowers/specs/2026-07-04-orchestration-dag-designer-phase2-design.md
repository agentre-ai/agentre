# 编排流程 DAG 设计器 — Phase 2 设计（表单式设计器）

> Phase 1（已合入并推送 develop/wyz `8288734`）把 graph 做成真源、投影注入 Leader、修库模式注入缺口、新建 Run 弹窗只读 mini-DAG 预览。**Phase 2 = 让用户能编辑这个 graph**：在流程库管理器里做一个「表单 + 实时 DAG + 实时提示词」的设计器（对齐 Pencil 稿 `a0fv2`）。

## 目标（一句话）

在**流程库管理器**（`workflow-manager-dialog.tsx`）的编辑区，把当前的自由文本编辑表单升级成 **DAG 表单设计器**：左侧结构化节点表单，右上实时 DAG 图，右下实时提示词预览；保存时把 graph 落库、后端自动投影 content/outline。

## 范围与前提

- **Phase 2 基本是纯前端。** Phase 1 后端已就绪：`WorkflowCreate/Update` 接受 `graph` 并 `applyGraph` 投影 content/outline；`WorkflowPreviewGraph(graph)` 提供实时投影；`FlowGraphView` 可复用渲染 DAG。Phase 2 不需要新迁移/新 service 方法/新绑定（除非发现缺口）。
- **软约束延续**：设计器只产出 graph 与投影散文，不做硬门控（Phase 3）。
- **不引入新前端依赖**（沿用 Phase 1 硬约束）：节点表单 + 边用自定义代码，DAG 用既有 `FlowGraphView`。
- **i18n**：所有可见文案走 `t(...)` + 双 locale；节点 label/brief 是用户动态数据不入 i18n。

## 落点：管理器编辑区（EditorPane）

现状：`workflow-manager-dialog.tsx` 是 920×640 模态，左 300px 列表 + 右 EditorPane（`WorkflowEditorForm` 自由文本：name/tags/outline/content）。`use-workflows` 的 `create/update` 目前签名 **不含 graph**。

Phase 2：
- **模态在「设计器模式」加宽**到 ~1200px（`max-w-[96vw]`），编辑区变 3 栏。浏览/普通编辑仍用原宽。
- EditorPane 编辑一个**有 graph 的流程**（内置默认流程 + 任何用设计器建的）时 → 渲染 **DAG 设计器**。
- **legacy 无 graph 流程**：保留自由文本 `WorkflowEditorForm`，顶部给一个「转成 DAG 设计器」入口（点了初始化一个单节点 graph 进设计器）。新建默认走设计器。

## 布局（对齐 a0fv2 三栏）

编辑区 3 栏：
- **左栏（~360px）：节点表单**
  - 顶部：流程名 Input（必填）+ tags chips（沿用现有展示层，可选）。
  - 节点列表：每个节点一行卡，可**上移/下移/删除**；点开编辑：
    - `label`（必填）
    - `kind` 二元 Select：`委派任务(task)` / `Leader 步骤(leader)`
    - `brief`（仅 task 显示，Textarea：任务说明 + 验收）
    - `依赖(depends on)`：多选 chips，从**本节点之前的节点** label 里选 → 生成 sequence 边（这就是表单里「画边」的方式）
    - `失败打回(on fail → bounce)`：可选单选，选一个上游节点 → 生成 bounce 边
  - 底部：「+ 添加节点」。
- **右上栏：实时 DAG** — 复用 `FlowGraphView graph={draftGraph}`，随表单编辑实时重渲。
- **右下栏：实时提示词预览** — 每次 graph 变 → 防抖(~250ms)调 `WorkflowPreviewGraph(name, graph)` → 展示投影出的散文 `content`（只读，等于将注入 Leader 的正文）。

## 数据流 / 保存

- 编辑态维护 `draftGraph`（`FlowGraph` TS 结构，Phase 1 `flow-graph.ts` 已有类型）。
- 表单每次改动 → 更新 `draftGraph` → ①`FlowGraphView` 重渲 ②防抖 `WorkflowPreviewGraph` 刷新提示词。
- **保存**：`create/update` 增加 `graph` 参数（`use-workflows.ts` + 管理器 draft 状态加 `draftGraph`），把 `JSON.stringify(draftGraph)` 传给 `WorkflowCreate/Update`；后端 `applyGraph` 投影 content/outline（Phase 1 已做）。前端不再手拼 content。
- 保存后回浏览态，`ViewPane` 展示投影出的 content（现有 MarkdownText）。

## 校验（软，前端）

- 名称必填（沿用）。
- 至少 1 个节点；每个 task 节点 brief 可空（投影回退用 label）。
- `depends on` 只能选**更早**的节点（防环，保持 DAG）——UI 层就只列前序节点，天然不成环。
- bounce 目标可为任意已存在节点（打回是回退语义，允许指回上游）。
- 无阻塞式硬校验（软约束）；非法/空 graph 时提示词预览区显示占位。

## 复用 / 需要改的文件（预估，纯前端）

- 复用：`flow-graph.ts`(类型/parse)、`flow-graph-view.tsx`(DAG 渲染)、`WorkflowPreviewGraph` 绑定。
- 新增：`workflow-dag-designer.tsx`（3 栏设计器）、`workflow-node-form.tsx`（单节点表单行）、可能 `use-flow-graph-draft.ts`（draftGraph 编辑 + 节点/边增删改的纯 hook，便于单测）。
- 改：`workflow-manager-dialog.tsx`（设计器模式加宽 + draftGraph 状态 + 分流 designer/free-text）、`use-workflows.ts`（create/update 加 graph 参数）、i18n 双 locale。
- 后端：**预期无改动**（Phase 1 已支持 graph 保存 + 预览绑定）。若发现 `WorkflowItem`/绑定缺字段再补。

## 测试

- 纯 hook（节点增删改、depends-on 生成边、bounce 边、reorder、防环只列前序）单测。
- 设计器组件测试：加节点→DAG 出现该节点、改 kind→brief 显隐、依赖多选→边生成、保存调 `WorkflowUpdate` 带 `graph`。
- 提示词预览：mock `WorkflowPreviewGraph` 返回投影 → 断言展示。
- i18n 覆盖 + tsc + eslint。

## 明确不做（Phase 3+）

- 运行时把当前 Run 的进度 overlay 到 DAG 上。
- 硬门控 / 状态机（节点必须按序 settle 才放行）。
- 自由拖拽画布（本设计是表单式，非 canvas 拖拽）。
- 多流程模板库扩充（示例流程作为额外 seed，不在本片）。

## 交付方式

沿用 Phase 1 的 Subagent-Driven（每 Task 一 implementer + 逐任务复审 + whole-branch 终审），合入 develop/wyz。
