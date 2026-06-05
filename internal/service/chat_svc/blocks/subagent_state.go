package blocks

import cagoblocks "github.com/cago-frame/agents/agent/blocks"

// SubagentStateBlock 持久化 subagent 累计态。
// ParentToolCallID 对应 Task 工具的 ToolCallID;NestedToolCallIDs 反向索引该 subagent
// 派遣下属于它的所有 NestedToolUseBlock.ID,replay 时按这个数组把内层卡片归集到父
// AgentSpawnCard。
type SubagentStateBlock struct {
	ParentToolCallID  string   `json:"parent_tool_call_id"`
	TaskID            string   `json:"task_id,omitempty"`
	Kind              string   `json:"kind,omitempty"`        // local_bash | local_agent
	Description       string   `json:"description,omitempty"` // 任务名（task_started.description）
	TotalTokens       int      `json:"total_tokens,omitempty"`
	DurationMs        int      `json:"duration_ms,omitempty"`
	Status            string   `json:"status"`            // running | completed | failed | canceled
	Summary           string   `json:"summary,omitempty"` // CLI task_notification.summary（如退出码说明）
	LastToolName      string   `json:"last_tool_name,omitempty"`
	ToolUses          int      `json:"tool_uses,omitempty"`
	NestedToolCallIDs []string `json:"nested_tool_call_ids,omitempty"`
}

func (SubagentStateBlock) Type() string                      { return "subagent_state" }
func (SubagentStateBlock) Audience() cagoblocks.AudienceMask { return cagoblocks.ToUI }

func init() { cagoblocks.RegisterFactory[SubagentStateBlock]() }
