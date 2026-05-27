package remote_device_watcher_svc

import (
	"context"
	"sync"
	"time"
)

// Clock 抽象 watcher 需要的全部时间相关 IO。生产用 realClock,单测注入 FakeClock。
type Clock interface {
	Now() time.Time
	NowMs() int64
	// Sleep 阻塞 d 后返回 true;ctx cancel 立即返回 false。
	Sleep(ctx context.Context, d time.Duration) bool
}

// NewRealClock 返回生产实现。
func NewRealClock() Clock { return realClock{} }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) NowMs() int64   { return time.Now().UnixMilli() }
func (realClock) Sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// FakeClock 是测试用的可推进时钟。Advance() 同步推进虚拟时间并唤醒
// 所有等到点的 Sleep 调用。
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []*fakeWaiter
}

type fakeWaiter struct {
	deadline time.Time
	ch       chan struct{}
}

// NewFakeClock 以 start 为初始时间创建。
func NewFakeClock(start time.Time) *FakeClock { return &FakeClock{now: start} }

// Now 当前虚拟时间。
func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// NowMs 当前虚拟时间(unix ms)。
func (f *FakeClock) NowMs() int64 { return f.Now().UnixMilli() }

// Sleep 阻塞直到虚拟时间到 deadline,或 ctx 被取消。
func (f *FakeClock) Sleep(ctx context.Context, d time.Duration) bool {
	f.mu.Lock()
	w := &fakeWaiter{deadline: f.now.Add(d), ch: make(chan struct{})}
	f.waiters = append(f.waiters, w)
	f.mu.Unlock()
	select {
	case <-ctx.Done():
		return false
	case <-w.ch:
		return true
	}
}

// Advance 推进虚拟时间,唤醒所有 deadline ≤ now 的 waiter。
func (f *FakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	remaining := f.waiters[:0]
	var fired []*fakeWaiter
	for _, w := range f.waiters {
		if !w.deadline.After(f.now) {
			fired = append(fired, w)
		} else {
			remaining = append(remaining, w)
		}
	}
	f.waiters = remaining
	f.mu.Unlock()
	for _, w := range fired {
		close(w.ch)
	}
}
