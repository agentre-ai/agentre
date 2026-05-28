package cc_usage_svc

import (
	"context"
	"time"
)

// tickerEntry 跟踪一个 device 的后台轮询 goroutine。
type tickerEntry struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// StartTicker 启动一个 goroutine,每隔 interval 调一次 Probe(ctx, key)。
// 同一 key 已有 ticker 时先 stop 再 start(防止 goroutine 泄漏)。
// ctx 取消或 StopTicker 调用时 goroutine 退出。
func (m *Manager) StartTicker(ctx context.Context, key DeviceKey, interval time.Duration) {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	m.tickerMu.Lock()
	if old, ok := m.tickers[key]; ok {
		old.cancel()
		<-old.done
	}
	tickCtx, tickCancel := context.WithCancel(ctx)
	entry := &tickerEntry{cancel: tickCancel, done: make(chan struct{})}
	m.tickers[key] = entry
	m.tickerMu.Unlock()

	go func() {
		defer close(entry.done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-tickCtx.Done():
				return
			case <-t.C:
				m.Probe(tickCtx, key)
			}
		}
	}()
}

// StopTicker 取消指定 key 的 ticker 并等待 goroutine 退出。
// 未注册的 key 静默无操作。
func (m *Manager) StopTicker(key DeviceKey) {
	m.tickerMu.Lock()
	entry, ok := m.tickers[key]
	if ok {
		delete(m.tickers, key)
	}
	m.tickerMu.Unlock()
	if !ok {
		return
	}
	entry.cancel()
	<-entry.done
}

// StopAllTickers 停止所有 ticker。App.Shutdown 时调用。
func (m *Manager) StopAllTickers() {
	m.tickerMu.Lock()
	all := m.tickers
	m.tickers = map[DeviceKey]*tickerEntry{}
	m.tickerMu.Unlock()
	for _, e := range all {
		e.cancel()
		<-e.done
	}
}
