# 砍掉编排流程步骤/DAG 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把编排流程库从「DAG 图 + 有序步骤 + Go 模板」退化为纯文本提示词库(name + content + tags),移除所有步骤/DAG 机制(前端 UI、MCP dispatch node、后端投影/模板/快照、数据库列)。

**Architecture:** 分层删除,保持每个 task 结束时可编译、测试全绿。后端先删 Go 实体字段与消费点(列暂留,GORM 忽略未映射列),最后追加迁移 DROP 空列;前端在 `make generate` 刷新绑定后删 DAG 组件、把编辑器收窄为正文。运行时注入路径(`run.FlowContent` → Leader system-prompt)完全保留。

**Tech Stack:** Go 1.26 / cago / gormigrate / sqlmock / goconvey;React 19 / TS / Vitest / react-i18next;Wails 绑定。

## Global Constraints

- 严格 TDD:改测试(红)→ 删/改实现 → 测试绿 → 提交。删除既有特性时,先删/改其测试到新期望,再动实现。
- 追加新迁移到 `migrationList()` 末尾,**绝不改已有迁移的 Migrate/Rollback**(改迁移的**测试**允许,有先例)。
- 实体 Go 字段删除与列 DROP **不在同一 commit 也安全**:先删字段(列暂留、GORM 忽略),迁移后删列。顺序:后端字段删除 → 迁移。
- 共享分支 `develop/wyz` 有并发会话:提交一律带 pathspec(`git commit <files...>`),禁止裸 `git commit`。
- gitmoji 提交;后端 golangci-lint v2 全绿;前端新增/保留 UI 文案走 i18n(zh-CN + en),`i18n.test.ts` 校验 key 覆盖。
- 不夹带无关重构 / 格式化 / import 重排。
- 收尾看真 exit code:`make test-backend` && `make lint` && `cd frontend && pnpm test`;补跑 `gofmt -l`(vitest/make ... | tail 会吞退出码)。

---

## Task 1: 后端 — 移除 dispatch `node` 参数与 `Task.NodeRef`(DAG 进度打标)

**Files:**
- Modify: `internal/model/entity/orch_entity/task.go:35`(删 `NodeRef` 字段)
- Modify: `internal/service/orch_svc/dispatch.go:14,69`(Dispatch 签名去 `node string`;去 `NodeRef: node`)
- Modify: `internal/service/orch_svc/mcp.go:173,183,383`(handleDispatch 结构去 `Node`;调用去 `p.Node`;工具 schema 去 `node` 属性)
- Modify: `internal/service/orch_svc/status.go:22,51`(响应结构去 `Node`;去赋值)
- Modify: `internal/app/orch.go:41-42,92`(task DTO 去 `NodeRef`)
- Test: `internal/service/orch_svc/dispatch_test.go`(删 `TestDispatch_StoresNodeRef`;其余 Dispatch 调用去末位 node 实参)

**Interfaces:**
- Produces: `func (s *orchSvc) Dispatch(ctx, parentSessionID int64, agentName, brief string, isolate bool) (int64, error)`(去掉最后的 `node string`)。`orchMCP.svc` 是 `*orchSvc` 具体类型,**无需 `make mock`**。

- [ ] **Step 1: 改红 — 删除/改写 dispatch 测试**
  - 删除 `TestDispatch_StoresNodeRef`(整个函数,`dispatch_test.go:160` 起)。
  - 把其余 5 处 `Dispatch(context.Background(), N, "…", "…", bool, "")` 的**末位 `""` 删掉**(58/79/102/127/154 行)。

- [ ] **Step 2: 跑红**
  Run: `go test -race ./internal/service/orch_svc/ -run TestDispatch`
  Expected: 编译失败(Dispatch 仍是 6 参)/断言不匹配。

- [ ] **Step 3: 改实现**
  - `task.go`:删 `NodeRef` 整行(含注释)。
  - `dispatch.go:14`:签名改为 `func (s *orchSvc) Dispatch(ctx context.Context, parentSessionID int64, agentName, brief string, isolate bool) (int64, error)`;删第 69 行 `NodeRef: node,`。
  - `mcp.go`:handleDispatch 的入参结构删 `Node string \`json:"node"\`` 行;第 183 行调用改 `m.svc.Dispatch(r.Context(), ref.sessionID, p.Agent, p.Brief, p.Isolate)`;第 383 行工具 schema 删整段 `"node": map[string]any{...}` 属性(注意保留其它属性的逗号语法正确)。
  - `status.go`:删结构体 `Node string \`json:"node,omitempty"\``(22)与 `Node: t.NodeRef,`(51)。
  - `app/orch.go`:删 DTO `NodeRef` 字段(41-42)与 `NodeRef: t.NodeRef,`(92)。

- [ ] **Step 4: 跑绿**
  Run: `go test -race ./internal/service/orch_svc/ ./internal/app/ ./internal/model/entity/orch_entity/`
  Expected: PASS。

- [ ] **Step 5: 提交**
  ```bash
  git commit internal/model/entity/orch_entity/task.go internal/service/orch_svc/dispatch.go internal/service/orch_svc/mcp.go internal/service/orch_svc/status.go internal/app/orch.go internal/service/orch_svc/dispatch_test.go \
    -m "♻️ orch: 删 dispatch node 参数与 Task.NodeRef(DAG 进度打标)"
  ```

---

## Task 2: 后端 — 移除 `run.FlowGraph` 快照与 `FlowGraphByID`

**Files:**
- Modify: `internal/model/entity/orch_entity/run.go:28`(删 `FlowGraph` 字段)
- Modify: `internal/service/orch_svc/create.go:52-60,64`(删 graph 快照分支;`OrchestrationRun{}` 去 `FlowGraph`)
- Modify: `internal/service/orch_svc/deps.go:67`(WorkflowReader 接口删 `FlowGraphByID`)
- Modify: `internal/app/orch_adapter.go:190`(删 `FlowGraphByID` 适配实现)
- Modify: `internal/app/orch.go:19-20,72`(run DTO 去 `FlowGraph`)
- Regen: `internal/service/orch_svc/mock_orch_svc/mock_deps.go`(接口变了 → `make mock`)
- Test: `internal/service/orch_svc/create_test.go`(去 `FlowGraphByID` 期望 stub / FlowGraph 断言)

**Interfaces:**
- Produces: `WorkflowReader` 接口只剩 `FlowContentByID(ctx, id) (string, error)`(去掉 `FlowGraphByID`)。

- [ ] **Step 1: 改红 — create_test 去掉 FlowGraphByID stub 与 FlowGraph 断言**
  在 `create_test.go` 里删除对 `FlowGraphByID` 的 mock 期望(`EXPECT().FlowGraphByID(...)`)与任何 `run.FlowGraph` 断言。

- [ ] **Step 2: 跑红**
  Run: `go test -race ./internal/service/orch_svc/ -run TestCreateRun`
  Expected: 编译失败(接口/字段仍在)或 mock 未满足。

- [ ] **Step 3: 改实现**
  - `run.go`:删 `FlowGraph` 整行。
  - `create.go`:删第 52-60 行整段(`// 库模式额外快照原始 graph JSON…` 到 `}`);`OrchestrationRun{...}` 里删 `FlowGraph: flowGraph,`(第 64 行内),并删上面已无用的 `var flowGraph string`。
  - `deps.go`:接口删 `FlowGraphByID(ctx context.Context, id int64) (string, error)` 行。
  - `orch_adapter.go`:删 `FlowGraphByID` 方法整体(190 起)。
  - `app/orch.go`:删 run DTO `FlowGraph` 字段(19-20)与 `FlowGraph: r.FlowGraph,`(72)。

- [ ] **Step 4: 重生 mock + 跑绿**
  Run: `make mock && go test -race ./internal/service/orch_svc/ ./internal/app/`
  Expected: PASS。

- [ ] **Step 5: 提交**
  ```bash
  git commit internal/model/entity/orch_entity/run.go internal/service/orch_svc/create.go internal/service/orch_svc/deps.go internal/app/orch_adapter.go internal/app/orch.go internal/service/orch_svc/mock_orch_svc/mock_deps.go internal/service/orch_svc/create_test.go \
    -m "♻️ orch: 删 run.FlowGraph 快照与 FlowGraphByID"
  ```

---

## Task 3: 后端 — 流程库收窄为纯文本(删 projection/render/template/outline/graph)

**Files:**
- Modify: `internal/model/entity/workflow_entity/workflow.go:20-23`(删 `Tags` 保留,删 `Outline`/`Graph`/`Template` 字段)
- Delete: `internal/service/workflow_svc/projection.go` + `projection_test.go`
- Delete: `internal/service/workflow_svc/render.go` + `render_test.go`
- Modify: `internal/service/workflow_svc/types.go`(见下)
- Modify: `internal/service/workflow_svc/workflow.go`(删 `applyTemplate`;Create/Update 直存 content;toItem 去三字段)
- Modify: `internal/app/workflow.go:27-50`(删 `WorkflowPreviewRequest`/`WorkflowPreviewResponse`/`WorkflowPreviewGraph`)
- Modify: `migrations/202607080001_workflow_default_presets_test.go`(retarget:local `row` 结构去 Template/Graph/Outline;删 RenderWorkflowContent 一致性断言)
- Test: `internal/service/workflow_svc/workflow_test.go`、`internal/repository/workflow_repo/workflow_test.go`(列集变化)

**Interfaces:**
- Produces:
  - `WorkflowItem{ ID int64; Name string; Content string; Tags []string; RunCount int; Createtime int64; Updatetime int64 }`
  - `CreateWorkflowRequest{ Name string; Content string; Tags []string }`
  - `UpdateWorkflowRequest{ ID int64; Name string; Content string; Tags []string }`
  - `workflow_entity.Workflow` 字段:`ID/Name/Content/Tags/Status/Createtime/Updatetime`(无 Graph/Template/Outline)。

- [ ] **Step 1: 改红 — 更新 workflow_svc / repo 单测到新形状**
  - `workflow_svc/workflow_test.go`:Create/Update 用例改传 `Content`(不再 Template/Graph/Outline);断言 `item.Content == 传入内容`;删任何 outline/graph/template 断言。
  - `workflow_repo/workflow_test.go`:sqlmock 期望的 INSERT/UPDATE 列集去掉 `graph/template/outline`(按新实体字段 SET)。
  - `migrations/202607080001_workflow_default_presets_test.go`:把 `type row struct{ Name, Content, Template, Graph, Tags, Outline string }` 改为 `type row struct{ Name, Content, Tags string }`;SELECT 只取 `name,content,tags`;删调用 `workflow_svc.RenderWorkflowContent` 的一致性断言块;保留「4 行存在 + content 非空 + tags 非空」断言。

- [ ] **Step 2: 跑红**
  Run: `go test -race ./internal/service/workflow_svc/ ./internal/repository/workflow_repo/ ./migrations/ -run 'Workflow|Preset'`
  Expected: 编译失败(字段/函数仍在旧形状)。

- [ ] **Step 3: 改实现**
  - `workflow.go`(entity):删 `Outline`、`Graph`、`Template` 三行(保留 `Tags`)。
  - `git rm internal/service/workflow_svc/projection.go internal/service/workflow_svc/projection_test.go internal/service/workflow_svc/render.go internal/service/workflow_svc/render_test.go`
  - `types.go`:`WorkflowItem` 去 `Template/Outline/Graph`;`CreateWorkflowRequest` 去 `Template/Outline/Graph`、加 `Content string \`json:"content"\``;`UpdateWorkflowRequest` 同样去三字段、加 `Content string \`json:"content"\``。
  - `workflow.go`(svc):
    - `toItem` 去 `Template/Outline/Graph` 三行。
    - 删 `applyTemplate` 整个函数;删其独占的 `fmt` import(若 `go build` 报未用)。
    - `Create`:
      ```go
      w := &workflow_entity.Workflow{
          Name:    strings.TrimSpace(req.Name),
          Content: req.Content,
          Tags:    encodeStringList(req.Tags),
          Status:  consts.ACTIVE,
      }
      if err := w.Check(ctx); err != nil {
          return nil, err
      }
      ```
      (删除 `Outline:` 行与 `applyTemplate(...)` 调用)
    - `Update`:
      ```go
      w.Name = strings.TrimSpace(req.Name)
      w.Content = req.Content
      w.Tags = encodeStringList(req.Tags)
      if err := w.Check(ctx); err != nil {
          return nil, err
      }
      ```
      (删除 `w.Outline = ...` 与 `applyTemplate(...)` 调用)
  - `app/workflow.go`:删 `WorkflowPreviewRequest`(28)、`WorkflowPreviewResponse`(36)两个类型与 `WorkflowPreviewGraph` 方法(43-51)。

- [ ] **Step 4: 跑绿**
  Run: `go test -race ./internal/service/workflow_svc/ ./internal/repository/workflow_repo/ ./internal/app/ ./migrations/`
  Expected: PASS。

- [ ] **Step 5: 提交**
  ```bash
  git add -A internal/service/workflow_svc internal/model/entity/workflow_entity internal/app/workflow.go internal/repository/workflow_repo migrations/202607080001_workflow_default_presets_test.go
  git commit internal/service/workflow_svc internal/model/entity/workflow_entity/workflow.go internal/app/workflow.go internal/repository/workflow_repo/workflow_test.go migrations/202607080001_workflow_default_presets_test.go \
    -m "♻️ workflow: 流程库收窄为纯文本(删 projection/render/template/outline/graph)"
  ```
  > `git add -A` 让删除的文件进 index;`git commit <pathspec>` 仍带路径,避免卷入并发改动。删除的 4 个文件路径也在 pathspec 覆盖目录下。

---

## Task 4: 数据库 — 追加迁移 DROP 步骤/DAG 列

**Files:**
- Create: `migrations/202607080002_drop_flow_dag_steps.go`
- Create: `migrations/202607080002_drop_flow_dag_steps_test.go`
- Modify: `migrations/migrations.go`(`migrationList()` 末尾追加 `migration202607080002()`)

> 若并发会话已占 `202607080002`,取末尾下一个可用 ID。

- [ ] **Step 1: 写红 — 迁移测试**
  `migrations/202607080002_drop_flow_dag_steps_test.go`:
  ```go
  package migrations

  import (
      "testing"

      "github.com/glebarez/sqlite"
      "github.com/stretchr/testify/assert"
      "gorm.io/gorm"
  )

  func TestMigration202607080002_DropsFlowDagColumns(t *testing.T) {
      db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
      assert.NoError(t, err)
      assert.NoError(t, RunMigrations(db))

      hasCol := func(table, col string) bool {
          var n int
          db.Raw(
              "SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?",
              table, col,
          ).Scan(&n)
          return n > 0
      }

      // DAG/步骤列已被删除
      assert.False(t, hasCol("workflows", "graph"))
      assert.False(t, hasCol("workflows", "template"))
      assert.False(t, hasCol("workflows", "outline"))
      assert.False(t, hasCol("orchestration_runs", "flow_graph"))
      assert.False(t, hasCol("tasks", "node_ref"))

      // 保留列仍在,且 4 内置流程正文存活
      assert.True(t, hasCol("workflows", "content"))
      var cnt int
      db.Raw("SELECT COUNT(*) FROM workflows WHERE content <> ''").Scan(&cnt)
      assert.GreaterOrEqual(t, cnt, 4)
  }
  ```

- [ ] **Step 2: 跑红**
  Run: `go test -race ./migrations/ -run TestMigration202607080002`
  Expected: FAIL(`migration202607080002` 未定义)。

- [ ] **Step 3: 写迁移**
  `migrations/202607080002_drop_flow_dag_steps.go`:
  ```go
  package migrations

  import (
      "github.com/go-gormigrate/gormigrate/v2"
      "gorm.io/gorm"
  )

  // migration202607080002 删除步骤/DAG 相关列:流程库退化为纯文本提示词库。
  //   - workflows: graph / template / outline
  //   - orchestration_runs: flow_graph
  //   - tasks: node_ref
  // content 已是渲染后正文,无需数据迁移;SQLite DROP COLUMN(≥3.35)已被 202607080001 用过。
  func migration202607080002() *gormigrate.Migration {
      return &gormigrate.Migration{
          ID: "202607080002",
          Migrate: func(tx *gorm.DB) error {
              stmts := []string{
                  `ALTER TABLE workflows DROP COLUMN graph`,
                  `ALTER TABLE workflows DROP COLUMN template`,
                  `ALTER TABLE workflows DROP COLUMN outline`,
                  `ALTER TABLE orchestration_runs DROP COLUMN flow_graph`,
                  `ALTER TABLE tasks DROP COLUMN node_ref`,
              }
              for _, s := range stmts {
                  if err := tx.Exec(s).Error; err != nil {
                      return err
                  }
              }
              return nil
          },
          Rollback: func(tx *gorm.DB) error {
              stmts := []string{
                  `ALTER TABLE workflows ADD COLUMN graph text NOT NULL DEFAULT ''`,
                  `ALTER TABLE workflows ADD COLUMN template text NOT NULL DEFAULT ''`,
                  `ALTER TABLE workflows ADD COLUMN outline text NOT NULL DEFAULT '[]'`,
                  `ALTER TABLE orchestration_runs ADD COLUMN flow_graph text NOT NULL DEFAULT ''`,
                  `ALTER TABLE tasks ADD COLUMN node_ref text NOT NULL DEFAULT ''`,
              }
              for _, s := range stmts {
                  if err := tx.Exec(s).Error; err != nil {
                      return err
                  }
              }
              return nil
          },
      }
  }
  ```
  在 `migrations/migrations.go` 的 `migrationList()` 返回切片末尾追加一行 `migration202607080002(),`。

- [ ] **Step 4: 跑绿**
  Run: `go test -race ./migrations/`
  Expected: PASS(新测试 + 既有 202607080001 retarget 测试全绿)。

- [ ] **Step 5: 提交**
  ```bash
  git commit migrations/202607080002_drop_flow_dag_steps.go migrations/202607080002_drop_flow_dag_steps_test.go migrations/migrations.go \
    -m "✨ migrate: 202607080002 删除步骤/DAG 列(workflows.graph/template/outline, runs.flow_graph, tasks.node_ref)"
  ```

---

## Task 5: 前端 — 流程库编辑器收窄为纯文本 + 删 DAG 设计器

**前置:** 后端 Task 1-4 已合入本地分支;先 `make generate` 刷新 `frontend/wailsjs/`(`WorkflowCreate/Update` 入参、`WorkflowPreviewGraph` 消失、DTO 去字段)。

**Files:**
- Regen: `make generate`(刷新 `frontend/wailsjs/go/**`)
- Delete: `frontend/src/components/agentre/workflows/workflow-dag-designer.tsx`、`workflow-node-form.tsx`、`flow-graph-draft.ts`(+ 其 `__tests__`)
- Modify: `frontend/src/hooks/use-workflows.ts`
- Modify: `frontend/src/components/agentre/workflows/workflow-editor-form.tsx`
- Modify: `frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx`(去设计器/预览,只挂 editor-form;create/update 传 content)
- Modify: `frontend/src/components/agentre/workflows/workflow-editor-form.test.tsx`、`workflow-manager-dialog.test.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json` + `en/common.json`(删 outline/DAG key)

**Interfaces:**
- Produces:
  - `useWorkflows().create(name: string, content: string, tags: string[])`
  - `useWorkflows().update(id: number, name: string, content: string, tags: string[])`
  - `WorkflowItem = { id; name; content; tags; runCount; createtime; updatetime }`
  - `WorkflowEditorFormProps = { name; content; tags; error; onNameChange; onContentChange; onTagsChange }`

- [ ] **Step 1: `make generate`**
  Run: `make generate`
  Expected: `frontend/wailsjs/go/app/App.d.ts` 里 `WorkflowPreviewGraph` 消失;`models.ts` 的 workflow 相关类型去掉 template/graph/outline。

- [ ] **Step 2: 改红 — 更新 editor-form / manager-dialog 测试到新形状**
  - `workflow-editor-form.test.tsx`:删所有 outline 步骤相关断言/交互(`workflow-outline-*` testid);断言表单只有 name/tags/content。
  - `workflow-manager-dialog.test.tsx`:删设计器/DAG 预览断言;新建/编辑用例断言调用 `WorkflowCreate({name, content, tags})` 形状。

- [ ] **Step 3: 跑红**
  Run: `cd frontend && pnpm test -- src/components/agentre/workflows/`
  Expected: FAIL/编译错(props 与实现仍旧形状)。

- [ ] **Step 4: 改实现**
  - `use-workflows.ts`:
    - `WorkflowItem` type 删 `template/graph/outline`。
    - `reload()` 的 map 去 `template/graph/outline` 三行。
    - `create`:
      ```ts
      const create = useCallback(
        async (name: string, content: string, tags: string[]) => {
          await WorkflowCreate({ name, content, tags });
          await reload();
        },
        [reload],
      );
      ```
    - `update`:
      ```ts
      const update = useCallback(
        async (id: number, name: string, content: string, tags: string[]) => {
          await WorkflowUpdate({ id, name, content, tags });
          await reload();
        },
        [reload],
      );
      ```
  - `workflow-editor-form.tsx`:
    - `WorkflowEditorFormProps` 删 `outline` 与 `onOutlineChange`。
    - 函数签名删 `outline` / `onOutlineChange` 解构;删 `stepDraft` state、`addStep`、`moveStep`。
    - 删整个「步骤(概览)」块(现 139-215 行 `<div ...outline...>`)。
    - 删 `ArrowDown, ArrowUp` import(若删步骤块后未用);`X` 仍被 tags 用,保留。
    - 保留名称/标签/正文块与 `insertTemplate`(正文骨架便捷键,与被删的 Go 模板机制无关)。
  - `workflow-manager-dialog.tsx`:删设计器(`WorkflowDagDesigner`)与 DAG 预览的 import/渲染/状态;编辑态只渲染 `WorkflowEditorForm`;保存改调 `create(name, content, tags)` / `update(id, name, content, tags)`;删 outline/graph 本地 state。
  - `git rm frontend/src/components/agentre/workflows/workflow-dag-designer.tsx frontend/src/components/agentre/workflows/workflow-node-form.tsx frontend/src/components/agentre/workflows/flow-graph-draft.ts`(及 `__tests__` 内对应 spec)。
  - i18n:`zh-CN/common.json` 与 `en/common.json` 删 `workflows.editor.outline`、`workflows.editor.outlineHint`、`workflows.editor.outlinePlaceholder`、`workflows.editor.moveUp`、`workflows.editor.moveDown`,以及 DAG 设计器专属 key(设计器文件里 `t("...")` 的 key,grep 确认无其它引用后删)。保留 `workflows.editor.template`(正文骨架)。

- [ ] **Step 5: 跑绿(含 i18n 覆盖 + 类型)**
  Run: `cd frontend && pnpm test -- src/components/agentre/workflows/ src/__tests__/i18n.test.ts && pnpm exec tsc --noEmit`
  Expected: PASS,无 tsc 报错。

- [ ] **Step 6: 提交**
  ```bash
  git add -A frontend/src/components/agentre/workflows frontend/src/hooks/use-workflows.ts frontend/src/i18n/locales frontend/wailsjs
  git commit frontend/src/components/agentre/workflows frontend/src/hooks/use-workflows.ts frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json frontend/wailsjs \
    -m "♻️ workflows: 流程库编辑器收窄为纯文本,删 DAG 设计器"
  ```

---

## Task 6: 前端 — 运行详情移除 DAG overlay

**Files:**
- Delete: `frontend/src/components/agentre/orchestration/run-flow-overlay.tsx`、`run-flow-blueprint.tsx`、`flow-graph-view.tsx`、`flow-graph.ts`、`flow-overlay.ts`(+ 各 `__tests__`)
- Modify: `frontend/src/components/agentre/orchestration/index.tsx`(删 `RunFlowOverlay` 渲染 + `flowGraph`/`selectedLabel` 相关 state/import)
- Modify: `frontend/src/components/agentre/orchestration/task-board.tsx`(删 `selectedLabel`/`nodeRef` 节点筛选)
- Modify: `frontend/src/components/agentre/orchestration/run-new-dialog.tsx`(删 `RunFlowBlueprint` 预览渲染/import)
- Test: 对应 orchestration `__tests__`(去 overlay/blueprint/node-filter 断言)

**Interfaces:**
- Consumes:后端已去 `RunDetailDTO.run.flowGraph`、`task.nodeRef`(Task 2/1 + generate);前端引用这些字段的代码必须删净,否则 tsc 报错。
- `StructureGraph`(建自 tasks/subagents,不读 flowGraph)、`TaskBoard`(去筛选后)保留。

- [ ] **Step 1: 改红 — 更新 orchestration 测试**
  删 orchestration `__tests__` 中对 `RunFlowOverlay` / `run-flow-blueprint` / 节点点击筛选(`selectedLabel`)/`nodeRef` 的断言与渲染探测。

- [ ] **Step 2: 跑红**
  Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/`
  Expected: FAIL/编译错(引用了将删的模块/字段)。

- [ ] **Step 3: 改实现**
  - `git rm` 上述 5 个文件(及 `__tests__` 内对应 spec)。
  - `index.tsx`:删 `import { RunFlowOverlay } ...`;删 `<RunFlowOverlay .../>`(约 287 行)块;删 `selectedLabel` state 与其 setter、传给 `TaskBoard` 的 `selectedLabel`/`onNodeClick` props;删任何 `detail.run?.flowGraph` 读取。保留 `<StructureGraph .../>`。
  - `task-board.tsx`:props 去 `selectedLabel` / `onNodeClick`;删按 `tk.nodeRef === selectedLabel` 的筛选分支(改为展示全部任务)。
  - `run-new-dialog.tsx`:删 `RunFlowBlueprint` import 与其预览渲染(选中流程时的 DAG 预览);保留流程选择下拉本身(仍传 FlowID)。

- [ ] **Step 4: 跑绿(orchestration + 类型)**
  Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/ && pnpm exec tsc --noEmit`
  Expected: PASS,tsc 无报错。

- [ ] **Step 5: 提交**
  ```bash
  git add -A frontend/src/components/agentre/orchestration
  git commit frontend/src/components/agentre/orchestration \
    -m "♻️ orch: 运行详情移除 DAG overlay/blueprint(保留任务执行树)"
  ```

---

## Task 7: 全量门禁 + 收尾核验

**Files:** 无代码改动(仅在有残留告警时回补对应 task)。

- [ ] **Step 1: 后端全量**
  Run: `make test-backend`
  Expected: PASS(真 exit code 0)。

- [ ] **Step 2: 后端 lint + 格式**
  Run: `make lint && gofmt -l internal/ migrations/`
  Expected: lint 0 issue;`gofmt -l` 无输出。

- [ ] **Step 3: 前端全量**
  Run: `cd frontend && pnpm test && pnpm exec tsc --noEmit && pnpm exec eslint .`
  Expected: 全 PASS;无 `i18next/no-literal-string`、无孤儿 i18n key(`i18n.test.ts` 绿)。

- [ ] **Step 4: 残留物扫描**
  Run:
  ```bash
  grep -rn "FlowGraph\|NodeRef\|node_ref\|ProjectGraph\|RenderWorkflowContent\|DAGPrompt\|PreviewGraph\|flow-graph\|run-flow-overlay\|run-flow-blueprint\|WorkflowDagDesigner" internal/ migrations/ frontend/src 2>/dev/null | grep -v "_test\|__tests__"
  ```
  Expected: 无输出(全部清干净;若有残留回到对应 task)。

- [ ] **Step 5: 无提交**(前面各 task 已分别提交;本 task 仅核验)

---

## Self-Review 备忘(计划作者已核对)

- **Spec 覆盖**:后端实体/service/repo/mcp/app(Task 1-3)、DB 迁移(Task 4)、前端库编辑器+DAG 设计器(Task 5)、运行详情 overlay(Task 6)、测试策略与全量门禁(各 task Step + Task 7)—— 均有对应 task。
- **保留项**:`run.FlowContent` 注入、`FlowContentByID`、`turn.go` BuildTurnExtras、`StructureGraph`、`TaskBoard`(去筛选)、`run-new-dialog` 选流程、`orchGuidance`、`workflows.editor.template` 骨架 —— 计划中均未触碰或显式保留。
- **类型一致**:`Dispatch` 5 参(去 node)贯穿 mcp/dispatch/test;`WorkflowItem`/`Create/UpdateWorkflowRequest` 三处形状一致(name+content+tags);`useWorkflows.create/update` 与后端请求字段一一对应。
- **顺序安全**:先删 Go 字段(列暂留,GORM 忽略)→ 后加迁移 DROP 空列;前端在 `make generate` 后再改,避免引用已删 DTO 字段。
