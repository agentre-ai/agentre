# Chat Context Sidebar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 ChatPanel 右侧加一个可切换、可收起的上下文侧栏：MVP 提供 Outline（消息概览）+ Files 两个视图 tab，顶部 chip 区显示当前会话的 git 分支 / worktree / dirty / ahead·behind。

**Architecture:** 前端在 `chat-panel.tsx` 内把 transcript 包进新的横向 MainRow，旁边塞 `<ChatContextSidebar />`（composer 不被挤）。Outline 与 Files 都在前端 derive，零 schema 变动。Git 状态走新增的 `GetSessionGitState` Wails RPC（本地直接 `os/exec git`；远端 backend MVP 阶段返回 `notARepo=true`，daemon handler 留 follow-up）。Sidebar toggle + activeTab 用 zustand `persist` 中间件落 localStorage 全局共享。

**Tech Stack:** Go 1.26 + cago framework（goconvey 测试）、Wails v2、React 19 + TypeScript、Vitest、zustand、shadcn/ui、lucide-react。

**Spec:** `docs/superpowers/specs/2026-05-27-chat-context-sidebar-design.md`

**Mockup:** `agentry.pen` → 顶层 frame `Agent Chat — With Right Sidebar` (id `MT2Dq`)

---

## File Structure

**Backend (Go):**
- `internal/service/chat_svc/git_state.go` — 新增。`runGitState(ctx, cwd)` helper + `(s *chatSvc) GetSessionGitState(ctx, req)` 服务方法。
- `internal/service/chat_svc/git_state_test.go` — 新增。本地 `t.TempDir()` 建 git repo + goconvey 单测。
- `internal/service/chat_svc/types.go:386` 附近 — 新增 `GetSessionGitStateRequest`、`ChatSessionGitState` 两个 shape。
- `internal/service/chat_svc/chat.go:59` 的 `ChatSvc` 接口 — 加 `GetSessionGitState` 一行。
- `internal/pkg/code/code.go` — 在 ChatLaunchCommand 段后加 `ChatGitStateUnavailable` 段。
- `internal/pkg/code/en.go` + `zh_cn.go` — 加对应文案。
- `internal/app/chat.go` — 加 `(a *App) GetSessionGitState(req) (*resp, error)` Wails 绑定。

**Frontend (TS/React):**
- `frontend/src/stores/chat-sidebar-store.ts` — 新增。zustand store + persist 中间件。
- `frontend/src/stores/__tests__/chat-sidebar-store.test.ts` — 新增。
- `frontend/src/hooks/use-chat-git-state.ts` — 新增。RPC 拉取 + doneTick 触发 refresh。
- `frontend/src/hooks/__tests__/use-chat-git-state.test.tsx` — 新增。
- `frontend/src/components/agentre/chat-context-sidebar/derive.ts` — 新增。outline / files 派生纯函数 + tool-file-extractor。
- `frontend/src/components/agentre/chat-context-sidebar/__tests__/derive.test.ts` — 新增。
- `frontend/src/components/agentre/chat-context-sidebar/context-chip-bar.tsx` — 新增。chip 渲染。
- `frontend/src/components/agentre/chat-context-sidebar/tab-bar.tsx` — 新增。Outline / Files 切换。
- `frontend/src/components/agentre/chat-context-sidebar/views/outline-view.tsx` — 新增。
- `frontend/src/components/agentre/chat-context-sidebar/views/files-view.tsx` — 新增。
- `frontend/src/components/agentre/chat-context-sidebar/index.tsx` — 新增。Sidebar 容器。
- `frontend/src/components/agentre/chat-context-sidebar/__tests__/*.test.tsx` — 新增。每个视图 / chip-bar 各一个测试文件。
- `frontend/src/components/agentre/chat-panel.tsx` — 修改。Toolbar 加 toggle 按钮、ChatArea 包 MainRow、挂载 Sidebar。
- `frontend/src/components/agentre/__tests__/chat-panel.test.tsx` — 修改。加 toggle + sidebar 渲染断言。

---

## Task 1: 加错误码 `ChatGitStateUnavailable`

**Files:**
- Modify: `internal/pkg/code/code.go:155`（在 `ChatPlanActionUnknown` 段之后插入新段）
- Modify: `internal/pkg/code/en.go`
- Modify: `internal/pkg/code/zh_cn.go`

- [ ] **Step 1: Add code constant**

在 `internal/pkg/code/code.go` 的 `ChatPlanActionUnknown` 段之后插入：

```go
// Chat git state 17100~
const (
	ChatGitStateUnavailable = iota + 17100 // 当前 cwd 不是 git 仓库 / git 命令读取失败
)
```

- [ ] **Step 2: Add translations**

`internal/pkg/code/en.go` 表的 ChatLaunchCommand 段附近加：

```go
ChatGitStateUnavailable: "Git state unavailable for this session's working directory",
```

`internal/pkg/code/zh_cn.go` 同位置加：

```go
ChatGitStateUnavailable: "当前会话的工作目录无法读取 git 状态",
```

- [ ] **Step 3: Compile check**

Run: `go build ./internal/pkg/code/...`
Expected: 成功，无输出。

- [ ] **Step 4: Commit**

```bash
git add internal/pkg/code/
git commit -m "✨ feat(code): add ChatGitStateUnavailable error code"
```

---

## Task 2: Backend — git command helper

**Files:**
- Create: `internal/service/chat_svc/git_state.go`
- Create: `internal/service/chat_svc/git_state_test.go`

- [ ] **Step 1: Write the failing test**

`internal/service/chat_svc/git_state_test.go`：

```go
package chat_svc

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// runGit 在 dir 下执行 git args。测试 helper, 失败直接 t.Fatal。
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRunGitState_HappyPath(t *testing.T) {
	Convey("Given a temp git repo on branch ai-chat with 2 dirty files", t, func() {
		dir := t.TempDir()
		runGit(t, dir, "init", "-q", "-b", "ai-chat")
		runGit(t, dir, "config", "user.email", "t@t")
		runGit(t, dir, "config", "user.name", "t")
		runGit(t, dir, "commit", "--allow-empty", "-m", "init")

		// 制造两个 untracked 文件
		_ = exec.Command("touch", filepath.Join(dir, "a.txt")).Run()
		_ = exec.Command("touch", filepath.Join(dir, "b.txt")).Run()

		Convey("When runGitState executes", func() {
			st, err := runGitState(context.Background(), dir)
			So(err, ShouldBeNil)
			So(st.NotARepo, ShouldBeFalse)
			So(st.Branch, ShouldEqual, "ai-chat")
			So(st.Dirty, ShouldEqual, 2)
			So(st.HasUpstream, ShouldBeFalse) // 没 push 过, 无 upstream
		})
	})
}

func TestRunGitState_NotARepo(t *testing.T) {
	Convey("Given a plain empty dir (not a git repo)", t, func() {
		dir := t.TempDir()
		Convey("When runGitState executes", func() {
			st, err := runGitState(context.Background(), dir)
			So(err, ShouldBeNil)
			So(st.NotARepo, ShouldBeTrue)
		})
	})
}

func TestRunGitState_Worktree(t *testing.T) {
	Convey("Given a repo with an attached worktree", t, func() {
		main := t.TempDir()
		runGit(t, main, "init", "-q", "-b", "main")
		runGit(t, main, "config", "user.email", "t@t")
		runGit(t, main, "config", "user.name", "t")
		runGit(t, main, "commit", "--allow-empty", "-m", "init")
		wt := filepath.Join(t.TempDir(), "wt-feat")
		runGit(t, main, "worktree", "add", "-b", "feat", wt)

		Convey("When runGitState runs inside the worktree", func() {
			st, err := runGitState(context.Background(), wt)
			So(err, ShouldBeNil)
			So(st.Branch, ShouldEqual, "feat")
			So(st.Worktree, ShouldNotEqual, "") // 非主仓 → 非空
		})
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestRunGitState ./internal/service/chat_svc/...`
Expected: FAIL — `undefined: runGitState`、`undefined: ChatSessionGitState` 之类的编译错。

- [ ] **Step 3: Add the type to types.go**

在 `internal/service/chat_svc/types.go` 的 `// ── Request / Response shapes ────` 段（line 386 附近）之前插入：

```go
// ChatSessionGitState 是一次 git 状态快照。Branch 为空 + NotARepo=true 意味着 cwd
// 不在 git 仓库内, 前端把整个 chip 区折叠掉。HasUpstream=false 时 Ahead/Behind 不渲染。
type ChatSessionGitState struct {
	Branch      string `json:"branch"`
	Worktree    string `json:"worktree"`
	Dirty       int    `json:"dirty"`
	Ahead       int    `json:"ahead"`
	Behind      int    `json:"behind"`
	HasUpstream bool   `json:"hasUpstream"`
	NotARepo    bool   `json:"notARepo"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type GetSessionGitStateRequest struct {
	SessionID int64 `json:"sessionId"`
}

type GetSessionGitStateResponse struct {
	State ChatSessionGitState `json:"state"`
}
```

- [ ] **Step 4: Implement the runner**

新建 `internal/service/chat_svc/git_state.go`：

```go
package chat_svc

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// runGitState 在 cwd 下连跑几条 git 命令汇成 ChatSessionGitState。
// 任意一条 fork 失败时, 不冒泡 error: 把对应字段留 zero, NotARepo 兜底
// 设 true, 让前端整段折叠。这样的容错语义是 by-design —— UI chip 不应该
// 因为 git 异常而挂掉。
func runGitState(ctx context.Context, cwd string) (ChatSessionGitState, error) {
	st := ChatSessionGitState{UpdatedAt: time.Now().Unix()}
	if cwd == "" {
		st.NotARepo = true
		return st, nil
	}

	if _, err := gitOutput(ctx, cwd, "rev-parse", "--is-inside-work-tree"); err != nil {
		st.NotARepo = true
		return st, nil
	}

	if br, err := gitOutput(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		st.Branch = strings.TrimSpace(br)
	}

	gitDir, _ := gitOutput(ctx, cwd, "rev-parse", "--git-dir")
	commonDir, _ := gitOutput(ctx, cwd, "rev-parse", "--git-common-dir")
	gitDir, commonDir = strings.TrimSpace(gitDir), strings.TrimSpace(commonDir)
	if gitDir != "" && commonDir != "" && gitDir != commonDir {
		// gitDir 形如 <common>/worktrees/<name>; 取尾段做短名。
		st.Worktree = filepath.Base(gitDir)
	}

	if out, err := gitOutput(ctx, cwd, "status", "--porcelain=v1"); err == nil {
		st.Dirty = countNonEmptyLines(out)
	}

	if out, err := gitOutput(ctx, cwd, "rev-list", "--left-right", "--count", "@{u}...HEAD"); err == nil {
		parts := strings.Fields(strings.TrimSpace(out))
		if len(parts) == 2 {
			st.Behind, _ = strconv.Atoi(parts[0])
			st.Ahead, _ = strconv.Atoi(parts[1])
			st.HasUpstream = true
		}
	}

	return st, nil
}

func gitOutput(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func countNonEmptyLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -race -run TestRunGitState ./internal/service/chat_svc/...`
Expected: PASS — 三个 case 都过。

- [ ] **Step 6: Commit**

```bash
git add internal/service/chat_svc/git_state.go internal/service/chat_svc/git_state_test.go internal/service/chat_svc/types.go
git commit -m "✨ feat(chat_svc): add runGitState helper and types"
```

---

## Task 3: Backend — `GetSessionGitState` service method

**Files:**
- Modify: `internal/service/chat_svc/chat.go:59-`（ChatSvc 接口）
- Modify: `internal/service/chat_svc/git_state.go`（加 service method）
- Modify: `internal/service/chat_svc/git_state_test.go`（加 service-level test）

- [ ] **Step 1: Write the failing service test**

在 `internal/service/chat_svc/git_state_test.go` 末尾追加：

```go
import (
	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
)

// 用现有 chat_internal_test.go 的方式 mock chat_repo / agent_backend_repo。
// 这里只测「路由」: 本地 backend 走 runGitState; 远端 backend 提前返回 notARepo=true。
func TestGetSessionGitState_LocalBackend(t *testing.T) {
	Convey("Given a local-backend session whose cwd resolves to a real git repo", t, func() {
		dir := t.TempDir()
		runGit(t, dir, "init", "-q", "-b", "main")
		runGit(t, dir, "config", "user.email", "t@t")
		runGit(t, dir, "config", "user.name", "t")
		runGit(t, dir, "commit", "--allow-empty", "-m", "init")

		sess := &chat_entity.Session{ID: 42, ProjectID: 0}
		be := &agent_backend_entity.AgentBackend{Type: agent_backend_entity.BackendTypeBuiltin}
		// 用 stub 把 resolveSessionCwd 绕到 dir
		RegisterCwdResolver(func(_ context.Context, _ *chat_entity.Session) (string, error) {
			return dir, nil
		})
		t.Cleanup(func() { RegisterCwdResolver(nil) })

		Convey("When GetSessionGitState is called", func() {
			s := &chatSvc{}
			resp, err := s.getSessionGitStateForSession(context.Background(), sess, be)
			So(err, ShouldBeNil)
			So(resp.Branch, ShouldEqual, "main")
		})
	})
}

func TestGetSessionGitState_RemoteBackend_StubsNotARepo(t *testing.T) {
	Convey("Given a remote backend session (MVP not wired through daemon yet)", t, func() {
		sess := &chat_entity.Session{ID: 42, ProjectID: 1}
		be := &agent_backend_entity.AgentBackend{Type: agent_backend_entity.BackendTypeClaudeCode, DeviceID: "dev-1"}
		Convey("Then service returns notARepo=true without erroring", func() {
			s := &chatSvc{}
			resp, err := s.getSessionGitStateForSession(context.Background(), sess, be)
			So(err, ShouldBeNil)
			So(resp.NotARepo, ShouldBeTrue)
		})
	})
}

func TestGetSessionGitState_SessionNotFound(t *testing.T) {
	Convey("Given req.SessionID = 0", t, func() {
		s := &chatSvc{}
		_, err := s.GetSessionGitState(context.Background(), &GetSessionGitStateRequest{SessionID: 0})
		So(err, ShouldNotBeNil)
		So(strings.Contains(err.Error(), strconv.Itoa(code.InvalidParameter)), ShouldBeTrue)
	})
}
```

> 注意:`code.InvalidParameter` 走 i18n 文案,err.Error() 不一定带 code 数字 —— 如果上面那行断言不工作就改成断言 `err != nil` 即可,优先让测试反映"输入校验生效"。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestGetSessionGitState ./internal/service/chat_svc/...`
Expected: FAIL — `getSessionGitStateForSession` / `GetSessionGitState` 未定义。

- [ ] **Step 3: Add ChatSvc interface entry**

`internal/service/chat_svc/chat.go` 的 `type ChatSvc interface` 段（line 59 附近）的 `GetLaunchCommand` 行之后插入：

```go
	GetSessionGitState(ctx context.Context, req *GetSessionGitStateRequest) (*GetSessionGitStateResponse, error)
```

- [ ] **Step 4: Implement service method**

`internal/service/chat_svc/git_state.go` 末尾追加：

```go
// GetSessionGitState 拉某 session 的 git 状态快照。
//   - 本地 backend: 调 runGitState 直接读 cwd。
//   - 远端 backend (claudecode/codex on agentred): MVP 阶段返回 notARepo=true,
//     daemon handler 留 follow-up PR。
func (s *chatSvc) GetSessionGitState(ctx context.Context, req *GetSessionGitStateRequest) (*GetSessionGitStateResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	a, err := agent_repo.Agent().Find(ctx, sess.AgentID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	var be *agent_backend_entity.AgentBackend
	if a != nil && a.AgentBackendID > 0 {
		be, err = agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID)
		if err != nil {
			return nil, i18n.NewError(ctx, code.OperationFailed)
		}
	}
	return s.getSessionGitStateForSession(ctx, sess, be)
}

func (s *chatSvc) getSessionGitStateForSession(ctx context.Context, sess *chat_entity.Session, be *agent_backend_entity.AgentBackend) (*GetSessionGitStateResponse, error) {
	if be != nil && be.IsRemote() {
		return &GetSessionGitStateResponse{State: ChatSessionGitState{
			NotARepo:  true,
			UpdatedAt: time.Now().Unix(),
		}}, nil
	}
	cwd, err := resolveSessionCwd(ctx, sess, be)
	if err != nil || cwd == "" {
		return &GetSessionGitStateResponse{State: ChatSessionGitState{
			NotARepo:  true,
			UpdatedAt: time.Now().Unix(),
		}}, nil
	}
	st, err := runGitState(ctx, cwd)
	if err != nil {
		return nil, i18n.NewError(ctx, code.ChatGitStateUnavailable)
	}
	return &GetSessionGitStateResponse{State: st}, nil
}
```

在 `git_state.go` 顶部加上：

```go
import (
	// ... existing
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -race -run TestGetSessionGitState ./internal/service/chat_svc/...`
Expected: PASS — 三个 case 都过。

> 如果 `TestGetSessionGitState_SessionNotFound` 失败因为 i18n 文案断言,改成 `So(err, ShouldNotBeNil)` 后再跑。

- [ ] **Step 6: Run full chat_svc tests for regression**

Run: `go test -race ./internal/service/chat_svc/...`
Expected: PASS — 加新方法不该破坏既有测试。

- [ ] **Step 7: Commit**

```bash
git add internal/service/chat_svc/chat.go internal/service/chat_svc/git_state.go internal/service/chat_svc/git_state_test.go
git commit -m "✨ feat(chat_svc): add GetSessionGitState service method"
```

---

## Task 4: Wails binding + generate

**Files:**
- Modify: `internal/app/chat.go`（在 `GetChatLaunchCommand` 附近加）

- [ ] **Step 1: Add Wails wrapper**

`internal/app/chat.go` 的 `GetChatLaunchCommand` 后面插入：

```go
// GetSessionGitState 拉某 session 对应 cwd 的 git 状态快照, 供右侧上下文侧栏的
// branch / worktree / dirty / ahead·behind 几个 chip 用。远端 backend 当前
// 返回 notARepo=true 让前端折叠 chip 区, daemon handler 留作 follow-up。
func (a *App) GetSessionGitState(req *chat_svc.GetSessionGitStateRequest) (*chat_svc.GetSessionGitStateResponse, error) {
	return chat_svc.Chat().GetSessionGitState(a.ctx, req)
}
```

- [ ] **Step 2: Compile check**

Run: `go build ./...`
Expected: 无错误。

- [ ] **Step 3: Regenerate wails bindings**

Run: `make generate`
Expected: 修改了 `frontend/wailsjs/go/app/App.js` 和 `App.d.ts`、`frontend/wailsjs/go/models.ts`，能 grep 到 `GetSessionGitState`。

```bash
grep -n GetSessionGitState frontend/wailsjs/go/app/App.d.ts
```

- [ ] **Step 4: Commit**

```bash
git add internal/app/chat.go frontend/wailsjs/
git commit -m "✨ feat(app): expose GetSessionGitState via Wails"
```

---

## Task 5: 前端 store — `chat-sidebar-store`

**Files:**
- Create: `frontend/src/stores/chat-sidebar-store.ts`
- Create: `frontend/src/stores/__tests__/chat-sidebar-store.test.ts`

- [ ] **Step 1: Write the failing test**

`frontend/src/stores/__tests__/chat-sidebar-store.test.ts`：

```ts
import { beforeEach, describe, expect, it } from "vitest";

import { useChatSidebarStore } from "../chat-sidebar-store";

describe("chat-sidebar-store", () => {
  beforeEach(() => {
    localStorage.clear();
    useChatSidebarStore.setState({ open: true, activeTab: "outline" });
  });

  it("toggles open and persists to localStorage", () => {
    useChatSidebarStore.getState().setOpen(false);
    expect(useChatSidebarStore.getState().open).toBe(false);
    const raw = localStorage.getItem("chat-sidebar-state");
    expect(raw).toContain('"open":false');
  });

  it("switches activeTab between outline and files", () => {
    useChatSidebarStore.getState().setActiveTab("files");
    expect(useChatSidebarStore.getState().activeTab).toBe("files");
  });

  it("rejects unknown tab values at runtime by no-op", () => {
    // @ts-expect-error narrowing: ensure caller can't pass arbitrary strings.
    useChatSidebarStore.getState().setActiveTab("bogus");
    expect(useChatSidebarStore.getState().activeTab).toBe("outline");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test src/stores/__tests__/chat-sidebar-store.test.ts`
Expected: FAIL — `Cannot find module '../chat-sidebar-store'`。

- [ ] **Step 3: Implement the store**

`frontend/src/stores/chat-sidebar-store.ts`：

```ts
import { create } from "zustand";
import { persist } from "zustand/middleware";

export type ChatSidebarTab = "outline" | "files";

type ChatSidebarState = {
  open: boolean;
  activeTab: ChatSidebarTab;
  setOpen: (open: boolean) => void;
  setActiveTab: (tab: ChatSidebarTab) => void;
};

const VALID_TABS: ReadonlySet<ChatSidebarTab> = new Set(["outline", "files"]);

export const useChatSidebarStore = create<ChatSidebarState>()(
  persist(
    (set) => ({
      open: true,
      activeTab: "outline",
      setOpen: (open) => set({ open }),
      setActiveTab: (tab) => {
        if (!VALID_TABS.has(tab)) return;
        set({ activeTab: tab });
      },
    }),
    { name: "chat-sidebar-state" },
  ),
);
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test src/stores/__tests__/chat-sidebar-store.test.ts`
Expected: PASS — 3 个 it 全过。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/stores/chat-sidebar-store.ts frontend/src/stores/__tests__/chat-sidebar-store.test.ts
git commit -m "✨ feat(stores): add chat-sidebar-store with localStorage persist"
```

---

## Task 6: 前端 derive — outline + files extractors

**Files:**
- Create: `frontend/src/components/agentre/chat-context-sidebar/derive.ts`
- Create: `frontend/src/components/agentre/chat-context-sidebar/__tests__/derive.test.ts`

- [ ] **Step 1: Write the failing test**

`frontend/src/components/agentre/chat-context-sidebar/__tests__/derive.test.ts`：

```ts
import { describe, expect, it } from "vitest";

import { deriveFiles, deriveOutline } from "../derive";

import type { chat_svc } from "../../../../../wailsjs/go/models";

type Msg = chat_svc.ChatMessage;

function userMsg(id: number, text: string, t = 0): Msg {
  return {
    id, role: "user", sessionId: 1, blocks: [{ type: "text", text }],
    model: "", promptTokens: 0, completionTokens: 0, durationMs: 0,
    errorText: "", seq: 0, createtime: t,
  } as unknown as Msg;
}

function assistantWithEdits(id: number, files: string[], errored = false): Msg {
  const blocks = files.map((p) => ({
    type: "tool_use", name: "Edit", input: { file_path: p },
  }));
  return {
    id, role: "assistant", sessionId: 1, blocks,
    model: "", promptTokens: 0, completionTokens: 0, durationMs: 0,
    errorText: errored ? "boom" : "", seq: 0, createtime: 0,
  } as unknown as Msg;
}

describe("deriveOutline", () => {
  it("treats each user message as one row in chronological order", () => {
    const msgs = [userMsg(1, "first", 1000), userMsg(2, "second", 2000)];
    const out = deriveOutline(msgs);
    expect(out).toHaveLength(2);
    expect(out[0].turn).toBe(1);
    expect(out[1].turn).toBe(2);
    expect(out[0].text).toBe("first");
  });

  it("counts edits between this user msg and the next", () => {
    const msgs = [
      userMsg(1, "do edits"),
      assistantWithEdits(2, ["a.go", "b.go"]),
      userMsg(3, "next"),
      assistantWithEdits(4, ["c.go"]),
    ];
    const out = deriveOutline(msgs);
    expect(out[0].edits).toBe(2);
    expect(out[1].edits).toBe(1);
  });

  it("marks err=true if the following assistant has errorText", () => {
    const msgs = [userMsg(1, "trigger"), assistantWithEdits(2, [], true)];
    const out = deriveOutline(msgs);
    expect(out[0].err).toBe(true);
  });

  it("returns empty array for empty input", () => {
    expect(deriveOutline([])).toEqual([]);
  });
});

describe("deriveFiles", () => {
  it("aggregates Edit/Write/MultiEdit by file_path across turns", () => {
    const msgs = [
      userMsg(1, "u1"), assistantWithEdits(2, ["a.go", "a.go", "b.go"]),
      userMsg(3, "u2"), assistantWithEdits(4, ["a.go"]),
    ];
    const files = deriveFiles(msgs);
    const a = files.find((f) => f.path === "a.go")!;
    const b = files.find((f) => f.path === "b.go")!;
    expect(a.edits).toBe(3);
    expect(b.edits).toBe(1);
    // a.lastTurn 应该是第 2 轮 (msg 3 是 user → turn=2)
    expect(a.lastTurn).toBe(2);
    expect(b.lastTurn).toBe(1);
  });

  it("sorts files by edits desc, ties broken by recency (lastTurn desc)", () => {
    const msgs = [
      userMsg(1, "u1"), assistantWithEdits(2, ["a.go"]),
      userMsg(3, "u2"), assistantWithEdits(4, ["b.go"]),
    ];
    const files = deriveFiles(msgs);
    expect(files[0].path).toBe("b.go"); // tie on edits, b.go 更新
  });

  it("returns empty array for empty input", () => {
    expect(deriveFiles([])).toEqual([]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test src/components/agentre/chat-context-sidebar/__tests__/derive.test.ts`
Expected: FAIL — `Cannot find module '../derive'`。

- [ ] **Step 3: Implement derive.ts**

`frontend/src/components/agentre/chat-context-sidebar/derive.ts`：

```ts
import type { chat_svc } from "../../../../wailsjs/go/models";

type Msg = chat_svc.ChatMessage;

export type OutlineItem = {
  messageId: number;
  turn: number;        // 1-based, 仅数 user message
  text: string;        // preview, 截到 200 字
  time: number;        // unix ms
  edits: number;       // 此 user → 下一个 user 之间 assistant 的 edit tool 数
  err: boolean;        // 下一条 assistant 有 errorText
};

export type FileEntry = {
  path: string;
  edits: number;
  reads: number;
  lastTurn: number;    // 最后一次出现在第几轮 (user msg turn 序号)
};

const EDIT_TOOLS = new Set(["Edit", "Write", "MultiEdit", "apply_patch"]);
const READ_TOOLS = new Set(["Read", "read"]);

function textOf(m: Msg): string {
  for (const b of m.blocks ?? []) {
    if ((b as { type?: string }).type === "text") {
      return (b as { text?: string }).text ?? "";
    }
  }
  return "";
}

// extractToolPath 适配三家 backend 的 input shape:
//   - claudecode Edit/Write/MultiEdit/Read: input.file_path
//   - codex apply_patch: input.path (单文件) 或 input.changes[].path (多文件)
//   - builtin Read: input.path
// 返回单条 message block 对应的 (toolName, paths[])。
function extractToolPaths(block: unknown): { name: string; paths: string[] } | null {
  const b = block as { type?: string; name?: string; input?: Record<string, unknown> };
  if (b.type !== "tool_use" || !b.name) return null;
  const input = b.input ?? {};
  const paths: string[] = [];
  if (typeof input.file_path === "string") paths.push(input.file_path);
  if (typeof input.path === "string") paths.push(input.path);
  const changes = (input as { changes?: Array<{ path?: string }> }).changes;
  if (Array.isArray(changes)) {
    for (const c of changes) {
      if (typeof c?.path === "string") paths.push(c.path);
    }
  }
  return paths.length > 0 ? { name: b.name, paths } : null;
}

export function deriveOutline(messages: Msg[]): OutlineItem[] {
  const out: OutlineItem[] = [];
  let turn = 0;
  for (let i = 0; i < messages.length; i++) {
    const m = messages[i];
    if (m.role !== "user") continue;
    turn += 1;
    let edits = 0;
    let err = false;
    for (let j = i + 1; j < messages.length && messages[j].role !== "user"; j++) {
      const peer = messages[j];
      if (peer.errorText) err = true;
      for (const block of peer.blocks ?? []) {
        const ext = extractToolPaths(block);
        if (ext && EDIT_TOOLS.has(ext.name)) edits += 1;
      }
    }
    out.push({
      messageId: m.id, turn,
      text: textOf(m).slice(0, 200),
      time: m.createtime ?? 0,
      edits, err,
    });
  }
  return out;
}

export function deriveFiles(messages: Msg[]): FileEntry[] {
  const map = new Map<string, FileEntry>();
  let turn = 0;
  for (const m of messages) {
    if (m.role === "user") {
      turn += 1;
      continue;
    }
    for (const block of m.blocks ?? []) {
      const ext = extractToolPaths(block);
      if (!ext) continue;
      const isEdit = EDIT_TOOLS.has(ext.name);
      const isRead = READ_TOOLS.has(ext.name);
      if (!isEdit && !isRead) continue;
      for (const p of ext.paths) {
        const cur = map.get(p) ?? { path: p, edits: 0, reads: 0, lastTurn: 0 };
        if (isEdit) cur.edits += 1;
        if (isRead) cur.reads += 1;
        cur.lastTurn = Math.max(cur.lastTurn, turn);
        map.set(p, cur);
      }
    }
  }
  return [...map.values()].sort((a, b) => {
    if (b.edits !== a.edits) return b.edits - a.edits;
    return b.lastTurn - a.lastTurn;
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test src/components/agentre/chat-context-sidebar/__tests__/derive.test.ts`
Expected: PASS — 7 个 it 全过。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-context-sidebar/derive.ts frontend/src/components/agentre/chat-context-sidebar/__tests__/derive.test.ts
git commit -m "✨ feat(chat-context-sidebar): add outline/files derive"
```

---

## Task 7: 前端 hook — `use-chat-git-state`

**Files:**
- Create: `frontend/src/hooks/use-chat-git-state.ts`
- Create: `frontend/src/hooks/__tests__/use-chat-git-state.test.tsx`

- [ ] **Step 1: Write the failing test**

`frontend/src/hooks/__tests__/use-chat-git-state.test.tsx`：

```tsx
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useSessionStatusStore } from "@/stores/session-status-store";

import { useChatGitState } from "../use-chat-git-state";

vi.mock("../../../wailsjs/go/app/App", () => ({
  GetSessionGitState: vi.fn(),
}));

import { GetSessionGitState } from "../../../wailsjs/go/app/App";

const mockedRpc = vi.mocked(GetSessionGitState);

describe("useChatGitState", () => {
  beforeEach(() => {
    mockedRpc.mockReset();
    useSessionStatusStore.getState().clear?.();
  });

  it("fetches on mount when sessionId > 0", async () => {
    mockedRpc.mockResolvedValue({ state: { branch: "main", notARepo: false, hasUpstream: false, dirty: 0, ahead: 0, behind: 0, worktree: "", updatedAt: 1 } } as never);
    const { result } = renderHook(() => useChatGitState(42));
    await waitFor(() => expect(result.current.state?.branch).toBe("main"));
    expect(mockedRpc).toHaveBeenCalledWith({ sessionId: 42 });
  });

  it("does not fetch when sessionId is 0", () => {
    renderHook(() => useChatGitState(0));
    expect(mockedRpc).not.toHaveBeenCalled();
  });

  it("refreshes when doneTick advances", async () => {
    mockedRpc.mockResolvedValue({ state: { branch: "main" } } as never);
    renderHook(() => useChatGitState(42));
    await waitFor(() => expect(mockedRpc).toHaveBeenCalledTimes(1));
    act(() => {
      useSessionStatusStore.getState().upsert(42, { agentStatus: "idle" });
      // 模拟 done event 让 doneTick 自增
      useSessionStatusStore.setState((s) => {
        const next = new Map(s.statuses);
        const cur = next.get(42) ?? { agentStatus: "idle", needsAttention: false };
        next.set(42, { ...cur, doneTick: ((cur as { doneTick?: number }).doneTick ?? 0) + 1 });
        return { ...s, statuses: next };
      });
    });
    await waitFor(() => expect(mockedRpc).toHaveBeenCalledTimes(2));
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test src/hooks/__tests__/use-chat-git-state.test.tsx`
Expected: FAIL — `Cannot find module '../use-chat-git-state'`。

- [ ] **Step 3: Implement the hook**

`frontend/src/hooks/use-chat-git-state.ts`：

```ts
import * as React from "react";

import { useSessionStatusStore } from "@/stores/session-status-store";

import { GetSessionGitState } from "../../wailsjs/go/app/App";
import type { chat_svc } from "../../wailsjs/go/models";

type GitState = chat_svc.ChatSessionGitState;

type UseChatGitStateResult = {
  state: GitState | null;
  loading: boolean;
  error: string | null;
  refresh: () => void;
};

// useChatGitState 拉某 session 的 git 状态。
// 触发时机:首次 mount + sessionId 变化 + 该 session 的 doneTick 自增 (turn 落定)
//   + 调用方手动 refresh()。不做后台轮询,不监听文件系统。
export function useChatGitState(sessionId: number): UseChatGitStateResult {
  const [state, setState] = React.useState<GitState | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const doneTick = useSessionStatusStore(
    (s) => (sessionId ? (s.statuses.get(sessionId)?.doneTick ?? 0) : 0),
  );
  const [manualTick, setManualTick] = React.useState(0);

  React.useEffect(() => {
    if (!sessionId) {
      setState(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    GetSessionGitState({ sessionId })
      .then((resp) => {
        if (cancelled) return;
        setState(resp.state);
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId, doneTick, manualTick]);

  const refresh = React.useCallback(() => setManualTick((t) => t + 1), []);
  return { state, loading, error, refresh };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test src/hooks/__tests__/use-chat-git-state.test.tsx`
Expected: PASS — 3 个 it 全过。

> 若 `useSessionStatusStore.getState().clear?.()` 不存在,改成 `useSessionStatusStore.setState({ statuses: new Map() })`。先跑测试再决定。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/hooks/use-chat-git-state.ts frontend/src/hooks/__tests__/use-chat-git-state.test.tsx
git commit -m "✨ feat(hooks): add useChatGitState"
```

---

## Task 8: 前端组件 — `ContextChipBar`

**Files:**
- Create: `frontend/src/components/agentre/chat-context-sidebar/context-chip-bar.tsx`
- Create: `frontend/src/components/agentre/chat-context-sidebar/__tests__/context-chip-bar.test.tsx`

- [ ] **Step 1: Write the failing test**

`frontend/src/components/agentre/chat-context-sidebar/__tests__/context-chip-bar.test.tsx`：

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ContextChipBar } from "../context-chip-bar";

import type { chat_svc } from "../../../../../wailsjs/go/models";

type GitState = chat_svc.ChatSessionGitState;

const base: GitState = {
  branch: "ai-chat", worktree: "",
  dirty: 0, ahead: 0, behind: 0,
  hasUpstream: false, notARepo: false, updatedAt: 0,
};

describe("ContextChipBar", () => {
  it("renders branch chip", () => {
    render(<ContextChipBar state={{ ...base }} loading={false} error={null} onRefresh={() => {}} />);
    expect(screen.getByText("ai-chat")).toBeInTheDocument();
  });

  it("hides dirty chip when dirty=0", () => {
    render(<ContextChipBar state={{ ...base, dirty: 0 }} loading={false} error={null} onRefresh={() => {}} />);
    expect(screen.queryByTestId("dirty-chip")).not.toBeInTheDocument();
  });

  it("shows dirty chip when dirty>0", () => {
    render(<ContextChipBar state={{ ...base, dirty: 5 }} loading={false} error={null} onRefresh={() => {}} />);
    expect(screen.getByTestId("dirty-chip")).toHaveTextContent("5");
  });

  it("shows ahead/behind chip only when hasUpstream", () => {
    render(<ContextChipBar state={{ ...base, hasUpstream: true, ahead: 3, behind: 0 }} loading={false} error={null} onRefresh={() => {}} />);
    expect(screen.getByTestId("sync-chip")).toBeInTheDocument();
  });

  it("hides ahead/behind chip when hasUpstream is false", () => {
    render(<ContextChipBar state={{ ...base, hasUpstream: false }} loading={false} error={null} onRefresh={() => {}} />);
    expect(screen.queryByTestId("sync-chip")).not.toBeInTheDocument();
  });

  it("collapses whole bar when notARepo=true", () => {
    const { container } = render(
      <ContextChipBar state={{ ...base, notARepo: true }} loading={false} error={null} onRefresh={() => {}} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("shows inline error with retry", () => {
    const onRefresh = vi.fn();
    render(<ContextChipBar state={null} loading={false} error="boom" onRefresh={onRefresh} />);
    expect(screen.getByText(/git 状态读取失败/)).toBeInTheDocument();
    screen.getByRole("button", { name: /重试|retry/i }).click();
    expect(onRefresh).toHaveBeenCalled();
  });
});
```

> `vi` 已经全局可用（vitest globals），如未启用就 `import { vi } from "vitest"`。

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test src/components/agentre/chat-context-sidebar/__tests__/context-chip-bar.test.tsx`
Expected: FAIL — `Cannot find module '../context-chip-bar'`。

- [ ] **Step 3: Implement the component**

`frontend/src/components/agentre/chat-context-sidebar/context-chip-bar.tsx`：

```tsx
import * as React from "react";
import { ArrowDown, ArrowUp, FolderTree, GitBranch, RefreshCcw } from "lucide-react";

import { Button } from "@/components/ui/button";

import type { chat_svc } from "../../../../wailsjs/go/models";

type GitState = chat_svc.ChatSessionGitState;

type Props = {
  state: GitState | null;
  loading: boolean;
  error: string | null;
  onRefresh: () => void;
};

export function ContextChipBar({ state, loading, error, onRefresh }: Props) {
  if (error) {
    return (
      <div className="border-b border-border bg-background px-4 py-3">
        <p className="mb-2 text-xs text-destructive">git 状态读取失败</p>
        <Button type="button" size="sm" variant="outline" onClick={onRefresh}>
          重试
        </Button>
      </div>
    );
  }
  if (!state || state.notARepo) return null;

  return (
    <div className="flex flex-col gap-2 border-b border-border px-4 pb-3 pt-3.5">
      <div className="flex items-center justify-between text-2xs font-semibold uppercase tracking-wider text-muted-foreground">
        <span>Context</span>
        <button
          type="button"
          onClick={onRefresh}
          disabled={loading}
          aria-label="刷新 git 状态"
          className="text-muted-foreground hover:text-foreground"
        >
          <RefreshCcw className={"size-3 " + (loading ? "animate-spin" : "")} aria-hidden="true" />
        </button>
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        <Chip data-testid="branch-chip">
          <GitBranch className="size-3 text-muted-foreground" aria-hidden="true" />
          <span className="font-mono text-xs">{state.branch || "(detached)"}</span>
        </Chip>
        {state.dirty > 0 ? (
          <Chip data-testid="dirty-chip" tone="warning">
            <span aria-hidden="true">●</span>
            <span className="font-mono text-xs">{state.dirty}</span>
          </Chip>
        ) : null}
        {state.hasUpstream ? (
          <Chip data-testid="sync-chip">
            <ArrowUp className="size-2.5" aria-hidden="true" />
            <span className="font-mono text-xs">{state.ahead}</span>
            <ArrowDown className="size-2.5" aria-hidden="true" />
            <span className="font-mono text-xs">{state.behind}</span>
          </Chip>
        ) : null}
      </div>
      {state.worktree ? (
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <FolderTree className="size-3" aria-hidden="true" />
          <span className="truncate font-mono">{state.worktree}</span>
        </div>
      ) : null}
    </div>
  );
}

function Chip({
  children,
  tone = "neutral",
  ...rest
}: React.HTMLAttributes<HTMLDivElement> & { tone?: "neutral" | "warning" }) {
  return (
    <div
      {...rest}
      className={
        "inline-flex h-[22px] items-center gap-1.5 rounded-md border px-2 text-xs " +
        (tone === "warning"
          ? "border-amber-500/40 bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300"
          : "border-border bg-card text-foreground")
      }
    >
      {children}
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test src/components/agentre/chat-context-sidebar/__tests__/context-chip-bar.test.tsx`
Expected: PASS — 7 个 it 全过。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-context-sidebar/context-chip-bar.tsx frontend/src/components/agentre/chat-context-sidebar/__tests__/context-chip-bar.test.tsx
git commit -m "✨ feat(chat-context-sidebar): add ContextChipBar"
```

---

## Task 9: 前端组件 — `OutlineView`

**Files:**
- Create: `frontend/src/components/agentre/chat-context-sidebar/views/outline-view.tsx`
- Create: `frontend/src/components/agentre/chat-context-sidebar/__tests__/outline-view.test.tsx`

- [ ] **Step 1: Write the failing test**

`frontend/src/components/agentre/chat-context-sidebar/__tests__/outline-view.test.tsx`：

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { OutlineView } from "../views/outline-view";

import type { OutlineItem } from "../derive";

const items: OutlineItem[] = [
  { messageId: 11, turn: 1, text: "do thing one", time: 1700000000000, edits: 2, err: false },
  { messageId: 22, turn: 2, text: "do thing two", time: 1700000060000, edits: 0, err: true },
];

describe("OutlineView", () => {
  it("renders each outline item", () => {
    render(<OutlineView items={items} activeMessageId={null} onSelect={() => {}} />);
    expect(screen.getByText("do thing one")).toBeInTheDocument();
    expect(screen.getByText("do thing two")).toBeInTheDocument();
  });

  it("renders edits badge and error badge", () => {
    render(<OutlineView items={items} activeMessageId={null} onSelect={() => {}} />);
    expect(screen.getByText(/2 edits/)).toBeInTheDocument();
    expect(screen.getByText(/error/i)).toBeInTheDocument();
  });

  it("highlights active row", () => {
    render(<OutlineView items={items} activeMessageId={22} onSelect={() => {}} />);
    const active = screen.getByText("do thing two").closest("[data-active]")!;
    expect(active.getAttribute("data-active")).toBe("true");
  });

  it("calls onSelect with messageId when row clicked", async () => {
    const onSelect = vi.fn();
    render(<OutlineView items={items} activeMessageId={null} onSelect={onSelect} />);
    await userEvent.click(screen.getByText("do thing one"));
    expect(onSelect).toHaveBeenCalledWith(11);
  });

  it("renders empty state when items is empty", () => {
    render(<OutlineView items={[]} activeMessageId={null} onSelect={() => {}} />);
    expect(screen.getByText(/还没有消息/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test src/components/agentre/chat-context-sidebar/__tests__/outline-view.test.tsx`
Expected: FAIL — `Cannot find module '../views/outline-view'`。

- [ ] **Step 3: Implement OutlineView**

`frontend/src/components/agentre/chat-context-sidebar/views/outline-view.tsx`：

```tsx
import * as React from "react";
import { CircleX, Pencil } from "lucide-react";

import { cn } from "@/lib/utils";

import type { OutlineItem } from "../derive";

type Props = {
  items: OutlineItem[];
  activeMessageId: number | null;
  onSelect: (messageId: number) => void;
};

function formatTime(ms: number): string {
  if (!ms) return "";
  const d = new Date(ms);
  return d.getHours().toString().padStart(2, "0") + ":" + d.getMinutes().toString().padStart(2, "0");
}

export function OutlineView({ items, activeMessageId, onSelect }: Props) {
  if (items.length === 0) {
    return <div className="px-3 py-6 text-center text-xs text-muted-foreground">本会话还没有消息</div>;
  }
  return (
    <div className="flex flex-col gap-0.5 px-2 py-2.5">
      {items.map((it) => {
        const active = it.messageId === activeMessageId;
        return (
          <button
            key={it.messageId}
            type="button"
            onClick={() => onSelect(it.messageId)}
            data-active={active}
            className={cn(
              "flex flex-col gap-1 rounded-md px-2.5 py-2 text-left text-xs transition-colors",
              active
                ? "border-l-2 border-primary bg-primary/10 text-foreground"
                : "border-l-2 border-transparent text-muted-foreground hover:bg-muted/50",
            )}
          >
            <div className={cn("flex items-center gap-1.5 font-mono text-[10px]", active ? "text-primary" : "text-muted-foreground/80")}>
              <span>{formatTime(it.time)}</span>
              <span className="text-border-strong">·</span>
              <span>第 {it.turn} 轮</span>
            </div>
            <p className={cn("line-clamp-2 leading-snug", active ? "font-medium" : "")}>{it.text}</p>
            {it.edits > 0 || it.err ? (
              <div className="flex items-center gap-1.5">
                {it.err ? (
                  <span className="inline-flex items-center gap-1 rounded-sm bg-destructive/10 px-1.5 py-0.5 text-[10px] font-medium text-destructive">
                    <CircleX className="size-2.5" aria-hidden="true" />
                    error
                  </span>
                ) : null}
                {it.edits > 0 ? (
                  <span className="inline-flex items-center gap-1 rounded-sm border border-border bg-card px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                    <Pencil className="size-2.5" aria-hidden="true" />
                    {it.edits} edits
                  </span>
                ) : null}
              </div>
            ) : null}
          </button>
        );
      })}
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test src/components/agentre/chat-context-sidebar/__tests__/outline-view.test.tsx`
Expected: PASS — 5 个 it 全过。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-context-sidebar/views/outline-view.tsx frontend/src/components/agentre/chat-context-sidebar/__tests__/outline-view.test.tsx
git commit -m "✨ feat(chat-context-sidebar): add OutlineView"
```

---

## Task 10: 前端组件 — `FilesView`

**Files:**
- Create: `frontend/src/components/agentre/chat-context-sidebar/views/files-view.tsx`
- Create: `frontend/src/components/agentre/chat-context-sidebar/__tests__/files-view.test.tsx`

- [ ] **Step 1: Write the failing test**

`frontend/src/components/agentre/chat-context-sidebar/__tests__/files-view.test.tsx`：

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { FilesView } from "../views/files-view";

import type { FileEntry } from "../derive";

const files: FileEntry[] = [
  { path: "internal/service/chat_svc/chat.go", edits: 5, reads: 1, lastTurn: 3 },
  { path: "frontend/src/components/chat-panel.tsx", edits: 2, reads: 0, lastTurn: 2 },
];

describe("FilesView", () => {
  it("renders each file with edits count", () => {
    render(<FilesView files={files} onJumpToTurn={() => {}} />);
    expect(screen.getByText(/chat\.go/)).toBeInTheDocument();
    expect(screen.getByText("5")).toBeInTheDocument();
  });

  it("calls onJumpToTurn with lastTurn when row clicked", async () => {
    const onJump = vi.fn();
    render(<FilesView files={files} onJumpToTurn={onJump} />);
    await userEvent.click(screen.getByText(/chat\.go/));
    expect(onJump).toHaveBeenCalledWith(3);
  });

  it("shows empty state when files is empty", () => {
    render(<FilesView files={[]} onJumpToTurn={() => {}} />);
    expect(screen.getByText(/没有文件/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test src/components/agentre/chat-context-sidebar/__tests__/files-view.test.tsx`
Expected: FAIL — `Cannot find module '../views/files-view'`。

- [ ] **Step 3: Implement FilesView**

`frontend/src/components/agentre/chat-context-sidebar/views/files-view.tsx`：

```tsx
import * as React from "react";
import { FileCode, Pencil } from "lucide-react";

import type { FileEntry } from "../derive";

type Props = {
  files: FileEntry[];
  onJumpToTurn: (turn: number) => void;
};

function shortPath(p: string): string {
  // 显示最后两段, e.g. service/chat_svc/chat.go → chat_svc/chat.go
  const parts = p.split("/");
  return parts.length <= 2 ? p : parts.slice(-2).join("/");
}

export function FilesView({ files, onJumpToTurn }: Props) {
  if (files.length === 0) {
    return <div className="px-3 py-6 text-center text-xs text-muted-foreground">本会话还没有改过任何文件</div>;
  }
  return (
    <div className="flex flex-col gap-0.5 px-2 py-2.5">
      {files.map((f) => (
        <button
          key={f.path}
          type="button"
          onClick={() => onJumpToTurn(f.lastTurn)}
          className="flex items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-xs text-muted-foreground transition-colors hover:bg-muted/50"
          title={f.path}
        >
          <FileCode className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
          <span className="flex-1 truncate font-mono">{shortPath(f.path)}</span>
          {f.edits > 0 ? (
            <span className="inline-flex shrink-0 items-center gap-0.5 text-[10px] font-medium text-foreground">
              <Pencil className="size-2.5" aria-hidden="true" />
              {f.edits}
            </span>
          ) : null}
        </button>
      ))}
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test src/components/agentre/chat-context-sidebar/__tests__/files-view.test.tsx`
Expected: PASS — 3 个 it 全过。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-context-sidebar/views/files-view.tsx frontend/src/components/agentre/chat-context-sidebar/__tests__/files-view.test.tsx
git commit -m "✨ feat(chat-context-sidebar): add FilesView"
```

---

## Task 11: 前端组件 — `TabBar` + Sidebar 容器

**Files:**
- Create: `frontend/src/components/agentre/chat-context-sidebar/tab-bar.tsx`
- Create: `frontend/src/components/agentre/chat-context-sidebar/index.tsx`
- Create: `frontend/src/components/agentre/chat-context-sidebar/__tests__/index.test.tsx`

- [ ] **Step 1: Write the failing test**

`frontend/src/components/agentre/chat-context-sidebar/__tests__/index.test.tsx`：

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useChatSidebarStore } from "@/stores/chat-sidebar-store";

import { ChatContextSidebar } from "../index";

import type { chat_svc } from "../../../../../wailsjs/go/models";

type Msg = chat_svc.ChatMessage;

vi.mock("../../../../../wailsjs/go/app/App", () => ({
  GetSessionGitState: vi.fn().mockResolvedValue({
    state: { branch: "main", notARepo: false, hasUpstream: false, worktree: "", dirty: 0, ahead: 0, behind: 0, updatedAt: 0 },
  }),
}));

const userM = (id: number, text: string): Msg => ({
  id, role: "user", sessionId: 1, blocks: [{ type: "text", text }],
  model: "", promptTokens: 0, completionTokens: 0, durationMs: 0,
  errorText: "", seq: 0, createtime: 0,
} as unknown as Msg);

describe("ChatContextSidebar", () => {
  beforeEach(() => {
    localStorage.clear();
    useChatSidebarStore.setState({ open: true, activeTab: "outline" });
  });

  it("shows OutlineView by default", () => {
    render(<ChatContextSidebar sessionId={1} messages={[userM(1, "hello world")]} onJumpToMessage={() => {}} />);
    expect(screen.getByText("hello world")).toBeInTheDocument();
  });

  it("switches to FilesView when Files tab clicked", async () => {
    render(<ChatContextSidebar sessionId={1} messages={[userM(1, "hello world")]} onJumpToMessage={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: /files/i }));
    expect(screen.getByText(/没有改过任何文件|没有文件/)).toBeInTheDocument();
    expect(useChatSidebarStore.getState().activeTab).toBe("files");
  });

  it("calls onJumpToMessage when outline row clicked", async () => {
    const onJump = vi.fn();
    render(<ChatContextSidebar sessionId={1} messages={[userM(99, "hello world")]} onJumpToMessage={onJump} />);
    await userEvent.click(screen.getByText("hello world"));
    expect(onJump).toHaveBeenCalledWith(99);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test src/components/agentre/chat-context-sidebar/__tests__/index.test.tsx`
Expected: FAIL — `Cannot find module '../index'`。

- [ ] **Step 3: Implement TabBar**

`frontend/src/components/agentre/chat-context-sidebar/tab-bar.tsx`：

```tsx
import * as React from "react";
import { FileCode, List } from "lucide-react";

import { cn } from "@/lib/utils";

import type { ChatSidebarTab } from "@/stores/chat-sidebar-store";

type Props = {
  active: ChatSidebarTab;
  onChange: (tab: ChatSidebarTab) => void;
  outlineCount: number;
  filesCount: number;
};

export function TabBar({ active, onChange, outlineCount, filesCount }: Props) {
  return (
    <div className="flex h-9 shrink-0 items-center gap-1 border-b border-border px-3" role="tablist">
      <Tab icon={<List className="size-3" aria-hidden="true" />} label="Outline" count={outlineCount} active={active === "outline"} onClick={() => onChange("outline")} />
      <Tab icon={<FileCode className="size-3" aria-hidden="true" />} label="Files" count={filesCount} active={active === "files"} onClick={() => onChange("files")} />
    </div>
  );
}

function Tab({ icon, label, count, active, onClick }: { icon: React.ReactNode; label: string; count: number; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium",
        active ? "bg-primary/10 text-primary" : "text-muted-foreground hover:bg-muted/50",
      )}
    >
      {icon}
      <span>{label}</span>
      <span className="font-mono text-[10px] opacity-80">{count}</span>
    </button>
  );
}
```

- [ ] **Step 4: Implement Sidebar container**

`frontend/src/components/agentre/chat-context-sidebar/index.tsx`：

```tsx
import * as React from "react";

import { useChatSidebarStore } from "@/stores/chat-sidebar-store";
import { useChatGitState } from "@/hooks/use-chat-git-state";

import type { chat_svc } from "../../../../wailsjs/go/models";

import { ContextChipBar } from "./context-chip-bar";
import { deriveFiles, deriveOutline } from "./derive";
import { TabBar } from "./tab-bar";
import { FilesView } from "./views/files-view";
import { OutlineView } from "./views/outline-view";

type Msg = chat_svc.ChatMessage;

type Props = {
  sessionId: number;
  messages: Msg[];
  onJumpToMessage: (messageId: number) => void;
};

export function ChatContextSidebar({ sessionId, messages, onJumpToMessage }: Props) {
  const activeTab = useChatSidebarStore((s) => s.activeTab);
  const setActiveTab = useChatSidebarStore((s) => s.setActiveTab);
  const git = useChatGitState(sessionId);

  const outline = React.useMemo(() => deriveOutline(messages), [messages]);
  const files = React.useMemo(() => deriveFiles(messages), [messages]);

  const turnToMessageId = React.useMemo(() => {
    const m = new Map<number, number>();
    let turn = 0;
    for (const msg of messages) {
      if (msg.role === "user") {
        turn += 1;
        m.set(turn, msg.id);
      }
    }
    return m;
  }, [messages]);

  return (
    <aside className="flex h-full w-[320px] shrink-0 flex-col border-l border-border bg-sidebar">
      <ContextChipBar state={git.state} loading={git.loading} error={git.error} onRefresh={git.refresh} />
      <TabBar active={activeTab} onChange={setActiveTab} outlineCount={outline.length} filesCount={files.length} />
      <div className="min-h-0 flex-1 overflow-auto">
        {activeTab === "outline" ? (
          <OutlineView items={outline} activeMessageId={null} onSelect={onJumpToMessage} />
        ) : (
          <FilesView
            files={files}
            onJumpToTurn={(turn) => {
              const mid = turnToMessageId.get(turn);
              if (mid != null) onJumpToMessage(mid);
            }}
          />
        )}
      </div>
    </aside>
  );
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd frontend && pnpm test src/components/agentre/chat-context-sidebar/__tests__/index.test.tsx`
Expected: PASS — 3 个 it 全过。

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/chat-context-sidebar/
git commit -m "✨ feat(chat-context-sidebar): add TabBar and sidebar container"
```

---

## Task 12: 接入 ChatPanel — toolbar toggle + MainRow 包裹 + 挂载

**Files:**
- Modify: `frontend/src/components/agentre/chat-panel.tsx`
- Modify: `frontend/src/components/agentre/__tests__/chat-panel.test.tsx`

- [ ] **Step 1: Write the failing test**

在 `frontend/src/components/agentre/__tests__/chat-panel.test.tsx` 末尾增加：

```tsx
// 新增: 右侧上下文 sidebar 默认渲染, toolbar 切换按钮可隐藏 / 显示。
it("renders context sidebar by default and toggles via toolbar button", async () => {
  // setup 沿用文件已有的 renderChatPanel helper (如不存在则用现有 render 模式)。
  // 这里只示意关键断言:
  const { getByRole, queryByRole } = renderChatPanel({ sessionId: 42 });
  expect(getByRole("complementary", { name: /context|上下文/i })).toBeInTheDocument();
  await userEvent.click(getByRole("button", { name: /上下文侧栏|context sidebar/i }));
  expect(queryByRole("complementary", { name: /context|上下文/i })).not.toBeInTheDocument();
});
```

> 文件已有的 `renderChatPanel` 辅助函数和现成 mock 链路重用。如果当前 chat-panel.test.tsx 没有 `renderChatPanel` helper，按照该文件已有的 mock + render 模式照抄即可，本步骤只是补一个新的 `it()` 用例。

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test src/components/agentre/__tests__/chat-panel.test.tsx`
Expected: FAIL — 新增的用例找不到 sidebar 或 toggle 按钮。

- [ ] **Step 3: 在 chat-panel.tsx 加 toolbar 按钮 + MainRow + sidebar 挂载**

**3a.** 顶部 import 增加：

```ts
import { PanelRight, PanelRightClose } from "lucide-react";
import { useChatSidebarStore } from "@/stores/chat-sidebar-store";
import { ChatContextSidebar } from "./chat-context-sidebar";
```

**3b.** 在 `ChatPanel` 函数体内 hook 区域加：

```ts
const sidebarOpen = useChatSidebarStore((s) => s.open);
const setSidebarOpen = useChatSidebarStore((s) => s.setOpen);
```

**3c.** 在 toolbar 的 `MoreHorizontal` DropdownMenu 之前插入新 Button：

```tsx
<Button
  type="button"
  variant="outline"
  size="icon-sm"
  aria-label="上下文侧栏"
  onClick={() => setSidebarOpen(!sidebarOpen)}
  title={sidebarOpen ? "隐藏上下文侧栏" : "显示上下文侧栏"}
>
  {sidebarOpen ? (
    <PanelRightClose data-icon="only" aria-hidden="true" />
  ) : (
    <PanelRight data-icon="only" aria-hidden="true" />
  )}
</Button>
```

**3d.** 把原本 `<section ref={transcriptRef} ...>` 块外面再裹一层 `<div className="flex min-h-0 flex-1">`，并在 section 后面挂 sidebar：

```tsx
<div className="flex min-h-0 flex-1">
  <section
    ref={transcriptRef}
    onScroll={handleTranscriptScroll}
    className="min-h-0 flex-1 overflow-auto px-7 py-5"
  >
    <ChatTranscript ... />
  </section>
  {sidebarOpen ? (
    <ChatContextSidebar
      sessionId={sessionId}
      messages={messages}
      onJumpToMessage={(mid) => {
        const el = document.querySelector(`[data-message-id="${mid}"]`);
        if (el && el instanceof HTMLElement) {
          el.scrollIntoView({ behavior: "smooth", block: "start" });
        }
      }}
    />
  ) : null}
</div>
```

**3e.** Transcript 需要给每条 message 渲染 `data-message-id`。找到 `ChatTranscript` 在 `chat.tsx` 里渲染单条 message 的 `<article>` / `<div>`，加上：

```tsx
<div data-message-id={message.id} ...>
```

> ChatTranscript 文件路径：`frontend/src/components/agentre/chat.tsx`（同目录已经导出 ChatTranscript）。这一改动属于 sidebar 接入的最小需求，可视作一个 in-scope 的 drift，不算 "drive-by"。

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test src/components/agentre/__tests__/chat-panel.test.tsx`
Expected: PASS — 包括新加的用例。

- [ ] **Step 5: 在浏览器里验证 (UI feature 强制要求)**

按 `CLAUDE.md` 「UI / frontend changes」一节，必须真机验：

```bash
make dev
```

打开 app → 任意已有 chat 会话 → 验证：
1. 默认右侧 sidebar 已经展开，顶部能看到 branch chip（仓库当前 branch=ai-chat）
2. 点 toolbar 上的 PanelRight 按钮，sidebar 整体消失；图标变成 PanelRight (open variant)
3. 再点一下回来
4. 切到 Files tab，能看到本会话改过的文件列表
5. 关闭 app 再开，sidebar 状态保持
6. 点 Outline 一项，transcript 滚到对应 user message

- [ ] **Step 6: Run lint + full test suite for regression**

```bash
make check
```

Expected: PASS — 后端 + 前端 lint 都过、go test + vitest 全过。

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/agentre/chat-panel.tsx frontend/src/components/agentre/chat.tsx frontend/src/components/agentre/__tests__/chat-panel.test.tsx
git commit -m "✨ feat(chat-panel): wire ChatContextSidebar with toolbar toggle"
```

---

## Self-Review Checklist

跑完所有 task 后，对照 spec 检查：

- [ ] Outline 视图 ↔ Task 6 + Task 9 实现 ✓
- [ ] Files 视图 ↔ Task 6 + Task 10 实现 ✓
- [ ] Context chip bar ↔ Task 8 实现 ✓
- [ ] Git state RPC ↔ Task 2 + Task 3 + Task 4 实现 ✓
- [ ] Refresh strategy（doneTick + manual）↔ Task 7 实现 ✓
- [ ] Sidebar toggle 全局 localStorage ↔ Task 5 实现 ✓
- [ ] 远端 backend 返回 notARepo=true ↔ Task 3 step 4 实现 ✓
- [ ] UI states 表（dirty=0 不渲染、notARepo 折叠等）↔ Task 8 测试覆盖 ✓
- [ ] Error chip + retry ↔ Task 8 测试覆盖 ✓
- [ ] 错误码 `ChatGitStateUnavailable` ↔ Task 1 ✓
