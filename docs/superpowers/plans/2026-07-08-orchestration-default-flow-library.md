# 编排默认流程库优化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把编排的单一硬编码默认流程换成 4 个内置流程（1 通用 + 3 拓扑预设，各带手写 prompt），删除 `is_default` 列，新建运行改为 sticky 上次选择 + 首个兜底。

**Architecture:** 复用现有 `graph→ProjectGraph→template→content` 管线，但每个内置流程的 `template` 是手写全文（不含 `{{ DAGPrompt }}`），故 `content == template`；`graph` 仅供设计器可视化 + 运行时 overlay，`outline` 仍由 `ProjectGraph` 投影。一支迁移一致性测试锁死 `graph↔template↔content↔outline`。

**Tech Stack:** Go 1.26 + gormigrate + glebarez/sqlite（迁移测试内存库）；React 19 + TypeScript + Vitest + Testing Library；Wails 绑定（gitignore 生成物，`make generate`）。

设计依据：`docs/superpowers/specs/2026-07-08-orchestration-default-flow-library-design.md`。

## Global Constraints

- **提交规范**：gitmoji commit；**共享分支 `develop/wyz` 有并发会话** → 提交必须 `git commit <files>` 带 pathspec，禁止裸 `git commit`。
- **TDD**：先写失败测试、跑一次看它按预期失败，再实现。
- **迁移 append-only**：新迁移追加到 `migrationList()` 末尾，不改已有迁移的 `Migrate`/`Rollback`；DDL 用原生 SQL。
- **无新增可见 UI 文案** → 无 i18n 改动（不碰 `common.json`）。
- **`is_default` 列与 `workflow_entity.IsDefault` 字段强耦合**：DROP COLUMN 与删字段必须同一个提交/任务落地（Task 1），否则 GORM 会 `SELECT` 一个已删列而崩。
- **不要在 Task 1 跑会触发 `wails generate` 的目标**（`make lint` / `make test` / `make test-frontend` 都会 regen 绑定，会提前删掉 TS 侧 `isDefault` 使前端 tsc 断裂）。Task 1 只用 `make test-backend` + 直接调 `gofmt`/`golangci-lint`（这些不 regen）。`make generate` 留到 Task 2 开头。
- **prompt 字符串无反引号** → 迁移里用 Go 反引号原始字符串字面量安全。

## File Structure

**Task 1（后端，原子）**
- Create `migrations/202607080001_workflow_default_presets.go` — 迁移：删旧默认 + 插 4 预设 + DROP is_default；4 个流程的 graph/prompt/tags/outline 字面量。
- Create `migrations/202607080001_workflow_default_presets_test.go` — 一致性测试（import workflow_svc）。
- Modify `migrations/migrations.go` — `migrationList()` 追加 `migration202607080001()`。
- Modify `migrations/202607040001_workflow_graph_default_test.go` — retarget（不再查已删的 is_default）。
- Modify `migrations/202607050001_workflow_template_test.go` — retarget（同上）。
- Modify `internal/model/entity/workflow_entity/workflow.go` — 删 `IsDefault` 字段。
- Modify `internal/service/workflow_svc/types.go` — 删 `WorkflowItem.IsDefault`。
- Modify `internal/service/workflow_svc/workflow.go` — `toItem` 删 `IsDefault` 映射。

**Task 2（前端）**
- Modify `frontend/src/components/agentre/orchestration/run-new-dialog.tsx` — 删 isDefault + sticky 预选 + 提交持久化。
- Modify `frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx` — 重写预选测试 + 加 localStorage 用例。
- Regenerate `frontend/wailsjs/**`（`make generate`，gitignore，不提交）。

**Task 3（集成验证）** — 无代码，全量 gate + 手动 smoke。

---

### Task 1: 后端 — 迁移四内置流程 + 删 is_default

**Files:**
- Create: `migrations/202607080001_workflow_default_presets.go`
- Create: `migrations/202607080001_workflow_default_presets_test.go`
- Modify: `migrations/migrations.go:51`（`migrationList()` 末尾追加）
- Modify: `migrations/202607040001_workflow_graph_default_test.go`（整个函数替换）
- Modify: `migrations/202607050001_workflow_template_test.go`（整个函数替换）
- Modify: `internal/model/entity/workflow_entity/workflow.go:24`
- Modify: `internal/service/workflow_svc/types.go:16`
- Modify: `internal/service/workflow_svc/workflow.go:84`

**Interfaces:**
- Consumes: `workflow_svc.RenderWorkflowContent(name, graph, template string) (content string, outline []string, err error)`（纯函数，无 DB）；`RunMigrations(db *gorm.DB) error`。
- Produces: 迁移后 `workflows` 表含 4 行内置流程、无 `is_default` 列；`workflow_entity.Workflow` 与 `workflow_svc.WorkflowItem` 不再有 `IsDefault`。

- [ ] **Step 1: 写迁移文件（含 4 流程字面量）**

Create `migrations/202607080001_workflow_default_presets.go`：

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607080001 用四个内置流程取代旧的单一「Default Orchestration Flow」,并删除 is_default 列。
//   - 每个流程 content == template(手写全文,不含 {{DAGPrompt}});graph 仅供设计器可视化 + 运行时 overlay;
//   - outline 由 ProjectGraph(graph) 投影;content/template/graph/tags/outline 手写,
//     一致性由 202607080001_workflow_default_presets_test.go 锁死(防漂移);
//   - updatetime/createtime 带递减偏移,保证 repo 的 updatetime DESC 排序稳定产出
//     Parallel Decompose 排第一(前端「首个兜底」= items[0])。
func migration202607080001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607080001",
		Migrate: func(tx *gorm.DB) error {
			// 1) 删旧内置默认行(趁 is_default 列还在)
			if err := tx.Exec(`DELETE FROM workflows WHERE is_default = 1`).Error; err != nil {
				return err
			}
			// 2) 插 4 个新流程(按 name 守卫幂等)
			for _, f := range presetFlows202607080001 {
				if err := tx.Exec(`INSERT INTO workflows (name, content, template, tags, outline, graph, status, createtime, updatetime)
SELECT ?, ?, ?, ?, ?, ?, 1,
	CAST(strftime('%s','now') AS INTEGER) * 1000 + ?,
	CAST(strftime('%s','now') AS INTEGER) * 1000 + ?
WHERE NOT EXISTS (SELECT 1 FROM workflows WHERE name = ?)`,
					f.name, f.prompt, f.prompt, f.tags, f.outline, f.graph, f.tsOffset, f.tsOffset, f.name).Error; err != nil {
					return err
				}
			}
			// 3) DROP is_default 列(与 workflow_entity.IsDefault 字段删除同批,见 spec §5)
			return tx.Exec(`ALTER TABLE workflows DROP COLUMN is_default`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE workflows ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			for _, f := range presetFlows202607080001 {
				if err := tx.Exec(`DELETE FROM workflows WHERE name = ?`, f.name).Error; err != nil {
					return err
				}
			}
			return nil
		},
	}
}

type presetFlow struct {
	name, prompt, tags, outline, graph string
	tsOffset                           int
}

// presetFlows202607080001 四个内置流程。顺序即 tsOffset 递减 → updatetime DESC 排序。
var presetFlows202607080001 = []presetFlow{
	{
		name:     "Parallel Decompose",
		tsOffset: 3,
		tags:     `["General","Parallel"]`,
		outline:  `["See members","Break down","Subtask A ∥ …","Integrate","Verify","Wrap up"]`,
		graph: `{"version":1,"nodes":[` +
			`{"id":"see","label":"See members","kind":"leader"},` +
			`{"id":"break","label":"Break down","kind":"leader"},` +
			`{"id":"t1","label":"Subtask A","kind":"task","brief":"An independent slice of the work, dispatched to a suitable member. Acceptance: concrete deliverable + criteria met.","sharedFiles":true},` +
			`{"id":"t2","label":"Subtask B","kind":"task","brief":"Another independent slice, dispatched to a suitable member. Acceptance: concrete deliverable + criteria met."},` +
			`{"id":"int","label":"Integrate","kind":"leader"},` +
			`{"id":"ver","label":"Verify","kind":"task","brief":"Run review / tests. Acceptance: all pass, no regressions."},` +
			`{"id":"wrap","label":"Wrap up","kind":"leader"}` +
			`],"edges":[` +
			`{"from":"see","to":"break"},{"from":"break","to":"t1"},{"from":"break","to":"t2"},` +
			`{"from":"t1","to":"int"},{"from":"t2","to":"int"},{"from":"int","to":"ver"},` +
			`{"from":"ver","to":"wrap"},{"from":"ver","to":"t1","kind":"bounce"}` +
			`]}`,
		prompt: `# Parallel Decompose

You are the **Leader** of this run. You do not do the work yourself — you split it, dispatch each piece to the right member, and integrate what comes back. Every result returns to you; you decide the next move.

## Flow
1. **See members.** Call the roster before planning — never assume who is available or what they can do.
2. **Break down.** Split the goal into independent subtasks that can run in parallel without blocking each other. If the work is inherently sequential, use the Pipeline flow instead.
3. **Dispatch in parallel.** Send each subtask to the best-matched member. Every dispatch is one concrete task plus explicit acceptance criteria — vague briefs produce vague work. If two subtasks touch the same files, dispatch with isolate=true so they do not clobber each other.
4. **Integrate.** Pull the results together and resolve conflicts yourself; do not hand integration off to a subtask.
5. **Verify.** Dispatch a review/test pass with a clear bar: all checks green, no regressions. On failure, send the work back to the responsible subtask — do not patch over it yourself.
6. **Wrap up.** Finish with a concise summary @user: what was built, what was verified, and anything still open.
`,
	},
	{
		name:     "Sequential Pipeline",
		tsOffset: 2,
		tags:     `["Sequential","Pipeline"]`,
		outline:  `["Investigate","Design","Implement","Verify","Wrap up"]`,
		graph: `{"version":1,"nodes":[` +
			`{"id":"inv","label":"Investigate","kind":"task","brief":"Gather the facts, constraints, and unknowns. Acceptance: the next stage's questions are answered."},` +
			`{"id":"des","label":"Design","kind":"task","brief":"Produce the approach from the investigation. Acceptance: a concrete design the implementer can follow."},` +
			`{"id":"impl","label":"Implement","kind":"task","brief":"Build against the approved design. Acceptance: design realized, with tests where they apply."},` +
			`{"id":"ver","label":"Verify","kind":"task","brief":"Run review / tests. Acceptance: all pass, no regressions."},` +
			`{"id":"wrap","label":"Wrap up","kind":"leader"}` +
			`],"edges":[` +
			`{"from":"inv","to":"des"},{"from":"des","to":"impl"},{"from":"impl","to":"ver"},` +
			`{"from":"ver","to":"wrap"},{"from":"ver","to":"impl","kind":"bounce"}` +
			`]}`,
		prompt: `# Sequential Pipeline

You are the **Leader** of a staged pipeline. Each stage consumes the previous stage's output, so order matters: never open a stage until its predecessor has met its acceptance criteria. You dispatch each stage, review the handoff, then pass it forward.

## Flow
1. **Investigate.** Dispatch a member to gather the facts, constraints, and unknowns. Acceptance: the questions the next stage needs answered are answered.
2. **Design.** Dispatch the plan/approach based on the investigation. Acceptance: a concrete design the implementer can follow without guessing.
3. **Implement.** Dispatch the build against the approved design. Acceptance: the design is realized, with tests where they apply.
4. **Verify.** Dispatch review/tests. Acceptance: all pass, no regressions. On failure, send it back to Implement — fix the stage that broke, do not bolt on a new one.
5. **Wrap up.** Finish with a concise summary @user: what each stage produced and what was verified.

Between every stage, confirm the handoff is solid before moving on. A weak earlier stage poisons everything downstream.
`,
	},
	{
		name:     "Research → Synthesize",
		tsOffset: 1,
		tags:     `["Research","Knowledge"]`,
		outline:  `["Frame questions","Angle A ∥ …","Synthesize","Wrap up"]`,
		graph: `{"version":1,"nodes":[` +
			`{"id":"frame","label":"Frame questions","kind":"leader"},` +
			`{"id":"a1","label":"Angle A","kind":"task","brief":"Investigate one angle. Return findings with sources/evidence, not opinions."},` +
			`{"id":"a2","label":"Angle B","kind":"task","brief":"Investigate a second angle. Return findings with sources/evidence."},` +
			`{"id":"a3","label":"Angle C","kind":"task","brief":"Investigate a third angle. Return findings with sources/evidence."},` +
			`{"id":"syn","label":"Synthesize","kind":"leader"},` +
			`{"id":"wrap","label":"Wrap up","kind":"leader"}` +
			`],"edges":[` +
			`{"from":"frame","to":"a1"},{"from":"frame","to":"a2"},{"from":"frame","to":"a3"},` +
			`{"from":"a1","to":"syn"},{"from":"a2","to":"syn"},{"from":"a3","to":"syn"},` +
			`{"from":"syn","to":"wrap"}` +
			`]}`,
		prompt: `# Research → Synthesize

You are the **Leader** of a research effort. This flow produces understanding, not code: you frame the questions, fan out independent investigations, then converge the findings into one coherent answer. Every finding returns to you.

## Flow
1. **Frame the questions.** State what you actually need to know and the distinct angles worth investigating. Good framing keeps the parallel work non-overlapping.
2. **Investigate in parallel.** Dispatch one member per angle. Each returns findings with sources/evidence, not opinions — an unsourced claim is a lead, not a conclusion. Angles are independent; they do not wait on each other.
3. **Synthesize.** Pull every angle together yourself. Reconcile conflicts, note where sources disagree, and separate what is well-supported from what is uncertain. Do not just concatenate the reports.
4. **Wrap up.** Deliver a concise report @user: the answer, the confidence behind it, the key evidence, and what remains open.

Do not write or verify code in this flow — if the task turns into building something, switch to Parallel Decompose or Pipeline.
`,
	},
	{
		name:     "Generate → Review → Iterate",
		tsOffset: 0,
		tags:     `["Quality","Loop"]`,
		outline:  `["Produce","Review","Wrap up"]`,
		graph: `{"version":1,"nodes":[` +
			`{"id":"prod","label":"Produce","kind":"task","brief":"Produce the work against concrete, testable acceptance criteria."},` +
			`{"id":"rev","label":"Review","kind":"task","brief":"A separate member reviews adversarially. Acceptance: criteria genuinely met."},` +
			`{"id":"wrap","label":"Wrap up","kind":"leader"}` +
			`],"edges":[` +
			`{"from":"prod","to":"rev"},{"from":"rev","to":"wrap"},{"from":"rev","to":"prod","kind":"bounce"}` +
			`]}`,
		prompt: `# Generate → Review → Iterate

You are the **Leader** of a quality-gated loop. One member produces, a *different* member reviews adversarially, and nothing ships until it passes. The whole point is the separation: the reviewer must not be the producer.

## Flow
1. **Produce.** Dispatch the work with a concrete task and explicit acceptance criteria. Acceptance is what Review will hold it to, so make it testable.
2. **Review.** Dispatch a *separate* member to review adversarially — actively try to break it, find the gaps, check the claims. A rubber-stamp review is worse than none. Acceptance: the reviewer signs off that the criteria are genuinely met.
3. **Iterate.** On any real issue, send it back to Produce with the specific defects — reuse the same node, do not spawn a new one. Loop until Review passes clean.
4. **Wrap up.** Finish with a concise summary @user: what was produced, what Review caught, and how it was resolved.

Keep producer and reviewer distinct across iterations so the review stays honest.
`,
	},
}
```

- [ ] **Step 2: 注册迁移**

Modify `migrations/migrations.go`，在 `migrationList()` 的 `migration202607050001()` 行后追加：

```go
		migration202607050001(), // workflows.template + 回填(带图=占位符 / 无图=content)
		migration202607080001(), // 四内置流程取代旧默认 + DROP is_default
	}
}
```

- [ ] **Step 3: 写失败的一致性测试**

Create `migrations/202607080001_workflow_default_presets_test.go`：

```go
package migrations

import (
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/service/workflow_svc"
)

func TestMigration202607080001_SeedsPresetFlows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	type row struct {
		Name, Content, Template, Graph, Tags, Outline string
		Updatetime                                    int64
	}
	var rows []row
	assert.NoError(t, db.Table("workflows").Order("updatetime DESC").Scan(&rows).Error)

	// 恰好 4 个内置流程,顺序(updatetime DESC)= Parallel Decompose 第一
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Name
	}
	assert.Equal(t, []string{
		"Parallel Decompose",
		"Sequential Pipeline",
		"Research → Synthesize",
		"Generate → Review → Iterate",
	}, names)

	// is_default 列已 DROP
	var cols []struct{ Name string }
	assert.NoError(t, db.Raw("PRAGMA table_info(workflows)").Scan(&cols).Error)
	for _, c := range cols {
		assert.NotEqual(t, "is_default", c.Name)
	}

	// 一致性:每行 content==render(template)、outline==ProjectGraph(graph)、tags 非空
	for _, r := range rows {
		assert.NotEmpty(t, r.Tags, r.Name)
		gotContent, gotOutline, err := workflow_svc.RenderWorkflowContent(r.Name, r.Graph, r.Template)
		assert.NoError(t, err, r.Name)
		assert.Equal(t, gotContent, r.Content, "content 应等于 template 渲染产物: "+r.Name)

		var storedOutline []string
		assert.NoError(t, json.Unmarshal([]byte(r.Outline), &storedOutline), r.Name)
		assert.Equal(t, gotOutline, storedOutline, "outline 应等于 ProjectGraph 投影: "+r.Name)
	}
}
```

- [ ] **Step 4: 跑测试看它失败(红)**

Run: `cd /Users/codfrm/Code/agentre/agentre && GOWORK=off go test ./migrations/ -run TestMigration202607080001_SeedsPresetFlows -v`
Expected: PASS —— 若 Step 1 的 graph/prompt/outline 手写一致则直接绿；**若 outline 或 content 有手写误差会在此报不等**，据报错修正字面量直到绿。（这一步的价值就是把手写漂移逼出来。）

- [ ] **Step 5: 删 Go 侧 is_default 三处**

Modify `internal/model/entity/workflow_entity/workflow.go` — 删第 24 行：

```go
	Graph      string `gorm:"column:graph;type:text;not null;default:''"`     // 流程 DAG 的 JSON 真源(空=adhoc/无图)
	Template   string `gorm:"column:template;type:text;not null;default:''"`  // 用户编辑的提示词模板(Go text/template 真源);content 是其渲染产物
	Status     int    `gorm:"column:status;type:int;not null;default:1"`
```

（即移除 `IsDefault int ...` 那一行。）

Modify `internal/service/workflow_svc/types.go` — 删第 16 行 `IsDefault bool ...`，使 `WorkflowItem` 变为：

```go
	Graph      string   `json:"graph"`
	RunCount   int      `json:"runCount"`
```

Modify `internal/service/workflow_svc/workflow.go` — `toItem` 删第 84 行 `IsDefault: w.IsDefault == 1,`：

```go
		Graph:      w.Graph,
		RunCount:   runCount,
```

- [ ] **Step 6: retarget 两个被连累的旧迁移测试**

这两个测试跑完**全链**迁移后查已被 DROP 的 `is_default` 列 → 会崩。改成验证各自 durable 贡献（`graph` / `template` 列），不再引用被删列/被删行。

Replace 整个函数 in `migrations/202607040001_workflow_graph_default_test.go`：

```go
func TestMigration202607040001_AddsGraphColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	// 旧内置「Default Orchestration Flow」已被 202607080001 取代并删除;
	// 这里只验证 202607040001 durable 贡献 = graph 列可用、且存在带 task 节点的流程图。
	var n int64
	assert.NoError(t, db.Table("workflows").Where("graph LIKE ?", `%"kind":"task"%`).Count(&n).Error)
	assert.GreaterOrEqual(t, n, int64(1))
}
```

Replace 整个函数 in `migrations/202607050001_workflow_template_test.go`：

```go
func TestMigration202607050001_AddsTemplateColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	// 旧默认流程(占位符模板)已被 202607080001 取代;这里验证 template 列可用、
	// 且带图流程有非空 template(新内置流程为手写全文)。
	var n int64
	assert.NoError(t, db.Table("workflows").Where("graph != '' AND template != ''").Count(&n).Error)
	assert.GreaterOrEqual(t, n, int64(1))
}
```

- [ ] **Step 7: 跑后端 gate（Go-only，不 regen）**

Run:
```bash
cd /Users/codfrm/Code/agentre/agentre
GOWORK=off gofmt -l migrations internal/model/entity/workflow_entity internal/service/workflow_svc
GOWORK=off go test ./migrations/... ./internal/service/workflow_svc/... -count=1
make test-backend
GOWORK=off golangci-lint run ./migrations/... ./internal/service/workflow_svc/... ./internal/model/entity/workflow_entity/...
```
Expected: `gofmt -l` 无输出；测试全 PASS（含 retarget 后的两个旧迁移测试 + 新一致性测试）；lint 干净。
> 注意：**不要**跑 `make lint` / `make test` / `make test-frontend`（会 `wails generate` 提前删掉 TS 侧 isDefault 使前端断裂）。

- [ ] **Step 8: 提交（pathspec）**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add migrations/202607080001_workflow_default_presets.go migrations/202607080001_workflow_default_presets_test.go
git commit \
  migrations/202607080001_workflow_default_presets.go \
  migrations/202607080001_workflow_default_presets_test.go \
  migrations/migrations.go \
  migrations/202607040001_workflow_graph_default_test.go \
  migrations/202607050001_workflow_template_test.go \
  internal/model/entity/workflow_entity/workflow.go \
  internal/service/workflow_svc/types.go \
  internal/service/workflow_svc/workflow.go \
  -m "✨ orch: 四内置流程(通用并行/流水线/研究/评审)取代旧默认 + 删 is_default"
```

---

### Task 2: 前端 — sticky 预选 + 删 isDefault

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/run-new-dialog.tsx`
- Modify: `frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`
- Regenerate: `frontend/wailsjs/**`（`make generate`，gitignore 不提交）

**Interfaces:**
- Consumes: `WorkflowList()` 返回 `resp.items[]`（Task 1 后不再含 `isDefault`）；浏览器 `localStorage`。
- Produces: 新建运行弹窗打开时 `flowId` = localStorage 上次选择（若仍在列表）否则 `items[0].id`；library 模式提交成功后写回 localStorage。

- [ ] **Step 1: regen 绑定（同步删掉 TS 侧 isDefault）**

Run: `cd /Users/codfrm/Code/agentre/agentre && make generate`
Expected: 成功；`frontend/wailsjs/go/models.ts` 的 `app.WorkflowItem` 不再有 `isDefault`。

- [ ] **Step 2: 改组件——删 isDefault + sticky 预选 + 持久化**

Modify `frontend/src/components/agentre/orchestration/run-new-dialog.tsx`：

(a) `WorkflowOption` 类型删 `isDefault`（约行 73-80）：

```ts
type WorkflowOption = {
  id: number;
  name: string;
  tags: string[];
  outline: string[];
  graph: string;
};
```

(b) 在 `type FlowMode` 定义下方新增模块级常量（约行 52 附近）：

```ts
// 上次选择的流程 id 持久化 key(sticky 预选;localStorage 是本仓前端持久化套路)
const LAST_FLOW_KEY = "agentre.orchestration.lastFlowId";
```

(c) 替换 `WorkflowList().then` 里的 map + 预选（约行 183-197）：

```ts
    WorkflowList()
      .then((resp) => {
        const items = (resp?.items ?? []).map((w) => ({
          id: w.id,
          name: w.name,
          tags: w.tags ?? [],
          outline: w.outline ?? [],
          graph: w.graph ?? "",
        }));
        setWorkflows(items);
        // sticky: 上次选择(若仍在列表)否则首个
        const lastId = Number(localStorage.getItem(LAST_FLOW_KEY) || 0);
        const picked = items.find((w) => w.id === lastId) ?? items[0];
        if (picked) setFlowId(picked.id);
      })
      .catch(() => setWorkflows([]));
```

(d) 在 `submit()` 里，`const d = await RunCreate({...});` 之后、`if (d.run?.id)` 之前，插入持久化：

```ts
      const d = await RunCreate({
        goal: goal.trim(),
        leaderAgentId: leaderId,
        flowId: flowMode === "library" ? flowId : 0,
        flowContent: flowMode === "adhoc" ? flowContent : "",
        projectId,
        allowedAgentIds,
      });
      // 记录本次实际使用的流程,供下次打开预选
      if (flowMode === "library" && flowId > 0) {
        localStorage.setItem(LAST_FLOW_KEY, String(flowId));
      }
      if (d.run?.id) {
```

- [ ] **Step 3: 改测试——beforeEach 清 localStorage + 重写预选用例**

Modify `frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`：

(a) `beforeEach` 里(`vi.clearAllMocks();` 之后)加：

```ts
    vi.clearAllMocks();
    localStorage.clear();
```

(b) 用下列 4 个用例**整体替换**原「默认落在 library 并预选 isDefault 流程」那一个 `it(...)`（约行 215-256）：

```ts
    it("无 localStorage 时预选列表首个流程 → RunCreate 带该 flowId", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      appMocks.WorkflowList.mockResolvedValue({
        items: [
          { id: 1, name: "Parallel Decompose", tags: ["General"], outline: [], graph: "" },
          { id: 2, name: "Sequential Pipeline", tags: ["Pipeline"], outline: [], graph: "" },
        ],
      });
      renderDialog();
      await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
      fireEvent.change(screen.getByTestId("run-goal"), { target: { value: "g" } });
      await user.click(screen.getByTestId("run-leader"));
      await user.click(await screen.findByRole("option", { name: "架构师" }));
      await user.click(screen.getByTestId("run-create"));
      await waitFor(() =>
        expect(appMocks.RunCreate).toHaveBeenCalledWith(
          expect.objectContaining({ flowId: 1 }),
        ),
      );
    });

    it("localStorage 有上次选择的流程 → 预选它", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      localStorage.setItem("agentre.orchestration.lastFlowId", "2");
      appMocks.WorkflowList.mockResolvedValue({
        items: [
          { id: 1, name: "Parallel Decompose", tags: [], outline: [], graph: "" },
          { id: 2, name: "Sequential Pipeline", tags: [], outline: [], graph: "" },
        ],
      });
      renderDialog();
      await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
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

    it("localStorage 上次选择已不在列表 → 回退首个流程", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      localStorage.setItem("agentre.orchestration.lastFlowId", "999");
      appMocks.WorkflowList.mockResolvedValue({
        items: [
          { id: 1, name: "Parallel Decompose", tags: [], outline: [], graph: "" },
          { id: 2, name: "Sequential Pipeline", tags: [], outline: [], graph: "" },
        ],
      });
      renderDialog();
      await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
      fireEvent.change(screen.getByTestId("run-goal"), { target: { value: "g" } });
      await user.click(screen.getByTestId("run-leader"));
      await user.click(await screen.findByRole("option", { name: "架构师" }));
      await user.click(screen.getByTestId("run-create"));
      await waitFor(() =>
        expect(appMocks.RunCreate).toHaveBeenCalledWith(
          expect.objectContaining({ flowId: 1 }),
        ),
      );
    });

    it("library 模式提交成功后把所选流程写入 localStorage", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      appMocks.WorkflowList.mockResolvedValue({
        items: [
          { id: 1, name: "Parallel Decompose", tags: [], outline: [], graph: "" },
          { id: 2, name: "Sequential Pipeline", tags: [], outline: [], graph: "" },
        ],
      });
      renderDialog();
      await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
      await user.click(screen.getByTestId("run-flow-select"));
      await user.click(
        await screen.findByRole("option", { name: /Sequential Pipeline/ }),
      );
      fireEvent.change(screen.getByTestId("run-goal"), { target: { value: "g" } });
      await user.click(screen.getByTestId("run-leader"));
      await user.click(await screen.findByRole("option", { name: "架构师" }));
      await user.click(screen.getByTestId("run-create"));
      await waitFor(() =>
        expect(localStorage.getItem("agentre.orchestration.lastFlowId")).toBe("2"),
      );
    });
```

- [ ] **Step 4: 跑前端 gate**

Run:
```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm test -- run-new-dialog
pnpm exec tsc --noEmit
pnpm exec eslint src/components/agentre/orchestration/run-new-dialog.tsx
```
Expected: run-new-dialog 测试全 PASS；tsc 无错（`app.WorkflowItem` 已无 isDefault，组件也不再引用）；eslint 干净（无新增可见字符串 → 不触发 `i18next/no-literal-string`）。

- [ ] **Step 5: 提交（pathspec）**

```bash
cd /Users/codfrm/Code/agentre/agentre
git commit \
  frontend/src/components/agentre/orchestration/run-new-dialog.tsx \
  frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx \
  -m "✨ orch: 新建运行改 sticky 上次选择+首个兜底(删 isDefault 预选)"
```
> `frontend/wailsjs/**` 是 gitignore 生成物，不提交。

---

### Task 3: 集成验证（无代码）

**Files:** 无

- [ ] **Step 1: 全量后端 gate**

Run: `cd /Users/codfrm/Code/agentre/agentre && make test-backend`
Expected: 全 PASS（看真 exit code）。

- [ ] **Step 2: 全量 lint + 前端全量测试**

Run:
```bash
cd /Users/codfrm/Code/agentre/agentre
make lint
cd frontend && pnpm test
```
Expected: `make lint`（含 regen）干净；全量 vitest PASS（预选逻辑改动未波及其它文件）。看真 exit code，别被 `| tail` 吞退出码。

- [ ] **Step 3: 手动 smoke（真机 wails dev）**

Run: `cd /Users/codfrm/Code/agentre/agentre && make dev`
手动确认：
1. 打开「新建运行」→ 流程库下拉列出 4 个内置流程，顺序 Parallel Decompose → Sequential Pipeline → Research → Synthesize → Generate → Review → Iterate。
2. 首次打开预选 Parallel Decompose；选另一个并创建 Run，再次打开预选变成上次那个。
3. 进流程库/设计器，四个流程的 DAG 图都能渲染；打开任一流程，注入正文(content)= 手写 prompt（非裸步骤清单）。
Expected: 均符合。

- [ ] **Step 4: 收尾**

用 superpowers:finishing-a-development-branch 决定合并/PR。

## Self-Review（作者自查，已核对）

- **Spec 覆盖**：§4 四流程→Task1 Step1；§5 删 is_default→Task1 Step5/Step6 + Task2；§6 迁移→Task1 Step1-2；§7 sticky 前端→Task2 Step2；§8.1 一致性测试→Task1 Step3；§8.2 前端测试→Task2 Step3；§9 gate→Task3。全覆盖。
- **占位符**：无 TBD/TODO；4 流程的 graph/prompt/tags/outline 全字面量给全；测试代码完整。
- **类型一致**：`presetFlow` 字段（name/prompt/tags/outline/graph/tsOffset）在 slice 与 INSERT 绑定顺序一致；`RenderWorkflowContent` 签名与源码一致；`LAST_FLOW_KEY` 常量值与测试里硬写的 `"agentre.orchestration.lastFlowId"` 一致。
- **隐患提示**：Step4(红) 实为「验证手写一致」——若 outline/prompt 手写与 ProjectGraph/模板渲染不符会在此暴露，据报错修字面量；这是刻意的防漂移关卡。
