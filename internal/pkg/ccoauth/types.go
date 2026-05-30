// Package ccoauth 封装 Claude Code 内部 OAuth usage endpoint
// (GET https://api.anthropic.com/api/oauth/usage) 的读取，复用给桌面端
// cc_usage_svc 和 agentred daemon 远端 RPC。
//
// 我们只读 OAuth access token，不做 refresh、不写回 .credentials.json，
// 避免和 Claude Code 主进程抢锁。token 过期时返回 ErrAuthExpired，
// 用户需自行去对应机器跑 `claude /login`。
package ccoauth

import "time"

// RateLimits 是 /api/oauth/usage 响应的归一化结果。百分比统一在 [0,100]，
// resets_at 字段缺失时为 nil。Sonnet/Opus 专属配额可选，account 没有就为 nil。
type RateLimits struct {
	FiveHourPercent  float64    `json:"fiveHourPercent"`
	WeeklyPercent    float64    `json:"weeklyPercent"`
	FiveHourResetsAt *time.Time `json:"fiveHourResetsAt,omitempty"`
	WeeklyResetsAt   *time.Time `json:"weeklyResetsAt,omitempty"`

	SonnetWeeklyPercent  *float64   `json:"sonnetWeeklyPercent,omitempty"`
	SonnetWeeklyResetsAt *time.Time `json:"sonnetWeeklyResetsAt,omitempty"`
	OpusWeeklyPercent    *float64   `json:"opusWeeklyPercent,omitempty"`
	OpusWeeklyResetsAt   *time.Time `json:"opusWeeklyResetsAt,omitempty"`
}
