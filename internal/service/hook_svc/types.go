// Package hook_svc 暴露脚本 Hook 的 CRUD / 试运行 / 调度服务契约给 Wails 绑定层。
package hook_svc

type EnvVar struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

type HookItem struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Interpreter    string   `json:"interpreter"`
	Command        string   `json:"command"`
	ScheduleExpr   string   `json:"scheduleExpr"`
	Timezone       string   `json:"timezone"`
	Env            []EnvVar `json:"env"`
	Enabled        bool     `json:"enabled"`
	NextRunAt      int64    `json:"nextRunAt"`
	LastRunAt      int64    `json:"lastRunAt"`
	LastStatus     string   `json:"lastStatus"`
	LastError      string   `json:"lastError"`
	LastDurationMs int64    `json:"lastDurationMs"`
	TotalCount     int64    `json:"totalCount"`
	Createtime     int64    `json:"createtime"`
	Updatetime     int64    `json:"updatetime"`
}

type HookEventItem struct {
	ID          int64  `json:"id"`
	HookID      int64  `json:"hookId"`
	Kind        string `json:"kind"` // "output"（脚本产出）| "failure"（运行失败留痕）
	Title       string `json:"title"`
	DedupeKey   string `json:"dedupeKey"`
	PayloadJSON string `json:"payloadJson"`
	ReceivedAt  int64  `json:"receivedAt"`
	Createtime  int64  `json:"createtime"`
}

type LoadHooksRequest struct {
	HookID int64 `json:"hookId"`
	Limit  int   `json:"limit"`
}

type LoadHooksResponse struct {
	Hooks  []*HookItem      `json:"hooks"`
	Events []*HookEventItem `json:"events"`
}

type CreateHookRequest struct {
	Name         string   `json:"name" binding:"required"`
	Interpreter  string   `json:"interpreter" binding:"required"`
	Command      string   `json:"command"`
	ScheduleExpr string   `json:"scheduleExpr"`
	Timezone     string   `json:"timezone"`
	Env          []EnvVar `json:"env"`
	Enabled      bool     `json:"enabled"`
}

type UpdateHookRequest struct {
	ID int64 `json:"id" binding:"required"`
	CreateHookRequest
}

type RunHookRequest struct {
	ID     int64 `json:"id" binding:"required"`
	DryRun bool  `json:"dryRun"`
}

type RunHookResult struct {
	ExitCode   int              `json:"exitCode"`
	DurationMs int64            `json:"durationMs"`
	TimedOut   bool             `json:"timedOut"`
	Stdout     string           `json:"stdout"`
	Stderr     string           `json:"stderr"`
	ParseError string           `json:"parseError"`
	Events     []*HookEventItem `json:"events"`   // 解析出的事件（dry-run 不落库）
	NewCount   int              `json:"newCount"` // 去重后将/已入库数
	DupCount   int              `json:"dupCount"`
	Persisted  bool             `json:"persisted"`
}
