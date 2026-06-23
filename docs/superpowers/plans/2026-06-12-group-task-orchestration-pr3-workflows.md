# 群任务卡编排 PR3(流程库)实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 落地 spec `docs/superpowers/specs/2026-06-11-group-task-orchestration-design.md` §3.2/§6.1/§6.3/§8 的 PR3 流程库:workflow service 层 + 4 个 Wails 绑定、流程库管理页(列表 + 正文预览 + 编辑弹窗带「插入骨架模板」+ 删除确认提示使用中群数)、建群弹窗「协作流程」下拉(替换写死的 `workflowID: 0`)。

**Architecture:** 后端 PR1 已铺好 `workflow_entity`(Name/Content/Status,`IsActive`/`Check`)、`workflow_repo` 全套 CRUD(含 mockgen mock)、`groups.workflow_id` 列与注入逻辑(`group_svc/group.go:611` 按 `Find`+`IsActive` 门控)。本 PR 只补:`workflow_svc`(List 聚合「使用中群数」= active 群按 `WorkflowID` 计数;Create/Update/Delete 带 `WorkflowNotFound` 校验)→ `internal/app/workflow.go` 薄绑定(parse → svc → return)→ 前端 `use-workflows` hook + `workflows/` 页面组件 + 建群弹窗下拉。无新表、无迁移、无新事件。

**Tech Stack:** Go(goconvey + testify + mockgen,svc 不连库)/ React 19 / TypeScript / react-i18next / shadcn(`@/components/ui/*`)/ Vitest + @testing-library/react。

---

## 后端接缝(PR1 已落地,直接用)

- `internal/model/entity/workflow_entity/workflow.go`:`Workflow{ID, Name, Content, Status, Createtime, Updatetime}`,表名 `workflows`;`IsActive()`(nil 安全)、`Check(ctx)`(name 必填,空名返回 `code.InvalidParameter`)。
- `internal/repository/workflow_repo/workflow.go`:接口 `Create/Update/Find/List/Delete` + `Workflow()/RegisterWorkflow()` accessor;`List` 只返回 `status=ACTIVE` 按 `updatetime DESC`;`Find` **不过滤软删**(调用方用 `IsActive()` 门控);`Delete` 是软删(status=DELETE)。mock 在 `mock_workflow_repo/`。时间戳为 **UnixMilli**(前端 `new Date(updatetime)` 直接可用)。
- `internal/repository/group_repo/group.go`:`GroupRepo.List(ctx)` 返回 active 群(含 `WorkflowID` 字段);mock 在 `mock_group_repo/`(`MockGroupRepo`)。
- `group_svc.CreateGroupRequest` 已有 `WorkflowID` 字段,`internal/app/group.go` 的 `GroupCreate` 已透传;前端 `group-new-dialog.tsx:85` 目前写死 `workflowID: 0`。
- 注入侧(主持人每轮读绑定流程)在 `group_svc`,已实现,**本 PR 不碰**;删除流程后群按「不绑定」处理是注入侧既有行为,本 PR 无需任何配合代码。

## 文件结构

| 文件 | 动作 | 职责 |
| ---- | ---- | ---- |
| `internal/service/workflow_svc/types.go` | Create | Wails 暴露的请求/响应类型(json tag 明确) |
| `internal/service/workflow_svc/workflow.go` | Create | svc 接口 + 实现(List 聚合群数 / Create / Update / Delete) |
| `internal/service/workflow_svc/workflow_test.go` | Create | goconvey + mock repo 注入,不连库 |
| `internal/pkg/code/code.go` + `zh_cn.go` + `en.go` | Modify | `WorkflowNotFound`(20800 段)双语文案 |
| `internal/app/workflow.go` | Create | 4 个绑定:WorkflowList/Create/Update/Delete |
| `frontend/src/hooks/use-workflows.ts` | Create | 列表加载 + create/update/remove 封装 |
| `frontend/src/hooks/use-workflows.test.ts` | Create | hook 行为测试 |
| `frontend/src/__tests__/mocks/wailsApp.ts` | Modify | 补 4 个 Workflow* 导出(否则 App.test 等全局别名测试会因缺命名导出而炸) |
| `frontend/src/components/agentre/workflows/workflows-page.tsx` | Create | 管理页:左列表 + 右预览面板 |
| `frontend/src/components/agentre/workflows/workflows-page.test.tsx` | Create | 页面集成测试 |
| `frontend/src/components/agentre/workflows/workflow-edit-dialog.tsx` | Create | 新建/编辑弹窗(名称 + Markdown 正文 + 插入骨架模板) |
| `frontend/src/components/agentre/workflows/workflow-edit-dialog.test.tsx` | Create | 弹窗行为测试 |
| `frontend/src/components/agentre/workflows/workflow-delete-dialog.tsx` | Create | 删除确认(提示使用中群数) |
| `frontend/src/components/agentre/index.ts` | Modify | 导出 `WorkflowsPage` |
| `frontend/src/App.tsx` | Modify | nav 入口(/workflows,与 /org 同级)+ 路由 + 面包屑 |
| `frontend/src/components/agentre/group-chat/group-new-dialog.tsx` | Modify | 「协作流程」下拉(可选,默认不绑定) |
| `frontend/src/components/agentre/group-chat/group-new-dialog.test.tsx` | Modify | 下拉选择 → `GroupCreate` 带 workflowID |
| `frontend/src/i18n/locales/{zh-CN,en}/common.json` | Modify | `nav.workflows` + `workflows.*` + `group.new.workflow*` |

**不做**(spec 其他 PR / Out of scope):e2e(PR4)、`group_create`(PR5)、流程 AI 生成草稿(已砍,本期靠骨架模板)、注入逻辑改动(PR1 已做)、workflow 与部门/项目关联(正交)、列表分页/搜索(YAGNI,桌面应用流程数量小)。

## 执行前置

在隔离 worktree 执行(superpowers:using-git-worktrees),基于 `develop/group` 开分支 `feature/group-task-pr3`。本仓 worktree 坑(memory 已踩过):

```bash
git worktree add ../agentre-pr3 -b feature/group-task-pr3 develop/group
cd ../agentre-pr3
mkdir -p frontend/dist && touch frontend/dist/.gitkeep   # go:embed 占位
GOWORK=off make generate                                  # wailsjs 是 gitignore 生成物
cd frontend && pnpm install
```

- Go 命令一律 `GOWORK=off`(worktree 不在 go.work 的 use 列表里)。
- 前端测试 `cd frontend && pnpm test -- <path>`(focused)或 `pnpm test`(全量);git 写操作需关 sandbox。
- Task 3 加了 Wails 绑定后要**再跑一次 `GOWORK=off make generate`** 刷新 wailsjs,前端才能 import `WorkflowList` 等。

---

### Task 1: workflow_svc 骨架 + List(含使用中群数)

**Files:**
- Create: `internal/service/workflow_svc/types.go`
- Create: `internal/service/workflow_svc/workflow.go`
- Test: `internal/service/workflow_svc/workflow_test.go`

- [x] **Step 1: 写失败的测试**

新建 `internal/service/workflow_svc/workflow_test.go`:

```go
package workflow_svc

import (
	"context"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo/mock_workflow_repo"
)

func setupSvc(t *testing.T) (
	context.Context,
	*mock_workflow_repo.MockWorkflowRepo,
	*mock_group_repo.MockGroupRepo,
	*workflowSvc,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	wfMock := mock_workflow_repo.NewMockWorkflowRepo(ctrl)
	groupMock := mock_group_repo.NewMockGroupRepo(ctrl)
	workflow_repo.RegisterWorkflow(wfMock)
	group_repo.RegisterGroup(groupMock)
	return context.Background(), wfMock, groupMock, &workflowSvc{}
}

func TestListWorkflows(t *testing.T) {
	convey.Convey("流程列表", t, func() {
		ctx, wfMock, groupMock, svc := setupSvc(t)

		convey.Convey("返回流程并统计使用中群数", func() {
			wfMock.EXPECT().List(gomock.Any()).Return([]*workflow_entity.Workflow{
				{ID: 1, Name: "产品开发流程", Content: "# 产品开发流程", Status: 1, Updatetime: 200},
				{ID: 2, Name: "紧急修复流程", Content: "# 紧急修复流程", Status: 1, Updatetime: 100},
			}, nil)
			groupMock.EXPECT().List(gomock.Any()).Return([]*group_entity.Group{
				{ID: 10, WorkflowID: 1},
				{ID: 11, WorkflowID: 1},
				{ID: 12, WorkflowID: 0},
			}, nil)
			resp, err := svc.List(ctx, &ListWorkflowsRequest{})
			assert.NoError(t, err)
			assert.Len(t, resp.Items, 2)
			assert.Equal(t, int64(1), resp.Items[0].ID)
			assert.Equal(t, 2, resp.Items[0].GroupCount)
			assert.Equal(t, 0, resp.Items[1].GroupCount)
		})

		convey.Convey("空库返回空列表(非 nil)", func() {
			wfMock.EXPECT().List(gomock.Any()).Return(nil, nil)
			groupMock.EXPECT().List(gomock.Any()).Return(nil, nil)
			resp, err := svc.List(ctx, &ListWorkflowsRequest{})
			assert.NoError(t, err)
			assert.NotNil(t, resp.Items)
			assert.Empty(t, resp.Items)
		})
	})
}
```

- [x] **Step 2: 跑测试确认编译失败(红)**

```bash
GOWORK=off go test -race ./internal/service/workflow_svc/...
```

预期:FAIL,`undefined: workflowSvc` / `undefined: ListWorkflowsRequest`(包还没有实现文件——新单元的正确失败方式)。

- [x] **Step 3: 写实现**

新建 `internal/service/workflow_svc/types.go`:

```go
// Package workflow_svc 暴露流程库的应用服务接口与请求/响应类型。
//
// 类型定义直接被 Wails 绑定层引用,会被 wails dev / wails build 提取为 TypeScript
// 类型暴露给前端,因此字段名要稳定、json tag 要明确。
package workflow_svc

// WorkflowItem 单条流程(含使用中群数,给列表/预览/删除确认用)。
type WorkflowItem struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	GroupCount int    `json:"groupCount"`
	Createtime int64  `json:"createtime"`
	Updatetime int64  `json:"updatetime"`
}

// ListWorkflowsRequest 占位。
type ListWorkflowsRequest struct{}

// ListWorkflowsResponse 全部 active 流程(repo 按 updatetime 倒序)。
type ListWorkflowsResponse struct {
	Items []*WorkflowItem `json:"items"`
}
```

新建 `internal/service/workflow_svc/workflow.go`:

```go
// Package workflow_svc 流程(剧本库)业务服务:列表/增改删,供 Wails 绑定层调用。
// 流程注入(主持人每轮读绑定流程)在 group_svc,不在本包。
package workflow_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo"
)

// WorkflowSvc 流程库应用服务。
type WorkflowSvc interface {
	List(ctx context.Context, req *ListWorkflowsRequest) (*ListWorkflowsResponse, error)
}

type workflowSvc struct{}

var defaultWorkflow WorkflowSvc = &workflowSvc{}

// Workflow 取默认服务单例。
func Workflow() WorkflowSvc { return defaultWorkflow }

// groupCounts 统计每个流程被多少个 active 群绑定(列表「使用中群数」与删除确认提示用)。
func (s *workflowSvc) groupCounts(ctx context.Context) (map[int64]int, error) {
	groups, err := group_repo.Group().List(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int)
	for _, g := range groups {
		if g.WorkflowID > 0 {
			counts[g.WorkflowID]++
		}
	}
	return counts, nil
}

func toItem(w *workflow_entity.Workflow, groupCount int) *WorkflowItem {
	return &WorkflowItem{
		ID:         w.ID,
		Name:       w.Name,
		Content:    w.Content,
		GroupCount: groupCount,
		Createtime: w.Createtime,
		Updatetime: w.Updatetime,
	}
}

// List 返回全部 active 流程 + 各自使用中群数。
func (s *workflowSvc) List(ctx context.Context, _ *ListWorkflowsRequest) (*ListWorkflowsResponse, error) {
	rows, err := workflow_repo.Workflow().List(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.groupCounts(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]*WorkflowItem, 0, len(rows))
	for _, w := range rows {
		items = append(items, toItem(w, counts[w.ID]))
	}
	return &ListWorkflowsResponse{Items: items}, nil
}
```

- [x] **Step 4: 跑测试确认通过(绿)**

```bash
GOWORK=off go test -race ./internal/service/workflow_svc/...
```

预期:PASS。

- [x] **Step 5: 提交**

```bash
git add internal/service/workflow_svc/
git commit -m "✨ workflow: workflow_svc 骨架 + List(聚合使用中群数)"
```

---

### Task 2: workflow_svc Create/Update/Delete + WorkflowNotFound 错误码

**Files:**
- Modify: `internal/pkg/code/code.go`(文件末尾追加 20800 段)
- Modify: `internal/pkg/code/zh_cn.go` / `internal/pkg/code/en.go`(消息 map 末尾追加)
- Modify: `internal/service/workflow_svc/types.go`
- Modify: `internal/service/workflow_svc/workflow.go`
- Test: `internal/service/workflow_svc/workflow_test.go`

- [x] **Step 1: 写失败的测试**

`workflow_test.go` import 增加 `"github.com/cago-frame/cago/pkg/consts"`,末尾追加:

```go
func TestCreateWorkflow(t *testing.T) {
	convey.Convey("新建流程", t, func() {
		ctx, wfMock, _, svc := setupSvc(t)

		convey.Convey("成功:trim 名称并落库", func() {
			wfMock.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, w *workflow_entity.Workflow) error {
					assert.Equal(t, "产品开发流程", w.Name)
					assert.Equal(t, consts.ACTIVE, w.Status)
					w.ID = 7
					return nil
				})
			resp, err := svc.Create(ctx, &CreateWorkflowRequest{Name: "  产品开发流程 ", Content: "# 产品开发流程"})
			assert.NoError(t, err)
			assert.Equal(t, int64(7), resp.Item.ID)
			assert.Equal(t, 0, resp.Item.GroupCount)
		})

		convey.Convey("空名拒绝", func() {
			resp, err := svc.Create(ctx, &CreateWorkflowRequest{Name: "   "})
			assert.Error(t, err)
			assert.Nil(t, resp)
		})
	})
}

func TestUpdateWorkflow(t *testing.T) {
	convey.Convey("编辑流程", t, func() {
		ctx, wfMock, groupMock, svc := setupSvc(t)

		convey.Convey("成功:改名改正文并回带群数", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&workflow_entity.Workflow{ID: 3, Name: "旧名", Status: 1}, nil)
			wfMock.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, w *workflow_entity.Workflow) error {
					assert.Equal(t, "新名", w.Name)
					assert.Equal(t, "## 新正文", w.Content)
					return nil
				})
			groupMock.EXPECT().List(gomock.Any()).
				Return([]*group_entity.Group{{ID: 1, WorkflowID: 3}}, nil)
			resp, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 3, Name: " 新名 ", Content: "## 新正文"})
			assert.NoError(t, err)
			assert.Equal(t, 1, resp.Item.GroupCount)
		})

		convey.Convey("不存在报 WorkflowNotFound", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(99)).Return(nil, nil)
			resp, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 99, Name: "x"})
			assert.Error(t, err)
			assert.Nil(t, resp)
		})

		convey.Convey("已软删的报 WorkflowNotFound", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(4)).
				Return(&workflow_entity.Workflow{ID: 4, Name: "已删", Status: 0}, nil)
			_, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 4, Name: "x"})
			assert.Error(t, err)
		})

		convey.Convey("改成空名拒绝", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&workflow_entity.Workflow{ID: 3, Name: "旧名", Status: 1}, nil)
			_, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 3, Name: "  "})
			assert.Error(t, err)
		})
	})
}

func TestDeleteWorkflow(t *testing.T) {
	convey.Convey("删除流程", t, func() {
		ctx, wfMock, _, svc := setupSvc(t)

		convey.Convey("成功软删", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&workflow_entity.Workflow{ID: 3, Name: "旧", Status: 1}, nil)
			wfMock.EXPECT().Delete(gomock.Any(), int64(3)).Return(nil)
			_, err := svc.Delete(ctx, &DeleteWorkflowRequest{ID: 3})
			assert.NoError(t, err)
		})

		convey.Convey("不存在报错", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(9)).Return(nil, nil)
			_, err := svc.Delete(ctx, &DeleteWorkflowRequest{ID: 9})
			assert.Error(t, err)
		})
	})
}
```

- [x] **Step 2: 跑测试确认编译失败(红)**

```bash
GOWORK=off go test -race ./internal/service/workflow_svc/...
```

预期:FAIL,`undefined: CreateWorkflowRequest` 等。

- [x] **Step 3: 写实现**

`internal/pkg/code/code.go` 文件末尾追加(20800 段未被占用,沿用现有分段注释风格):

```go
// 流程库(workflow)20800~
const (
	WorkflowNotFound = iota + 20800 // 流程不存在
)
```

`internal/pkg/code/zh_cn.go` 消息 map 末尾(`}` 前)追加,对齐既有条目缩进:

```go
	// 流程库
	WorkflowNotFound: "流程不存在",
```

`internal/pkg/code/en.go` 消息 map 末尾(`}` 前)追加:

```go
	// workflow
	WorkflowNotFound: "Workflow not found",
```

`types.go` 末尾追加:

```go
// CreateWorkflowRequest 新建流程(name 必填,trim 后校验)。
type CreateWorkflowRequest struct {
	Name    string `json:"name" binding:"required"`
	Content string `json:"content"`
}

type CreateWorkflowResponse struct {
	Item *WorkflowItem `json:"item"`
}

// UpdateWorkflowRequest 编辑流程名称/正文;进行中的群下一轮注入即取到最新正文。
type UpdateWorkflowRequest struct {
	ID      int64  `json:"id" binding:"required"`
	Name    string `json:"name" binding:"required"`
	Content string `json:"content"`
}

type UpdateWorkflowResponse struct {
	Item *WorkflowItem `json:"item"`
}

// DeleteWorkflowRequest 软删流程;已绑定的群按「不绑定」处理(注入侧 IsActive 门控)。
type DeleteWorkflowRequest struct {
	ID int64 `json:"id" binding:"required"`
}

type DeleteWorkflowResponse struct{}
```

`workflow.go`:接口扩成四个方法,import 增加 `"strings"`、`"github.com/cago-frame/cago/pkg/consts"`、`"github.com/cago-frame/cago/pkg/i18n"`、`"github.com/agentre-ai/agentre/internal/pkg/code"`:

```go
// WorkflowSvc 流程库应用服务。
type WorkflowSvc interface {
	List(ctx context.Context, req *ListWorkflowsRequest) (*ListWorkflowsResponse, error)
	Create(ctx context.Context, req *CreateWorkflowRequest) (*CreateWorkflowResponse, error)
	Update(ctx context.Context, req *UpdateWorkflowRequest) (*UpdateWorkflowResponse, error)
	Delete(ctx context.Context, req *DeleteWorkflowRequest) (*DeleteWorkflowResponse, error)
}
```

实现方法追加到文件末尾:

```go
// findActive 取 active 流程;不存在或已软删返回 WorkflowNotFound。
func (s *workflowSvc) findActive(ctx context.Context, id int64) (*workflow_entity.Workflow, error) {
	w, err := workflow_repo.Workflow().Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if !w.IsActive() {
		return nil, i18n.NewError(ctx, code.WorkflowNotFound)
	}
	return w, nil
}

// Create 新建流程。
func (s *workflowSvc) Create(ctx context.Context, req *CreateWorkflowRequest) (*CreateWorkflowResponse, error) {
	w := &workflow_entity.Workflow{
		Name:    strings.TrimSpace(req.Name),
		Content: req.Content,
		Status:  consts.ACTIVE,
	}
	if err := w.Check(ctx); err != nil {
		return nil, err
	}
	if err := workflow_repo.Workflow().Create(ctx, w); err != nil {
		return nil, err
	}
	return &CreateWorkflowResponse{Item: toItem(w, 0)}, nil
}

// Update 编辑流程名称/正文;改动对已绑定的进行中群下一轮即生效(spec §6.1)。
func (s *workflowSvc) Update(ctx context.Context, req *UpdateWorkflowRequest) (*UpdateWorkflowResponse, error) {
	w, err := s.findActive(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	w.Name = strings.TrimSpace(req.Name)
	w.Content = req.Content
	if err := w.Check(ctx); err != nil {
		return nil, err
	}
	if err := workflow_repo.Workflow().Update(ctx, w); err != nil {
		return nil, err
	}
	counts, err := s.groupCounts(ctx)
	if err != nil {
		return nil, err
	}
	return &UpdateWorkflowResponse{Item: toItem(w, counts[w.ID])}, nil
}

// Delete 软删流程;已绑定该流程的群按「不绑定」处理(注入侧跳过,不报错)。
func (s *workflowSvc) Delete(ctx context.Context, req *DeleteWorkflowRequest) (*DeleteWorkflowResponse, error) {
	if _, err := s.findActive(ctx, req.ID); err != nil {
		return nil, err
	}
	if err := workflow_repo.Workflow().Delete(ctx, req.ID); err != nil {
		return nil, err
	}
	return &DeleteWorkflowResponse{}, nil
}
```

- [x] **Step 4: 跑测试确认通过(绿)**

```bash
GOWORK=off go test -race ./internal/service/workflow_svc/... ./internal/pkg/code/...
```

预期:PASS。

- [x] **Step 5: 提交**

```bash
git add internal/service/workflow_svc/ internal/pkg/code/
git commit -m "✨ workflow: svc Create/Update/Delete + WorkflowNotFound 错误码"
```

---

### Task 3: Wails 绑定 + 刷新 wailsjs

**Files:**
- Create: `internal/app/workflow.go`

绑定层只做 parse → `svc.Xxx().Method` → return,不写业务逻辑、不触 repo(AGENTS.md 硬规则),与 `internal/app/department.go` 同构,无单独测试(逻辑全在 Task 1/2 的 svc 测试里)。

- [x] **Step 1: 写绑定**

新建 `internal/app/workflow.go`:

```go
package app

import (
	"github.com/agentre-ai/agentre/internal/service/workflow_svc"
)

// WorkflowList 流程库列表(含每条流程的使用中群数)。
func (a *App) WorkflowList() (*workflow_svc.ListWorkflowsResponse, error) {
	return workflow_svc.Workflow().List(a.ctx, &workflow_svc.ListWorkflowsRequest{})
}

// WorkflowCreate 新建流程。
func (a *App) WorkflowCreate(req *workflow_svc.CreateWorkflowRequest) (*workflow_svc.CreateWorkflowResponse, error) {
	return workflow_svc.Workflow().Create(a.ctx, req)
}

// WorkflowUpdate 编辑流程(名称/正文);进行中的群下一轮即注入最新正文。
func (a *App) WorkflowUpdate(req *workflow_svc.UpdateWorkflowRequest) (*workflow_svc.UpdateWorkflowResponse, error) {
	return workflow_svc.Workflow().Update(a.ctx, req)
}

// WorkflowDelete 软删流程;已绑定的群按「不绑定」处理。
func (a *App) WorkflowDelete(req *workflow_svc.DeleteWorkflowRequest) (*workflow_svc.DeleteWorkflowResponse, error) {
	return workflow_svc.Workflow().Delete(a.ctx, req)
}
```

- [x] **Step 2: 编译 + 刷新 wailsjs + 后端全量**

```bash
GOWORK=off make generate
GOWORK=off make test-backend
```

预期:generate 后 `frontend/wailsjs/go/app/App.d.ts` 出现 `WorkflowList/WorkflowCreate/WorkflowUpdate/WorkflowDelete`;test-backend 全 PASS。

- [x] **Step 3: 提交**

```bash
git add internal/app/workflow.go
git commit -m "✨ workflow: Wails 绑定 WorkflowList/Create/Update/Delete"
```

(wailsjs 是 gitignore 生成物,不提交。)

---

### Task 4: nav 入口 + 路由 + 页面骨架 + i18n 基础 key

**Files:**
- Create: `frontend/src/components/agentre/workflows/workflows-page.tsx`(本任务先落骨架,Task 6 替换为完整实现)
- Modify: `frontend/src/components/agentre/index.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json` + `frontend/src/i18n/locales/en/common.json`
- Test: `frontend/src/components/agentre/workflows/workflows-page.test.tsx`

- [x] **Step 1: 写失败的测试**

新建 `frontend/src/components/agentre/workflows/workflows-page.test.tsx`(测试 harness 跑 en 文案):

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { WorkflowsPage } from "./workflows-page";

describe("WorkflowsPage 骨架", () => {
  it("渲染标题与新建按钮", () => {
    render(<WorkflowsPage />);
    expect(screen.getByRole("heading", { name: "Workflows" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "New workflow" })).toBeTruthy();
  });
});
```

- [x] **Step 2: 跑测试确认失败(红)**

```bash
cd frontend && pnpm test -- src/components/agentre/workflows/workflows-page.test.tsx
```

预期:FAIL,模块不存在。

- [x] **Step 3: 写骨架 + 接线**

新建 `frontend/src/components/agentre/workflows/workflows-page.tsx`:

```tsx
import * as React from "react";
import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";

export function WorkflowsPage() {
  const { t } = useTranslation();
  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
        <div className="flex min-w-0 flex-col gap-0.5">
          <h1 className="text-sm font-semibold text-foreground">
            {t("workflows.title")}
          </h1>
          <p className="truncate text-2xs text-muted-foreground">
            {t("workflows.subtitle")}
          </p>
        </div>
        <Button type="button" size="sm">
          <Plus className="size-3.5" aria-hidden="true" />
          {t("workflows.new")}
        </Button>
      </header>
      <div className="flex flex-1 items-center justify-center text-xs text-muted-foreground">
        {t("workflows.empty")}
      </div>
    </div>
  );
}
```

`frontend/src/components/agentre/index.ts` 追加(按字母序插入既有导出之间):

```ts
export { WorkflowsPage } from "./workflows/workflows-page";
```

`frontend/src/App.tsx` 四处修改:

1. 图标 import(与既有 `@iconify-icons/tabler/*` import 放一起;**注意包里没有 `route`,用 `route-2`**):

```ts
import routeIcon from "@iconify-icons/tabler/route-2";
```

2. `navItems` 数组,在 `/org` 项之后插入:

```ts
  {
    path: "/workflows",
    labelKey: "nav.workflows",
    icon: routeIcon,
  },
```

3. `pageBreadcrumbKeys` 追加:

```ts
  "/workflows": "nav.workflows",
```

4. 组件 import(`OrgChartPage` 所在的 `@/components/agentre` import 列表里加 `WorkflowsPage`)+ 路由(`/org` Route 之后):

```tsx
<Route path="/workflows" element={<WorkflowsPage />} />
```

i18n 两份 locale。`frontend/src/i18n/locales/zh-CN/common.json` 的 `nav` 对象加:

```json
"workflows": "流程"
```

顶层(与 `org` 同级)加 `workflows` 段:

```json
"workflows": {
  "title": "流程库",
  "subtitle": "跨群复用的协作流程(SOP);主持人每轮注入绑定流程的最新内容",
  "new": "新建流程",
  "empty": "还没有流程,点击「新建流程」开始"
}
```

`frontend/src/i18n/locales/en/common.json` 的 `nav` 加:

```json
"workflows": "Workflows"
```

顶层加:

```json
"workflows": {
  "title": "Workflows",
  "subtitle": "Reusable collaboration SOPs; the host gets the latest bound workflow every round",
  "new": "New workflow",
  "empty": "No workflows yet — click \"New workflow\" to start"
}
```

- [x] **Step 4: 跑测试确认通过(绿)+ 回归 App/i18n 测试**

```bash
cd frontend && pnpm test -- src/components/agentre/workflows/workflows-page.test.tsx src/__tests__/App.test.tsx src/__tests__/i18n.test.ts
```

预期:全 PASS(App.test 按钮断言是 by-name 的,新增 nav 项不影响;i18n.test 校验两 locale key 集合一致)。

- [x] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/workflows/ frontend/src/components/agentre/index.ts frontend/src/App.tsx frontend/src/i18n/
git commit -m "✨ workflow: 流程库 nav 入口 + /workflows 路由 + 页面骨架"
```

---

### Task 5: use-workflows hook + 全局 wailsApp mock 补导出

**Files:**
- Create: `frontend/src/hooks/use-workflows.ts`
- Modify: `frontend/src/__tests__/mocks/wailsApp.ts`
- Test: `frontend/src/hooks/use-workflows.test.ts`

> ⚠️ `wailsApp.ts` 必须补 `Workflow*` 命名导出:vitest 全局 alias 把 wailsjs import 重定向到这个 mock 文件,任何(不带 per-file vi.mock 的)测试只要 import 链碰到 use-workflows,缺导出就直接抛 "does not provide an export named 'WorkflowList'"。

- [x] **Step 1: 写失败的测试**

新建 `frontend/src/hooks/use-workflows.test.ts`:

```ts
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const workflowList = vi.fn();
const workflowCreate = vi.fn();
const workflowUpdate = vi.fn();
const workflowDelete = vi.fn();

vi.mock("../../wailsjs/go/app/App", () => ({
  WorkflowList: (...a: unknown[]) => workflowList(...a),
  WorkflowCreate: (...a: unknown[]) => workflowCreate(...a),
  WorkflowUpdate: (...a: unknown[]) => workflowUpdate(...a),
  WorkflowDelete: (...a: unknown[]) => workflowDelete(...a),
}));

import { useWorkflows } from "./use-workflows";

describe("useWorkflows", () => {
  beforeEach(() => {
    workflowList.mockReset().mockResolvedValue({
      items: [
        {
          id: 1,
          name: "产品开发流程",
          content: "# A",
          groupCount: 2,
          createtime: 1,
          updatetime: 2,
        },
      ],
    });
    workflowCreate.mockReset().mockResolvedValue({ item: { id: 9 } });
    workflowUpdate.mockReset().mockResolvedValue({ item: { id: 1 } });
    workflowDelete.mockReset().mockResolvedValue({});
  });

  it("挂载即加载列表", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(result.current.workflows).toHaveLength(1));
    expect(result.current.workflows[0].name).toBe("产品开发流程");
    expect(result.current.workflows[0].groupCount).toBe(2);
  });

  it("create/update/remove 调绑定后重新加载", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(result.current.workflows).toHaveLength(1));
    await act(async () => {
      await result.current.create("新流程", "# 新");
    });
    expect(workflowCreate).toHaveBeenCalledWith({ name: "新流程", content: "# 新" });
    await act(async () => {
      await result.current.update(1, "改名", "# 改");
    });
    expect(workflowUpdate).toHaveBeenCalledWith({ id: 1, name: "改名", content: "# 改" });
    await act(async () => {
      await result.current.remove(1);
    });
    expect(workflowDelete).toHaveBeenCalledWith({ id: 1 });
    // 初始 1 次 + 三个写操作后各 reload 1 次
    expect(workflowList).toHaveBeenCalledTimes(4);
  });

  it("加载失败落 error", async () => {
    workflowList.mockRejectedValueOnce(new Error("boom"));
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(result.current.error).toBe("boom"));
    expect(result.current.workflows).toHaveLength(0);
  });
});
```

- [x] **Step 2: 跑测试确认失败(红)**

```bash
cd frontend && pnpm test -- src/hooks/use-workflows.test.ts
```

预期:FAIL,`./use-workflows` 模块不存在。

- [x] **Step 3: 写实现**

新建 `frontend/src/hooks/use-workflows.ts`(模式照抄 `use-project-list.ts`):

```ts
import { useCallback, useEffect, useState } from "react";

import {
  WorkflowCreate,
  WorkflowDelete,
  WorkflowList,
  WorkflowUpdate,
} from "../../wailsjs/go/app/App";

// 平铺投影:页面/弹窗只需要这些字段,避免直接耦合 wails models 类。
export type WorkflowItem = {
  id: number;
  name: string;
  content: string;
  groupCount: number;
  createtime: number;
  updatetime: number;
};

export function useWorkflows() {
  const [workflows, setWorkflows] = useState<WorkflowItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const resp = await WorkflowList();
      setWorkflows(
        (resp?.items ?? []).map((i) => ({
          id: i.id,
          name: i.name,
          content: i.content,
          groupCount: i.groupCount,
          createtime: i.createtime,
          updatetime: i.updatetime,
        })),
      );
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const create = useCallback(
    async (name: string, content: string) => {
      await WorkflowCreate({ name, content });
      await reload();
    },
    [reload],
  );

  const update = useCallback(
    async (id: number, name: string, content: string) => {
      await WorkflowUpdate({ id, name, content });
      await reload();
    },
    [reload],
  );

  const remove = useCallback(
    async (id: number) => {
      await WorkflowDelete({ id });
      await reload();
    },
    [reload],
  );

  return { workflows, loading, error, reload, create, update, remove };
}
```

(若 `WorkflowCreate({ name, content })` 的对象字面量与生成的请求类类型不兼容报 TS 错,参照 `group-new-dialog.tsx` 调 `GroupCreate` 的写法——生成类型是结构化兼容的,直接传字面量即可;真报错时用 `new workflow_svc.CreateWorkflowRequest(...)`,但优先字面量。)

`frontend/src/__tests__/mocks/wailsApp.ts` 末尾追加(沿用文件里的 `windowBackedMock` 模式):

```ts
export const WorkflowList = windowBackedMock("WorkflowList", () =>
  Promise.resolve({ items: [] }),
);
export const WorkflowCreate = windowBackedMock("WorkflowCreate", () =>
  Promise.resolve({ item: null }),
);
export const WorkflowUpdate = windowBackedMock("WorkflowUpdate", () =>
  Promise.resolve({ item: null }),
);
export const WorkflowDelete = windowBackedMock("WorkflowDelete", () =>
  Promise.resolve({}),
);
```

- [x] **Step 4: 跑测试确认通过(绿)**

```bash
cd frontend && pnpm test -- src/hooks/use-workflows.test.ts
```

预期:PASS。

- [x] **Step 5: 提交**

```bash
git add frontend/src/hooks/use-workflows.ts frontend/src/hooks/use-workflows.test.ts frontend/src/__tests__/mocks/wailsApp.ts
git commit -m "✨ workflow: use-workflows hook(列表加载 + CRUD 封装)"
```

---

### Task 6: 流程库页:列表 + 正文预览面板

**Files:**
- Modify: `frontend/src/components/agentre/workflows/workflows-page.tsx`(骨架替换为完整实现)
- Test: `frontend/src/components/agentre/workflows/workflows-page.test.tsx`

- [x] **Step 1: 写失败的测试**

`workflows-page.test.tsx` 整体替换为:

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const workflowList = vi.fn();
const workflowCreate = vi.fn();
const workflowUpdate = vi.fn();
const workflowDelete = vi.fn();

vi.mock("../../../../wailsjs/go/app/App", () => ({
  WorkflowList: (...a: unknown[]) => workflowList(...a),
  WorkflowCreate: (...a: unknown[]) => workflowCreate(...a),
  WorkflowUpdate: (...a: unknown[]) => workflowUpdate(...a),
  WorkflowDelete: (...a: unknown[]) => workflowDelete(...a),
}));

import { WorkflowsPage } from "./workflows-page";

const items = [
  {
    id: 1,
    name: "产品开发流程",
    content: "# 产品开发流程\n\n适用:新功能完整交付。\n\n## 角色",
    groupCount: 2,
    createtime: 1700000000000,
    updatetime: 1700000000000,
  },
  {
    id: 2,
    name: "紧急修复流程",
    content: "",
    groupCount: 0,
    createtime: 1700000000000,
    updatetime: 1700000000000,
  },
];

describe("WorkflowsPage", () => {
  beforeEach(() => {
    workflowList.mockReset().mockResolvedValue({ items });
    workflowCreate.mockReset().mockResolvedValue({ item: { id: 9 } });
    workflowUpdate.mockReset().mockResolvedValue({ item: { id: 1 } });
    workflowDelete.mockReset().mockResolvedValue({});
  });

  it("列表行:名称 + 摘要首行 + 使用中群数", async () => {
    render(<WorkflowsPage />);
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    // 摘要首行跳过 markdown 标题与空行
    expect(screen.getByText("适用:新功能完整交付。")).toBeTruthy();
    // 使用中群数 badge(en:Used by 2 groups);0 个群不显示 badge
    expect(screen.getByText("Used by 2 groups")).toBeTruthy();
    expect(screen.queryByText("Used by 0 groups")).toBeNull();
  });

  it("点列表行 → 右侧预览正文 + 「修改即时生效」标注", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowsPage />);
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    // 未选中时显示预览空态
    expect(screen.getByText("Select a workflow to preview")).toBeTruthy();
    await user.click(screen.getByText("产品开发流程"));
    // 预览面板:markdown 正文渲染 + live hint + 底部编辑按钮
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { level: 1, name: "产品开发流程" }),
      ).toBeTruthy(),
    );
    expect(
      screen.getByText(
        "Changes take effect immediately: running groups get the latest content next round",
      ),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Edit workflow" })).toBeTruthy();
  });

  it("空列表显示空态", async () => {
    workflowList.mockResolvedValue({ items: [] });
    render(<WorkflowsPage />);
    await waitFor(() =>
      expect(
        screen.getByText('No workflows yet — click "New workflow" to start'),
      ).toBeTruthy(),
    );
  });
});
```

- [x] **Step 2: 跑测试确认失败(红)**

```bash
cd frontend && pnpm test -- src/components/agentre/workflows/workflows-page.test.tsx
```

预期:FAIL(骨架页没有列表/预览)。

- [x] **Step 3: 写实现**

`workflows-page.tsx` 整体替换为(编辑/删除按钮的 onClick 本任务先留空实现 `() => {}`,Task 7/8 接线;`firstSummaryLine` 导出供测试直接复用):

```tsx
import * as React from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { useWorkflows } from "@/hooks/use-workflows";
import { cn } from "@/lib/utils";

import { MarkdownText } from "../markdown-text";

// 摘要首行:跳过空行与 markdown 标题行,取第一行正文(列表行副标题用)。
export function firstSummaryLine(content: string): string {
  for (const raw of content.split("\n")) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    return line;
  }
  return "";
}

export function WorkflowsPage() {
  const { t } = useTranslation();
  const { workflows, loading, error } = useWorkflows();
  const [selectedID, setSelectedID] = React.useState(0);

  const selected = workflows.find((w) => w.id === selectedID) ?? null;

  return (
    <div className="flex h-full min-h-0">
      <aside className="flex w-[340px] shrink-0 flex-col border-r border-border">
        <header className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
          <div className="flex min-w-0 flex-col gap-0.5">
            <h1 className="text-sm font-semibold text-foreground">
              {t("workflows.title")}
            </h1>
            <p className="truncate text-2xs text-muted-foreground">
              {t("workflows.subtitle")}
            </p>
          </div>
          <Button type="button" size="sm" onClick={() => {}}>
            <Plus className="size-3.5" aria-hidden="true" />
            {t("workflows.new")}
          </Button>
        </header>
        <div className="flex-1 overflow-y-auto">
          {error ? (
            <div className="px-4 py-2 text-2xs text-destructive">{error}</div>
          ) : null}
          {workflows.length === 0 && !loading && !error ? (
            <div className="px-4 py-8 text-center text-xs text-muted-foreground">
              {t("workflows.empty")}
            </div>
          ) : null}
          {workflows.map((w) => {
            const summary = firstSummaryLine(w.content);
            return (
              <div
                key={w.id}
                role="button"
                tabIndex={0}
                onClick={() => setSelectedID(w.id)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") setSelectedID(w.id);
                }}
                className={cn(
                  "group flex w-full cursor-pointer flex-col gap-1 border-b border-border px-4 py-3 text-left hover:bg-accent/50",
                  selectedID === w.id && "bg-accent",
                )}
              >
                <div className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-xs font-medium text-foreground">
                    {w.name}
                  </span>
                  {w.groupCount > 0 ? (
                    <span className="shrink-0 rounded-full bg-accent px-1.5 py-0.5 text-2xs text-muted-foreground">
                      {t("workflows.groupCount", { n: w.groupCount })}
                    </span>
                  ) : null}
                </div>
                {summary ? (
                  <p className="truncate text-2xs text-muted-foreground">
                    {summary}
                  </p>
                ) : null}
                <div className="flex items-center justify-between">
                  <span className="text-2xs text-muted-foreground">
                    {t("workflows.updatedAt", {
                      time: new Date(w.updatetime).toLocaleString(),
                    })}
                  </span>
                  <span className="flex gap-1 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-xs"
                      aria-label={t("workflows.edit")}
                      onClick={(e) => {
                        e.stopPropagation();
                      }}
                    >
                      <Pencil className="size-3" aria-hidden="true" />
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-xs"
                      aria-label={t("workflows.delete")}
                      onClick={(e) => {
                        e.stopPropagation();
                      }}
                    >
                      <Trash2 className="size-3" aria-hidden="true" />
                    </Button>
                  </span>
                </div>
              </div>
            );
          })}
        </div>
      </aside>
      <section className="flex min-w-0 flex-1 flex-col">
        {selected ? (
          <>
            <header className="flex flex-col gap-0.5 border-b border-border px-5 py-3">
              <h2 className="text-sm font-semibold text-foreground">
                {selected.name}
              </h2>
              <p className="text-2xs text-muted-foreground">
                {t("workflows.preview.liveHint")}
              </p>
            </header>
            <div className="flex-1 overflow-y-auto px-5 py-4">
              <MarkdownText text={selected.content} />
            </div>
            <footer className="border-t border-border px-5 py-3">
              <Button type="button" size="sm" onClick={() => {}}>
                {t("workflows.edit")}
              </Button>
            </footer>
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center text-xs text-muted-foreground">
            {t("workflows.preview.empty")}
          </div>
        )}
      </section>
    </div>
  );
}
```

i18n key 两 locale 的 `workflows` 段内追加。zh-CN:

```json
"groupCount": "{{n}} 个群使用中",
"updatedAt": "更新于 {{time}}",
"edit": "编辑流程",
"delete": "删除",
"preview": {
  "liveHint": "修改即时生效:进行中的群下一轮即注入最新正文",
  "empty": "选择左侧流程查看正文"
}
```

en:

```json
"groupCount": "Used by {{n}} groups",
"updatedAt": "Updated {{time}}",
"edit": "Edit workflow",
"delete": "Delete",
"preview": {
  "liveHint": "Changes take effect immediately: running groups get the latest content next round",
  "empty": "Select a workflow to preview"
}
```

- [x] **Step 4: 跑测试确认通过(绿)**

```bash
cd frontend && pnpm test -- src/components/agentre/workflows/workflows-page.test.tsx src/__tests__/i18n.test.ts
```

预期:PASS。

- [x] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/workflows/ frontend/src/i18n/
git commit -m "✨ workflow: 流程库页列表 + 正文预览面板(摘要首行/群数 badge/即时生效标注)"
```

---

### Task 7: 编辑弹窗(新建/编辑 + 插入骨架模板)

**Files:**
- Create: `frontend/src/components/agentre/workflows/workflow-edit-dialog.tsx`
- Modify: `frontend/src/components/agentre/workflows/workflows-page.tsx`(接线三个入口:头部新建/行内铅笔/预览底部编辑)
- Test: `frontend/src/components/agentre/workflows/workflow-edit-dialog.test.tsx`
- Test: `frontend/src/components/agentre/workflows/workflows-page.test.tsx`(追加集成用例)

- [x] **Step 1: 写失败的测试**

新建 `frontend/src/components/agentre/workflows/workflow-edit-dialog.test.tsx`:

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { WorkflowEditDialog } from "./workflow-edit-dialog";

describe("WorkflowEditDialog", () => {
  it("新建模式:填名称正文 → 保存回调收到 trim 后的值", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(
      <WorkflowEditDialog
        open
        onOpenChange={() => {}}
        editing={null}
        onSubmit={onSubmit}
      />,
    );
    expect(screen.getByText("New workflow")).toBeTruthy();
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "  产品开发流程 " },
    });
    fireEvent.change(
      screen.getByRole("textbox", { name: "Workflow content (Markdown)" }),
      { target: { value: "# 产品开发流程" } },
    );
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith("产品开发流程", "# 产品开发流程"),
    );
  });

  it("名称为空时保存禁用", () => {
    render(
      <WorkflowEditDialog
        open
        onOpenChange={() => {}}
        editing={null}
        onSubmit={vi.fn()}
      />,
    );
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("编辑模式:表单预填 + 标题为编辑", () => {
    render(
      <WorkflowEditDialog
        open
        onOpenChange={() => {}}
        editing={{
          id: 3,
          name: "旧名",
          content: "## 旧正文",
          groupCount: 1,
          createtime: 1,
          updatetime: 2,
        }}
        onSubmit={vi.fn()}
      />,
    );
    expect(screen.getByText("Edit workflow")).toBeTruthy();
    expect(
      (screen.getByRole("textbox", { name: "Name" }) as HTMLInputElement).value,
    ).toBe("旧名");
    expect(
      (
        screen.getByRole("textbox", {
          name: "Workflow content (Markdown)",
        }) as HTMLTextAreaElement
      ).value,
    ).toBe("## 旧正文");
  });

  it("插入骨架模板:空正文直接填入,非空追加到末尾", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(
      <WorkflowEditDialog
        open
        onOpenChange={() => {}}
        editing={null}
        onSubmit={vi.fn()}
      />,
    );
    const textarea = screen.getByRole("textbox", {
      name: "Workflow content (Markdown)",
    }) as HTMLTextAreaElement;
    await user.click(screen.getByRole("button", { name: "Insert template" }));
    // 骨架四段(spec §6.3):适用/角色/步骤/纪律
    expect(textarea.value).toContain("## Roles");
    expect(textarea.value).toContain("## Steps");
    expect(textarea.value).toContain("## Discipline");
    const first = textarea.value;
    await user.click(screen.getByRole("button", { name: "Insert template" }));
    expect(textarea.value.length).toBeGreaterThan(first.length);
    expect(textarea.value.startsWith(first)).toBe(true);
  });
});
```

`workflows-page.test.tsx` 末尾追加集成用例:

```tsx
  it("新建按钮开弹窗 → 保存调 WorkflowCreate 并 reload", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowsPage />);
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByRole("button", { name: "New workflow" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "评审流程" },
    });
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(workflowCreate).toHaveBeenCalledWith({
        name: "评审流程",
        content: "",
      }),
    );
    // 写后重载列表
    expect(workflowList.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("行内铅笔开编辑弹窗 → 保存调 WorkflowUpdate", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowsPage />);
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(
      screen.getAllByRole("button", { name: "Edit workflow" })[0],
    );
    const nameInput = screen.getByRole("textbox", {
      name: "Name",
    }) as HTMLInputElement;
    expect(nameInput.value).toBe("产品开发流程");
    fireEvent.change(nameInput, { target: { value: "产品开发流程 v2" } });
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(workflowUpdate).toHaveBeenCalledWith(
        expect.objectContaining({ id: 1, name: "产品开发流程 v2" }),
      ),
    );
  });
```

(注意:`fireEvent` 已在该文件 import;若没有,补 `import { fireEvent } from "@testing-library/react"`。行内铅笔与预览底部按钮 aria-label/文案同为 `workflows.edit`,用 `getAllByRole(...)[0]` 取列表行那颗。)

- [x] **Step 2: 跑测试确认失败(红)**

```bash
cd frontend && pnpm test -- src/components/agentre/workflows/
```

预期:FAIL,`workflow-edit-dialog` 模块不存在 + 页面集成用例失败。

- [x] **Step 3: 写实现**

新建 `frontend/src/components/agentre/workflows/workflow-edit-dialog.tsx`:

```tsx
import * as React from "react";
import { Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import type { WorkflowItem } from "@/hooks/use-workflows";

import { AgentreDialog } from "../app-dialog";

export type WorkflowEditDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** null = 新建;非 null = 编辑该流程。 */
  editing: WorkflowItem | null;
  onSubmit: (name: string, content: string) => Promise<void>;
};

function WorkflowEditDialog({
  open,
  onOpenChange,
  editing,
  onSubmit,
}: WorkflowEditDialogProps) {
  const { t } = useTranslation();
  const [name, setName] = React.useState("");
  const [content, setContent] = React.useState("");
  const [submitting, setSubmitting] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  // 每次打开按 editing 重置表单,避免上次输入残留。
  React.useEffect(() => {
    if (open) {
      setName(editing?.name ?? "");
      setContent(editing?.content ?? "");
      setError(null);
    }
  }, [open, editing]);

  // 骨架模板(spec §6.3 四段:适用/角色/步骤/纪律);正文非空时追加到末尾不覆盖。
  const insertTemplate = () => {
    const tpl = t("workflows.editor.template");
    setContent((prev) => (prev.trim() ? `${prev.trimEnd()}\n\n${tpl}` : tpl));
  };

  const canSubmit = name.trim().length > 0 && !submitting;

  const submit = async () => {
    setError(null);
    setSubmitting(true);
    try {
      await onSubmit(name.trim(), content);
      onOpenChange(false);
    } catch (err) {
      setError(String(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <AgentreDialog
      open={open}
      onOpenChange={onOpenChange}
      title={
        editing
          ? t("workflows.editor.editTitle")
          : t("workflows.editor.createTitle")
      }
      contentClassName="max-w-[640px]"
      footer={
        <>
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={submitting}
          >
            {t("common.cancel")}
          </Button>
          <Button
            type="button"
            disabled={!canSubmit}
            onClick={() => void submit()}
          >
            {submitting ? (
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
            ) : null}
            {t("workflows.editor.save")}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3.5">
        <label className="flex flex-col gap-1.5 text-xs">
          <span className="font-medium text-foreground">
            {t("workflows.editor.name")}
            <span className="ml-0.5 text-destructive">*</span>
          </span>
          <Input
            aria-label={t("workflows.editor.name")}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t("workflows.editor.namePlaceholder")}
            className="h-9 text-xs"
          />
        </label>
        <label className="flex flex-col gap-1.5 text-xs">
          <span className="flex items-center justify-between font-medium text-foreground">
            {t("workflows.editor.content")}
            <Button
              type="button"
              variant="link"
              size="sm"
              className="h-auto p-0 text-2xs"
              onClick={insertTemplate}
            >
              {t("workflows.editor.insertTemplate")}
            </Button>
          </span>
          <Textarea
            aria-label={t("workflows.editor.content")}
            value={content}
            onChange={(e) => setContent(e.target.value)}
            rows={16}
            className="font-mono text-xs"
          />
        </label>
        {error ? (
          <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
            {error}
          </div>
        ) : null}
      </div>
    </AgentreDialog>
  );
}

export { WorkflowEditDialog };
```

`workflows-page.tsx` 接线:

1. import 增加:

```tsx
import { type WorkflowItem } from "@/hooks/use-workflows";
import { WorkflowEditDialog } from "./workflow-edit-dialog";
```

(`useWorkflows` 解构补上 `create, update`。)

2. 组件内加状态与提交回调:

```tsx
const [editorOpen, setEditorOpen] = React.useState(false);
const [editing, setEditing] = React.useState<WorkflowItem | null>(null);

const openCreate = () => {
  setEditing(null);
  setEditorOpen(true);
};
const openEdit = (w: WorkflowItem) => {
  setEditing(w);
  setEditorOpen(true);
};
const handleSubmit = async (name: string, content: string) => {
  if (editing) {
    await update(editing.id, name, content);
  } else {
    await create(name, content);
  }
};
```

3. 三个空 onClick 接线:头部新建按钮 `onClick={openCreate}`;行内铅笔 `onClick={(e) => { e.stopPropagation(); openEdit(w); }}`;预览底部编辑按钮 `onClick={() => openEdit(selected)}`。

4. 根 `<div>` 闭合前挂弹窗:

```tsx
<WorkflowEditDialog
  open={editorOpen}
  onOpenChange={setEditorOpen}
  editing={editing}
  onSubmit={handleSubmit}
/>
```

i18n `workflows` 段内追加 `editor` 子段。zh-CN(模板即 spec §6.3 骨架,角色用抽象角色不写真名):

```json
"editor": {
  "createTitle": "新建流程",
  "editTitle": "编辑流程",
  "name": "名称",
  "namePlaceholder": "如:产品开发流程",
  "content": "流程正文(Markdown)",
  "insertTemplate": "插入骨架模板",
  "save": "保存",
  "template": "# 流程名称\n\n适用:什么场景用这个流程。\n\n## 角色\n- 角色 A:职责与产出\n- 角色 B:职责与产出\n(用抽象角色,不写 agent 真名;按群成员职责对号入座,缺角色先 group_invite 招募或询问用户)\n\n## 步骤\n1. 角色 A:输入 → 产出,交付 .agentre/handoff/<群>/task-N-xxx.md,含验收标准\n2. 角色 B:依据上一棒交付物继续\n3. 并行验证:可并行的环节明确标注(验证任务回指被验证任务)\n4. 全部通过 → 汇总 @用户;有问题 → 打回对应环节(新任务卡)\n\n## 纪律\n- 每一步都用任务卡交接,交付物写明路径\n- 可能改同一片代码的任务不要并行\n- 验收标准在派活时写进 brief"
}
```

en:

```json
"editor": {
  "createTitle": "New workflow",
  "editTitle": "Edit workflow",
  "name": "Name",
  "namePlaceholder": "e.g. Product development workflow",
  "content": "Workflow content (Markdown)",
  "insertTemplate": "Insert template",
  "save": "Save",
  "template": "# Workflow name\n\nApplies to: when to use this workflow.\n\n## Roles\n- Role A: responsibility and deliverable\n- Role B: responsibility and deliverable\n(Use abstract roles, not agent names; map to roster members by duty, recruit via group_invite or ask the user when a role is missing)\n\n## Steps\n1. Role A: input → output, deliver .agentre/handoff/<group>/task-N-xxx.md with acceptance criteria\n2. Role B: continue from the previous deliverable\n3. Parallel verification: mark parallelizable steps explicitly (verification tasks reference the verified task)\n4. All pass → summarize @user; issues → send back to the owning step (new task card)\n\n## Discipline\n- Hand over every step with a task card; write deliverable paths explicitly\n- Never parallelize tasks that may touch the same code\n- Put acceptance criteria into the brief when assigning"
}
```

(注:弹窗标题键 `workflows.editor.createTitle` 与列表头按钮 `workflows.new` 的 en 文案同为 "New workflow"——测试里弹窗标题用 `getByText`、按钮用 `getByRole("button")` 区分,不冲突。)

- [x] **Step 4: 跑测试确认通过(绿)**

```bash
cd frontend && pnpm test -- src/components/agentre/workflows/ src/__tests__/i18n.test.ts
```

预期:PASS。

- [x] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/workflows/ frontend/src/i18n/
git commit -m "✨ workflow: 新建/编辑弹窗(Markdown 正文 + 插入骨架模板)"
```

---

### Task 8: 删除确认(提示使用中群数)

**Files:**
- Create: `frontend/src/components/agentre/workflows/workflow-delete-dialog.tsx`
- Modify: `frontend/src/components/agentre/workflows/workflows-page.tsx`
- Test: `frontend/src/components/agentre/workflows/workflows-page.test.tsx`(追加用例)

- [x] **Step 1: 写失败的测试**

`workflows-page.test.tsx` 末尾追加:

```tsx
  it("删除:确认弹窗提示使用中群数 → 确认调 WorkflowDelete", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowsPage />);
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getAllByRole("button", { name: "Delete" })[0]);
    // 使用中群数警示(items[0].groupCount = 2)
    expect(
      screen.getByText(
        '"产品开发流程" is used by 2 groups; after deletion they fall back to "no workflow". This cannot be undone.',
      ),
    ).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Delete workflow" }));
    await waitFor(() =>
      expect(workflowDelete).toHaveBeenCalledWith({ id: 1 }),
    );
  });

  it("删除选中流程后预览回空态", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<WorkflowsPage />);
    await waitFor(() => expect(screen.getByText("产品开发流程")).toBeTruthy());
    await user.click(screen.getByText("产品开发流程"));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Edit workflow" })).toBeTruthy(),
    );
    // 删除后列表只剩 id=2
    workflowList.mockResolvedValue({ items: [items[1]] });
    await user.click(screen.getAllByRole("button", { name: "Delete" })[0]);
    await user.click(screen.getByRole("button", { name: "Delete workflow" }));
    await waitFor(() =>
      expect(screen.getByText("Select a workflow to preview")).toBeTruthy(),
    );
  });
```

- [x] **Step 2: 跑测试确认失败(红)**

```bash
cd frontend && pnpm test -- src/components/agentre/workflows/workflows-page.test.tsx
```

预期:FAIL(还没有删除弹窗)。

- [x] **Step 3: 写实现**

新建 `frontend/src/components/agentre/workflows/workflow-delete-dialog.tsx`(模式照抄 `group-delete-dialog.tsx`):

```tsx
import * as React from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import type { WorkflowItem } from "@/hooks/use-workflows";

import { AgentreDialog } from "../app-dialog";

export type WorkflowDeleteDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  workflow: WorkflowItem | null;
  onConfirm: () => void;
};

// 删除确认:groupCount > 0 时强提示使用中群数(spec §8:删除前确认并提示使用中的群数)。
function WorkflowDeleteDialog({
  open,
  onOpenChange,
  workflow,
  onConfirm,
}: WorkflowDeleteDialogProps) {
  const { t } = useTranslation();
  if (!workflow) return null;
  return (
    <AgentreDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("workflows.deleteConfirm.title")}
      description={
        workflow.groupCount > 0
          ? t("workflows.deleteConfirm.desc", {
              name: workflow.name,
              n: workflow.groupCount,
            })
          : t("workflows.deleteConfirm.descUnused", { name: workflow.name })
      }
      footer={
        <>
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
          >
            {t("common.cancel")}
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={() => {
              onConfirm();
              onOpenChange(false);
            }}
          >
            {t("workflows.deleteConfirm.confirm")}
          </Button>
        </>
      }
    />
  );
}

export { WorkflowDeleteDialog };
```

`workflows-page.tsx` 接线:

1. import 增加 `import { WorkflowDeleteDialog } from "./workflow-delete-dialog";`,`useWorkflows` 解构补 `remove`。
2. 状态 + 回调:

```tsx
const [deleting, setDeleting] = React.useState<WorkflowItem | null>(null);

const confirmDelete = () => {
  if (!deleting) return;
  const id = deleting.id;
  if (selectedID === id) setSelectedID(0);
  void remove(id);
};
```

3. 行内垃圾桶按钮接线:`onClick={(e) => { e.stopPropagation(); setDeleting(w); }}`。
4. 根 `<div>` 闭合前挂弹窗:

```tsx
<WorkflowDeleteDialog
  open={deleting !== null}
  onOpenChange={(o) => {
    if (!o) setDeleting(null);
  }}
  workflow={deleting}
  onConfirm={confirmDelete}
/>
```

i18n `workflows` 段内追加 `deleteConfirm` 子段。zh-CN:

```json
"deleteConfirm": {
  "title": "删除流程",
  "desc": "「{{name}}」正被 {{n}} 个群使用;删除后这些群按「不绑定流程」处理,且不可恢复。",
  "descUnused": "删除「{{name}}」后不可恢复。",
  "confirm": "删除流程"
}
```

en:

```json
"deleteConfirm": {
  "title": "Delete workflow",
  "desc": "\"{{name}}\" is used by {{n}} groups; after deletion they fall back to \"no workflow\". This cannot be undone.",
  "descUnused": "Deleting \"{{name}}\" cannot be undone.",
  "confirm": "Delete workflow"
}
```

(i18next 默认对插值做 HTML 转义,`getByText` 比对的是转义后的 DOM 文本——引号/分号为 ASCII,无转义问题;若断言因标点不匹配失败,以 locale 文件实际渲染为准修断言而不是改文案。)

- [x] **Step 4: 跑测试确认通过(绿)**

```bash
cd frontend && pnpm test -- src/components/agentre/workflows/ src/__tests__/i18n.test.ts
```

预期:PASS。

- [x] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/workflows/ frontend/src/i18n/
git commit -m "✨ workflow: 删除确认弹窗(提示使用中群数,删后群按不绑定处理)"
```

---

### Task 9: 建群弹窗「协作流程」下拉

**Files:**
- Modify: `frontend/src/components/agentre/group-chat/group-new-dialog.tsx`
- Test: `frontend/src/components/agentre/group-chat/group-new-dialog.test.tsx`

- [x] **Step 1: 写失败的测试**

`group-new-dialog.test.tsx` 三处修改:

1. mock 工厂补 `WorkflowList`(**必须**——组件 import 链新增该命名导出,工厂缺了会直接抛错):

```tsx
const workflowList = vi.fn();

vi.mock("../../../../wailsjs/go/app/App", () => ({
  GroupCreate: (...a: unknown[]) => groupCreate(...a),
  WorkflowList: (...a: unknown[]) => workflowList(...a),
}));
```

2. `beforeEach` 里加:

```tsx
workflowList
  .mockReset()
  .mockResolvedValue({ items: [{ id: 4, name: "产品开发流程" }] });
```

3. 末尾追加用例:

```tsx
  it("选协作流程 → GroupCreate 带 workflowID", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<GroupNewDialog open onOpenChange={() => {}} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Group title" }), {
      target: { value: "支付小队" },
    });
    await user.click(screen.getByRole("combobox", { name: "Host" }));
    await user.click(screen.getByRole("option", { name: "云溪" }));
    await user.click(screen.getByRole("combobox", { name: "Workflow" }));
    await user.click(
      await screen.findByRole("option", { name: "产品开发流程" }),
    );
    await user.click(screen.getByRole("button", { name: "Create group" }));
    await waitFor(() => expect(groupCreate).toHaveBeenCalled());
    expect(groupCreate.mock.calls[0][0]).toMatchObject({ workflowID: 4 });
  });
```

(既有「提交调 GroupCreate」用例断言 `workflowID: 0` 不动——未选流程时默认不绑定,正好回归这条语义。)

- [x] **Step 2: 跑测试确认失败(红)**

```bash
cd frontend && pnpm test -- src/components/agentre/group-chat/group-new-dialog.test.tsx
```

预期:新用例 FAIL(找不到 "Workflow" combobox)。

- [x] **Step 3: 写实现**

`group-new-dialog.tsx` 五处修改:

1. wails import 行扩为:

```tsx
import { GroupCreate, WorkflowList } from "../../../../wailsjs/go/app/App";
```

2. 状态(放在 `memberIDs` 声明之后):

```tsx
const [workflowID, setWorkflowID] = React.useState(0);
const [workflowOptions, setWorkflowOptions] = React.useState<
  { id: number; name: string }[]
>([]);
```

3. 打开重置 effect 里补重置 + 拉取(弹窗打开才拉,失败静默为不显示选项——流程是可选绑定,不挡建群):

```tsx
React.useEffect(() => {
  if (open) {
    setTitle("");
    setHostID(0);
    setProjectID(projectContext?.projectID ?? 0);
    setMemberIDs([]);
    setWorkflowID(0);
    setError(null);
    WorkflowList()
      .then((resp) =>
        setWorkflowOptions(
          (resp?.items ?? []).map((i: { id: number; name: string }) => ({
            id: i.id,
            name: i.name,
          })),
        ),
      )
      .catch(() => setWorkflowOptions([]));
  }
}, [open, projectContext]);
```

4. submit 的 `GroupCreate` 载荷把 `workflowID: 0,` 改为 `workflowID,`。

5. JSX:项目 Select 的 `</label>` 之后、成员 picker `<div>` 之前插入:

```tsx
<label className="flex flex-col gap-1.5 text-xs">
  <span className="font-medium text-foreground">
    {t("group.new.workflow")}
  </span>
  <Select
    value={String(workflowID)}
    onValueChange={(v) => setWorkflowID(Number(v))}
  >
    <SelectTrigger
      aria-label={t("group.new.workflow")}
      className="h-9 text-xs"
    >
      <SelectValue placeholder={t("group.new.workflowNone")} />
    </SelectTrigger>
    <SelectContent>
      <SelectItem value="0">{t("group.new.workflowNone")}</SelectItem>
      {workflowOptions.map((w) => (
        <SelectItem key={w.id} value={String(w.id)}>
          {w.name}
        </SelectItem>
      ))}
    </SelectContent>
  </Select>
  <span className="text-2xs text-muted-foreground">
    {t("group.new.workflowHint")}
  </span>
</label>
```

i18n `group.new` 段内追加。zh-CN:

```json
"workflow": "协作流程",
"workflowNone": "不绑定",
"workflowHint": "可选;主持人将按该流程的最新内容编排协作"
```

en:

```json
"workflow": "Workflow",
"workflowNone": "None",
"workflowHint": "Optional; the host orchestrates by the latest content of this workflow"
```

- [x] **Step 4: 跑测试确认通过(绿)**

```bash
cd frontend && pnpm test -- src/components/agentre/group-chat/group-new-dialog.test.tsx src/__tests__/i18n.test.ts
```

预期:PASS(含既有用例回归)。

- [x] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/group-chat/ frontend/src/i18n/
git commit -m "✨ group: 建群弹窗「协作流程」下拉(可选绑定 workflow)"
```

---

### Task 10: 全量验证 + 收尾

- [x] **Step 1: 后端全量 + 前端全量 + lint**

```bash
GOWORK=off make test-backend
cd frontend && pnpm test
cd .. && GOWORK=off make lint
```

预期:全 PASS / 0 issues。前端全量特别盯:`App.test.tsx`(nav 新增项)、任何 import 链碰到 `use-workflows` 的既有测试(wailsApp mock 已补导出,理论无恙;若有 per-file vi.mock 工厂因缺 `WorkflowList` 抛错,在该工厂里补一行 `WorkflowList: vi.fn(() => Promise.resolve({ items: [] }))`)。

- [x] **Step 2: 确认 diff 干净**

```bash
git diff develop/group --stat
```

预期:只有本计划文件结构表中列出的文件 + 本计划文件;无 drive-by 改动。

- [x] **Step 3: 勾选计划 + 提交**

把本计划文件中完成的 checkbox 勾上:

```bash
git add docs/superpowers/plans/2026-06-12-group-task-orchestration-pr3-workflows.md
git commit -m "📝 plan: 勾选 PR3 全部任务(实施完成)"
```

- [x] **Step 4: 真机验证提示(人工,不阻塞合并)**

`make dev` 起 app → 核对:nav「流程」入口 → 新建流程(插入骨架模板)→ 列表行(摘要首行/群数 badge/更新时间)→ 选中预览(markdown 渲染 + 即时生效标注)→ 编辑保存 → 建群弹窗选该流程建群 → 回到流程库看「使用中群数」变 1 → 删除确认提示群数 → 暗色模式。设计稿对照 `~/Desktop/agentry.pen` 帧「Workflows — 流程库 · Light」「⑤ 流程编辑弹窗」。

- [x] **Step 5: 若实现与 spec 有偏差,回写 spec**

参照 PR1 的做法(commit `9ead36e`),把实现偏差(如有)回写 `docs/superpowers/specs/2026-06-11-group-task-orchestration-design.md` §8,单独一条 `📝 spec:` commit。

---

## Spec 覆盖对照(自查)

| spec 要求 | 任务 |
| ---- | ---- |
| §8 流程库独立管理页,与组织页同级入口 | Task 4(nav `/workflows` 紧邻 `/org`) |
| §8 列表:名称 + 摘要首行 + 使用中群数 + 更新时间 + 编辑/删除 | Task 6(行结构)+ 7(编辑接线)+ 8(删除接线) |
| §8 右侧选中流程正文预览面板(底部「编辑流程」) | Task 6 + 7 |
| §8 预览头部标注「修改即时生效」(所见即注入) | Task 6(`workflows.preview.liveHint`) |
| §8/§6.3 新建/编辑弹窗,「插入骨架模板」降低首次书写门槛,模板文案进 i18n 两语言 | Task 7 |
| §6.3 骨架四段:适用/角色(抽象角色不写真名)/步骤(交付物落点+并行标注)/纪律 | Task 7 的 template 文案 |
| §8 删除前确认并提示使用中的群数 | Task 8 |
| §8 已删流程的群按「不绑定」处理(注入查不到即跳过) | PR1 已落地(`Find`+`IsActive` 门控);Task 2 的 Delete 注释钉死语义 |
| §3.2 建群下拉 UI 在 PR3(「协作流程」可选绑定) | Task 9 |
| §8 全部静态文案走 i18n 双语;控件用 shadcn `@/components/ui/*` | 各任务 i18n step + i18n.test/eslint-i18n.test 自动校验 |
| 使用中群数数据来源 | Task 1(svc List 聚合 active 群 `WorkflowID` 计数) |
| 不存在/已删流程的编辑删除防御 | Task 2(`WorkflowNotFound`) |

---

## 实施偏差记录(review 驱动,均已实现)

- Task 1/2:补充计划外的错误路径测试(List/Update/Delete 的 repo error 透传、Update 后 groupCounts 出错)。
- Task 6:列表行结构偏离计划原文——重构为「主体 button + 并排绝对定位操作按钮」(不嵌套交互元素,ARIA 合规,对齐 group-task-list 先例)+ `aria-current`;`groupCount` 插值改 `{{count}}` 并增 `groupCount_one`(两 locale 同步);时间用 `toLocaleDateString()`。
- Task 7:正文字段外层 `<label>` 改 `<div>`(「插入骨架模板」按钮不污染 textarea 可访问名),`aria-label` 直接落在 Textarea。
- Task 8:hook `remove` 改为捕获错误落 hook `error`(与 create/update 抛出语义有意不对称,注释已说明);删除弹窗拆分 `deleteOpen`/`deleting` 两状态保留 Radix 退出动画。
- Task 9:连带修 `chat-page.test.tsx` 的 wails mock 工厂(补 `GroupCreate`/`WorkflowList` stub,因其全量替换模块且渲染 GroupNewDialog)。
- 设计 spec(2026-06-11-group-task-orchestration-design.md)层面无偏差,无需回写。
