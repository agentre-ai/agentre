package pairing

import (
	"sync"
	"time"
)

type RateLimitOpts struct {
	MaxAttempts int
	Window      time.Duration
	Clock       func() time.Time
}

type RateLimiter struct {
	opts RateLimitOpts
	mu   sync.Mutex
	hits map[string][]time.Time
}

func NewRateLimiter(opts RateLimitOpts) *RateLimiter {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	return &RateLimiter{opts: opts, hits: map[string][]time.Time{}}
}

// Allow returns true if the IP has not exceeded MaxAttempts within Window.
func (r *RateLimiter) Allow(ip string) bool {
	now := r.opts.Clock()
	cutoff := now.Add(-r.opts.Window)
	r.mu.Lock()
	defer r.mu.Unlock()

	hist := r.hits[ip]
	pruned := hist[:0]
	for _, t := range hist {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	if len(pruned) >= r.opts.MaxAttempts {
		r.hits[ip] = pruned
		return false
	}
	r.hits[ip] = append(pruned, now)
	return true
}
