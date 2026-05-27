package canonical

// UserAsk 结构化问答;前端 UserAskCard 渲染。
// 来源:claudecode AskUserQuestion control_request / codex request_user_input RPC。
//
// Answered/Skipped/Answers 由 UserAskResolved event 反向回灌(等同 blocks.UserAskBlock
// 的状态)。Questions/Answers 用 any 是因为顶层 agentruntime/blocks 包内的 DTO 类型
// 命名相同但放在不同包,canonical 不内联(避免循环导入)。view 投影时按断言传 wire。
type UserAsk struct {
	RequestID string `json:"requestId"`
	Questions any    `json:"questions"`
	Answers   any    `json:"answers,omitempty"`
	Answered  bool   `json:"answered,omitempty"`
	Skipped   bool   `json:"skipped,omitempty"`
}

func (UserAsk) canonicalKind() Kind { return KindUserAsk }
