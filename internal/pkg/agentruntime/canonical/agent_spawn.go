package canonical

// AgentSpawn 子代理派遣;前端 AgentSpawnCard 渲染。
// 来源:claudecode Task 工具 + subagent frames / codex collabAgentToolCall。
type AgentSpawn struct {
	TaskID          string `json:"taskId"`
	SubagentType    string `json:"subagentType,omitempty"`
	TaskDescription string `json:"taskDescription,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
	// 运行时累计态(来自 SubagentStarted/Progress/Done events 或 SubagentStateBlock):
	LastToolName string `json:"lastToolName,omitempty"`
	ToolUses     int    `json:"toolUses,omitempty"`
	TotalTokens  int    `json:"totalTokens,omitempty"`
	DurationMs   int    `json:"durationMs,omitempty"`
	Status       string `json:"status,omitempty"` // running | completed | failed
}

func (AgentSpawn) canonicalKind() Kind { return KindAgentSpawn }
