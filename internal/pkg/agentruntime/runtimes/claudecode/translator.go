package claudecode

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cago-frame/agents/provider"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/canonical"
	"github.com/agentre-ai/agentre/internal/pkg/diff"
	"github.com/agentre-ai/agentre/internal/pkg/llmcatalog"
	"github.com/agentre-ai/agentre/pkg/claudecode"
)

// writeContentByteCap canonical.FileWrite 内容字节上限。
const writeContentByteCap = 64 * 1024

// translate 把单帧 claudecode.Event 翻译成 0/1/n 个 sealed agentruntime.Event。
//
// emit canonical 识别:
//   - Write 工具 → ToolCall.Canonical = canonical.FileWrite
//   - Edit / MultiEdit → ToolCall.Canonical = canonical.FileEdit(走 diff.FromEdit/FromMultiEdit
//     得到 diff.Payload,再降级到 canonical 表)
//   - TodoWrite → ToolCall.Canonical = canonical.PlanUpdate
//   - AskUserQuestion / ExitPlanMode 仍走 control_request 路径,
//     此处保留 isAskUserQuestionToolName 过滤
//   - Task → ToolCall.Canonical = canonical.AgentSpawn(只填 description/
//     subagent_type/prompt 静态字段;运行时累计态由 SubagentStarted/Progress/
//     Done 经 SubagentStateBlock 维护,前端 AgentSpawnCard 读 toolBlock.subagent
//     overlay)
//
// usage / stopErr 与旧 translator 同步:EventDone 时填 usage;EventError 时填
// stopErr。
func translate(ev claudecode.Event) (events []agentruntime.Event, usage *provider.Usage, stopErr error) {
	switch ev.Kind {
	case claudecode.EventTextDelta:
		events = append(events, agentruntime.TextDelta{Text: ev.Text})
	case claudecode.EventThinkingDelta:
		events = append(events, agentruntime.ThinkingDelta{Text: ev.Text})
	case claudecode.EventPreToolUse:
		// AskUserQuestion 走独立的 control_request 路径 emit UserAskRequest,
		// PreToolUse 这条会被前端再渲染成通用 ToolInvocationCard 重复一遍。
		if ev.Tool != nil && !isAskUserQuestionToolName(ev.Tool.Name) {
			tc := agentruntime.ToolCall{
				ID:               ev.Tool.ID,
				Name:             ev.Tool.Name,
				Input:            ev.Tool.Input,
				ParentToolCallID: ev.ParentToolUseID,
			}
			tc.Canonical = recognizeCanonical(ev.Tool.Name, ev.Tool.Input)
			events = append(events, tc)
		}
	case claudecode.EventPostToolUse:
		// 同 PreToolUse:AskUserQuestion 的 tool_result 由 UserAskRequest/Resolved
		// 承载,这条 PostToolUse 不入流避免重复卡片。
		if ev.Tool != nil && !isAskUserQuestionToolName(ev.Tool.Name) {
			isErr := ev.Tool.Err != nil
			events = append(events, agentruntime.ToolResult{
				ToolCallID:       ev.Tool.ID,
				Content:          string(ev.Tool.Response),
				IsError:          isErr,
				ParentToolCallID: ev.ParentToolUseID,
				Meta:             append([]byte(nil), ev.Tool.ResultMeta...),
			})
		}
	case claudecode.EventTaskStarted, claudecode.EventTaskProgress, claudecode.EventTaskNotification:
		if ev.Tool != nil && ev.Tool.Subagent != nil {
			info := subagentInfoFromMeta(ev.Tool.Subagent)
			switch ev.Kind {
			case claudecode.EventTaskStarted:
				events = append(events, agentruntime.SubagentStarted{ToolCallID: ev.Tool.ID, Info: info})
			case claudecode.EventTaskProgress:
				events = append(events, agentruntime.SubagentProgress{ToolCallID: ev.Tool.ID, Info: info})
			case claudecode.EventTaskNotification:
				events = append(events, agentruntime.SubagentDone{ToolCallID: ev.Tool.ID, Info: info})
			}
		}
	case claudecode.EventError:
		if ev.Err != nil {
			events = append(events, agentruntime.ErrorEvent{Err: ev.Err})
			stopErr = ev.Err
		}
	case claudecode.EventRetry:
		// system.api_retry 帧:把 CLI 的结构化字段渲染成两路同形的 Retry,前端
		// RetryNoticeCard 零分支。
		if ev.Retry != nil {
			events = append(events, agentruntime.Retry{
				Message: formatRetryMessage(ev.Retry),
				Details: formatRetryDetails(ev.Retry),
				Attempt: ev.Retry.Attempt,
				Max:     ev.Retry.MaxAttempts,
			})
		}
	case claudecode.EventPermissionModeChanged:
		if ev.PermissionMode != "" {
			events = append(events, agentruntime.PermissionModeChanged{Mode: ev.PermissionMode})
		}
	case claudecode.EventCompactBoundary:
		// system.compact_boundary 帧:CLI 内部已完成上下文压缩,LLM 只看得到摘要。
		// 透传 metadata 给 chat_svc,由它落 system message + 通知前端折叠旧消息。
		// Compact 帧理论上一定带 CompactEvent(parseSystemTask 会初始化),nil 保护
		// 兼容老解析路径。
		var info agentruntime.CompactBoundary
		if ev.Compact != nil {
			info.PreTokens = ev.Compact.PreTokens
			info.PostTokens = ev.Compact.PostTokens
			info.Trigger = ev.Compact.Trigger
			info.DurationMs = ev.Compact.DurationMs
		}
		events = append(events, info)
	case claudecode.EventStatus:
		// system{subtype:"status",status:<非空>} 帧:CLI 通报运行状态过渡 (compacting 等)。
		// 空 Status 不 emit —— 静默忽略与 EventPermissionModeChanged 同款守门规则。
		// 清理信号由 EventCompactBoundary / EventDone / EventError 路径传达。
		if ev.Status != "" {
			events = append(events, agentruntime.RuntimeStatus{Status: ev.Status})
		}
	case claudecode.EventInit:
		// system.init 帧带 model:Claude Code SDK 协议本身不报上下文窗口大小,
		// 这里查 cago llmcatalog 兜底,emit ContextWindowUpdated 让前端 turn 内
		// 就能看到窗口总量,不必等 EventDone 才显示进度条。catalog miss → 不 emit,
		// chat_svc resolveContextWindow* 仍会用 provider.ContextWindow / provider.Model
		// 兜底,不依赖本事件存在。
		if ev.Model != "" {
			if info, ok := llmcatalog.Lookup(ev.Model); ok && info.ContextWindow > 0 {
				events = append(events, agentruntime.ContextWindowUpdated{Tokens: info.ContextWindow})
			}
		}
	case claudecode.EventUsage:
		// 主 agent 帧的 per-call usage:turn 内每次 API call 边界都推一条,让
		// 上层(chat_svc → 前端 Composer 进度条)阶梯式刷新「已用上下文」。
		// TotalInputTokens 按 Anthropic family 聚合 = prompt + cached + cacheCreation
		// (spec §A token contract;event.go:109 的 UsageUpdate 文档)。
		u := ev.Usage
		events = append(events, agentruntime.UsageUpdate{
			Usage:            &u,
			TotalInputTokens: u.PromptTokens + u.CachedTokens + u.CacheCreationTokens,
		})
	case claudecode.EventDone:
		u := ev.Usage
		usage = &u
	}
	return
}

// recognizeCanonical 按工具名 + raw input JSON 识别已知 canonical 形状。
// 解析失败 / 工具不认识 → 返 nil,表示走 raw tool_use 路径(前端通用 ToolInvocationCard)。
func recognizeCanonical(name string, rawInput json.RawMessage) canonical.CanonicalTool {
	if len(rawInput) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(rawInput, &m); err != nil {
		return nil
	}
	switch name {
	case "Write":
		return recognizeFileWrite(m)
	case "Edit":
		return recognizeFileEdit(m)
	case "MultiEdit":
		return recognizeMultiEdit(m)
	case "TodoWrite":
		return recognizeTodoWrite(m)
	}
	// Task/Agent 走 canonical 包的共享 helper —— live emit 路径(这里)和 replay
	// 路径(canonical.FromToolUse)共用同一份识别,避免两边各搞一套漂移。
	if canonical.IsAgentSpawnToolName(name) {
		if as, ok := canonical.AgentSpawnFromInput(m); ok {
			return as
		}
	}
	return nil
}

func recognizeFileWrite(m map[string]any) canonical.CanonicalTool {
	path, _ := m["file_path"].(string)
	content, ok := m["content"].(string)
	if !ok {
		return nil
	}
	bytes := len(content)
	truncated := false
	if bytes > writeContentByteCap {
		content = content[:writeContentByteCap]
		truncated = true
	}
	lines := 0
	if content != "" {
		lines = strings.Count(content, "\n")
		if !strings.HasSuffix(content, "\n") {
			lines++
		}
	}
	return canonical.FileWrite{
		Path:      path,
		Content:   content,
		Lines:     lines,
		Bytes:     bytes,
		Truncated: truncated,
	}
}

func recognizeFileEdit(m map[string]any) canonical.CanonicalTool {
	payload := diff.FromEdit(m)
	if len(payload.Files) == 0 {
		return nil
	}
	replaceAll, _ := m["replace_all"].(bool)
	patches := canonical.PatchesFromDiff(payload)
	if replaceAll {
		// Edit.replace_all 只对单文件 Edit 有效;MultiEdit 永远 false。
		for i := range patches {
			patches[i].ReplaceAll = true
		}
	}
	return canonical.FileEdit{Files: patches}
}

func recognizeMultiEdit(m map[string]any) canonical.CanonicalTool {
	payload := diff.FromMultiEdit(m)
	if len(payload.Files) == 0 {
		return nil
	}
	// diff.FromMultiEdit 即使 edits 为空也会返单 File(0 hunks)。这种空 patch
	// 走 raw 路径更合适 —— 前端不需要为空 diff 起 DiffCard。
	totalHunks := 0
	for _, f := range payload.Files {
		totalHunks += len(f.Hunks)
	}
	if totalHunks == 0 {
		return nil
	}
	return canonical.FileEdit{Files: canonical.PatchesFromDiff(payload)}
}

func recognizeTodoWrite(m map[string]any) canonical.CanonicalTool {
	todosRaw, _ := m["todos"].([]any)
	if len(todosRaw) == 0 {
		return nil
	}
	steps := make([]canonical.PlanStep, 0, len(todosRaw))
	for _, t := range todosRaw {
		todo, ok := t.(map[string]any)
		if !ok {
			continue
		}
		id, _ := todo["id"].(string)
		content, _ := todo["content"].(string)
		status, _ := todo["status"].(string)
		steps = append(steps, canonical.PlanStep{
			ID:     id,
			Step:   content,
			Status: canonical.PlanStepStatus(status),
		})
	}
	if len(steps) == 0 {
		return nil
	}
	return canonical.PlanUpdate{Steps: steps}
}

// isAskUserQuestionToolName 识别 AskUserQuestion 工具名(snake/Pascal 双写)。
func isAskUserQuestionToolName(name string) bool {
	return name == "AskUserQuestion" || name == "ask_user_question"
}

// subagentInfoFromMeta 镜像顶层 claudecode.go 同名函数;返值类型从指针改为
// 值(SubagentStarted/Progress/Done 的 Info 字段直接是值)。nil 入参产零值
// SubagentInfo,留给下游做差量合并。
func subagentInfoFromMeta(m *claudecode.SubagentMeta) agentruntime.SubagentInfo {
	if m == nil {
		return agentruntime.SubagentInfo{}
	}
	return agentruntime.SubagentInfo{
		TaskID:          m.TaskID,
		SubagentType:    m.SubagentType,
		Kind:            m.TaskType,
		TaskDescription: m.TaskDescription,
		Prompt:          m.Prompt,
		LastToolName:    m.LastToolName,
		ToolUses:        m.ToolUses,
		TotalTokens:     m.TotalTokens,
		DurationMs:      m.DurationMs,
		Status:          m.Status,
	}
}

// formatRetryMessage 镜像顶层 claudecode.go formatClaudeRetryMessage。
func formatRetryMessage(r *claudecode.RetryEvent) string {
	switch {
	case r.ErrorStatus > 0 && r.ErrorCode != "":
		return fmt.Sprintf("HTTP %d %s", r.ErrorStatus, r.ErrorCode)
	case r.ErrorStatus > 0:
		return fmt.Sprintf("HTTP %d", r.ErrorStatus)
	default:
		return r.ErrorCode
	}
}

// formatRetryDetails 镜像顶层 claudecode.go formatClaudeRetryDetails。
func formatRetryDetails(r *claudecode.RetryEvent) string {
	if r.DelayMs <= 0 {
		return ""
	}
	if r.DelayMs < 1000 {
		return fmt.Sprintf("≈%.0fms 后重试", r.DelayMs)
	}
	return fmt.Sprintf("≈%.1fs 后重试", r.DelayMs/1000)
}
