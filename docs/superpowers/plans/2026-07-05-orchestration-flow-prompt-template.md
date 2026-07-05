# 编排流程 · 可编辑提示词模板 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让「新建/编辑流程」弹窗里的提示词从「DAG 只读投影」变成用户可编辑的 Go `text/template` 模板,用 `{{ DAGPrompt }}` 占位符选择性嵌入 DAG 投影。

**Architecture:** `workflow` 新增 `template` 字段(用户模板真源);`content` 变为 `template` 的渲染产物(注册函数 `DAGPrompt` 返回 `ProjectGraph` 投影)。渲染在保存时发生,`run.FlowContent` 快照 + `turn.go` 注入链路零改动。前端把 DAG 设计器右下的只读预览换成「编辑/预览」双态模板编辑器。

**Tech Stack:** Go 1.26 (`text/template`)、cago、gorm/gormigrate、sqlmock、goconvey/gomock;React 19 + TS + Vitest + react-i18next + shadcn `@/components/ui/*`;Wails 绑定。

## Global Constraints

- **TDD Red→Green→Refactor**:先写失败测试并跑到红,再实现。
- **不变量(回归护栏)**:`internal/service/orch_svc/turn.go`、`internal/service/orch_svc/create.go`、`internal/app/orch_adapter.go` **零改动**;它们读的仍是 `workflow.Content` / `run.FlowContent`。
- **迁移只追加**:新迁移加到 `migrations/migrations.go` 的 `migrationList()` 末尾,不改历史迁移;DDL 用原生 SQL。
- **前端 i18n**:新可见文案走 `t(...)` + 同步 `frontend/src/i18n/locales/{zh-CN,en}/common.json`;不加硬编码中文。
- **i18next 占位符坑**:i18next 默认插值语法就是 `{{var}}`。译文里要出现字面量 `{{ DAGPrompt }}` 时,**不能**直接写进译文(会被当插值变量吃掉)。用插值传入:译文写 `{{token}}`,代码 `t(key, { token: "{{ DAGPrompt }}" })`;或在 JSX 里用 `<code>{DAG_TOKEN}</code>` 渲染(`DAG_TOKEN` 是 JS 变量,ASCII,不触发 `no-literal-string`)。
- **表单控件用 shadcn** `@/components/ui/*`(`Textarea`/`Button`/`Input`),禁止原生 `<select>`。
- **共享分支提交用 pathspec**:分支 `develop/wyz` 有并发会话共享 index,每次 `git commit <files...>` 带显式路径,勿裸 `git commit`。
- **收尾全量 gate**:`make test-backend`、`make lint`、`cd frontend && pnpm test` 看**真 exit code**;`make generate` 刷新 `frontend/wailsjs/`(生成物,不提交)。
- **占位符 token 规范**:`{{ DAGPrompt }}`(Go 模板注册函数,niladic);空模板回落 `DefaultTemplate = "{{ DAGPrompt }}"`,保证「带图流程」渲染 = DAG 投影,与旧行为逐字节一致。

---

## Task 1: `RenderTemplate` 纯函数

把用户模板渲染成注入正文的纯函数,独立可测,后续 Create/Update/Preview 复用。

**Files:**
- Create: `internal/service/workflow_svc/render.go`
- Test: `internal/service/workflow_svc/render_test.go`

**Interfaces:**
- Produces:
  - `const DefaultTemplate = "{{ DAGPrompt }}"`
  - `func RenderTemplate(tmpl, name, dagPrompt string) (string, error)` — 用 `text/template` 渲染;注册函数 `DAGPrompt() string` 返回 `dagPrompt`;数据上下文 `struct{ FlowName string }` 暴露 `{{ .FlowName }}`;parse/execute 失败返回 error。

- [ ] **Step 1: 写失败测试** `internal/service/workflow_svc/render_test.go`

```go
package workflow_svc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderTemplate(t *testing.T) {
	t.Run("占位符渲染为 dagPrompt", func(t *testing.T) {
		out, err := RenderTemplate("{{ DAGPrompt }}", "F", "STEP-A\nSTEP-B")
		require.NoError(t, err)
		assert.Equal(t, "STEP-A\nSTEP-B", out)
	})
	t.Run("前后包裹文本", func(t *testing.T) {
		out, err := RenderTemplate("intro\n{{ DAGPrompt }}\nouttro", "F", "DAG")
		require.NoError(t, err)
		assert.Equal(t, "intro\nDAG\nouttro", out)
	})
	t.Run("FlowName 变量", func(t *testing.T) {
		out, err := RenderTemplate("# {{ .FlowName }}", "标准流", "")
		require.NoError(t, err)
		assert.Equal(t, "# 标准流", out)
	})
	t.Run("if 条件(空 DAG 走 else)", func(t *testing.T) {
		out, err := RenderTemplate(`{{ if DAGPrompt }}has{{ else }}none{{ end }}`, "F", "")
		require.NoError(t, err)
		assert.Equal(t, "none", out)
	})
	t.Run("无占位符=纯文本原样", func(t *testing.T) {
		out, err := RenderTemplate("just prose", "F", "DAG")
		require.NoError(t, err)
		assert.Equal(t, "just prose", out)
	})
	t.Run("空模板→空串", func(t *testing.T) {
		out, err := RenderTemplate("", "F", "DAG")
		require.NoError(t, err)
		assert.Equal(t, "", out)
	})
	t.Run("未定义函数→报错", func(t *testing.T) {
		_, err := RenderTemplate("{{ DAGPromt }}", "F", "DAG")
		require.Error(t, err)
	})
	t.Run("坏语法→报错", func(t *testing.T) {
		_, err := RenderTemplate("{{ if }}", "F", "DAG")
		require.Error(t, err)
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/service/workflow_svc/ -run TestRenderTemplate`
Expected: FAIL(`undefined: RenderTemplate` / `undefined: DefaultTemplate`)

- [ ] **Step 3: 实现** `internal/service/workflow_svc/render.go`

```go
// Package workflow_svc — render.go:把用户模板(Go text/template)渲染成注入 Leader 的正文。
package workflow_svc

import (
	"strings"
	"text/template"
)

// DefaultTemplate 默认模板:只放 DAG 占位符。空模板回落到它,
// 保证「带图流程」渲染=DAG 投影,与旧行为逐字节一致。
const DefaultTemplate = "{{ DAGPrompt }}"

// RenderTemplate 用 Go text/template 渲染 tmpl:
//   - 注册函数 DAGPrompt() 返回 dagPrompt(= ProjectGraph 投影);
//   - 数据上下文暴露 {{ .FlowName }};
//   - 用 text/template(非 html/template),提示词不做 HTML 转义;
//   - parse/execute 失败返回 error(调用方据此阻止保存 / 预览显示错误)。
func RenderTemplate(tmpl, name, dagPrompt string) (string, error) {
	t, err := template.New("workflow").Funcs(template.FuncMap{
		"DAGPrompt": func() string { return dagPrompt },
	}).Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, struct{ FlowName string }{FlowName: name}); err != nil {
		return "", err
	}
	return b.String(), nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/service/workflow_svc/ -run TestRenderTemplate -v`
Expected: PASS(全部子测试)

- [ ] **Step 5: 提交**

```bash
git commit internal/service/workflow_svc/render.go internal/service/workflow_svc/render_test.go \
  -m "✨ workflow_svc: RenderTemplate(Go text/template + DAGPrompt 函数)"
```

---

## Task 2: 实体 `template` 字段 + 迁移 + repo sqlmock 修正

加 DB 列与实体字段;因为 `repo.Update` 走 `Save` 全列覆盖,加字段会改 sqlmock 的参数序,必须同任务修好 repo 测试保持绿。

**Files:**
- Modify: `internal/model/entity/workflow_entity/workflow.go`(加 `Template` 字段,置于 `Graph` 之后)
- Create: `migrations/202607050001_workflow_template.go`
- Modify: `migrations/migrations.go`(注册到 `migrationList()` 末尾)
- Create: `migrations/202607050001_workflow_template_test.go`
- Modify: `internal/repository/workflow_repo/workflow_test.go`(`TestWorkflowRepo_Update` 补 template 参数)

**Interfaces:**
- Produces: `workflow_entity.Workflow.Template string`(`gorm:"column:template"`)

- [ ] **Step 1: 加实体字段** `internal/model/entity/workflow_entity/workflow.go`

在 `Graph` 行之后插入(字段顺序决定 `Save` 列序,放在 `Graph` 之后、`IsDefault` 之前):

```go
	Graph      string `gorm:"column:graph;type:text;not null;default:''"`     // 流程 DAG 的 JSON 真源(空=adhoc/无图)
	Template   string `gorm:"column:template;type:text;not null;default:''"`  // 用户编辑的提示词模板(Go text/template 真源);content 是其渲染产物
	IsDefault  int    `gorm:"column:is_default;type:int;not null;default:0"`  // 1=内置默认流程(仿 system_badge)
```

- [ ] **Step 2: 写迁移** `migrations/202607050001_workflow_template.go`

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607050001 给 workflows 加 template 列并回填:
//   - 带图流程 template='{{ DAGPrompt }}'(其 content 已=投影,render('{{ DAGPrompt }}') 逐字节一致);
//   - legacy 无图流程 template=content(无占位符渲染即原样)。
// content 无需重算,故纯 SQL 回填(不调 Go 投影)。
func migration202607050001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607050001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE workflows ADD COLUMN template TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE workflows SET template = '{{ DAGPrompt }}' WHERE graph != ''`).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE workflows SET template = content WHERE graph = ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE workflows DROP COLUMN template`).Error
		},
	}
}
```

- [ ] **Step 3: 注册迁移** `migrations/migrations.go`

在 `migration202607040002(),` 行之后追加:

```go
		migration202607040002(), // 运行时进度 overlay:orch_tasks.node_ref + orchestration_runs.flow_graph
		migration202607050001(), // workflows.template + 回填(带图=占位符 / 无图=content)
```

- [ ] **Step 4: 写迁移测试** `migrations/202607050001_workflow_template_test.go`

```go
package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607050001_BackfillsTemplate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	// seed 的默认流程带 graph → 回填成占位符模板
	var row struct{ Template string }
	assert.NoError(t, db.Table("workflows").Where("is_default = 1").Scan(&row).Error)
	assert.Equal(t, "{{ DAGPrompt }}", row.Template)
}
```

- [ ] **Step 5: 跑迁移测试确认通过**

Run: `go test ./migrations/ -run TestMigration202607050001`
Expected: PASS

- [ ] **Step 6: 修 repo Update sqlmock 参数** `internal/repository/workflow_repo/workflow_test.go`

`TestWorkflowRepo_Update` 的 `WithArgs`:在 `graph`("")与 `is_default`(int64(0))之间插入一个 `""`(template),并更新注释:

```go
		mock.ExpectExec("(?s)UPDATE `workflows` SET .* WHERE `id` = \\?").
			WithArgs(
				// SET 全列(name, content, tags, outline, graph, template, is_default, status, createtime, updatetime)
				"产品开发流程", "# 流程", "", "", "", "", int64(0), consts.ACTIVE, int64(0), sqlmock.AnyArg(),
				int64(3), // WHERE: id 主键
			).
			WillReturnResult(sqlmock.NewResult(0, 1))
```

- [ ] **Step 7: 跑 repo 测试确认通过**

Run: `go test ./internal/repository/workflow_repo/ -run TestWorkflowRepo`
Expected: PASS(尤其 `TestWorkflowRepo_Update`)

- [ ] **Step 8: 提交**

```bash
git commit internal/model/entity/workflow_entity/workflow.go \
  migrations/202607050001_workflow_template.go migrations/migrations.go \
  migrations/202607050001_workflow_template_test.go \
  internal/repository/workflow_repo/workflow_test.go \
  -m "✨ workflow: 加 template 列 + 迁移回填(带图=占位符 / 无图=content)"
```

---

## Task 3: 服务渲染(Create/Update)+ DTO + 既有测试更新

把 Create/Update 从「graph 投影覆写 content」改成「渲染 template → content」;DTO 请求把 `content` 换成 `template`,`WorkflowItem` 增 `template`。这一步会改既有 svc 测试的入参/断言(behavior change,同包内)。

**Files:**
- Modify: `internal/service/workflow_svc/types.go`
- Modify: `internal/service/workflow_svc/workflow.go`(`applyGraph`→`applyTemplate`;`Create`/`Update`;`toItem`)
- Modify: `internal/service/workflow_svc/workflow_test.go`(既有测试改入参/断言 + 新增渲染用例)

**Interfaces:**
- Consumes: `RenderTemplate`、`DefaultTemplate`(Task 1);`ParseFlowGraph`、`ProjectGraph`(既有)
- Produces:
  - `CreateWorkflowRequest{Name, Template, Tags, Outline, Graph}`(去掉 `Content`)
  - `UpdateWorkflowRequest{ID, Name, Template, Tags, Outline, Graph}`(去掉 `Content`)
  - `WorkflowItem{... Template string ...}`(保留 `Content` = 渲染产物,给展示/摘要)

- [ ] **Step 1: 改 DTO** `internal/service/workflow_svc/types.go`

`WorkflowItem` 加 `Template`(保留 `Content`):

```go
type WorkflowItem struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Content    string   `json:"content"`  // 渲染产物(展示/摘要用)
	Template   string   `json:"template"` // 用户模板真源(编辑载入用)
	Tags       []string `json:"tags"`
	Outline    []string `json:"outline"`
	Graph      string   `json:"graph"`
	IsDefault  bool     `json:"isDefault"`
	RunCount   int      `json:"runCount"`
	Createtime int64    `json:"createtime"`
	Updatetime int64    `json:"updatetime"`
}
```

`CreateWorkflowRequest` / `UpdateWorkflowRequest` 把 `Content` 换成 `Template`:

```go
type CreateWorkflowRequest struct {
	Name     string   `json:"name" binding:"required"`
	Template string   `json:"template"` // 用户模板;空→回落 DefaultTemplate
	Tags     []string `json:"tags"`
	Outline  []string `json:"outline"`
	Graph    string   `json:"graph,omitempty"`
}

type UpdateWorkflowRequest struct {
	ID       int64    `json:"id" binding:"required"`
	Name     string   `json:"name" binding:"required"`
	Template string   `json:"template"`
	Tags     []string `json:"tags"`
	Outline  []string `json:"outline"`
	Graph    string   `json:"graph,omitempty"`
}
```

- [ ] **Step 2: 改服务** `internal/service/workflow_svc/workflow.go`

加 `"fmt"` import。把 `applyGraph`(89-102 行)整段替换成 `applyTemplate`:

```go
// applyTemplate 用 graph(若合法)投影出 DAG 提示词 + outline,再渲染用户 template 成 content。
//   - graph 空 → 保留已存 graph(no-op 守卫,避免 Update 未回传 graph 时清空),dagPrompt 为空;
//   - template 空 → 回落 DefaultTemplate;
//   - 渲染失败返回 error(不写坏 content)。
func applyTemplate(w *workflow_entity.Workflow, graph, tmpl string) error {
	if g := strings.TrimSpace(graph); g != "" {
		w.Graph = g
	}
	raw := tmpl
	if strings.TrimSpace(raw) == "" {
		raw = DefaultTemplate
	}
	w.Template = raw

	var dagPrompt string
	if g, ok := ParseFlowGraph(w.Graph); ok {
		content, outline := ProjectGraph(w.Name, g)
		dagPrompt = content
		w.Outline = encodeStringList(outline)
	}
	rendered, err := RenderTemplate(w.Template, w.Name, dagPrompt)
	if err != nil {
		return fmt.Errorf("workflow_svc: 渲染流程模板失败: %w", err)
	}
	w.Content = rendered
	return nil
}
```

`Create`(现 134-150 行)去掉 `Content: req.Content`,`applyGraph` 换成 `applyTemplate`(带错误返回):

```go
func (s *workflowSvc) Create(ctx context.Context, req *CreateWorkflowRequest) (*CreateWorkflowResponse, error) {
	w := &workflow_entity.Workflow{
		Name:    strings.TrimSpace(req.Name),
		Tags:    encodeStringList(req.Tags),
		Outline: encodeStringList(req.Outline),
		Status:  consts.ACTIVE,
	}
	if err := applyTemplate(w, req.Graph, req.Template); err != nil {
		return nil, err
	}
	if err := w.Check(ctx); err != nil {
		return nil, err
	}
	if err := workflow_repo.Workflow().Create(ctx, w); err != nil {
		return nil, err
	}
	return &CreateWorkflowResponse{Item: toItem(w, 0)}, nil
}
```

`Update`(现 153-174 行)去掉 `w.Content = req.Content`,`applyGraph` 换成 `applyTemplate`:

```go
func (s *workflowSvc) Update(ctx context.Context, req *UpdateWorkflowRequest) (*UpdateWorkflowResponse, error) {
	w, err := s.findActive(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	w.Name = strings.TrimSpace(req.Name)
	w.Tags = encodeStringList(req.Tags)
	w.Outline = encodeStringList(req.Outline)
	if err := applyTemplate(w, req.Graph, req.Template); err != nil {
		return nil, err
	}
	if err := w.Check(ctx); err != nil {
		return nil, err
	}
	if err := workflow_repo.Workflow().Update(ctx, w); err != nil {
		return nil, err
	}
	counts, err := s.runCounts(ctx)
	if err != nil {
		return nil, err
	}
	return &UpdateWorkflowResponse{Item: toItem(w, counts[w.ID])}, nil
}
```

`toItem`(74-87 行)加 `Template: w.Template,`:

```go
	return &WorkflowItem{
		ID:         w.ID,
		Name:       w.Name,
		Content:    w.Content,
		Template:   w.Template,
		Tags:       decodeStringList(w.Tags),
		...
```

- [ ] **Step 3: 更新既有 svc 测试到新契约** `internal/service/workflow_svc/workflow_test.go`

改动清单(逐处):
- `TestCreateWorkflow` → "成功" 分支:`&CreateWorkflowRequest{Name: "  产品开发流程 ", Content: "# 产品开发流程"}` 去掉 `Content` 字段 → `&CreateWorkflowRequest{Name: "  产品开发流程 "}`。
- `TestCreateWorkflow_TagsOutline` → 第一子测试:`Content: "# 标准功能开发流"` 删除(留 Name/Tags/Outline)。
- `TestUpdateWorkflow` → "成功:改名改正文并回带 Run 数":入参改为传 `Template`,断言 content 为渲染产物:

```go
			convey.Convey("成功:改名 + 模板渲染进 content 并回带 Run 数", func() {
				wfMock.EXPECT().Find(gomock.Any(), int64(3)).
					Return(&workflow_entity.Workflow{ID: 3, Name: "旧名", Status: 1}, nil)
				wfMock.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, w *workflow_entity.Workflow) error {
						assert.Equal(t, "新名", w.Name)
						assert.Equal(t, "hi 新名", w.Content) // 无 graph → DAGPrompt 空,只渲染 .FlowName
						assert.Equal(t, "hi {{ .FlowName }}", w.Template)
						return nil
					})
				runMock.EXPECT().List(gomock.Any()).
					Return([]*orch_entity.OrchestrationRun{{ID: 1, FlowID: 3}}, nil)
				resp, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 3, Name: " 新名 ", Template: "hi {{ .FlowName }}"})
				assert.NoError(t, err)
				assert.Equal(t, 1, resp.Item.RunCount)
			})
```

- `TestUpdateWorkflow_TagsOutline`:入参无 `Content`,无需改(编译即可,断言 Tags/Outline 不变)。
- `TestCreateWorkflow_ProjectsGraphIntoContent`:去掉 `Content: "ignored user text"` 与 `assert.NotEqual(t, "ignored user text", saved.Content)` 那行(不再相关);其余断言(content 含 `# F`、`finish with a summary @user`、graph 保存)保持:

```go
			resp, err := svc.Create(ctx, &CreateWorkflowRequest{Name: "F", Graph: graph})
			assert.NoError(t, err)
			assert.Contains(t, saved.Content, "# F")
			assert.Contains(t, saved.Content, "finish with a summary @user")
			assert.Equal(t, graph, saved.Graph)
			assert.Equal(t, "{{ DAGPrompt }}", saved.Template) // 空模板回落默认
			assert.Contains(t, resp.Item.Content, "# F")
```

- `TestUpdateWorkflow_EmptyGraphPreservesStoredGraph`:去掉入参 `Content: "## body"`(其余断言只看 `saved.Graph`,不变):

```go
			_, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 3, Name: "新名"})
```

- [ ] **Step 4: 新增渲染行为测试** 追加到 `internal/service/workflow_svc/workflow_test.go`

```go
func TestCreateWorkflow_RendersTemplateWithDAG(t *testing.T) {
	convey.Convey("Create:模板包裹 DAG 占位符 → content 为渲染产物", t, func() {
		ctx, wfMock, _, svc := setupSvc(t)
		var saved *workflow_entity.Workflow
		wfMock.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, w *workflow_entity.Workflow) error { saved = w; w.ID = 5; return nil })
		graph := `{"version":1,"nodes":[{"id":"a","label":"Do","kind":"task","brief":"do it"}],"edges":[]}`
		_, err := svc.Create(ctx, &CreateWorkflowRequest{
			Name: "F", Graph: graph, Template: "intro\n{{ DAGPrompt }}\noutro",
		})
		assert.NoError(t, err)
		assert.True(t, strings.HasPrefix(saved.Content, "intro\n"))
		assert.Contains(t, saved.Content, "# F")           // DAG 投影嵌入
		assert.True(t, strings.HasSuffix(saved.Content, "\noutro"))
		assert.Equal(t, "intro\n{{ DAGPrompt }}\noutro", saved.Template)
	})
}

func TestCreateWorkflow_RenderErrorBlocksSave(t *testing.T) {
	convey.Convey("Create:模板语法错误 → 返回 error 且不落库", t, func() {
		ctx, _, _, svc := setupSvc(t) // 不 EXPECT Create,被调用即失败
		_, err := svc.Create(ctx, &CreateWorkflowRequest{Name: "F", Template: "{{ DAGPromt }}"})
		assert.Error(t, err)
	})
}
```

> 需在 `workflow_test.go` import 里补 `"strings"`。

- [ ] **Step 5: 跑全包 svc 测试确认通过**

Run: `go test ./internal/service/workflow_svc/ -v`
Expected: PASS(既有 + 新增)

- [ ] **Step 6: 提交**

```bash
git commit internal/service/workflow_svc/types.go internal/service/workflow_svc/workflow.go \
  internal/service/workflow_svc/workflow_test.go \
  -m "✨ workflow_svc: content 改由 template 渲染(applyTemplate),DTO content→template"
```

---

## Task 4: 预览绑定 template 感知 + 报错

设计器实时预览必须与保存渲染同源:`WorkflowPreviewGraph` 接受 `template`,返回渲染 content 或 `error`。

**Files:**
- Modify: `internal/app/workflow.go`(`WorkflowPreviewRequest`/`Response`/`WorkflowPreviewGraph`)
- Modify: `internal/app/workflow_test.go`

**Interfaces:**
- Consumes: `workflow_svc.RenderTemplate`、`workflow_svc.DefaultTemplate`、`workflow_svc.ParseFlowGraph`、`workflow_svc.ProjectGraph`
- Produces:
  - `WorkflowPreviewRequest{Name, Graph, Template string}`
  - `WorkflowPreviewResponse{Content string; Outline []string; Error string}`

- [ ] **Step 1: 写失败测试** 追加/改 `internal/app/workflow_test.go`

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
	assert.Contains(t, resp.Content, "# P") // 空模板回落占位符 → 投影
	assert.Empty(t, resp.Error)
}

func TestWorkflowPreviewGraph_Template(t *testing.T) {
	a := &App{}
	resp, err := a.WorkflowPreviewGraph(&WorkflowPreviewRequest{
		Name:     "P",
		Graph:    `{"version":1,"nodes":[{"id":"a","label":"Do","kind":"task","brief":"x"}],"edges":[]}`,
		Template: "head\n{{ DAGPrompt }}",
	})
	assert.NoError(t, err)
	assert.Contains(t, resp.Content, "head\n")
	assert.Contains(t, resp.Content, "# P")
}

func TestWorkflowPreviewGraph_Error(t *testing.T) {
	a := &App{}
	resp, err := a.WorkflowPreviewGraph(&WorkflowPreviewRequest{Name: "P", Template: "{{ DAGPromt }}"})
	assert.NoError(t, err)      // 错误走响应字段,不是 Go error
	assert.NotEmpty(t, resp.Error)
	assert.Empty(t, resp.Content)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/app/ -run TestWorkflowPreviewGraph`
Expected: FAIL(`Template`/`Error` 未定义)

- [ ] **Step 3: 实现** `internal/app/workflow.go`

加 `"strings"` import。替换 27-47 行:

```go
// WorkflowPreviewRequest 设计器实时预览入参(未落库的草稿 graph + template)。
type WorkflowPreviewRequest struct {
	Name     string `json:"name"`
	Graph    string `json:"graph"`
	Template string `json:"template"`
}

// WorkflowPreviewResponse 预览结果:content=渲染后即将注入 Leader 的正文;
// outline 仅展示;error=模板 parse/execute 失败时的说明(前端展示报错态,不算 Go error)。
type WorkflowPreviewResponse struct {
	Content string   `json:"content"`
	Outline []string `json:"outline"`
	Error   string   `json:"error"`
}

// WorkflowPreviewGraph 与保存渲染同源:先投影 graph 得 DAG 提示词,再渲染用户 template。
func (a *App) WorkflowPreviewGraph(req *WorkflowPreviewRequest) (*WorkflowPreviewResponse, error) {
	var dagPrompt string
	var outline []string
	if g, ok := workflow_svc.ParseFlowGraph(req.Graph); ok {
		dagPrompt, outline = workflow_svc.ProjectGraph(req.Name, g)
	}
	tmpl := req.Template
	if strings.TrimSpace(tmpl) == "" {
		tmpl = workflow_svc.DefaultTemplate
	}
	content, err := workflow_svc.RenderTemplate(tmpl, req.Name, dagPrompt)
	if err != nil {
		return &WorkflowPreviewResponse{Error: err.Error()}, nil
	}
	return &WorkflowPreviewResponse{Content: content, Outline: outline}, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/app/ -run TestWorkflowPreviewGraph -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git commit internal/app/workflow.go internal/app/workflow_test.go \
  -m "✨ app: WorkflowPreviewGraph 加 template 感知 + 报错字段(预览与保存同源)"
```

---

## Task 5: 前端 hook `use-workflows`

把 `create`/`update` 的 `content` 形参换成 `template`,`WorkflowItem` 增 `template`。

**Files:**
- Modify: `frontend/src/hooks/use-workflows.ts`
- Modify: `frontend/src/hooks/use-workflows.test.ts`

**Interfaces:**
- Produces:
  - `WorkflowItem` 增 `template: string`
  - `create(name, template, tags, outline, graph?)` → `WorkflowCreate({ name, template, tags, outline, graph })`
  - `update(id, name, template, tags, outline, graph?)` → `WorkflowUpdate({ id, name, template, tags, outline, graph })`

- [ ] **Step 1: 改测试到新契约** `frontend/src/hooks/use-workflows.test.ts`

`create/update` 断言里 `content` 换成 `template`(第二个位置形参即模板):

```ts
    await act(async () => {
      await result.current.create("新流程", "# 新", ["通用"], ["需求拆解", "方案设计"]);
    });
    expect(workflowCreate).toHaveBeenCalledWith({
      name: "新流程",
      template: "# 新",
      tags: ["通用"],
      outline: ["需求拆解", "方案设计"],
      graph: "",
    });
    await act(async () => {
      await result.current.update(1, "改名", "# 改", ["修复"], ["复现"]);
    });
    expect(workflowUpdate).toHaveBeenCalledWith({
      id: 1,
      name: "改名",
      template: "# 改",
      tags: ["修复"],
      outline: ["复现"],
      graph: "",
    });
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/hooks/use-workflows.test.ts`
Expected: FAIL(`WorkflowCreate` 收到 `content` 而非 `template`)

- [ ] **Step 3: 改实现** `frontend/src/hooks/use-workflows.ts`

`WorkflowItem` 类型加 `template: string;`(在 `content` 下面)。reload 的 map 加 `template: i.template ?? "",`。`create`/`update` 形参 `content`→`template`,payload 同步:

```ts
  const create = useCallback(
    async (name: string, template: string, tags: string[], outline: string[], graph = "") => {
      await WorkflowCreate({ name, template, tags, outline, graph });
      await reload();
    },
    [reload],
  );

  const update = useCallback(
    async (id: number, name: string, template: string, tags: string[], outline: string[], graph = "") => {
      await WorkflowUpdate({ id, name, template, tags, outline, graph });
      await reload();
    },
    [reload],
  );
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/hooks/use-workflows.test.ts`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git commit frontend/src/hooks/use-workflows.ts frontend/src/hooks/use-workflows.test.ts \
  -m "✨ use-workflows: create/update 形参 content→template + WorkflowItem.template"
```

---

## Task 6: DAG 设计器模板 pane(编辑/预览/报错)+ i18n

把 `workflow-dag-designer.tsx` 右下只读预览换成「编辑/预览」双态模板编辑器 + 插入按钮 + 报错态。对齐 Pencil `编辑流程 · DAG 设计器`。

**Files:**
- Modify: `frontend/src/components/agentre/workflows/workflow-dag-designer.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`(`workflows.designer.*`)
- Modify: `frontend/src/i18n/locales/en/common.json`(`workflows.designer.*`)
- Modify: `frontend/src/components/agentre/workflows/__tests__/workflow-dag-designer.test.tsx`

**Interfaces:**
- Consumes: `WorkflowPreviewGraph({name, graph, template}) → {content, outline, error}`(Task 4)
- Produces: `WorkflowDagDesigner` 新增 props `template: string`、`onTemplateChange: (v:string)=>void`、`onTemplateError: (hasError:boolean)=>void`

- [ ] **Step 1: 加 i18n key**(两个 locale 同步)`frontend/src/i18n/locales/zh-CN/common.json` 的 `workflows.designer` 对象加:

```json
"templateTitle": "提示词模板",
"editableBadge": "可编辑",
"tabEdit": "编辑",
"tabPreview": "预览",
"insertToken": "插入",
"tokenHint": "{{token}} = 流程图生成的提示词 · Go 模板",
"templatePlaceholder": "在这里编写提示词;用 {{token}} 插入流程图生成的提示词",
"templateHint": "放 {{token}} = 嵌入流程图;不放 = 纯自由文本;不画流程图 = 纯提示词。模板走 Go text/template。",
"templateErrorLabel": "模板错误:"
```

`frontend/src/i18n/locales/en/common.json` 的 `workflows.designer` 对象加:

```json
"templateTitle": "Prompt template",
"editableBadge": "Editable",
"tabEdit": "Edit",
"tabPreview": "Preview",
"insertToken": "Insert",
"tokenHint": "{{token}} = prompt from the flow graph · Go template",
"templatePlaceholder": "Write the prompt here; use {{token}} to insert the flow-graph prompt",
"templateHint": "With {{token}} = embed the flow graph; without = plain text; no graph = pure prompt. Uses Go text/template.",
"templateErrorLabel": "Template error: "
```

> `{{token}}` 是 i18next 插值,代码传 `{ token: DAG_TOKEN }`,DAG_TOKEN 值即字面 `{{ DAGPrompt }}`。

- [ ] **Step 2: 写失败测试** `frontend/src/components/agentre/workflows/__tests__/workflow-dag-designer.test.tsx`

用一个 render helper 覆盖新契约(mock 预览绑定;i18n/wails runtime 按本仓 per-file mock 规范)。替换该测试文件为:

```tsx
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const previewGraph = vi.fn();
vi.mock("../../../../wailsjs/go/app/App", () => ({
  WorkflowPreviewGraph: (...a: unknown[]) => previewGraph(...a),
}));

import { WorkflowDagDesigner } from "../workflow-dag-designer";
import type { FlowGraph } from "../../orchestration/flow-graph";

const graph: FlowGraph = {
  version: 1,
  nodes: [{ id: "n1", label: "拆解", kind: "leader" }],
  edges: [],
};

function setup(template = "{{ DAGPrompt }}") {
  const onTemplateChange = vi.fn();
  const onTemplateError = vi.fn();
  render(
    <WorkflowDagDesigner
      name="F"
      graph={graph}
      template={template}
      error={null}
      onNameChange={() => {}}
      onGraphChange={() => {}}
      onTemplateChange={onTemplateChange}
      onTemplateError={onTemplateError}
    />,
  );
  return { onTemplateChange, onTemplateError };
}

describe("WorkflowDagDesigner 模板 pane", () => {
  beforeEach(() => {
    previewGraph.mockReset().mockResolvedValue({ content: "# F\nRENDERED", outline: [], error: "" });
  });

  it("编辑态渲染可编辑模板文本域", () => {
    setup("hello");
    expect(screen.getByTestId("designer-template-input")).toHaveValue("hello");
  });

  it("改文本域回调 onTemplateChange", () => {
    const { onTemplateChange } = setup("a");
    fireEvent.change(screen.getByTestId("designer-template-input"), {
      target: { value: "ab" },
    });
    expect(onTemplateChange).toHaveBeenCalledWith("ab");
  });

  it("插入按钮把 {{ DAGPrompt }} 拼进模板", () => {
    const { onTemplateChange } = setup("x");
    fireEvent.click(screen.getByTestId("designer-insert-token"));
    expect(onTemplateChange).toHaveBeenCalledWith(expect.stringContaining("{{ DAGPrompt }}"));
  });

  it("切到预览显示渲染 content", async () => {
    setup("{{ DAGPrompt }}");
    fireEvent.click(screen.getByTestId("designer-tab-preview"));
    await waitFor(() =>
      expect(screen.getByTestId("designer-prompt-preview")).toHaveTextContent("RENDERED"),
    );
  });

  it("预览报错→显示错误并回调 onTemplateError(true)", async () => {
    previewGraph.mockResolvedValue({ content: "", outline: [], error: 'function "DAGPromt" not defined' });
    const { onTemplateError } = setup("{{ DAGPromt }}");
    await act(async () => {
      await Promise.resolve();
    });
    await waitFor(() => expect(onTemplateError).toHaveBeenCalledWith(true));
    fireEvent.click(screen.getByTestId("designer-tab-preview"));
    await waitFor(() =>
      expect(screen.getByTestId("designer-template-error")).toHaveTextContent("DAGPromt"),
    );
  });
});
```

- [ ] **Step 3: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/__tests__/workflow-dag-designer.test.tsx`
Expected: FAIL(新 props/ testid 未实现)

- [ ] **Step 4: 改实现** `frontend/src/components/agentre/workflows/workflow-dag-designer.tsx`

顶部 import 增 `Textarea`;定义 token 常量。props 与预览逻辑改造:

```tsx
import * as React from "react";
import { Braces, Plus } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

import type { FlowGraph, FlowKind } from "../orchestration/flow-graph";
import { FlowGraphView } from "../orchestration/flow-graph-view";
import { MarkdownText } from "../markdown-text";
import { WorkflowPreviewGraph } from "../../../../wailsjs/go/app/App";
import {
  addNode, earlierNodeIds, graphToJSON, moveNode, nodeBounce,
  nodeDependsOn, removeNode, setBounce, setDependsOn, updateNode,
} from "./flow-graph-draft";
import { WorkflowNodeForm } from "./workflow-node-form";

const DAG_TOKEN = "{{ DAGPrompt }}";

export function WorkflowDagDesigner({
  name, graph, template, error,
  onNameChange, onGraphChange, onTemplateChange, onTemplateError,
}: {
  name: string;
  graph: FlowGraph;
  template: string;
  error: string | null;
  onNameChange: (v: string) => void;
  onGraphChange: (g: FlowGraph) => void;
  onTemplateChange: (v: string) => void;
  onTemplateError: (hasError: boolean) => void;
}) {
  const { t } = useTranslation();
  const [preview, setPreview] = React.useState("");
  const [previewError, setPreviewError] = React.useState("");
  const [tab, setTab] = React.useState<"edit" | "preview">("edit");
  const taRef = React.useRef<HTMLTextAreaElement>(null);

  const graphJSON = React.useMemo(() => graphToJSON(graph), [graph]);
  React.useEffect(() => {
    let alive = true;
    const timer = setTimeout(() => {
      WorkflowPreviewGraph({ name, graph: graphJSON, template })
        .then((resp) => {
          if (!alive) return;
          const err = resp?.error ?? "";
          setPreviewError(err);
          setPreview(err ? "" : (resp?.content ?? ""));
          onTemplateError(!!err);
        })
        .catch(() => {
          if (!alive) return;
          setPreviewError("");
          setPreview("");
          onTemplateError(false);
        });
    }, 250);
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [name, graphJSON, template, onTemplateError]);

  const insertToken = () => {
    const el = taRef.current;
    if (!el) {
      onTemplateChange(template + DAG_TOKEN);
      return;
    }
    const s = el.selectionStart ?? template.length;
    const e = el.selectionEnd ?? template.length;
    onTemplateChange(template.slice(0, s) + DAG_TOKEN + template.slice(e));
  };

  const nodeById = React.useMemo(
    () => new Map(graph.nodes.map((n) => [n.id, n])),
    [graph],
  );
  const earlierNodes = (id: string) =>
    earlierNodeIds(graph, id)
      .map((eid) => nodeById.get(eid))
      .filter((n): n is NonNullable<typeof n> => n != null);
  const otherNodes = (id: string) => graph.nodes.filter((n) => n.id !== id);

  const toggleDep = (id: string, depId: string) => {
    const cur = nodeDependsOn(graph, id);
    const next = cur.includes(depId) ? cur.filter((d) => d !== depId) : [...cur, depId];
    onGraphChange(setDependsOn(graph, id, next));
  };
```

左栏(名称 + 节点表单 + 添加)与右上 `FlowGraphView` 保持不变。把右栏「下实时提示词」那块(现 151-167 行)替换成模板 pane:

```tsx
          <div className="flex min-h-0 flex-1 flex-col gap-1.5">
            <div className="flex items-center gap-2">
              <span className="text-2xs font-medium text-foreground">
                {t("workflows.designer.templateTitle")}
              </span>
              <span className="rounded bg-primary-soft px-1.5 py-0.5 text-2xs font-medium text-primary-text">
                {t("workflows.designer.editableBadge")}
              </span>
              <div className="flex-1" />
              <div className="flex items-center gap-0.5 rounded-md bg-secondary p-0.5">
                <button
                  type="button"
                  data-testid="designer-tab-edit"
                  onClick={() => setTab("edit")}
                  className={cn(
                    "rounded px-2.5 py-0.5 text-2xs",
                    tab === "edit" ? "bg-card font-medium text-foreground shadow-sm" : "text-muted-foreground",
                  )}
                >
                  {t("workflows.designer.tabEdit")}
                </button>
                <button
                  type="button"
                  data-testid="designer-tab-preview"
                  onClick={() => setTab("preview")}
                  className={cn(
                    "rounded px-2.5 py-0.5 text-2xs",
                    tab === "preview" ? "bg-card font-medium text-foreground shadow-sm" : "text-muted-foreground",
                  )}
                >
                  {t("workflows.designer.tabPreview")}
                </button>
              </div>
            </div>

            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                data-testid="designer-insert-token"
                onClick={insertToken}
              >
                <Braces className="size-3.5" aria-hidden="true" />
                {t("workflows.designer.insertToken")} <code className="ml-1 font-mono">{DAG_TOKEN}</code>
              </Button>
              <span className="truncate text-2xs text-muted-foreground">
                {t("workflows.designer.tokenHint", { token: DAG_TOKEN })}
              </span>
            </div>

            {tab === "edit" ? (
              <Textarea
                ref={taRef}
                data-testid="designer-template-input"
                aria-label={t("workflows.designer.templateTitle")}
                value={template}
                onChange={(e) => onTemplateChange(e.target.value)}
                placeholder={t("workflows.designer.templatePlaceholder", { token: DAG_TOKEN })}
                className="min-h-0 flex-1 resize-none font-mono text-xs"
              />
            ) : previewError ? (
              <div
                data-testid="designer-template-error"
                className="min-h-0 flex-1 overflow-y-auto rounded-lg border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive"
              >
                {t("workflows.designer.templateErrorLabel")}
                {previewError}
              </div>
            ) : (
              <div
                data-testid="designer-prompt-preview"
                className="min-h-0 flex-1 overflow-y-auto rounded-lg border border-border bg-card/40 px-3 py-2"
              >
                {preview.trim() ? (
                  <MarkdownText text={preview} />
                ) : (
                  <span className="text-2xs text-muted-foreground">
                    {t("workflows.designer.previewEmpty")}
                  </span>
                )}
              </div>
            )}

            <span className="text-2xs text-muted-foreground">
              {t("workflows.designer.templateHint", { token: DAG_TOKEN })}
            </span>
          </div>
```

> `Textarea` 组件路径以本仓 shadcn 约定为准;若 `WorkflowEditorForm` 已从别处引入 textarea,复用同一路径。`ref` 透传若 `Textarea` 未 forwardRef,则退化用 `insertToken` 的 `template + DAG_TOKEN` 分支(测试仅断言 `stringContaining`,不依赖光标位)。

- [ ] **Step 5: 跑设计器测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/__tests__/workflow-dag-designer.test.tsx`
Expected: PASS

- [ ] **Step 6: i18n 校验**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS(新 key 两 locale 齐全)

- [ ] **Step 7: 提交**

```bash
git commit frontend/src/components/agentre/workflows/workflow-dag-designer.tsx \
  frontend/src/components/agentre/workflows/__tests__/workflow-dag-designer.test.tsx \
  frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json \
  -m "✨ workflow 设计器: 只读预览→可编辑模板 pane(编辑/预览/插入/报错)"
```

---

## Task 7: 弹窗接线 `workflow-manager-dialog`

草稿态 `draftContent`→`draftTemplate`(默认 `{{ DAGPrompt }}`),接 `templateError` 门控保存,DesignerPane 透传模板 props,EditorPane 文本改写 `draftTemplate`。

**Files:**
- Modify: `frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx`
- Modify: `frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx`

**Interfaces:**
- Consumes: `WorkflowDagDesigner`(Task 6)、`useWorkflows.create/update`(Task 5,第二参 = template)

- [ ] **Step 1: 改实现** `frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx`

文件顶部加常量:

```tsx
const DEFAULT_TEMPLATE = "{{ DAGPrompt }}";
```

状态:`draftContent`→`draftTemplate`,新增 `templateError`:

```tsx
  const [draftTemplate, setDraftTemplate] = React.useState(DEFAULT_TEMPLATE);
  const [templateError, setTemplateError] = React.useState(false);
```

三处初始化(mount effect / `openCreate` / `openEdit`):
- create(mount effect 与 `openCreate`):`setDraftTemplate(DEFAULT_TEMPLATE); setTemplateError(false);`(替换原 `setDraftContent("")`)。
- `openEdit`:`setDraftTemplate(w.template || DEFAULT_TEMPLATE); setTemplateError(false);`(替换原 `setDraftContent(w.content)`)。

`canSave` 增 `!templateError`:

```tsx
  const canSave =
    !submitting &&
    !templateError &&
    draftName.trim().length > 0 &&
    (draftGraph
      ? draftGraph.nodes.length > 0 && draftGraph.nodes.every((n) => n.label.trim().length > 0)
      : true);
```

`submit` 里 `draftContent`→`draftTemplate`(两处调用 `update`/`create` 的第三个实参):

```tsx
      if (editingId > 0) {
        await update(editingId, draftName.trim(), draftTemplate, draftTags, draftOutline, graphStr);
        setSelectedId(editingId);
      } else {
        await create(draftName.trim(), draftTemplate, draftTags, draftOutline, graphStr);
        setSelectedId(0);
      }
```

`DesignerPane` 调用处透传模板 props:

```tsx
                <DesignerPane
                  editing={editingId > 0}
                  name={draftName}
                  graph={draftGraph}
                  template={draftTemplate}
                  error={formError}
                  canSave={canSave}
                  onNameChange={setDraftName}
                  onGraphChange={setDraftGraph}
                  onTemplateChange={setDraftTemplate}
                  onTemplateError={setTemplateError}
                  onCancel={cancelEdit}
                  onSave={() => void submit()}
                  onKeyDown={onEditorKeyDown}
                />
```

`EditorPane` 调用处把 content 接到 draftTemplate(保留 EditorPane prop 名,仅换数据源):

```tsx
                <EditorPane
                  editing={editingId > 0}
                  name={draftName}
                  content={draftTemplate}
                  tags={draftTags}
                  outline={draftOutline}
                  error={formError}
                  canSave={canSave}
                  onNameChange={setDraftName}
                  onContentChange={setDraftTemplate}
                  onTagsChange={setDraftTags}
                  onOutlineChange={setDraftOutline}
                  onConvertToDag={() => setDraftGraph(emptyDraftGraph())}
                  onCancel={cancelEdit}
                  onSave={() => void submit()}
                  onKeyDown={onEditorKeyDown}
                />
```

`DesignerPane` 组件定义加透传 props 到 `WorkflowDagDesigner`:

```tsx
function DesignerPane({
  editing, name, graph, template, error, canSave,
  onNameChange, onGraphChange, onTemplateChange, onTemplateError,
  onCancel, onSave, onKeyDown,
}: {
  editing: boolean;
  name: string;
  graph: FlowGraph;
  template: string;
  error: string | null;
  canSave: boolean;
  onNameChange: (v: string) => void;
  onGraphChange: (g: FlowGraph) => void;
  onTemplateChange: (v: string) => void;
  onTemplateError: (hasError: boolean) => void;
  onCancel: () => void;
  onSave: () => void;
  onKeyDown: (e: React.KeyboardEvent) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-0 flex-1 flex-col" onKeyDown={onKeyDown}>
      {/* header 不变 */}
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden px-5 py-4">
        <WorkflowDagDesigner
          name={name}
          graph={graph}
          template={template}
          error={error}
          onNameChange={onNameChange}
          onGraphChange={onGraphChange}
          onTemplateChange={onTemplateChange}
          onTemplateError={onTemplateError}
        />
      </div>
      {/* footer 不变 */}
    </div>
  );
}
```

> `firstSummaryLine(w.content)` / `ViewPane` 的 `MarkdownText text={workflow.content}` 保持不变——展示仍用渲染产物 `content`。

- [ ] **Step 2: 跑弹窗测试,按新契约修断言** `frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx`

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/workflow-manager-dialog.test.tsx`

按失败项对齐:凡断言 `create`/`update` 入参含 `content:` 的,改成 `template:`(第二/第三位实参现在是模板);新建默认模板为 `{{ DAGPrompt }}`。若有「保存按钮 disabled」相关用例,补 `templateError` 场景。逐条改到绿。

- [ ] **Step 3: 跑整目录前端测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git commit frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx \
  frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx \
  -m "✨ workflow 弹窗: draftTemplate + templateError 门控保存 + 透传模板 props"
```

---

## Task 8: 重生成绑定 + 全量 gate + 手验

刷新 wails 绑定(生成物不提交),跑全量后端/前端/lint 看真 exit code,再手动验证一条流程的编辑→保存→建 Run。

**Files:**
- Regenerate (不提交): `frontend/wailsjs/`

- [ ] **Step 1: 重生成 wails 绑定**

Run: `make generate`
Expected: 无错误;`frontend/wailsjs/go/models.ts` 里 `CreateWorkflowRequest`/`UpdateWorkflowRequest` 出现 `template`、`WorkflowPreviewRequest` 出现 `template`、`WorkflowPreviewResponse` 出现 `error`、`WorkflowItem` 出现 `template`。

- [ ] **Step 2: 后端全量**

Run: `make test-backend`
Expected: PASS(exit 0)。重点包:`internal/service/workflow_svc`、`internal/app`、`internal/repository/workflow_repo`、`migrations`、`internal/service/orch_svc`(护栏 `turn_test` 仍绿)。

- [ ] **Step 3: lint(后端 + 前端)**

Run: `make lint`
Expected: PASS(exit 0)。若只跑了部分,补 `gofmt -l internal/ | head`(应为空)与 `cd frontend && pnpm exec tsc --noEmit`。

- [ ] **Step 4: 前端全量**

Run: `cd frontend && pnpm test`
Expected: PASS(看真 exit code,勿只看 tail)。

- [ ] **Step 5: 手动验证(verify skill)**

用 `verify` 或 `make dev` 起应用,走一遍:
1. 打开流程库 → 编辑内置默认流程 → 右下模板显示 `{{ DAGPrompt }}`,预览态渲染出投影正文。
2. 模板改成 `intro\n{{ DAGPrompt }}\nouttro` → 预览态出现前后包裹。
3. 把 token 改坏(`{{ DAGPromt }}`)→ 预览报错态出现、保存按钮置灰。
4. 修正并保存 → 用该流程建 Run → Leader 会话首轮系统提示词含渲染后的正文。

- [ ] **Step 6: 收尾提交(若手验发现小修)**

```bash
git commit <touched files> -m "🐛 workflow 模板: 手验修正"
```

---

## Self-Review 记录

- **Spec 覆盖**:§4 数据模型→Task 2;§5 渲染→Task 1+3;§6 预览→Task 4;§7 DTO→Task 3;§8 前端→Task 5/6/7;§9 不变量→Task 8 Step 2 护栏;§10 测试→各任务 TDD + Task 8 全量。迁移回填(带图/无图)→Task 2 Step 2+4。
- **默认模板回落**:空 `template`→`DefaultTemplate`,保证既有「传 graph 不传 template」的调用/测试仍得 DAG 投影(Task 3 `applyTemplate` + `TestCreateWorkflow_ProjectsGraphIntoContent`)。
- **类型一致**:`RenderTemplate(tmpl,name,dagPrompt)`、`DefaultTemplate`、`WorkflowPreviewGraph({name,graph,template})→{content,outline,error}`、`WorkflowDagDesigner` 三新 props 在 Task 1/4/6/7 间签名一致。
- **i18next `{{ }}` 坑**:模板占位符字面量不进译文,走 `{ token: DAG_TOKEN }` 插值 / `<code>{DAG_TOKEN}</code>`(Global Constraints + Task 6)。
