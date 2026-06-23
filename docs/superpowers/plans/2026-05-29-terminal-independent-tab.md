# 终端独立成 Tab + 项目菜单「新建终端」Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把终端从会话 tab 里拆出来,变成与会话平级、有独立生命周期的独立 tab;在项目页项目菜单加「新建终端 ▶」(本地 / 在线远端 device)。

**Architecture:** 后端 `terminal_svc` 从 `sessionID int64` 解耦为不透明的 `terminalID string`,`Open` 直收已解析好的 `(deviceID, cwd)`;cwd 解析上移到 app 层(`project_svc.ResolveProjectCwd`)。前端给 `chat-tabs-store` 加 `terminal` tab 变体 + `openTerminal` action,终端面板按 terminalID 接线;**完全移除**会话内终端(toggle/⌘\`/`chat-terminal-store`)。

**Tech Stack:** Go 1.26 (cago, goconvey, gomock)、React 19 + TS、zustand、xterm.js、Wails v2 bindings。

---

## Spec & 决策

Spec:`docs/superpowers/specs/2026-05-29-terminal-independent-tab-design.md`。已定决策:
1. 完全移除会话内终端;终端一律独立 tab,后端按 terminalID 索引。
2. 新建终端可选 backend:本地 / 在线远端 device(远端 cwd 走 `project_location`)。
3. 终端 tab 持久化;重启后 tab 在、PTY 重新 spawn(历史丢失)。
4. 子菜单里未配置 `project_location` 的远端 device 置灰 + hover 提示。

## ⚠️ 实现纪律(仓库 CLAUDE.md 强制)

- **严格 TDD:Red → Green → Refactor**,无失败测试不写实现。
- **每个文件改前先完整 `Read`**(本计划摘录的"当前代码"以仓库实际为准 —— 本计划撰写时部分 file-read 被环境截断,务必现场核对)。
- **只动 scope 内文件**,禁止顺手 refactor / 重命名 sweep / 格式化 / import 重排。
- 工作树已有未提交改动(`internal/service/chat_svc/chat.go` + `chat_test.go`)——**与本任务无关,不要碰、也不要把它们卷进本任务的 commit**。

## 真实集成点(撰写时已核对的现状)

- `project-page.tsx`:
  - `ProjectSelection` 联合(line ~84):**当前只有 `session | new`,无 terminal/open-terminal**,Task 9 扩展之。
  - `selectOnTab`(line ~223,`useCallback`):把 `ProjectSelection` 翻成 store action(`openSession`/`openSessionInNewTab`/`openNewSession`)。Task 9 在这里接 `openTerminal`。
  - `ProjectCard` 的 `onSelect` prop 类型即 `(sel: ProjectSelection | null, opts?) => void`(line ~557),顶层 `onSelect={selectOnTab}`(line ~387)。扩了 union 后,卡片内菜单 fire 新变体即可。
  - 项目卡片「更多操作」下拉(line ~963-983):`DropdownMenuContent` 内现有 `项目设置` / `新建子项目`。Task 10 在此加「新建终端」子菜单。
  - 同文件已有 `NewSessionMenu`(line ~991):lazy 拉成员的下拉范式,`NewTerminalSubMenu` 照抄其 lazy-load 思路。
- `chat-tabs-store.ts`:无 zustand persist 中间件;持久化靠自定义 `chat-tabs-persistence.ts`(subscribe 防抖写 + import 时同步 hydrate)。已有 `nextId()`(crypto.randomUUID,`__setNextIdFactoryForTesting` 可 stub)与 `now()`(`__setNowForTesting`)。
- `chat-tabs-persistence.ts`:**flat、仅 session 的 v1 格式** —— `writePersistedTabs` 只持久化 `kind==="session" && !isPreview`,条目形如 `{id, sessionId, isPinned, pinAt, openedAt}`;`readPersistedTabs` 在 `v!==1` 时返回 null。Task 5 升级到 v2 以持久化终端 tab。
- `terminal_svc` 测试文件:`service_test.go`、`backend_test.go`、`emitter_test.go`、`open_preempt_test.go`、`pump_exit_race_test.go`、`integration_test.go`(均按 sessionID + SessionLookup mock,需改)。
- `project_location_repo.FindByProjectAndDevice` 用 `.Take()` —— **未命中返回 `gorm.ErrRecordNotFound`(不是 nil,nil)**;参照 `chat_svc/cwd.go:57-64` 的 `errors.Is(err, gorm.ErrRecordNotFound) → code.ProjectLocationMissing` 写法。
- `code.ProjectLocationMissing` 与 `code.ProjectNotFound` 均已存在。

## 文件改动总览

### 后端
- `internal/service/terminal_svc/backend.go` — `Pick(deviceID string)`
- `internal/service/terminal_svc/emitter.go` — 事件名按 terminalID
- `internal/service/terminal_svc/service.go` — `Service` 以 terminalID 索引;`Open(terminalID, deviceID, cwd, cols, rows)`;删 `SessionLookup`/`ErrSessionNotFound`;`NewService(sel, emitter)`
- `internal/service/terminal_svc/*_test.go` — 改按 terminalID,去 SessionLookup mock
- `internal/service/project_svc/project.go` + `cwd.go` — 新增 `ResolveProjectCwd`;`cwd_terminal_test.go`(新建)
- `internal/app/terminal.go` — 4 绑定改 terminalID,Open 先解析 cwd
- `internal/app/terminal_wiring.go` — 删 `sessionLookupAdapter`,`NewService(selector, emitter)`
- `frontend/wailsjs/...` — `make generate` 重生成

### 前端
- `frontend/src/stores/chat-tabs-store.ts` — `TabKind` 加 terminal;`openTerminal` action
- `frontend/src/stores/chat-tabs-persistence.ts` — 升级到 v2,持久化 terminal tab
- `frontend/src/components/agentre/terminal/use-terminal.ts` — 按 terminalID,去 `chat-terminal-store`
- `frontend/src/components/agentre/terminal/terminal-panel.tsx` — props 改;`onClose` 回调替代 `toggle`
- `frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx` — terminal tab 走 `HostedTerminalPanel`
- `frontend/src/components/agentre/chat-tabs/tab.tsx` — `ICON_BY_KIND.terminal`(注:确认 `tab.tsx` 是否真有 `ICON_BY_KIND` 映射;撰写时未直接见到该常量,可能图标在 `tab-strip.tsx`/`use-tabs-view` 里按 kind 选 —— **实现前先 grep `ICON_BY_KIND` / 终端图标渲染处**)
- `frontend/src/components/agentre/project-page.tsx` — `ProjectSelection` 加 `open-terminal` + `selectOnTab` 接 `openTerminal` + 「新建终端」子菜单
- `frontend/src/components/agentre/chat-panel.tsx` — 移除会话内终端
- 删除 `frontend/src/stores/chat-terminal-store.ts`(及其测试)

---

# Phase 0 — 后端:terminal_svc 解耦为 terminalID

### Task 0: `BackendSelector.Pick(deviceID string)`

**Files:**
- Modify: `internal/service/terminal_svc/backend.go`
- Test: `internal/service/terminal_svc/backend_test.go`

- [ ] **Step 1: 改测试为按 deviceID(RED)**

```go
func TestBackendSelector_Pick_LocalOnEmptyDeviceID(t *testing.T) {
	local := fakeBackend{} // 复用文件里既有 fake;若无则最小实现 Open 返回 nil,nil
	sel := NewBackendSelector(local, func(string) (PTYBackend, error) {
		t.Fatal("remote factory must not be called for local")
		return nil, nil
	})
	got, err := sel.Pick("")
	if err != nil || got != local {
		t.Fatalf("want local backend, got %v err %v", got, err)
	}
}

func TestBackendSelector_Pick_RemoteOnNonEmptyDeviceID(t *testing.T) {
	remote := fakeBackend{}
	var gotID string
	sel := NewBackendSelector(fakeBackend{}, func(id string) (PTYBackend, error) {
		gotID = id
		return remote, nil
	})
	got, err := sel.Pick("42")
	if err != nil || got != remote || gotID != "42" {
		t.Fatalf("want remote backend for id 42, got %v id %q err %v", got, gotID, err)
	}
}
```

- [ ] **Step 2: 跑测试看 RED**

Run: `go test ./internal/service/terminal_svc/ -run TestBackendSelector_Pick`
Expected: 编译失败(`Pick` 仍要 `*agent_backend_entity.AgentBackend`)。

- [ ] **Step 3: 改实现(GREEN)**

`backend.go`:删 `agent_backend_entity` import,改:
```go
func (s *BackendSelector) Pick(deviceID string) (PTYBackend, error) {
	if deviceID == "" {
		return s.local, nil
	}
	return s.remoteFactor(deviceID)
}
```
`ErrNoBackend` 若不再被引用则删(先 `grep -rn ErrNoBackend ./` 确认)。

- [ ] **Step 4: 跑测试(与 Task 1 连做后统一 GREEN)**

Run: `go test ./internal/service/terminal_svc/ -run TestBackendSelector_Pick`
Expected: PASS(service.go 调用点未改前整包可能编译断 —— Task 1 修复后统一跑;提交也与 Task 1 合并)。

---

### Task 1: `Service` 以 terminalID 索引,`Open` 直收 deviceID/cwd

**Files:**
- Modify: `internal/service/terminal_svc/service.go`, `emitter.go`
- Test: `service_test.go`, `open_preempt_test.go`, `pump_exit_race_test.go`, `integration_test.go`, `emitter_test.go`

- [ ] **Step 1: emitter(RED→GREEN 小步)**

`emitter.go`:
```go
func DataEventName(terminalID string) string { return fmt.Sprintf("terminal:%s:data", terminalID) }
func ExitEventName(terminalID string) string { return fmt.Sprintf("terminal:%s:exit", terminalID) }
```
`emitter_test.go`:`DataEventName("t1") == "terminal:t1:data"`、`ExitEventName("t1") == "terminal:t1:exit"`。

- [ ] **Step 2: 改 service 测试(RED)**

把所有 int64 key 改成 string terminalID,**删 `SessionLookup` mock**,`Open` 直接喂 deviceID/cwd:
```go
func TestService_Open_LocalEmitsDataAndExit(t *testing.T) {
	h := newFakeHandle()              // 复用既有 fake handle
	be := fakeBackend{handle: h}
	em := newCapturingEmitter()       // 复用既有 capturing emitter
	svc := NewService(NewBackendSelector(be, nil), em) // ← 不再传 lookup
	if err := svc.Open(context.Background(), "t1", "", "/tmp", 80, 24); err != nil {
		t.Fatalf("open: %v", err)
	}
	h.pushData([]byte("hello"))
	em.waitData(t, "terminal:t1:data", "hello")
	h.pushExit(pty.ExitInfo{Code: 0, Reason: "natural"})
	em.waitExit(t, "terminal:t1:exit")
	if svc.lookupHandle("t1") != nil {
		t.Fatal("handle should be cleared after exit")
	}
}

func TestService_Write_UnknownTerminalReturnsClosed(t *testing.T) {
	svc := NewService(NewBackendSelector(fakeBackend{}, nil), NoopEmitter{})
	if err := svc.Write(context.Background(), "ghost", "x"); !errors.Is(err, ErrTerminalClosed) {
		t.Fatalf("want ErrTerminalClosed, got %v", err)
	}
}
```
`open_preempt_test.go` / `pump_exit_race_test.go` / `integration_test.go`:key 数字→字符串(`1`→`"1"`),删 SessionLookup/ErrSessionNotFound setup,`Open` 调用补 `deviceID, cwd`。
> fake helper(`fakeBackend`/`newFakeHandle`/`newCapturingEmitter`)沿用 service_test.go 既有等价物,**不引新依赖**。

- [ ] **Step 3: 跑测试看 RED**

Run: `go test ./internal/service/terminal_svc/`
Expected: 编译失败(`NewService` 3 参 / `Open` 4 参 / key int64)。

- [ ] **Step 4: 改 service.go(GREEN)**

- `sessions map[int64]pty.Handle` → `map[string]pty.Handle`;`inFlight map[int64]*openAttempt` → `map[string]*openAttempt`。
- 删 `SessionLookup` 接口、`ErrSessionNotFound`、`lookup` 字段;删 `import chat_entity / agent_backend_entity`。
- `NewService(sel *BackendSelector, emitter Emitter)`(去掉 lookup 形参与赋值)。
- `Open`:
  ```go
  func (s *Service) Open(ctx context.Context, terminalID string, deviceID string, cwd string, cols, rows uint16) error {
  	backend, err := s.selector.Pick(deviceID)
  	if err != nil {
  		return err
  	}
  	// 以下 evict / inFlight 注册 / backend.Open / 抢占判定 / go s.pump 原样保留,
  	// 仅 key sessionID→terminalID,backend.Open 用 pty.Spec{Cwd: cwd, Cols: cols, Rows: rows}。
  	...
  }
  ```
  (删原 `s.lookup.Lookup(...)` 与 `sess==nil` 分支。)
- `Write/Resize/Close/lookupHandle/pump/Shutdown`:`sessionID int64` → `terminalID string`;`pump` 内 `DataEventName(terminalID)`/`ExitEventName(terminalID)`。

- [ ] **Step 5: 跑测试看 GREEN**

Run: `go test -race ./internal/service/terminal_svc/`
Expected: PASS(`internal/app` 未改,**整仓 build 仍断** —— Task 3 修)。

- [ ] **Step 6: Commit**

```bash
git add internal/service/terminal_svc/
git commit -m "refactor(terminal_svc): key terminals by opaque terminalID, decouple from session"
```

---

# Phase 1 — 后端:项目 cwd 解析 + app 绑定

### Task 2: `project_svc.ResolveProjectCwd`

**Files:**
- Modify: `internal/service/project_svc/project.go`(接口), `cwd.go`(实现)
- Test: `internal/service/project_svc/cwd_terminal_test.go`(新建)

- [ ] **Step 1: 写失败测试(RED)** — 参照同目录 `cwd_test.go` 的 `setupCwdTest` / mock repo 范式

```go
func TestResolveProjectCwd(t *testing.T) {
	convey.Convey("ResolveProjectCwd", t, func() {
		convey.Convey("本地: deviceID 空 → project.Path", func() {
			ctx, mocks, svc := setupCwdTest(t)
			mocks.proj.EXPECT().Find(ctx, int64(7)).Return(
				&project_entity.Project{ID: 7, Path: "/repo", Status: consts.ACTIVE}, nil)
			cwd, err := svc.ResolveProjectCwd(ctx, 7, "")
			convey.So(err, convey.ShouldBeNil)
			convey.So(cwd, convey.ShouldEqual, "/repo")
		})
		convey.Convey("本地: 项目不存在 → 报错", func() {
			ctx, mocks, svc := setupCwdTest(t)
			mocks.proj.EXPECT().Find(ctx, int64(7)).Return(nil, nil)
			_, err := svc.ResolveProjectCwd(ctx, 7, "")
			convey.So(err, convey.ShouldNotBeNil)
		})
		convey.Convey("远端: 命中 → loc.Path", func() {
			ctx, mocks, svc := setupCwdTest(t)
			mocks.loc.EXPECT().FindByProjectAndDevice(ctx, int64(7), "42").Return(
				&project_location_entity.ProjectLocation{Path: "/remote/repo"}, nil)
			cwd, err := svc.ResolveProjectCwd(ctx, 7, "42")
			convey.So(err, convey.ShouldBeNil)
			convey.So(cwd, convey.ShouldEqual, "/remote/repo")
		})
		convey.Convey("远端: 未配置 → ProjectLocationMissing", func() {
			ctx, mocks, svc := setupCwdTest(t)
			mocks.loc.EXPECT().FindByProjectAndDevice(ctx, int64(7), "42").Return(nil, gorm.ErrRecordNotFound)
			_, err := svc.ResolveProjectCwd(ctx, 7, "42")
			convey.So(err, convey.ShouldNotBeNil)
		})
	})
}
```
> 若 `setupCwdTest` 只注入了 project repo,扩展它注入 `project_location_repo` mock(`project_location_repo.RegisterProjectLocation(mockLoc)`,mock 包 `mock_project_location_repo`)。

- [ ] **Step 2: 跑测试看 RED**

Run: `go test ./internal/service/project_svc/ -run TestResolveProjectCwd`
Expected: FAIL —— `svc.ResolveProjectCwd undefined`。

- [ ] **Step 3: 加实现(GREEN)**

`project.go` 接口 `// cwd` 段加:
```go
ResolveProjectCwd(ctx context.Context, projectID int64, deviceID string) (string, error)
```
`cwd.go`(import `errors`、`gorm.io/gorm`、`project_location_repo`):
```go
// ResolveProjectCwd 解析「某 device 下某项目」的终端工作目录。
//   deviceID == ""  → project.Path(本地);项目不存在/软删 → ProjectNotFound
//   deviceID != ""  → project_location.FindByProjectAndDevice;未配置 → ProjectLocationMissing
func (s *projectSvc) ResolveProjectCwd(ctx context.Context, projectID int64, deviceID string) (string, error) {
	if deviceID == "" {
		p, err := project_repo.Project().Find(ctx, projectID)
		if err != nil {
			return "", err
		}
		if p == nil || !p.IsActive() {
			return "", i18n.NewError(ctx, code.ProjectNotFound)
		}
		return p.Path, nil
	}
	loc, err := project_location_repo.ProjectLocation().FindByProjectAndDevice(ctx, projectID, deviceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", i18n.NewError(ctx, code.ProjectLocationMissing)
		}
		return "", err
	}
	return loc.Path, nil
}
```

- [ ] **Step 4: 跑测试看 GREEN**

Run: `go test ./internal/service/project_svc/ -run TestResolveProjectCwd`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/service/project_svc/
git commit -m "feat(project_svc): add ResolveProjectCwd for terminal cwd resolution"
```

---

### Task 3: app 绑定改 terminalID + 解析 cwd + 去 sessionLookupAdapter

**Files:**
- Modify: `internal/app/terminal.go`, `internal/app/terminal_wiring.go`

- [ ] **Step 1: 改 terminal.go(无独立单测,靠 build + CHECKPOINT)**

```go
package app

import (
	"errors"

	"github.com/agentre-ai/agentre/internal/service/project_svc"
)

var errTerminalSvcNotInitialized = errors.New("terminal service not initialized")

func (a *App) TerminalOpen(terminalID string, projectID int64, deviceID string, cols, rows uint16) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	cwd, err := project_svc.Default().ResolveProjectCwd(a.ctx, projectID, deviceID)
	if err != nil {
		return err
	}
	return a.terminalSvc.Open(a.ctx, terminalID, deviceID, cwd, cols, rows)
}

func (a *App) TerminalWrite(terminalID string, data string) error {
	if a.terminalSvc == nil { return errTerminalSvcNotInitialized }
	return a.terminalSvc.Write(a.ctx, terminalID, data)
}

func (a *App) TerminalResize(terminalID string, cols, rows uint16) error {
	if a.terminalSvc == nil { return errTerminalSvcNotInitialized }
	return a.terminalSvc.Resize(a.ctx, terminalID, cols, rows)
}

func (a *App) TerminalClose(terminalID string) error {
	if a.terminalSvc == nil { return errTerminalSvcNotInitialized }
	return a.terminalSvc.Close(a.ctx, terminalID)
}
```

- [ ] **Step 2: 改 terminal_wiring.go**

- 删 `sessionLookupAdapter` 整段 + 其专用 import(`chatrepo`/`agentrepo`/`agentbackendrepo`/`chat_entity`/`agent_backend_entity`)。
- `ptyBackendAdapter` + remoteFactory(用 `chat_svc.BorrowDeviceClient`)**保留**。
- `newTerminalService` 末行:`return terminal_svc.NewService(selector, emitter)`。
- 保留 import:`strconv`、`wailsruntime`、`pty`/`local`/`remote`、`chat_svc`、`terminal_svc`。

- [ ] **Step 3: 整仓编译**

Run: `go build ./...`
Expected: 通过。若报 `ErrSessionNotFound`/`SessionLookup` 未定义:`grep -rn "ErrSessionNotFound\|SessionLookup" ./internal` 清漏网点。

- [ ] **Step 4: 重新生成 wails 绑定**

Run: `make generate`
确认 `frontend/wailsjs/go/app/App.d.ts`:
```
export function TerminalOpen(arg1:string,arg2:number,arg3:string,arg4:number,arg5:number):Promise<void>;
export function TerminalWrite(arg1:string,arg2:string):Promise<void>;
export function TerminalResize(arg1:string,arg2:number,arg3:number):Promise<void>;
export function TerminalClose(arg1:string):Promise<void>;
```

- [ ] **Step 5: Commit**

```bash
git add internal/app/terminal.go internal/app/terminal_wiring.go frontend/wailsjs/
git commit -m "feat(app): terminal bindings keyed by terminalID, resolve project cwd"
```

> ### ✅ CHECKPOINT 1 — STOP
> 汇报并验证:`go build ./...` 通过;`go test -race ./internal/service/terminal_svc/... ./internal/service/project_svc/... ./internal/app/...` 全绿;贴出新 `App.d.ts` 终端签名。等确认后进 Phase 2。

---

# Phase 2 — 前端:终端 tab 模型 + 面板接线

### Task 4: `chat-tabs-store` 加 terminal tab + `openTerminal`

**Files:**
- Modify: `frontend/src/stores/chat-tabs-store.ts`
- Test: `frontend/src/stores/chat-tabs-store.test.ts`(无则新建)

- [ ] **Step 1: 写失败测试(RED)**

```ts
import { useChatTabsStore, __setNextIdFactoryForTesting, __setNowForTesting } from "./chat-tabs-store";

beforeEach(() => {
  useChatTabsStore.setState({ tabs: [], activeTabId: null });
  __setNowForTesting(() => 1000);
});

it("openTerminal 新增 terminal tab 并激活", () => {
  let n = 0;
  __setNextIdFactoryForTesting(() => `id-${++n}`);
  useChatTabsStore.getState().openTerminal(7, "", undefined);
  const s = useChatTabsStore.getState();
  expect(s.tabs).toHaveLength(1);
  expect(s.tabs[0].meta).toMatchObject({ kind: "terminal", projectId: 7, deviceId: "" });
  expect((s.tabs[0].meta as { terminalId: string }).terminalId).toBeTruthy();
  expect(s.tabs[0].title).toBe("终端");
  expect(s.tabs[0].isPreview).toBe(false);
  expect(s.activeTabId).toBe(s.tabs[0].id);
});

it("openTerminal 远端带设备名进标题", () => {
  useChatTabsStore.getState().openTerminal(7, "42", "MacMini");
  const tab = useChatTabsStore.getState().tabs.at(-1)!;
  expect(tab.meta).toMatchObject({ kind: "terminal", deviceId: "42" });
  expect(tab.title).toBe("终端 · MacMini");
});
```

- [ ] **Step 2: 跑测试看 RED**

Run: `cd frontend && pnpm test -- src/stores/chat-tabs-store.test.ts`
Expected: FAIL —— `openTerminal is not a function`。

- [ ] **Step 3: 改实现(GREEN)**

`chat-tabs-store.ts`:
- `TabKind` 加变体:
  ```ts
  export type TabKind =
    | { kind: "session"; sessionId: number }
    | { kind: "new"; projectId: number; agentId: number; workMode: string }
    | { kind: "terminal"; projectId: number; deviceId: string; terminalId: string };
  ```
- `Actions` 加 `openTerminal: (projectId: number, deviceId: string, deviceName?: string) => void;`
- 实现(复用 `nextId()`/`now()`):
  ```ts
  openTerminal: (projectId, deviceId, deviceName) =>
    set((state) => {
      const newTab: ChatTab = {
        id: nextId(),
        meta: { kind: "terminal", projectId, deviceId, terminalId: nextId() },
        isPreview: false,
        isPinned: false,
        pinAt: 0,
        openedAt: now(),
        title: deviceName ? `终端 · ${deviceName}` : "终端",
      };
      return { tabs: [...state.tabs, newTab], activeTabId: newTab.id };
    }),
  ```

- [ ] **Step 4: 跑测试看 GREEN**

Run: `cd frontend && pnpm test -- src/stores/chat-tabs-store.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/stores/chat-tabs-store.ts frontend/src/stores/chat-tabs-store.test.ts
git commit -m "feat(chat-tabs): add terminal tab kind and openTerminal action"
```

---

### Task 5: 持久化升级到 v2(持久化 terminal tab)

**Files:**
- Modify: `frontend/src/stores/chat-tabs-persistence.ts`
- Test: `frontend/src/stores/chat-tabs-persistence.test.ts`(无则新建)

> 现状:`PersistedV1` 是 flat、仅 session 的格式。要持久化 terminal tab,升级到带 `meta` 的 v2;并保留 v1 读取(老 localStorage 里的 session tab 不丢)。

- [ ] **Step 1: 写失败测试(RED)**

```ts
import { writePersistedTabs, readPersistedTabs, CHAT_TABS_STORAGE_KEY } from "./chat-tabs-persistence";

beforeEach(() => localStorage.clear());

it("terminal tab 往返持久化不被丢弃", () => {
  const tab = {
    id: "t1",
    meta: { kind: "terminal" as const, projectId: 7, deviceId: "", terminalId: "term-1" },
    isPreview: false, isPinned: false, pinAt: 0, openedAt: 1, title: "终端",
  };
  writePersistedTabs([tab], "t1");
  const restored = readPersistedTabs();
  expect(restored?.tabs).toHaveLength(1);
  expect(restored?.tabs[0].meta).toMatchObject({ kind: "terminal", projectId: 7, deviceId: "", terminalId: "term-1" });
});

it("session tab 仍能往返", () => {
  const tab = {
    id: "s1", meta: { kind: "session" as const, sessionId: 9 },
    isPreview: false, isPinned: true, pinAt: 5, openedAt: 1,
  };
  writePersistedTabs([tab], "s1");
  expect(readPersistedTabs()?.tabs[0].meta).toMatchObject({ kind: "session", sessionId: 9 });
});

it("旧 v1 数据仍可读为 session tab", () => {
  localStorage.setItem(CHAT_TABS_STORAGE_KEY, JSON.stringify({
    v: 1, activeTabId: "x",
    tabs: [{ id: "x", sessionId: 3, isPinned: false, pinAt: 0, openedAt: 1 }],
  }));
  expect(readPersistedTabs()?.tabs[0].meta).toMatchObject({ kind: "session", sessionId: 3 });
});
```

- [ ] **Step 2: 跑测试看 RED**

Run: `cd frontend && pnpm test -- src/stores/chat-tabs-persistence.test.ts`
Expected: FAIL(terminal tab 不被写入;v2 读取不存在)。

- [ ] **Step 3: 改实现(GREEN)** — 重写 `chat-tabs-persistence.ts`

```ts
import type { ChatTab, TabKind } from "./chat-tabs-store";

export const CHAT_TABS_STORAGE_KEY = "agentre.chatTabs";

type PersistMeta = Extract<TabKind, { kind: "session" } | { kind: "terminal" }>;

type PersistedTabV2 = {
  id: string;
  meta: PersistMeta;
  isPinned: boolean;
  pinAt: number;
  openedAt: number;
  title?: string;
};
type PersistedV2 = { v: 2; tabs: PersistedTabV2[]; activeTabId: string | null };

// 旧格式:flat、仅 session。
type PersistedV1 = {
  v: 1;
  tabs: Array<{ id: string; sessionId: number; isPinned: boolean; pinAt: number; openedAt: number }>;
  activeTabId: string | null;
};

function getStorage(): Storage | null {
  if (typeof window === "undefined") return null;
  try { return window.localStorage; } catch { return null; }
}

// 哪些 tab 进持久化:session(非 preview)与 terminal;new / preview 不持久化。
function persistable(t: ChatTab): PersistMeta | null {
  if (t.meta.kind === "session" && !t.isPreview) return t.meta;
  if (t.meta.kind === "terminal") return t.meta;
  return null;
}

export function writePersistedTabs(tabs: ChatTab[], activeTabId: string | null): void {
  const storage = getStorage();
  if (!storage) return;
  const out: PersistedV2 = {
    v: 2,
    tabs: tabs.flatMap((t) => {
      const meta = persistable(t);
      if (!meta) return [];
      return [{ id: t.id, meta, isPinned: t.isPinned, pinAt: t.pinAt, openedAt: t.openedAt, title: t.title }];
    }),
    activeTabId,
  };
  try { storage.setItem(CHAT_TABS_STORAGE_KEY, JSON.stringify(out)); } catch { /* quota / private */ }
}

function toChatTab(id: string, meta: PersistMeta, isPinned: boolean, pinAt: number, openedAt: number, title?: string): ChatTab {
  return { id, meta, isPreview: false, isPinned, pinAt, openedAt, title };
}

export function readPersistedTabs(): { tabs: ChatTab[]; activeTabId: string | null } | null {
  const storage = getStorage();
  if (!storage) return null;
  let raw: string | null;
  try { raw = storage.getItem(CHAT_TABS_STORAGE_KEY); } catch { return null; }
  if (!raw) return null;
  let parsed: unknown;
  try { parsed = JSON.parse(raw); } catch { return null; }
  if (!parsed || typeof parsed !== "object") return null;
  const v = (parsed as { v?: number }).v;

  if (v === 2) {
    const p = parsed as PersistedV2;
    const tabs: ChatTab[] = [];
    for (const r of p.tabs ?? []) {
      const m = r.meta;
      if (m?.kind === "session" && typeof m.sessionId === "number") {
        tabs.push(toChatTab(r.id, { kind: "session", sessionId: m.sessionId }, r.isPinned, r.pinAt, r.openedAt, r.title));
      } else if (
        m?.kind === "terminal" &&
        typeof m.projectId === "number" && typeof m.deviceId === "string" && typeof m.terminalId === "string"
      ) {
        tabs.push(toChatTab(
          r.id,
          { kind: "terminal", projectId: m.projectId, deviceId: m.deviceId, terminalId: m.terminalId },
          r.isPinned, r.pinAt, r.openedAt, r.title,
        ));
      }
    }
    return { tabs, activeTabId: p.activeTabId };
  }

  if (v === 1) {
    const p = parsed as PersistedV1;
    return {
      tabs: (p.tabs ?? []).map((r) =>
        toChatTab(r.id, { kind: "session", sessionId: r.sessionId }, r.isPinned, r.pinAt, r.openedAt)),
      activeTabId: p.activeTabId,
    };
  }
  return null;
}
```
> 注:`TabKind` 需从 `chat-tabs-store` 导出(已 `export type TabKind`)。

- [ ] **Step 4: 跑测试看 GREEN**

Run: `cd frontend && pnpm test -- src/stores/chat-tabs-persistence.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/stores/chat-tabs-persistence.ts frontend/src/stores/chat-tabs-persistence.test.ts
git commit -m "feat(chat-tabs): persist terminal tabs (v2 format with meta union)"
```

---

### Task 6: `use-terminal` + `terminal-panel` 按 terminalID,去 chat-terminal-store

**Files:**
- Modify: `frontend/src/components/agentre/terminal/use-terminal.ts`, `terminal-panel.tsx`
- Test: `frontend/src/components/agentre/terminal/use-terminal.test.ts`(无则新建)

- [ ] **Step 1: 写失败测试(RED)**

mock `@/../wailsjs/go/app/App` 与 `@/../wailsjs/runtime/runtime`,用 `renderHook` 渲染 `useTerminal({ terminalID:"t1", projectId:7, deviceId:"", cols:80, rows:24 })`,断言:
- `App.TerminalOpen` 以 `("t1", 7, "", 80, 24)` 被调用;
- `EventsOn` 注册了 `"terminal:t1:data"` 与 `"terminal:t1:exit"`;
- `write("x")` → `App.TerminalWrite("t1","x")`;
- 卸载 → `App.TerminalClose("t1")`。

- [ ] **Step 2: 跑测试看 RED**

Run: `cd frontend && pnpm test -- src/components/agentre/terminal/use-terminal.test.ts`
Expected: FAIL(签名/参数不符)。

- [ ] **Step 3: 改实现(GREEN)**

`use-terminal.ts`:
- 删 `import { useChatTerminalStore }` 及所有 `setTransitioning(...)`。
- `UseTerminalArgs`:`sessionID: number` → `terminalID: string; projectId: number; deviceId: string`(cols/rows/onData/onExit 不变)。
- 事件名:``terminal:${args.terminalID}:data` / `:exit``。
- `App.TerminalOpen(args.terminalID, args.projectId, args.deviceId, args.cols, args.rows)`;`write`→`TerminalWrite(args.terminalID, data)`;`resize`→`TerminalResize(args.terminalID, cols, rows)`;cleanup→`TerminalClose(args.terminalID)`。
- effect deps:`[args.terminalID]`。

`terminal-panel.tsx`:
- 删 `import { useChatTerminalStore }` 与 `const toggle = ...`。
- props:`{ terminalID: string; projectId: number; deviceId: string; onClose: () => void }`。
- `dismissAndClose` 与 `onExit` 内原 `toggle(sessionID)` 全改 `onClose()`;`connection_lost` 仍只显示 banner(不自动关)。
- `useTerminal({ terminalID, projectId, deviceId, cols:80, rows:24, onData, onExit })`。

- [ ] **Step 4: 跑测试看 GREEN**

Run: `cd frontend && pnpm test -- src/components/agentre/terminal/use-terminal.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/terminal/
git commit -m "refactor(terminal): drive panel by terminalID with onClose, drop chat-terminal-store"
```

---

### Task 7: `chat-panel-host` 渲染终端 tab

**Files:**
- Modify: `frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx`
- Test: `frontend/src/components/agentre/chat-tabs/__tests__/chat-panel-host.test.tsx`(已存在)

- [ ] **Step 1: 写失败测试(RED)**

参考既有 `__tests__/chat-panel-host.test.tsx` 的 `openNewSession` 用法,加:用 `openTerminal(7, "", undefined)` 放一个 terminal tab,渲染 `<ChatPanelHost/>`,断言出现 `data-testid="terminal-panel"`。

- [ ] **Step 2: 跑测试看 RED**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-tabs/__tests__/chat-panel-host.test.tsx`
Expected: FAIL(terminal tab 当前走 `sid=0` 的 ChatPanel)。

- [ ] **Step 3: 改实现(GREEN)**

`chat-panel-host.tsx`:
- import `TerminalPanel`(`from "../terminal/terminal-panel"`)、`type TabKind`。
- `ChatPanelHost` 的 `.map`:
  ```tsx
  {panelTabs.map((t) =>
    t.meta.kind === "terminal" ? (
      <HostedTerminalPanel key={t.id} tab={t} active={t.id === activeTabId} />
    ) : (
      <HostedPanel key={t.id} tab={t} active={t.id === activeTabId} />
    ),
  )}
  ```
- 新增组件(同文件):
  ```tsx
  function HostedTerminalPanel({ tab, active }: { tab: ChatTab; active: boolean }) {
    const closeTab = useChatTabsStore((s) => s.closeTab);
    const meta = tab.meta as Extract<TabKind, { kind: "terminal" }>;
    return (
      <div
        data-tab-id={tab.id}
        data-active={active}
        style={{ display: active ? "flex" : "none" }}
        className="flex h-full min-h-0 flex-1 flex-col"
      >
        <TerminalPanel
          terminalID={meta.terminalId}
          projectId={meta.projectId}
          deviceId={meta.deviceId}
          onClose={() => closeTab(tab.id)}
        />
      </div>
    );
  }
  ```

- [ ] **Step 4: 跑测试看 GREEN**

Run: `cd frontend && pnpm test -- src/components/agentre/chat-tabs/__tests__/chat-panel-host.test.tsx`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx frontend/src/components/agentre/chat-tabs/__tests__/chat-panel-host.test.tsx
git commit -m "feat(chat-tabs): render terminal tabs via HostedTerminalPanel"
```

---

### Task 8: tab 条终端图标

**Files:**
- Modify: 终端图标渲染处(**先 `grep -rn "ICON_BY_KIND\|MessageSquare\|meta.kind" frontend/src/components/agentre/chat-tabs/` 定位** —— 可能在 `tab-strip.tsx` 或 `use-tabs-view`,不一定在 `tab.tsx`)
- Test: 对应组件测试

- [ ] **Step 1: 写失败测试(RED)**

给一个 `kind:"terminal"` tab,断言其图标为 `TerminalSquare`(用 `data-testid` 或 svg class,参考既有图标断言方式)。

- [ ] **Step 2: 跑测试看 RED**

Run: `cd frontend && pnpm test -- <对应 test 路径>`
Expected: FAIL(回退成默认图标)。

- [ ] **Step 3: 改实现(GREEN)**

在 kind→图标的映射里加 `terminal: TerminalSquare`(从 `lucide-react` import `TerminalSquare`);标题用 `tab.title`(终端 tab 已设)。

- [ ] **Step 4: 跑测试看 GREEN**

Run: `cd frontend && pnpm test -- <对应 test 路径>`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add <改动文件> <test 文件>
git commit -m "feat(chat-tabs): terminal tab icon"
```

---

# Phase 3 — 前端:项目菜单 + 移除会话内终端

### Task 9: `ProjectSelection` 加 `open-terminal` + `selectOnTab` 接 `openTerminal`

**Files:**
- Modify: `frontend/src/components/agentre/project-page.tsx`
- Test: `frontend/src/components/agentre/__tests__/project-page.test.tsx`(已存在)

- [ ] **Step 1: 写失败测试(RED)**

断言:`selectOnTab({ kind: "open-terminal", projectID: 7, deviceID: "42", deviceName: "Mac" })` 后,`useChatTabsStore` 多了一个 `{kind:"terminal", projectId:7, deviceId:"42"}` tab。若 `selectOnTab` 不易直接触发,改 Task 10 的菜单点击间接覆盖(则本 Task 仅做实现 + 跟随 Task 10 测试)。

- [ ] **Step 2: 跑测试看 RED**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/project-page.test.tsx`
Expected: FAIL(`open-terminal` 不是合法 ProjectSelection / 无 tab 新增)。

- [ ] **Step 3: 改实现(GREEN)**

`project-page.tsx`:
- `ProjectSelection` 联合(line ~84)加:
  ```ts
  | { kind: "open-terminal"; projectID: number; deviceID: string; deviceName?: string }
  ```
- 取 store action(挨着 `openSession`/`openNewSession`):`const openTerminal = useChatTabsStore((s) => s.openTerminal);`
- `selectOnTab`(line ~223)在 `sel.kind === "new"` 之前加:
  ```ts
  if (sel.kind === "open-terminal") {
    openTerminal(sel.projectID, sel.deviceID, sel.deviceName);
    return;
  }
  ```
- `useCallback` deps 补 `openTerminal`。

- [ ] **Step 4: 跑测试看 GREEN**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/project-page.test.tsx`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/project-page.tsx frontend/src/components/agentre/__tests__/project-page.test.tsx
git commit -m "feat(project-page): route open-terminal selection to openTerminal"
```

---

### Task 10: 项目菜单「新建终端」子菜单

**Files:**
- Modify: `frontend/src/components/agentre/project-page.tsx`
- Test: `frontend/src/components/agentre/__tests__/project-page.test.tsx`

- [ ] **Step 1: 写失败测试(RED)**

mock `useRemoteDevices`(返回若干 device,含 online/offline)与 `App.ProjectLocationList`(返回已配 deviceId)。断言:
- 打开项目「更多操作」菜单 → 有「新建终端」;
- 展开子菜单 → 有「本地」;点击 → `onSelect` 收到 `{kind:"open-terminal", projectID, deviceID:""}`(或最终 store 多一个本地 terminal tab);
- online 且已配 location 的 device → 可点;点击 → `deviceID=String(id)`、`deviceName` 正确;
- online 但未配 location 的 device → 渲染 disabled(`aria-disabled`/`data-disabled`);offline device → disabled。

- [ ] **Step 2: 跑测试看 RED**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/project-page.test.tsx`
Expected: FAIL(无「新建终端」)。

- [ ] **Step 3: 改实现(GREEN)**

`project-page.tsx`:
- import:
  ```ts
  import {
    DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,
    DropdownMenuSub, DropdownMenuSubTrigger, DropdownMenuSubContent, DropdownMenuSeparator,
  } from "@/components/ui/dropdown-menu";
  import { TerminalSquare } from "lucide-react";
  import { useRemoteDevices } from "./remote-devices/use-remote-devices";
  ```
  > 确认 `@/components/ui/dropdown-menu` 已导出 `DropdownMenuSub*`/`DropdownMenuSeparator`;缺则按 shadcn 约定补(`/shadcn` skill),不要手写 Radix。
  > `ProjectLocationList` 从 `WailsApp`(`import * as WailsApp from "../../../wailsjs/go/app/App"`,文件已有)取。
- `ProjectCard` 的 `DropdownMenuContent`(line ~973)在「新建子项目」后加:
  ```tsx
  <NewTerminalSubMenu
    projectID={project.id}
    onPick={(deviceID, deviceName) =>
      onSelect({ kind: "open-terminal", projectID: project.id, deviceID, deviceName })
    }
  />
  ```
- 新增组件(同文件,参考 `NewSessionMenu` lazy-load 写法):
  ```tsx
  function NewTerminalSubMenu({
    projectID, onPick,
  }: { projectID: number; onPick: (deviceID: string, deviceName?: string) => void }) {
    const { devices } = useRemoteDevices();
    const [configured, setConfigured] = React.useState<Set<string> | null>(null);
    const loadLocations = React.useCallback(() => {
      void WailsApp.ProjectLocationList(projectID).then((rows) =>
        setConfigured(new Set((rows ?? []).map((r) => r.deviceID))), // 字段名以 wailsjs/go/models 为准
      );
    }, [projectID]);
    return (
      <DropdownMenuSub onOpenChange={(open) => { if (open && configured === null) loadLocations(); }}>
        <DropdownMenuSubTrigger>
          <TerminalSquare className="size-3.5" aria-hidden="true" />
          新建终端
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent>
          <DropdownMenuItem onSelect={() => onPick("", undefined)}>本地</DropdownMenuItem>
          {devices.length > 0 ? <DropdownMenuSeparator /> : null}
          {devices.map((d) => {
            const id = String(d.id);
            const hasPath = configured?.has(id) ?? false;
            const disabled = !d.online || !hasPath;
            return (
              <DropdownMenuItem
                key={id}
                disabled={disabled}
                title={!d.online ? "设备离线" : !hasPath ? "先在项目设置配置远端路径" : undefined}
                onSelect={() => { if (!disabled) onPick(id, d.name); }}
              >
                {d.name}{!d.online ? " · 离线" : !hasPath ? " · 未配置路径" : ""}
              </DropdownMenuItem>
            );
          })}
        </DropdownMenuSubContent>
      </DropdownMenuSub>
    );
  }
  ```

- [ ] **Step 4: 跑测试看 GREEN**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/project-page.test.tsx`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/project-page.tsx frontend/src/components/agentre/__tests__/project-page.test.tsx
git commit -m "feat(project-page): 新建终端 submenu (local + online devices, grey-out unconfigured)"
```

---

### Task 11: 移除会话内终端 + 删除 chat-terminal-store

**Files:**
- Modify: `frontend/src/components/agentre/chat-panel.tsx`
- Delete: `frontend/src/stores/chat-terminal-store.ts`(及其测试,若有)
- Test: `frontend/src/components/agentre/__tests__/chat-panel.test.tsx`

- [ ] **Step 1: 写失败测试(RED)**

在 chat-panel 测试里断言:渲染后**不存在** `title` 含「终端」的 toggle 按钮(原 `title="终端 (⌘`)"`)。

- [ ] **Step 2: 跑测试看 RED**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/chat-panel.test.tsx`
Expected: FAIL(按钮仍在)。

- [ ] **Step 3: 改实现(GREEN)**

`chat-panel.tsx`:
- 删 `import { useChatTerminalStore }`、`import { TerminalPanel }` 及派生 `isTerminalOpen/isTerminalTransitioning/toggleTerminal/terminalOn/terminalTransitioning`。
- 删终端 toggle 按钮 JSX(TerminalSquare,⌘\` title)。
- 删 ⌘\` 键盘快捷键分支。
- `terminalOn ? <TerminalPanel sessionID=.../> : <ChatContent/>` → 直接渲染 `<ChatContent/>`(保留原 else 分支)。

- [ ] **Step 4: 删除 store + 清理引用**

```bash
cd frontend && grep -rn "chat-terminal-store\|useChatTerminalStore" src/
```
清掉残余引用(预期仅剩 chat-panel.tsx,已在 Step 3 处理;若有 app 关停处调 `closeAll` 也删 —— 关闭已由「关 tab → HostedTerminalPanel 卸载 → TerminalClose」+ 后端 `Shutdown()` 兜底)。然后:
```bash
git rm frontend/src/stores/chat-terminal-store.ts
# 若存在 chat-terminal-store.test.ts 一并 git rm
```

- [ ] **Step 5: 全量前端测试**

Run: `cd frontend && pnpm test`
Expected: 全绿(引用 store 的旧测试同步删/改)。

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/chat-panel.tsx frontend/src/stores/
git commit -m "refactor(chat-panel): remove in-session terminal toggle and chat-terminal-store"
```

> ### ✅ CHECKPOINT 2 — STOP
> 汇报并验证:`cd frontend && pnpm test` 全绿;`make dev` 手动冒烟:项目菜单→新建终端(本地)→新 tab 出现、可输入命令;开两个终端互不串;关 tab 后进程结束;tab 条终端图标正确;离线/未配路径 device 置灰。等确认后进 Phase 4。

---

# Phase 4 — 全量验证 + 收尾

### Task 12: 全量 check + 手动回归

- [ ] **Step 1: 全量 check**

Run: `make generate && make check`
Expected: golangci-lint + ESLint 通过;`go test -race ./...` + Vitest 全绿。

- [ ] **Step 2: 持久化手动回归**

`make dev` → 开 1 个终端 tab → 完全退出再起 → 终端 tab 仍在、显示空白新 shell(PTY 重 spawn,历史丢失,符合决策 3)。

- [ ] **Step 3: 远端冒烟(若有在线已配路径 device)**

项目菜单→新建终端→选该 device → 终端在远端 cwd 启动;关 daemon → 面板显示「连接已断开」banner。

- [ ] **Step 4: scope 自检**

```bash
git status   # 确认未误改 chat.go/chat_test.go(任务前就存在的无关改动)
git log --oneline origin/main..HEAD
```
确认 diff 仅限本计划涉及文件;无顺手 refactor / 格式化漂移。

---

## 测试策略汇总

- **Go(`-race`)**:`terminal_svc`(terminalID 重写 + Pick + 事件名)、`project_svc.ResolveProjectCwd`(本地/远端/缺失三态)。
- **Vitest**:`openTerminal`(T4)、持久化 v2 往返 + v1 兼容(T5)、`use-terminal` 接线(T6)、host 分支(T7)、tab 图标(T8)、open-terminal 路由(T9)、项目子菜单含置灰(T10)、chat-panel 移除回归(T11)。

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| 删 `SessionLookup`/`ErrSessionNotFound` 漏改引用 → 编译断 | T3 Step 3 `go build ./...` + grep 兜底。 |
| 持久化升级遗漏 → 重启后终端 tab 被丢 / 老 session tab 被清 | T5 三个往返/兼容用例(terminal、session、v1)。 |
| `terminal_wiring.go` import 误删 remoteFactory 仍需的 `BorrowDeviceClient` | T3 Step 2 列「保留 import」清单。 |
| `DropdownMenuSub*` 未从 ui/dropdown-menu 导出 | T10 Step 3 先确认,缺则按 shadcn 约定补。 |
| `ProjectLocationView` 字段名(`deviceID` vs `DeviceID`)与 wails 生成不符 | 实现前查 `frontend/wailsjs/go/models` 里实际字段名,以生成为准。 |
| 终端图标映射位置不在 `tab.tsx` | T8 先 grep 定位 kind→图标处再改。 |
| 多项目卡片导致 locations 过度请求 | 子菜单 `onOpenChange` lazy 加载,`configured===null` 才拉一次。 |
| 误删 `TerminalPanel` 组件(host 仍用) | T11 只删 chat-panel 内 import/用法,不删组件文件。 |

## 不在范围

- 不绑定项目的「自由终端」。
- 远端路径配置 UI(复用现有「项目设置 → 远端路径」)。
- 终端 scrollback 跨重启持久化(PTY 重 spawn 即丢历史)。
