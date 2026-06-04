# 原生系统通知（图标 + 点击跳转）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** macOS（及 Win/Linux）系统通知显示 Agentre 自己的图标，点击通知聚焦窗口并跳到对应会话；改用 Wails 内置原生通知，删除 beeep 与前端声音子系统。

**Architecture:** 复用 Wails v2.12 公开运行时通知 API（`github.com/wailsapp/wails/v2/pkg/runtime`），零 CGO。`notification_svc.Notifier` 接口加 `ctx`+`sessionID`；`internal/pkg/sysnotify` 重写为薄 Wails 适配器（懒授权 + fire-and-forget）；`app.Startup` 注册 `InitializeNotifications` + `OnNotificationResponse`（点击 → 聚焦窗口 + emit 事件）；前端把 `sessionId` 串进通知并订阅 `notification:click` → `openSession`。

**Tech Stack:** Go 1.26 / Wails v2.12 / cago / goconvey + gomock + sqlmock / React 19 + TS + Vitest / Zustand。

**前置约定（每个 worker 必读）：**
- 工作目录：`/Users/codfrm/Code/agentre/agentre`（agentre 仓库内提交，**勿混入 agentre-server**）。
- 后端聚焦测试：`go test -race ./<pkg>/...`；全量 `make test-backend`（排除 frontend）。
- 前端测试：`cd frontend && pnpm test -- <path>`。
- mock 重生成：`go generate ./internal/service/notification_svc/`（需 mockgen，已在 toolchain）。
- 提交用 gitmoji。**只在计划明确写 commit 步骤时提交**，提交到当前分支 `feat/completion-notification`。
- 分两阶段：Phase 1 原生通知（Task 1-4）、Phase 2 声音移除（Task 5-6）、Task 7 收尾验证。

> **执行落地校准（事后补记）** —— 本计划已执行并合入 `feat/completion-notification`。实际落地与下文略有出入，以此为准：
> - **行号是编写时快照**，执行时一律以符号/内容定位。
> - **Task 2** 还需同步更新既有 `internal/pkg/sysnotify/sysnotify_test.go`（它仍断言旧的 2 参接口形状，否则该包测试编译不过）。
> - **Task 4** `frontend/wailsjs` 是 **gitignore 的生成物**：`make generate` 重生成以保证类型正确，但**不提交**（提交只含源码+测试）。另需在 `frontend/src/__tests__/App.test.tsx` 加一个最小 runtime mock（只桩 `EventsOn`/`EventsOff`），否则新挂载期的 `EventsOn` 会让 ~33 个未设置 `window.runtime` 的 App 用例崩。
> - **Task 6** 除所列文件外，还需：删 `frontend/src/lib/__tests__/notify-sound.test.ts`、改 `frontend/src/stores/__tests__/notification-settings-store.test.ts` 与 `frontend/src/components/agentre/__tests__/notifications-panel.test.tsx`、微调 `frontend/src/__tests__/i18n.test.ts` 里硬编码的声音 key。
> - **收尾**额外修了分支既有的 2 个 `prettier/prettier` 报错（`notification-toast.test.tsx`、`session-avatar.test.ts`，非本功能引入）使 `make lint` 绿（30 个既有 warning 未碰）。
> - **实际提交**：Task 1-6 各 1 个 + lint 修复 1 个 + 本 spec/plan 文档 1 个。

---

## Phase 1：原生通知（Wails 运行时）

### Task 1：`notification_svc` 接口加 `ctx`+`sessionID`，`ShowRequest` 加 `SessionID`

**Files:**
- Modify: `internal/service/notification_svc/notification.go`
- Modify: `internal/service/notification_svc/types.go`
- Regenerate: `internal/service/notification_svc/mock_notification_svc/mock_notification.go`
- Test: `internal/service/notification_svc/notification_test.go`

- [ ] **Step 1: 改测试到新签名（先红）**

把 `internal/service/notification_svc/notification_test.go` 整体替换为：

```go
package notification_svc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"agentre/internal/service/notification_svc"
	"agentre/internal/service/notification_svc/mock_notification_svc"
)

func TestShow(t *testing.T) {
	convey.Convey("Show", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		n := mock_notification_svc.NewMockNotifier(ctrl)
		notification_svc.RegisterNotifier(n)
		t.Cleanup(func() { notification_svc.RegisterNotifier(nil) })

		convey.Convey("透传 title/body/sessionID", func() {
			n.EXPECT().Notify(gomock.Any(), "fix bug", "已完成", int64(42)).Return(nil)
			assert.NoError(t, notification_svc.Notification().Show(
				context.Background(),
				&notification_svc.ShowRequest{Title: "fix bug", Body: "已完成", SessionID: 42}))
		})

		convey.Convey("空 title 兜底 Agentre", func() {
			n.EXPECT().Notify(gomock.Any(), "Agentre", "x", int64(0)).Return(nil)
			assert.NoError(t, notification_svc.Notification().Show(
				context.Background(), &notification_svc.ShowRequest{Title: "  ", Body: "x"}))
		})

		convey.Convey("Notifier 报错向上传播", func() {
			n.EXPECT().Notify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("boom"))
			assert.Error(t, notification_svc.Notification().Show(
				context.Background(), &notification_svc.ShowRequest{Title: "t", Body: "b"}))
		})
	})

	convey.Convey("未注入 notifier 时 no-op", t, func() {
		notification_svc.RegisterNotifier(nil)
		assert.NoError(t, notification_svc.Notification().Show(
			context.Background(), &notification_svc.ShowRequest{Title: "t"}))
	})
}
```

- [ ] **Step 2: 跑测试看它失败（编译错误即红）**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/notification_svc/...`
Expected: FAIL —— mock 的 `Notify` 仍是旧的两参签名 / `ShowRequest` 无 `SessionID`，编译不过。

- [ ] **Step 3: 改接口与 Show**

把 `internal/service/notification_svc/notification.go` 第 10-13 行的 `Notifier` 接口与第 33-42 行的 `Show` 改为：

```go
// Notifier 平台原生通知能力，由 internal/pkg/sysnotify 提供实现，bootstrap 注入。
// 携带 ctx（Wails 运行时调用需要）与 sessionID（点击时跳转用）。
type Notifier interface {
	Notify(ctx context.Context, title, body string, sessionID int64) error
}
```

```go
// Show 弹一条系统通知；未注入实现或 req 为空时安全 no-op。空 title 兜底为 "Agentre"。
func (s *notificationSvc) Show(ctx context.Context, req *ShowRequest) error {
	if s.notifier == nil || req == nil {
		return nil
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Agentre"
	}
	return s.notifier.Notify(ctx, title, req.Body, req.SessionID)
}
```

- [ ] **Step 4: `ShowRequest` 加字段**

把 `internal/service/notification_svc/types.go` 的 `ShowRequest` 改为：

```go
// ShowRequest 展示一条系统通知。Title/Body 已由前端按 i18n 生成。
// SessionID 标识来源会话，供点击通知时聚焦/跳转（0 = 无具体会话）。
type ShowRequest struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	SessionID int64  `json:"sessionId"`
}
```

- [ ] **Step 5: 重生成 mock**

Run: `cd /Users/codfrm/Code/agentre/agentre && go generate ./internal/service/notification_svc/`
Expected: `mock_notification_svc/mock_notification.go` 的 `Notify` 变为 `Notify(ctx context.Context, title, body string, sessionID int64) error`。

- [ ] **Step 6: 跑测试看它通过**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test -race ./internal/service/notification_svc/...`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/service/notification_svc/
git commit -m "♻️ notify: Notifier 接口加 ctx 与 sessionID（为原生通知做准备）"
```

---

### Task 2：`sysnotify` 重写为 Wails 运行时适配器，删除 beeep

**Files:**
- Rewrite: `internal/pkg/sysnotify/sysnotify.go`
- Modify: `go.mod` / `go.sum`（`go mod tidy` 移除 beeep）

> 该适配器依赖 live Wails 运行时，无法单测；正确性靠 Task 7 手动清单。本任务只保证编译通过 + 结构化满足 `notification_svc.Notifier`。

- [ ] **Step 1: 重写 sysnotify.go**

把 `internal/pkg/sysnotify/sysnotify.go` 整体替换为：

```go
// Package sysnotify 基于 Wails 内置原生通知的系统通知实现（平台叶子，不反向依赖 service 层）。
// macOS 走 UNUserNotificationCenter（由 Wails 维护），通知以 app 自身身份投递 → 自动显示 app 图标。
package sysnotify

import (
	"context"
	"fmt"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Notifier 结构化满足 notification_svc.Notifier（不 import service，避免 pkg 反向依赖）。
type Notifier struct {
	mu         sync.Mutex
	authorized bool // 懒触发：一旦授权成功即缓存，跳过后续 check
}

// New 构造一个系统通知器。
func New() *Notifier { return &Notifier{} }

// Notify fire-and-forget：把懒授权 + 投递丢给 goroutine，立即返回 nil（不阻塞前端 RPC）。
// 非 bundle / 拒绝授权 / 平台不支持时静默 no-op。
func (n *Notifier) Notify(ctx context.Context, title, body string, sessionID int64) error {
	go n.deliver(ctx, title, body, sessionID)
	return nil
}

func (n *Notifier) deliver(ctx context.Context, title, body string, sessionID int64) {
	if !n.ensureAuthorized(ctx) {
		return
	}
	_ = wailsruntime.SendNotification(ctx, wailsruntime.NotificationOptions{
		ID:    fmt.Sprintf("session-%d", sessionID), // 同会话去重/替换
		Title: title,
		Body:  body,
		Data:  map[string]interface{}{"sessionID": sessionID},
	})
}

// ensureAuthorized 懒检查授权；未确定则请求（macOS 首次弹窗）。只缓存正向结果，
// 这样用户事后在系统设置里打开通知后能恢复。
func (n *Notifier) ensureAuthorized(ctx context.Context) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.authorized {
		return true
	}
	ok, _ := wailsruntime.CheckNotificationAuthorization(ctx)
	if !ok {
		ok, _ = wailsruntime.RequestNotificationAuthorization(ctx)
	}
	n.authorized = ok
	return ok
}
```

- [ ] **Step 2: tidy 掉 beeep**

Run: `cd /Users/codfrm/Code/agentre/agentre && go mod tidy`
Expected: `go.mod` 中 `github.com/gen2brain/beeep` 行消失（确认无其它包引用：`grep -rl gen2brain ./internal ./cmd ./pkg 2>/dev/null` 应为空）。

- [ ] **Step 3: 编译 + 确认结构化满足接口**

Run: `cd /Users/codfrm/Code/agentre/agentre && go build ./internal/... ./cmd/...`
Expected: 通过（`bootstrap/cago.go` 的 `notification_svc.RegisterNotifier(sysnotify.New())` 仍编译，证明 `*sysnotify.Notifier` 满足新接口）。

- [ ] **Step 4: 跑通知服务测试确认未回归**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test -race ./internal/service/notification_svc/... ./internal/bootstrap/...`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/pkg/sysnotify/sysnotify.go go.mod go.sum
git commit -m "✨ notify: sysnotify 改用 Wails 内置原生通知，移除 beeep"
```

---

### Task 3：`app.Startup` 注册初始化 + 点击回调；`sessionIDFromUserInfo` 纯函数 + 测试

**Files:**
- Modify: `internal/app/notification.go`（加 `sessionIDFromUserInfo` + `RegisterNotificationHandlers`）
- Modify: `internal/app/app.go`（`Startup` 调用 `RegisterNotificationHandlers`）
- Test: `internal/app/notification_test.go`

- [ ] **Step 1: 写 sessionIDFromUserInfo 的失败测试（先红）**

新建 `internal/app/notification_test.go`：

```go
package app

import "testing"

func TestSessionIDFromUserInfo(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]interface{}
		want int64
	}{
		{"float64(JSON 往返)", map[string]interface{}{"sessionID": float64(42)}, 42},
		{"int64", map[string]interface{}{"sessionID": int64(7)}, 7},
		{"string 数字", map[string]interface{}{"sessionID": "13"}, 13},
		{"缺失", map[string]interface{}{"other": 1}, 0},
		{"nil map", nil, 0},
		{"非法字符串", map[string]interface{}{"sessionID": "abc"}, 0},
	}
	for _, c := range cases {
		if got := sessionIDFromUserInfo(c.in); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/app/ -run TestSessionIDFromUserInfo`
Expected: FAIL —— `sessionIDFromUserInfo` 未定义，编译错误。

- [ ] **Step 3: 实现 helper + 注册函数**

把 `internal/app/notification.go` 整体替换为：

```go
package app

import (
	"strconv"

	"agentre/internal/service/notification_svc"

	"github.com/cago-frame/cago/pkg/logger"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"go.uber.org/zap"
)

// ShowNotification 弹一条系统通知；文案已由前端按 i18n 生成。
func (a *App) ShowNotification(req *notification_svc.ShowRequest) error {
	return notification_svc.Notification().Show(a.ctx, req)
}

// RegisterNotificationHandlers 在 Startup 调用：初始化 Wails 通知 + 注册点击回调。
// 非 bundle / 旧系统下 InitializeNotifications 报错 → 仅告警降级。
func (a *App) RegisterNotificationHandlers() {
	if err := wailsruntime.InitializeNotifications(a.ctx); err != nil {
		logger.Ctx(a.ctx).Warn("app.RegisterNotificationHandlers: init notifications", zap.Error(err))
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
}

// sessionIDFromUserInfo 从通知 userInfo 取 sessionID，兼容 JSON 往返后的 float64、int64、数字字符串；
// 缺失或非法返回 0。
func sessionIDFromUserInfo(userInfo map[string]interface{}) int64 {
	v, ok := userInfo["sessionID"]
	if !ok {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}
```

- [ ] **Step 4: 跑 helper 测试看它通过**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/app/ -run TestSessionIDFromUserInfo`
Expected: PASS。

- [ ] **Step 5: 在 Startup 接线**

在 `internal/app/app.go` 的 `Startup` 中、`a.ctx = ctx` 之后、`a.resetStaleSessionsOnStartup(ctx)` 之前插入一行：

```go
	a.ctx = ctx
	a.RegisterNotificationHandlers()
	a.resetStaleSessionsOnStartup(ctx)
```

- [ ] **Step 6: 编译全后端**

Run: `cd /Users/codfrm/Code/agentre/agentre && go build ./internal/... ./cmd/... && go test -race ./internal/app/ -run TestSessionIDFromUserInfo`
Expected: 编译通过 + 测试 PASS。

- [ ] **Step 7: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/app/notification.go internal/app/notification_test.go internal/app/app.go
git commit -m "✨ notify: app 注册原生通知初始化与点击回调（聚焦窗口+emit notification:click）"
```

---

### Task 4：前端串 `sessionId` + 订阅 `notification:click` → `openSession`

**Files:**
- Regenerate: `frontend/wailsjs/*`（`make generate`）
- Modify: `frontend/src/lib/turn-notify.ts`
- Modify: `frontend/src/components/agentre/turn-complete-notifier.tsx`
- Test: `frontend/src/lib/__tests__/turn-notify.test.ts`
- Test: `frontend/src/components/agentre/__tests__/turn-complete-notifier.test.tsx`

- [ ] **Step 1: 重生成 Wails 绑定（ShowRequest 多 sessionId）**

Run: `cd /Users/codfrm/Code/agentre/agentre && make generate`
Expected: `frontend/wailsjs/go/models.ts` 的 `notification_svc.ShowRequest` 多出 `sessionId: number`。

- [ ] **Step 2: 改 turn-notify 测试到新 showSystemNotification 签名（先红）**

在 `frontend/src/lib/__tests__/turn-notify.test.ts` 中，把 `maybeNotify` 三处对 `showSystemNotification` 的断言改为带 sessionId（第一参为 `42`）：

第 50-53 行的断言改为：
```ts
    expect(d.showSystemNotification).toHaveBeenCalledWith(
      42,
      "我的会话",
      "notify.body.done",
    );
```

第 118-121 行的断言改为：
```ts
    expect(d.showSystemNotification).toHaveBeenCalledWith(
      42,
      "notify.app",
      "notify.body.error",
    );
```

- [ ] **Step 3: 跑 turn-notify 测试看它失败**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/lib/__tests__/turn-notify.test.ts`
Expected: FAIL —— `showSystemNotification` 仍按旧两参被调用，断言不匹配。

- [ ] **Step 4: 改 turn-notify.ts 签名与调用**

在 `frontend/src/lib/turn-notify.ts`：

第 15 行 `NotifyDeps.showSystemNotification` 改为：
```ts
  showSystemNotification: (sessionId: number, title: string, body: string) => void;
```

第 57 行调用改为：
```ts
  if (s.system) deps.showSystemNotification(sessionId, title, body);
```

- [ ] **Step 5: 跑 turn-notify 测试看它通过**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/lib/__tests__/turn-notify.test.ts`
Expected: PASS。

- [ ] **Step 6: 给 turn-complete-notifier 加点击订阅的失败测试（先红）**

在 `frontend/src/components/agentre/__tests__/turn-complete-notifier.test.tsx` 顶部，紧跟现有 `vi.mock("../../../lib/window-focus", ...)` 之后，加入对 runtime 的 mock：

```ts
const eventHandlers: Record<string, (data: unknown) => void> = {};
vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: (event: string, cb: (data: unknown) => void) => {
    eventHandlers[event] = cb;
    return () => delete eventHandlers[event];
  },
  EventsOff: (event: string) => {
    delete eventHandlers[event];
  },
}));
```

在 `describe("TurnCompleteNotifier", ...)` 内新增用例：

```ts
  it("收到 notification:click 事件 → 打开/激活对应会话 tab", async () => {
    render(<TurnCompleteNotifier />);
    await act(async () => {});
    act(() => {
      eventHandlers["notification:click"]?.(99);
    });
    const st = useChatTabsStore.getState();
    const tab = st.tabs.find((x) => x.id === st.activeTabId);
    expect(tab?.meta).toMatchObject({ kind: "session", sessionId: 99 });
  });
```

- [ ] **Step 7: 跑 turn-complete-notifier 测试看它失败**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/components/agentre/__tests__/turn-complete-notifier.test.tsx`
Expected: FAIL —— 组件还没订阅 `notification:click`，`eventHandlers["notification:click"]` 为 undefined，tab 未打开。

- [ ] **Step 8: 改 turn-complete-notifier.tsx**

在 `frontend/src/components/agentre/turn-complete-notifier.tsx`：

顶部加 import（紧跟第 4 行 `ShowNotification` import 之后）：
```ts
import { EventsOn, EventsOff } from "../../../wailsjs/runtime/runtime";
```

第 41-43 行 `showSystemNotification` 改为带 sessionId：
```ts
      showSystemNotification: (sessionId, title, body) => {
        ShowNotification({ title, body, sessionId }).catch(() => {});
      },
```

在第二个 `React.useEffect`（订阅 session 状态那个，第 59 行起的 `load()` effect 之后、或之前均可）旁，新增一个订阅 effect：
```ts
  React.useEffect(() => {
    EventsOn("notification:click", (sessionId: number) => {
      useChatTabsStore.getState().openSession(sessionId);
    });
    return () => EventsOff("notification:click");
  }, []);
```

- [ ] **Step 9: 跑 turn-complete-notifier 测试看它通过**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/components/agentre/__tests__/turn-complete-notifier.test.tsx`
Expected: PASS（原有用例 + 新点击用例都过）。

- [ ] **Step 10: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
# 注意：frontend/wailsjs 是 gitignore 生成物，不要提交（make generate 重生成即可）。
git add frontend/src/lib/turn-notify.ts frontend/src/components/agentre/turn-complete-notifier.tsx frontend/src/lib/__tests__/turn-notify.test.ts frontend/src/components/agentre/__tests__/turn-complete-notifier.test.tsx frontend/src/__tests__/App.test.tsx
git commit -m "✨ notify(fe): 通知带 sessionId，点击经 notification:click 跳转会话"
```

---

## Phase 2：移除前端声音子系统

> 因 Wails `SendNotification` 硬编码系统提示音、无法静音，前端声音会与系统声音双响。统一用系统声音，删除前端声音子系统。

### Task 5：后端移除 `notify.sound` / `notify.sound_preset` 及校验、错误码

**Files:**
- Modify: `internal/model/entity/app_setting_entity/app_setting.go`
- Modify: `internal/service/app_settings_svc/app_settings.go`
- Modify: `internal/pkg/code/code.go` / `en.go` / `zh_cn.go`
- Test: `internal/service/app_settings_svc/`（删除 sound preset 校验用例）

- [ ] **Step 1: 找出并删除 app_settings_svc 的 sound 校验用例（先红）**

Run: `cd /Users/codfrm/Code/agentre/agentre && grep -rn "Sound\|sound_preset\|SoundPreset" internal/service/app_settings_svc/`
对找到的 `*_test.go` 中针对 `notify.sound` / `notify.sound_preset` 的校验用例（合法/非法 preset、sound bool）整段删除。删完先不动实现。

- [ ] **Step 2: 跑测试确认仍编译（基线绿，准备改实现）**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/app_settings_svc/...`
Expected: PASS（仅删了测试用例，实现还在）。

- [ ] **Step 3: 删 entity 常量与校验函数**

在 `internal/model/entity/app_setting_entity/app_setting.go`：

删第 37-38 行两个常量，保留其余 notify 键：
```go
	KeyNotifyEnabled           = "notify.enabled"             // 通知总开关
	KeyNotifyOnlyWhenUnfocused = "notify.only_when_unfocused" // 仅窗口未激活时通知
	KeyNotifySystem            = "notify.system"              // 系统原生通知
	KeyNotifyToast             = "notify.toast"               // 应用内 toast
```
把第 33 行注释 `// 通知设置。bool 型存 "true"/"false"；sound_preset 为枚举。` 改为：
```go
	// 通知设置。bool 型存 "true"/"false"。
```
删除第 139-146 行的 `ValidateSoundPreset` 整个函数。

- [ ] **Step 4: 删 service 校验 case**

在 `internal/service/app_settings_svc/app_settings.go` 第 79-89 行附近，把 notify 校验改为（去掉 `KeyNotifySound` 与 `KeyNotifySoundPreset` 分支）：

```go
		case app_setting_entity.KeyNotifyEnabled,
			app_setting_entity.KeyNotifyOnlyWhenUnfocused,
			app_setting_entity.KeyNotifySystem,
			app_setting_entity.KeyNotifyToast:
			if err := app_setting_entity.ValidateBoolSetting(ctx, val); err != nil {
				return err
			}
```
（删除原 `app_setting_entity.KeyNotifySound,` 行与整个 `case app_setting_entity.KeyNotifySoundPreset:` 分支。）

- [ ] **Step 5: 删错误码 AppSettingInvalidSoundPreset**

- `internal/pkg/code/code.go`：删第 65 行 `AppSettingInvalidSoundPreset // 提示音预设非法`。
- `internal/pkg/code/en.go`：删第 46 行 `AppSettingInvalidSoundPreset: "Unknown notification sound preset",`。
- `internal/pkg/code/zh_cn.go`：删第 46 行 `AppSettingInvalidSoundPreset: "提示音预设不在可选范围内",`。

- [ ] **Step 6: 编译 + 测试**

Run: `cd /Users/codfrm/Code/agentre/agentre && go build ./internal/... && go test -race ./internal/service/app_settings_svc/... ./internal/model/entity/app_setting_entity/...`
Expected: 通过（无 `AppSettingInvalidSoundPreset` / `ValidateSoundPreset` / `KeyNotifySound*` 残留引用）。

- [ ] **Step 7: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/model/entity/app_setting_entity/app_setting.go internal/service/app_settings_svc/ internal/pkg/code/
git commit -m "➖ notify: 移除 notify.sound / sound_preset 设置与校验（统一用系统通知声音）"
```

---

### Task 6：前端移除声音子系统（lib / store / panel / i18n / 测试）

**Files:**
- Delete: `frontend/src/lib/notify-sound.ts`
- Modify: `frontend/src/stores/notification-settings-store.ts`
- Modify: `frontend/src/components/agentre/notifications-panel.tsx`
- Modify: `frontend/src/lib/turn-notify.ts`、`frontend/src/components/agentre/turn-complete-notifier.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`、`frontend/src/i18n/locales/en/common.json`
- Test: 上述对应 `*.test.ts(x)` + `frontend/src/__tests__/i18n.test.ts`

- [ ] **Step 1: 从 turn-notify.ts 移除 playSound**

在 `frontend/src/lib/turn-notify.ts`：
- 删第 6 行 `import type { SoundPreset } from "./notify-sound";`。
- 删 `NotifyDeps` 中的 `playSound: (preset: SoundPreset) => void;` 行。
- 删 `maybeNotify` 中的 `if (s.sound) deps.playSound(s.soundPreset);` 行。

- [ ] **Step 2: 从 turn-complete-notifier.tsx 移除 playSound**

在 `frontend/src/components/agentre/turn-complete-notifier.tsx`：
- 删第 6 行 `import { playNotifySound } from "../../lib/notify-sound";`。
- 删 `deps` 中的 `playSound: playNotifySound,` 行。

- [ ] **Step 3: 从 store 移除 sound/soundPreset**

把 `frontend/src/stores/notification-settings-store.ts` 整体替换为：

```ts
import { create } from "zustand";

import { GetAppSetting, UpdateAppSettings } from "../../wailsjs/go/app/App";
import type { app_settings_svc } from "../../wailsjs/go/models";

export type NotificationSettings = {
  enabled: boolean;
  onlyWhenUnfocused: boolean;
  system: boolean;
  toast: boolean;
};

export const DEFAULT_NOTIFICATION_SETTINGS: NotificationSettings = {
  enabled: true,
  onlyWhenUnfocused: true,
  system: true,
  toast: false,
};

const KEYS = {
  enabled: "notify.enabled",
  onlyWhenUnfocused: "notify.only_when_unfocused",
  system: "notify.system",
  toast: "notify.toast",
} as const;

// GetAppSetting 在 key 不存在时会 reject（后端 AppSettingNotFound），逐 key 兜底默认值。
async function readRaw(key: string): Promise<string | null> {
  try {
    const r = await GetAppSetting({ key });
    return r?.value ?? null;
  } catch {
    return null;
  }
}

type State = {
  settings: NotificationSettings;
  load: () => Promise<void>;
  save: (patch: Partial<NotificationSettings>) => Promise<void>;
};

export const useNotificationSettingsStore = create<State>((set, get) => ({
  settings: { ...DEFAULT_NOTIFICATION_SETTINGS },
  load: async () => {
    const [enabled, onlyWhenUnfocused, system, toast] = await Promise.all([
      readRaw(KEYS.enabled),
      readRaw(KEYS.onlyWhenUnfocused),
      readRaw(KEYS.system),
      readRaw(KEYS.toast),
    ]);
    const d = DEFAULT_NOTIFICATION_SETTINGS;
    set({
      settings: {
        enabled: enabled === null ? d.enabled : enabled === "true",
        onlyWhenUnfocused:
          onlyWhenUnfocused === null
            ? d.onlyWhenUnfocused
            : onlyWhenUnfocused === "true",
        system: system === null ? d.system : system === "true",
        toast: toast === null ? d.toast : toast === "true",
      },
    });
  },
  save: async (patch) => {
    const entries = Object.entries(patch).map(([k, v]) => ({
      key: KEYS[k as keyof NotificationSettings],
      value: String(v),
    }));
    if (entries.length === 0) return;
    await UpdateAppSettings({ entries } as app_settings_svc.UpdateRequest);
    set({ settings: { ...get().settings, ...patch } });
  },
}));
```

- [ ] **Step 4: 从 panel 移除声音 Row**

在 `frontend/src/components/agentre/notifications-panel.tsx`：
- 删第 2 行 `import { Play } from "lucide-react";`。
- 删第 6-12 行整个 `Select*` import 块。
- 删第 5 行 `import { Button } from "@/components/ui/button";`（确认本文件无其它 Button 用法后再删；若有保留）。
- 删第 15-19 行 `notify-sound` import 块。
- 删第 91-125 行整个「声音」`<Row ...>...</Row>`（含 Select + 试听 Button + Switch）。
- 第 20-23 行的 `useNotificationSettingsStore, type NotificationSettings` import 保留。

- [ ] **Step 5: 删 notify-sound.ts**

Run: `cd /Users/codfrm/Code/agentre/agentre && rm frontend/src/lib/notify-sound.ts`

- [ ] **Step 6: 删 i18n 声音文案**

在 `frontend/src/i18n/locales/zh-CN/common.json` 和 `frontend/src/i18n/locales/en/common.json` 的 `settings.notifications` 下，删除键：`soundLabel`、`soundDesc`、`soundTest`、`preset`（含其下 `ding`/`chime`/`blip`）。两个语言文件都要删，保持对称。

- [ ] **Step 7: 更新前端测试（移除 playSound / 声音 UI 引用）**

- `frontend/src/lib/__tests__/turn-notify.test.ts`：删 `deps()` 里的 `playSound: vi.fn(),`；删所有 `playSound` 断言（如「触发全部已启用渠道」用例里 `expect(d.playSound)...`、「只开系统通知」用例里的 `playSound` 断言）；含 `sound: true` 的 `getSettings` 覆盖项删掉 `sound`。
- `frontend/src/components/agentre/__tests__/turn-complete-notifier.test.tsx`：删 `playSound` mock（第 15-19 行 `notify-sound` 的 `vi.mock` + `const playSound`）与所有 `playSound` 断言；`useNotificationSettingsStore.setState` 里的 `sound: true` 删掉；App mock 里 `GetAppSetting` 对 `notify.sound`/`notify.toast` 的判断改为只认 `notify.toast`。
- `frontend/src/components/agentre/` 下若有 `notifications-panel` 测试，删除声音相关用例。
- `frontend/src/__tests__/i18n.test.ts`：若它静态校验具体 key，移除对已删声音 key 的引用（按测试报错处定位）。

- [ ] **Step 8: 跑全部相关前端测试看通过**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/lib/__tests__/turn-notify.test.ts src/components/agentre/__tests__/turn-complete-notifier.test.tsx src/__tests__/i18n.test.ts`
Expected: PASS。再跑一次 ESLint 确认无悬挂 import / 无 hardcoded 文案：`cd /Users/codfrm/Code/agentre/agentre && make lint`（或 `cd frontend && pnpm lint`）。

- [ ] **Step 9: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src internal -A
git commit -m "➖ notify(fe): 移除前端声音子系统（统一用系统通知声音）"
```

---

## Task 7：整体验证 + 手动清单

**Files:** 无（仅验证）

- [ ] **Step 1: 后端全量 race 测试**

Run: `cd /Users/codfrm/Code/agentre/agentre && make test-backend`
Expected: PASS，无 `beeep` / `SoundPreset` / `AppSettingInvalidSoundPreset` 残留编译引用。

- [ ] **Step 2: 前端全量测试**

Run: `cd /Users/codfrm/Code/agentre/agentre && make test-frontend`
Expected: PASS（含 i18n 双语齐全校验）。

- [ ] **Step 3: Lint**

Run: `cd /Users/codfrm/Code/agentre/agentre && make lint`
Expected: 通过（golangci-lint v2 + ESLint，无 hardcoded 中文 / 无未用 import）。

- [ ] **Step 4: 手动验证（打包后，适配器不可自动化）**

```bash
cd /Users/codfrm/Code/agentre/agentre && make build
open build/bin/Agentre.app
```
逐项确认：
1. 后台触发一次回合完成（让窗口失焦），出现系统通知；**横幅图标是 Agentre**（不是 Script Editor）。
2. **首次**触发时弹系统授权窗；点允许后通知正常出现。
3. **点击通知** → Agentre 窗口被拉到前台 + 自动切到/打开对应会话 tab。
4. 在系统设置里关掉 Agentre 通知 → 不再弹、app 不崩；重新打开 → 恢复。
5. 同会话连续完成多次 → 通知按 `session-<id>` 替换而非无限堆叠。

- [ ] **Step 5: 收尾**

确认 `git status` 仅含本计划相关改动；`git log --oneline` 应见 6 个聚焦提交（Task1-6 各一）。如需合并/PR，走 finishing-a-development-branch。

---

## Self-Review（计划作者已核对）

- **Spec 覆盖**：图标=以 app 身份投递（Task 2）；点击聚焦+跳转=Task 3（app 回调）+Task 4（前端 openSession）；懒授权=Task 2 `ensureAuthorized`；降级 no-op=Task 2/3；删 beeep=Task 2；删声音=Task 5/6；bundle id 不动=未触碰。✓
- **占位扫描**：无 TBD/TODO；每个改动给了完整代码或精确删除位置。✓
- **类型一致**：`Notify(ctx, title, body, sessionID int64)` 在接口/mock/调用处一致；`showSystemNotification(sessionId, title, body)` 在类型/调用/测试一致；`sessionIDFromUserInfo` 定义与使用一致；事件名 `"notification:click"` 后端 emit 与前端 EventsOn 一致。✓
