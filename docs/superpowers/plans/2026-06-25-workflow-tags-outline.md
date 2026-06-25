# 流程库 tags/outline 展示层 + Run 流程蓝图带 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 `workflow`(流程库)加两个**仅展示、绝不注入 Leader** 的结构化字段 `tags`/`outline`,让流程库/编辑器/新建 Run picker 能一眼显示「标签 + 步骤面包屑」,并在 Run 视图顶部加一条与实时执行解耦的「流程蓝图」参考带。

**Architecture:** 后端在 `workflows` 表加 `tags`/`outline` 两个 JSON-TEXT 列,经 `workflow_entity` → `workflow_svc`(DTO/请求)平铺成 `[]string` 给前端;注入链路(`orch_svc.BuildTurnExtras`)只读 `run.FlowContent`,tags/outline 不进 `OrchestrationRun`/`CreateRunRequest`,因此天然不入提示 —— 用一条 orch_svc 测试锁死此不变量。前端编辑器录入 tags/outline,管理弹窗预览渲染「蓝图 band」,Run 视图渲染「流程蓝图」参考带(读 `run.flowId` → 流程 outline,display-only)。

**Tech Stack:** Go 1.26 + cago(gormigrate / gorm / sqlmock / mockgen / goconvey)+ React 19 / TS / Vitest / react-i18next。Wails 绑定由 `make generate` 生成。

## Global Constraints

- **TDD 严格 Red→Green→Refactor**:无失败测试不写实现;每个后端改动配 sqlmock(repo)/mockgen(svc)测试,前端配 Vitest。
- **`tags`/`outline` 是 display-only,绝不注入 Leader**:注入仅来自 `run.FlowContent`(`internal/service/orch_svc/turn.go` 的 `BuildTurnExtras`)。**不得**给 `orch_entity.OrchestrationRun` / `orch_svc.CreateRunRequest` / `app.RunCreateRequest` 加 tags/outline 字段;Task 3 有断言测试锁死。
- **迁移只追加**:新迁移构造函数 append 到 `migrations/migrations.go` 的 `migrationList()` 末尾,**不改任何已存在迁移**;DDL 用原生 SQL。
- **i18n**:新增可见文案必须走 `t(...)` 并同步 `frontend/src/i18n/locales/{zh-CN,en}/common.json`;不得硬编码中文 JSX 文案。
- **表单控件**只用 shadcn `@/components/ui/*`;不加原生 `<select>`。
- **仓储单测用 sqlmock**,**服务单测用 mockgen 注入、不连 DB**。
- **commit gitmoji**;golangci-lint v2 干净。
- 后端测试用 `make test-backend`(`go test ./...` 会扫到 `frontend/node_modules`);前端用 `cd frontend && pnpm test -- <file>`。

---

### Task 1: 迁移 — `workflows` 加 `tags`/`outline` 列

**Files:**
- Create: `migrations/202606250002_workflow_tags_outline.go`
- Create: `migrations/202606250002_workflow_tags_outline_test.go`
- Modify: `migrations/migrations.go`(`migrationList()` 末尾追加一行)

**Interfaces:**
- Produces: `migration202606250002()` (gormigrate 构造函数);`workflows` 表新增列 `tags TEXT NOT NULL DEFAULT '[]'`、`outline TEXT NOT NULL DEFAULT '[]'`。

- [ ] **Step 1: 写失败的迁移测试**

`migrations/202606250002_workflow_tags_outline_test.go`:

```go
package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202606250002(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(gdb))

	require.True(t, gdb.Migrator().HasColumn("workflows", "tags"),
		"expected workflows.tags column")
	require.True(t, gdb.Migrator().HasColumn("workflows", "outline"),
		"expected workflows.outline column")

	// 默认 '[]' 可插入并读出。
	require.NoError(t, gdb.Exec(
		`INSERT INTO workflows(name,content,status,createtime,updatetime)
		 VALUES ('w','# w',1,0,0)`).Error)
	var tags, outline string
	require.NoError(t, gdb.Raw(`SELECT tags, outline FROM workflows WHERE name='w'`).
		Row().Scan(&tags, &outline))
	require.Equal(t, "[]", tags)
	require.Equal(t, "[]", outline)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./migrations/ -run TestMigration202606250002 -v`
Expected: FAIL —— `migration202606250002` 未定义(编译错误)。

- [ ] **Step 3: 写迁移**

`migrations/202606250002_workflow_tags_outline.go`:

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606250002 流程库 tags/outline:给人看的展示层(标签+步骤概览),
// JSON 数组存 TEXT,绝不注入 Leader(注入仍只 run.flow_content)。
func migration202606250002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606250002",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE workflows ADD COLUMN tags TEXT NOT NULL DEFAULT '[]'`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE workflows ADD COLUMN outline TEXT NOT NULL DEFAULT '[]'`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE workflows DROP COLUMN outline`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE workflows DROP COLUMN tags`).Error
		},
	}
}
```

- [ ] **Step 4: 把迁移追加进 `migrationList()`**

`migrations/migrations.go` —— 在 `migration202606250001(), // hooks.interpreter_path` 这一行**之后**追加:

```go
		migration202606250002(), // workflows.tags/outline:流程库展示层(标签/步骤概览)
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./migrations/ -run TestMigration202606250002 -v`
Expected: PASS。再跑全量迁移测试确认没破坏顺序:`go test ./migrations/`,Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add migrations/202606250002_workflow_tags_outline.go migrations/202606250002_workflow_tags_outline_test.go migrations/migrations.go
git commit -m "✨ 迁移:workflows 加 tags/outline 列(流程库展示层)"
```

---

### Task 2: 后端 entity + svc DTO/请求 + 编解码 + Create/Update/toItem 接线

**Files:**
- Modify: `internal/model/entity/workflow_entity/workflow.go`(加 2 字段)
- Modify: `internal/service/workflow_svc/types.go`(DTO/请求加 2 字段)
- Modify: `internal/service/workflow_svc/workflow.go`(编解码 + Create/Update/toItem)
- Modify: `internal/service/workflow_svc/workflow_test.go`(新增断言)

**Interfaces:**
- Consumes: Task 1 的 `workflows.tags/outline` 列。
- Produces:
  - `workflow_entity.Workflow.Tags string`、`.Outline string`(JSON 文本,gorm 列 `tags`/`outline`)。
  - `workflow_svc.WorkflowItem.Tags []string`、`.Outline []string`。
  - `workflow_svc.CreateWorkflowRequest.Tags []string`、`.Outline []string`;`UpdateWorkflowRequest.Tags []string`、`.Outline []string`。
  - 包内 helper:`encodeStringList(v []string) string`、`decodeStringList(s string) []string`。

- [ ] **Step 1: 写失败的 svc 测试(扩展 `workflow_test.go`)**

在 `internal/service/workflow_svc/workflow_test.go` 末尾追加:

```go
func TestCreateWorkflow_TagsOutline(t *testing.T) {
	convey.Convey("新建流程带 tags/outline", t, func() {
		ctx, wfMock, _, svc := setupSvc(t)

		convey.Convey("tags/outline 编码成 JSON 落库,DTO 平铺回 []string", func() {
			wfMock.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, w *workflow_entity.Workflow) error {
					assert.Equal(t, `["通用","新功能"]`, w.Tags)
					assert.Equal(t, `["需求拆解","方案设计"]`, w.Outline)
					w.ID = 5
					return nil
				})
			resp, err := svc.Create(ctx, &CreateWorkflowRequest{
				Name:    "标准功能开发流",
				Content: "# 标准功能开发流",
				Tags:    []string{"通用", "新功能"},
				Outline: []string{"需求拆解", "方案设计"},
			})
			assert.NoError(t, err)
			assert.Equal(t, []string{"通用", "新功能"}, resp.Item.Tags)
			assert.Equal(t, []string{"需求拆解", "方案设计"}, resp.Item.Outline)
		})

		convey.Convey("空 tags/outline 编码成 []，DTO 平铺成 nil", func() {
			wfMock.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, w *workflow_entity.Workflow) error {
					assert.Equal(t, "[]", w.Tags)
					assert.Equal(t, "[]", w.Outline)
					return nil
				})
			resp, err := svc.Create(ctx, &CreateWorkflowRequest{Name: "x"})
			assert.NoError(t, err)
			assert.Empty(t, resp.Item.Tags)
			assert.Empty(t, resp.Item.Outline)
		})
	})
}

func TestListWorkflow_DecodesTagsOutline(t *testing.T) {
	convey.Convey("列表解码 tags/outline", t, func() {
		ctx, wfMock, runMock, svc := setupSvc(t)
		wfMock.EXPECT().List(gomock.Any()).Return([]*workflow_entity.Workflow{
			{ID: 1, Name: "x", Tags: `["修复"]`, Outline: `["复现","定位"]`, Status: 1},
		}, nil)
		runMock.EXPECT().List(gomock.Any()).Return(nil, nil)
		resp, err := svc.List(ctx, &ListWorkflowsRequest{})
		assert.NoError(t, err)
		assert.Equal(t, []string{"修复"}, resp.Items[0].Tags)
		assert.Equal(t, []string{"复现", "定位"}, resp.Items[0].Outline)
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/service/workflow_svc/ -run 'TagsOutline|DecodesTagsOutline' -v`
Expected: FAIL —— `CreateWorkflowRequest` 无 `Tags`/`Outline` 字段(编译错误)。

- [ ] **Step 3a: entity 加字段**

`internal/model/entity/workflow_entity/workflow.go` —— 在 `Content` 字段之后、`Status` 之前插入:

```go
	Tags       string `gorm:"column:tags;type:text;not null;default:'[]'"`    // JSON []string,仅展示,不注入
	Outline    string `gorm:"column:outline;type:text;not null;default:'[]'"` // JSON []string,仅展示,不注入
```

- [ ] **Step 3b: types.go DTO/请求加字段**

`internal/service/workflow_svc/types.go`:

`WorkflowItem` 结构体里 `Content` 之后加:

```go
	Tags    []string `json:"tags"`
	Outline []string `json:"outline"`
```

`CreateWorkflowRequest` 里 `Content` 之后加:

```go
	Tags    []string `json:"tags"`
	Outline []string `json:"outline"`
```

`UpdateWorkflowRequest` 里 `Content` 之后加:

```go
	Tags    []string `json:"tags"`
	Outline []string `json:"outline"`
```

- [ ] **Step 3c: workflow.go 编解码 + 接线**

`internal/service/workflow_svc/workflow.go`:

import 区加 `"encoding/json"`(放在 `"context"` 之后、`"strings"` 之前,保持字母序由 goimports 处理)。

在 `toItem` 之前加 helper:

```go
// decodeStringList 把 JSON 文本解成 []string;空/非法 → nil(DTO 给前端就是空数组)。
func decodeStringList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// encodeStringList 把 []string 编成 JSON 文本;空 → "[]"。
func encodeStringList(v []string) string {
	if len(v) == 0 {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
```

改 `toItem`(加 Tags/Outline 解码):

```go
func toItem(w *workflow_entity.Workflow, runCount int) *WorkflowItem {
	return &WorkflowItem{
		ID:         w.ID,
		Name:       w.Name,
		Content:    w.Content,
		Tags:       decodeStringList(w.Tags),
		Outline:    decodeStringList(w.Outline),
		RunCount:   runCount,
		Createtime: w.Createtime,
		Updatetime: w.Updatetime,
	}
}
```

改 `Create`(组装 entity 时编码):在 `w := &workflow_entity.Workflow{...}` 里 `Content: req.Content,` 之后加:

```go
		Tags:    encodeStringList(req.Tags),
		Outline: encodeStringList(req.Outline),
```

改 `Update`(`w.Content = req.Content` 之后加):

```go
	w.Tags = encodeStringList(req.Tags)
	w.Outline = encodeStringList(req.Outline)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/service/workflow_svc/ -v`
Expected: PASS(含既有用例 + 新增 tags/outline 用例)。

- [ ] **Step 5: 提交**

```bash
git add internal/model/entity/workflow_entity/workflow.go internal/service/workflow_svc/types.go internal/service/workflow_svc/workflow.go internal/service/workflow_svc/workflow_test.go
git commit -m "✨ workflow_svc:tags/outline 展示层字段(entity/DTO/请求 + JSON 编解码)"
```

---

### Task 3: 不变量测试 —— tags/outline 绝不进 Leader 注入

**Files:**
- Modify: `internal/service/orch_svc/turn_test.go`(无则 Create;放 `BuildTurnExtras` 的注入测试)

**Interfaces:**
- Consumes: Task 2 的字段;`orch_svc.(*orchSvc).BuildTurnExtras`(`internal/service/orch_svc/turn.go`,只读 `run.FlowContent`)。
- Produces: 一条锁死「注入只来自 `FlowContent`、不含 tags/outline」的回归测试。

**说明:** `BuildTurnExtras` 只从 `run.FlowContent` 拼 system-prompt 后缀,**根本不读 workflow 的 tags/outline**。本测试构造一个 `FlowContent` 只含正文、tags/outline 仅以哨兵串单独存在的场景,断言注入后缀含正文、**不含哨兵**,从而锁死:未来若有人把 tags/outline 拼进注入,此测试会红。

- [ ] **Step 1: 看 orch_svc 测试如何注入 mock(读现有 turn 测试/依赖)**

Run: `ls internal/service/orch_svc/ && grep -rn "RegisterDeps\|tasks \|runs \|FindBySession\|s.runs\|s.tasks\|mock_orch_repo\|setupOrch\|newSvcForTest" internal/service/orch_svc/*_test.go | head -30`
Expected: 看清 `orchSvc` 的 `tasks`/`runs` 字段如何在测试里被替成 mock(`mock_orch_repo.NewMockTaskRepo`/`NewMockRunRepo`),以及 agent 入参怎么造(`agent_entity.Agent` + `ToolEnabled(agenttool.KeyOrchestrate)`)。**按现有测试的 setup 习惯**写下面的测试(若已有 helper 复用之)。

- [ ] **Step 2: 写失败的不变量测试**

在 `internal/service/orch_svc/turn_test.go` 追加(若无此文件则 Create,`package orch_svc`):

```go
func TestBuildTurnExtras_NeverInjectsTagsOutline(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)

	const sessionID int64 = 42
	// 根任务(ParentTaskID==0),指向 run 7。
	tasks.EXPECT().FindBySession(gomock.Any(), sessionID).
		Return(&orch_entity.Task{ID: 100, RunID: 7, ParentTaskID: 0}, nil)
	// run 的 FlowContent 只含正文;tags/outline 的哨兵串绝不应出现在注入里。
	runs.EXPECT().Find(gomock.Any(), int64(7)).Return(&orch_entity.OrchestrationRun{
		ID: 7, RootTaskID: 100, FlowContent: "正文-CONTENT-SENTINEL",
	}, nil)

	s := &orchSvc{tasks: tasks, runs: runs}
	leader := &agent_entity.Agent{ID: 1}
	leader.SetTools([]agent_entity.AgentTool{{Key: agenttool.KeyOrchestrate, Enabled: true}})

	_, suffix, ok := s.BuildTurnExtras(context.Background(), leader, sessionID, 0)
	assert.True(t, ok)
	assert.Contains(t, suffix, "正文-CONTENT-SENTINEL")
	assert.NotContains(t, suffix, "TAG-SENTINEL")
	assert.NotContains(t, suffix, "OUTLINE-SENTINEL")
}
```

> 注:`leader.SetTools(...)` 的具体造法以 Step 1 看到的现有测试为准(项目里 agent 工具门控有统一 helper / `tools_json`);目标是让 `leader.ToolEnabled(agenttool.KeyOrchestrate)` 为真。import 需含 `mock_orch_repo`、`orch_entity`、`agent_entity`、`agenttool`、`assert`、`gomock`、`context`、`testing`。

- [ ] **Step 3: 跑测试确认失败(或先因 setup 编译失败)**

Run: `go test ./internal/service/orch_svc/ -run TestBuildTurnExtras_NeverInjectsTagsOutline -v`
Expected: 先因 import/ctor 编译失败 → 按 Step 1 的既有 setup 修正到能编译;编译通过后该测试应**直接 PASS**(因为当前实现本就不注入 tags/outline)。这是一条**护栏测试**:它在当前代码即绿,目的是锁死未来不被改红。

- [ ] **Step 4: 确认通过**

Run: `go test ./internal/service/orch_svc/ -run TestBuildTurnExtras_NeverInjectsTagsOutline -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/service/orch_svc/turn_test.go
git commit -m "✅ orch_svc:护栏测试——tags/outline 绝不进 Leader 注入(只 FlowContent)"
```

---

### Task 4: 重新生成 Wails 绑定

**Files:**
- 生成物:`frontend/wailsjs/go/models.ts`(`workflow_svc.WorkflowItem` 等)

**Interfaces:**
- Produces: TS 侧 `models` 的 `WorkflowItem.tags: string[]`、`.outline: string[]`;`WorkflowCreate`/`WorkflowUpdate` 入参含 `tags`/`outline`。

- [ ] **Step 1: 生成**

Run: `cd /Users/codfrm/Code/agentre/agentre && make generate`
Expected: 无错误,`frontend/wailsjs/go/models.ts` 更新。

- [ ] **Step 2: 验证生成内容**

Run: `grep -n "tags\|outline" frontend/wailsjs/go/models.ts | head`
Expected: 看到 `WorkflowItem` 的 `tags`/`outline`(`string[]`)字段。

- [ ] **Step 3: 提交**

```bash
git add frontend/wailsjs/go/models.ts
git commit -m "🤖 generate:workflow tags/outline 绑定"
```

> `frontend/wailsjs/` 是 gitignore 生成物之分情况;若被忽略则跳过提交,仅本地生成供前端编译。

---

### Task 5: i18n 文案(zh + en)

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`

**Interfaces:**
- Produces: 键 `workflows.editor.{tags,tagsHint,tagsPlaceholder,outline,outlineHint,outlinePlaceholder,moveUp,moveDown,removeItem}`、`workflows.preview.{blueprintTags,blueprintSteps,blueprintHint}`、`orchestration.run.{blueprintTitle,blueprintRef}`。

- [ ] **Step 1: zh-CN 加键**

`frontend/src/i18n/locales/zh-CN/common.json` —— 在 `workflows.editor` 对象内加(与既有 `name`/`content` 同级):

```json
        "tags": "标签",
        "tagsHint": "仅展示给人,不注入 AI",
        "tagsPlaceholder": "输入标签后回车",
        "outline": "步骤(概览)",
        "outlineHint": "给人看的流程骨架,可增删排序;仅展示,不约束 AI",
        "outlinePlaceholder": "输入步骤后回车",
        "moveUp": "上移",
        "moveDown": "下移",
        "removeItem": "删除"
```

在 `workflows.preview` 对象内加:

```json
        "blueprintTags": "标签",
        "blueprintSteps": "步骤",
        "blueprintHint": "仅供一眼读懂流程 · 不约束 AI"
```

在 `orchestration.run` 对象内加(无则新建该对象):

```json
        "blueprintTitle": "流程蓝图",
        "blueprintRef": "仅参考 · 不约束执行"
```

- [ ] **Step 2: en 加同结构键**

`frontend/src/i18n/locales/en/common.json` 对应位置:

```json
        "tags": "Tags",
        "tagsHint": "For humans only — not sent to the AI",
        "tagsPlaceholder": "Type a tag, press Enter",
        "outline": "Steps (overview)",
        "outlineHint": "A human-readable skeleton; add/remove/reorder. Display only — does not constrain the AI",
        "outlinePlaceholder": "Type a step, press Enter",
        "moveUp": "Move up",
        "moveDown": "Move down",
        "removeItem": "Remove"
```
```json
        "blueprintTags": "Tags",
        "blueprintSteps": "Steps",
        "blueprintHint": "At-a-glance only · does not constrain the AI"
```
```json
        "blueprintTitle": "Flow blueprint",
        "blueprintRef": "Reference only · does not constrain execution"
```

- [ ] **Step 3: 跑 i18n 覆盖测试**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS(zh/en 键齐、无缺失)。

- [ ] **Step 4: 提交**

```bash
git add frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "🌐 流程库 tags/outline + Run 流程蓝图 i18n"
```

---

### Task 6: `use-workflows` hook —— create/update 带 tags/outline

**Files:**
- Modify: `frontend/src/hooks/use-workflows.ts`
- Test: `frontend/src/hooks/__tests__/use-workflows.test.ts`(无则 Create)

**Interfaces:**
- Consumes: Task 4 的绑定。
- Produces:
  - `WorkflowItem` 类型加 `tags: string[]`、`outline: string[]`。
  - `create(name, content, tags: string[], outline: string[])`
  - `update(id, name, content, tags: string[], outline: string[])`

- [ ] **Step 1: 写失败的 hook 测试**

`frontend/src/hooks/__tests__/use-workflows.test.ts`:

```ts
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const WorkflowCreate = vi.fn().mockResolvedValue({});
const WorkflowUpdate = vi.fn().mockResolvedValue({});
const WorkflowDelete = vi.fn().mockResolvedValue({});
const WorkflowList = vi.fn().mockResolvedValue({ items: [] });

vi.mock("../../../wailsjs/go/app/App", () => ({
  WorkflowCreate: (...a: unknown[]) => WorkflowCreate(...a),
  WorkflowUpdate: (...a: unknown[]) => WorkflowUpdate(...a),
  WorkflowDelete: (...a: unknown[]) => WorkflowDelete(...a),
  WorkflowList: (...a: unknown[]) => WorkflowList(...a),
}));

import { useWorkflows } from "../use-workflows";

describe("useWorkflows tags/outline", () => {
  beforeEach(() => vi.clearAllMocks());

  it("create 透传 tags/outline", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(WorkflowList).toHaveBeenCalled());
    await act(async () => {
      await result.current.create("n", "c", ["通用"], ["需求拆解", "方案设计"]);
    });
    expect(WorkflowCreate).toHaveBeenCalledWith({
      name: "n",
      content: "c",
      tags: ["通用"],
      outline: ["需求拆解", "方案设计"],
    });
  });

  it("update 透传 tags/outline", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(WorkflowList).toHaveBeenCalled());
    await act(async () => {
      await result.current.update(3, "n2", "c2", ["修复"], ["复现"]);
    });
    expect(WorkflowUpdate).toHaveBeenCalledWith({
      id: 3,
      name: "n2",
      content: "c2",
      tags: ["修复"],
      outline: ["复现"],
    });
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/hooks/__tests__/use-workflows.test.ts`
Expected: FAIL —— `create` 只接 2 参,`WorkflowCreate` 未带 tags/outline。

- [ ] **Step 3: 改 hook**

`frontend/src/hooks/use-workflows.ts`:

`WorkflowItem` 类型 `content: string;` 之后加:

```ts
  tags: string[];
  outline: string[];
```

`reload` 里 `.map((i) => ({...}))` 加:

```ts
          tags: i.tags ?? [],
          outline: i.outline ?? [],
```

`create` 改:

```ts
  const create = useCallback(
    async (name: string, content: string, tags: string[], outline: string[]) => {
      await WorkflowCreate({ name, content, tags, outline });
      await reload();
    },
    [reload],
  );
```

`update` 改:

```ts
  const update = useCallback(
    async (
      id: number,
      name: string,
      content: string,
      tags: string[],
      outline: string[],
    ) => {
      await WorkflowUpdate({ id, name, content, tags, outline });
      await reload();
    },
    [reload],
  );
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/hooks/__tests__/use-workflows.test.ts`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/hooks/use-workflows.ts frontend/src/hooks/__tests__/use-workflows.test.ts
git commit -m "✨ use-workflows:create/update 带 tags/outline"
```

---

### Task 7: `WorkflowEditorForm` —— 标签 chips + 步骤 outline 列表

**Files:**
- Modify: `frontend/src/components/agentre/workflows/workflow-editor-form.tsx`
- Test: `frontend/src/components/agentre/workflows/workflow-editor-form.test.tsx`(无则 Create)

**Interfaces:**
- Consumes: i18n 键(Task 5)。
- Produces: `WorkflowEditorFormProps` 加 `tags: string[]`、`outline: string[]`、`onTagsChange: (v: string[]) => void`、`onOutlineChange: (v: string[]) => void`。

- [ ] **Step 1: 写失败的组件测试**

`frontend/src/components/agentre/workflows/workflow-editor-form.test.tsx`:

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { WorkflowEditorForm } from "./workflow-editor-form";

const base = {
  name: "n",
  content: "c",
  error: null,
  onNameChange: vi.fn(),
  onContentChange: vi.fn(),
};

describe("WorkflowEditorForm tags/outline", () => {
  it("回车添加标签 → onTagsChange 追加", () => {
    const onTagsChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        tags={["通用"]}
        outline={[]}
        onTagsChange={onTagsChange}
        onOutlineChange={vi.fn()}
      />,
    );
    const input = screen.getByTestId("workflow-tags-input");
    fireEvent.change(input, { target: { value: "新功能" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onTagsChange).toHaveBeenCalledWith(["通用", "新功能"]);
  });

  it("点标签 × → onTagsChange 移除", () => {
    const onTagsChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        tags={["通用", "新功能"]}
        outline={[]}
        onTagsChange={onTagsChange}
        onOutlineChange={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId("workflow-tag-remove-0"));
    expect(onTagsChange).toHaveBeenCalledWith(["新功能"]);
  });

  it("回车添加步骤 → onOutlineChange 追加", () => {
    const onOutlineChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        tags={[]}
        outline={["需求拆解"]}
        onTagsChange={vi.fn()}
        onOutlineChange={onOutlineChange}
      />,
    );
    const input = screen.getByTestId("workflow-outline-input");
    fireEvent.change(input, { target: { value: "方案设计" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onOutlineChange).toHaveBeenCalledWith(["需求拆解", "方案设计"]);
  });

  it("删除步骤 → onOutlineChange 去掉该项", () => {
    const onOutlineChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        tags={[]}
        outline={["需求拆解", "方案设计"]}
        onTagsChange={vi.fn()}
        onOutlineChange={onOutlineChange}
      />,
    );
    fireEvent.click(screen.getByTestId("workflow-outline-remove-0"));
    expect(onOutlineChange).toHaveBeenCalledWith(["方案设计"]);
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/workflow-editor-form.test.tsx`
Expected: FAIL —— 组件不接 tags/outline props,找不到 testid。

- [ ] **Step 3: 改组件**

`frontend/src/components/agentre/workflows/workflow-editor-form.tsx` —— 替换整文件:

```tsx
import * as React from "react";
import { ArrowDown, ArrowUp, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

export type WorkflowEditorFormProps = {
  name: string;
  content: string;
  tags: string[];
  outline: string[];
  error: string | null;
  onNameChange: (v: string) => void;
  onContentChange: (v: string) => void;
  onTagsChange: (v: string[]) => void;
  onOutlineChange: (v: string[]) => void;
};

// 受控编辑表单:名称 + 标签(chips) + 步骤(outline 有序) + 正文(Markdown)。
// 标签/步骤是给人看的展示层,绝不注入 AI(见 hint)。提交由宿主统一管理。
export function WorkflowEditorForm({
  name,
  content,
  tags,
  outline,
  error,
  onNameChange,
  onContentChange,
  onTagsChange,
  onOutlineChange,
}: WorkflowEditorFormProps) {
  const { t } = useTranslation();
  const [tagDraft, setTagDraft] = React.useState("");
  const [stepDraft, setStepDraft] = React.useState("");

  const insertTemplate = () => {
    const tpl = t("workflows.editor.template");
    onContentChange(content.trim() ? `${content.trimEnd()}\n\n${tpl}` : tpl);
  };

  const addTag = () => {
    const v = tagDraft.trim();
    if (!v || tags.includes(v)) {
      setTagDraft("");
      return;
    }
    onTagsChange([...tags, v]);
    setTagDraft("");
  };
  const addStep = () => {
    const v = stepDraft.trim();
    if (!v) return;
    onOutlineChange([...outline, v]);
    setStepDraft("");
  };
  const moveStep = (i: number, d: -1 | 1) => {
    const j = i + d;
    if (j < 0 || j >= outline.length) return;
    const next = [...outline];
    [next[i], next[j]] = [next[j], next[i]];
    onOutlineChange(next);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3.5">
      {/* 名称 */}
      <label className="flex flex-col gap-1.5 text-xs">
        <span className="font-medium text-foreground">
          {t("workflows.editor.name")}
          <span className="ml-0.5 text-destructive">*</span>
        </span>
        <Input
          data-testid="workflow-name-input"
          aria-label={t("workflows.editor.name")}
          value={name}
          onChange={(e) => onNameChange(e.target.value)}
          placeholder={t("workflows.editor.namePlaceholder")}
          className="h-9 text-xs"
        />
      </label>

      {/* 标签 */}
      <div className="flex flex-col gap-1.5 text-xs">
        <span className="font-medium text-foreground">
          {t("workflows.editor.tags")}
        </span>
        <div className="flex flex-wrap items-center gap-1.5">
          {tags.map((tag, i) => (
            <span
              key={`${tag}-${i}`}
              className="flex items-center gap-1 rounded bg-accent px-1.5 py-0.5 text-foreground"
            >
              {tag}
              <button
                type="button"
                data-testid={`workflow-tag-remove-${i}`}
                aria-label={t("workflows.editor.removeItem")}
                onClick={() => onTagsChange(tags.filter((_, k) => k !== i))}
              >
                <X className="size-3 text-muted-foreground" aria-hidden="true" />
              </button>
            </span>
          ))}
          <Input
            data-testid="workflow-tags-input"
            aria-label={t("workflows.editor.tags")}
            value={tagDraft}
            onChange={(e) => setTagDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addTag();
              }
            }}
            placeholder={t("workflows.editor.tagsPlaceholder")}
            className="h-7 w-40 text-2xs"
          />
        </div>
        <span className="text-2xs text-muted-foreground">
          {t("workflows.editor.tagsHint")}
        </span>
      </div>

      {/* 步骤(概览) */}
      <div className="flex flex-col gap-1.5 text-xs">
        <span className="font-medium text-foreground">
          {t("workflows.editor.outline")}
        </span>
        <div className="flex flex-col gap-1.5">
          {outline.map((step, i) => (
            <div key={`${step}-${i}`} className="flex items-center gap-2">
              <span className="w-4 shrink-0 text-center text-2xs text-muted-foreground">
                {i + 1}
              </span>
              <Input
                aria-label={`${t("workflows.editor.outline")} ${i + 1}`}
                value={step}
                onChange={(e) =>
                  onOutlineChange(
                    outline.map((s, k) => (k === i ? e.target.value : s)),
                  )
                }
                className="h-7 flex-1 text-2xs"
              />
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={t("workflows.editor.moveUp")}
                onClick={() => moveStep(i, -1)}
              >
                <ArrowUp className="size-3" aria-hidden="true" />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={t("workflows.editor.moveDown")}
                onClick={() => moveStep(i, 1)}
              >
                <ArrowDown className="size-3" aria-hidden="true" />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                data-testid={`workflow-outline-remove-${i}`}
                aria-label={t("workflows.editor.removeItem")}
                onClick={() => onOutlineChange(outline.filter((_, k) => k !== i))}
              >
                <X className="size-3" aria-hidden="true" />
              </Button>
            </div>
          ))}
          <Input
            data-testid="workflow-outline-input"
            aria-label={t("workflows.editor.outline")}
            value={stepDraft}
            onChange={(e) => setStepDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addStep();
              }
            }}
            placeholder={t("workflows.editor.outlinePlaceholder")}
            className="h-7 text-2xs"
          />
        </div>
        <span className="text-2xs text-muted-foreground">
          {t("workflows.editor.outlineHint")}
        </span>
      </div>

      {/* 正文(Markdown) */}
      <div className="flex min-h-0 flex-1 flex-col gap-1.5 text-xs">
        <span className="flex items-center justify-between font-medium text-foreground">
          <span>{t("workflows.editor.content")}</span>
          <Button
            type="button"
            variant="link"
            size="sm"
            data-testid="workflow-insert-template-button"
            className="h-auto p-0 text-2xs"
            onClick={insertTemplate}
          >
            {t("workflows.editor.insertTemplate")}
          </Button>
        </span>
        <Textarea
          data-testid="workflow-content-input"
          aria-label={t("workflows.editor.content")}
          value={content}
          onChange={(e) => onContentChange(e.target.value)}
          className="min-h-0 flex-1 resize-none font-mono text-xs"
        />
      </div>

      {error ? (
        <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
          {error}
        </div>
      ) : null}
    </div>
  );
}
```

> 若 `size="icon-xs"` 不是 button 组件的合法 size,改用 `size="icon-sm"`(以 `@/components/ui/button` 实际枚举为准,Step 4 报错即调整)。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/workflow-editor-form.test.tsx`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/workflows/workflow-editor-form.tsx frontend/src/components/agentre/workflows/workflow-editor-form.test.tsx
git commit -m "✨ 流程编辑器:标签 chips + 步骤 outline 录入(仅展示不注入)"
```

---

### Task 8: `WorkflowManagerDialog` —— 接 draftTags/draftOutline + 列表标签 chip + 预览蓝图 band

**Files:**
- Modify: `frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx`
- Test: `frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx`(无则 Create;按现有 wails mock 规则 per-file mock)

**Interfaces:**
- Consumes: Task 6 的 `create/update(name,content,tags,outline)`、`WorkflowItem.tags/outline`;Task 7 的 `WorkflowEditorForm` 新 props;i18n(Task 5)。

- [ ] **Step 1: 写失败的测试(保存带 tags/outline + 预览渲染蓝图)**

`frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx`:

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const create = vi.fn().mockResolvedValue(undefined);
const update = vi.fn().mockResolvedValue(undefined);
const remove = vi.fn().mockResolvedValue(true);
let items: unknown[] = [];
vi.mock("@/hooks/use-workflows", () => ({
  useWorkflows: () => ({
    workflows: items,
    loading: false,
    error: null,
    reload: vi.fn(),
    create,
    update,
    remove,
  }),
}));

import { WorkflowManagerDialog } from "./workflow-manager-dialog";
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

describe("WorkflowManagerDialog tags/outline", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    items = [];
    useWorkflowManagerStore.setState({ open: true, intent: "create" });
  });

  it("新建保存时把 tags/outline 一并提交", async () => {
    render(<WorkflowManagerDialog />);
    fireEvent.change(screen.getByTestId("workflow-name-input"), {
      target: { value: "标准功能开发流" },
    });
    const tagInput = screen.getByTestId("workflow-tags-input");
    fireEvent.change(tagInput, { target: { value: "通用" } });
    fireEvent.keyDown(tagInput, { key: "Enter" });
    const stepInput = screen.getByTestId("workflow-outline-input");
    fireEvent.change(stepInput, { target: { value: "需求拆解" } });
    fireEvent.keyDown(stepInput, { key: "Enter" });
    fireEvent.click(screen.getByTestId("workflow-save-button"));
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith(
        "标准功能开发流",
        "",
        ["通用"],
        ["需求拆解"],
      ),
    );
  });

  it("预览态渲染蓝图 band(标签 + 步骤面包屑)", () => {
    items = [
      {
        id: 1,
        name: "标准功能开发流",
        content: "# 标准功能开发流",
        tags: ["通用"],
        outline: ["需求拆解", "方案设计"],
        runCount: 0,
        createtime: 0,
        updatetime: 0,
      },
    ];
    useWorkflowManagerStore.setState({ open: true, intent: "browse" });
    render(<WorkflowManagerDialog />);
    fireEvent.click(screen.getByTestId("workflow-row-1"));
    expect(screen.getByTestId("workflow-blueprint-band")).toBeInTheDocument();
    expect(screen.getByText("需求拆解")).toBeInTheDocument();
    expect(screen.getByText("方案设计")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/workflow-manager-dialog.test.tsx`
Expected: FAIL —— 保存没传 tags/outline;无 `workflow-blueprint-band`。

- [ ] **Step 3: 改弹窗**

`frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx`:

(a) 在 `WorkflowManagerBody` 的 `const [draftContent, setDraftContent] = React.useState("");` 之后加:

```tsx
  const [draftTags, setDraftTags] = React.useState<string[]>([]);
  const [draftOutline, setDraftOutline] = React.useState<string[]>([]);
```

(b) `intent==="create"` 的初始化 effect 里、`setDraftContent("");` 之后加:

```tsx
      setDraftTags([]);
      setDraftOutline([]);
```

(c) `openCreate` 里 `setDraftContent("");` 之后加 `setDraftTags([]); setDraftOutline([]);`;
`openEdit` 里 `setDraftContent(w.content);` 之后加 `setDraftTags(w.tags); setDraftOutline(w.outline);`。

(d) `submit` 里:
`await update(editingId, draftName.trim(), draftContent);` → `await update(editingId, draftName.trim(), draftContent, draftTags, draftOutline);`
`await create(draftName.trim(), draftContent);` → `await create(draftName.trim(), draftContent, draftTags, draftOutline);`

(e) `EditorPane` 调用处把新 props 透传:

```tsx
              <EditorPane
                editing={editingId > 0}
                name={draftName}
                content={draftContent}
                tags={draftTags}
                outline={draftOutline}
                error={formError}
                canSave={canSave}
                onNameChange={setDraftName}
                onContentChange={setDraftContent}
                onTagsChange={setDraftTags}
                onOutlineChange={setDraftOutline}
                onCancel={cancelEdit}
                onSave={() => void submit()}
                onKeyDown={onEditorKeyDown}
              />
```

`EditorPane` 函数签名与内部 `<WorkflowEditorForm/>` 同步加 `tags/outline/onTagsChange/onOutlineChange` 透传(在 `content`/`onContentChange` 旁)。

(f) 列表行加标签 chip:在 `filtered.map` 行内 `{w.name}` 的 `<span>` 之后、runCount pill 之前插:

```tsx
                      {w.tags.length > 0 ? (
                        <span className="shrink-0 rounded bg-accent px-1 py-0.5 text-2xs text-muted-foreground">
                          {w.tags[0]}
                        </span>
                      ) : null}
```

(g) `ViewPane` 的 `<MarkdownText text={workflow.content} />` 那个滚动容器**之前**插入蓝图 band:

```tsx
      {workflow.tags.length > 0 || workflow.outline.length > 0 ? (
        <div
          data-testid="workflow-blueprint-band"
          className="flex flex-col gap-2 border-b border-border bg-muted/40 px-5 py-3"
        >
          {workflow.tags.length > 0 ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-2xs text-subtle-foreground">
                {t("workflows.preview.blueprintTags")}
              </span>
              {workflow.tags.map((tag, i) => (
                <span
                  key={`${tag}-${i}`}
                  className="rounded bg-accent px-1.5 py-0.5 text-2xs text-foreground"
                >
                  {tag}
                </span>
              ))}
            </div>
          ) : null}
          {workflow.outline.length > 0 ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-2xs text-subtle-foreground">
                {t("workflows.preview.blueprintSteps")}
              </span>
              {workflow.outline.map((step, i) => (
                <React.Fragment key={`${step}-${i}`}>
                  {i > 0 ? (
                    <span className="text-2xs text-subtle-foreground">›</span>
                  ) : null}
                  <span className="rounded border border-border bg-card px-1.5 py-0.5 text-2xs text-foreground">
                    {step}
                  </span>
                </React.Fragment>
              ))}
            </div>
          ) : null}
          <span className="text-2xs text-subtle-foreground">
            {t("workflows.preview.blueprintHint")}
          </span>
        </div>
      ) : null}
```

> `ViewPane` / `EditorPane` 顶部需要 `import * as React from "react";`(若未导入则加);`useTranslation` 的 `t` 在 `ViewPane` 已有。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/workflows/workflow-manager-dialog.test.tsx`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx frontend/src/components/agentre/workflows/workflow-manager-dialog.test.tsx
git commit -m "✨ 流程库弹窗:列表标签 chip + 预览蓝图 band + 保存 tags/outline"
```

---

### Task 9: `run-new-dialog` 从流程库 picker —— 标签 chip + 步骤面包屑

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/run-new-dialog.tsx`
- Test: `frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`(无则 Create,per-file wails mock)

**Interfaces:**
- Consumes: `WorkflowList()` 返回的 `items[].tags/outline`(Task 4 绑定)。

**说明:** 现有 picker 用 shadcn `Select` 列流程名;升级成「单选行列表(名称 + 标签 chip + 步骤面包屑)」。`WorkflowOption` 类型扩展带 tags/outline,加载时映射进来。

- [ ] **Step 1: 写失败的测试**

`frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`:

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const ListChatAgents = vi.fn().mockResolvedValue({ agents: [] });
const WorkflowList = vi.fn().mockResolvedValue({
  items: [
    { id: 1, name: "标准功能开发流", tags: ["通用"], outline: ["需求拆解", "方案设计"] },
  ],
});
const RunCreate = vi.fn().mockResolvedValue({ run: { id: 1 } });
vi.mock("../../../../wailsjs/go/app/App", () => ({
  ListChatAgents: (...a: unknown[]) => ListChatAgents(...a),
  WorkflowList: (...a: unknown[]) => WorkflowList(...a),
  RunCreate: (...a: unknown[]) => RunCreate(...a),
}));

import { RunNewDialog } from "../run-new-dialog";

describe("RunNewDialog flow picker tags/outline", () => {
  beforeEach(() => vi.clearAllMocks());

  it("从流程库模式下,流程行显示标签 + 步骤面包屑", async () => {
    render(<RunNewDialog open onOpenChange={vi.fn()} />);
    await waitFor(() => expect(WorkflowList).toHaveBeenCalled());
    // 切到「流程库」模式(实现里通过 flowMode 状态;测试用 testid 触发 —— 见实现 Step 3)
    screen.getByTestId("run-flow-mode-library").click();
    expect(await screen.findByText("标准功能开发流")).toBeInTheDocument();
    expect(screen.getByText("通用")).toBeInTheDocument();
    expect(screen.getByText("需求拆解")).toBeInTheDocument();
    expect(screen.getByText("方案设计")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`
Expected: FAIL —— picker 无标签/步骤,无 `run-flow-mode-library` testid。

- [ ] **Step 3: 改 picker**

`frontend/src/components/agentre/orchestration/run-new-dialog.tsx`:

(a) `WorkflowOption` 类型扩展:

```tsx
type WorkflowOption = {
  id: number;
  name: string;
  tags: string[];
  outline: string[];
};
```

(b) `WorkflowList().then(...)` 的 map 改:

```tsx
        setWorkflows(
          (resp?.items ?? []).map(
            (w: { id: number; name: string; tags?: string[]; outline?: string[] }) => ({
              id: w.id,
              name: w.name,
              tags: w.tags ?? [],
              outline: w.outline ?? [],
            }),
          ),
        );
```

(c) 给「流程模式」三个 `SelectItem` 加可点的 testid 不便(shadcn Select 是 portal),为测试稳定，**在 flowMode 区块下方**保留 Select,同时把 `flowMode==="library"` 的下拉替换成单选行列表:把原 `{flowMode === "library" ? (<label>…<Select>…</Select></label>) : null}` 整块替换为:

```tsx
          {flowMode === "library" ? (
            <div className="flex flex-col gap-1.5 text-xs">
              <span className="font-medium text-foreground">
                {t("orchestration.new.flowSelect")}
              </span>
              <div className="flex flex-col gap-1.5">
                {workflows.map((w) => (
                  <button
                    key={w.id}
                    type="button"
                    data-testid={`run-flow-pick-${w.id}`}
                    onClick={() => setFlowId(w.id)}
                    className={
                      flowId === w.id
                        ? "flex flex-col gap-1 rounded-md border border-primary bg-primary-soft px-3 py-2 text-left"
                        : "flex flex-col gap-1 rounded-md border border-border px-3 py-2 text-left hover:bg-accent/50"
                    }
                  >
                    <span className="flex items-center gap-2">
                      <span className="font-medium text-foreground">{w.name}</span>
                      {w.tags[0] ? (
                        <span className="rounded bg-accent px-1 py-0.5 text-2xs text-muted-foreground">
                          {w.tags[0]}
                        </span>
                      ) : null}
                    </span>
                    {w.outline.length > 0 ? (
                      <span className="flex flex-wrap items-center gap-1">
                        {w.outline.map((step, i) => (
                          <React.Fragment key={`${step}-${i}`}>
                            {i > 0 ? (
                              <span className="text-2xs text-subtle-foreground">›</span>
                            ) : null}
                            <span className="rounded border border-border bg-card px-1.5 py-0.5 text-2xs text-muted-foreground">
                              {step}
                            </span>
                          </React.Fragment>
                        ))}
                      </span>
                    ) : null}
                  </button>
                ))}
              </div>
            </div>
          ) : null}
```

(d) 为让测试能切到 library 模式,给 flowMode 的 `SelectContent` 之外加一个隐藏的快捷触发不优雅;改测试策略:**直接在测试里 set flowMode**不可行(内部 state)。改为给三态加一行 segmented 按钮替代 Select 也可,但超范围。**最小改动**:在 flowMode `Select` 这块的 `<SelectTrigger>` 同级加一组**仅测试可见**的隐藏按钮不合规。**采用**:把 flowMode 从 `Select` 换成三个 shadcn `Button`(toggle 组),每个带 `data-testid={`run-flow-mode-${mode}`}`。替换 flowMode 区块为:

```tsx
          <div className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("orchestration.new.flow")}
            </span>
            <div className="flex gap-1.5">
              {(["none", "library", "adhoc"] as FlowMode[]).map((m) => (
                <Button
                  key={m}
                  type="button"
                  size="sm"
                  variant={flowMode === m ? "default" : "outline"}
                  data-testid={`run-flow-mode-${m}`}
                  onClick={() => setFlowMode(m)}
                >
                  {t(
                    m === "none"
                      ? "orchestration.new.flowNone"
                      : m === "library"
                        ? "orchestration.new.flowLibrary"
                        : "orchestration.new.flowAdhoc",
                  )}
                </Button>
              ))}
            </div>
          </div>
```

并在文件顶部确保 `import * as React from "react";` 与 `import { Button } from "@/components/ui/button";`(已存在则不重复)。删除不再使用的 `Select*` import(若 adhoc/leader 仍用 Select 则保留)。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`
Expected: PASS。再跑既有 run-new-dialog 相关测试确认没回归:`cd frontend && pnpm test -- run-new-dialog`。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/orchestration/run-new-dialog.tsx frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx
git commit -m "✨ 新建 Run·从流程库:picker 行显示标签 + 步骤面包屑"
```

---

### Task 10 (S2): Run 流程蓝图参考带

**Files:**
- Create: `frontend/src/components/agentre/orchestration/run-flow-blueprint.tsx`
- Create: `frontend/src/components/agentre/orchestration/__tests__/run-flow-blueprint.test.tsx`
- Modify: `frontend/src/components/agentre/orchestration/index.tsx`(在 `<RunHeader/>` 下挂载)

**Interfaces:**
- Consumes: `useWorkflows()`(Task 6:`workflows[].{name,outline}`);`detail.run.flowId`(`RunItemDTO.flowId`)。
- Produces: `RunFlowBlueprint({ flowId }: { flowId: number })`。

**说明:** 蓝图带是**给人看的、与执行解耦**的参考。读 `flowId` → 在已加载流程里找到该流程 → 渲染「流程蓝图:名称 + 步骤面包屑 + 仅参考·不约束执行」。`flowId===0` 或找不到则渲染 `null`。**不读注入、不读任务状态** —— 纯展示。

- [ ] **Step 1: 写失败的组件测试**

`frontend/src/components/agentre/orchestration/__tests__/run-flow-blueprint.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

let items: unknown[] = [];
vi.mock("@/hooks/use-workflows", () => ({
  useWorkflows: () => ({ workflows: items }),
}));

import { RunFlowBlueprint } from "../run-flow-blueprint";

describe("RunFlowBlueprint", () => {
  it("flowId=0 → 不渲染", () => {
    items = [];
    const { container } = render(<RunFlowBlueprint flowId={0} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("找到流程 → 渲染名称 + 步骤面包屑 + 参考提示", () => {
    items = [
      { id: 7, name: "标准功能开发流", outline: ["需求拆解", "灰度上线"], tags: [], content: "", runCount: 0, createtime: 0, updatetime: 0 },
    ];
    render(<RunFlowBlueprint flowId={7} />);
    expect(screen.getByText("标准功能开发流")).toBeInTheDocument();
    expect(screen.getByText("需求拆解")).toBeInTheDocument();
    expect(screen.getByText("灰度上线")).toBeInTheDocument();
    expect(screen.getByTestId("run-flow-blueprint")).toBeInTheDocument();
  });

  it("flowId 找不到对应流程 → 不渲染", () => {
    items = [{ id: 1, name: "x", outline: [], tags: [], content: "", runCount: 0, createtime: 0, updatetime: 0 }];
    const { container } = render(<RunFlowBlueprint flowId={7} />);
    expect(container).toBeEmptyDOMElement();
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/run-flow-blueprint.test.tsx`
Expected: FAIL —— 模块不存在。

- [ ] **Step 3: 写组件**

`frontend/src/components/agentre/orchestration/run-flow-blueprint.tsx`:

```tsx
import * as React from "react";
import { Route } from "lucide-react";
import { useTranslation } from "react-i18next";

import { useWorkflows } from "@/hooks/use-workflows";

// Run 流程蓝图参考带:给人看的、与实时执行解耦的「这条 Run 套用的流程概览」。
// 读 flowId → 流程 outline,纯展示;不读注入、不读任务状态。flowId=0 或找不到 → null。
export function RunFlowBlueprint({ flowId }: { flowId: number }) {
  const { t } = useTranslation();
  const { workflows } = useWorkflows();
  const flow = flowId > 0 ? workflows.find((w) => w.id === flowId) : undefined;
  if (!flow) return null;

  return (
    <div
      data-testid="run-flow-blueprint"
      className="flex items-center gap-2.5 border-b border-border bg-muted/40 px-4 py-2"
    >
      <Route className="size-3.5 shrink-0 text-subtle-foreground" aria-hidden="true" />
      <span className="text-2xs text-subtle-foreground">
        {t("orchestration.run.blueprintTitle")}
      </span>
      <span className="text-xs font-medium text-foreground">{flow.name}</span>
      {flow.outline.length > 0 ? (
        <span className="flex flex-wrap items-center gap-1">
          {flow.outline.map((step, i) => (
            <React.Fragment key={`${step}-${i}`}>
              {i > 0 ? (
                <span className="text-2xs text-subtle-foreground">›</span>
              ) : null}
              <span className="rounded border border-border bg-card px-1.5 py-0.5 text-2xs text-muted-foreground">
                {step}
              </span>
            </React.Fragment>
          ))}
        </span>
      ) : null}
      <span className="ml-auto shrink-0 rounded-full bg-accent px-2 py-0.5 text-2xs text-muted-foreground">
        {t("orchestration.run.blueprintRef")}
      </span>
    </div>
  );
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/run-flow-blueprint.test.tsx`
Expected: PASS。

- [ ] **Step 5: 挂载到 Run 视图**

`frontend/src/components/agentre/orchestration/index.tsx`:
顶部 import 加:

```tsx
import { RunFlowBlueprint } from "./run-flow-blueprint";
```

在 `<RunHeader detail={detail} view={view} onView={setView} />` 这一行**之后**插:

```tsx
            <RunFlowBlueprint flowId={detail.run.flowId} />
```

> `detail.run.flowId` 来自 `RunItemDTO.flowId`(已有)。若 TS 报 `flowId` 可空,用 `detail.run.flowId ?? 0`。

- [ ] **Step 6: 跑 Run 容器相关测试 + 全量前端**

Run: `cd frontend && pnpm test -- orchestration`
Expected: PASS(含既有编排测试无回归)。

- [ ] **Step 7: 提交**

```bash
git add frontend/src/components/agentre/orchestration/run-flow-blueprint.tsx frontend/src/components/agentre/orchestration/__tests__/run-flow-blueprint.test.tsx frontend/src/components/agentre/orchestration/index.tsx
git commit -m "✨ Run 流程蓝图参考带(读 flowId outline,与执行解耦)"
```

---

### Task 11: 全量校验 + 收尾

**Files:** 无(校验)

- [ ] **Step 1: 后端全量**

Run: `cd /Users/codfrm/Code/agentre/agentre && make test-backend`
Expected: PASS。

- [ ] **Step 2: 前端全量 + lint**

Run: `cd /Users/codfrm/Code/agentre/agentre && make lint && cd frontend && pnpm test`
Expected: PASS(含 i18n 覆盖、no-literal-string)。

- [ ] **Step 3: 若有 lint/格式问题就地修,提交**

```bash
git add -A
git commit -m "🎨 流程库 tags/outline + Run 蓝图带:lint/格式收尾"
```

---

## 自检结论(spec coverage)

- §6.1「数据模型:tags/outline display-only、绝不注入」→ Task 1/2(字段)+ Task 3(护栏测试)。
- §6.1「编辑器 chips + 有序列表录入,标注仅展示」→ Task 5(hint 文案)+ Task 7。
- §3 表行 8「流程库弹窗预览蓝图 band + 列表标签 chip」→ Task 8。
- §3 表行 7 / §5.12「picker 行标签 + 步骤面包屑」→ Task 9。
- §3 表行 3 / §6.1「Run 流程蓝图参考带,与执行解耦」→ Task 10。
- §9「迁移末尾 append、native SQL」→ Task 1。
- i18n 同步 zh/en → Task 5。
- **未覆盖(明确排除,留给其它切片)**:库模式 `flowId→content` 注入快照的既有缺口(本切片不动注入)、reorder 拖拽(本切片用 ▲▼ 替代)、命令面板入口(已存在)。
