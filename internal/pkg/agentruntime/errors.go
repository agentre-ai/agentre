package agentruntime

import "errors"

// ErrNoActiveTurn 来自 Steer / Abort / SetPermissionMode 等控制接口:对应
// sessionID 没有 in-flight turn 时返回。chat_svc 据此翻译为 ChatSteerNoActive /
// ChatStopNoActive 给前端。
var ErrNoActiveTurn = errors.New("agentruntime: no active turn for session")

// ErrSteerNotFound CancelSteer 收到非空 queuedID 但 pending 队列里找不到
// (AI 已消费或 ID 从未入队)。chat_svc 翻译为 ChatCancelNotFound。
var ErrSteerNotFound = errors.New("agentruntime: queued steer entry not found")

// ErrAborted RunResult.StopErr 在用户主动 Abort 时填这个,让 chat_svc 区分
// "正常 Done" / "用户中止" / "真错误"三态。当前由 remote runner 使用。
var ErrAborted = errors.New("agentruntime: chat aborted by user")

// ErrUnsupported runtime 不支持的能力(对应 capability bool=false)。
var ErrUnsupported = errors.New("agentruntime: capability unsupported by this runtime")
