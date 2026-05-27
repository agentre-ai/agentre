// Package remote_device_watcher_svc 维护每台已配对 agentred 的长连状态机:
// 拨号 → 心跳 → 断开 → 退避重连;状态变更同步写 repo + emit 给前端。
package remote_device_watcher_svc

import (
	"math/rand"
	"time"
)

// BackoffConfig 控制 Backoff 行为。
type BackoffConfig struct {
	Initial    time.Duration
	Max        time.Duration
	Multiplier float64
	Jitter     float64 // 0~1,实际等待 = base * (1 ± Jitter * rand)
}

// Backoff 是非线程安全的指数退避计数器。watcher goroutine 独占。
type Backoff struct {
	cfg     BackoffConfig
	current time.Duration
	rand    *rand.Rand
}

// NewBackoff 构造一个等待 cfg.Initial 起步的退避器。
func NewBackoff(cfg BackoffConfig, r *rand.Rand) *Backoff {
	return &Backoff{cfg: cfg, current: cfg.Initial, rand: r}
}

// Next 返回本次等待时长,并把 current 推到下一档(不超过 Max)。
func (b *Backoff) Next() time.Duration {
	base := b.current
	next := time.Duration(float64(b.current) * b.cfg.Multiplier)
	if next > b.cfg.Max {
		next = b.cfg.Max
	}
	b.current = next
	if b.cfg.Jitter <= 0 {
		return base
	}
	delta := (b.rand.Float64()*2 - 1) * b.cfg.Jitter
	return time.Duration(float64(base) * (1 + delta))
}

// Reset 把退避计数器恢复到 cfg.Initial(连接成功后调用)。
func (b *Backoff) Reset() { b.current = b.cfg.Initial }
