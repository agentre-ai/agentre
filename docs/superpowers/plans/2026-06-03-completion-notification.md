# 对话完成通知 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 当一轮对话结束(完成 / 出错 / 等待用户输入)且用户没在看该会话时,通过系统通知 / 提示音 / 应用内 toast 提醒用户,渠道可在「设置 → 通知」配置。

**Architecture:** 前端编排 + 后端薄绑定。前端 `TurnCompleteNotifier` 用本地信号(`session-status-store` 状态转换 + `chat-tabs-store` 当前会话 + 窗口焦点)做全部决策,系统通知调薄 Wails 绑定 `App.ShowNotification` → `notification_svc` → `pkg/sysnotify`(beeep)弹原生通知;提示音(Web Audio 合成)与应用内 toast 纯前端。应用内 toast 是 bespoke 卡片(`notification-toast-store` 列表 + `NotificationToastViewport` 渲染),**不复用 sonner**,带「跳转到会话」动作。设置沿用 `app_settings_svc` 的离散 KV key。

**Tech Stack:** Go 1.26 / cago / gomock / goconvey;`github.com/gen2brain/beeep`;React 19 / TS / zustand / Vitest / @testing-library/react / Web Audio API。

**参考 spec:** `docs/superpowers/specs/2026-06-03-completion-notification-design.md`

**约定:**
- 所有命令在 worktree 根 `/Users/codfrm/Code/agentre/agentre/.claude/worktrees/completion-notification` 下运行(Go 命令在该目录,前端命令在其 `frontend/`)。
- 后端聚焦测试:`go test -race ./internal/...`;前端:`cd frontend && pnpm test -- <file>`。
- gitmoji commit;每个 Task 末尾提交。

---

## 文件结构总览

**后端(新建)**
- `internal/service/notification_svc/notification.go` — `NotificationSvc` + `Notifier` 接口 + 单例/注入。
- `internal/service/notification_svc/types.go` — `ShowRequest`。
- `internal/service/notification_svc/notification_test.go` — svc 单测(mock Notifier)。
- `internal/service/notification_svc/mock_notification_svc/mock_notification.go` — `go generate` 产物。
- `internal/pkg/sysnotify/sysnotify.go` — beeep 实现(平台叶子)。
- `internal/pkg/sysnotify/sysnotify_test.go` — 构造测试。
- `internal/app/notification.go` — Wails 绑定 `App.ShowNotification`。

**后端(修改)**
- `internal/pkg/code/code.go`、`zh_cn.go`、`en.go` — 两个新错误码 + 文案。
- `internal/model/entity/app_setting_entity/app_setting.go` — 5 个 key 常量 + 2 个校验函数 + bool 解析助手。
- `internal/model/entity/app_setting_entity/app_setting_test.go` — 校验函数测试(若文件不存在则新建)。
- `internal/service/app_settings_svc/app_settings.go` — Update switch 增 case。
- `internal/service/app_settings_svc/app_settings_test.go` — 新 key 校验用例。
- `internal/bootstrap/cago.go` — 注入 `sysnotify.New()`。
- `go.mod` / `go.sum` — 增 beeep 直接依赖。
- `frontend/wailsjs/go/...` — `make generate` 产物(ShowNotification + notification_svc.ShowRequest)。

**前端(新建)**
- `frontend/src/lib/notify-sound.ts` — Web Audio 提示音预设。
- `frontend/src/lib/window-focus.ts` — 窗口焦点单例 getter。
- `frontend/src/stores/notification-settings-store.ts` — 通知设置 store(load/save/默认值)。
- `frontend/src/lib/turn-notify.ts` — 纯逻辑:`classifyTransition` + `maybeNotify`。
- `frontend/src/components/agentre/turn-complete-notifier.tsx` — 宿主 + `useTurnNotifications` hook。
- `frontend/src/components/agentre/notifications-panel.tsx` — 设置面板。
- `frontend/src/stores/notification-toast-store.ts` — 应用内 toast 列表 store(`push`/`dismiss`/`clear`,上限 5 条)。
- `frontend/src/components/agentre/notification-toast.tsx` — bespoke 通知卡 + 右下角常驻 `NotificationToastViewport`(不复用 sonner)。
- `frontend/src/components/agentre/session-avatar.ts` — `tokenToCssColor`/`firstLetter`/`avatarFromMeta`,与 tab 头像共用(从 `use-tabs-view.ts` 抽出去重)。
- 对应 `__tests__/*.test.ts(x)`。

**前端(修改)**
- `frontend/src/App.tsx` — 挂 `<TurnCompleteNotifier />` + `<NotificationToastViewport />`(`<Toaster/>` 保留给别处 sonner 用)。
- `frontend/src/components/agentre/settings.tsx` — notifications 分支换成真面板 + 去掉 under-construction 条目。
- `frontend/src/components/agentre/chat-tabs/use-tabs-view.ts` — avatar 推导改用共享的 `session-avatar`。
- `frontend/src/i18n/locales/zh-CN/common.json`、`en/common.json` — 新增 `settings.notifications.*` + `notify.*`(含 `notify.openSession`/`dismiss`/`justNow`),删除 `settings.underConstruction.notifications.*`。
- `frontend/src/__tests__/i18n.test.ts` — 更新 `shellAndSettingsKeys` 列表。

---

## Task 1: 后端 — 通知校验错误码

**Files:**
- Modify: `internal/pkg/code/code.go`(App 设置段 15000~15999 末尾)
- Modify: `internal/pkg/code/zh_cn.go`
- Modify: `internal/pkg/code/en.go`

- [ ] **Step 1: 加错误码常量**

在 `internal/pkg/code/code.go` 的 App 设置段把末尾两个常量补上(追加在 `AppGatewayRestartFailed` 之后,保持 iota 递增):

```go
// App 设置 15000~15999
const (
	AppSettingNotFound      = iota + 15000 // 设置项不存在
	AppSettingInvalidPort                  // 端口越界或非数字
	AppSettingInvalidHost                  // 监听地址非合法 IP
	AppGatewayRestartFailed                // 应用并重启时绑定端口失败
	AppSettingInvalidBool                  // 布尔设置项取值非法
	AppSettingInvalidSoundPreset           // 提示音预设非法
)
```

- [ ] **Step 2: 加中文文案**

在 `internal/pkg/code/zh_cn.go` 的对应 map 里,`AppGatewayRestartFailed` 那条之后追加:

```go
	AppSettingInvalidBool:        "开关取值必须是 true 或 false",
	AppSettingInvalidSoundPreset: "提示音预设不在可选范围内",
```

- [ ] **Step 3: 加英文文案**

在 `internal/pkg/code/en.go` 的对应 map 里追加(键名与 zh_cn.go 对齐):

```go
	AppSettingInvalidBool:        "Toggle value must be true or false",
	AppSettingInvalidSoundPreset: "Unknown notification sound preset",
```

- [ ] **Step 4: 编译确认**

Run: `go build ./internal/pkg/code/...`
Expected: 无输出(成功)。

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/code
git commit -m "✨ notify: 通知设置校验错误码"
```

---

## Task 2: 后端 — app_setting_entity 新 key + 校验函数

**Files:**
- Modify: `internal/model/entity/app_setting_entity/app_setting.go`
- Test: `internal/model/entity/app_setting_entity/app_setting_test.go`(若不存在则新建)

- [ ] **Step 1: 写失败测试**

新建/追加 `internal/model/entity/app_setting_entity/app_setting_test.go`:

```go
package app_setting_entity

import (
	"context"
	"testing"
)

func TestValidateBoolSetting(t *testing.T) {
	ctx := context.Background()
	for _, ok := range []string{"true", "false", " true "} {
		if err := ValidateBoolSetting(ctx, ok); err != nil {
			t.Fatalf("ValidateBoolSetting(%q) 应通过, got %v", ok, err)
		}
	}
	for _, bad := range []string{"", "1", "yes", "True", "maybe"} {
		if err := ValidateBoolSetting(ctx, bad); err == nil {
			t.Fatalf("ValidateBoolSetting(%q) 应报错", bad)
		}
	}
}

func TestValidateSoundPreset(t *testing.T) {
	ctx := context.Background()
	for _, ok := range []string{"ding", "chime", "blip", " ding "} {
		if err := ValidateSoundPreset(ctx, ok); err != nil {
			t.Fatalf("ValidateSoundPreset(%q) 应通过, got %v", ok, err)
		}
	}
	for _, bad := range []string{"", "bell", "DING"} {
		if err := ValidateSoundPreset(ctx, bad); err == nil {
			t.Fatalf("ValidateSoundPreset(%q) 应报错", bad)
		}
	}
}

func TestParseBoolSetting(t *testing.T) {
	if !ParseBoolSetting("true") {
		t.Fatal(`ParseBoolSetting("true") 应为 true`)
	}
	for _, f := range []string{"false", "", "1", "x"} {
		if ParseBoolSetting(f) {
			t.Fatalf("ParseBoolSetting(%q) 应为 false", f)
		}
	}
}
```

- [ ] **Step 2: 运行,确认失败**

Run: `go test ./internal/model/entity/app_setting_entity/ -run 'ValidateBoolSetting|ValidateSoundPreset|ParseBoolSetting'`
Expected: FAIL,报 `undefined: ValidateBoolSetting` 等。

- [ ] **Step 3: 加 key 常量 + 校验函数**

在 `internal/model/entity/app_setting_entity/app_setting.go` 的 key 常量块(`KeyDebugLogging` 之后)追加:

```go
	// 通知设置。bool 型存 "true"/"false";sound_preset 为枚举。
	KeyNotifyEnabled           = "notify.enabled"             // 通知总开关
	KeyNotifyOnlyWhenUnfocused = "notify.only_when_unfocused" // 仅窗口未激活时通知
	KeyNotifySystem            = "notify.system"              // 系统原生通知
	KeyNotifySound             = "notify.sound"               // 提示音
	KeyNotifySoundPreset       = "notify.sound_preset"        // 提示音预设
	KeyNotifyToast             = "notify.toast"               // 应用内 toast
```

在文件末尾(`ValidateUpdateChannel` 之后)追加:

```go
// ValidateBoolSetting 校验布尔型设置项取值，仅接受 "true"/"false"（service 层调用前已 TrimSpace）。
func ValidateBoolSetting(ctx context.Context, v string) error {
	switch strings.TrimSpace(v) {
	case "true", "false":
		return nil
	}
	return i18n.NewError(ctx, code.AppSettingInvalidBool)
}

// ValidateSoundPreset 校验提示音预设取值。
func ValidateSoundPreset(ctx context.Context, v string) error {
	switch strings.TrimSpace(v) {
	case "ding", "chime", "blip":
		return nil
	}
	return i18n.NewError(ctx, code.AppSettingInvalidSoundPreset)
}

// ParseBoolSetting 解析布尔型设置项；仅 "true" 视为开启，其余(含空)关闭。
func ParseBoolSetting(v string) bool {
	return strings.TrimSpace(v) == "true"
}
```

- [ ] **Step 4: 运行,确认通过**

Run: `go test ./internal/model/entity/app_setting_entity/ -run 'ValidateBoolSetting|ValidateSoundPreset|ParseBoolSetting'`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/model/entity/app_setting_entity
git commit -m "✨ notify: app_setting 通知 key 与校验函数"
```

---

## Task 3: 后端 — app_settings_svc Update 接受通知 key

**Files:**
- Modify: `internal/service/app_settings_svc/app_settings.go:65-85`(Update 校验 switch)
- Test: `internal/service/app_settings_svc/app_settings_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/service/app_settings_svc/app_settings_test.go` 追加(复用文件里已有的 `setupSvcTest` 助手):

```go
func TestUpdate_NotifyKeys(t *testing.T) {
	convey.Convey("Update 通知设置 key", t, func() {
		ctx, repo, gw, svc := setupSvcTest(t)

		convey.Convey("合法 bool 写入,不触发 gateway", func() {
			repo.EXPECT().Set(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			_, err := svc.Update(ctx, &UpdateRequest{Entries: []SettingEntry{
				{Key: app_setting_entity.KeyNotifySystem, Value: "true"},
			}})
			assert.NoError(t, err)
			assert.Equal(t, int32(0), gw.restartCalls.Load(), "通知 key 不应触发 Restart")
		})

		convey.Convey("非法 bool 直接拒,不落库", func() {
			_, err := svc.Update(ctx, &UpdateRequest{Entries: []SettingEntry{
				{Key: app_setting_entity.KeyNotifyEnabled, Value: "maybe"},
			}})
			assert.Error(t, err)
		})

		convey.Convey("合法 preset 写入", func() {
			repo.EXPECT().Set(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			_, err := svc.Update(ctx, &UpdateRequest{Entries: []SettingEntry{
				{Key: app_setting_entity.KeyNotifySoundPreset, Value: "chime"},
			}})
			assert.NoError(t, err)
		})

		convey.Convey("非法 preset 直接拒", func() {
			_, err := svc.Update(ctx, &UpdateRequest{Entries: []SettingEntry{
				{Key: app_setting_entity.KeyNotifySoundPreset, Value: "bell"},
			}})
			assert.Error(t, err)
		})
	})
}
```

> 若 `app_settings_svc` 测试文件没 import `app_setting_entity`,补上 import。

- [ ] **Step 2: 运行,确认失败**

Run: `go test ./internal/service/app_settings_svc/ -run TestUpdate_NotifyKeys`
Expected: FAIL —— "合法 bool 写入" 会因为命中 `default` 分支返回 `AppSettingNotFound` 而 `repo.Set` 未被调用(gomock 报缺失调用)。

- [ ] **Step 3: 在 Update switch 加 case**

在 `internal/service/app_settings_svc/app_settings.go` 的 Update 校验循环里,`case "":` 之前插入:

```go
		case app_setting_entity.KeyNotifyEnabled,
			app_setting_entity.KeyNotifyOnlyWhenUnfocused,
			app_setting_entity.KeyNotifySystem,
			app_setting_entity.KeyNotifySound,
			app_setting_entity.KeyNotifyToast:
			if err := app_setting_entity.ValidateBoolSetting(ctx, val); err != nil {
				return nil, err
			}
		case app_setting_entity.KeyNotifySoundPreset:
			if err := app_setting_entity.ValidateSoundPreset(ctx, val); err != nil {
				return nil, err
			}
```

- [ ] **Step 4: 运行,确认通过**

Run: `go test ./internal/service/app_settings_svc/ -run TestUpdate_NotifyKeys`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/service/app_settings_svc
git commit -m "✨ notify: app_settings 接受通知设置 key"
```

---

## Task 4: 后端 — notification_svc(接口 + mock + 单测)

**Files:**
- Create: `internal/service/notification_svc/notification.go`
- Create: `internal/service/notification_svc/types.go`
- Create: `internal/service/notification_svc/notification_test.go`
- Generate: `internal/service/notification_svc/mock_notification_svc/mock_notification.go`

- [ ] **Step 1: 写接口 + 实现 + types**

`internal/service/notification_svc/types.go`:

```go
// Package notification_svc 暴露应用级系统通知能力。
// 决策(要不要弹、文案 i18n)在前端;本服务只负责把一条已成型的通知交给平台原生实现。
package notification_svc

// ShowRequest 展示一条系统通知。Title/Body 已由前端按 i18n 生成。
type ShowRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}
```

`internal/service/notification_svc/notification.go`:

```go
package notification_svc

import (
	"context"
	"strings"
)

//go:generate mockgen -source notification.go -destination mock_notification_svc/mock_notification.go

// Notifier 平台原生通知能力,由 internal/pkg/sysnotify 提供实现,bootstrap 注入。
type Notifier interface {
	Notify(title, body string) error
}

// NotificationSvc 应用通知服务。
type NotificationSvc interface {
	Show(ctx context.Context, req *ShowRequest) error
}

type notificationSvc struct {
	notifier Notifier
}

var defaultSvc = &notificationSvc{}

// Notification 取默认服务单例。
func Notification() NotificationSvc { return defaultSvc }

// RegisterNotifier 由 bootstrap 注入平台通知实现。
func RegisterNotifier(n Notifier) { defaultSvc.notifier = n }

// Show 弹一条系统通知;未注入实现或 req 为空时安全 no-op。空 title 兜底为 "Agentre"。
func (s *notificationSvc) Show(_ context.Context, req *ShowRequest) error {
	if s.notifier == nil || req == nil {
		return nil
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Agentre"
	}
	return s.notifier.Notify(title, req.Body)
}
```

- [ ] **Step 2: 生成 mock**

Run: `cd internal/service/notification_svc && go generate ./... && cd -`
Expected: 生成 `mock_notification_svc/mock_notification.go`(含 `MockNotifier`)。
> 若本地未装 mockgen:`go install go.uber.org/mock/mockgen@latest`。

- [ ] **Step 3: 写测试**

`internal/service/notification_svc/notification_test.go`:

```go
package notification_svc

import (
	"context"
	"errors"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"agentre/internal/service/notification_svc/mock_notification_svc"
)

func TestShow(t *testing.T) {
	convey.Convey("Show", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		n := mock_notification_svc.NewMockNotifier(ctrl)
		RegisterNotifier(n)

		convey.Convey("透传 title/body", func() {
			n.EXPECT().Notify("fix bug", "已完成").Return(nil)
			assert.NoError(t, Notification().Show(context.Background(), &ShowRequest{Title: "fix bug", Body: "已完成"}))
		})

		convey.Convey("空 title 兜底 Agentre", func() {
			n.EXPECT().Notify("Agentre", "x").Return(nil)
			assert.NoError(t, Notification().Show(context.Background(), &ShowRequest{Title: "  ", Body: "x"}))
		})

		convey.Convey("Notifier 报错向上传播", func() {
			n.EXPECT().Notify(gomock.Any(), gomock.Any()).Return(errors.New("boom"))
			assert.Error(t, Notification().Show(context.Background(), &ShowRequest{Title: "t", Body: "b"}))
		})
	})

	convey.Convey("未注入 notifier 时 no-op", t, func() {
		defaultSvc.notifier = nil
		assert.NoError(t, Notification().Show(context.Background(), &ShowRequest{Title: "t"}))
	})
}
```

- [ ] **Step 4: 运行,确认通过**

Run: `go test -race ./internal/service/notification_svc/...`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/service/notification_svc
git commit -m "✨ notify: notification_svc 服务与 Notifier 接口"
```

---

## Task 5: 后端 — pkg/sysnotify(beeep 实现)

**Files:**
- Create: `internal/pkg/sysnotify/sysnotify.go`
- Create: `internal/pkg/sysnotify/sysnotify_test.go`
- Modify: `go.mod` / `go.sum`

- [ ] **Step 1: 加 beeep 依赖**

Run: `go get github.com/gen2brain/beeep`
然后确认 Notify 签名:
Run: `go doc github.com/gen2brain/beeep.Notify`
Expected: `func Notify(title, message, appIcon string) error`。
> 若该版本签名不同,仅需在下一步的 wrapper 里调整这一行调用(beeep 只在此一处被引用)。

- [ ] **Step 2: 写实现**

`internal/pkg/sysnotify/sysnotify.go`:

```go
// Package sysnotify 基于 beeep 的跨平台系统通知实现(平台叶子,不反向依赖 service 层)。
package sysnotify

import "github.com/gen2brain/beeep"

// Notifier 满足 notification_svc.Notifier(结构化接口)。
type Notifier struct{}

// New 构造一个系统通知器。
func New() *Notifier { return &Notifier{} }

// Notify 弹一条静音系统通知;提示音由前端独立控制,这里不带声音以免重复响。
func (*Notifier) Notify(title, body string) error {
	return beeep.Notify(title, body, "")
}
```

- [ ] **Step 3: 写测试**

`internal/pkg/sysnotify/sysnotify_test.go`:

```go
package sysnotify

import "testing"

// 不在 CI 真弹通知(会触达 OS);只验证构造与接口形状。
func TestNew(t *testing.T) {
	n := New()
	if n == nil {
		t.Fatal("New() 返回 nil")
	}
	// 编译期保证 *Notifier 满足 Notify(title, body string) error 形状。
	var _ interface{ Notify(string, string) error } = n
}
```

- [ ] **Step 4: 运行,确认通过 + tidy**

Run: `go test ./internal/pkg/sysnotify/... && go mod tidy`
Expected: PASS;`go.mod` 里 beeep 变成直接依赖(去掉 `// indirect`),`go-toast/v2` 仍在。

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/sysnotify go.mod go.sum
git commit -m "✨ notify: sysnotify 基于 beeep 的系统通知实现"
```

---

## Task 6: 后端 — Wails 绑定 + bootstrap 接线 + 生成绑定

**Files:**
- Create: `internal/app/notification.go`
- Modify: `internal/bootstrap/cago.go:127-129`(wiring 区)
- Generate: `frontend/wailsjs/go/app/App.*`、`frontend/wailsjs/go/models.ts`

- [ ] **Step 1: 写 Wails 绑定**

`internal/app/notification.go`:

```go
package app

import "agentre/internal/service/notification_svc"

// ShowNotification 弹一条系统通知;文案已由前端按 i18n 生成。
func (a *App) ShowNotification(req *notification_svc.ShowRequest) error {
	return notification_svc.Notification().Show(a.ctx, req)
}
```

- [ ] **Step 2: bootstrap 注入 beeep 实现**

在 `internal/bootstrap/cago.go` 的 wiring 区(`chat_svc.RegisterGateway(gw)` 一带,line ~129 之后)追加:

```go
	notification_svc.RegisterNotifier(sysnotify.New())
```

并补 import:

```go
	"agentre/internal/pkg/sysnotify"
	"agentre/internal/service/notification_svc"
```

- [ ] **Step 3: 编译后端**

Run: `go build ./...`
Expected: 成功。

- [ ] **Step 4: 生成前端绑定**

Run: `make generate`
Expected: `frontend/wailsjs/go/app/App.d.ts` 出现 `export function ShowNotification(arg1:notification_svc.ShowRequest):Promise<void>;`,`models.ts` 出现 `notification_svc.ShowRequest`。

- [ ] **Step 5: Commit**

```bash
git add internal/app/notification.go internal/bootstrap/cago.go frontend/wailsjs
git commit -m "✨ notify: App.ShowNotification 绑定与 bootstrap 接线"
```

---

## Task 7: 前端 — 提示音预设(Web Audio)

**Files:**
- Create: `frontend/src/lib/notify-sound.ts`
- Test: `frontend/src/lib/__tests__/notify-sound.test.ts`

- [ ] **Step 1: 写失败测试**

`frontend/src/lib/__tests__/notify-sound.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { SOUND_PRESETS, playNotifySound } from "../notify-sound";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("notify-sound", () => {
  it("暴露三个预设", () => {
    expect(SOUND_PRESETS).toEqual(["ding", "chime", "blip"]);
  });

  it("无 AudioContext 时安全 no-op,不抛错", () => {
    vi.stubGlobal("AudioContext", undefined);
    vi.stubGlobal("webkitAudioContext", undefined);
    expect(() => playNotifySound("ding")).not.toThrow();
  });

  it("有 AudioContext 时为 chime 调度多个振荡器", () => {
    const starts: number[] = [];
    const fakeOsc = () => ({
      type: "",
      frequency: { value: 0 },
      connect: vi.fn(),
      start: (t: number) => starts.push(t),
      stop: vi.fn(),
    });
    const fakeGain = () => ({
      gain: {
        setValueAtTime: vi.fn(),
        linearRampToValueAtTime: vi.fn(),
        exponentialRampToValueAtTime: vi.fn(),
      },
      connect: vi.fn(),
    });
    const FakeCtx = vi.fn(() => ({
      currentTime: 0,
      destination: {},
      createOscillator: vi.fn(fakeOsc),
      createGain: vi.fn(fakeGain),
    }));
    vi.stubGlobal("AudioContext", FakeCtx);
    playNotifySound("chime");
    expect(starts.length).toBeGreaterThanOrEqual(3); // chime = 三音琶音
  });
});
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd frontend && pnpm test -- src/lib/__tests__/notify-sound.test.ts`
Expected: FAIL —— 模块不存在。

- [ ] **Step 3: 写实现**

`frontend/src/lib/notify-sound.ts`:

```ts
export type SoundPreset = "ding" | "chime" | "blip";

export const SOUND_PRESETS: SoundPreset[] = ["ding", "chime", "blip"];

let ctx: AudioContext | null = null;

function audioContext(): AudioContext | null {
  try {
    const Ctor =
      (globalThis as { AudioContext?: typeof AudioContext }).AudioContext ??
      (globalThis as { webkitAudioContext?: typeof AudioContext })
        .webkitAudioContext;
    if (!Ctor) return null;
    ctx = ctx ?? new Ctor();
    return ctx;
  } catch {
    return null;
  }
}

function tone(
  c: AudioContext,
  freq: number,
  start: number,
  dur: number,
  peak: number,
): void {
  const osc = c.createOscillator();
  const gain = c.createGain();
  osc.type = "sine";
  osc.frequency.value = freq;
  gain.gain.setValueAtTime(0, start);
  gain.gain.linearRampToValueAtTime(peak, start + 0.01);
  gain.gain.exponentialRampToValueAtTime(0.0001, start + dur);
  osc.connect(gain);
  gain.connect(c.destination);
  osc.start(start);
  osc.stop(start + dur + 0.02);
}

// 各预设的音符表 [频率Hz, 相对起始秒, 时长秒]。
const RECIPES: Record<SoundPreset, [number, number, number][]> = {
  ding: [
    [880, 0, 0.45],
    [1320, 0.04, 0.45],
  ],
  chime: [
    [659, 0, 0.4],
    [880, 0.09, 0.4],
    [1175, 0.18, 0.5],
  ],
  blip: [[440, 0, 0.16]],
};

export function playNotifySound(preset: SoundPreset): void {
  const c = audioContext();
  if (!c) return;
  const t0 = c.currentTime;
  for (const [freq, offset, dur] of RECIPES[preset] ?? RECIPES.ding) {
    tone(c, freq, t0 + offset, dur, 0.18);
  }
}
```

- [ ] **Step 4: 运行,确认通过**

Run: `cd frontend && pnpm test -- src/lib/__tests__/notify-sound.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/notify-sound.ts frontend/src/lib/__tests__/notify-sound.test.ts
git commit -m "✨ notify(fe): Web Audio 提示音预设"
```

---

## Task 8: 前端 — 窗口焦点单例

**Files:**
- Create: `frontend/src/lib/window-focus.ts`
- Test: `frontend/src/lib/__tests__/window-focus.test.ts`

- [ ] **Step 1: 写失败测试**

`frontend/src/lib/__tests__/window-focus.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { isWindowFocused } from "../window-focus";

describe("window-focus", () => {
  it("blur 后失焦, focus 后恢复", () => {
    window.dispatchEvent(new Event("focus"));
    expect(isWindowFocused()).toBe(true);
    window.dispatchEvent(new Event("blur"));
    expect(isWindowFocused()).toBe(false);
    window.dispatchEvent(new Event("focus"));
    expect(isWindowFocused()).toBe(true);
  });
});
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd frontend && pnpm test -- src/lib/__tests__/window-focus.test.ts`
Expected: FAIL —— 模块不存在。

- [ ] **Step 3: 写实现**

`frontend/src/lib/window-focus.ts`:

```ts
// 跟踪应用窗口是否处于前台/聚焦。事件驱动(不依赖 document.hasFocus(),便于测试)。
let focused = true;

function set(v: boolean): void {
  focused = v;
}

if (typeof window !== "undefined") {
  window.addEventListener("focus", () => set(true));
  window.addEventListener("blur", () => set(false));
  if (typeof document !== "undefined") {
    document.addEventListener("visibilitychange", () => {
      set(document.visibilityState !== "hidden");
    });
  }
}

// isWindowFocused 当前窗口是否聚焦且可见。
export function isWindowFocused(): boolean {
  return focused;
}
```

- [ ] **Step 4: 运行,确认通过**

Run: `cd frontend && pnpm test -- src/lib/__tests__/window-focus.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/window-focus.ts frontend/src/lib/__tests__/window-focus.test.ts
git commit -m "✨ notify(fe): 窗口焦点单例"
```

---

## Task 9: 前端 — 通知设置 store

**Files:**
- Create: `frontend/src/stores/notification-settings-store.ts`
- Test: `frontend/src/stores/__tests__/notification-settings-store.test.ts`

- [ ] **Step 1: 写失败测试**

`frontend/src/stores/__tests__/notification-settings-store.test.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../wailsjs/go/app/App", () => ({
  GetAppSetting: vi.fn(),
  UpdateAppSettings: vi.fn(),
}));

import { GetAppSetting, UpdateAppSettings } from "../../../wailsjs/go/app/App";
import {
  DEFAULT_NOTIFICATION_SETTINGS,
  useNotificationSettingsStore,
} from "../notification-settings-store";

const getMock = vi.mocked(GetAppSetting);
const updateMock = vi.mocked(UpdateAppSettings);

beforeEach(() => {
  vi.clearAllMocks();
  useNotificationSettingsStore.setState({
    settings: { ...DEFAULT_NOTIFICATION_SETTINGS },
  });
});

describe("notification-settings-store", () => {
  it("默认值: 仅系统通知开 + 仅失焦时通知,提示音/toast 关", () => {
    expect(DEFAULT_NOTIFICATION_SETTINGS).toEqual({
      enabled: true,
      onlyWhenUnfocused: true,
      system: true,
      sound: false,
      soundPreset: "ding",
      toast: false,
    });
  });

  it("load: 某 key 缺失(reject)时回落默认值", async () => {
    getMock.mockImplementation((req: { key: string }) => {
      if (req.key === "notify.sound") return Promise.resolve({ key: req.key, value: "true" });
      if (req.key === "notify.toast") return Promise.reject(new Error("not found"));
      return Promise.resolve({ key: req.key, value: "false" });
    });
    await useNotificationSettingsStore.getState().load();
    const s = useNotificationSettingsStore.getState().settings;
    expect(s.sound).toBe(true); // 读到 "true"
    expect(s.toast).toBe(false); // reject → 默认 false
    expect(s.enabled).toBe(false); // 读到 "false"
  });

  it("save: 写一个 partial 后 UpdateAppSettings 收到对应 entry,并更新本地 state", async () => {
    updateMock.mockResolvedValue({});
    await useNotificationSettingsStore.getState().save({ toast: true });
    expect(updateMock).toHaveBeenCalledWith({
      entries: [{ key: "notify.toast", value: "true" }],
    });
    expect(useNotificationSettingsStore.getState().settings.toast).toBe(true);
  });
});
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd frontend && pnpm test -- src/stores/__tests__/notification-settings-store.test.ts`
Expected: FAIL —— 模块不存在。

- [ ] **Step 3: 写实现**

`frontend/src/stores/notification-settings-store.ts`:

```ts
import { create } from "zustand";

import { GetAppSetting, UpdateAppSettings } from "../../wailsjs/go/app/App";
import type { SoundPreset } from "../lib/notify-sound";

export type NotificationSettings = {
  enabled: boolean;
  onlyWhenUnfocused: boolean;
  system: boolean;
  sound: boolean;
  soundPreset: SoundPreset;
  toast: boolean;
};

export const DEFAULT_NOTIFICATION_SETTINGS: NotificationSettings = {
  enabled: true,
  onlyWhenUnfocused: true,
  system: true,
  sound: false,
  soundPreset: "ding",
  toast: false,
};

const KEYS = {
  enabled: "notify.enabled",
  onlyWhenUnfocused: "notify.only_when_unfocused",
  system: "notify.system",
  sound: "notify.sound",
  soundPreset: "notify.sound_preset",
  toast: "notify.toast",
} as const;

// GetAppSetting 在 key 不存在时会 reject(后端 AppSettingNotFound),逐 key 兜底默认值。
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
    const [enabled, onlyWhenUnfocused, system, sound, preset, toast] =
      await Promise.all([
        readRaw(KEYS.enabled),
        readRaw(KEYS.onlyWhenUnfocused),
        readRaw(KEYS.system),
        readRaw(KEYS.sound),
        readRaw(KEYS.soundPreset),
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
        sound: sound === null ? d.sound : sound === "true",
        soundPreset: (preset as SoundPreset) || d.soundPreset,
        toast: toast === null ? d.toast : toast === "true",
      },
    });
  },
  save: async (patch) => {
    const entries = Object.entries(patch).map(([k, v]) => {
      const key = KEYS[k as keyof NotificationSettings];
      const value = typeof v === "boolean" ? String(v) : String(v);
      return { key, value };
    });
    if (entries.length === 0) return;
    await UpdateAppSettings({ entries });
    set({ settings: { ...get().settings, ...patch } });
  },
}));
```

- [ ] **Step 4: 运行,确认通过**

Run: `cd frontend && pnpm test -- src/stores/__tests__/notification-settings-store.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/stores/notification-settings-store.ts frontend/src/stores/__tests__/notification-settings-store.test.ts
git commit -m "✨ notify(fe): 通知设置 store(load/save/默认值)"
```

---

## Task 10: 前端 — 通知决策纯逻辑

**Files:**
- Create: `frontend/src/lib/turn-notify.ts`
- Test: `frontend/src/lib/__tests__/turn-notify.test.ts`

- [ ] **Step 1: 写失败测试**

`frontend/src/lib/__tests__/turn-notify.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { DEFAULT_NOTIFICATION_SETTINGS } from "../../stores/notification-settings-store";
import { classifyTransition, maybeNotify, type NotifyDeps } from "../turn-notify";

describe("classifyTransition", () => {
  it("running→idle = done", () => {
    expect(classifyTransition("running", "idle", "done")).toBe("done");
  });
  it("running→idle 但 aborted = null(用户自己停的)", () => {
    expect(classifyTransition("running", "idle", "aborted")).toBeNull();
  });
  it("running→error = error", () => {
    expect(classifyTransition("running", "error", "error")).toBe("error");
  });
  it("running→waiting = waiting", () => {
    expect(classifyTransition("running", "waiting", undefined)).toBe("waiting");
  });
  it("非 running 起点不触发", () => {
    expect(classifyTransition("idle", "running", undefined)).toBeNull();
    expect(classifyTransition(undefined, "running", undefined)).toBeNull();
  });
});

function deps(over: Partial<NotifyDeps> = {}): NotifyDeps {
  return {
    isWindowFocused: () => false,
    getActiveSessionId: () => 0,
    getSettings: () => ({ ...DEFAULT_NOTIFICATION_SETTINGS, sound: true, toast: true }),
    getSessionTitle: () => "我的会话",
    showSystemNotification: vi.fn(),
    playSound: vi.fn(),
    showToast: vi.fn(),
    t: (k: string) => k,
    ...over,
  };
}

describe("maybeNotify", () => {
  it("默认(仅失焦)+ 失焦 → 触发全部已启用渠道", () => {
    const d = deps();
    maybeNotify(42, "done", d);
    expect(d.showSystemNotification).toHaveBeenCalledWith("我的会话", "notify.body.done");
    expect(d.playSound).toHaveBeenCalledWith("ding");
    expect(d.showToast).toHaveBeenCalledWith("done", "我的会话", "notify.body.done");
  });

  it("默认(仅失焦)+ 聚焦(任意会话)→ 全部静默", () => {
    const d = deps({ isWindowFocused: () => true, getActiveSessionId: () => 7 });
    maybeNotify(42, "done", d);
    expect(d.showSystemNotification).not.toHaveBeenCalled();
    expect(d.playSound).not.toHaveBeenCalled();
    expect(d.showToast).not.toHaveBeenCalled();
  });

  it("关掉 onlyWhenUnfocused + 聚焦 + 非当前会话 → 触发", () => {
    const d = deps({
      isWindowFocused: () => true,
      getActiveSessionId: () => 7,
      getSettings: () => ({ ...DEFAULT_NOTIFICATION_SETTINGS, onlyWhenUnfocused: false, sound: true, toast: true }),
    });
    maybeNotify(42, "done", d);
    expect(d.showSystemNotification).toHaveBeenCalled();
  });

  it("关掉 onlyWhenUnfocused + 聚焦 + 当前会话 → 静默", () => {
    const d = deps({
      isWindowFocused: () => true,
      getActiveSessionId: () => 42,
      getSettings: () => ({ ...DEFAULT_NOTIFICATION_SETTINGS, onlyWhenUnfocused: false, sound: true, toast: true }),
    });
    maybeNotify(42, "done", d);
    expect(d.showSystemNotification).not.toHaveBeenCalled();
  });

  it("总开关关 → 不触发", () => {
    const d = deps({ getSettings: () => ({ ...DEFAULT_NOTIFICATION_SETTINGS, enabled: false }) });
    maybeNotify(42, "done", d);
    expect(d.showSystemNotification).not.toHaveBeenCalled();
  });

  it("无 session 标题时回落 notify.app", () => {
    const d = deps({ getSessionTitle: () => undefined });
    maybeNotify(42, "error", d);
    expect(d.showSystemNotification).toHaveBeenCalledWith("notify.app", "notify.body.error");
  });

  it("只开系统通知时不响铃不弹 toast", () => {
    const d = deps({
      getSettings: () => ({ ...DEFAULT_NOTIFICATION_SETTINGS, sound: false, toast: false, system: true }),
    });
    maybeNotify(42, "done", d);
    expect(d.showSystemNotification).toHaveBeenCalled();
    expect(d.playSound).not.toHaveBeenCalled();
    expect(d.showToast).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd frontend && pnpm test -- src/lib/__tests__/turn-notify.test.ts`
Expected: FAIL —— 模块不存在。

- [ ] **Step 3: 写实现**

`frontend/src/lib/turn-notify.ts`:

```ts
import type { TFunction } from "i18next";

import type { AgentStatus } from "../stores/types";
import type { DoneEvent } from "../stores/session-status-store";
import type { NotificationSettings } from "../stores/notification-settings-store";
import type { SoundPreset } from "./notify-sound";

export type NotifyKind = "done" | "error" | "waiting";

export type NotifyDeps = {
  isWindowFocused: () => boolean;
  getActiveSessionId: () => number | null;
  getSettings: () => NotificationSettings;
  getSessionTitle: (sessionId: number) => string | undefined;
  showSystemNotification: (title: string, body: string) => void;
  playSound: (preset: SoundPreset) => void;
  showToast: (kind: NotifyKind, title: string, body: string) => void;
  t: TFunction;
};

// classifyTransition 把一次 agentStatus 转换归类为通知类型;仅在「离开 running」时触发。
// 用户自己点停止(lastDoneEventKind==="aborted")不通知。
export function classifyTransition(
  prev: AgentStatus | undefined,
  next: AgentStatus,
  lastDoneEventKind: DoneEvent["kind"] | undefined,
): NotifyKind | null {
  if (prev !== "running") return null;
  if (next === "error") return "error";
  if (next === "waiting") return "waiting";
  if (next === "idle") return lastDoneEventKind === "aborted" ? null : "done";
  return null;
}

// maybeNotify 在门槛通过时,按设置触发各启用渠道。
// onlyWhenUnfocused 开(默认):仅窗口失焦时通知;关:失焦 或 非当前会话 时通知。
export function maybeNotify(
  sessionId: number,
  kind: NotifyKind,
  deps: NotifyDeps,
): void {
  const s = deps.getSettings();
  if (!s.enabled) return;
  const focused = deps.isWindowFocused();
  const suppress = s.onlyWhenUnfocused
    ? focused
    : focused && deps.getActiveSessionId() === sessionId;
  if (suppress) return;

  const title = deps.getSessionTitle(sessionId) ?? deps.t("notify.app");
  const body = deps.t(`notify.body.${kind}`);
  if (s.system) deps.showSystemNotification(title, body);
  if (s.sound) deps.playSound(s.soundPreset);
  if (s.toast) deps.showToast(kind, title, body);
}
```

> 注:`AgentStatus` 从 `src/stores/types.ts` import;其取值含 `"idle" | "running" | "waiting" | "error"`(见该文件)。

- [ ] **Step 4: 运行,确认通过**

Run: `cd frontend && pnpm test -- src/lib/__tests__/turn-notify.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/turn-notify.ts frontend/src/lib/__tests__/turn-notify.test.ts
git commit -m "✨ notify(fe): 通知决策纯逻辑(classify + maybeNotify)"
```

---

## Task 11: 前端 — 订阅 hook + 宿主组件 + 挂载 + notify.* 文案

**Files:**
- Create: `frontend/src/components/agentre/turn-complete-notifier.tsx`
- Test: `frontend/src/components/agentre/__tests__/turn-complete-notifier.test.tsx`
- Modify: `frontend/src/App.tsx`(挂 `<TurnCompleteNotifier />`)
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`、`en/common.json`(加 `notify.*`)

- [ ] **Step 1: 加 notify.* 文案(两端同时)**

在 `frontend/src/i18n/locales/zh-CN/common.json` 顶层加:

```json
  "notify": {
    "app": "Agentre",
    "body": {
      "done": "会话已完成",
      "error": "会话出错",
      "waiting": "需要你的操作"
    }
  },
```

在 `frontend/src/i18n/locales/en/common.json` 顶层加(键完全一致):

```json
  "notify": {
    "app": "Agentre",
    "body": {
      "done": "Session completed",
      "error": "Session failed",
      "waiting": "Waiting for your input"
    }
  },
```

- [ ] **Step 2: 写失败测试**

`frontend/src/components/agentre/__tests__/turn-complete-notifier.test.tsx`:

```tsx
import { render } from "@testing-library/react";
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const showNotification = vi.fn(() => Promise.resolve());
vi.mock("../../../../wailsjs/go/app/App", () => ({
  ShowNotification: (req: unknown) => showNotification(req),
  GetAppSetting: vi.fn(() => Promise.reject(new Error("nf"))),
  UpdateAppSettings: vi.fn(() => Promise.resolve({})),
}));
const playSound = vi.fn();
vi.mock("../../../lib/notify-sound", () => ({
  playNotifySound: (p: unknown) => playSound(p),
  SOUND_PRESETS: ["ding", "chime", "blip"],
}));
const toastSuccess = vi.fn();
vi.mock("sonner", () => ({
  toast: { success: (...a: unknown[]) => toastSuccess(...a), error: vi.fn(), warning: vi.fn() },
}));
let focused = false;
vi.mock("../../../lib/window-focus", () => ({ isWindowFocused: () => focused }));

import { useSessionStatusStore } from "../../../stores/session-status-store";
import { useChatTabsStore } from "../../../stores/chat-tabs-store";
import { useNotificationSettingsStore, DEFAULT_NOTIFICATION_SETTINGS } from "../../../stores/notification-settings-store";
import { TurnCompleteNotifier } from "../turn-complete-notifier";

beforeEach(() => {
  vi.clearAllMocks();
  focused = false;
  useSessionStatusStore.getState().__reset();
  useChatTabsStore.setState({ tabs: [], activeTabId: null });
  useNotificationSettingsStore.setState({
    settings: { ...DEFAULT_NOTIFICATION_SETTINGS, sound: true, toast: true },
  });
});
afterEach(() => vi.restoreAllMocks());

describe("TurnCompleteNotifier", () => {
  it("非当前会话 running→idle 触发系统通知+声音+toast", () => {
    render(<TurnCompleteNotifier />);
    act(() => {
      useSessionStatusStore.getState().upsert(42, { agentStatus: "running", needsAttention: false });
    });
    act(() => {
      useSessionStatusStore.getState().upsert(42, { agentStatus: "idle", needsAttention: false });
      useSessionStatusStore.getState().bumpDone(42, { kind: "done" });
    });
    expect(showNotification).toHaveBeenCalledTimes(1);
    expect(playSound).toHaveBeenCalledWith("ding");
    expect(toastSuccess).toHaveBeenCalledTimes(1);
  });

  it("挂载前已存在的 idle 会话不误报", () => {
    act(() => {
      useSessionStatusStore.getState().upsert(7, { agentStatus: "idle", needsAttention: false });
    });
    render(<TurnCompleteNotifier />);
    expect(showNotification).not.toHaveBeenCalled();
  });
});
```

> mock 路径深度:`turn-complete-notifier.tsx` 在 `frontend/src/components/agentre/`,故 wailsjs 为 `../../../../wailsjs/go/app/App`、stores/lib 为 `../../../...`。如实际宿主放置层级不同,按需调整相对路径。

- [ ] **Step 3: 运行,确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/turn-complete-notifier.test.tsx`
Expected: FAIL —— 模块不存在。

- [ ] **Step 4: 写实现**

`frontend/src/components/agentre/turn-complete-notifier.tsx`:

> **调整(2026-06-04):应用内 toast 改为 bespoke 卡片,不复用 sonner。**
> `showToast` 不再调 `toast.success/...`,而是把 `{sessionId,kind,title,body}` 推入新增的 `notification-toast-store`,由新增的 `NotificationToastViewport`(`notification-toast.tsx`)在右下角渲染 bespoke 卡片(左状态条 + agent 头像 + 「跳转到会话」+ ✕;`done` 自动消失,`error`/`waiting` 常驻)。因此:`NotifyDeps.showToast` 签名加 `sessionId`(见 Task 10 的 `turn-notify.ts`);头像复用从 `use-tabs-view.ts` 抽出的 `session-avatar.ts`;App 根同时挂 `<NotificationToastViewport/>`;新增 `notify.openSession`/`dismiss`/`justNow` 文案。下面代码块即实际实现。

```tsx
import * as React from "react";
import { useTranslation } from "react-i18next";

import { ShowNotification } from "../../../wailsjs/go/app/App";
import { isWindowFocused } from "../../lib/window-focus";
import { playNotifySound } from "../../lib/notify-sound";
import {
  classifyTransition,
  maybeNotify,
  type NotifyDeps,
} from "../../lib/turn-notify";
import { useChatTabsStore } from "../../stores/chat-tabs-store";
import { useNotificationSettingsStore } from "../../stores/notification-settings-store";
import { useNotificationToastStore } from "../../stores/notification-toast-store";
import { useSessionStatusStore } from "../../stores/session-status-store";

function activeSessionId(): number | null {
  const st = useChatTabsStore.getState();
  const tab = st.tabs.find((x) => x.id === st.activeTabId);
  return tab?.meta.kind === "session" ? tab.meta.sessionId : null;
}

function sessionTitle(sessionId: number): string | undefined {
  return useChatTabsStore
    .getState()
    .tabs.find(
      (x) => x.meta.kind === "session" && x.meta.sessionId === sessionId,
    )?.title;
}

// TurnCompleteNotifier 常驻 App 根、不渲染任何 UI;订阅 session 状态转换并在合适时机通知。
export function TurnCompleteNotifier(): null {
  const { t } = useTranslation();

  const deps = React.useMemo<NotifyDeps>(
    () => ({
      isWindowFocused,
      getActiveSessionId: activeSessionId,
      getSettings: () => useNotificationSettingsStore.getState().settings,
      getSessionTitle: sessionTitle,
      showSystemNotification: (title, body) => {
        ShowNotification({ title, body }).catch(() => {});
      },
      playSound: playNotifySound,
      showToast: (sessionId, kind, title, body) => {
        useNotificationToastStore
          .getState()
          .push({ sessionId, kind, title, body });
      },
      t,
    }),
    [t],
  );

  React.useEffect(() => {
    void useNotificationSettingsStore.getState().load();
  }, []);

  React.useEffect(() => {
    const prev = new Map<number, string>();
    for (const [id, v] of useSessionStatusStore.getState().statuses) {
      prev.set(id, v.agentStatus);
    }
    const unsub = useSessionStatusStore.subscribe((state) => {
      for (const [id, v] of state.statuses) {
        const before = prev.get(id);
        const after = v.agentStatus;
        if (before === after) continue;
        prev.set(id, after);
        const kind = classifyTransition(
          before as never,
          after,
          v.lastDoneEvent?.kind,
        );
        if (kind) maybeNotify(id, kind, deps);
      }
    });
    return unsub;
  }, [deps]);

  return null;
}
```

- [ ] **Step 5: 运行,确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/turn-complete-notifier.test.tsx`
Expected: PASS。

- [ ] **Step 6: 在 App 根挂载**

在 `frontend/src/App.tsx` 的 `App()` 里、`<ChatStreamsHost />` 旁边加 `<TurnCompleteNotifier />` 和 `<NotificationToastViewport />`(二者经 `@/components/agentre` barrel 导出):

```tsx
import { TurnCompleteNotifier, NotificationToastViewport } from "@/components/agentre";
// ...
      <ChatStreamsHost />
      <TurnCompleteNotifier />
      <NotificationToastViewport />
```

> `<Toaster/>`(sonner)继续保留 —— 别处(update / rich-link / terminal / data-backup / clipboard)仍用 sonner,只有 turn 完成通知改成 bespoke 卡片。

- [ ] **Step 7: 全量前端测试 + Commit**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/turn-complete-notifier.test.tsx`
Expected: PASS。

```bash
git add frontend/src/components/agentre/turn-complete-notifier.tsx \
  frontend/src/components/agentre/__tests__/turn-complete-notifier.test.tsx \
  frontend/src/App.tsx \
  frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "✨ notify(fe): turn 完成通知宿主 + 订阅逻辑 + 挂载"
```

---

## Task 12: 前端 — 通知设置面板 + 接入 settings.tsx

**Files:**
- Create: `frontend/src/components/agentre/notifications-panel.tsx`
- Test: `frontend/src/components/agentre/__tests__/notifications-panel.test.tsx`
- Modify: `frontend/src/components/agentre/settings.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`、`en/common.json`(加 `settings.notifications.*`,删 `settings.underConstruction.notifications.*`)
- Modify: `frontend/src/__tests__/i18n.test.ts`

- [ ] **Step 1: 加面板文案(两端同时),删 under-construction notifications**

在两个 `common.json` 的 `settings` 节点下加 `notifications`(zh-CN 值如下,en 用对应英文):

```json
    "notifications": {
      "pageTitle": "通知",
      "pageDesc": "对话完成、出错或需要你输入时提醒你。",
      "masterTitle": "通知",
      "masterDesc": "总开关 —— 关闭后下面所有方式都不生效。",
      "enableLabel": "启用通知",
      "enableDesc": "一轮对话结束时主动提醒你。",
      "onlyWhenUnfocusedLabel": "仅窗口未激活时通知",
      "onlyWhenUnfocusedDesc": "默认开;关掉后,应用在前台时,完成的若不是你正在看的会话也会提醒。",
      "channelsTitle": "通知方式",
      "channelsDesc": "满足提醒条件时,用以下已开启的方式提醒。",
      "systemLabel": "系统通知",
      "systemDesc": "弹到 macOS 通知中心 / Windows 操作中心,应用在后台也能看到。",
      "soundLabel": "提示音",
      "soundDesc": "完成时播放一段提示音。",
      "soundTest": "试听",
      "preset": { "ding": "叮", "chime": "和弦", "blip": "气泡" },
      "toastLabel": "应用内提示",
      "toastDesc": "在应用右下角弹一条提示(仅应用在前台时可见)。",
      "ruleTitle": "什么时候提醒",
      "ruleDesc": "只在你没盯着该会话时提醒 —— 窗口失焦,或完成的不是你当前正在看的会话。完成、出错、等你输入都会提醒;你手动停止的不会。"
    },
```

en 对应值:

```json
    "notifications": {
      "pageTitle": "Notifications",
      "pageDesc": "Get notified when a conversation finishes, fails, or needs your input.",
      "masterTitle": "Notifications",
      "masterDesc": "Master switch — turning this off disables every method below.",
      "enableLabel": "Enable notifications",
      "enableDesc": "Alert you when a turn finishes.",
      "onlyWhenUnfocusedLabel": "Only when app is in background",
      "onlyWhenUnfocusedDesc": "On by default; turn off to also be notified when the app is focused but the finished session isn't the one you're viewing.",
      "channelsTitle": "Methods",
      "channelsDesc": "When the alert condition is met, notify via any method enabled below.",
      "systemLabel": "System notification",
      "systemDesc": "Posts to macOS Notification Center / Windows Action Center — visible even when the app is in the background.",
      "soundLabel": "Sound",
      "soundDesc": "Play a short chime on completion.",
      "soundTest": "Test",
      "preset": { "ding": "Ding", "chime": "Chime", "blip": "Blip" },
      "toastLabel": "In-app toast",
      "toastDesc": "Pop a toast in the bottom-right of the app (only visible while the app is focused).",
      "ruleTitle": "When you get notified",
      "ruleDesc": "Only when you're not watching that session — the window is unfocused, or the finished session isn't the one you're viewing. Done, error, and needs-input all notify; ones you stop manually don't."
    },
```

删除两个 `common.json` 里 `settings.underConstruction.notifications`(整段三行)。

- [ ] **Step 2: 改 i18n.test.ts 的 key 列表**

在 `frontend/src/__tests__/i18n.test.ts` 的 `shellAndSettingsKeys` 数组里:
- 删除 `"settings.underConstruction.notifications.description"` 与 `"settings.underConstruction.notifications.title"` 两行;
- 追加面板代表性 key:

```ts
    "settings.notifications.pageTitle",
    "settings.notifications.enableLabel",
    "settings.notifications.onlyWhenUnfocusedLabel",
    "settings.notifications.soundTest",
    "settings.notifications.ruleDesc",
    "notify.body.done",
```

- [ ] **Step 3: 写面板失败测试**

`frontend/src/components/agentre/__tests__/notifications-panel.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const updateMock = vi.fn(() => Promise.resolve({}));
vi.mock("../../../wailsjs/go/app/App", () => ({
  GetAppSetting: vi.fn(() => Promise.reject(new Error("nf"))),
  UpdateAppSettings: (req: unknown) => updateMock(req),
}));
const playSound = vi.fn();
vi.mock("../../lib/notify-sound", () => ({
  playNotifySound: (p: unknown) => playSound(p),
  SOUND_PRESETS: ["ding", "chime", "blip"],
}));

import "../../../i18n";
import { NotificationsPanel } from "../notifications-panel";
import { useNotificationSettingsStore, DEFAULT_NOTIFICATION_SETTINGS } from "../../../stores/notification-settings-store";

beforeEach(() => {
  vi.clearAllMocks();
  useNotificationSettingsStore.setState({ settings: { ...DEFAULT_NOTIFICATION_SETTINGS } });
});
afterEach(() => vi.restoreAllMocks());

describe("NotificationsPanel", () => {
  it("渲染各开关 + 试听按钮", () => {
    render(<NotificationsPanel />);
    expect(screen.getByText("启用通知")).toBeInTheDocument();
    expect(screen.getByText("系统通知")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "试听" })).toBeInTheDocument();
  });

  it("点试听调 playNotifySound", async () => {
    render(<NotificationsPanel />);
    await userEvent.click(screen.getByRole("button", { name: "试听" }));
    expect(playSound).toHaveBeenCalledWith("ding");
  });

  it("切换系统通知开关写库", async () => {
    render(<NotificationsPanel />);
    const sw = screen.getByRole("switch", { name: "系统通知" });
    await userEvent.click(sw); // 默认开 → 关
    expect(updateMock).toHaveBeenCalledWith({ entries: [{ key: "notify.system", value: "false" }] });
  });
});
```

> 测试默认语言为 zh-CN(见 `src/i18n` 初始化);若默认 en,把断言文案改成英文。

- [ ] **Step 4: 运行,确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/notifications-panel.test.tsx`
Expected: FAIL —— 模块不存在。

- [ ] **Step 5: 写面板实现**

`frontend/src/components/agentre/notifications-panel.tsx`:

```tsx
import * as React from "react";
import { Play } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";

import { SOUND_PRESETS, playNotifySound, type SoundPreset } from "../../lib/notify-sound";
import {
  useNotificationSettingsStore,
  type NotificationSettings,
} from "../../stores/notification-settings-store";

export function NotificationsPanel() {
  const { t } = useTranslation();
  const settings = useNotificationSettingsStore((s) => s.settings);
  const save = useNotificationSettingsStore((s) => s.save);
  const load = useNotificationSettingsStore((s) => s.load);

  React.useEffect(() => {
    void load();
  }, [load]);

  const set = (patch: Partial<NotificationSettings>) => void save(patch);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <header className="border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold">{t("settings.notifications.masterTitle")}</h2>
          <p className="text-2xs leading-relaxed text-muted-foreground">
            {t("settings.notifications.masterDesc")}
          </p>
        </header>
        <Row label={t("settings.notifications.enableLabel")} desc={t("settings.notifications.enableDesc")}>
          <Switch
            aria-label={t("settings.notifications.enableLabel")}
            checked={settings.enabled}
            onCheckedChange={(v) => set({ enabled: v })}
          />
        </Row>
        <Row
          label={t("settings.notifications.onlyWhenUnfocusedLabel")}
          desc={t("settings.notifications.onlyWhenUnfocusedDesc")}
        >
          <Switch
            aria-label={t("settings.notifications.onlyWhenUnfocusedLabel")}
            checked={settings.onlyWhenUnfocused}
            onCheckedChange={(v) => set({ onlyWhenUnfocused: v })}
          />
        </Row>
      </section>

      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <header className="border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold">{t("settings.notifications.channelsTitle")}</h2>
          <p className="text-2xs leading-relaxed text-muted-foreground">
            {t("settings.notifications.channelsDesc")}
          </p>
        </header>

        <Row label={t("settings.notifications.systemLabel")} desc={t("settings.notifications.systemDesc")}>
          <Switch
            aria-label={t("settings.notifications.systemLabel")}
            checked={settings.system}
            onCheckedChange={(v) => set({ system: v })}
          />
        </Row>

        <Row label={t("settings.notifications.soundLabel")} desc={t("settings.notifications.soundDesc")}>
          <Select
            value={settings.soundPreset}
            onValueChange={(v) => set({ soundPreset: v as SoundPreset })}
          >
            <SelectTrigger className="h-8 w-28 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {SOUND_PRESETS.map((p) => (
                <SelectItem key={p} value={p}>
                  {t(`settings.notifications.preset.${p}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-8 gap-1.5 px-3 text-xs"
            onClick={() => playNotifySound(settings.soundPreset)}
          >
            <Play className="size-3.5" aria-hidden="true" />
            {t("settings.notifications.soundTest")}
          </Button>
          <Switch
            aria-label={t("settings.notifications.soundLabel")}
            checked={settings.sound}
            onCheckedChange={(v) => set({ sound: v })}
          />
        </Row>

        <Row label={t("settings.notifications.toastLabel")} desc={t("settings.notifications.toastDesc")}>
          <Switch
            aria-label={t("settings.notifications.toastLabel")}
            checked={settings.toast}
            onCheckedChange={(v) => set({ toast: v })}
          />
        </Row>
      </section>

      <div className="rounded-lg border border-primary-text/30 bg-primary-soft px-4 py-3 text-primary-text">
        <p className="text-xs font-semibold">{t("settings.notifications.ruleTitle")}</p>
        <p className="text-2xs leading-relaxed">{t("settings.notifications.ruleDesc")}</p>
      </div>
    </div>
  );
}

function Row({
  label,
  desc,
  children,
}: {
  label: string;
  desc: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-4 border-t border-border px-4 py-3 first:border-t-0">
      <div className="min-w-0 flex-1">
        <div className="text-xs font-medium">{label}</div>
        <div className="text-2xs leading-relaxed text-muted-foreground">{desc}</div>
      </div>
      <div className="flex items-center gap-2">{children}</div>
    </div>
  );
}
```

- [ ] **Step 6: 接入 settings.tsx**

在 `frontend/src/components/agentre/settings.tsx`:
1. import:`import { NotificationsPanel } from "./notifications-panel";`
2. 从 `underConstructionSettingsPages` 删掉 `notifications` 条目,并把它从该 map 类型的 `Exclude<SettingsPageId, ...>` 联合里加进排除项(即在 `Exclude` 第二参里追加 `| "notifications"`)。
3. 在渲染选中页的 switch/分支里,`case "notifications":` 返回 `<NotificationsPanel />`(参照 `local-proxy` → `<SettingsProxyPanel />` 的分支写法)。

- [ ] **Step 7: 运行面板测试 + i18n 测试**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/notifications-panel.test.tsx src/__tests__/i18n.test.ts`
Expected: PASS(两文件全绿)。

- [ ] **Step 8: Commit**

```bash
git add frontend/src/components/agentre/notifications-panel.tsx \
  frontend/src/components/agentre/__tests__/notifications-panel.test.tsx \
  frontend/src/components/agentre/settings.tsx \
  frontend/src/__tests__/i18n.test.ts \
  frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "✨ notify(fe): 通知设置面板 + 接入设置页"
```

---

## Task 13: 全量验证

**Files:** 无新增。

- [ ] **Step 1: 后端 lint + race 测试**

Run: `make lint && make test-backend`
Expected: 0 lint 问题;`./internal/...` race 测试全绿。

- [ ] **Step 2: 前端 lint + 全量测试**

Run: `cd frontend && pnpm lint && pnpm test`
Expected: ESLint 0 错误(特别是 `i18next/no-literal-string` 不应报新面板/宿主);Vitest 全绿。

- [ ] **Step 3: 手动冒烟(macOS,可选但建议)**

Run: `make dev`
操作:开两个会话 tab → 停在会话 A → 在会话 B 发一条消息 → 切到别的 app(失焦)→ 等 B 跑完。
Expected:收到一条系统通知「会话已完成」;若开了提示音能听到「叮」;回到前台停在 B 自己发消息跑完则**不**弹(你正盯着它)。

- [ ] **Step 4: 最终提交(若冒烟有微调)**

```bash
git add -A
git commit -m "✅ notify: 全量 lint/test 通过"
```

---

## Self-Review 记录(plan 作者已核对)

- **Spec 覆盖**:三渠道(系统/声音/toast)= Task 5/7/11/12;可配置 + 默认值 = Task 9/12;三终态 done/error/waiting + 跳过 aborted = Task 10;门槛(`onlyWhenUnfocused` 开关,默认仅失焦;关掉回到失焦或非当前会话)= Task 10/11/12;`app_settings_svc` 离散 key = Task 1/2/3;前端编排 + 薄后端绑定 = Task 4/5/6/11;i18n 双语 = Task 11/12。
- **2026-06-04 调整**:应用内 toast 由 sonner 改为 bespoke 卡片(`notification-toast-store` + `NotificationToastViewport` + 复用的 `session-avatar`,见 Task 11 调整说明)。spec「已知局限」里的「无点击跳转」**仅对系统通知仍成立**——应用内 toast 现已带「跳转到会话」动作(`openSession`);无节流 / 后台音频 / 无 per-event 开关 三项按设计仍不实现。
- **类型一致性**:`SoundPreset`(notify-sound.ts)被 store / turn-notify / 面板复用;`NotifyKind` / `NotifyDeps`(turn-notify.ts)被 hook 复用;`NotificationSettings` / `DEFAULT_NOTIFICATION_SETTINGS`(store)被 turn-notify / 面板 / 宿主复用;`Notifier`(notification_svc)被 sysnotify 结构化满足、bootstrap 注入;`ShowRequest` 后端定义、前端经 `make generate` 取得。
- **占位符扫描**:无 TBD/TODO;每个改代码的 step 均含完整代码与精确命令。唯一外部依赖不确定点(beeep `Notify` 签名)已在 Task 5 Step 1 给出 `go doc` 校验命令 + 唯一调整点说明。
