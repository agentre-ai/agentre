# 组织架构拖拽排序 + 拖拽手感优化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让组织架构的树形图与列表都能拖拽给**同级**排序,并修掉树拖拽「卡卡的」手感。

**Architecture:** 后端照抄既有 `project` 的 ReorderSiblings 范式(repo 事务密集重编号 `sort_order=1..N` → svc 全集校验 → App binding),为 `agent` / `department` 各加一套 Reorder。前端树用「插入位 drop-zone」新增同级排序、关掉拖拽时的 CSS 补间、落点乐观更新;列表在 hierarchy 模式下用 `useSortable` 行拖拽,按原始 placement 分桶持久化。

**Tech Stack:** Go 1.26 + cago + gorm/sqlmock/mockgen + goconvey;React 19 + TS + `@dnd-kit/core` + `@dnd-kit/sortable` + Vitest;Wails v2 bindings。

## Global Constraints

- **严格 TDD:先红后绿。** 每个 producer 改动都先写失败测试、跑一次看它按预期失败,再实现。
- **Repository 单测只用 `testutils.Database(t)` + sqlmock;service 单测用 mockgen 注入 repo mock,不连库。** (见 `internal/repository/agent_repo/agent_test.go`、`internal/service/agent_svc/agent_test.go`)
- **不改现有迁移**,`sort_order` 列已存在,本计划**无 DB migration**。
- **不改现有 re-parent 拖拽行为 / `Move` 的绝对 `NewSortOrder` 语义。**
- **排序只在同类型组内**(agent 之间、department 之间),**不做 agent↔department 混排**;列表拖拽只在 hierarchy 模式、不做跨父/re-parent。
- **新增可见前端文案必须走 i18n**,同时写 `frontend/src/i18n/locales/zh-CN/common.json` 与 `en/common.json`;`i18next/no-literal-string` 会拦硬编码中文。
- **共享分支 `develop/wyz` 有并发会话**:提交一律 `git commit <files>` 带 pathspec,别裸 `git commit`。
- **提交用 gitmoji**;后端跑 `make test-backend`,前端跑 `make lint` + 全量 `pnpm test`(看真 exit code,别 `| tail` 吞退出码)。
- **wailsjs 绑定是 gitignore 生成物**:`make generate` 只刷新本地文件,无需提交;前端任务开工前先 `make generate` 让 `ReorderAgents`/`ReorderDepartments` 可 import。

---

## Phase 1 — 后端:Agent 同级排序

### Task 1: `agent_repo.ReorderSiblings`

**Files:**
- Modify: `internal/repository/agent_repo/agent.go`(接口 L17-38 加一行;impl 追加函数;imports 加 `fmt`/`time`)
- Test: `internal/repository/agent_repo/agent_test.go`
- Regenerate: `internal/repository/agent_repo/mock_agent_repo/mock_agent.go`(`make mock`)

**Interfaces:**
- Produces: `ReorderSiblings(ctx context.Context, departmentID, parentAgentID int64, orderedIDs []int64) error` —— 在一个事务里把该同级组(`department_id=? AND parent_agent_id=?`)按 `orderedIDs` 顺序写 `sort_order=1..N`;任一 id 不属于该组时 `RowsAffected!=1` → 报错。

- [ ] **Step 1: 写失败测试**

在 `internal/repository/agent_repo/agent_test.go` 末尾追加:

```go
func TestAgentReorderSiblings(t *testing.T) {
	ctx, mock, repo := setupRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE agents SET sort_order = \\?, updatetime = \\? WHERE id = \\? AND department_id = \\? AND parent_agent_id = \\? AND status = \\?").
		WithArgs(1, sqlmock.AnyArg(), int64(3), int64(2), int64(0), consts.ACTIVE).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE agents SET sort_order = \\?, updatetime = \\? WHERE id = \\? AND department_id = \\? AND parent_agent_id = \\? AND status = \\?").
		WithArgs(2, sqlmock.AnyArg(), int64(1), int64(2), int64(0), consts.ACTIVE).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.ReorderSiblings(ctx, 2, 0, []int64{3, 1})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `go test ./internal/repository/agent_repo/ -run TestAgentReorderSiblings`
Expected: 编译失败 —— `repo.ReorderSiblings undefined`。

- [ ] **Step 3: 加接口方法**

`internal/repository/agent_repo/agent.go` 接口(L17-38)在 `UpdatePlacement` 那行下面加:

```go
	ReorderSiblings(ctx context.Context, departmentID, parentAgentID int64, orderedIDs []int64) error
```

- [ ] **Step 4: 加 imports + 实现**

`agent.go` 顶部 import 块补 `"fmt"` 和 `"time"`(与现有 `"context"`/`"errors"` 并列)。在 `UpdatePlacement` 函数后追加实现:

```go
func (r *agentRepo) ReorderSiblings(ctx context.Context, departmentID, parentAgentID int64, orderedIDs []int64) error {
	now := time.Now().Unix()
	return db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		for idx, id := range orderedIDs {
			sortOrder := idx + 1
			result := tx.Exec(
				"UPDATE agents SET sort_order = ?, updatetime = ? WHERE id = ? AND department_id = ? AND parent_agent_id = ? AND status = ?",
				sortOrder, now, id, departmentID, parentAgentID, consts.ACTIVE,
			)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("agent reorder affected %d rows for id %d", result.RowsAffected, id)
			}
		}
		return nil
	})
}
```

- [ ] **Step 5: 跑测试看它通过**

Run: `go test ./internal/repository/agent_repo/ -run TestAgentReorderSiblings`
Expected: PASS。

- [ ] **Step 6: 重新生成 mock**

Run: `make mock`
Expected: `mock_agent_repo/mock_agent.go` 出现 `ReorderSiblings` 方法(供 Task 2 用)。

- [ ] **Step 7: 提交**

```bash
git add internal/repository/agent_repo/agent.go internal/repository/agent_repo/agent_test.go internal/repository/agent_repo/mock_agent_repo/mock_agent.go
git commit internal/repository/agent_repo/agent.go internal/repository/agent_repo/agent_test.go internal/repository/agent_repo/mock_agent_repo/mock_agent.go -m "✨ agent: repo 新增 ReorderSiblings 同级密集重编号"
```

---

### Task 2: `agent_svc.Reorder` + `ReorderAgentsRequest` + `App.ReorderAgents`

**Files:**
- Modify: `internal/service/agent_svc/types.go`(加 request 类型)
- Modify: `internal/service/agent_svc/agent.go`(接口 L35-43 加一行;impl 加 `Reorder`)
- Modify: `internal/app/agent.go`(加 binding)
- Test: `internal/service/agent_svc/agent_test.go`

**Interfaces:**
- Consumes: `agent_repo.Agent().ReorderSiblings(...)`(Task 1);`agent_repo.Agent().ListByDepartment(ctx, deptID)` 返回 `department_id=deptID AND parent_agent_id=0` 的组;`agent_repo.Agent().ListByParent(ctx, parentID)` 返回 `parent_agent_id=parentID` 的组。
- Produces: `ReorderAgentsRequest{ DepartmentID, ParentAgentID int64; OrderedIDs []int64 }`(json: `departmentId`/`parentAgentId`/`orderedIds`);`AgentSvc.Reorder(ctx, *ReorderAgentsRequest) error`;Wails `App.ReorderAgents(req *agent_svc.ReorderAgentsRequest) error`。

- [ ] **Step 1: 写失败测试**

`internal/service/agent_svc/agent_test.go` 末尾追加(`setupSvc` 已提供 `agentMock`):

```go
func TestReorderAgents(t *testing.T) {
	convey.Convey("Agent 同级排序", t, func() {
		ctx, agentMock, _, _, svc := setupSvc(t)

		convey.Convey("部门下成功重排", func() {
			agentMock.EXPECT().ListByDepartment(gomock.Any(), int64(2)).
				Return([]*agent_entity.Agent{{ID: 1}, {ID: 3}}, nil)
			agentMock.EXPECT().ReorderSiblings(gomock.Any(), int64(2), int64(0), []int64{3, 1}).Return(nil)
			err := svc.Reorder(ctx, &ReorderAgentsRequest{DepartmentID: 2, OrderedIDs: []int64{3, 1}})
			convey.So(err, convey.ShouldBeNil)
		})

		convey.Convey("外来 id 拒绝", func() {
			agentMock.EXPECT().ListByDepartment(gomock.Any(), int64(2)).
				Return([]*agent_entity.Agent{{ID: 1}, {ID: 3}}, nil)
			err := svc.Reorder(ctx, &ReorderAgentsRequest{DepartmentID: 2, OrderedIDs: []int64{3, 9}})
			convey.So(err, convey.ShouldNotBeNil)
		})

		convey.Convey("既没部门也没上级 → 参数错误", func() {
			err := svc.Reorder(ctx, &ReorderAgentsRequest{OrderedIDs: []int64{1}})
			convey.So(err, convey.ShouldNotBeNil)
		})
	})
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `go test ./internal/service/agent_svc/ -run TestReorderAgents`
Expected: 编译失败 —— `ReorderAgentsRequest` / `svc.Reorder` 未定义。

- [ ] **Step 3: 加 request 类型**

`internal/service/agent_svc/types.go` 在 `MoveAgentResponse`(L52-54)后追加:

```go
// ReorderAgentsRequest 同级密集重排:orderedIds 必须是该组的完整集合。
type ReorderAgentsRequest struct {
	DepartmentID  int64   `json:"departmentId"`
	ParentAgentID int64   `json:"parentAgentId"`
	OrderedIDs    []int64 `json:"orderedIds"`
}
```

- [ ] **Step 4: 接口 + 实现**

`internal/service/agent_svc/agent.go` 接口(L35-43)`SetPinned` 那行下加:

```go
	Reorder(ctx context.Context, req *ReorderAgentsRequest) error
```

在 `Move` 函数(L138-167)之后追加实现:

```go
func (s *agentSvc) Reorder(ctx context.Context, req *ReorderAgentsRequest) error {
	if req == nil || len(req.OrderedIDs) == 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	// 与实体 Check 一致:恰好挂在部门或上级之一。
	if (req.DepartmentID > 0) == (req.ParentAgentID > 0) {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	var siblings []*agent_entity.Agent
	var err error
	if req.ParentAgentID > 0 {
		siblings, err = agent_repo.Agent().ListByParent(ctx, req.ParentAgentID)
	} else {
		siblings, err = agent_repo.Agent().ListByDepartment(ctx, req.DepartmentID)
	}
	if err != nil {
		return err
	}
	if len(siblings) != len(req.OrderedIDs) {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	allowed := make(map[int64]struct{}, len(siblings))
	for _, a := range siblings {
		allowed[a.ID] = struct{}{}
	}
	seen := make(map[int64]struct{}, len(req.OrderedIDs))
	for _, id := range req.OrderedIDs {
		if id <= 0 {
			return i18n.NewError(ctx, code.InvalidParameter)
		}
		if _, ok := allowed[id]; !ok {
			return i18n.NewError(ctx, code.InvalidParameter)
		}
		if _, ok := seen[id]; ok {
			return i18n.NewError(ctx, code.InvalidParameter)
		}
		seen[id] = struct{}{}
	}
	return agent_repo.Agent().ReorderSiblings(ctx, req.DepartmentID, req.ParentAgentID, req.OrderedIDs)
}
```

- [ ] **Step 5: 加 Wails binding**

`internal/app/agent.go` 在 `MoveAgent`(L18-21)后追加:

```go
// ReorderAgents 同级密集重排(orderedIds 为该组完整集合)。
func (a *App) ReorderAgents(req *agent_svc.ReorderAgentsRequest) error {
	return agent_svc.Agent().Reorder(a.ctx, req)
}
```

- [ ] **Step 6: 跑测试看它通过**

Run: `go test ./internal/service/agent_svc/ -run TestReorderAgents`
Expected: PASS(三个子用例全绿)。

- [ ] **Step 7: 提交**

```bash
git add internal/service/agent_svc/types.go internal/service/agent_svc/agent.go internal/service/agent_svc/agent_test.go internal/app/agent.go
git commit internal/service/agent_svc/types.go internal/service/agent_svc/agent.go internal/service/agent_svc/agent_test.go internal/app/agent.go -m "✨ agent: svc/app 新增 Reorder 同级排序(全集校验)"
```

---

## Phase 2 — 后端:Department 同级排序

### Task 3: `department_repo.ReorderSiblings`

**Files:**
- Modify: `internal/repository/department_repo/department.go`(接口 L18-28 加一行;impl 追加;imports 加 `fmt`/`time`)
- Test: `internal/repository/department_repo/department_test.go`
- Regenerate: `internal/repository/department_repo/mock_department_repo/mock_department.go`(`make mock`)

**Interfaces:**
- Produces: `ReorderSiblings(ctx context.Context, parentID int64, orderedIDs []int64) error` —— 事务内对 `parent_id=?` 组按序写 `sort_order=1..N`,`RowsAffected!=1` 报错。

- [ ] **Step 1: 写失败测试**

`internal/repository/department_repo/department_test.go` 末尾追加:

```go
func TestDepartmentReorderSiblings(t *testing.T) {
	ctx, mock, repo := setupRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE departments SET sort_order = \\?, updatetime = \\? WHERE id = \\? AND parent_id = \\? AND status = \\?").
		WithArgs(1, sqlmock.AnyArg(), int64(5), int64(0), consts.ACTIVE).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE departments SET sort_order = \\?, updatetime = \\? WHERE id = \\? AND parent_id = \\? AND status = \\?").
		WithArgs(2, sqlmock.AnyArg(), int64(4), int64(0), consts.ACTIVE).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.ReorderSiblings(ctx, 0, []int64{5, 4})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

> 若 `department_test.go` 顶部尚未 import `sqlmock`/`consts`/`require`/`assert`,照 `agent_test.go` L3-15 补齐(结构一致)。

- [ ] **Step 2: 跑测试看它失败**

Run: `go test ./internal/repository/department_repo/ -run TestDepartmentReorderSiblings`
Expected: 编译失败 —— `repo.ReorderSiblings undefined`。

- [ ] **Step 3: 加接口方法**

`internal/repository/department_repo/department.go` 接口(L18-28)`NextSortOrder` 那行下加:

```go
	ReorderSiblings(ctx context.Context, parentID int64, orderedIDs []int64) error
```

- [ ] **Step 4: 加 imports + 实现**

`department.go` import 块补 `"fmt"`、`"time"`。在 `NextSortOrder` 函数后追加:

```go
func (r *departmentRepo) ReorderSiblings(ctx context.Context, parentID int64, orderedIDs []int64) error {
	now := time.Now().Unix()
	return db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		for idx, id := range orderedIDs {
			sortOrder := idx + 1
			result := tx.Exec(
				"UPDATE departments SET sort_order = ?, updatetime = ? WHERE id = ? AND parent_id = ? AND status = ?",
				sortOrder, now, id, parentID, consts.ACTIVE,
			)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("department reorder affected %d rows for id %d", result.RowsAffected, id)
			}
		}
		return nil
	})
}
```

- [ ] **Step 5: 跑测试看它通过**

Run: `go test ./internal/repository/department_repo/ -run TestDepartmentReorderSiblings`
Expected: PASS。

- [ ] **Step 6: 重新生成 mock**

Run: `make mock`
Expected: `mock_department_repo/mock_department.go` 出现 `ReorderSiblings`。

- [ ] **Step 7: 提交**

```bash
git add internal/repository/department_repo/department.go internal/repository/department_repo/department_test.go internal/repository/department_repo/mock_department_repo/mock_department.go
git commit internal/repository/department_repo/department.go internal/repository/department_repo/department_test.go internal/repository/department_repo/mock_department_repo/mock_department.go -m "✨ department: repo 新增 ReorderSiblings 同级密集重编号"
```

---

### Task 4: `department_svc.Reorder` + `ReorderDepartmentsRequest` + `App.ReorderDepartments`

**Files:**
- Modify: `internal/service/department_svc/types.go`
- Modify: `internal/service/department_svc/department.go`(接口 L30-36 加一行;impl 加 `Reorder`)
- Modify: `internal/app/department.go`
- Test: `internal/service/department_svc/department_test.go`

**Interfaces:**
- Consumes: `department_repo.Department().ListByParent(ctx, parentID)`;`department_repo.Department().ReorderSiblings(...)`(Task 3)。
- Produces: `ReorderDepartmentsRequest{ ParentID int64; OrderedIDs []int64 }`(json: `parentId`/`orderedIds`);`DepartmentSvc.Reorder(ctx, *ReorderDepartmentsRequest) error`;`App.ReorderDepartments(req *department_svc.ReorderDepartmentsRequest) error`。

- [ ] **Step 1: 写失败测试**

`internal/service/department_svc/department_test.go` 末尾追加(用该文件既有的 setup helper —— 参考 L48/L90 的 `deptMock` 用法):

```go
func TestReorderDepartments(t *testing.T) {
	convey.Convey("部门同级排序", t, func() {
		ctx, deptMock, svc := setupSvc(t)

		convey.Convey("成功重排顶层", func() {
			deptMock.EXPECT().ListByParent(gomock.Any(), int64(0)).
				Return([]*department_entity.Department{{ID: 4}, {ID: 5}}, nil)
			deptMock.EXPECT().ReorderSiblings(gomock.Any(), int64(0), []int64{5, 4}).Return(nil)
			err := svc.Reorder(ctx, &ReorderDepartmentsRequest{ParentID: 0, OrderedIDs: []int64{5, 4}})
			convey.So(err, convey.ShouldBeNil)
		})

		convey.Convey("集合不全 → 参数错误", func() {
			deptMock.EXPECT().ListByParent(gomock.Any(), int64(0)).
				Return([]*department_entity.Department{{ID: 4}, {ID: 5}}, nil)
			err := svc.Reorder(ctx, &ReorderDepartmentsRequest{ParentID: 0, OrderedIDs: []int64{5}})
			convey.So(err, convey.ShouldNotBeNil)
		})
	})
}
```

> ⚠️ 先打开 `department_test.go` 确认 setup helper 的**真实签名与返回顺序**(是 `setupSvc(t)` 还是别的名字、返回 `(ctx, deptMock, svc)` 还是含更多 mock),按实际调整这两个用例的解构。若还没 import `department_entity`/`gomock`,照 `agent_test.go` L11-15 补。

- [ ] **Step 2: 跑测试看它失败**

Run: `go test ./internal/service/department_svc/ -run TestReorderDepartments`
Expected: 编译失败 —— 未定义。

- [ ] **Step 3: 加 request 类型**

`internal/service/department_svc/types.go` 在 `MoveDepartmentResponse`(L114-116)后追加:

```go
// ReorderDepartmentsRequest 同级密集重排:orderedIds 必须是该父级下的完整集合。
type ReorderDepartmentsRequest struct {
	ParentID   int64   `json:"parentId"`
	OrderedIDs []int64 `json:"orderedIds"`
}
```

- [ ] **Step 4: 接口 + 实现**

`internal/service/department_svc/department.go` 接口(L30-36)`Delete` 那行下加:

```go
	Reorder(ctx context.Context, req *ReorderDepartmentsRequest) error
```

在 `Move` 函数(L290 起)之后追加实现:

```go
func (s *departmentSvc) Reorder(ctx context.Context, req *ReorderDepartmentsRequest) error {
	if req == nil || req.ParentID < 0 || len(req.OrderedIDs) == 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	siblings, err := department_repo.Department().ListByParent(ctx, req.ParentID)
	if err != nil {
		return err
	}
	if len(siblings) != len(req.OrderedIDs) {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	allowed := make(map[int64]struct{}, len(siblings))
	for _, d := range siblings {
		allowed[d.ID] = struct{}{}
	}
	seen := make(map[int64]struct{}, len(req.OrderedIDs))
	for _, id := range req.OrderedIDs {
		if id <= 0 {
			return i18n.NewError(ctx, code.InvalidParameter)
		}
		if _, ok := allowed[id]; !ok {
			return i18n.NewError(ctx, code.InvalidParameter)
		}
		if _, ok := seen[id]; ok {
			return i18n.NewError(ctx, code.InvalidParameter)
		}
		seen[id] = struct{}{}
	}
	return department_repo.Department().ReorderSiblings(ctx, req.ParentID, req.OrderedIDs)
}
```

> 确认 `department.go` 顶部已 import `code` 与 `i18n`(`Move`/`Create` 已用到,应已存在)。

- [ ] **Step 5: 加 Wails binding**

`internal/app/department.go` 找到 `MoveDepartment` binding,在其后追加:

```go
// ReorderDepartments 同级密集重排(orderedIds 为该父级下完整集合)。
func (a *App) ReorderDepartments(req *department_svc.ReorderDepartmentsRequest) error {
	return department_svc.Default().Reorder(a.ctx, req)
}
```

> ⚠️ 先看 `internal/app/department.go` 里现有 binding 是用 `department_svc.Default()` 还是别的 accessor(department_svc 用 `Default()`,见 `department.go` L44 附近),照抄同一个。

- [ ] **Step 6: 跑测试 + 全量后端**

Run: `go test ./internal/service/department_svc/ -run TestReorderDepartments`
Expected: PASS。
Run: `make test-backend`
Expected: 全绿(确认没碰坏别的包)。

- [ ] **Step 7: 提交**

```bash
git add internal/service/department_svc/types.go internal/service/department_svc/department.go internal/service/department_svc/department_test.go internal/app/department.go
git commit internal/service/department_svc/types.go internal/service/department_svc/department.go internal/service/department_svc/department_test.go internal/app/department.go -m "✨ department: svc/app 新增 Reorder 同级排序(全集校验)"
```

---

## Phase 3 — 前端:树形图

> 开工前 Run: `make generate`(刷新 `frontend/wailsjs`,让 `ReorderAgents`/`ReorderDepartments` 可 import;gitignore 生成物,不提交)。

### Task 5: 树同级按 sortOrder 稳定排序 + 关掉拖拽补间(手感)

**Files:**
- Modify: `frontend/src/components/agentre/org/org-tree.tsx`(`agentChildren` L393、`buildDepartmentNode` L417-420、`buildLayoutRoots` L438-448、`AgentCard`/`DepartmentBanner` className)
- Test: `frontend/src/components/agentre/org/__tests__/org-tree-layout.test.ts`(新建;若已有同名布局测试文件则并入)

**Interfaces:**
- Produces: 树子节点按 `sortOrder`(次键 `id`)升序;`buildOrgTreeLayout` 输出顺序随之确定。拖拽中被拖卡片 `transition-none`。

- [ ] **Step 1: 写失败测试**

新建 `frontend/src/components/agentre/org/__tests__/org-tree-layout.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { buildOrgTreeLayout } from "../org-tree";
import type { OrgAgent } from "../types";

function agent(partial: Partial<OrgAgent> & { id: number }): OrgAgent {
  return {
    id: partial.id,
    name: `a${partial.id}`,
    description: "",
    avatarColor: "neutral",
    avatarIcon: "",
    avatarDataUrl: "",
    systemBadge: partial.systemBadge ?? "",
    departmentId: partial.departmentId ?? 0,
    parentAgentId: partial.parentAgentId ?? 0,
    agentBackendId: 0,
    sortOrder: partial.sortOrder ?? 0,
    skills: [],
  } as unknown as OrgAgent;
}

describe("buildOrgTreeLayout 同级顺序", () => {
  it("同级 agent 按 sortOrder 升序排列(x 从小到大)", () => {
    const ceo = agent({ id: 1, systemBadge: "DEFAULT" });
    const a2 = agent({ id: 2, parentAgentId: 1, sortOrder: 2 });
    const a3 = agent({ id: 3, parentAgentId: 1, sortOrder: 1 });
    const layout = buildOrgTreeLayout({
      agents: [ceo, a2, a3],
      departments: [],
      collapse: {},
    });
    const n2 = layout.nodes.find((n) => n.key === "agent-2")!;
    const n3 = layout.nodes.find((n) => n.key === "agent-3")!;
    // sortOrder=1 的 a3 应在左边(x 更小)
    expect(n3.x).toBeLessThan(n2.x);
  });
});
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-tree-layout.test.ts`
Expected: FAIL —— 目前按数组顺序,`a2` 在左(x 更小),断言不成立。

- [ ] **Step 3: 加同级比较器并排序**

`org-tree.tsx` 在 `agentChildren`(L393)上方加一个模块级比较器:

```ts
function bySortOrder(
  a: { sortOrder?: number; id: number },
  b: { sortOrder?: number; id: number },
): number {
  return (a.sortOrder ?? 0) - (b.sortOrder ?? 0) || a.id - b.id;
}
```

改 `agentChildren`(L393-397):

```ts
function agentChildren(agent: OrgAgent, all: OrgTreeLayoutInput) {
  return all.agents
    .filter((a) => (a.parentAgentId ?? 0) === agent.id && a.id !== agent.id)
    .sort(bySortOrder)
    .map((a) => buildAgentNode(a, all));
}
```

改 `buildDepartmentNode`(L417-420)的两个 filter,各接 `.sort(bySortOrder)`:

```ts
  const childAgents = all.agents
    .filter((a) => a.departmentId === dept.id && (a.parentAgentId ?? 0) === 0)
    .sort(bySortOrder);
  const childDepts = all.departments
    .filter((d) => d.parentId === dept.id)
    .sort(bySortOrder);
```

改 `buildLayoutRoots`(L439-448)的 `topDepartments` / `topAgents`,同样各接 `.sort(bySortOrder)`(注意 `topAgents` 的 filter 保持不变,只在末尾 `.sort(bySortOrder)`)。

- [ ] **Step 4: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-tree-layout.test.ts`
Expected: PASS。

- [ ] **Step 5: 关掉拖拽时的 CSS 补间(手感修复)**

`org-tree.tsx` `DepartmentBanner` 的 `className`(L771-780,`cn(...)` 里含 `transition-all`)追加一项:

```ts
        drag.isDragging && "transition-none",
```

`AgentCard` 的 `className`(L878-890,含 `transition-all`)同样追加:

```ts
        drag.isDragging && "transition-none",
```

> 原理:拖拽时 dnd-kit 每帧写 inline `style.transform`,`transition-all` 会补间它导致「橡皮筋」滞后;`transition-none` 让 transform 立即生效、跟手。

- [ ] **Step 6: 跑相关测试 + lint**

Run: `cd frontend && pnpm test -- src/components/agentre/org/`
Expected: PASS(含既有 org-tree 测试)。
Run: `make lint`(在 `agentre/` 根)
Expected: 无新增错误。

- [ ] **Step 7: 提交**

```bash
git add frontend/src/components/agentre/org/org-tree.tsx frontend/src/components/agentre/org/__tests__/org-tree-layout.test.ts
git commit frontend/src/components/agentre/org/org-tree.tsx frontend/src/components/agentre/org/__tests__/org-tree-layout.test.ts -m "✨ org-tree: 同级按 sortOrder 稳定排序 + 拖拽 transition-none 修手感"
```

---

### Task 6: `use-org-data` 乐观重排 + reorder 纯函数

**Files:**
- Create: `frontend/src/components/agentre/org/reorder.ts`
- Create: `frontend/src/components/agentre/org/__tests__/reorder.test.ts`
- Modify: `frontend/src/components/agentre/org/use-org-data.ts`

**Interfaces:**
- Produces:
  - `computeReorder(orderedIds: number[], draggedId: number, insertIndex: number): number[]` —— 把 `draggedId` 移到原组下标 `insertIndex`(0..n)处。
  - `applyAgentOrder(agents, departmentId, parentAgentId, orderedIds): OrgAgent[]` / `applyDepartmentOrder(departments, parentId, orderedIds): OrgDepartment[]` —— 按新序回填 `sortOrder`(乐观)。
  - `useOrgData()` 新增 `reorderAgents(departmentId, parentAgentId, orderedIds)` 与 `reorderDepartments(parentId, orderedIds)`。

- [ ] **Step 1: 写失败测试**

新建 `frontend/src/components/agentre/org/__tests__/reorder.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { applyAgentOrder, computeReorder } from "../reorder";
import type { OrgAgent } from "../types";

describe("computeReorder", () => {
  it("把中间项拖到最前", () => {
    expect(computeReorder([1, 2, 3], 3, 0)).toEqual([3, 1, 2]);
  });
  it("把首项拖到末尾(insertIndex=组长度)", () => {
    expect(computeReorder([1, 2, 3], 1, 3)).toEqual([2, 3, 1]);
  });
  it("拖到原位不变", () => {
    expect(computeReorder([1, 2, 3], 2, 1)).toEqual([1, 2, 3]);
  });
});

describe("applyAgentOrder", () => {
  it("只回填目标组的 sortOrder", () => {
    const agents = [
      { id: 1, departmentId: 2, parentAgentId: 0, sortOrder: 1 },
      { id: 3, departmentId: 2, parentAgentId: 0, sortOrder: 2 },
      { id: 9, departmentId: 7, parentAgentId: 0, sortOrder: 1 },
    ] as unknown as OrgAgent[];
    const out = applyAgentOrder(agents, 2, 0, [3, 1]);
    expect(out.find((a) => a.id === 3)!.sortOrder).toBe(1);
    expect(out.find((a) => a.id === 1)!.sortOrder).toBe(2);
    expect(out.find((a) => a.id === 9)!.sortOrder).toBe(1); // 别的组不动
  });
});
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/reorder.test.ts`
Expected: FAIL —— `../reorder` 不存在。

- [ ] **Step 3: 实现纯函数**

新建 `frontend/src/components/agentre/org/reorder.ts`:

```ts
import type { OrgAgent, OrgDepartment } from "./types";

// 把 draggedId 移到原组下标 insertIndex(0..n)处。insertIndex 以「移除前」的下标计。
export function computeReorder(
  orderedIds: number[],
  draggedId: number,
  insertIndex: number,
): number[] {
  const from = orderedIds.indexOf(draggedId);
  if (from < 0) return orderedIds.slice();
  const without = orderedIds.filter((id) => id !== draggedId);
  const adjusted = insertIndex > from ? insertIndex - 1 : insertIndex;
  const clamped = Math.max(0, Math.min(adjusted, without.length));
  return [...without.slice(0, clamped), draggedId, ...without.slice(clamped)];
}

export function applyAgentOrder(
  agents: OrgAgent[],
  departmentId: number,
  parentAgentId: number,
  orderedIds: number[],
): OrgAgent[] {
  const pos = new Map(orderedIds.map((id, i) => [id, i + 1]));
  return agents.map((a) =>
    (a.departmentId ?? 0) === departmentId &&
    (a.parentAgentId ?? 0) === parentAgentId &&
    pos.has(a.id)
      ? { ...a, sortOrder: pos.get(a.id)! }
      : a,
  );
}

export function applyDepartmentOrder(
  departments: OrgDepartment[],
  parentId: number,
  orderedIds: number[],
): OrgDepartment[] {
  const pos = new Map(orderedIds.map((id, i) => [id, i + 1]));
  return departments.map((d) =>
    (d.parentId ?? 0) === parentId && pos.has(d.id)
      ? { ...d, sortOrder: pos.get(d.id)! }
      : d,
  );
}
```

- [ ] **Step 4: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/reorder.test.ts`
Expected: PASS。

- [ ] **Step 5: 接入 use-org-data 乐观重排**

`use-org-data.ts` import 块(L5-18)加两个 binding:

```ts
  ReorderAgents,
  ReorderDepartments,
```

并从新文件引入(放在既有 `import type { OrgAgent, OrgDepartment } from "./types";` 附近):

```ts
import { applyAgentOrder, applyDepartmentOrder } from "./reorder";
```

在 return 对象(L92-115)里,`moveAgent`/`moveDepartment` 附近追加:

```ts
    reorderAgents: (
      departmentId: number,
      parentAgentId: number,
      orderedIds: number[],
    ) => {
      setState((s) => ({
        ...s,
        agents: applyAgentOrder(s.agents, departmentId, parentAgentId, orderedIds),
      }));
      return mutate(() =>
        ReorderAgents({ departmentId, parentAgentId, orderedIds }),
      );
    },
    reorderDepartments: (parentId: number, orderedIds: number[]) => {
      setState((s) => ({
        ...s,
        departments: applyDepartmentOrder(s.departments, parentId, orderedIds),
      }));
      return mutate(() => ReorderDepartments({ parentId, orderedIds }));
    },
```

> `mutate` 的 `finally` 总会 `reload()` 对账:成功时服务器顺序 == 乐观顺序 → 不闪;失败时 reload 回滚到服务器真序 + 走 error 通道。`ReorderAgents`/`ReorderDepartments` 接收普通对象即可(与既有 `moveAgent` 传普通对象一致)。

- [ ] **Step 6: 跑测试 + lint**

Run: `cd frontend && pnpm test -- src/components/agentre/org/`
Expected: PASS。
Run: `make lint`
Expected: 无新增错误(注意 `ReorderAgents`/`ReorderDepartments` 需 `make generate` 后才存在)。

- [ ] **Step 7: 提交**

```bash
git add frontend/src/components/agentre/org/reorder.ts frontend/src/components/agentre/org/__tests__/reorder.test.ts frontend/src/components/agentre/org/use-org-data.ts
git commit frontend/src/components/agentre/org/reorder.ts frontend/src/components/agentre/org/__tests__/reorder.test.ts frontend/src/components/agentre/org/use-org-data.ts -m "✨ org: use-org-data 乐观重排 + reorder 纯函数"
```

---

### Task 7: 树插入位 drop-zone + 接线 reorder

**Files:**
- Create: `frontend/src/components/agentre/org/tree-insert-zones.ts`
- Create: `frontend/src/components/agentre/org/__tests__/tree-insert-zones.test.ts`
- Modify: `frontend/src/components/agentre/org/org-tree.tsx`(props、DndContext、TreeCanvas、AgentCard/DepartmentBanner 的 `data`)
- Modify: `frontend/src/components/agentre/org-chart-page.tsx`(接线 `onReorderAgent`/`onReorderDepartment`)

**Interfaces:**
- Consumes: `computeReorder`(Task 6);`useOrgData().reorderAgents/reorderDepartments`(Task 6);`buildOrgTreeLayout` 输出的 `OrgTreeLayout`(Task 5 已确定顺序)。
- Produces:
  - `type InsertZone`(带 `id/kind/departmentId/parentAgentId/parentId/index/orderedIds/x/y/height`)。
  - `buildInsertZones(layout, input): InsertZone[]`。
  - `type ActiveDrag = { id: number; kind: "agent" | "dept"; departmentId: number; parentAgentId: number; parentId: number }`。
  - `isZoneValidTarget(zone: InsertZone, active: ActiveDrag): boolean`。
  - `OrgTree` 新增 props `onReorderAgent(departmentId, parentAgentId, orderedIds)` / `onReorderDepartment(parentId, orderedIds)`。

- [ ] **Step 1: 写失败测试**

新建 `frontend/src/components/agentre/org/__tests__/tree-insert-zones.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { buildOrgTreeLayout } from "../org-tree";
import { buildInsertZones, isZoneValidTarget } from "../tree-insert-zones";
import type { OrgAgent } from "../types";

function agent(p: Partial<OrgAgent> & { id: number }): OrgAgent {
  return {
    id: p.id,
    name: `a${p.id}`,
    description: "",
    avatarColor: "neutral",
    avatarIcon: "",
    avatarDataUrl: "",
    systemBadge: p.systemBadge ?? "",
    departmentId: p.departmentId ?? 0,
    parentAgentId: p.parentAgentId ?? 0,
    agentBackendId: 0,
    sortOrder: p.sortOrder ?? 0,
    skills: [],
  } as unknown as OrgAgent;
}

describe("buildInsertZones", () => {
  it("N 个同级 agent 产出 N+1 个插入位,orderedIds 为该组现序", () => {
    const ceo = agent({ id: 1, systemBadge: "DEFAULT" });
    const a2 = agent({ id: 2, parentAgentId: 1, sortOrder: 1 });
    const a3 = agent({ id: 3, parentAgentId: 1, sortOrder: 2 });
    const layout = buildOrgTreeLayout({
      agents: [ceo, a2, a3],
      departments: [],
      collapse: {},
    });
    const zones = buildInsertZones(layout, {
      agents: [ceo, a2, a3],
      departments: [],
      collapse: {},
    });
    const group = zones.filter(
      (z) => z.kind === "agent" && z.parentAgentId === 1,
    );
    expect(group).toHaveLength(3); // before / between / after
    expect(group.every((z) => z.orderedIds.join() === "2,3")).toBe(true);
    // x 递增
    expect(group[0].x).toBeLessThan(group[1].x);
    expect(group[1].x).toBeLessThan(group[2].x);
  });
});

describe("isZoneValidTarget", () => {
  const zone = {
    id: "insert-agent-0-1-0",
    kind: "agent" as const,
    departmentId: 0,
    parentAgentId: 1,
    parentId: 0,
    index: 0,
    orderedIds: [2, 3],
    x: 0,
    y: 0,
    height: 10,
  };
  it("同组同类型 → 合法", () => {
    expect(
      isZoneValidTarget(zone, {
        id: 2,
        kind: "agent",
        departmentId: 0,
        parentAgentId: 1,
        parentId: 0,
      }),
    ).toBe(true);
  });
  it("跨组 → 非法", () => {
    expect(
      isZoneValidTarget(zone, {
        id: 9,
        kind: "agent",
        departmentId: 5,
        parentAgentId: 0,
        parentId: 0,
      }),
    ).toBe(false);
  });
});
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/tree-insert-zones.test.ts`
Expected: FAIL —— `../tree-insert-zones` 不存在。

- [ ] **Step 3: 实现 buildInsertZones / isZoneValidTarget**

新建 `frontend/src/components/agentre/org/tree-insert-zones.ts`:

```ts
import type { OrgTreeLayout, OrgTreeLayoutNode } from "./org-tree";
import type { OrgAgent, OrgDepartment } from "./types";

const ZONE_HALF = 10; // 命中条半宽(canvas 坐标)

export type InsertZone = {
  id: string;
  kind: "agent" | "dept";
  departmentId: number; // agent 组键
  parentAgentId: number; // agent 组键
  parentId: number; // dept 组键
  index: number; // 插入下标(0..n)
  orderedIds: number[]; // 该组现序 id
  x: number; // 命中条中心 x(canvas 坐标)
  y: number;
  height: number;
};

export type ActiveDrag = {
  id: number;
  kind: "agent" | "dept";
  departmentId: number;
  parentAgentId: number;
  parentId: number;
};

type ZoneInput = {
  agents: OrgAgent[];
  departments: OrgDepartment[];
  collapse: Record<number, boolean>;
};

export function isZoneValidTarget(zone: InsertZone, active: ActiveDrag): boolean {
  if (zone.kind !== active.kind) return false;
  if (zone.kind === "agent") {
    return (
      zone.departmentId === active.departmentId &&
      zone.parentAgentId === active.parentAgentId
    );
  }
  return zone.parentId === active.parentId;
}

// 从一组左右相邻(按 x 升序)的节点算出 N+1 个插入位几何。
function slotsForGroup(
  nodes: OrgTreeLayoutNode[],
): Array<{ index: number; x: number; y: number; height: number }> {
  const sorted = [...nodes].sort((a, b) => a.x - b.x);
  const y = Math.min(...sorted.map((n) => n.y));
  const height = Math.max(...sorted.map((n) => n.height));
  const out: Array<{ index: number; x: number; y: number; height: number }> = [];
  for (let i = 0; i <= sorted.length; i++) {
    let x: number;
    if (i === 0) {
      x = sorted[0].x - sorted[0].width / 2 - ZONE_HALF;
    } else if (i === sorted.length) {
      const last = sorted[sorted.length - 1];
      x = last.x + last.width / 2 + ZONE_HALF;
    } else {
      const left = sorted[i - 1];
      const right = sorted[i];
      x = (left.x + left.width / 2 + (right.x - right.width / 2)) / 2;
    }
    out.push({ index: i, x, y, height });
  }
  return out;
}

export function buildInsertZones(
  layout: OrgTreeLayout,
  input: ZoneInput,
): InsertZone[] {
  const nodeByKey = new Map<string, OrgTreeLayoutNode>(
    layout.nodes.map((n) => [n.key, n]),
  );
  const zones: InsertZone[] = [];

  // ── agent 组:按 (departmentId, parentAgentId) 分组(排除 CEO/系统 agent)──
  const agentGroups = new Map<string, OrgAgent[]>();
  for (const a of input.agents) {
    if (a.systemBadge === "DEFAULT") continue;
    const key = `${a.departmentId ?? 0}:${a.parentAgentId ?? 0}`;
    if (!agentGroups.has(key)) agentGroups.set(key, []);
    agentGroups.get(key)!.push(a);
  }
  for (const members of agentGroups.values()) {
    const ordered = [...members].sort(
      (a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0) || a.id - b.id,
    );
    const nodes = ordered
      .map((a) => nodeByKey.get(`agent-${a.id}`))
      .filter((n): n is OrgTreeLayoutNode => Boolean(n));
    if (nodes.length !== ordered.length || nodes.length === 0) continue; // 有成员没渲染(折叠)→ 跳过
    const departmentId = ordered[0].departmentId ?? 0;
    const parentAgentId = ordered[0].parentAgentId ?? 0;
    const orderedIds = ordered.map((a) => a.id);
    for (const slot of slotsForGroup(nodes)) {
      zones.push({
        id: `insert-agent-${departmentId}-${parentAgentId}-${slot.index}`,
        kind: "agent",
        departmentId,
        parentAgentId,
        parentId: 0,
        index: slot.index,
        orderedIds,
        x: slot.x,
        y: slot.y,
        height: slot.height,
      });
    }
  }

  // ── department 组:按 parentId 分组 ──
  const deptGroups = new Map<number, OrgDepartment[]>();
  for (const d of input.departments) {
    const key = d.parentId ?? 0;
    if (!deptGroups.has(key)) deptGroups.set(key, []);
    deptGroups.get(key)!.push(d);
  }
  for (const [parentId, members] of deptGroups.entries()) {
    // 父部门折叠时子部门不渲染 → 跳过
    if (parentId > 0 && input.collapse[parentId]) continue;
    const ordered = [...members].sort(
      (a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0) || a.id - b.id,
    );
    const nodes = ordered
      .map((d) => nodeByKey.get(`dept-${d.id}`))
      .filter((n): n is OrgTreeLayoutNode => Boolean(n));
    if (nodes.length !== ordered.length || nodes.length === 0) continue;
    const orderedIds = ordered.map((d) => d.id);
    for (const slot of slotsForGroup(nodes)) {
      zones.push({
        id: `insert-dept-${parentId}-${slot.index}`,
        kind: "dept",
        departmentId: 0,
        parentAgentId: 0,
        parentId,
        index: slot.index,
        orderedIds,
        x: slot.x,
        y: slot.y,
        height: slot.height,
      });
    }
  }

  return zones;
}
```

- [ ] **Step 4: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/tree-insert-zones.test.ts`
Expected: PASS。

- [ ] **Step 5: OrgTree 接线插入位 + reorder**

在 `org-tree.tsx`:

(a) `OrgTreeProps`(L29-45)追加两个回调:

```ts
  onReorderAgent: (
    departmentId: number,
    parentAgentId: number,
    orderedIds: number[],
  ) => void;
  onReorderDepartment: (parentId: number, orderedIds: number[]) => void;
```

(b) import 补 `useDndContext`(来自 `@dnd-kit/core`)与本地模块:

```ts
import {
  buildInsertZones,
  isZoneValidTarget,
  type ActiveDrag,
  type InsertZone,
} from "./tree-insert-zones";
import { computeReorder } from "./reorder";
```

(c) `OrgTree` 组件内加 active 状态,并改 `DndContext`:

```tsx
  const [active, setActive] = React.useState<ActiveDrag | null>(null);
```

`DndContext` 加 `onDragStart` / `onDragCancel`,并把 `handleDragEnd` 扩展为分流(见下)。`onDragStart` 从 `e.active.data.current` 取 `ActiveDrag`;`onDragCancel` 清 `active`。

(d) 改 `handleDragEnd`(L497-517):

```tsx
  const handleDragEnd = React.useCallback(
    (e: DragEndEvent) => {
      setActive(null);
      if (!e.over) return;
      const overId = String(e.over.id);
      if (overId.startsWith("insert-")) {
        const zone = e.over.data.current as InsertZone | undefined;
        const activeData = e.active.data.current as ActiveDrag | undefined;
        if (!zone || !activeData || !isZoneValidTarget(zone, activeData)) return;
        const orderedIds = computeReorder(zone.orderedIds, activeData.id, zone.index);
        if (zone.kind === "agent") {
          props.onReorderAgent(zone.departmentId, zone.parentAgentId, orderedIds);
        } else {
          props.onReorderDepartment(zone.parentId, orderedIds);
        }
        return;
      }
      const intent = getOrgDragIntent(
        props.departments,
        props.agents,
        String(e.active.id),
        overId,
      );
      if (!intent) return;
      if (intent.kind === "agent") {
        props.onMoveAgent(intent.id, {
          departmentId: intent.departmentId,
          parentAgentId: intent.parentAgentId,
        });
        return;
      }
      props.onMoveDepartment(intent.id, intent.parentId);
    },
    [props],
  );
```

(e) 被拖节点声明 `data`。`DepartmentBanner` 的 `useDraggable`(L729)改为:

```ts
  const drag = useDraggable({
    id: dragId,
    data: {
      id: dept.id,
      kind: "dept",
      departmentId: 0,
      parentAgentId: 0,
      parentId: dept.parentId ?? 0,
    } satisfies ActiveDrag,
  });
```

`AgentCard` 的 `useDraggable`(L845-848)改为:

```ts
  const drag = useDraggable({
    id: dragId,
    disabled: agent.systemBadge === "DEFAULT",
    data: {
      id: agent.id,
      kind: "agent",
      departmentId: agent.departmentId ?? 0,
      parentAgentId: agent.parentAgentId ?? 0,
      parentId: 0,
    } satisfies ActiveDrag,
  });
```

(f) 渲染插入位。在 `OrgTree` 里 `layout` 之后加:

```tsx
  const insertZones = React.useMemo(
    () =>
      buildInsertZones(layout, {
        agents: props.agents,
        departments: props.departments,
        collapse: props.collapse,
      }),
    [layout, props.agents, props.departments, props.collapse],
  );
```

把 `<TreeCanvas all={props} layout={layout} />` 改成同时传 zones + active:

```tsx
            <TreeCanvas
              all={props}
              layout={layout}
              zones={insertZones}
              active={active}
            />
```

(g) `TreeCanvas`(L635-683)签名加 `zones: InsertZone[]; active: ActiveDrag | null`(解构出这两个新参数),在 `layout.nodes.map(...)` 那段之后、`org-tree-canvas` 闭合 `</div>` 之前追加插入位渲染:

```tsx
      {zones.map((zone) => (
        <InsertZoneView key={zone.id} zone={zone} active={active} />
      ))}
```

(h) 新增 `InsertZoneView` 组件(放在 `TreeCanvas` 下方):

```tsx
function InsertZoneView({
  zone,
  active,
}: {
  zone: InsertZone;
  active: ActiveDrag | null;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: zone.id, data: zone });
  const valid = active != null && isZoneValidTarget(zone, active);
  return (
    <div
      ref={setNodeRef}
      data-slot="org-insert-zone"
      className="absolute flex items-center justify-center"
      style={{
        left: zone.x - 10,
        top: zone.y,
        width: 20,
        height: zone.height,
        // 只有正在拖 + 合法目标时才吃事件,避免平时挡住节点点击
        pointerEvents: active && valid ? "auto" : "none",
      }}
    >
      {valid && isOver && (
        <span
          className="w-0.5 rounded-full bg-primary"
          style={{ height: zone.height }}
        />
      )}
    </div>
  );
}
```

> `useDroppable` 已在文件顶部 import(L10)。`active && valid ? "auto" : "none"` 保证非拖拽态/非法组时插入位不拦截节点的 click/hover。

- [ ] **Step 6: org-chart-page 接线**

`org-chart-page.tsx` 从 `useOrgData()`(L43-60)解构里加 `reorderAgents, reorderDepartments`。给 `<OrgTree>`(L271-297)补两个回调:

```tsx
              onReorderAgent={(departmentId, parentAgentId, orderedIds) => {
                void reorderAgents(departmentId, parentAgentId, orderedIds);
              }}
              onReorderDepartment={(parentId, orderedIds) => {
                void reorderDepartments(parentId, orderedIds);
              }}
```

- [ ] **Step 7: 跑测试 + lint + tsc**

Run: `cd frontend && pnpm test -- src/components/agentre/org/`
Expected: PASS。
Run: `make lint`(根)
Expected: 无新增错误(尤其 tsc 类型:`ActiveDrag` 的 `satisfies`、`TreeCanvas` 新 props)。

- [ ] **Step 8: 提交**

```bash
git add frontend/src/components/agentre/org/tree-insert-zones.ts frontend/src/components/agentre/org/__tests__/tree-insert-zones.test.ts frontend/src/components/agentre/org/org-tree.tsx frontend/src/components/agentre/org-chart-page.tsx
git commit frontend/src/components/agentre/org/tree-insert-zones.ts frontend/src/components/agentre/org/__tests__/tree-insert-zones.test.ts frontend/src/components/agentre/org/org-tree.tsx frontend/src/components/agentre/org-chart-page.tsx -m "✨ org-tree: 插入位 drop-zone 拖拽排序 + 乐观落库"
```

---

## Phase 4 — 前端:列表

### Task 8: 列表 hierarchy 模式拖拽排序

**Files:**
- Create: `frontend/src/components/agentre/org/__tests__/org-list-reorder.test.ts`
- Modify: `frontend/src/components/agentre/org/reorder.ts`(加 `bucketByPlacement`)
- Modify: `frontend/src/components/agentre/org/org-list.tsx`(SortableContext + 行 + onDragEnd)
- Modify: `frontend/src/components/agentre/org-chart-page.tsx`(给 `<OrgList>` 传 `onReorderAgent`)
- Modify: `frontend/src/i18n/locales/zh-CN/common.json` + `frontend/src/i18n/locales/en/common.json`(reorder 失败文案)

**Interfaces:**
- Consumes: `useOrgData().reorderAgents`(Task 6);`buildReportToMap`(`./reporting`,已被 org-list 使用);`useOrgData().error` 走既有 error 通道展示。
- Produces:
  - `bucketByPlacement(agentById: Map<number, OrgAgent>, orderedIds: number[]): Array<{ departmentId: number; parentAgentId: number; orderedIds: number[] }>` —— 把重排后的一串 id 按原始 `(departmentId, parentAgentId)` 分桶、保持相对序。
  - `OrgList` 新增 prop `onReorderAgent(departmentId, parentAgentId, orderedIds)`。

- [ ] **Step 1: 写失败测试**

新建 `frontend/src/components/agentre/org/__tests__/org-list-reorder.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { bucketByPlacement } from "../reorder";
import type { OrgAgent } from "../types";

describe("bucketByPlacement", () => {
  it("按原始 placement 分桶并保持相对序", () => {
    const byId = new Map<number, OrgAgent>([
      [1, { id: 1, departmentId: 2, parentAgentId: 0 } as OrgAgent],
      [2, { id: 2, departmentId: 2, parentAgentId: 0 } as OrgAgent],
      [3, { id: 3, departmentId: 0, parentAgentId: 5 } as OrgAgent],
    ]);
    const buckets = bucketByPlacement(byId, [2, 3, 1]);
    const dept2 = buckets.find((b) => b.departmentId === 2)!;
    const parent5 = buckets.find((b) => b.parentAgentId === 5)!;
    expect(dept2.orderedIds).toEqual([2, 1]); // 相对序:2 在 1 前
    expect(parent5.orderedIds).toEqual([3]);
  });
});
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-list-reorder.test.ts`
Expected: FAIL —— `bucketByPlacement` 未导出。

- [ ] **Step 3: 实现 bucketByPlacement**

`frontend/src/components/agentre/org/reorder.ts` 末尾追加:

```ts
export function bucketByPlacement(
  agentById: Map<number, OrgAgent>,
  orderedIds: number[],
): Array<{ departmentId: number; parentAgentId: number; orderedIds: number[] }> {
  const buckets = new Map<
    string,
    { departmentId: number; parentAgentId: number; orderedIds: number[] }
  >();
  for (const id of orderedIds) {
    const a = agentById.get(id);
    if (!a) continue;
    const departmentId = a.departmentId ?? 0;
    const parentAgentId = a.parentAgentId ?? 0;
    const key = `${departmentId}:${parentAgentId}`;
    if (!buckets.has(key)) {
      buckets.set(key, { departmentId, parentAgentId, orderedIds: [] });
    }
    buckets.get(key)!.orderedIds.push(id);
  }
  return [...buckets.values()];
}
```

- [ ] **Step 4: 跑测试看它通过**

Run: `cd frontend && pnpm test -- src/components/agentre/org/__tests__/org-list-reorder.test.ts`
Expected: PASS。

- [ ] **Step 5: 列表接 useSortable + onDragEnd**

`org-list.tsx`:

(a) import 补(参考 `tab-strip.tsx` L6-19):

```ts
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";

import { bucketByPlacement } from "./reorder";
```

(b) `OrgListProps`(L28-34)加:

```ts
  onReorderAgent: (
    departmentId: number,
    parentAgentId: number,
    orderedIds: number[],
  ) => void;
```

(c) `OrgList` 组件内(`rows` useMemo 之后)加 sensors + onDragEnd:

```tsx
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const dragEnabled = sortKey === "hierarchy";

  const handleDragEnd = React.useCallback(
    (e: DragEndEvent) => {
      if (!dragEnabled || !e.over || e.active.id === e.over.id) return;
      const activeId = Number(String(e.active.id));
      const overId = Number(String(e.over.id));
      const ap = effectiveParent.get(activeId) ?? 0;
      const op = effectiveParent.get(overId) ?? 0;
      if (ap !== op) return; // 只在同一有效父级内排序
      const blockIds = rows
        .map((r) => r.agent.id)
        .filter((id) => (effectiveParent.get(id) ?? 0) === ap);
      const from = blockIds.indexOf(activeId);
      const to = blockIds.indexOf(overId);
      if (from < 0 || to < 0) return;
      const reordered = blockIds.slice();
      reordered.splice(to, 0, reordered.splice(from, 1)[0]);
      for (const bucket of bucketByPlacement(agentById, reordered)) {
        onReorderAgent(bucket.departmentId, bucket.parentAgentId, bucket.orderedIds);
      }
    },
    [dragEnabled, effectiveParent, rows, agentById, onReorderAgent],
  );
```

(d) 把列表 body(L216-241 的 `<div data-slot="org-list-body">…</div>`)包进 DndContext + SortableContext,并把 `ListRow` 换成 `SortableListRow`(下一步):

```tsx
        <div className="flex-1 overflow-y-auto" data-slot="org-list-body">
          {rows.length === 0 ? (
            <div className="flex h-full items-center justify-center p-8 text-sm text-muted-foreground">
              {t("org.list.empty")}
            </div>
          ) : (
            <DndContext sensors={sensors} onDragEnd={handleDragEnd}>
              <SortableContext
                items={rows.map((r) => String(r.agent.id))}
                strategy={verticalListSortingStrategy}
              >
                {rows.map((row) => {
                  const pid = effectiveParent.get(row.agent.id) ?? 0;
                  const parentAgent =
                    pid !== 0 ? (agentById.get(pid) ?? null) : null;
                  return (
                    <SortableListRow
                      key={row.agent.id}
                      row={row}
                      dragEnabled={dragEnabled}
                      isSelected={
                        selected?.kind === "agent" &&
                        selected.id === row.agent.id
                      }
                      parentAgent={parentAgent}
                      showIndentConnector={
                        sortKey === "hierarchy" && row.depth > 0
                      }
                      onClick={() =>
                        onSelect({ kind: "agent", id: row.agent.id })
                      }
                    />
                  );
                })}
              </SortableContext>
            </DndContext>
          )}
        </div>
```

(e) 新增 `SortableListRow` 包一层 `useSortable`,把 dnd 的 ref/listeners/style 透传给现有 `ListRow`。为此给 `ListRow` 加一个可选 `drag` prop 并应用到根 `<button>`:

`ListRowProps`(L451-457)加:

```ts
  drag?: {
    setNodeRef: (node: HTMLElement | null) => void;
    listeners: React.HTMLAttributes<HTMLElement>;
    style: React.CSSProperties;
    isDragging: boolean;
  };
```

`ListRow` 的根 `<button>`(L486-501)接上 drag:

```tsx
    <button
      ref={drag?.setNodeRef}
      style={drag?.style}
      {...(drag?.listeners ?? {})}
      type="button"
      role="option"
      aria-selected={isSelected}
      onClick={onClick}
      data-slot="org-list-row"
      data-agent-id={agent.id}
      className={cn(
        "flex w-full items-center gap-3 border-b border-l-[3px] border-border/60 py-2.5 pl-[17px] pr-5 text-left transition-colors",
        "hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40",
        isSelected ? "border-l-primary bg-primary-soft" : "border-l-transparent",
        drag?.isDragging && "opacity-50",
      )}
    >
```

新增组件(放 `ListRow` 上方):

```tsx
function SortableListRow({
  dragEnabled,
  ...rowProps
}: ListRowProps & { dragEnabled: boolean }) {
  const { setNodeRef, listeners, transform, transition, isDragging } =
    useSortable({ id: String(rowProps.row.agent.id), disabled: !dragEnabled });
  const style: React.CSSProperties = {
    transform: transform
      ? `translate3d(0, ${transform.y}px, 0)`
      : undefined,
    transition,
  };
  return (
    <ListRow
      {...rowProps}
      drag={
        dragEnabled
          ? {
              setNodeRef,
              listeners: listeners ?? {},
              style,
              isDragging,
            }
          : undefined
      }
    />
  );
}
```

> 只用 `transform.y`(垂直列表)。name 模式 `dragEnabled=false` → `useSortable disabled`,行为回退到纯只读。

- [ ] **Step 6: org-chart-page 给 OrgList 传回调**

`org-chart-page.tsx` 的 `<OrgList>`(L299-306)补:

```tsx
            <OrgList
              departments={departments}
              agents={agents}
              backends={backends}
              selected={view.selected}
              onSelect={view.setSelected}
              onReorderAgent={(departmentId, parentAgentId, orderedIds) => {
                void reorderAgents(departmentId, parentAgentId, orderedIds);
              }}
            />
```

- [ ] **Step 7: i18n(即使当前不弹 toast 也预置失败文案键,供后续 error 展示)**

`frontend/src/i18n/locales/zh-CN/common.json` 的 `org` 对象内加:

```json
"reorderFailed": "排序保存失败,请重试",
```

`frontend/src/i18n/locales/en/common.json` 的 `org` 对象内加:

```json
"reorderFailed": "Failed to save order, please retry",
```

> 打开两个 JSON 确认 `org` 命名空间的确切嵌套位置再插入,保持 JSON 合法(逗号)。`org-list`/`org-chart-page` 的报错沿用 `useOrgData().error`(已有渲染)。此键为后续把 reorder 失败单独提示预留,先加进来让 i18n 覆盖测试通过。

- [ ] **Step 8: 跑测试 + lint**

Run: `cd frontend && pnpm test -- src/components/agentre/org/`
Expected: PASS。
Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS(两个 locale 都有新键)。
Run: `make lint`
Expected: 无新增错误。

- [ ] **Step 9: 提交**

```bash
git add frontend/src/components/agentre/org/reorder.ts frontend/src/components/agentre/org/__tests__/org-list-reorder.test.ts frontend/src/components/agentre/org/org-list.tsx frontend/src/components/agentre/org-chart-page.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit frontend/src/components/agentre/org/reorder.ts frontend/src/components/agentre/org/__tests__/org-list-reorder.test.ts frontend/src/components/agentre/org/org-list.tsx frontend/src/components/agentre/org-chart-page.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json -m "✨ org-list: hierarchy 模式拖拽排序(分桶持久化 + 乐观)"
```

---

## 收尾:全量 gate

- [ ] **后端全量**:Run `make test-backend` → 全绿。
- [ ] **前端全量 + 类型 + lint**:Run `make lint`(会先 generate)→ 无错;Run `cd frontend && pnpm test`(**全量**,别 focused)→ 看真 exit code 全绿。
- [ ] **真机手验点**(GUI,非自动化):
  - 树:拖 agent/department 到同级空档出现高亮插入线并排序;拖到节点身上仍 re-parent;拖拽跟手不卡;缩放态下插入线位置准确。
  - 列表:hierarchy 模式可拖行排序、name 模式不可拖;排序后切树/切回列表顺序一致。

---

## Self-Review 记录(作者自查)

- **Spec 覆盖**:§1 后端 = Task 1-4;§2.1 排序 = Task 5;§2.3 手感 = Task 5;§2.4 乐观 = Task 6 + Task 7;§2.2 插入位 = Task 7;§3 列表 = Task 8;§4 i18n = Task 8 Step 7。全部有对应任务。
- **类型一致**:`reorderAgents(departmentId, parentAgentId, orderedIds)` / `reorderDepartments(parentId, orderedIds)`(Task 6)在 Task 7/8 的调用签名一致;`ActiveDrag` / `InsertZone` 在 tree-insert-zones.ts 定义、org-tree.tsx 消费,字段名一致;Go json tag `departmentId`/`parentAgentId`/`orderedIds`/`parentId`(Task 2/4)与前端调用对象键一致。
- **已知取舍**:列表 effectiveParent 块跨多个 raw placement 组时,分桶后某桶若不是该 raw 组的**完整集合**,svc 全集校验会拒绝 → 走 error 通道 + reload 回滚(安全失败,不损坏数据)。见 spec §3 边界,已接受。
- **待实现时确认**(非阻塞):`department_test.go` 的 setup helper 真实签名(Task 4 Step 1 已标注);`internal/app/department.go` 的 accessor 是否 `Default()`(Task 4 Step 5 已标注);两个 `common.json` 的 `org` 命名空间确切嵌套(Task 8 Step 7 已标注)。
