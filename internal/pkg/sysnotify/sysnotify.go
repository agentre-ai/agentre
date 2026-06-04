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
// ctx 必须是 Wails 运行时/生命周期 ctx（即 app.ctx）；传入其它 ctx 会经 Wails getFrontend
// 触发 log.Fatalf/os.Exit 终止进程（错误被忽略，不会以其它方式暴露）。
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
