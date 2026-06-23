// Package view 集中 chat_svc 的 wire DTO:ChatBlock(history replay) + CanonicalDTO
// (canonical 投影)+ ProjectMessageBlocks([]ContentBlock → []ChatBlock)。
//
// 注:chat_svc.ChatBlock(types.go)仍在使用,view.ChatBlock 是其精简兄弟,
// 用于 replay/wire 投影。
package view

import (
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/canonical"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
)

// ChatBlock 是 wire DTO,前端 React 直接吃。canonical 字段在工具调用上时填充,
// 与 raw input 并存(canonical 是计算视图)。
type ChatBlock struct {
	Type             string         `json:"type"`
	Text             string         `json:"text,omitempty"`
	ToolUseID        string         `json:"toolUseId,omitempty"`
	ToolName         string         `json:"toolName,omitempty"`
	ToolInput        map[string]any `json:"toolInput,omitempty"`
	ToolResult       string         `json:"toolResult,omitempty"`
	IsError          bool           `json:"isError,omitempty"`
	ParentToolCallID string         `json:"parentToolCallId,omitempty"`

	// Canonical: 工具调用走 canonical 识别成功时填,与 raw input 并存。
	Canonical *CanonicalDTO `json:"canonical,omitempty"`

	// 控制事件投影
	UserAsk        *blocks.UserAskBlock        `json:"userAsk,omitempty"`
	ToolPermission *blocks.ToolPermissionBlock `json:"toolPermission,omitempty"`
	Subagent       *blocks.SubagentStateBlock  `json:"subagent,omitempty"`
}

// CanonicalDTO wire 形态,与前端 TS discriminated union 一一对应。
type CanonicalDTO struct {
	Kind           canonical.Kind                `json:"kind"`
	FileWrite      *canonical.FileWrite          `json:"fileWrite,omitempty"`
	FileEdit       *canonical.FileEdit           `json:"fileEdit,omitempty"`
	UserAsk        *canonical.UserAsk            `json:"userAsk,omitempty"`
	PlanUpdate     *canonical.PlanUpdate         `json:"planUpdate,omitempty"`
	PlanApprove    *canonical.PlanApproveRequest `json:"planApprove,omitempty"`
	AgentSpawn     *canonical.AgentSpawn         `json:"agentSpawn,omitempty"`
	ToolPermission *canonical.ToolPermission     `json:"toolPermission,omitempty"`
}
