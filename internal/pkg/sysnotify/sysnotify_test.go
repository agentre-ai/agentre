package sysnotify

import "testing"

// 不在 CI 真弹通知（会触达 OS）；只验证构造与接口形状。
func TestNew(t *testing.T) {
	n := New()
	if n == nil {
		t.Fatal("New() 返回 nil")
	}
	// 编译期保证 *Notifier 满足 Notify(title, body string) error 形状。
	var _ interface{ Notify(string, string) error } = n
}
