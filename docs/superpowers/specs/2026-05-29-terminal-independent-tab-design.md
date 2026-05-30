# 终端独立成 Tab + 项目菜单「新建终端」设计

**日期**: 2026-05-29
**状态**: 设计完成,待 review
**作者**: Claude (结对 with 王一之)

## 背景

当前终端功能**寄生在会话(session)tab 里**:

- 后端 `terminal_svc.Service` 以 `sessionID int64` 为 key 索引 PTY,`Open` 时通过 `SessionLookup.Lookup(sessionID)` 沿 `session → agent → agent_backend → cwd` 链解析工作目录与 backend。
- 前端终端不是独立 tab,而是 `ChatPanel` 内的一个开关:`terminalOn ? <TerminalPanel/> : <ChatContent/>`,由 `chat-terminal-store`(`openSessionIDs` set)按 sessionID 驱动,工具栏按钮 / ⌘\` 切换。

诉求:**把终端从会话 tab 拆出来,让它有独立的 tab 和生命周期**,并在项目页的项目菜单里加「新建终端」入口,点击新开一个终端 tab。

### 关键可行性结论(已核实)

- **远端 PTY 协议本身已与 sessionID 解耦**:`pkg/agentred/protocol.TerminalOpenParams/Result` 用不透明的 `TerminalID string`,`pty/remote/client_adapter.go` 按 terminalID demux。只有桌面侧 `terminal_svc.Service` 用 sessionID。**因此解耦是纯桌面侧改动,daemon 协议不动。**
- 设备列表:`App.RemoteDeviceList() → []*remote_device_svc.DeviceView{ID, Name, Online, …}`,前端已有 `useRemoteDevices()` hook。
- 远端 cwd:`App.ProjectLocationList(projectID) → [{DeviceID, Path, DeviceName, Online}]`、`project_location_repo.FindByProjectAndDevice`;本地 cwd = `project.Path`。

## 已确认的设计决策

1. **完全移除会话内终端** —— 删掉 `ChatPanel` 的终端 toggle 与 swap,所有终端一律走独立 tab,后端 key 从 sessionID 改为独立的 terminal id。
2. **本地 + 选 device** —— 新建终端时让用户选 backend(本地 / 某个在线远端 device);远端用 `project_location` 解析该 device 下的项目路径。
3. **tab 持久化,PTY 重开** —— 终端 tab 跟会话 tab 一样进 zustand persist;app 重启后 tab 还在,但 PTY 重新 spawn(历史输出丢失,符合预期)。
4. **未配路径的远端 device 置灰 + 提示** —— 子菜单打开时 lazy 加载 `ProjectLocationList`,未配置远端路径的在线 device 置灰不可选,hover 提示「先在项目设置配置远端路径」。

## 架构选择:后端按什么 key 索引

| 方案 | 做法 | 取舍 |
|---|---|---|
| **A(采纳)纯 PTY 多路复用器** | `terminal_svc.Service` 丢掉 `SessionLookup`,以 `terminalID string` 为 key;`Open(ctx, terminalID, deviceID, cwd, cols, rows)` 直接收已解析好的 deviceID/cwd。cwd/device 解析上移到 app 层。 | 彻底解耦,terminal_svc 不再认识 session/project,单测最简单。 |
| B 注入 resolver | terminal_svc 保留注入的 `Resolver`,source 描述符 → (deviceID, cwd)。 | 沿用 SessionLookup 模式,但仍耦合「source」概念。 |
| C 复用 sessionID(合成负 ID) | 保留 int64 key,项目终端用合成 ID。 | hacky,正是要去掉的耦合。否决。 |

**采纳 A**,与「完全移除会话内终端 / 独立生命周期」最契合。

## 后端设计

### 1. `internal/service/terminal_svc/service.go`

- `sessions map[int64]pty.Handle` → `map[string]pty.Handle`;`inFlight map[int64]*openAttempt` → `map[string]*openAttempt`。
- `Open(ctx, terminalID string, deviceID string, cwd string, cols, rows uint16) error`:删去 `lookup.Lookup`,直接 `selector.Pick(deviceID)` + `backend.Open(pty.Spec{Cwd: cwd, Cols, Rows})`。evict / inFlight-preemption / pump 逻辑保持不变,只是 key 改成 string。
- `Write/Resize/Close/lookupHandle/pump` 形参 `sessionID int64` → `terminalID string`。
- **删除 `SessionLookup` 接口**及 `ErrSessionNotFound`(改由 app 层 `ResolveProjectCwd` 返回项目相关错误)。
- 事件名 `emitter.go`:`DataEventName/ExitEventName(sessionID int64)` → `(terminalID string)`,格式 `terminal:{terminalID}:data` / `:exit`。

### 2. `internal/service/terminal_svc/backend.go`

- `BackendSelector.Pick(be *agent_backend_entity.AgentBackend)` → `Pick(deviceID string)`:`deviceID == ""` → local backend;非空 → `remoteFactory(deviceID)`。删除对 `agent_backend_entity` 的依赖。

### 3. `internal/service/project_svc` 新增 `ResolveProjectCwd`

```go
// ResolveProjectCwd 解析「某 device 下某项目」的工作目录。
//   deviceID == ""  → project.Path(本地);项目不存在/软删 → 报错
//   deviceID != ""  → project_location.FindByProjectAndDevice;缺失 → ProjectLocationMissing
func (s *projectSvc) ResolveProjectCwd(ctx context.Context, projectID int64, deviceID string) (string, error)
```

加入 `project_svc.ProjectSvc` 接口。需要新引 `project_location_repo`。

> 注:与会话用的 `ResolveSessionCwd` 区别——终端没有 agent,因此本地项目不存在时**直接报错**(无 AgentCwd 兜底)。

### 4. `internal/app/terminal.go` + `terminal_wiring.go`

- `TerminalOpen(terminalID string, projectID int64, deviceID string, cols, rows uint16) error`:先 `project_svc.Default().ResolveProjectCwd(ctx, projectID, deviceID)` 解析 cwd,再 `terminalSvc.Open(ctx, terminalID, deviceID, cwd, cols, rows)`。
- `TerminalWrite(terminalID, data string)` / `TerminalResize(terminalID string, cols, rows)` / `TerminalClose(terminalID string)`。
- 删除 `terminal_wiring.go` 的 `sessionLookupAdapter`;`newTerminalService` 不再注入 lookup,selector 的 remoteFactory 已是 `func(deviceIDStr string)`,基本不变。
- `make generate` 刷新 `frontend/wailsjs` 绑定。

## 前端设计

### 1. `frontend/src/stores/chat-tabs-store.ts`

- `TabKind` 增加变体:`{ kind: "terminal"; projectId: number; deviceId: string; terminalId: string; title: string }`。
- 新增 action `openTerminal(projectId: number, deviceId: string, deviceName?: string)`:`terminalId = crypto.randomUUID()`,标题如 `终端`(本地)/ `终端 · {deviceName}`(远端);总是新建 tab(非 preview)并激活。
- **持久化**:终端 tab 跟会话 tab 一起进 persist(无需 partialize 排除)。rehydrate 后 `TerminalPanel` 挂载即重新 `TerminalOpen`,自然得到新 PTY。

### 2. `terminal-panel.tsx` / `use-terminal.ts`

- props `sessionID: number` → `terminalID: string`,并新增 `projectId: number`、`deviceId: string`。
- 事件订阅 `terminal:${terminalID}:data` / `:exit`;调用 `App.TerminalOpen(terminalID, projectId, deviceId, cols, rows)`、`TerminalWrite(terminalID, data)`、`TerminalResize(terminalID, …)`、`TerminalClose(terminalID)`。
- 其余(xterm、主题、resize observer、断线 banner)不变。

### 3. `chat-panel-host.tsx`

- `HostedPanel` 按 `tab.meta.kind` 分支:`"terminal"` 渲染全高终端面板组件;否则照旧渲染 `ChatPanel`。

### 4. `chat-panel.tsx`(移除会话内终端)

- 删除:终端 toggle 按钮、⌘\` 快捷键、`terminalOn` 条件 swap、`useChatTerminalStore` 与 `TerminalPanel` 的 import 及派生状态。

### 5. 删除 `chat-terminal-store.ts`

- toggle 概念消失。审查并处理 `closeAll`(app 关停清理)的调用点——改为不需要(后端 `Shutdown()` 已统一关 PTY;前端终端 tab 关闭即 `TerminalClose`)。

### 6. `project-page.tsx`(项目菜单新增「新建终端」)

- 项目下拉菜单加 `DropdownMenuSub`「新建终端 ▶」:
  - `本地` 常驻可选。
  - 分隔线后列出在线远端 device(`useRemoteDevices()`)。
  - 子菜单打开时 lazy 加载 `ProjectLocationList(project.id)`;**未配置远端路径的在线 device 置灰不可选**,hover 提示「先在项目设置配置远端路径」。离线 device 置灰。
  - 选中 → `openTerminal(project.id, deviceId, deviceName)`(本地传 `deviceId=""`)。

### 7. tab 条渲染

- 终端 tab 显示 `TerminalSquare` 图标 + 标题;关闭按钮复用(关 tab → 卸载面板 → `TerminalClose`)。具体 tab 条组件在 plan 阶段定位。

## 生命周期 / 边界

- 关 tab = 杀 PTY(面板卸载时 `TerminalClose`)。
- app 关停:`terminal_svc.Shutdown()` 关全部 PTY(已有)。
- 远端断线:exit 事件 `reason: connection_lost` → 面板 banner(已有)。
- 重启:tab 持久化保留,PTY 重新 spawn;若远端此时离线则 `Open` 失败,面板内报错。
- 同一项目可开多个终端 tab(各自独立 terminalId)。
- 项目被删后再开旧终端 tab:`ResolveProjectCwd` 报错,面板内提示。

## 测试(TDD,Red → Green → Refactor)

### Go(`-race`)

- `terminal_svc`:按 `terminalID string` 重写既有用例,覆盖 Open/Write/Resize/Close、并发 Open evict、Close 抢占 in-flight Open;Open 直接喂 `(deviceID, cwd)`,不再 mock SessionLookup。
- `BackendSelector.Pick(deviceID)`:`""` → local,非空 → remoteFactory。
- `project_svc.ResolveProjectCwd`:本地命中 `project.Path`、远端命中 `project_location.Path`、项目缺失报错、远端路径缺失 `ProjectLocationMissing`(goconvey + mock repo)。

### Vitest

- `chat-tabs-store`:`openTerminal` 生成带 uuid 的终端 tab 并激活;persist/rehydrate 保留终端 tab;`closeTab` 正常关闭。
- `use-terminal`:按 terminalID 订阅 data/exit 并 Open/Write/Resize/Close。
- `chat-panel-host`:`kind==="terminal"` 渲染终端面板。
- `project-page`:「新建终端」子菜单列出 本地 + 在线 device;未配路径 device 置灰;选中调用 `openTerminal` 且参数正确。
- `chat-panel`:断言终端 toggle 按钮已移除(移除回归)。

## 不在范围

- 不做「不绑定项目的自由终端」(仅按需求加项目菜单入口)。
- 远端路径配置复用现有「项目设置 → 远端路径」。
- 终端 scrollback 跨重启持久化(PTY 重 spawn 即丢历史,设计如此)。

## 影响面提示

本次 diff 横跨后端 `terminal_svc`/`project_svc`/`app` 与前端 `chat-tabs`/`chat-panel`/`project-page`,改动面较大但均在「终端解耦」范围内;移除会话内终端(toggle / ⌘\` / `chat-terminal-store`)是用户明确要求的行为删除。实现时严格遵守仓库 TDD 与「只动 scope 内文件」约束。
