# 实现计划:终端独立成 Tab + 项目菜单「新建终端」

**Spec**: `docs/superpowers/specs/2026-05-29-terminal-independent-tab-design.md`
**日期**: 2026-05-29
**目标分支**: `develop/wyz`(当前)

## 状态追踪

- [ ] Phase 0 — 后端:`terminal_svc` 从 sessionID 解耦为 terminalID
- [ ] Phase 1 — 后端:`ResolveProjectCwd` + app 绑定改签名 + 重新生成 wails 绑定
- [ ] **CHECKPOINT 1** — 后端编译通过、`go test -race ./...` 绿
- [ ] Phase 2 — 前端:终端 tab 模型 + 面板按 terminalID 接线
- [ ] Phase 3 — 前端:项目菜单「新建终端」+ 移除会话内终端
- [ ] **CHECKPOINT 2** — 前端 Vitest 绿 + `make dev` 手动冒烟
- [ ] Phase 4 — 全量验证(`make check`)+ 收尾

---

## 概述

把终端从会话(session)里拆出来,变成与会话平级的独立 tab,拥有独立生命周期。后端 `terminal_svc` 不再以 `sessionID int64` 索引、也不再做 `session→agent→backend→cwd` 解析,改为以前端生成的 `terminalID string` 索引,`Open` 时直接接收已解析好的 `deviceID` 与 `cwd`。项目页的项目下拉菜单新增「新建终端 ▶」子菜单(本地 / 在线远端 device),点击新开一个终端 tab。**完全移除**会话内终端(toggle 按钮、⌘\` 快捷键、`chat-terminal-store`)。

### 已定决策(来自 spec)

1. 完全移除会话内终端;所有终端走独立 tab。
2. 新建终端可选 backend:本地 / 在线远端 device。
3. 终端 tab 进 persist;app 重启后 tab 在、PTY 重新 spawn(历史丢失)。
4. 未配置远端路径(`project_location`)的远端 device 在子菜单里置灰 + hover 提示。

### 关键事实(已核实)

- **远端 PTY 协议本就用不透明 `TerminalID string`**(`pkg/agentred/protocol.TerminalOpenParams/Result`,`internal/pkg/pty/remote/client_adapter.go` 按 terminalID demux)。只有桌面侧 `terminal_svc.Service` 用 sessionID。**daemon 协议不动。**
- 设备列表:`App.RemoteDeviceList() → []*remote_device_svc.DeviceView{ID int64, Name, Online bool, …}`;前端 `frontend/src/components/agentre/remote-devices/use-remote-devices.ts` 已有 hook。
- 远端 cwd:`project_location_repo.FindByProjectAndDevice(ctx, projectID, deviceID) → *ProjectLocation`(未命中返回 `nil, nil`);本地 cwd = `project_entity.Project.Path`。
- `App.ProjectLocationList(projectID) → []*ProjectLocationView{DeviceID, Path, DeviceName, Online}`(前端判断哪些 device 已配路径用)。

> ⚠️ 实现纪律(仓库 CLAUDE.md 强制):严格 TDD(Red→Green→Refactor);**每个文件改前先完整 Read**(本计划内的"当前代码"摘录可能不全);只动 scope 内文件,禁止顺手 refactor/重命名/格式化。

---

## 当前代码地图(改动点)

### 后端

| 文件 | 现状 | 角色 |
|---|---|---|
| `internal/service/terminal_svc/service.go` | `Service` 以 `map[int64]pty.Handle` 索引;`Open(ctx, sessionID, cols, rows)` 调 `lookup.Lookup`;有 `SessionLookup` 接口、`ErrSessionNotFound`;`pump` 发 `DataEventName(sessionID)`。 | PTY 多路复用器 |
| `internal/service/terminal_svc/backend.go` | `BackendSelector.Pick(be *agent_backend_entity.AgentBackend)`;`RemoteBackendFactory func(deviceID string)`。 | 选 local/remote backend |
| `internal/service/terminal_svc/emitter.go` | `DataEventName/ExitEventName(sessionID int64)` → `terminal:{sessionID}:data/:exit`。 | 事件名 |
| `internal/service/terminal_svc/service_test.go` | 现有用例按 sessionID + mock SessionLookup。 | 测试 |
| `internal/app/terminal.go` | `TerminalOpen/Write/Resize/Close(sessionID int64, …)` 转发到 `terminalSvc`。 | wails 绑定 |
| `internal/app/terminal_wiring.go` | `sessionLookupAdapter` 实现 `SessionLookup`;`newTerminalService` 注入 lookup+selector+emitter;remoteFactory 用 `chat_svc.BorrowDeviceClient`。 | DI 接线 |
| `internal/service/project_svc/project.go` | `ProjectSvc` 接口含 `Find`、`ResolveSessionCwd`;`Default()`。 | 项目服务 |
| `internal/service/project_svc/cwd.go` | `ResolveSessionCwd(session)`:project_id=0→AgentCwd,否则 project.Path。 | cwd 解析 |
| `internal/repository/project_location_repo/project_location.go` | `FindByProjectAndDevice(ctx, projectID, deviceID)`。 | 远端路径仓储 |

### 前端

| 文件 | 现状 | 角色 |
|---|---|---|
| `frontend/src/stores/chat-tabs-store.ts` | `TabKind = session \| new`;`ChatTab`;persist name `agentre-chat-tabs`,partialize 存 `{tabs, activeTabId}`;已 import `uuid`。 | tab 状态 |
| `frontend/src/stores/chat-terminal-store.ts` | 按 sessionID 的 `openSessionIDs`/`toggle`/`closeAll`。 | **将删除** |
| `frontend/src/components/agentre/terminal/use-terminal.ts` | 入参 `sessionID:number`;订阅 `terminal:{sessionID}:data/:exit`;调 `App.TerminalOpen/Write/Resize/Close(sessionID,…)`;状态机 opening→open→idle。 | 终端生命周期 hook |
| `frontend/src/components/agentre/terminal/terminal-panel.tsx` | props `{ sessionID:number }`;xterm 渲染 + 断线 banner。 | 终端 UI |
| `frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx` | `HostedPanel` 取 `sid = kind==="session"?sessionId:0`,渲染 `ChatPanel`。 | tab 内容宿主 |
| `frontend/src/components/agentre/chat-tabs/chat-tab-strip.tsx` (+ `.test.tsx`) | 渲染 tab 标题条。 | tab 条 |
| `frontend/src/components/agentre/chat-panel.tsx` | 终端 toggle 按钮、⌘\` 快捷键、`terminalOn ? <TerminalPanel/> : <ChatContent/>`、import `useChatTerminalStore`+`TerminalPanel`。 | **移除终端部分** |
| `frontend/src/components/agentre/project-page.tsx` | 项目卡片下拉菜单:`项目设置`、`新建子项目`(约 905-912 行附近的 `DropdownMenuContent`)。 | 加「新建终端」 |
| `frontend/src/components/agentre/remote-devices/use-remote-devices.ts` | 列出/订阅 device 在线态。 | device 数据源 |

---

## Phase 0 — 后端:`terminal_svc` 从 sessionID 解耦为 terminalID

### Task 0.1 — `BackendSelector.Pick(deviceID string)`

**RED** — 改 `internal/service/terminal_svc/service_test.go`(或新增 `backend_test.go`):
- `Pick("")` 返回注入的 local backend、err==nil。
- `Pick("42")` 调用 remoteFactory("42") 并返回其结果;factory 报错时透传。
- 跑 `go test ./internal/service/terminal_svc/...` 应编译失败(签名不符)→ 这是预期 RED。

**GREEN** — `backend.go`:
- `Pick(be *agent_backend_entity.AgentBackend)` → `Pick(deviceID string) (PTYBackend, error)`:`deviceID==""`→`s.local`,否则 `s.remote(deviceID)`。
- 删除对 `agent_backend_entity` 的 import。

### Task 0.2 — `Service` 以 terminalID 索引、`Open` 直收 deviceID/cwd

**RED** — 重写 `service_test.go` 的核心用例,以 `terminalID string` 为 key,**不再 mock `SessionLookup`**:
- `Open(ctx, "t1", "", "/tmp", 80, 24)` 用 fake local backend(返回 fake `pty.Handle`)→ 注册成功;data chunk 经 fake handle 的 Data() 通道发出后,emitter 收到 `terminal:t1:data`。
- handle 的 Exit() 发出后,emitter 收到 `terminal:t1:exit`,且 `sessions["t1"]` 被清除。
- `Open` 同一 `terminalID` 第二次:evict 旧 handle(旧 handle.Close 被调)。
- `Close(ctx, "t1")` 抢占 in-flight `Open`(用阻塞 backend 验证 cancel 行为,沿用现有抢占用例改 key)。
- `Write/Resize` 在未 open 的 terminalID 上返回 `ErrTerminalClosed`。

**GREEN** — `service.go`:
- `sessions map[int64]pty.Handle` → `map[string]pty.Handle`;`inFlight map[int64]*openAttempt` → `map[string]*openAttempt`。
- `Open(ctx context.Context, terminalID string, deviceID string, cwd string, cols, rows uint16) error`:
  - 删去 `lookup.Lookup(...)` 与 `sess==nil` 分支;
  - `backend, err := s.selector.Pick(deviceID)`;
  - 其余 evict / inFlight 注册 / `backend.Open(pty.Spec{Cwd: cwd, Cols: cols, Rows: rows})` / 抢占判定 / `go s.pump(ctx, terminalID, h)` 逻辑**原样保留**,仅 key 改 string。
- `Write/Resize/Close/lookupHandle/pump/Shutdown` 形参 `sessionID int64` → `terminalID string`。
- **删除** `SessionLookup` 接口与 `ErrSessionNotFound`;保留 `ErrTerminalClosed`/`ErrTerminalNotOpen`。
- `NewService(lookup SessionLookup, sel *BackendSelector, emitter Emitter)` → `NewService(sel *BackendSelector, emitter Emitter)`。

### Task 0.3 — 事件名按 terminalID

**GREEN(随 0.2 一起绿)** — `emitter.go`:
- `DataEventName(sessionID int64)` → `DataEventName(terminalID string) string { return fmt.Sprintf("terminal:%s:data", terminalID) }`;`ExitEventName` 同理 `:exit`。
- 注释里的 "session" 文案改为 "terminal"。

**验证 Phase 0**:`go test -race ./internal/service/terminal_svc/...` 全绿。

---

## Phase 1 — 后端:项目 cwd 解析 + app 绑定改签名

### Task 1.1 — `project_svc.ResolveProjectCwd`

**RED** — 新增 `internal/service/project_svc/cwd_terminal_test.go`(goconvey + mock repo,参考 `cwd_test.go` 的 `setupCwdTest`):
- `deviceID==""` 且项目存在 → 返回 `project.Path`。
- `deviceID==""` 且 `Project().Find` 返回 nil → 返回错误(项目不存在;**无 AgentCwd 兜底**)。
- `deviceID=="42"` 且 `project_location.FindByProjectAndDevice(projectID,"42")` 命中 → 返回该 `loc.Path`。
- `deviceID=="42"` 且未命中(nil,nil) → 返回 `ProjectLocationMissing` 错误。

> 需要确认错误码:复查 `internal/pkg/code` 是否已有 `ProjectLocationMissing`(`chat_svc.resolveSessionCwd` 远端缺失时用过同类错误——见 spec 引用);有则复用,无则在 code 包补一个,并加 i18n 文案(本地化文件位置参照现有 code 用法)。

**GREEN** — 新增 `internal/service/project_svc/cwd.go` 里的方法(或同包新文件):
```go
func (s *projectSvc) ResolveProjectCwd(ctx context.Context, projectID int64, deviceID string) (string, error) {
    if deviceID == "" {
        p, err := project_repo.Project().Find(ctx, projectID)
        if err != nil { return "", err }
        if p == nil || !p.IsActive() { return "", i18n.NewError(ctx, code.ProjectNotFound) }
        return p.Path, nil
    }
    loc, err := project_location_repo.ProjectLocation().FindByProjectAndDevice(ctx, projectID, deviceID)
    if err != nil { return "", err }
    if loc == nil { return "", i18n.NewError(ctx, code.ProjectLocationMissing) }
    return loc.Path, nil
}
```
- 在 `project.go` 的 `ProjectSvc` 接口加 `ResolveProjectCwd(ctx, projectID int64, deviceID string) (string, error)`。
- 新引 `project_location_repo`(确认无 import 环:project_svc → project_location_repo 是 svc→repo,合法)。

### Task 1.2 — app 绑定改签名 + 解析 cwd

**GREEN** — `internal/app/terminal.go`:
- `TerminalOpen(terminalID string, projectID int64, deviceID string, cols, rows uint16) error`:
  ```go
  cwd, err := project_svc.Default().ResolveProjectCwd(a.ctx, projectID, deviceID)
  if err != nil { return err }
  return a.ensureTerminalSvc().Open(a.ctx, terminalID, deviceID, cwd, cols, rows)
  ```
- `TerminalWrite(terminalID, data string)` / `TerminalResize(terminalID string, cols, rows uint16)` / `TerminalClose(terminalID string)` 转发。
- import `agentre/internal/service/project_svc`。

**GREEN** — `internal/app/terminal_wiring.go`:
- **删除** `sessionLookupAdapter` 及其所有 import(`chatrepo`/`agentrepo`/`agentbackendrepo`/`chat_entity`/`agent_backend_entity`/`chat_svc` 中仅为 lookup 用的部分——逐个核对,remoteFactory 仍需 `chat_svc.BorrowDeviceClient`,保留)。
- `newTerminalService`:`terminal_svc.NewService(selector, emitter)`(去掉 lookup 实参)。

### Task 1.3 — 重新生成 wails 绑定

- 运行 `make generate`(或 `wails generate module`)刷新 `frontend/wailsjs/go/app/App.{d.ts,js}`。
- 确认 `TerminalOpen(arg1:string, arg2:number, arg3:string, arg4:number, arg5:number)` 等新签名出现。

> **CHECKPOINT 1** — STOP。向用户汇报并验证:
> - `go build ./...` 通过;
> - `go test -race ./internal/service/terminal_svc/... ./internal/service/project_svc/... ./internal/app/...` 全绿;
> - 展示新 `App.d.ts` 终端签名。
> 等用户确认后再进 Phase 2。

---

## Phase 2 — 前端:终端 tab 模型 + 面板按 terminalID 接线

### Task 2.1 — `chat-tabs-store` 增终端 tab 与 `openTerminal`

**RED** — 新增/扩展 store 测试(参照现有 chat-tabs 相关测试约定;文件 `frontend/src/stores/chat-tabs-store.test.ts`,无则新建):
- `openTerminal(7, "", undefined)` → 新增一个 `meta.kind==="terminal"` 的 tab,带非空 `terminalId`(uuid)、`projectId===7`、`deviceId===""`、`title` 含「终端」;`activeTabId` 指向它;`isPreview===false`。
- `openTerminal(7, "42", "MacMini")` → `deviceId==="42"`,`title` 含设备名(如 `终端 · MacMini`)。
- 连续两次 `openTerminal` → 两个不同 `terminalId` 的独立 tab。
- persist:`partialize` 仍包含终端 tab(rehydrate 后保留)——验证 `partialize(state).tabs` 含终端 tab。
- `closeTab(terminalTabId)` 正常移除并按既有规则切换 active。

**GREEN** — `chat-tabs-store.ts`:
- `TabKind` 加 `| { kind: "terminal"; projectId: number; deviceId: string; terminalId: string }`。
- 加 action `openTerminal(projectId: number, deviceId: string, deviceName?: string)`:`const terminalId = uuid()`;`title = deviceName ? \`终端 · ${deviceName}\` : "终端"`;push 一个新 tab(`isPreview:false, isPinned:false, openedAt: Date.now()`,与现有 `openSessionInNewTab` 同构),设 `activeTabId`。
- partialize **不变**(终端 tab 自然随 `tabs` 持久化)。

### Task 2.2 — `use-terminal` + `terminal-panel` 按 terminalID 接线

**RED** — `frontend/src/components/agentre/terminal/use-terminal.test.ts`(扩展现有或新建):
- 传 `{ terminalID:"t1", projectId:7, deviceId:"" }` → mount 后调用 `App.TerminalOpen("t1", 7, "", cols, rows)`;订阅 `terminal:t1:data` / `terminal:t1:exit`。
- 收到 `terminal:t1:data` → `onData` 被调。
- 收到 `terminal:t1:exit` → `onExit` 被调、`EventsOff` 解绑。
- `write("x")` → `App.TerminalWrite("t1","x")`;`resize` → `App.TerminalResize("t1",…)`;卸载 → `App.TerminalClose("t1")`。

**GREEN**:
- `use-terminal.ts`:入参 `sessionID:number` → `{ terminalID:string; projectId:number; deviceId:string; onData; onExit }`;事件名与 4 个 App 调用全部改用 terminalID/projectId/deviceId。状态机不变。
- `terminal-panel.tsx`:props `{ sessionID:number }` → `{ terminalID:string; projectId:number; deviceId:string }`;传给 `useTerminal`。xterm/主题/resize/断线 banner 不变。

### Task 2.3 — `chat-panel-host` 分支渲染终端面板

**RED** — host 测试(`chat-panel-host.test.tsx` 无则新建):给一个 `kind==="terminal"` 的 tab,断言渲染 `TerminalPanel`(用 testid/role)而非 `ChatPanel`。

**GREEN** — `chat-panel-host.tsx` 的 `HostedPanel`:
```tsx
if (tab.meta.kind === "terminal") {
  return (
    <div data-tab-id={tab.id} data-active={active} style={{ display: active ? "flex" : "none" }} className="...">
      <TerminalPanel terminalID={tab.meta.terminalId} projectId={tab.meta.projectId} deviceId={tab.meta.deviceId} />
    </div>
  );
}
// 否则照旧渲染 ChatPanel
```
保持 inactive tab `display:none` 以维持 PTY/xterm 状态。

### Task 2.4 — `chat-tab-strip` 终端 tab 图标/标题

**RED** — `chat-tab-strip.test.tsx`:给终端 tab 断言渲染 `TerminalSquare` 图标 + `title`。

**GREEN** — `chat-tab-strip.tsx`:`kind==="terminal"` 时图标用 `TerminalSquare`(lucide),标题用 `tab.title`;关闭按钮复用现有逻辑(关 tab → host 卸载 → `TerminalClose`)。

---

## Phase 3 — 前端:项目菜单「新建终端」+ 移除会话内终端

### Task 3.1 — 项目菜单「新建终端」子菜单

**RED** — `project-page.test.tsx`(扩展现有 `frontend/src/components/agentre/__tests__/project-page.test.tsx`):
- 打开项目下拉菜单 → 出现「新建终端」。
- 子菜单含「本地」项;点击 → 调 `openTerminal(project.id, "", undefined)`。
- 给定一个在线且**已配路径**的 device → 列出且可点;点击 → `openTerminal(project.id, String(device.id), device.name)`。
- 给定一个在线但**未配路径**的 device → 渲染为 disabled,且有提示文案(置灰 + tooltip/title「先在项目设置配置远端路径」)。
- 离线 device → disabled。

**GREEN** — `project-page.tsx`:
- 在 `DropdownMenuContent`(项目设置/新建子项目 旁)加一个 `<NewTerminalSubMenu projectId={project.id} />` 子组件,内部用 `DropdownMenuSub/DropdownMenuSubTrigger/DropdownMenuSubContent`(shadcn dropdown-menu,确认这些已导出,没有则从 `@/components/ui/dropdown-menu` 引入)。
- 子组件:
  - `本地` `DropdownMenuItem` → `useChatTabsStore.getState().openTerminal(projectId, "", undefined)`(或经 props 回调,沿用 project-page 既有交互注入方式)。
  - 远端:`const devices = useRemoteDevices()`;子菜单**打开时** lazy 调 `ProjectLocationList(projectId)` 得到已配 deviceId 集合(用 state + 在 SubTrigger `onOpenChange`/挂载时拉)。
  - 每个 device 一项:`disabled = !device.online || !configuredDeviceIds.has(String(device.id))`;disabled 时加 `title`/tooltip 提示;可点时 `onSelect={() => openTerminal(projectId, String(device.id), device.name)}`。
- 图标用 `TerminalSquare`。

> 注:project-page 是项目树,会渲染多个卡片。**locations 必须 lazy 加载**(子菜单打开才拉),避免每卡片一次请求。

### Task 3.2 — 移除会话内终端

**RED** — `chat-panel` 测试:断言终端 toggle 按钮(原 `title="终端 (⌘`)"` / `TerminalSquare`)**不存在**;⌘\` 不再触发任何终端行为。

**GREEN** — `chat-panel.tsx`:
- 删除:终端 toggle 按钮 JSX、⌘\` 键盘快捷键分支、`terminalOn ? <TerminalPanel/> : <ChatContent/>` 改为直接渲染 `<ChatContent/>`(或等价的 transcript+composer 主体)、`useChatTerminalStore` 与 `TerminalPanel` 的 import 及派生状态(`isTerminalOpen/isTerminalTransitioning/toggleTerminal/terminalOn/terminalTransitioning`)。
- 注意:`TerminalPanel` 仍被 `chat-panel-host` 使用,**只删 chat-panel 内的 import/用法**,不要删组件文件。

### Task 3.3 — 删除 `chat-terminal-store` 并清理引用

**GREEN**:
- `grep` 全前端 `useChatTerminalStore` / `chat-terminal-store` 的引用点(预期:chat-panel.tsx 已在 3.2 清除;可能还有 app 关停时调 `closeAll` 的地方)。
- 移除所有引用:终端关闭现由「关 tab → host 卸载 → `TerminalClose`」与后端 `Shutdown()` 兜底,无需前端 `closeAll`。
- 删除 `frontend/src/stores/chat-terminal-store.ts` 及其测试(若有)。

> **CHECKPOINT 2** — STOP。验证:
> - `cd frontend && pnpm test` 全绿;
> - `make dev` 手动冒烟:项目菜单→新建终端(本地)→新 tab 出现并可交互;开两个终端互不串;关 tab 进程结束;(如有在线远端 device + 已配路径)远端终端可开;未配路径 device 置灰。
> 等用户确认后进 Phase 4。

---

## Phase 4 — 全量验证 + 收尾

- `make generate`(确保绑定最新)→ `make check`(lint + test,含 `-race` 与 Vitest)。
- 复查 diff 仅限 scope 内文件(无顺手 refactor/格式化漂移)。
- 自检:重启 app(`make dev` 重起)后终端 tab 仍在、PTY 重新 spawn(空白新终端,符合预期)。
- 按仓库 commit 风格分阶段提交(后端解耦 / app 绑定 / 前端 tab 模型 / 项目菜单 / 移除会话内终端)。

---

## 测试策略汇总

- **Go(`-race`)**:`terminal_svc`(terminalID 重写 + Pick + 事件名)、`project_svc.ResolveProjectCwd`(本地/远端/缺失三态)。
- **Vitest**:`chat-tabs-store.openTerminal`+persist、`use-terminal`(terminalID 接线)、`chat-panel-host` 分支、`chat-tab-strip` 图标、`project-page` 子菜单(置灰逻辑)、`chat-panel` 移除回归。

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| 删 `SessionLookup`/`ErrSessionNotFound` 时漏改引用导致编译断 | 改前 `grep` 全仓引用;CHECKPOINT 1 用 `go build ./...` 兜底。 |
| `terminal_wiring.go` import 清理误删 remoteFactory 仍需的 `chat_svc.BorrowDeviceClient` | 逐 import 核对;保留 remoteFactory 路径,只删 lookup 专用 import。 |
| `ProjectLocationMissing` 错误码/i18n 不存在 | Task 1.1 先确认 `internal/pkg/code`,缺则补码 + 文案(在 scope 内)。 |
| project-page 多卡片导致 locations 过度请求 | 子菜单 lazy 加载(打开才拉),不在卡片渲染时拉。 |
| 终端 tab persist 后 rehydrate 复用旧 terminalId,但后端无对应 handle | 设计如此:`Open` 对新 id 直接创建新 PTY;历史丢失符合决策 3。 |
| wails 绑定未重生成致前端 TS 类型不符 | Task 1.3 显式 `make generate`;CHECKPOINT 1 检查 `App.d.ts`。 |
| 误删 `TerminalPanel` 组件(它仍被 host 用) | 3.2 只删 chat-panel 内 import/用法,不删组件文件。 |

## 不在范围

- 不绑定项目的「自由终端」。
- 远端路径配置 UI(复用现有「项目设置→远端路径」)。
- 终端 scrollback 跨重启持久化。
