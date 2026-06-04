# 原生系统通知（图标 + 点击跳转会话，复用 Wails 内置通知）设计

- 日期：2026-06-04
- 状态：已实现并合入分支 `feat/completion-notification`（实现计划见 [2026-06-04-native-system-notification.md](../plans/2026-06-04-native-system-notification.md)）
- 前置背景：完成通知 v1（分类/门槛 + bespoke 应用内 toast + beeep 系统通知 + 前端声音子系统）。本设计**取代**其 beeep 系统通知、并**删除**前端声音子系统；v1 的 spec/plan 已随本次清理移除（分类/门槛/toast 设计仍在代码中沿用）。

## 背景

完成通知 v1 用 `internal/pkg/sysnotify` → `beeep.Notify(title, body, "")`。macOS 上两个用户可见缺陷：

1. **不显示 app 图标**：beeep 的 darwin 路径没有「进程内」实现，先试外部 `terminal-notifier`、失败回退外部 `osascript`；因为传空图标 `""`，必然回退到 `osascript display notification`，以「脚本编辑器」身份投递，横幅显示的是 Script Editor 图标。
2. **点击不激活 / 不跳会话**：`osascript display notification` 不注册点击动作，beeep 的 API 也没有点击回调能力。

### 重大修订：不自己写 CGO，复用 Wails 内置原生通知

评审最初决定「自己写 `_darwin.go` + `.m`/`.h` 调 `UNUserNotificationCenter`」。实现前核对 Wails 源码时发现：**Wails v2.12.0 已经把整套原生 `UNUserNotificationCenter` 写好，并通过公开包 `github.com/wailsapp/wails/v2/pkg/runtime` 暴露**（darwin/linux/windows 三端均有实现），delegate、bundle 校验、非 bundle 防崩都已内置。

因此**本设计不写任何 CGO/ObjC**，直接调用 Wails 公开运行时 API。这比自写 CGO 严格更优：零 CGO、代码量小、桥由上游维护、天然跨平台。

公开 API（均吃 `ctx`）：

| 函数 | 用途 |
| --- | --- |
| `runtime.InitializeNotifications(ctx) error` | 初始化（macOS 装 delegate）；非 bundle 返回错误 |
| `runtime.RequestNotificationAuthorization(ctx) (bool, error)` | 请求授权（macOS 弹窗；阻塞至多 180s） |
| `runtime.CheckNotificationAuthorization(ctx) (bool, error)` | 查授权状态（阻塞至多 15s） |
| `runtime.SendNotification(ctx, NotificationOptions) error` | 以 app 自身身份投递 ⇒ 自动用 app 图标 |
| `runtime.OnNotificationResponse(ctx, func(NotificationResult))` | 点击回调，`Response.UserInfo` 带回 `Data` |

`NotificationOptions{ID, Title, Subtitle, Body, CategoryID, Data map[string]interface{}}`；`NotificationResponse{ActionIdentifier, UserInfo map[string]interface{}, ...}`。

## 目标

- 三端统一用 Wails 运行时通知发系统通知，**自动显示 Agentre 图标**（app 以自身 bundle id `com.wails.Agentre` 投递）。
- 点击通知：**聚焦 / 抬升 Agentre 窗口 + 跳到对应会话**（tab 已开切过去，未开新开 tab）。
- 原生不可用（非 bundle / `wails dev` / `go test` / 拒绝授权 / macOS < 10.14）：**静默 no-op**，不崩。
- **移除 beeep 依赖**（三端都走 Wails）。
- **移除前端「声音」子系统**，统一用系统通知自带声音（见下「声音」节）。

## 非目标

- 不写 CGO/ObjC（复用 Wails 内置）。
- 不改 bundle id：保持 `com.wails.Agentre`（默认占位 id，通知功能用它即可正常工作）。
- 不在设置面板新增「通知权限被拒」提示 UI（YAGNI）。
- 不做跨会话节流（仅用 `ID="session-<id>"` 同会话去重）。`error`/`waiting` 文案分类、`onlyWhenUnfocused` 门槛、`toast` 渠道沿用 v1。

## 决策摘要（评审已锁定）

| 决策点 | 结论 |
| --- | --- |
| 实现路径 | **复用 Wails `pkg/runtime` 内置原生通知，零 CGO** |
| 点击行为 | 聚焦窗口 + 跳到该会话（tab 没开就新开） |
| 授权时机 | **懒触发**：首次发通知时请求；授权成功后再投递 |
| 授权选项 | 由 Wails 决定（`Alert|Sound|Badge`，我们无法改） |
| 降级 | 原生不可用 → 静默 no-op |
| beeep | **整个删除**，三端统一走 Wails |
| 声音 | **删除前端声音子系统**，用系统通知自带声音 |
| bundle id | 保持 `com.wails.Agentre` |

## 声音

Wails 的 `SendNotification` 在 ObjC 内**硬编码** `content.sound = defaultSound`，公开 `NotificationOptions` 无关闭选项 —— 系统通知**必然响**。原 v1「系统通知静音、声音交前端」的前提不再成立，否则前端声音 + 系统声音会**双响**。

评审决策：**删除前端声音子系统**，统一由系统通知发声。移除范围见「涉及文件清单 · 声音移除」。

## 架构与分层

严守 `internal/app → service → internal/pkg`，`internal/pkg` 叶子层不反向依赖 service。

### 通知适配器（`internal/pkg/sysnotify`）

重写为**薄薄的 Wails 运行时适配器**（跨平台、无 build tag、无 CGO、无 beeep）：

```go
// Notifier 用 Wails 内置原生通知投递；结构化满足 notification_svc.Notifier（不 import service）。
type Notifier struct {
    mu         sync.Mutex
    authorized bool // 懒触发：一旦授权成功即缓存，跳过后续 check
}

func New() *Notifier { return &Notifier{} }

// Notify fire-and-forget：丢给 goroutine 做懒授权 + 投递，立即返回 nil。
func (n *Notifier) Notify(ctx context.Context, title, body string, sessionID int64) error {
    go n.deliver(ctx, title, body, sessionID)
    return nil
}

func (n *Notifier) deliver(ctx context.Context, title, body string, sessionID int64) {
    if !n.ensureAuthorized(ctx) {
        return // 非 bundle / 拒绝 / 不可用 → 静默 no-op
    }
    _ = wailsruntime.SendNotification(ctx, wailsruntime.NotificationOptions{
        ID:    fmt.Sprintf("session-%d", sessionID), // 同会话去重
        Title: title,
        Body:  body,
        Data:  map[string]interface{}{"sessionID": sessionID},
    })
}

func (n *Notifier) ensureAuthorized(ctx context.Context) bool {
    n.mu.Lock()
    defer n.mu.Unlock()
    if n.authorized {
        return true
    }
    ok, _ := wailsruntime.CheckNotificationAuthorization(ctx)
    if !ok {
        ok, _ = wailsruntime.RequestNotificationAuthorization(ctx) // NotDetermined 弹窗；Denied 立即 false
    }
    n.authorized = ok // 只缓存正向；未授权时下次仍会重查（用户事后在系统设置打开可恢复）
    return ok
}
```

- 导入 `wailsruntime`（外部依赖，pkg 可引）；**不 import `notification_svc`**（结构化满足接口）。
- `bootstrap/cago.go` 的 `RegisterNotifier(sysnotify.New())` 不变。

### 接口变更（`internal/service/notification_svc`）

```go
// 投递能力（消费端定义，sysnotify 结构化实现，不 import 本包）。
type Notifier interface {
    Notify(ctx context.Context, title, body string, sessionID int64) error
}
```

- `types.go`：`ShowRequest` 增加 `SessionID int64`（`json:"sessionId"`）。
- `Show(ctx, req)`：`s.notifier.Notify(ctx, title, req.Body, req.SessionID)`；空 title 兜底 `"Agentre"`、nil notifier/req 安全 no-op 沿用。
- **删除** 原修订草案里的 `ActivatableNotifier` / `OnActivate`：点击回调改由 app 层直接经 `wailsruntime.OnNotificationResponse` 注册，无需这层间接。
- `mock_notification_svc/mock_notification.go`：`make mock` 重新生成（`Notify` 签名加 `ctx` 与 `sessionID`）。

### 应用层（`internal/app`）

- `notification.go`：`Show(a.ctx, req)` 已透传 `req`，`ShowRequest` 带 `SessionID` 后**无需改**。
- `app.go` 的 `Startup`（持有 `a.ctx`）新增：

```go
if err := wailsruntime.InitializeNotifications(a.ctx); err != nil {
    logger.Ctx(a.ctx).Warn("app.Startup: init notifications", zap.Error(err)) // 非 bundle/旧系统 → 降级
}
wailsruntime.OnNotificationResponse(a.ctx, func(res wailsruntime.NotificationResult) {
    if res.Error != nil {
        return
    }
    sid := sessionIDFromUserInfo(res.Response.UserInfo)
    if sid <= 0 {
        return
    }
    wailsruntime.WindowUnminimise(a.ctx)
    wailsruntime.WindowShow(a.ctx)
    wailsruntime.EventsEmit(a.ctx, "notification:click", sid)
})
```

- `sessionIDFromUserInfo(map[string]interface{}) int64`：从 userInfo 取 `sessionID`，兼容 JSON 往返后的 `float64`（也容错 `int64`/`string`），缺失/非法返回 0。**纯函数，可单测**。

## 数据流

### 下行：发通知

```
前端 turn-complete-notifier: maybeNotify(sessionId, kind, deps)
  → deps.showSystemNotification(sessionId, title, body)
  → ShowNotification({title, body, sessionId})           // Wails binding
  → notification_svc.Show(ctx, req)                        // 透传
  → Notifier.Notify(ctx, title, body, sessionID)           // sysnotify 适配器
  → go deliver: ensureAuthorized → wailsruntime.SendNotification(ctx, {ID, Title, Body, Data{sessionID}})
  → Wails 以 app 自身身份投递 ⇒ 自动用 app 图标
```

### 上行：点击回调

```
用户点通知 → Wails 内置 delegate → 公开回调
  → app 注册的 OnNotificationResponse(res)
  → sid := sessionIDFromUserInfo(res.Response.UserInfo)
  → WindowUnminimise + WindowShow + EventsEmit("notification:click", sid)
  → 前端订阅 "notification:click" → useChatTabsStore.getState().openSession(sid)
       （tab 已开则激活，未开则新开 preview tab 并激活）
```

## 授权流程

- **懒触发（决策 A）**：首次投递时 `ensureAuthorized` 走 `Check → 必要时 Request`。`RequestNotificationAuthorization` 在 `NotDetermined` 时弹系统授权窗，授权成功后该次 `deliver` 继续投递。整个 `deliver` 在 goroutine 内，不阻塞前端 RPC。
- **授权选项**由 Wails 决定（`Alert|Sound|Badge`），我们无法改；不影响功能（我们不设 badge）。
- **非 bundle 防崩**：Wails 内部 `checkBundleIdentifier` / `IsNotificationAvailable` 处理；`InitializeNotifications` 在非 bundle 返回错误、`ensureAuthorized` 拿到 false → 静默 no-op。`go test` / 裸二进制安全。
- **拒绝后恢复**：只缓存正向授权，未授权时每次重查，用户事后在系统设置打开即恢复。

## 降级与错误处理

- 原生不可用的所有情形统一**静默 no-op**：`Notify` 永远返回 `nil`，`deliver` 内忽略 `SendNotification` 错误。
- `InitializeNotifications` 失败仅 `Warn` 日志，不阻断 Startup。

## 前端

- `lib/turn-notify.ts`：
  - `NotifyDeps.showSystemNotification` 签名改 `(sessionId: number, title: string, body: string) => void`；`maybeNotify` 调用处传 `sessionId`。
  - **移除** `playSound` 依赖与 `if (s.sound) deps.playSound(...)` 分支（声音子系统删除）。
- `components/agentre/turn-complete-notifier.tsx`：
  - `showSystemNotification` 改 `(sessionId, title, body) => ShowNotification({ title, body, sessionId }).catch(() => {})`。
  - 新增 `notification:click` 订阅（`EventsOn`/`EventsOff`，镜像 `use-terminal.ts` 写法）→ `useChatTabsStore.getState().openSession(sessionId)`。
  - **移除** `playSound: playNotifySound` 接线与 import。
- `make generate` 重生成 `frontend/wailsjs`（`ShowRequest` 多 `sessionId`）。
- 复用 chat-tabs store 现有 `openSession(sessionId)`（`chat-tabs-store.ts:72`），无需新增 store 动作。
- i18n：点击导航无新增可见文案；通知标题/正文 key v1 已存在。声音相关 key 删除（见下）。

## 测试（TDD）

**后端 Go（mock，不连 DB）**
- `notification_svc`（`notification_test.go`）：`Notify(ctx, title, body, sessionID)` 透传（mock 断言 4 参）；空 title 兜底 `"Agentre"`；Notifier 报错向上传播；nil notifier no-op。更新现有用例签名（加 `ctx`、`sessionID`）。
- `app`：`sessionIDFromUserInfo` 纯函数单测 —— `float64(42)`→42；`int64`/`string` 容错；缺失/非法→0。
- `app_settings_svc`：删除 sound/sound_preset 校验用例（随子系统移除）。
- `sysnotify` 适配器：依赖 live Wails，**不单测**，靠手动清单。

**前端（Vitest）**
- `turn-notify`：`showSystemNotification` 带 `sessionId` 断言；移除 `playSound` 相关断言/deps。
- `turn-complete-notifier`：① `ShowNotification` 收到 `sessionId`；② 新增 —— 触发 `notification:click`（mock `EventsOn`）后以正确 `sessionId` 调 `openSession`；移除 `playSound` mock 与断言。
- `notifications-panel`：移除声音 Row 相关用例。
- `i18n.test.ts`：随声音 key 删除而更新。

**手动（适配器不可自动化，记入清单）**
1. 打包签名 `.app` 运行 → 后台触发回合完成。
2. 横幅显示 **Agentre 图标**（非 Script Editor）。
3. 点击通知 → **聚焦窗口 + 跳到对应会话**（已开切过去 / 未开新开 tab）。
4. 首次触发弹**系统授权窗**；授权后投递。
5. 拒绝授权 → 不弹、不崩；系统设置里打开后恢复。

## 涉及文件清单

### 核心：原生通知
- `internal/pkg/sysnotify/sysnotify.go`：重写为 Wails 运行时适配器（删 beeep import，加 `wailsruntime`；`Notify` 加 `ctx`+`sessionID`+懒授权）。
- `internal/service/notification_svc/notification.go`：`Notifier.Notify(ctx, title, body, sessionID)`；`types.go`：`ShowRequest.SessionID`；`mock_*` 重生成；`notification_test.go` 更新。
- `internal/app/app.go`：`Startup` 加 `InitializeNotifications` + `OnNotificationResponse`；新增纯函数 `sessionIDFromUserInfo` + 其测试。
- `go.mod` / `go.sum`：`go mod tidy` 移除 beeep。
- 前端：`lib/turn-notify.ts`、`components/agentre/turn-complete-notifier.tsx`、`frontend/wailsjs/*`（regen）+ 对应测试。

### 声音移除（单列一节，独立提交，保持可审/可 bisect）
- `internal/model/entity/app_setting_entity/app_setting.go`：删 `KeyNotifySound`、`KeyNotifySoundPreset`、`ValidateSoundPreset`。
- `internal/service/app_settings_svc/app_settings.go`：删 `KeyNotifySound` bool 校验 + `KeyNotifySoundPreset` case；对应测试用例删除。
- `internal/pkg/code/{code,en,zh_cn}.go`：删 `AppSettingInvalidSoundPreset`。
- `frontend/src/lib/notify-sound.ts`：删除文件。
- `frontend/src/stores/notification-settings-store.ts`：删 `sound`/`soundPreset`（type/defaults/KEYS/load）。
- `frontend/src/components/agentre/notifications-panel.tsx`：删声音 Row（Select+试听 Button+Switch）及 import。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json`：删 `settings.notifications.soundLabel/soundDesc/soundTest/preset.*`。
- 相关测试：`turn-notify.test.ts`、`turn-complete-notifier.test.tsx`、`notifications-panel` 测试、`i18n.test.ts`。

## 已知局限（future）

- 仅 `ID` 同会话去重，**无跨会话节流**：多会话密集完成仍连弹。
- bundle id 仍是默认占位 `com.wails.Agentre`；换自有 reverse-DNS 为独立任务。
- `wails dev` / 非打包 / 拒绝授权 / macOS < 10.14 下无系统通知（静默 no-op）。
- 授权选项含 `Sound|Badge`（Wails 固定），但我们不使用 badge。
- 无 per-event 开关、无 tab 会话 `done` 不弹等 v1 局限不在本次范围。

## 风险

- **Wails 适配器不可单测**：靠手动清单；纯逻辑（`sessionIDFromUserInfo`、`notification_svc` 透传）尽量上单测。
- **`InitializeNotifications` 时机**：须在 Startup 且窗口上下文就绪后调；若早于窗口就绪可能失败 —— 实现时验证（必要时 `OnDomReady` 兜底重试）。
- **userInfo 数字类型**：JSON 往返后 `sessionID` 为 `float64`，`sessionIDFromUserInfo` 必须容错。
- **删 beeep 的连带**：`go mod tidy` 后确认无其它引用；`make test-backend` + `make lint` 全绿。
- **声音移除的连带**：确保删干净（无悬挂 import / dead i18n key / `AppSettingInvalidSoundPreset` 残留引用），`i18n.test.ts` 通过。
