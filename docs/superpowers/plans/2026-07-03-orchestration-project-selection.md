# 编排新建时可选项目 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新建编排 Run 时可选一个项目；选中后该 Run 的所有 agent（Leader + 派发子 agent + ask 新建会话）以该项目目录为工作目录。

**Architecture:** 后端补两处 `EnsureOrchSession` 调用的 `ProjectID` 传播（`orch_svc` 从 `runs` 仓库查 Run 的 `ProjectID`，`chat_svc` 保持项目无关）；前端在新建对话框加一个可选「项目」下拉，替换硬编码的 `projectId: 0`。cwd 解析链路（`project_svc.ResolveSessionCwd`）已就绪，无需改动。

**Tech Stack:** Go 1.26 + cago + goconvey + gomock；React 19 + TypeScript + Wails 绑定 + shadcn `@/components/ui/*` + react-i18next + Vitest。

## Global Constraints

- 严格 TDD：先写会失败的测试，运行看红（且红对原因），再实现。
- 仓库单测用 gomock 注 mock，**不连真库**；service 单测不碰 DB。
- 新增前端可见文案必须走 i18n（`t(...)` + `zh-CN` 和 `en` 两个 `common.json`），禁止硬编码中文；表单控件用 shadcn `@/components/ui/*`，禁止原生 `<select>`。
- 不改与本任务无关的文件；不夹带顺手重构 / 格式化 / import 重排。
- 分支 `develop/wyz` 有并发会话共享 index → **提交必须带 pathspec**（`git commit <files> -m ...`，不要裸 `git commit`）。
- gitmoji 提交信息。
- 无 DB 迁移（`orchestration_runs.project_id`、`chat_sessions.project_id` 均已存在）。

关键既有事实（实现时依赖）：
- `orchSvc` 结构体含 `runs orch_repo.RunRepo`（`internal/service/orch_svc/orch.go:33`）；`RunRepo.Find(ctx, id)` 返回 `*orch_entity.OrchestrationRun`，其含 `ProjectID int64`。
- `EnsureOrchSessionInput`（`internal/service/orch_svc/deps.go`）已有 `ProjectID int64` 字段；`orchChatAdapter.EnsureOrchSession`（`internal/app/orch_adapter.go:40`）已把它透传给 `chat_svc`。
- `RunCreate` 前端绑定（`RunCreateRequest.projectId`）与 `CreateRun`/根会话已支持 `ProjectID`——本计划**不动** create 路径。

---

### Task 1: 后端——派发子会话继承 Run 的 ProjectID

**Files:**
- Modify: `internal/service/orch_svc/dispatch.go`（`Dispatch`，第 36-44 行建子会话处）
- Create（新增私有方法，放在 `dispatch.go` 末尾即可）: `runProjectID`
- Test: `internal/service/orch_svc/dispatch_test.go`（`TestDispatch_SpawnsChildSessionAndTask`、`TestDispatch_EnsureOrchSessionError`）
- Test: `internal/service/orch_svc/event_test.go`（`TestDispatch_EmitsRunUpdated`）

**Interfaces:**
- Produces: `func (s *orchSvc) runProjectID(ctx context.Context, runID int64) (int64, error)` —— Run 不存在返回 `(0, nil)`；`s.runs.Find` 出错则透传 error。Task 2 复用它。

- [ ] **Step 1: 改红派发 happy-path 测试——断言子会话带上 Run 的 ProjectID**

编辑 `internal/service/orch_svc/dispatch_test.go` 的 `TestDispatch_SpawnsChildSessionAndTask`：在 `CountByRunAgent` 那条 EXPECT 之后新增一条 `runs.Find` 期望，并在 `EnsureOrchSession` 的 `DoAndReturn` 里断言 `in.ProjectID`。

```go
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(100), int64(3)).Return(int64(0), nil)
	// Run 100 挂在项目 77 → 子会话应继承 ProjectID=77。
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(
		&orch_entity.OrchestrationRun{ID: 100, ProjectID: 77}, nil)
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, in orch_svc.EnsureOrchSessionInput) (int64, error) {
		So(in.ParentSessionID, ShouldEqual, 500)
		So(in.RunID, ShouldEqual, 100)
		So(in.AgentID, ShouldEqual, 3)
		So(in.Isolate, ShouldBeTrue)
		So(in.ProjectID, ShouldEqual, 77) // 新断言
		So(in.Title, ShouldEqual, "实现登录表单")
		return 600, nil
	})
```

- [ ] **Step 2: 运行看红**

Run: `go test -race -run TestDispatch_SpawnsChildSessionAndTask ./internal/service/orch_svc/`
Expected: FAIL —— `runs.Find` 期望未被调用（`missing call(s) to ...Find`），且 `in.ProjectID` 断言为 `0 != 77`。

- [ ] **Step 3: 实现 runProjectID + 接进 Dispatch**

在 `internal/service/orch_svc/dispatch.go` 末尾新增：

```go
// runProjectID 返回 Run 挂载的 project id；Run 不存在时返回 0（走自由会话兜底）。
// 派发子会话 / ask 新建会话据此继承 Run 的工作目录（project.Path）。
func (s *orchSvc) runProjectID(ctx context.Context, runID int64) (int64, error) {
	run, err := s.runs.Find(ctx, runID)
	if err != nil {
		return 0, err
	}
	if run == nil {
		return 0, nil
	}
	return run.ProjectID, nil
}
```

在 `Dispatch` 里，`CountByRunAgent` 之后、`EnsureOrchSession` 之前插入查询，并把结果填进入参：

```go
	n, err := s.tasks.CountByRunAgent(ctx, parent.RunID, target.ID)
	if err != nil {
		return 0, err
	}

	projectID, err := s.runProjectID(ctx, parent.RunID)
	if err != nil {
		return 0, err
	}

	childSession, err := s.chat.EnsureOrchSession(ctx, EnsureOrchSessionInput{
		AgentID:         target.ID,
		ParentSessionID: parentSessionID,
		RunID:           parent.RunID,
		ProjectID:       projectID,
		Isolate:         isolate,
	})
```

- [ ] **Step 4: 运行看绿（happy-path）**

Run: `go test -race -run TestDispatch_SpawnsChildSessionAndTask ./internal/service/orch_svc/`
Expected: PASS

- [ ] **Step 5: 修因新增 runs.Find 而破的同域测试**

改动让 `Dispatch` 多调一次 `s.runs.Find`，凡是走到 `EnsureOrchSession` 的派发测试都要补 `runs.Find` 期望。

`dispatch_test.go` 的 `TestDispatch_EnsureOrchSessionError`（该用例 `runs` mock 已在，仅补期望），在 `CountByRunAgent` 之后加：

```go
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(400), int64(3)).Return(int64(0), nil)
	runs.EXPECT().Find(gomock.Any(), int64(400)).Return(&orch_entity.OrchestrationRun{ID: 400}, nil)
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).Return(int64(0), boomEnsure)
```

（`TestDispatch_NilParent` / `TestDispatch_NilAgent` / `TestDispatch_CountByRunAgentError` 在到达 `runProjectID` 前就返回，无需改动。）

`event_test.go` 的 `TestDispatch_EmitsRunUpdated` 当前用 `runs=nil` 注册，会 nil panic——改为注入 `runs` mock 并补期望：

```go
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, emit)
```

并在 `CountByRunAgent` 之后加：

```go
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(100), int64(3)).Return(int64(0), nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100}, nil)
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).Return(int64(600), nil)
```

- [ ] **Step 6: 运行整个 dispatch/event 相关测试看绿**

Run: `go test -race -run 'TestDispatch' ./internal/service/orch_svc/`
Expected: PASS（含 `TestDispatch_SpawnsChildSessionAndTask`、`TestDispatch_EnsureOrchSessionError`、`TestDispatch_EmitsRunUpdated` 等）

- [ ] **Step 7: 提交**

```bash
git add internal/service/orch_svc/dispatch.go internal/service/orch_svc/dispatch_test.go internal/service/orch_svc/event_test.go
git commit internal/service/orch_svc/dispatch.go internal/service/orch_svc/dispatch_test.go internal/service/orch_svc/event_test.go -m "✨ orchestration: 派发子会话继承 Run 的 ProjectID(工作目录)"
```

---

### Task 2: 后端——ask 新建会话继承 Run 的 ProjectID

**Files:**
- Modify: `internal/service/orch_svc/ask.go`（`resolveOrCreateAgentSession`，第 148-149 行新建会话分支）
- Test: `internal/service/orch_svc/ask_test.go`（`TestAsk_CreatesSessionWithQuestionTitle`）

**Interfaces:**
- Consumes: `s.runProjectID(ctx, runID)`（Task 1 产出）。

- [ ] **Step 1: 改红 ask 新建会话测试——注入 runs mock 并断言 ProjectID**

编辑 `internal/service/orch_svc/ask_test.go` 的 `TestAsk_CreatesSessionWithQuestionTitle`：把 `runs=nil` 换成真 mock，加 `runs.Find` 期望，并在 `EnsureOrchSession` 的 `DoAndReturn` 里断言 `in.ProjectID`。

```go
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, AgentID: 2, SessionID: 500}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "王").Return(&agent_entity.Agent{ID: 1, Name: "王"}, nil)
	agents.EXPECT().Find(gomock.Any(), int64(2)).Return(&agent_entity.Agent{ID: 2, Name: "李"}, nil).AnyTimes()
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{{ID: 9, AgentID: 2, SessionID: 500, Status: orch_entity.TaskRunning}}, nil).AnyTimes()
	// Run 100 挂项目 55 → ask 新建的会话应继承 ProjectID=55。
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, ProjectID: 55}, nil)
	// EnsureOrchSession 的 DoAndReturn 跑在 Ask 的 `go func` goroutine 里(非 Convey 上下文)，
	// 直接 So(...) 会 panic「called outside Convey」。故用普通变量捕获，在 Convey 块内断言，
	// 参照 event_test.go 的 capturedRunID 写法。
	titleCh := make(chan string, 1)
	var capturedProjectID int64
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, in orch_svc.EnsureOrchSessionInput) (int64, error) {
		capturedProjectID = in.ProjectID
		titleCh <- in.Title
		return 800, nil
	})
```

并在该用例的 `Convey` 块内、拿到 `<-titleCh` 之后加断言：

```go
		So(<-titleCh, ShouldEqual, "鉴权用什么?")
		So(capturedProjectID, ShouldEqual, 55) // 新断言
```

需要 `mock_orch_repo` 已在该文件 import（`ask_test.go` 顶部已 import，Step 前确认）。若未 import，加：

```go
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
```

- [ ] **Step 2: 运行看红**

Run: `go test -race -run TestAsk_CreatesSessionWithQuestionTitle ./internal/service/orch_svc/`
Expected: FAIL —— 当前 `resolveOrCreateAgentSession` 未查 Run，`runs.Find` 期望未命中，且 `in.ProjectID` 为 `0 != 55`。

- [ ] **Step 3: 接进 resolveOrCreateAgentSession**

编辑 `internal/service/orch_svc/ask.go` 第 148-149 行的新建会话分支：

```go
	// 该 agent 在本 Run 还没有会话 → 建一条（无前置任务上下文，只能据 persona + 问题答）。
	projectID, err := s.runProjectID(ctx, runID)
	if err != nil {
		return 0, err
	}
	return s.chat.EnsureOrchSession(ctx, EnsureOrchSessionInput{AgentID: agentID, RunID: runID, ProjectID: projectID})
```

- [ ] **Step 4: 运行看绿**

Run: `go test -race -run TestAsk_CreatesSessionWithQuestionTitle ./internal/service/orch_svc/`
Expected: PASS

- [ ] **Step 5: 跑整包，确认无回归**

Run: `go test -race ./internal/service/orch_svc/`
Expected: PASS（`ask_test.go` 其余用例走「活会话」分支不建新会话，不触发 `runs.Find`，故不受影响）。

- [ ] **Step 6: 提交**

```bash
git add internal/service/orch_svc/ask.go internal/service/orch_svc/ask_test.go
git commit internal/service/orch_svc/ask.go internal/service/orch_svc/ask_test.go -m "✨ orchestration: ask 新建会话继承 Run 的 ProjectID(工作目录)"
```

---

### Task 3: 前端——新建编排对话框加可选「项目」下拉

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/run-new-dialog.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`（`orchestration.new` 段）
- Modify: `frontend/src/i18n/locales/en/common.json`（`orchestration.new` 段）
- Test: `frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`

**Interfaces:**
- Consumes: 后端 `RunCreate({..., projectId})`（已存在绑定）；`ProjectListTree(): Promise<app.ProjectTreeNode[]>`（已存在绑定）。`app.ProjectTreeNode = { project?: {id,name,...}; children: ProjectTreeNode[] }`。

- [ ] **Step 1: 加 i18n key（两个 locale）**

`frontend/src/i18n/locales/zh-CN/common.json` 的 `orchestration.new` 段加：

```json
    "project": "项目",
    "projectNone": "无（使用 Agent 默认目录）",
```

`frontend/src/i18n/locales/en/common.json` 的 `orchestration.new` 段加：

```json
    "project": "Project",
    "projectNone": "None (agent default directory)",
```

- [ ] **Step 2: 写会失败的组件测试**

编辑 `frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`：

在 `vi.hoisted` 的 `appMocks` 加 `ProjectListTree: vi.fn()`：

```ts
const appMocks = vi.hoisted(() => ({
  RunCreate: vi.fn(),
  ListChatAgents: vi.fn(),
  WorkflowList: vi.fn(),
  ProjectListTree: vi.fn(),
}));
```

在 `beforeEach` 里给它一个默认返回：

```ts
    appMocks.ProjectListTree.mockResolvedValue([
      { project: { id: 5, name: "我的项目" }, children: [] },
    ]);
```

新增一个 `describe`：

```ts
  describe("项目选择", () => {
    it("默认不选项目 → RunCreate 带 projectId: 0", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      renderDialog();
      fireEvent.change(await screen.findByTestId("run-goal"), {
        target: { value: "做登录页" },
      });
      await user.click(screen.getByTestId("run-leader"));
      await user.click(await screen.findByRole("option", { name: "架构师" }));
      fireEvent.click(screen.getByTestId("run-create"));
      await waitFor(() =>
        expect(appMocks.RunCreate).toHaveBeenCalledWith(
          expect.objectContaining({ projectId: 0 }),
        ),
      );
    });

    it("选中项目 → RunCreate 带该 projectId", async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      renderDialog();
      await waitFor(() => expect(appMocks.ProjectListTree).toHaveBeenCalled());
      fireEvent.change(await screen.findByTestId("run-goal"), {
        target: { value: "做登录页" },
      });
      await user.click(screen.getByTestId("run-leader"));
      await user.click(await screen.findByRole("option", { name: "架构师" }));
      await user.click(screen.getByTestId("run-project"));
      await user.click(await screen.findByRole("option", { name: /我的项目/ }));
      fireEvent.click(screen.getByTestId("run-create"));
      await waitFor(() =>
        expect(appMocks.RunCreate).toHaveBeenCalledWith(
          expect.objectContaining({ projectId: 5 }),
        ),
      );
    });
  });
```

- [ ] **Step 3: 运行看红**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`
Expected: FAIL —— 找不到 `run-project` testid（控件尚不存在）；且默认用例断言 `projectId: 0` 也会因 `RunCreate` 未被以含 `projectId` 的对象调用而…（实际当前代码就传 `projectId: 0`，故「默认」用例可能已 PASS；关键红点是「选中项目」用例——`run-project` 不存在）。

- [ ] **Step 4: 实现下拉控件**

编辑 `frontend/src/components/agentre/orchestration/run-new-dialog.tsx`：

4a. import 处加 `ProjectListTree`，并 import 模型类型：

```tsx
import {
  ListChatAgents,
  ProjectListTree,
  RunCreate,
  WorkflowList,
} from "../../../../wailsjs/go/app/App";
import { app } from "../../../../wailsjs/go/models";
```

4b. 在文件内（组件外，紧挨 `WorkflowOption` 类型附近）加拍平工具与类型：

```tsx
type FlatProject = { id: number; name: string; depth: number };

// flattenTree 把项目树拍平成 [{id,name,depth}] 供下拉用(与 project-new-dialog 同款，
// depth 决定缩进)。就地复刻以避免改动无关文件。
function flattenTree(nodes: app.ProjectTreeNode[], depth = 0): FlatProject[] {
  const out: FlatProject[] = [];
  for (const n of nodes) {
    if (!n.project) continue;
    out.push({ id: n.project.id, name: n.project.name, depth });
    if (n.children) out.push(...flattenTree(n.children, depth + 1));
  }
  return out;
}
```

4c. 组件内加 state（挨着 `flowContent` 等 state）：

```tsx
  const [projects, setProjects] = React.useState<FlatProject[]>([]);
  const [projectId, setProjectId] = React.useState(0);
```

4d. 在打开时的 `useEffect` 里：重置项加 `setProjectId(0);`，并加载项目树（放在 `WorkflowList()` 之后）：

```tsx
    setFlowContent("");
    setAllowedAgentIds([]);
    setProjectId(0);
    setError(null);
```

```tsx
    ProjectListTree()
      .then((tree) => setProjects(flattenTree(tree ?? [])))
      .catch(() => setProjects([]));
```

4e. `submit()` 里把硬编码 `projectId: 0` 换成 state：

```tsx
      const d = await RunCreate({
        goal: goal.trim(),
        leaderAgentId: leaderId,
        flowId: flowMode === "library" ? flowId : 0,
        flowContent: flowMode === "adhoc" ? flowContent : "",
        projectId,
        allowedAgentIds,
      });
```

4f. 在「起始流程」区块之后（临时流程 `flowMode === "adhoc"` 块的闭合 `) : null}` 之后、「可参与 agent」块之前）插入项目下拉：

```tsx
          {/* 项目: 可选; 选中后该 Run 全部 agent 以项目目录为工作目录 */}
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("orchestration.new.project")}
            </span>
            <Select
              value={String(projectId)}
              onValueChange={(v) => setProjectId(Number(v))}
            >
              <SelectTrigger
                data-testid="run-project"
                aria-label={t("orchestration.new.project")}
                className="h-9 text-xs"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">
                  {t("orchestration.new.projectNone")}
                </SelectItem>
                {projects.map((p) => (
                  <SelectItem key={p.id} value={String(p.id)}>
                    <span style={{ paddingInlineStart: `${p.depth * 12}px` }}>
                      {p.name}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
```

- [ ] **Step 5: 运行看绿**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`
Expected: PASS（含新增「项目选择」两条用例）

- [ ] **Step 6: 跑 i18n 覆盖测试**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS（`orchestration.new.project` / `projectNone` 在中英两侧均覆盖）

- [ ] **Step 7: 提交**

```bash
git add frontend/src/components/agentre/orchestration/run-new-dialog.tsx frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit frontend/src/components/agentre/orchestration/run-new-dialog.tsx frontend/src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json -m "✨ orchestration: 新建编排可选项目(下拉+i18n)"
```

---

### Task 4: 收尾门禁

- [ ] **Step 1: 后端全量**

Run: `make test-backend`
Expected: PASS（关注 `internal/service/orch_svc` 与 `internal/app`）。

- [ ] **Step 2: 前端全量 + 类型/lint**

Run: `cd frontend && pnpm test`
Expected: PASS

Run: `make lint`
Expected: PASS（含 `i18next/no-literal-string`、tsc、eslint、gofmt/goimports）。注意看**真 exit code**，别被 `| tail` 吞掉。

- [ ] **Step 3（可选，若已跑 dev/app）: 冒烟**

新建一个选了项目的编排 Run，确认 Leader 与被派发子 agent 的工作目录均为项目 `Path`（可查日志中 `RunRequest.Cwd` 或 agent 落地目录）。此为手验，非自动化门禁。

## 计划自检

- **Spec 覆盖**：范围决策（全部 agent / 标签「项目」/ 放流程后）→ Task 1+2（全部 agent 继承）、Task 3（标签「项目」、位置在 flow 之后）。改动一（后端传播）→ Task 1（dispatch）+ Task 2（ask）。改动二（前端下拉）→ Task 3。测试与门禁 → Task 4。无遗漏。
- **占位符**：无 TBD/TODO；每个代码步给了完整代码。
- **类型一致**：`runProjectID(ctx, runID) (int64, error)` 在 Task 1 定义、Task 2 消费，签名一致；`FlatProject` / `flattenTree` 前端自洽；`EnsureOrchSessionInput.ProjectID` 为既有字段。
- **边界**：Run 不存在 → `runProjectID` 返回 0（自由会话兜底）；前端默认 `projectId=0`（「无」项）。
