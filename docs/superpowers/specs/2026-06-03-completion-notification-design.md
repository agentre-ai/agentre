# 对话完成通知设计

**日期**: 2026-06-03
**状态**: 设计完成,待 review
**作者**: Claude (结对 with 王一之)

## 背景

当一个会话(session)的一轮对话(turn)跑完时,目前**没有任何主动提示**:用户切走去做别的事,只能靠不断回来看 tab 状态才知道 agent 干完没。诉求是——**turn 结束时主动通知用户**,并且通知方式可在「设置 → 通知」里配置。

### 现状(已核实)

- **后端 turn 收尾**在 `internal/service/chat_svc/chat.go` 的 `finalize`(~2450–2581 行):把 `sess.AgentStatus` 落成 `idle` / `error` / `waiting` 并 `persistSessionStatus`,随后 emit 终态事件(`StreamDone` / `StreamError` / `StreamAborted` / `StreamSessionStatus` / `StreamClosed`)。
- **前端单一事实源**是 `frontend/src/stores/session-status-store.ts` 的 `useSessionStatusStore`:按 `sessionId` 存 `{ agentStatus, needsAttention, doneTick, lastDoneEvent }`;`doneTick` 在每次 turn 结束自增,`lastDoneEvent.kind ∈ {done, error, aborted, closed, steer_consumed}`。订阅惯例见 `chat-panel.tsx`(用 ref 跳过首挂载、watch `doneTick`)。
- **当前会话**:`frontend/src/stores/chat-tabs-store.ts` 的 `useChatTabsStore.activeTabId` + `tabs`;`activeTab.meta.kind === "session"` 时 `activeTab.meta.sessionId` 即用户正在看的会话(见 `chat-page.tsx`)。
- **设置基建**:Wails 绑定 `GetAppSetting(GetRequest{key})` / `UpdateAppSettings(UpdateRequest{entries:[{key,value}]})`;后端 `app_settings_svc.Update` 用 **per-key switch** 校验,未知 key 直接报 `AppSettingNotFound`。样板面板 `settings-proxy.tsx`(load→本地 state→Apply 时 `UpdateAppSettings`)。设置页 `settings.tsx` 里 `notifications` 项现在是 `UnderConstructionPage` 占位。
- **依赖**:`go.mod` 里 `git.sr.ht/~jackmordaunt/go-toast/v2` 已在依赖树(indirect);`beeep` **未引入**。前端 `sonner@^2.0.7` 已在 `App.tsx` 挂载 `<Toaster position="bottom-right" richColors />`。shadcn 有 `switch` / `select` / `button`,**无** `label` / `slider`。
- **不存在**:任何系统通知 / 声音 / 窗口焦点检测代码(`window` 的 `focus`/`blur` 监听本身在本 webview 已被 `use-remote-devices.ts` 证明可用)。

## 已确认的设计决策

1. **三个通知渠道,各自独立开关、均可配置**:系统原生通知、提示音、应用内 toast。外加一个总开关。
2. **提示音用 Web Audio API 实时合成**几个短音预设(零二进制资产、无版权问题),不捆绑音频文件。
3. **触发的终态**:`done`(完成)、`error`(出错)、`waiting`(等用户输入)三种都通知。v1 **不做** per-event 开关(YAGNI)。用户自己点「停止」导致的 `aborted` **不通知**。
4. **打扰门槛**:`正在看 = 窗口聚焦 且 完成的会话 == 当前会话`。正在看 ⇒ 全部静默;否则(**失焦 或 非当前会话**)⇒ 触发各启用渠道。系统通知正是在「失焦 或 非当前会话」时弹。
5. **决策放前端**:见下方架构选择。

## 架构选择:谁来决策「要不要通知 + 怎么通知」

| 方案 | 做法 | 取舍 |
|---|---|---|
| **A(采纳)前端编排 + 后端薄绑定** | 前端 `TurnCompleteNotifier` 用本地已有信号(`session-status` 转换 + `activeTabId` + 窗口焦点)做全部决策;系统通知调一个薄 Wails 绑定 `App.ShowNotification(title, body)` 去弹。 | 「非当前会话」判定前端天然成立;通知文案在前端 `t()` 走 i18n;后端零业务逻辑。 |
| B 后端编排 | `finalize` 里读设置 + 查窗口焦点 + 直接 beeep。 | 后端**不知道**前端在看哪个会话,「非当前会话」判定不了;后端拼文案难 i18n;耦合重。否决。 |
| C 新增 turn-complete 语义事件 | 后端额外发一个事件,前端再决策。 | 相比 A 多一个冗余事件,`session-status-store` 已能表达 turn 结束,无必要。否决。 |

**采纳 A**。系统通知是唯一**必须**走原生的部分(WKWebView / WebView2 的 Web Notification API 不可靠),其余都在前端完成。

## 触发与判定(前端)

新增 `TurnCompleteNotifier` 宿主组件,挂在 App 根、与 `chat-streams-host` 并列(跨路由常驻)。

**转换边检测**:订阅 `useSessionStatusStore` 全量(`statuses` map),用「每会话上一次 `agentStatus`」的 ref 检测 `running →` 的转换:

| 转换 | 分类 | 说明 |
|---|---|---|
| `running → idle` | 完成 | 若 `lastDoneEvent.kind === "aborted"` ⇒ **跳过**(用户自己停的) |
| `running → error` | 出错 | |
| `running → waiting` | 等你输入 | agent 卡住要权限 / 问问题 |

- **去重**:基于「上一状态」边检测,离开 `running` 后尾随的 `closed` 不会再次进入 `running`,天然每个 turn 只触发一次。
- **挂载 seed**:首次拿到 `statuses` 时先把各会话现状写进 ref,**不触发**——避免重启/重连时已 idle 的老会话误报。
- **分类后**:计算 `正在看`,通过门槛后调 `notify(kind, sessionId)` 触发各启用渠道。

**门槛**:
```
正在看 = isWindowFocused && completedSessionId === activeSessionId
若 正在看 → 静默
否则 → 触发各「已启用」渠道(系统通知 / 提示音 / toast)
```
- `activeSessionId` 来自 `useChatTabsStore`(`activeTab.meta.kind==="session" ? sessionId : 0`)。
- `isWindowFocused` 来自新增 `useWindowFocus`(见下)。

**通知文案**(前端 `t()`,按 kind):title 用会话/agent 名(取不到则 `"Agentre"`),body 例:
- 完成 → `notify.body.done`(例「会话已完成」/ "Session completed")
- 出错 → `notify.body.error`(例「会话出错」/ "Session failed")
- 等待 → `notify.body.waiting`(例「需要你的操作」/ "Waiting for your input")

toast 按 kind 选 `toast.success` / `toast.error` / `toast.info`(`sonner`);系统通知把**已本地化**的 title/body 传给后端。

## 前端组件

### 1. `frontend/src/hooks/use-window-focus.ts`(新增)
`window` 的 `focus`/`blur` + `document.visibilityState`(`visibilitychange`)合成一个 `isWindowFocused: boolean`。初值 `document.hasFocus() && document.visibilityState === "visible"`。

### 2. `frontend/src/lib/notify-sound.ts`(新增)
Web Audio 合成的提示音预设。导出 `NOTIFY_SOUND_PRESETS`(枚举集合)与 `playNotifySound(preset)`:
- `ding` —— 单个明亮短音(正弦,~880Hz,短包络)。
- `chime` —— 上行三音琶音。
- `blip` —— 短促低频 blip。
- (面板里「无」= 关闭提示音渠道,不进 preset。)

惰性 `new AudioContext()`,首次用户交互后可用;合成节点用短 gain 包络避免爆音。

### 3. `frontend/src/components/agentre/notifications-panel.tsx`(新增)
镜像 `settings-proxy.tsx` 的 load 模式:挂载时 `GetAppSetting` 逐 key 读进本地 state。保存策略——**开关/选择改动即时 `UpdateAppSettings`**(通知设置无重启副作用,不像 proxy 需要 Apply 按钮),保存后刷新共享 store。控件:
- 总开关 `Switch`(`notify.enabled`)。
- 系统通知 `Switch`(`notify.system`)。
- 提示音 `Switch`(`notify.sound`)+ 预设 `Select`(`notify.sound_preset`)+ **「试听」** `Button`(调 `playNotifySound`)。
- toast `Switch`(`notify.toast`)。
- 无 `label` 组件 → 用原生 `<label>`;全部文案走 i18n。

在 `settings.tsx` 把 `notifications` 分支的 `UnderConstructionPage` 换成 `<NotificationsPanel/>`。

### 4. `frontend/src/stores/notification-settings-store.ts`(新增,可选)
集中读取/缓存通知设置,供 `TurnCompleteNotifier` 与面板共用,避免每次通知都打 RPC。面板保存后刷新该 store。

### 5. `frontend/src/components/agentre/turn-complete-notifier.tsx`(新增)
上面「触发与判定」的实现。把副作用依赖 `playSound` / `showSystemNotification`(调 Wails `ShowNotification`)/ `showToast` / `isAppFocused` 设计为**可注入**,以便单测。

## 后端组件(薄)

### 1. `internal/pkg/sysnotify`(新增,平台叶子)
`Notifier` 接口的 beeep 实现:`Notify(title, body string) error` → `beeep.Notify(title, body, "")`(静音版,避免和前端提示音重复响)。新增直接依赖 `github.com/gen2brain/beeep`(其 Windows 后端正是已存在的 `go-toast/v2`)。

### 2. `internal/service/notification_svc`(新增)
```go
type Notifier interface { Notify(title, body string) error } // 消费侧定义(ISP)

type NotificationSvc interface {
    Notify(ctx context.Context, req *NotifyRequest) (*NotifyResponse, error)
}
```
`Notify` 做空 title 兜底(→ `"Agentre"`)等少量校验,再委托注入的 `Notifier`。`RegisterNotification(impl)` + `Default()` accessor,与现有 svc 一致。

### 3. `internal/app/notification.go` + 接线
- `App.ShowNotification(req notification_svc.NotifyRequest) error`:只做 parse → `notification_svc.Default().Notify(ctx, &req)` → return。
- 在 `app.go` 里 `notification_svc.RegisterNotification(sysnotify.New())` 接线 beeep 实现。
- `make generate` 刷新 `frontend/wailsjs` 绑定。

## 设置存储(沿用 `app_settings_svc`,离散 key)

在 `app_setting_entity` 增 const key,并在 `app_settings_svc.Update` 的 switch 加 case:

| key | 类型 | 校验 |
|---|---|---|
| `notify.enabled` | bool | 值 ∈ {"true","false"} |
| `notify.system` | bool | 同上 |
| `notify.sound` | bool | 同上 |
| `notify.sound_preset` | enum | 值 ∈ {ding, chime, blip} |
| `notify.toast` | bool | 同上 |

bool 用 `ValidateBoolSetting`、preset 用 `ValidateSoundPreset`(放在 `app_setting_entity`,与现有 `ValidateProxyHost/Port` 同处)。**无新表、无新迁移**——复用既有 KV。读取未设置(空值)时,前端按以下**默认值**兜底:

| key | 默认 |
|---|---|
| `notify.enabled` | `true`(总开关默认开) |
| `notify.system` | `true` |
| `notify.sound` | `false`(默认不响,避免打扰;用户主动开启) |
| `notify.sound_preset` | `ding` |
| `notify.toast` | `false`(默认关;用户主动开启) |

> 默认下只有「系统通知」开着,提示音与应用内 toast 都默认关 —— 默认行为克制,用户按需开启。

## 数据流总览

```
后端 finalize → AgentStatus=idle/error/waiting + emit 终态事件
   → Wails EventsEmit → 前端 session-status-store(doneTick++ / agentStatus 转换)
      → TurnCompleteNotifier 检测 running→{idle|error|waiting} 转换(跳过 aborted)
         → 计算「正在看」门槛
            → 失焦 或 非当前会话 时,触发各启用渠道:
                 · toast  → sonner(前端)
                 · 提示音 → playNotifySound(preset)(前端 Web Audio)
                 · 系统通知 → App.ShowNotification(t(title), t(body)) → notification_svc → beeep(原生)
```

## 测试(TDD,Red → Green → Refactor)

**后端**
- `notification_svc`:mockgen 出 `Notifier` mock,断言 `Notify` 收到期望 title/body;空 title → 兜底 `"Agentre"`;`Notifier` 报错向上传播。**不连 DB**。
- `app_settings_svc`:给 5 个新 key 加 Update 校验用例(合法 bool 通过、非法 bool 报错、preset 合法/非法),镜像现有 proxy 校验测试,sqlmock。

**前端(Vitest)**
- `turn-complete-notifier`:注入 mock 的 `playSound`/`showSystemNotification`/`showToast`/`isAppFocused`/`activeSessionId`,断言:
  - 非当前会话 `running→idle` ⇒ 触发三渠道;
  - 聚焦 + 当前会话 `running→idle` ⇒ 全部静默;
  - `lastDoneEvent.kind==="aborted"` ⇒ 跳过;
  - `error` / `waiting` ⇒ 正确分类文案;
  - 同一 turn **不重复触发**;挂载 seed 不误报。
- `notifications-panel`:渲染各开关、load(`GetAppSetting`)/save(`UpdateAppSettings`)、「试听」调 `playNotifySound`。
- `use-window-focus`:focus/blur/visibilitychange 切换 `isWindowFocused`。
- `i18n.test.ts`:新增文案 key 在 `zh-CN` 与 `en` 双语齐全。

## 已知局限(记为 future,v1 不做)

- **beeep 是 fire-and-forget**:点击系统通知**无法**聚焦窗口 / 跳转到对应会话。
- **无节流**:多个会话短时间密集完成会连弹多条通知。
- **后台音频**:app 完全最小化时,部分平台可能挂起 webview 音频导致提示音不响;系统通知此时仍可靠,可兜底。
- **无 per-event 开关**:done/error/waiting 不能分别开关(全开)。
