// Package sysnotify 基于 beeep 的跨平台系统通知实现（平台叶子，不反向依赖 service 层）。
package sysnotify

import "github.com/gen2brain/beeep"

// Notifier 满足 notification_svc.Notifier（结构化接口）。
type Notifier struct{}

// New 构造一个系统通知器。
func New() *Notifier { return &Notifier{} }

// Notify 弹一条静音系统通知；提示音由前端独立控制，这里不带声音以免重复响。
func (*Notifier) Notify(title, body string) error {
	return beeep.Notify(title, body, "")
}
