package canonical

// AgentSpawn 子代理派遣;前端 AgentSpawnCard 渲染。
// 来源:claudecode Task 工具 + subagent frames / codex collabAgentToolCall。
type AgentSpawn struct {
	TaskID          string `json:"taskId"`
	SubagentType    string `json:"subagentType,omitempty"`
	TaskDescription string `json:"taskDescription,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
	Model           string `json:"model,omitempty"` // 工具入参别名 (haiku)；两个写入方(translator.go 实时路径、chat.go replay 路径)都只读 input["model"]，dispatcher_emitter.go 重建时也不拷贝——「实际模型」只在前端 readSpawn 合并运行时字段之后才成立
	// 运行时累计态(来自 SubagentStarted/Progress/Done events 或 SubagentStateBlock):
	LastToolName string `json:"lastToolName,omitempty"`
	ToolUses     int    `json:"toolUses,omitempty"`
	TotalTokens  int    `json:"totalTokens,omitempty"`
	DurationMs   int    `json:"durationMs,omitempty"`
	Status       string `json:"status,omitempty"` // running | completed | failed
}

func (AgentSpawn) canonicalKind() Kind { return KindAgentSpawn }
