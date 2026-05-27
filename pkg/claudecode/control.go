package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ControlRequestEvent EventControlRequest 携带，来自 Claude Code 在
// --permission-prompt-tool stdio 模式下发的工具调用许可请求。
//
// 协议形状（实测 Claude Code 2.1.145，/tmp/control-trace.jsonl）：
//
//	{"type":"control_request","request_id":"<uuid>","request":{
//	    "subtype":"can_use_tool",
//	    "tool_name":"AskUserQuestion",
//	    "input":{...原 tool_use.input...}
//	}}
//
// host 必须用 Session.RespondToControl 回一帧 control_response 才能让 CLI
// 继续推进；超时或不回会让 turn 永远悬挂。
type ControlRequestEvent struct {
	RequestID string
	ToolName  string
	Input     json.RawMessage
}

// PermissionResult 是 control_response.response.response 的 payload。
//
//   - Behavior == "allow"：CLI 用 UpdatedInput 执行该工具。**必填**：CLI 端 Zod
//     schema 强制要求 `updatedInput: record`，即便 host 不想改 input，也必须把
//     原 input 解析为 map 回传。RespondToControl 会在 allow + nil UpdatedInput 时
//     直接返错，防止再次出 Zod 故障。
//   - Behavior == "deny"：CLI 把 Message 作为 tool_result 内容回灌给 LLM。
//
// UpdatedPermissions 是 SDK 原生的 "approve and remember" 通路：host 在 allow
// 响应里 echo 一个 PermissionUpdate，SDK 把规则加进 allow rules，后续匹配的
// can_use_tool 在 CLI 端就提前命中，不再发到 host。详见
// https://code.claude.com/docs/en/agent-sdk/user-input#approve-and-remember
type PermissionResult struct {
	Behavior           string             `json:"behavior"`
	UpdatedInput       map[string]any     `json:"updatedInput,omitempty"`
	UpdatedPermissions []PermissionUpdate `json:"updatedPermissions,omitempty"`
	Message            string             `json:"message,omitempty"`
}

// PermissionUpdate 对齐 Python SDK 的 PermissionUpdate.to_dict() 形状。
// 详见 anthropics/claude-agent-sdk-python `src/claude_agent_sdk/types.py`。
type PermissionUpdate struct {
	// Type: "addRules" | "replaceRules" | "removeRules" | "setMode" |
	// "addDirectories" | "removeDirectories"
	Type        string           `json:"type"`
	Rules       []PermissionRule `json:"rules,omitempty"`
	Behavior    string           `json:"behavior,omitempty"` // "allow" | "deny" | "ask"
	Mode        string           `json:"mode,omitempty"`
	Directories []string         `json:"directories,omitempty"`
	// Destination: "userSettings" | "projectSettings" | "localSettings" | "session"
	Destination string `json:"destination,omitempty"`
}

// PermissionRule 对应 PermissionRuleValue：注意 JSON 字段是驼峰。
type PermissionRule struct {
	ToolName    string `json:"toolName"`
	RuleContent string `json:"ruleContent,omitempty"`
}

type incomingControlRequestFrame struct {
	RequestID string `json:"request_id"`
	Request   struct {
		Subtype  string          `json:"subtype"`
		ToolName string          `json:"tool_name"`
		Input    json.RawMessage `json:"input"`
	} `json:"request"`
}

// parseControlRequest 拆 control_request 帧；仅 can_use_tool subtype 关心，
// 其它 subtype（包括偶发的 interrupt 回环）返回 (nil, false) 由 caller 忽略。
func parseControlRequest(line []byte) (*ControlRequestEvent, bool) {
	var f incomingControlRequestFrame
	if err := json.Unmarshal(line, &f); err != nil || f.RequestID == "" {
		return nil, false
	}
	if f.Request.Subtype != "can_use_tool" {
		return nil, false
	}
	return &ControlRequestEvent{
		RequestID: f.RequestID,
		ToolName:  f.Request.ToolName,
		Input:     f.Request.Input,
	}, true
}

// RespondToControl 在 Session 上写一帧 control_response 回 Claude，
// 是 control_request{subtype:"can_use_tool"} 的配对响应。
//
// 复用 stdinMu 与 Turn / Interrupt 串行化（不会跨帧持锁）。Close 之后调用返错。
// reqID 必须与上行 ControlRequestEvent.RequestID 完全一致，CLI 才能匹配。
func (s *Session) RespondToControl(ctx context.Context, reqID string, result PermissionResult) error {
	if reqID == "" {
		return errors.New("claudecode: empty request_id for control_response")
	}
	if result.Behavior != "allow" && result.Behavior != "deny" {
		return fmt.Errorf("claudecode: invalid PermissionResult.Behavior %q (want allow|deny)", result.Behavior)
	}
	if result.Behavior == "allow" && result.UpdatedInput == nil {
		return errors.New("claudecode: allow response requires non-nil UpdatedInput (CLI Zod schema)")
	}

	s.stdinMu.Lock()
	defer s.stdinMu.Unlock()
	if s.closed {
		return errors.New("claudecode: session closed")
	}
	envelope := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": reqID,
			"response":   result,
		},
	}
	enc, mErr := json.Marshal(envelope)
	if mErr != nil {
		return mErr
	}
	if _, err := fmt.Fprintf(s.proc.stdin, "%s\n", enc); err != nil {
		return err
	}
	return nil
}
