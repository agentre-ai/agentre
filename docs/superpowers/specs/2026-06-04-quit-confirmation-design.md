# 退出 App 二次确认(活跃会话保护)设计

**日期**: 2026-06-04
**状态**: 设计完成,待 review
**作者**: Claude (结对 with 王一之)

## 背景

用户按 cmd+Q(macOS)/ 点关闭按钮 / Alt+F4(Windows)退出 App 时,如果当前**有正在进行的会话**,会直接杀掉常驻 CLI 子进程、中断当前回合,未完成的回合丢失且无任何提示。诉求:**退出前若存在活跃会话,弹一个二次确认框**,跨平台(macOS / Windows / Linux)统一处理。

### 关键可行性结论(已核实)

- **`OnBeforeClose` 是 Wails v2.12.0 所有平台唯一的退出拦截点**。已读源码确认三条退出路径全部汇入 `Frontend.Quit()` → `OnBeforeClose`:
  - macOS:cmd+Q / 菜单 Quit / Dock Quit → `AppDelegate.applicationShouldTerminate` 返回 `NSTerminateCancel` 并 `processMessage("Q")` → dispatcher `case 'Q': sender.Quit()`(`darwin/AppDelegate.m`、`darwin/frontend.go:364`、`dispatcher.go:69`)。
  - Windows:关闭按钮 / Alt+F4 / `wailsruntime.Quit` → `windows/frontend.go:453 Quit()`。
  - 返回 `true` 即阻止退出。
  - 平台差异:macOS 在 goroutine 里跑 `OnBeforeClose`,Windows 在主线程同步跑。**因此 `OnBeforeClose` 内必须只做一次快速 COUNT 查询、不可阻塞**(不能在里面弹原生对话框等待)。
- 当前 `main.go` 只接了 `OnStartup` / `OnShutdown`,**没接 `OnBeforeClose`**。
- 「活跃」判定沿用启动时 `ResetStaleActiveSessions` 的口径:`agent_status IN ('running','waiting') AND status = consts.ACTIVE`(`chat_repo/session.go:308`)。
- 自动更新「重启」(`internal/app/update.go:128`)也调 `wailsruntime.Quit`——它在拉起新进程后退出旧进程,**必须跳过确认**,否则旧进程被拦、新进程撞单实例锁(`SingleInstanceLock`)→ 更新静默失败。
- 前端常驻挂载点:`App.tsx` 的 `AppLayout` 里 `<Toaster/>` 旁(始终挂载,跨路由存活);事件订阅沿用 `EventsOn`(`wailsjs/runtime/runtime`,参考 `hooks/use-chat-stream.ts:156`)。
- 代码库**没有 shadcn `alert-dialog`**;复用现成的 `components/agentre/app-dialog.tsx`(`AgentreDialog`,基于 `components/ui/dialog.tsx`)。
- 会话展示数据分两处:运行态 `stores/session-status-store.ts`(只有 `agentStatus`/`needsAttention`,**无名称**);展示名在 `stores/session-meta-store.ts`(`SessionMeta`:`agentName`/`agentColor`/`projectId`/`title`)。

## 已确认的设计决策

1. **确认框形式 = 应用内 shadcn 弹窗**(复用 `AgentreDialog`),走 react-i18next,和无边框自定义窗口风格一致。不用系统原生 MessageDialog。
2. **活跃口径 = `running` + `waiting`**(任何进行中的回合;与 `ResetStaleActiveSessions` 一致)。
3. **确认框带「进行中会话列表」**:每行 agent 头像 + 名称 + 项目/分支 + 状态胶囊(绿 `运行中` / 琥珀 `等待确认`)。
4. **列表溢出**:可见行**上限 3 行**,其余折叠为一行 `还有 N 个会话`(放在可滚动区域内);**header 计数永远显示后端真实总数**。
5. **计数以后端为准**:确认框 header 的数字来自后端 `OnBeforeClose` 的 COUNT;列表行从前端 store 派生(best-effort,store 不全时只少列几行,不影响 header 数字)。
6. 设计稿:`~/Desktop/agentry.pen` 的 `Quit Confirm — Light`(常规)与 `Quit Confirm — Many`(溢出)两帧,复用既有 Delete Dialog 的 token。

## 后端设计

### 1. `internal/repository/chat_repo/session.go` — 新增 `CountActive`

仿照已有 `CountActiveByProject`,去掉 project 过滤,做全局计数:

```go
// CountActive 统计 status=ACTIVE 且 agent_status 在指定集合内的会话总数(跨所有 agent/项目)。
// 用于退出二次确认:agentStatuses 传 {"running","waiting"}。
func (r *sessionRepo) CountActive(ctx context.Context, agentStatuses []string) (int64, error)
```

加入 `Session` 接口(`session.go:28` 附近)。实现:`db.Ctx(ctx).Model(&chat_entity.Session{}).Where("agent_status IN ? AND status = ?", agentStatuses, consts.ACTIVE).Count(&n)`。需 `make mock` 重生 `mock_chat_repo`。

### 2. `internal/service/chat_svc/chat.go` — 新增 `CountActiveSessions`

```go
// CountActiveSessions 返回正在进行(running|waiting)的会话数,供退出二次确认判断。
func (s *chatSvc) CountActiveSessions(ctx context.Context) (int, error)
```

加入 `ChatSvc` 接口(`chat.go:73`)。实现:`n, err := chat_repo.Session().CountActive(ctx, []string{"running", "waiting"})`,返回 `int(n)`。

### 3. `internal/app/app.go` — `OnBeforeClose` + `ConfirmQuit`

App 新增字段 `quitConfirmed atomic.Bool`。决策抽成可测纯函数,真实依赖在 `OnBeforeClose` 注入:

```go
// shouldPreventQuit 决定是否拦截退出并通知前端弹确认框。
//   confirmed=true(已确认/自动更新重启) → 放行
//   count 出错或为 0 → 放行(fail-open:计数失败不困住用户)
//   count>0 → emit "app:quit-blocked"{count} 并拦截
func shouldPreventQuit(ctx context.Context, confirmed bool,
    count func(context.Context) (int, error), emit func(n int)) bool {
    if confirmed {
        return false
    }
    n, err := count(ctx)
    if err != nil || n == 0 {
        return false
    }
    emit(n)
    return true
}

func (a *App) OnBeforeClose(ctx context.Context) (prevent bool) {
    return shouldPreventQuit(ctx, a.quitConfirmed.Load(),
        func(c context.Context) (int, error) { return chat_svc.Chat().CountActiveSessions(c) },
        func(n int) {
            logger.Ctx(ctx).Info("app.OnBeforeClose: quit blocked by active sessions", zap.Int("count", n))
            wailsruntime.EventsEmit(a.ctx, "app:quit-blocked", map[string]any{"count": n})
        })
}

// ConfirmQuit 前端点「仍然退出」后调用:标记已确认并真正退出。
func (a *App) ConfirmQuit() {
    a.quitConfirmed.Store(true)
    wailsruntime.Quit(a.ctx)
}
```

- `main.go` 的 wails options 加 `OnBeforeClose: appInst.OnBeforeClose`。
- **fail-open 取舍**:COUNT 出错时放行而非拦截——拦截会因瞬时 DB 错误把用户困在「退不出去」,代价仅是极罕见错误下漏一次确认;已 `logger.Warn`。

### 4. `internal/app/update.go` — 重启跳过确认

`restartApp`(`update.go:128`)在 `wailsruntime.Quit(a.ctx)` 前加 `a.quitConfirmed.Store(true)`,让自动更新重启绕过确认。

## 前端设计

### 1. `App.ConfirmQuit` 绑定

`make generate` 后 `wailsjs/go/app/App.d.ts` 生成 `ConfirmQuit():Promise<void>`(无参)。

### 2. 新组件 `frontend/src/components/agentre/quit-confirm-dialog.tsx`

始终挂载在 `App.tsx` 的 `AppLayout` 内 `<Toaster/>` 旁:

- 内部 `useState` 持有 `{ open, count }`;`useEffect` 里 `EventsOn("app:quit-blocked", p => setState({open:true, count:p.count}))`,返回 `off` 清理(镜像 `use-chat-stream.ts` 的 ref 模式)。
- 渲染用 `AgentreDialog`:标题「退出 Agentre？」+ 琥珀 `triangle-alert`,描述含 `count`,footer `取消`(关闭)+ `仍然退出`(destructive,调 `ConfirmQuit()`)。
- **会话列表**:`useSessionStatusStore` 取所有 `agentStatus ∈ {running,waiting}` 的 sessionId,`useSessionMetaStore` 取每个的 `agentName/agentColor/projectId/title` 渲染行;可见上限 3,其余折叠为 `还有 N 个会话` 行,列表区 `max-h` 可滚。header 数字用后端 `count`。store 无 meta 的行用兜底文案。
- `取消` / Esc / 点遮罩 = 关闭(安全默认),**不**调 `ConfirmQuit`。

### 3. i18n

`frontend/src/i18n/locales/{zh-CN,en}/common.json` 新增 `quitConfirm` 段:`title` / `description`(带 `{{count}}`)/ `runningSessions`(带 `{{count}}`)/ `more`(带 `{{count}}`)/ `statusRunning` / `statusWaiting` / `cancel` / `quitAnyway` / 兜底会话名。两份 locale 同步,过 `i18n.test.ts`。

## 边界与交互

- **取消后再次退出**:`quitConfirmed` 仍为 false,重新 COUNT、重新弹框(符合预期)。
- **确认后**:`quitConfirmed=true` → `Quit` 重入 `OnBeforeClose` → 放行退出。
- **无活跃会话**:`OnBeforeClose` 返回 false,正常秒退,无框。
- **自动更新重启**:`update.go` 预置 `quitConfirmed=true`,不弹框,正常重启。
- **重复触发**(连按 cmd+Q):每次 emit 一次事件,前端 `open` 幂等(已开着就保持开)。

## 测试计划(TDD:Red → Green)

| 层 | 文件 | 用例 |
|---|---|---|
| repo (sqlmock) | `chat_repo/session_test.go` | `TestSessionRepo_CountActive`:断言 SQL `agent_status IN ('running','waiting') AND status=ACTIVE` 的 COUNT,返回 N(仿 `TestSessionRepo_CountActiveByProject`) |
| svc (repo mock) | `chat_svc/..._test.go` | `CountActiveSessions` 透传 mock 计数;断言传入 statuses 为 `{running,waiting}` |
| app (纯函数+fake) | `internal/app/app_quit_test.go` | `shouldPreventQuit`:confirmed→false 不 emit;count=0→false 不 emit;count>0→true 且 emit(n);count err→false 不 emit |
| 前端 (vitest) | `components/agentre/__tests__/quit-confirm-dialog.test.tsx` | 触发 `app:quit-blocked` 事件→框打开且显示 count;点「仍然退出」→调 `ConfirmQuit`;点「取消」→关闭且不调 `ConfirmQuit`;>3 会话→显示「还有 N 个会话」 |

## 不做的事(YAGNI)

- 不做「下次不再提醒」偏好。
- 不把活跃判定扩展到终端进程/未保存草稿(只看 chat 会话 running/waiting)。
- 不在事件 payload 里下发会话列表(列表由前端 store 派生);后端只给权威 count。
- 不新增 shadcn `alert-dialog` 组件(复用 `AgentreDialog`)。
