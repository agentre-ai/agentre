# 编排流程 DAG — Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让编排 Run 以 DAG（graph JSON）为真源、把图投影成散文提示词真正注入 Leader；新建 Run 弹窗删「从零开始」、默认预选内置「Default Orchestration Flow」并展示只读 mini-DAG 预览。

**Architecture:** `workflows` 新增 `graph`(JSON 真源) 与 `is_default` 列；后端纯函数 `ProjectGraph(name, graph) → (content, outline)` 在 Create/Update 时把图投影进 `content`；`orch_svc.CreateRun` 把选中流程的 `content` 快照进 `run.FlowContent`（修复库模式注入为空的缺口），`turn.go` 注入路径不变。前端弹窗改分段控件、预选默认流程、用新组件 `FlowGraphView` 渲染 graph。

**Tech Stack:** Go 1.26 + gorm/gormigrate（原生 SQL 迁移）+ goconvey/gomock 单测；React 19 + TS + Vitest；Wails 绑定（`make generate`）。

## Global Constraints

- **不引入任何新前端依赖**（`@xyflow`/dagre/elk 等一律禁止）；DAG 布局与渲染用自定义代码，风格对齐既有 `structure-graph.tsx`。
- **graph 是唯一真源**；`content` 是投影派生（Create/Update 覆写）；`tags`/`outline` 仍**不注入 Leader**（`turn_test.go` 不变量）。
- **软约束**：不做硬门控/状态机（Phase 3 才议）。
- **新增可见 UI 文案必须走 i18n**：`react-i18next` 的 `t(...)` + 同步 `frontend/src/i18n/locales/{zh-CN,en}/common.json`；禁止硬编码中文。
- **默认流程 seed 内容用 English**（name/content/tags/graph 内的 label/brief），属 DB 数据不进 i18n。
- **迁移只能追加到 `migrationList()` 末尾**，DDL 用原生 SQL，不改既有迁移。
- **repo 单测用 sqlmock**，**service 单测用 mockgen 注入 repo mock、不连库**（沿用现有 `workflow_test.go` / `create_test.go` 风格）。
- **TDD：Red → Green → Refactor**，每个 Task 收尾一次提交。
- **共享分支 `develop/wyz`：`git commit <files>` 必须带 pathspec**（有并发会话共享 index），gitmoji 提交信息。

---

## File Structure

- `internal/service/workflow_svc/projection.go` (Create) — `FlowGraph` 类型 + `ProjectGraph` 纯函数 + `ParseFlowGraph`。
- `internal/service/workflow_svc/projection_test.go` (Create) — 投影 golden/结构断言。
- `migrations/202607040001_workflow_graph_default.go` (Create) — 加 `graph`/`is_default` 列 + seed 默认流程。
- `internal/model/entity/workflow_entity/workflow.go` (Modify) — 加 `Graph`/`IsDefault` 字段。
- `internal/service/workflow_svc/types.go` (Modify) — DTO/请求加 `Graph`/`IsDefault`。
- `internal/service/workflow_svc/workflow.go` (Modify) — `toItem` 带新字段；`Create/Update` 投影覆写 `content`/`outline`。
- `internal/service/workflow_svc/workflow_test.go` (Modify) — 投影覆写与 DTO 断言。
- `internal/service/orch_svc/deps.go` (Modify) — 新增 `WorkflowReader` 接口（进 mockgen source）。
- `internal/service/orch_svc/orch.go` (Modify) — `wf` 字段 + `RegisterWorkflowReader`。
- `internal/service/orch_svc/create.go` (Modify) — `flowId>0 && flowContent==""` 时快照 `content`。
- `internal/service/orch_svc/create_test.go` (Modify) — 库模式注入回归。
- `internal/app/app.go` (Modify) — `orchWorkflowAdapter` + `RegisterWorkflowReader`。
- `internal/app/workflow.go` (Modify) — `WorkflowPreviewGraph` binding。
- `frontend/src/components/agentre/orchestration/flow-graph.ts` (Create) — TS 类型 + `parseFlowGraph` + `layoutFlowGraph`。
- `frontend/src/components/agentre/orchestration/__tests__/flow-graph.test.ts` (Create) — 布局纯函数测试。
- `frontend/src/components/agentre/orchestration/flow-graph-view.tsx` (Create) — 只读 mini-DAG 渲染组件。
- `frontend/src/components/agentre/orchestration/run-new-dialog.tsx` (Modify) — 去 none/默认 library/预选 isDefault/渲染 FlowGraphView。
- `frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx` (Modify) — 更新 flowMode 用例。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` (Modify) — 删 `flowNone`，加 `flowPreview`。

---

## Task 1: graph→prose 投影纯函数

**Files:**
- Create: `internal/service/workflow_svc/projection.go`
- Test: `internal/service/workflow_svc/projection_test.go`

**Interfaces:**
- Produces:
  - `type FlowNode struct { ID, Label, Kind, Brief string; SharedFiles bool }`（`Kind` ∈ `"task"|"leader"`）
  - `type FlowEdge struct { From, To, Kind string }`（`Kind` ∈ `""(sequence)|"bounce"`）
  - `type FlowGraph struct { Version int; Nodes []FlowNode; Edges []FlowEdge }`
  - `func ParseFlowGraph(s string) (FlowGraph, bool)` — 空/非法 → `(zero,false)`
  - `func ProjectGraph(name string, g FlowGraph) (content string, outline []string)`

- [ ] **Step 1: Write the failing test**

```go
package workflow_svc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func defaultSeedGraph() FlowGraph {
	return FlowGraph{
		Version: 1,
		Nodes: []FlowNode{
			{ID: "see", Label: "See members", Kind: "leader"},
			{ID: "break", Label: "Break down", Kind: "leader"},
			{ID: "fe", Label: "Frontend", Kind: "task", Brief: "Build the UI.", SharedFiles: true},
			{ID: "be", Label: "Backend", Kind: "task", Brief: "Build the API."},
			{ID: "int", Label: "Integrate", Kind: "leader"},
			{ID: "ver", Label: "Verify", Kind: "task", Brief: "Run tests / review."},
			{ID: "wrap", Label: "Wrap up", Kind: "leader"},
		},
		Edges: []FlowEdge{
			{From: "see", To: "break"},
			{From: "break", To: "fe"}, {From: "break", To: "be"},
			{From: "fe", To: "int"}, {From: "be", To: "int"},
			{From: "int", To: "ver"}, {From: "ver", To: "wrap"},
			{From: "ver", To: "fe", Kind: "bounce"},
		},
	}
}

func TestProjectGraph_DefaultSeed(t *testing.T) {
	content, outline := ProjectGraph("Default Orchestration Flow", defaultSeedGraph())

	assert.True(t, strings.HasPrefix(content, "# Default Orchestration Flow\n"))
	assert.Contains(t, content, "You are the Leader.")
	// 并行组
	assert.Contains(t, content, "In parallel:")
	assert.Contains(t, content, "- Frontend — dispatch: Build the UI.")
	assert.Contains(t, content, "- Backend — dispatch: Build the API.")
	assert.Contains(t, content, "isolate=true")
	// 打回边挂在 Verify
	assert.Contains(t, content, "On fail → send back to Frontend (no new node).")
	// sink → finish
	assert.Contains(t, content, "finish with a summary @user")
	// 顺序：See members 在 Frontend 之前
	assert.Less(t, strings.Index(content, "See members"), strings.Index(content, "Frontend"))
	// outline 为各层代表
	assert.Equal(t, []string{"See members", "Break down", "Frontend ∥ …", "Integrate", "Verify", "Wrap up"}, outline)
}

func TestParseFlowGraph_EmptyOrInvalid(t *testing.T) {
	_, ok := ParseFlowGraph("")
	assert.False(t, ok)
	_, ok = ParseFlowGraph("not json")
	assert.False(t, ok)
	g, ok := ParseFlowGraph(`{"version":1,"nodes":[{"id":"a","label":"A","kind":"leader"}],"edges":[]}`)
	assert.True(t, ok)
	assert.Len(t, g.Nodes, 1)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/workflow_svc/ -run 'TestProjectGraph|TestParseFlowGraph' -v`
Expected: FAIL（`undefined: ProjectGraph` / `FlowGraph` 等）

- [ ] **Step 3: Write minimal implementation**

```go
// Package workflow_svc — projection.go：DAG(graph) → 注入 Leader 的散文提示词（唯一真源在 graph）。
package workflow_svc

import (
	"encoding/json"
	"strconv"
	"strings"
)

// FlowNode 流程图节点。Kind: "task"(委派,含 Brief) | "leader"(Leader 自己做)。
type FlowNode struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Kind        string `json:"kind"`
	Brief       string `json:"brief,omitempty"`
	SharedFiles bool   `json:"sharedFiles,omitempty"`
}

// FlowEdge 流程图边。Kind: ""(sequence) | "bounce"(fail 打回)。
type FlowEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind,omitempty"`
}

// FlowGraph 流程 DAG（workflows.graph 的 JSON 真源）。
type FlowGraph struct {
	Version int        `json:"version"`
	Nodes   []FlowNode `json:"nodes"`
	Edges   []FlowEdge `json:"edges"`
}

// ParseFlowGraph 解析 graph JSON；空/非法/无节点 → (zero,false)。
func ParseFlowGraph(s string) (FlowGraph, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return FlowGraph{}, false
	}
	var g FlowGraph
	if err := json.Unmarshal([]byte(s), &g); err != nil || len(g.Nodes) == 0 {
		return FlowGraph{}, false
	}
	return g, true
}

// ProjectGraph 把流程 DAG 确定性投影成 (content, outline)。
// content = 注入 Leader 的散文；outline = 各层代表 label（仅展示）。
func ProjectGraph(name string, g FlowGraph) (string, []string) {
	byID := make(map[string]FlowNode, len(g.Nodes))
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	seqPreds := map[string][]string{}
	seqSucc := map[string]int{}
	bounceTo := map[string]string{}
	for _, e := range g.Edges {
		if e.Kind == "bounce" {
			bounceTo[e.From] = byID[e.To].Label
			continue
		}
		seqPreds[e.To] = append(seqPreds[e.To], e.From)
		seqSucc[e.From]++
	}

	layer := map[string]int{}
	var depth func(id string, guard int) int
	depth = func(id string, guard int) int {
		if v, ok := layer[id]; ok {
			return v
		}
		best := 0
		if guard < 256 {
			for _, p := range seqPreds[id] {
				if d := depth(p, guard+1) + 1; d > best {
					best = d
				}
			}
		}
		layer[id] = best
		return best
	}
	maxLayer := 0
	for _, n := range g.Nodes {
		if d := depth(n.ID, 0); d > maxLayer {
			maxLayer = d
		}
	}
	layers := make([][]FlowNode, maxLayer+1)
	for _, n := range g.Nodes {
		layers[layer[n.ID]] = append(layers[layer[n.ID]], n)
	}

	var b strings.Builder
	var outline []string
	b.WriteString("# " + name + "\n")
	b.WriteString("You are the Leader. Every result returns to you; you decide the next move.\n")
	step := 0
	for _, grp := range layers {
		if len(grp) == 0 {
			continue
		}
		step++
		num := strconv.Itoa(step)
		if len(grp) == 1 {
			n := grp[0]
			outline = append(outline, n.Label)
			b.WriteString("\n" + num + ". " + singleLine(n, seqSucc[n.ID] == 0) + "\n")
			if to, ok := bounceTo[n.ID]; ok {
				b.WriteString("   On fail → send back to " + to + " (no new node).\n")
			}
			continue
		}
		outline = append(outline, grp[0].Label+" ∥ …")
		b.WriteString("\n" + num + ". In parallel:\n")
		shared := false
		for _, n := range grp {
			brief := n.Brief
			if brief == "" {
				brief = n.Label
			}
			b.WriteString("   - " + n.Label + " — dispatch: " + brief + "\n")
			shared = shared || n.SharedFiles
			if to, ok := bounceTo[n.ID]; ok {
				b.WriteString("     On fail → send back to " + to + " (no new node).\n")
			}
		}
		if shared {
			b.WriteString("   (use isolate=true if they touch the same files)\n")
		}
	}
	return b.String(), outline
}

func singleLine(n FlowNode, isSink bool) string {
	if isSink {
		return n.Label + " — finish with a summary @user."
	}
	if n.Kind == "task" {
		brief := n.Brief
		if brief == "" {
			brief = n.Label
		}
		return "Dispatch to the " + n.Label + " role: " + brief
	}
	if n.Brief != "" {
		return n.Label + " — " + n.Brief
	}
	return n.Label
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/workflow_svc/ -run 'TestProjectGraph|TestParseFlowGraph' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/service/workflow_svc/projection.go internal/service/workflow_svc/projection_test.go
git commit internal/service/workflow_svc/projection.go internal/service/workflow_svc/projection_test.go -m "✨ workflow: graph→prose 投影纯函数(ProjectGraph/ParseFlowGraph)"
```

---

## Task 2: 迁移 + entity 字段 + seed 默认流程

**Files:**
- Create: `migrations/202607040001_workflow_graph_default.go`
- Modify: `migrations/migrations.go`（追加到 `migrationList()` 末尾）
- Modify: `internal/model/entity/workflow_entity/workflow.go`
- Test: `migrations/202607040001_workflow_graph_default_test.go`

**Interfaces:**
- Consumes: Task 1 的 graph 形状（seed 的 `graph` 列存 JSON、`content` 列存其投影产物）。
- Produces: `workflows.graph`/`workflows.is_default` 两列 + 一行 `is_default=1` 默认流程；entity 字段 `Graph string` / `IsDefault int`。

- [ ] **Step 1: Write the failing test**

```go
package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607040001_SeedsDefaultFlow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	var count int64
	assert.NoError(t, db.Table("workflows").Where("is_default = 1").Count(&count).Error)
	assert.Equal(t, int64(1), count)

	var row struct {
		Name    string
		Content string
		Graph   string
	}
	assert.NoError(t, db.Table("workflows").Where("is_default = 1").Scan(&row).Error)
	assert.Equal(t, "Default Orchestration Flow", row.Name)
	assert.Contains(t, row.Content, "finish with a summary @user")
	assert.Contains(t, row.Graph, "\"kind\":\"task\"")
}
```

> 说明：`RunMigrations` 跑全部迁移（含新加的）；用内存 sqlite（迁移自测是 sqlmock 规则的既有例外）。若包内无 `sqlite` 依赖，参照同目录既有 `*_test.go` 的 import。

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./migrations/ -run TestMigration202607040001 -v`
Expected: FAIL（`migration202607040001` 未在 list / 表无该行）

- [ ] **Step 3: Write minimal implementation**

先给 entity 加字段（`internal/model/entity/workflow_entity/workflow.go`，在 `Outline` 后）：

```go
	Outline    string `gorm:"column:outline;type:text;not null;default:'[]'"` // JSON []string,仅展示,不注入
	Graph      string `gorm:"column:graph;type:text;not null;default:''"`     // 流程 DAG 的 JSON 真源(空=adhoc/无图)
	IsDefault  int    `gorm:"column:is_default;type:int;not null;default:0"`  // 1=内置默认流程(仿 system_badge)
```

新建迁移 `migrations/202607040001_workflow_graph_default.go`：

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607040001 给 workflows 加 graph/is_default 两列，并 seed 内置「Default Orchestration Flow」。
// graph = 流程 DAG 的 JSON 真源；content = 其确定性投影(注入 Leader)。二者手写保持一致。
func migration202607040001() *gormigrate.Migration {
	const graph = `{"version":1,"nodes":[` +
		`{"id":"see","label":"See members","kind":"leader"},` +
		`{"id":"break","label":"Break down","kind":"leader"},` +
		`{"id":"fe","label":"Frontend","kind":"task","brief":"Build the UI per the spec. Acceptance: renders, states covered.","sharedFiles":true},` +
		`{"id":"be","label":"Backend","kind":"task","brief":"Build the API per the spec. Acceptance: endpoints + tests."},` +
		`{"id":"int","label":"Integrate","kind":"leader"},` +
		`{"id":"ver","label":"Verify","kind":"task","brief":"Run review / tests. Acceptance: all pass, no regressions."},` +
		`{"id":"wrap","label":"Wrap up","kind":"leader"}` +
		`],"edges":[` +
		`{"from":"see","to":"break"},{"from":"break","to":"fe"},{"from":"break","to":"be"},` +
		`{"from":"fe","to":"int"},{"from":"be","to":"int"},{"from":"int","to":"ver"},` +
		`{"from":"ver","to":"wrap"},{"from":"ver","to":"fe","kind":"bounce"}` +
		`]}`

	const content = "# Default Orchestration Flow\n" +
		"You are the Leader. Every result returns to you; you decide the next move.\n\n" +
		"1. See members\n\n" +
		"2. Break down\n\n" +
		"3. In parallel:\n" +
		"   - Frontend — dispatch: Build the UI per the spec. Acceptance: renders, states covered.\n" +
		"   - Backend — dispatch: Build the API per the spec. Acceptance: endpoints + tests.\n" +
		"   (use isolate=true if they touch the same files)\n\n" +
		"4. Integrate\n\n" +
		"5. Dispatch to the Verify role: Run review / tests. Acceptance: all pass, no regressions.\n" +
		"   On fail → send back to Frontend (no new node).\n\n" +
		"6. Wrap up — finish with a summary @user.\n"

	const tags = `["Default","General"]`
	const outline = `["See members","Break down","Frontend ∥ …","Integrate","Verify","Wrap up"]`

	return &gormigrate.Migration{
		ID: "202607040001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE workflows ADD COLUMN graph TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE workflows ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO workflows (name, content, tags, outline, graph, is_default, status, createtime, updatetime)
SELECT ?, ?, ?, ?, ?, 1, 1,
	CAST(strftime('%s','now') AS INTEGER) * 1000,
	CAST(strftime('%s','now') AS INTEGER) * 1000
WHERE NOT EXISTS (SELECT 1 FROM workflows WHERE is_default = 1)`,
				"Default Orchestration Flow", content, tags, outline, graph).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DELETE FROM workflows WHERE is_default = 1`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE workflows DROP COLUMN is_default`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE workflows DROP COLUMN graph`).Error
		},
	}
}
```

在 `migrations/migrations.go` 的 `migrationList()` 末尾追加：

```go
		migration202607030002(), // 回报分层:orch_tasks.summary
		migration202607040001(), // workflows.graph/is_default + seed 默认流程
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./migrations/ -run TestMigration202607040001 -v`
Expected: PASS

- [ ] **Step 5: Run full migrations test to confirm no ordering breakage**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./migrations/... ./internal/bootstrap/...`
Expected: PASS（含既有 `cago_test.go` 全量迁移）

- [ ] **Step 6: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add migrations/202607040001_workflow_graph_default.go migrations/202607040001_workflow_graph_default_test.go migrations/migrations.go internal/model/entity/workflow_entity/workflow.go
git commit migrations/202607040001_workflow_graph_default.go migrations/202607040001_workflow_graph_default_test.go migrations/migrations.go internal/model/entity/workflow_entity/workflow.go -m "✨ workflow: 加 graph/is_default 列 + seed 内置默认流程"
```

---

## Task 3: workflow_svc DTO + Create/Update 投影覆写

**Files:**
- Modify: `internal/service/workflow_svc/types.go`
- Modify: `internal/service/workflow_svc/workflow.go`
- Test: `internal/service/workflow_svc/workflow_test.go`

**Interfaces:**
- Consumes: `ProjectGraph`（Task 1）、entity `Graph`/`IsDefault`（Task 2）。
- Produces: `WorkflowItem{... Graph string; IsDefault bool}`；`Create/UpdateWorkflowRequest{... Graph string}`；Create/Update 传非空 `Graph` 时 `content`/`outline` 被投影覆写。

- [ ] **Step 1: Write the failing test**（追加到 `workflow_test.go`）

```go
func TestCreateWorkflow_ProjectsGraphIntoContent(t *testing.T) {
	convey.Convey("Create 传 graph → content/outline 被投影覆写", t, func() {
		ctx, wfMock, _, svc := setupSvc(t)
		var saved *workflow_entity.Workflow
		wfMock.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, w *workflow_entity.Workflow) error { saved = w; w.ID = 5; return nil },
		)
		graph := `{"version":1,"nodes":[{"id":"a","label":"Plan","kind":"leader"},{"id":"b","label":"Do","kind":"task","brief":"do it"}],"edges":[{"from":"a","to":"b"}]}`
		resp, err := svc.Create(ctx, &CreateWorkflowRequest{Name: "F", Content: "ignored user text", Graph: graph})
		assert.NoError(t, err)
		assert.Contains(t, saved.Content, "# F")
		assert.Contains(t, saved.Content, "finish with a summary @user") // sink=Do
		assert.NotEqual(t, "ignored user text", saved.Content)            // 图存在时投影覆写
		assert.Equal(t, graph, saved.Graph)
		assert.Contains(t, resp.Item.Content, "# F")
	})
}

func TestListWorkflows_ExposesDefaultAndGraph(t *testing.T) {
	convey.Convey("List DTO 带 isDefault/graph", t, func() {
		ctx, wfMock, runMock, svc := setupSvc(t)
		wfMock.EXPECT().List(gomock.Any()).Return([]*workflow_entity.Workflow{
			{ID: 1, Name: "D", Content: "# D", Graph: `{"version":1}`, IsDefault: 1, Status: 1},
		}, nil)
		runMock.EXPECT().List(gomock.Any()).Return(nil, nil)
		resp, err := svc.List(ctx, &ListWorkflowsRequest{})
		assert.NoError(t, err)
		assert.True(t, resp.Items[0].IsDefault)
		assert.Equal(t, `{"version":1}`, resp.Items[0].Graph)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/workflow_svc/ -run 'TestCreateWorkflow_ProjectsGraph|TestListWorkflows_ExposesDefault' -v`
Expected: FAIL（`Graph`/`IsDefault` 未定义 / content 未投影）

- [ ] **Step 3: Write minimal implementation**

`types.go` — `WorkflowItem` 加两字段，`Create/UpdateWorkflowRequest` 加 `Graph`：

```go
type WorkflowItem struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags"`
	Outline    []string `json:"outline"`
	Graph      string   `json:"graph"`
	IsDefault  bool     `json:"isDefault"`
	RunCount   int      `json:"runCount"`
	Createtime int64    `json:"createtime"`
	Updatetime int64    `json:"updatetime"`
}
```

在 `CreateWorkflowRequest` 与 `UpdateWorkflowRequest` 各加一行：

```go
	Graph   string   `json:"graph"`
```

`workflow.go` — `toItem` 带新字段：

```go
func toItem(w *workflow_entity.Workflow, runCount int) *WorkflowItem {
	return &WorkflowItem{
		ID:         w.ID,
		Name:       w.Name,
		Content:    w.Content,
		Tags:       decodeStringList(w.Tags),
		Outline:    decodeStringList(w.Outline),
		Graph:      w.Graph,
		IsDefault:  w.IsDefault == 1,
		RunCount:   runCount,
		Createtime: w.Createtime,
		Updatetime: w.Updatetime,
	}
}
```

新增一个私有 helper（`workflow.go`），Create/Update 复用——图存在时投影覆写 content/outline：

```go
// applyGraph 若 req 带合法 graph，则 graph 为真源：投影覆写 content/outline，并回存 graph JSON。
func applyGraph(w *workflow_entity.Workflow, graph string) {
	w.Graph = strings.TrimSpace(graph)
	if g, ok := ParseFlowGraph(w.Graph); ok {
		content, outline := ProjectGraph(w.Name, g)
		w.Content = content
		w.Outline = encodeStringList(outline)
	}
}
```

`Create` 中，构造 `w` 后、`Check` 前调用（注意 `w.Name` 需已 trim）：

```go
	w := &workflow_entity.Workflow{
		Name:    strings.TrimSpace(req.Name),
		Content: req.Content,
		Tags:    encodeStringList(req.Tags),
		Outline: encodeStringList(req.Outline),
		Status:  consts.ACTIVE,
	}
	applyGraph(w, req.Graph)
	if err := w.Check(ctx); err != nil {
		return nil, err
	}
```

`Update` 中，赋完 `w.Name`/`w.Content`/`w.Tags`/`w.Outline` 后加：

```go
	applyGraph(w, req.Graph)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/workflow_svc/ -v`
Expected: PASS（含既有 List/Create/Update 用例）

- [ ] **Step 5: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/service/workflow_svc/types.go internal/service/workflow_svc/workflow.go internal/service/workflow_svc/workflow_test.go
git commit internal/service/workflow_svc/types.go internal/service/workflow_svc/workflow.go internal/service/workflow_svc/workflow_test.go -m "✨ workflow: DTO 暴露 graph/isDefault + Create/Update 图为真源投影覆写 content"
```

---

## Task 4: 修复库模式注入缺口（CreateRun 快照 content）

**Files:**
- Modify: `internal/service/orch_svc/deps.go`（加 `WorkflowReader` 接口）
- Modify: `internal/service/orch_svc/orch.go`（`wf` 字段 + `RegisterWorkflowReader`）
- Modify: `internal/service/orch_svc/create.go`
- Modify: `internal/app/app.go`（adapter + 注册）
- Test: `internal/service/orch_svc/create_test.go`
- 生成: `internal/service/orch_svc/mock_orch_svc/mock_deps.go`（`make mock`）

**Interfaces:**
- Consumes: workflow entity（经 adapter 读 `w.Content`）。
- Produces: `orch_svc.WorkflowReader interface { FlowContentByID(ctx, id int64) (string, error) }`；`(*orchSvc).RegisterWorkflowReader(WorkflowReader)`；`CreateRun` 在 `FlowID>0 && FlowContent==""` 时快照 content 进 `run.FlowContent`。

- [ ] **Step 1: Write the failing test**（追加到 `create_test.go`）

```go
func TestCreateRun_LibraryModeSnapshotsFlowContent(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	wf := mock_orch_svc.NewMockWorkflowReader(ctrl)

	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)
	orch_svc.Default().RegisterWorkflowReader(wf)

	agents.EXPECT().Find(gomock.Any(), int64(2)).Return(&agent_entity.Agent{ID: 2, Name: "L"}, nil)
	wf.EXPECT().FlowContentByID(gomock.Any(), int64(9)).Return("# Flow\nprojected body", nil)

	var savedFlow string
	runs.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		savedFlow = r.FlowContent
		r.ID = 100
		return nil
	})
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).Return(int64(500), nil)
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error { tk.ID = 9; return nil })
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).Return(nil)

	Convey("库模式(只传 FlowID)→ 快照 workflow.content 进 run.FlowContent", t, func() {
		_, err := orch_svc.Default().CreateRun(context.Background(), &orch_svc.CreateRunRequest{
			Goal: "g", LeaderAgentID: 2, FlowID: 9,
		})
		So(err, ShouldBeNil)
		So(savedFlow, ShouldEqual, "# Flow\nprojected body")
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/orch_svc/... -run TestCreateRun_LibraryModeSnapshots -v`
Expected: FAIL（`MockWorkflowReader`/`RegisterWorkflowReader` 未定义）——**这正是缺口证据**。

- [ ] **Step 3: Write minimal implementation**

`deps.go` 末尾加接口（在 mockgen source 内，会被 `make mock` 生成）：

```go
// WorkflowReader 编排对流程库的最小依赖：按 ID 取已投影的流程正文(用于 CreateRun 快照进 run.FlowContent)。
type WorkflowReader interface {
	FlowContentByID(ctx context.Context, id int64) (string, error)
}
```

`orch.go` — `orchSvc` struct 加字段（与 `chat`/`agents` 并列）：

```go
	wf WorkflowReader
```

并加注册方法（`RegisterDeps` 附近）：

```go
// RegisterWorkflowReader 注入流程库读取器(bootstrap/app 接线)；测试注 mock。
func (s *orchSvc) RegisterWorkflowReader(wr WorkflowReader) { s.wf = wr }
```

`create.go` — 在构造 `run` 之前解析 FlowContent 快照：

```go
	allowed := marshalAllowedAgentIDs(req.AllowedAgentIDs)

	// 库模式(只传 FlowID、未直传 FlowContent)→ 快照该流程已投影的正文进 run.FlowContent，
	// 否则 turn.go 只读 run.FlowContent、库流程将注入为空。adhoc 直传 FlowContent 时跳过。
	flowContent := req.FlowContent
	if flowContent == "" && req.FlowID > 0 && s.wf != nil {
		if c, err := s.wf.FlowContentByID(ctx, req.FlowID); err == nil {
			flowContent = c
		} else {
			logger.Ctx(ctx).Warn("orch.CreateRun: 取流程正文失败,按无流程继续", zap.Int64("flow", req.FlowID), zap.Error(err))
		}
	}

	run := &orch_entity.OrchestrationRun{
		Goal: req.Goal, LeaderAgentID: req.LeaderAgentID,
		FlowID: req.FlowID, FlowContent: flowContent,
		ProjectID: req.ProjectID, Status: orch_entity.RunRunning,
		AllowedAgentIDs: allowed,
	}
```

`app.go` — 在 `orch_svc.Default().RegisterDeps(...)` 之后加 adapter + 注册：

```go
	orch_svc.Default().RegisterWorkflowReader(orchWorkflowAdapter{})
```

adapter（放 app.go 里既有 orch adapter 附近，或同文件底部）：

```go
// orchWorkflowAdapter 让 orch_svc 经 workflow_repo 取已投影的流程正文(软删/不存在 → 空)。
type orchWorkflowAdapter struct{}

func (orchWorkflowAdapter) FlowContentByID(ctx context.Context, id int64) (string, error) {
	w, err := workflow_repo.Workflow().Find(ctx, id)
	if err != nil {
		return "", err
	}
	if w == nil || !w.IsActive() {
		return "", nil
	}
	return w.Content, nil
}
```

（确认 `app.go` 已 import `workflow_repo`；未导入则加 `"github.com/agentre-ai/agentre/internal/repository/workflow_repo"`。）

- [ ] **Step 4: Regenerate mocks**

Run: `cd /Users/codfrm/Code/agentre/agentre && make mock`
Expected: `mock_orch_svc/mock_deps.go` 新增 `MockWorkflowReader`

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/orch_svc/...`
Expected: PASS（含既有 CreateRun 用例 + 新回归）

- [ ] **Step 6: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/service/orch_svc/deps.go internal/service/orch_svc/orch.go internal/service/orch_svc/create.go internal/service/orch_svc/create_test.go internal/service/orch_svc/mock_orch_svc/mock_deps.go internal/app/app.go
git commit internal/service/orch_svc/deps.go internal/service/orch_svc/orch.go internal/service/orch_svc/create.go internal/service/orch_svc/create_test.go internal/service/orch_svc/mock_orch_svc/mock_deps.go internal/app/app.go -m "🐛 orchestration: 库模式快照流程正文进 run.FlowContent(修注入为空缺口)"
```

---

## Task 5: 绑定 WorkflowPreviewGraph + 刷新前端绑定

**Files:**
- Modify: `internal/app/workflow.go`
- 生成: `frontend/wailsjs/go/app/App.{js,d.ts}` + `frontend/wailsjs/go/models.ts`（`make generate`）
- Test: `internal/app/workflow_test.go`（新建或追加，若无则最小验证）

**Interfaces:**
- Consumes: `ProjectGraph`/`ParseFlowGraph`（Task 1）。
- Produces: `App.WorkflowPreviewGraph(req) -> {content, outline}` 给设计器实时预览；`WorkflowItem` 绑定含 `graph`/`isDefault`（供前端）。

- [ ] **Step 1: Write the failing test**

```go
package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorkflowPreviewGraph(t *testing.T) {
	a := &App{}
	resp, err := a.WorkflowPreviewGraph(&WorkflowPreviewRequest{
		Name:  "P",
		Graph: `{"version":1,"nodes":[{"id":"a","label":"Do","kind":"task","brief":"x"}],"edges":[]}`,
	})
	assert.NoError(t, err)
	assert.Contains(t, resp.Content, "# P")
	assert.Contains(t, resp.Content, "finish with a summary @user")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/app/ -run TestWorkflowPreviewGraph -v`
Expected: FAIL（`WorkflowPreviewGraph`/`WorkflowPreviewRequest` 未定义）

- [ ] **Step 3: Write minimal implementation**（`internal/app/workflow.go`）

```go
// WorkflowPreviewRequest 设计器实时预览入参（未落库的草稿 graph）。
type WorkflowPreviewRequest struct {
	Name  string `json:"name"`
	Graph string `json:"graph"`
}

// WorkflowPreviewResponse 投影结果（content 即将注入 Leader 的正文；outline 仅展示）。
type WorkflowPreviewResponse struct {
	Content string   `json:"content"`
	Outline []string `json:"outline"`
}

// WorkflowPreviewGraph 把草稿 graph 投影成正文/大纲，供 DAG 设计器实时预览（投影只有后端一份实现）。
func (a *App) WorkflowPreviewGraph(req *WorkflowPreviewRequest) (*WorkflowPreviewResponse, error) {
	g, ok := workflow_svc.ParseFlowGraph(req.Graph)
	if !ok {
		return &WorkflowPreviewResponse{}, nil
	}
	content, outline := workflow_svc.ProjectGraph(req.Name, g)
	return &WorkflowPreviewResponse{Content: content, Outline: outline}, nil
}
```

- [ ] **Step 4: Run test + regenerate bindings**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/app/ -run TestWorkflowPreviewGraph && make generate`
Expected: 测试 PASS；`WorkflowItem` 在 `models.ts` 出现 `graph`/`isDefault`，`App.d.ts` 出现 `WorkflowPreviewGraph`

- [ ] **Step 5: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/app/workflow.go internal/app/workflow_test.go frontend/wailsjs/go/app/App.js frontend/wailsjs/go/app/App.d.ts frontend/wailsjs/go/models.ts
git commit internal/app/workflow.go internal/app/workflow_test.go frontend/wailsjs/go/app/App.js frontend/wailsjs/go/app/App.d.ts frontend/wailsjs/go/models.ts -m "✨ workflow: WorkflowPreviewGraph 绑定 + 刷新 graph/isDefault 绑定"
```

---

## Task 6: 前端 flow-graph.ts + FlowGraphView 只读组件

**Files:**
- Create: `frontend/src/components/agentre/orchestration/flow-graph.ts`
- Create: `frontend/src/components/agentre/orchestration/flow-graph-view.tsx`
- Test: `frontend/src/components/agentre/orchestration/__tests__/flow-graph.test.ts`

**Interfaces:**
- Produces:
  - `type FlowGraph = { version: number; nodes: FlowNode[]; edges: FlowEdge[] }`（`FlowNode={id,label,kind:"task"|"leader",brief?,sharedFiles?}`，`FlowEdge={from,to,kind?:"bounce"}`）
  - `parseFlowGraph(json?: string): FlowGraph | null`
  - `layoutFlowGraph(g: FlowGraph): { placed: Array<{node:FlowNode; col:number; row:number}>; cols:number; rows:number }`（col=最长路径层，row=层内序）
  - `<FlowGraphView graph={...} />`（只读 mini-DAG）

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { parseFlowGraph, layoutFlowGraph } from "../flow-graph";

const seed = JSON.stringify({
  version: 1,
  nodes: [
    { id: "see", label: "See", kind: "leader" },
    { id: "break", label: "Break", kind: "leader" },
    { id: "fe", label: "FE", kind: "task" },
    { id: "be", label: "BE", kind: "task" },
    { id: "wrap", label: "Wrap", kind: "leader" },
  ],
  edges: [
    { from: "see", to: "break" },
    { from: "break", to: "fe" },
    { from: "break", to: "be" },
    { from: "fe", to: "wrap" },
    { from: "be", to: "wrap" },
  ],
});

describe("flow-graph", () => {
  it("parseFlowGraph 非法/空返回 null", () => {
    expect(parseFlowGraph("")).toBeNull();
    expect(parseFlowGraph("nope")).toBeNull();
    expect(parseFlowGraph(seed)?.nodes.length).toBe(5);
  });

  it("layoutFlowGraph 按最长路径分层, 并行节点同列不同行", () => {
    const g = parseFlowGraph(seed)!;
    const { placed, cols } = layoutFlowGraph(g);
    const col = (id: string) => placed.find((p) => p.node.id === id)!.col;
    expect(col("see")).toBe(0);
    expect(col("break")).toBe(1);
    expect(col("fe")).toBe(2);
    expect(col("be")).toBe(2);
    expect(col("wrap")).toBe(3);
    // fe/be 并行 → 同列不同行
    const feRow = placed.find((p) => p.node.id === "fe")!.row;
    const beRow = placed.find((p) => p.node.id === "be")!.row;
    expect(feRow).not.toBe(beRow);
    expect(cols).toBe(4);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- flow-graph.test.ts`
Expected: FAIL（模块不存在）

- [ ] **Step 3: Write minimal implementation**

`flow-graph.ts`：

```ts
export type FlowKind = "task" | "leader";
export interface FlowNode {
  id: string;
  label: string;
  kind: FlowKind;
  brief?: string;
  sharedFiles?: boolean;
}
export interface FlowEdge {
  from: string;
  to: string;
  kind?: "bounce";
}
export interface FlowGraph {
  version: number;
  nodes: FlowNode[];
  edges: FlowEdge[];
}

export function parseFlowGraph(json?: string): FlowGraph | null {
  if (!json || !json.trim()) return null;
  try {
    const g = JSON.parse(json) as FlowGraph;
    if (!g || !Array.isArray(g.nodes) || g.nodes.length === 0) return null;
    if (!Array.isArray(g.edges)) g.edges = [];
    return g;
  } catch {
    return null;
  }
}

export interface Placed {
  node: FlowNode;
  col: number;
  row: number;
}

// layoutFlowGraph: col = 最长路径层(仅 sequence 边), row = 层内出现序。
export function layoutFlowGraph(g: FlowGraph): {
  placed: Placed[];
  cols: number;
  rows: number;
} {
  const preds = new Map<string, string[]>();
  for (const e of g.edges) {
    if (e.kind === "bounce") continue;
    preds.set(e.to, [...(preds.get(e.to) ?? []), e.from]);
  }
  const layer = new Map<string, number>();
  const depth = (id: string, guard = 0): number => {
    const cached = layer.get(id);
    if (cached !== undefined) return cached;
    let best = 0;
    if (guard < 256) {
      for (const p of preds.get(id) ?? []) best = Math.max(best, depth(p, guard + 1) + 1);
    }
    layer.set(id, best);
    return best;
  };
  const rowCounter = new Map<number, number>();
  const placed: Placed[] = g.nodes.map((node) => {
    const col = depth(node.id);
    const row = rowCounter.get(col) ?? 0;
    rowCounter.set(col, row + 1);
    return { node, col, row };
  });
  const cols = placed.reduce((m, p) => Math.max(m, p.col + 1), 0);
  const rows = [...rowCounter.values()].reduce((m, r) => Math.max(m, r), 0);
  return { placed, cols, rows };
}

export function isBounceSource(g: FlowGraph, id: string): boolean {
  return g.edges.some((e) => e.kind === "bounce" && e.from === id);
}
```

`flow-graph-view.tsx`（只读；横向布局，列=层，节点小卡 + 二元 kind 徽章；边用 SVG）：

```tsx
import * as React from "react";
import { cn } from "@/lib/utils";
import {
  type FlowGraph,
  isBounceSource,
  layoutFlowGraph,
  parseFlowGraph,
} from "./flow-graph";

const COL_W = 118;
const ROW_H = 52;
const NODE_W = 96;
const NODE_H = 34;
const PAD = 16;

// kind → 徽章样式(二元 + 派生收口)
function kindBadge(g: FlowGraph, id: string, kind: string, isSink: boolean) {
  if (isSink) return { label: "finish", cls: "text-status-running" };
  if (kind === "task") return { label: "task", cls: "text-primary-text" };
  return { label: "leader", cls: "text-muted-foreground" };
}

export function FlowGraphView({
  graph,
  className,
}: {
  graph?: string | FlowGraph;
  className?: string;
}) {
  const g = typeof graph === "string" ? parseFlowGraph(graph) : (graph ?? null);
  if (!g) return null;
  const { placed, cols, rows } = layoutFlowGraph(g);
  const width = PAD * 2 + cols * COL_W;
  const height = PAD * 2 + (rows + 1) * ROW_H;
  const pos = new Map(
    placed.map((p) => [
      p.node.id,
      {
        x: PAD + p.col * COL_W,
        y: PAD + p.row * ROW_H,
      },
    ]),
  );
  const cx = (id: string) => (pos.get(id)?.x ?? 0) + NODE_W / 2;
  const cy = (id: string) => (pos.get(id)?.y ?? 0) + NODE_H / 2;

  return (
    <div
      className={cn("relative overflow-auto rounded-lg border border-border bg-card/40", className)}
      style={{ width: "100%", height }}
    >
      <svg width={width} height={height} className="absolute inset-0">
        {g.edges.map((e, i) => {
          const bounce = e.kind === "bounce";
          return (
            <line
              key={i}
              x1={cx(e.from)}
              y1={cy(e.from)}
              x2={cx(e.to)}
              y2={cy(e.to)}
              stroke={bounce ? "var(--color-status-waiting)" : "var(--color-primary)"}
              strokeWidth={1.5}
            />
          );
        })}
      </svg>
      {placed.map((p) => {
        const isSink = !g.edges.some((e) => e.kind !== "bounce" && e.from === p.node.id);
        const badge = kindBadge(g, p.node.id, p.node.kind, isSink);
        return (
          <div
            key={p.node.id}
            className="absolute flex flex-col justify-center rounded-md border border-border bg-card px-2 py-1"
            style={{ left: pos.get(p.node.id)!.x, top: pos.get(p.node.id)!.y, width: NODE_W, height: NODE_H }}
            title={p.node.brief}
            data-testid={`flow-node-${p.node.id}`}
          >
            <span className="truncate text-2xs font-medium text-foreground">{p.node.label}</span>
            <span className={cn("text-[9px]", badge.cls)}>
              {badge.label}
              {isBounceSource(g, p.node.id) ? " · fail↩" : ""}
            </span>
          </div>
        );
      })}
    </div>
  );
}
```

> 说明：颜色用现有 CSS 变量（见 `docs/DESIGN.md` 的 `--color-primary`/`--color-status-*`）；`text-2xs` 若无则用 `text-[10px]`。此组件**不含 i18n 文案**（label/brief 是动态数据，`task/leader/finish` 是技术标签非 UI 文案）。

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- flow-graph.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/orchestration/flow-graph.ts frontend/src/components/agentre/orchestration/flow-graph-view.tsx frontend/src/components/agentre/orchestration/__tests__/flow-graph.test.ts
git commit frontend/src/components/agentre/orchestration/flow-graph.ts frontend/src/components/agentre/orchestration/flow-graph-view.tsx frontend/src/components/agentre/orchestration/__tests__/flow-graph.test.ts -m "✨ orchestration: FlowGraphView 只读 mini-DAG + layoutFlowGraph 纯函数"
```

---

## Task 7: 新建 Run 弹窗改造（去 none / 默认 library / 预选默认 / mini-DAG 预览）

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/run-new-dialog.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`、`frontend/src/i18n/locales/en/common.json`
- Test: `frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`

**Interfaces:**
- Consumes: `FlowGraphView`（Task 6）；`WorkflowList` 现返回 `graph`/`isDefault`（Task 5）。

- [ ] **Step 1: Write the failing test**（替换旧的「三态 none/library/adhoc」相关用例）

删除 `默认显示三个 flowMode 按钮(none/library/adhoc)` 与 `点击 none 按钮后不显示 picker...` 两个用例，新增：

```ts
    it("不再有「从零开始」段, 只有 library/adhoc 两段", async () => {
      renderDialog();
      await screen.findByTestId("run-goal");
      expect(screen.queryByTestId("run-flow-mode-none")).toBeNull();
      expect(screen.getByTestId("run-flow-mode-library")).toBeInTheDocument();
      expect(screen.getByTestId("run-flow-mode-adhoc")).toBeInTheDocument();
    });

    it("默认落在 library 并预选 isDefault 流程 → RunCreate 带该 flowId", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      appMocks.WorkflowList.mockResolvedValue({
        items: [
          { id: 1, name: "Custom", tags: [], outline: [], graph: "", isDefault: false },
          {
            id: 2,
            name: "Default Orchestration Flow",
            tags: ["Default"],
            outline: [],
            graph: JSON.stringify({
              version: 1,
              nodes: [{ id: "a", label: "See", kind: "leader" }],
              edges: [],
            }),
            isDefault: true,
          },
        ],
      });
      renderDialog();
      await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
      // 预选默认流程 → 渲染其 mini-DAG(flow-node-a)
      expect(await screen.findByTestId("flow-node-a")).toBeInTheDocument();
      fireEvent.change(screen.getByTestId("run-goal"), { target: { value: "g" } });
      await user.click(screen.getByTestId("run-leader"));
      await user.click(await screen.findByRole("option", { name: "架构师" }));
      await user.click(screen.getByTestId("run-create"));
      await waitFor(() =>
        expect(appMocks.RunCreate).toHaveBeenCalledWith(
          expect.objectContaining({ flowId: 2 }),
        ),
      );
    });
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- run-new-dialog.test.tsx`
Expected: FAIL（仍有 none 段 / 未预选 / 无 flow-node）

- [ ] **Step 3: Write minimal implementation**

`run-new-dialog.tsx` 改动：

1. `FlowMode` 与段配置去掉 `none`：

```ts
type FlowMode = "library" | "adhoc";

const FLOW_SEGMENTS: { mode: FlowMode; icon: LucideIcon; labelKey: string }[] = [
  { mode: "library", icon: LibraryBig, labelKey: "orchestration.new.flowLibrary" },
  { mode: "adhoc", icon: SquarePen, labelKey: "orchestration.new.flowAdhoc" },
];
```

移除 `Sparkles` import。

2. `WorkflowOption` 加 `graph`/`isDefault`；`WorkflowList` 映射带上：

```ts
type WorkflowOption = {
  id: number;
  name: string;
  tags: string[];
  outline: string[];
  graph: string;
  isDefault: boolean;
};
```

```ts
    WorkflowList()
      .then((resp) => {
        const items = (resp?.items ?? []).map((w) => ({
          id: w.id,
          name: w.name,
          tags: w.tags ?? [],
          outline: w.outline ?? [],
          graph: w.graph ?? "",
          isDefault: w.isDefault ?? false,
        }));
        setWorkflows(items);
        const def = items.find((w) => w.isDefault);
        if (def) setFlowId(def.id);
      })
      .catch(() => setWorkflows([]));
```

3. 初始/重置 flowMode 改默认 `library`：

```ts
  const [flowMode, setFlowMode] = React.useState<FlowMode>("library");
```
以及 `useEffect` 重置块里 `setFlowMode("library");`（原 `setFlowMode("none")`）。

4. 库模式下把旧 outline 面包屑替换为 `FlowGraphView`（import 顶部加 `import { FlowGraphView } from "./flow-graph-view";`）。在 `flowMode === "library"` 块内，选中流程有 graph 时渲染预览：

```tsx
              {selectedWorkflow && selectedWorkflow.graph ? (
                <div data-testid="run-flow-preview" className="flex flex-col gap-1.5">
                  <span className="text-2xs text-subtle-foreground">
                    {t("orchestration.new.flowPreview")}
                  </span>
                  <FlowGraphView graph={selectedWorkflow.graph} />
                </div>
              ) : selectedWorkflow && selectedWorkflow.outline.length > 0 ? (
                /* 无 graph 的老流程回退到 outline 面包屑(保留既有那段 JSX) */
                <span data-testid="run-flow-outline" className="flex flex-wrap items-center gap-1">
                  {/* …既有 outline 面包屑映射保持不变… */}
                </span>
              ) : null}
```

（`submit()` 已按 `flowMode === "library" ? flowId : 0` 传 `flowId`，无需改。）

5. i18n：`zh-CN/common.json` 与 `en/common.json` 的 `orchestration.new` 下**删 `flowNone`**、**加 `flowPreview`**：

zh-CN：
```json
      "flowPreview": "流程预览（DAG）",
```
en：
```json
      "flowPreview": "Flow preview (DAG)",
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- run-new-dialog.test.tsx`
Expected: PASS

- [ ] **Step 5: Run i18n + lint + type gates**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- i18n.test.ts && pnpm exec tsc --noEmit && pnpm exec eslint src/components/agentre/orchestration/`
Expected: PASS（i18n key 覆盖、无类型错误、无 `no-literal-string`）

- [ ] **Step 6: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/orchestration/run-new-dialog.tsx frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit frontend/src/components/agentre/orchestration/run-new-dialog.tsx frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json -m "✨ orchestration: 新建 Run 去「从零开始」+ 默认预选默认流程 + mini-DAG 预览"
```

---

## Task 8: 收尾全量 gate

**Files:** 无新增（仅验证）

- [ ] **Step 1: 后端全量**

Run: `cd /Users/codfrm/Code/agentre/agentre && make test-backend`
Expected: PASS（真实 exit code；勿 `| tail` 吞码）

- [ ] **Step 2: 后端 lint**

Run: `cd /Users/codfrm/Code/agentre/agentre && make lint`
Expected: PASS（golangci-lint v2 + 前端 ESLint）

- [ ] **Step 3: 前端全量 vitest + 类型**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test && pnpm exec tsc --noEmit`
Expected: PASS（全量,含 App.test/foundation.test；勿只跑 focused）

- [ ] **Step 4: 若全绿, 无需额外提交**（各 Task 已分别提交）。若发现跨包回归（如 sqlmock/goroutine-leak flake），按 systematic-debugging 定位后单独补测补修再提交。

---

## Self-Review（对照 spec）

- **spec §2 注入缺口** → Task 4（红→绿回归 + 快照修复）。✅
- **spec §4 数据模型（graph/is_default + JSON schema）** → Task 1(类型) + Task 2(列+seed) + Task 3(DTO)。✅
- **spec §5 graph→prose 投影** → Task 1(纯函数) + Task 5(预览绑定)。✅
- **spec §6 默认流程 English seed** → Task 2 seed。✅
- **spec §7 后端改动** → Task 2/3/4/5 全覆盖（迁移/entity/DTO/svc/orch 快照/binding）。✅
- **spec §8 Phase 1 前端** → Task 6(FlowGraphView) + Task 7(弹窗去 none/默认预选/预览/i18n)。✅
- **Phase 2/3** 明确不在本计划（spec §9）。✅
- **Placeholder 扫描**：无 TBD/TODO；每个 code step 有完整代码。✅
- **类型一致性**：`ProjectGraph`/`ParseFlowGraph`(Go) 与 `parseFlowGraph`/`layoutFlowGraph`(TS) 命名区分清晰；`WorkflowReader.FlowContentByID` 在 deps/orch/create/app/test 一致；DTO `Graph string`/`IsDefault bool` 贯穿 entity→svc→binding→前端 `graph`/`isDefault`。✅
