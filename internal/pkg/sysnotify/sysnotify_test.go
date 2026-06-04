package sysnotify

import (
	"context"
	"testing"
)

// 不在 CI 真弹通知（会触达 OS）；只验证构造与接口形状。
func TestNew(t *testing.T) {
	n := New()
	if n == nil {
		t.Fatal("New() 返回 nil")
	}
	// 编译期保证 *Notifier 满足 Notify(ctx, title, body string, sessionID int64) error 形状。
	var _ interface {
		Notify(context.Context, string, string, int64) error
	} = n
}
