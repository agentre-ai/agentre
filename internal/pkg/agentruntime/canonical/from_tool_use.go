package canonical

import (
	"strings"

	"github.com/agentre-ai/agentre/internal/pkg/diff"
)

// WriteContentByteCap 是 FileWrite.Content 字节上限。超过后截断并标 Truncated=true,
// 避免 GB 级文件撑爆 Wails event 序列化。各 runtime translator + 重放路径共用。
const WriteContentByteCap = 64 * 1024

// FromToolUse 把 tool_use block 的 (toolName, input) 翻译成 canonical 表示。
// 用于 chat_svc 重放路径(LoadSession 时从持久化的 tool_use 实体重建 canonical);
// runtime translator 的 live 路径直接构造,不走这条。
//
// 命中:返回 (CanonicalTool, true)。未命中(普通工具如 Bash / Read):(nil, false)。
func FromToolUse(toolName string, input map[string]any) (CanonicalTool, bool) {
	switch toolName {
	case "Write":
		if fw, ok := fileWriteFromWriteInput(input); ok {
			return fw, true
		}
	case "Edit":
		payload := diff.FromEdit(input)
		if len(payload.Files) == 0 {
			return nil, false
		}
		replaceAll, _ := input["replace_all"].(bool)
		patches := PatchesFromDiff(payload)
		if replaceAll {
			for i := range patches {
				patches[i].ReplaceAll = true
			}
		}
		return FileEdit{Files: patches}, true
	case "MultiEdit":
		payload := diff.FromMultiEdit(input)
		if len(payload.Files) == 0 {
			return nil, false
		}
		totalHunks := 0
		for _, f := range payload.Files {
			totalHunks += len(f.Hunks)
		}
		if totalHunks == 0 {
			return nil, false
		}
		return FileEdit{Files: PatchesFromDiff(payload)}, true
	case "file_change":
		payload, ok := diff.FromFileChange(input)
		if !ok || len(payload.Files) == 0 {
			return nil, false
		}
		return FileEdit{Files: PatchesFromDiff(payload)}, true
	case "update_plan":
		if pu, ok := planUpdateFromUpdatePlanInput(input); ok {
			return pu, true
		}
	}
	if IsAgentSpawnToolName(toolName) {
		if as, ok := AgentSpawnFromInput(input); ok {
			return as, true
		}
	}
	return nil, false
}

// IsAgentSpawnToolName 不同 claudecode CLI 版本对 subagent 派遣工具有两种命名:
// 旧版叫 "Task",新版(pkg/claudecode/testdata/stream_subagent.jsonl)叫 "Agent"。
// 大小写不敏感双名匹配镜像 main 分支前端 SUBAGENT_TOOL_NAMES = {"agent","task"} 行为。
func IsAgentSpawnToolName(name string) bool {
	switch strings.ToLower(name) {
	case "task", "agent":
		return true
	}
	return false
}

// AgentSpawnFromInput 从 Task/Agent 工具 raw input 提取 AgentSpawn 静态字段
// (description/subagent_type/prompt);运行时累计态由 SubagentStarted/Progress/Done
// 经 SubagentStateBlock 维护,不在这里填。三字段全空返 (zero, false)。
func AgentSpawnFromInput(input map[string]any) (AgentSpawn, bool) {
	description, _ := input["description"].(string)
	subagentType, _ := input["subagent_type"].(string)
	prompt, _ := input["prompt"].(string)
	if description == "" && subagentType == "" && prompt == "" {
		return AgentSpawn{}, false
	}
	return AgentSpawn{
		TaskDescription: description,
		SubagentType:    subagentType,
		Prompt:          prompt,
	}, true
}

func planUpdateFromUpdatePlanInput(input map[string]any) (PlanUpdate, bool) {
	rawPlan, ok := input["plan"].([]any)
	if !ok || len(rawPlan) == 0 {
		return PlanUpdate{}, false
	}
	steps := make([]PlanStep, 0, len(rawPlan))
	for _, raw := range rawPlan {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		step, _ := m["step"].(string)
		if strings.TrimSpace(step) == "" {
			continue
		}
		status, _ := m["status"].(string)
		steps = append(steps, PlanStep{
			Step:   step,
			Status: normalizePlanStepStatus(status),
		})
	}
	if len(steps) == 0 {
		return PlanUpdate{}, false
	}
	return PlanUpdate{Steps: steps}, true
}

func normalizePlanStepStatus(status string) PlanStepStatus {
	const legacyCancelledStatus = "cancelled" //nolint:misspell // Accept legacy/British spelling from older tool payloads.

	switch status {
	case string(StepInProgress), "in_progress":
		return StepInProgress
	case string(StepCompleted), "complete":
		return StepCompleted
	case string(StepCancelled), legacyCancelledStatus:
		return StepCancelled
	default:
		return StepPending
	}
}

func fileWriteFromWriteInput(input map[string]any) (FileWrite, bool) {
	path, _ := input["file_path"].(string)
	content, ok := input["content"].(string)
	if !ok {
		return FileWrite{}, false
	}
	bytes := len(content)
	truncated := false
	if bytes > WriteContentByteCap {
		content = content[:WriteContentByteCap]
		truncated = true
	}
	lines := 0
	if content != "" {
		lines = strings.Count(content, "\n")
		if !strings.HasSuffix(content, "\n") {
			lines++
		}
	}
	return FileWrite{
		Path:      path,
		Content:   content,
		Lines:     lines,
		Bytes:     bytes,
		Truncated: truncated,
	}, true
}

// PatchesFromDiff 把 diff.Payload 降级到 canonical.FileEditPatch 列表。
// 字段一一对应(diff.Op 与 canonical.DiffOp 同字符串值;diff.Kind 与
// canonical.FileChangeKind 同字符串值)。runtime translator + 重放路径共用。
func PatchesFromDiff(p diff.Payload) []FileEditPatch {
	out := make([]FileEditPatch, 0, len(p.Files))
	for _, f := range p.Files {
		patch := FileEditPatch{
			Path:       f.Path,
			Kind:       FileChangeKind(string(f.Kind)),
			Plus:       f.Plus,
			Minus:      f.Minus,
			Truncated:  f.Truncated,
			ReplaceAll: f.ReplaceAll,
		}
		patch.Hunks = make([]DiffHunk, 0, len(f.Hunks))
		for _, h := range f.Hunks {
			ch := DiffHunk{
				OldStart: h.OldStart,
				OldLines: h.OldLines,
				NewStart: h.NewStart,
				NewLines: h.NewLines,
				Header:   h.Header,
			}
			ch.Lines = make([]DiffLine, 0, len(h.Lines))
			for _, ln := range h.Lines {
				ch.Lines = append(ch.Lines, DiffLine{
					Op:   DiffOp(string(ln.Op)),
					Old:  ln.Old,
					New:  ln.New,
					Text: ln.Text,
				})
			}
			patch.Hunks = append(patch.Hunks, ch)
		}
		out = append(out, patch)
	}
	return out
}
