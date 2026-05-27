package view

import (
	"strings"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"

	"agentre/internal/service/chat_svc/blocks"
)

// ProjectMessageBlocks 把 cago []ContentBlock 投影成 wire []ChatBlock。
// History replay 路径调这里;live emit 路径走 handlers 直接拼 wire payload。
//
// 注:本投影不重新计算 canonical —— canonical 是 runtime 层翻译产物,replay 时
// 只能从 raw ToolUseBlock.Name + Input 推断(走 runtime-agnostic 的简化识别)。
func ProjectMessageBlocks(bs []cagoblocks.ContentBlock) []ChatBlock {
	out := make([]ChatBlock, 0, len(bs))
	for _, b := range bs {
		switch t := b.(type) {
		case *cagoblocks.TextBlock:
			out = append(out, ChatBlock{Type: "text", Text: t.Text})
		case cagoblocks.TextBlock:
			out = append(out, ChatBlock{Type: "text", Text: t.Text})
		case *cagoblocks.ThinkingBlock:
			out = append(out, ChatBlock{Type: "thinking", Text: t.Text})
		case cagoblocks.ThinkingBlock:
			out = append(out, ChatBlock{Type: "thinking", Text: t.Text})
		case *cagoblocks.ToolUseBlock:
			out = append(out, ChatBlock{Type: "tool_use", ToolUseID: t.ID, ToolName: t.Name, ToolInput: t.Input})
		case cagoblocks.ToolUseBlock:
			out = append(out, ChatBlock{Type: "tool_use", ToolUseID: t.ID, ToolName: t.Name, ToolInput: t.Input})
		case *cagoblocks.ToolResultBlock:
			out = append(out, ChatBlock{
				Type: "tool_result", ToolUseID: t.ToolUseID, IsError: t.IsError,
				ToolResult: flattenToolResultText(t.Content),
			})
		case cagoblocks.ToolResultBlock:
			out = append(out, ChatBlock{
				Type: "tool_result", ToolUseID: t.ToolUseID, IsError: t.IsError,
				ToolResult: flattenToolResultText(t.Content),
			})
		case *blocks.NestedToolUseBlock:
			out = append(out, ChatBlock{
				Type: "tool_use", ToolUseID: t.ID, ToolName: t.Name, ToolInput: t.Input,
				ParentToolCallID: t.ParentToolCallID,
			})
		case blocks.NestedToolUseBlock:
			out = append(out, ChatBlock{
				Type: "tool_use", ToolUseID: t.ID, ToolName: t.Name, ToolInput: t.Input,
				ParentToolCallID: t.ParentToolCallID,
			})
		case *blocks.NestedToolResultBlock:
			out = append(out, ChatBlock{
				Type: "tool_result", ToolUseID: t.ToolCallID, IsError: t.IsError,
				ToolResult: t.Content, ParentToolCallID: t.ParentToolCallID,
			})
		case blocks.NestedToolResultBlock:
			out = append(out, ChatBlock{
				Type: "tool_result", ToolUseID: t.ToolCallID, IsError: t.IsError,
				ToolResult: t.Content, ParentToolCallID: t.ParentToolCallID,
			})
		case *blocks.UserAskBlock:
			out = append(out, ChatBlock{Type: "user_ask", UserAsk: t})
		case blocks.UserAskBlock:
			out = append(out, ChatBlock{Type: "user_ask", UserAsk: &t})
		case *blocks.ToolPermissionBlock:
			out = append(out, ChatBlock{Type: "tool_permission", ToolPermission: t})
		case blocks.ToolPermissionBlock:
			out = append(out, ChatBlock{Type: "tool_permission", ToolPermission: &t})
		case *blocks.SubagentStateBlock:
			out = append(out, ChatBlock{Type: "subagent_state", Subagent: t})
		case blocks.SubagentStateBlock:
			out = append(out, ChatBlock{Type: "subagent_state", Subagent: &t})
		case *blocks.PermissionModeChangeBlock, blocks.PermissionModeChangeBlock:
			out = append(out, ChatBlock{Type: "permission_mode_change"})
		case *blocks.CompactBoundaryBlock:
			if t != nil {
				out = append(out, ChatBlock{Type: "compact_boundary"})
			}
		case blocks.CompactBoundaryBlock:
			out = append(out, ChatBlock{Type: "compact_boundary"})
		}
	}
	return out
}

func flattenToolResultText(cs []cagoblocks.ContentBlock) string {
	var sb strings.Builder
	for _, c := range cs {
		switch t := c.(type) {
		case *cagoblocks.TextBlock:
			sb.WriteString(t.Text)
		case cagoblocks.TextBlock:
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}
