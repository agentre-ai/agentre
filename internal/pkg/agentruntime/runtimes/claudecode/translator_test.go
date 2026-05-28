package claudecode

import (
	"encoding/json"
	"testing"

	"github.com/cago-frame/agents/provider"
	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/canonical"
	"agentre/pkg/claudecode"
)

// TestTranslate_TextThinkingDelta 基本 emit 类型映射。
func TestTranslate_TextThinkingDelta(t *testing.T) {
	Convey("EventTextDelta → TextDelta", t, func() {
		out, _, _ := translate(claudecode.Event{Kind: claudecode.EventTextDelta, Text: "hi"})
		So(len(out), ShouldEqual, 1)
		td, ok := out[0].(agentruntime.TextDelta)
		So(ok, ShouldBeTrue)
		So(td.Text, ShouldEqual, "hi")
	})

	Convey("EventThinkingDelta → ThinkingDelta", t, func() {
		out, _, _ := translate(claudecode.Event{Kind: claudecode.EventThinkingDelta, Text: "think"})
		So(len(out), ShouldEqual, 1)
		_, ok := out[0].(agentruntime.ThinkingDelta)
		So(ok, ShouldBeTrue)
	})
}

// TestTranslate_AskUserQuestionFiltered AskUserQuestion 的 PreToolUse / PostToolUse
// 都过滤,wire 上仅经过 control_request 路径 emit UserAskRequest/Resolved。
// Part 0 §1.1 把这一规则归到 spec;Plan A 在新 subpackage 仍守住。
func TestTranslate_AskUserQuestionFiltered(t *testing.T) {
	Convey("PreToolUse + AskUserQuestion 不入流", t, func() {
		out, _, _ := translate(claudecode.Event{
			Kind: claudecode.EventPreToolUse,
			Tool: &claudecode.ToolEvent{ID: "tu", Name: "AskUserQuestion"},
		})
		So(out, ShouldBeNil)

		// snake_case alias 同样过滤
		out, _, _ = translate(claudecode.Event{
			Kind: claudecode.EventPreToolUse,
			Tool: &claudecode.ToolEvent{ID: "tu", Name: "ask_user_question"},
		})
		So(out, ShouldBeNil)
	})

	Convey("PostToolUse + AskUserQuestion 不入流", t, func() {
		out, _, _ := translate(claudecode.Event{
			Kind: claudecode.EventPostToolUse,
			Tool: &claudecode.ToolEvent{ID: "tu", Name: "AskUserQuestion", Response: "anything"},
		})
		So(out, ShouldBeNil)
	})
}

// TestRecognizeCanonical_FileWrite Write 工具→canonical.FileWrite,含 lines/bytes
// 计算与 64KB 截断逻辑。
func TestRecognizeCanonical_FileWrite(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantPath    string
		wantContent string
		wantLines   int
		wantBytes   int
		wantTrunc   bool
	}{
		{
			name:        "正常内容,末尾有换行",
			input:       `{"file_path":"/x.go","content":"line1\nline2\n"}`,
			wantPath:    "/x.go",
			wantContent: "line1\nline2\n",
			wantLines:   2,
			wantBytes:   12,
			wantTrunc:   false,
		},
		{
			name:        "末尾无换行",
			input:       `{"file_path":"/x.go","content":"only"}`,
			wantPath:    "/x.go",
			wantContent: "only",
			wantLines:   1,
			wantBytes:   4,
			wantTrunc:   false,
		},
		{
			name:        "空内容",
			input:       `{"file_path":"/x.go","content":""}`,
			wantPath:    "/x.go",
			wantContent: "",
			wantLines:   0,
			wantBytes:   0,
			wantTrunc:   false,
		},
	}
	for _, c := range cases {
		Convey("FileWrite "+c.name, t, func() {
			r := recognizeCanonical("Write", json.RawMessage(c.input))
			fw, ok := r.(canonical.FileWrite)
			So(ok, ShouldBeTrue)
			So(fw.Path, ShouldEqual, c.wantPath)
			So(fw.Content, ShouldEqual, c.wantContent)
			So(fw.Lines, ShouldEqual, c.wantLines)
			So(fw.Bytes, ShouldEqual, c.wantBytes)
			So(fw.Truncated, ShouldEqual, c.wantTrunc)
		})
	}

	Convey("超 64KB 截断", t, func() {
		big := make([]byte, writeContentByteCap+10)
		for i := range big {
			big[i] = 'x'
		}
		raw, _ := json.Marshal(map[string]any{"file_path": "/big.go", "content": string(big)})
		r := recognizeCanonical("Write", raw)
		fw, ok := r.(canonical.FileWrite)
		So(ok, ShouldBeTrue)
		So(fw.Truncated, ShouldBeTrue)
		So(len(fw.Content), ShouldEqual, writeContentByteCap)
		So(fw.Bytes, ShouldEqual, writeContentByteCap+10) // 原始字节数,不是截断后
	})

	Convey("非 string content → nil", t, func() {
		raw := json.RawMessage(`{"file_path":"/x","content":123}`)
		So(recognizeCanonical("Write", raw), ShouldBeNil)
	})

	Convey("非 Write 工具不识别", t, func() {
		raw := json.RawMessage(`{"file_path":"/x","content":"hi"}`)
		So(recognizeCanonical("OtherTool", raw), ShouldBeNil)
	})
}

// TestRecognizeCanonical_FileEdit Edit 工具→canonical.FileEdit;single patch +
// replace_all 透传。
func TestRecognizeCanonical_FileEdit(t *testing.T) {
	Convey("Edit single hunk", t, func() {
		raw := json.RawMessage(`{"file_path":"/x.go","old_string":"foo","new_string":"bar","replace_all":false}`)
		r := recognizeCanonical("Edit", raw)
		fe, ok := r.(canonical.FileEdit)
		So(ok, ShouldBeTrue)
		So(len(fe.Files), ShouldEqual, 1)
		So(fe.Files[0].Path, ShouldEqual, "/x.go")
		So(fe.Files[0].ReplaceAll, ShouldBeFalse)
		// 至少有一个 hunk
		So(len(fe.Files[0].Hunks), ShouldBeGreaterThan, 0)
	})

	Convey("Edit replace_all 透传到 patch.ReplaceAll", t, func() {
		raw := json.RawMessage(`{"file_path":"/x.go","old_string":"foo","new_string":"bar","replace_all":true}`)
		r := recognizeCanonical("Edit", raw)
		fe := r.(canonical.FileEdit)
		So(fe.Files[0].ReplaceAll, ShouldBeTrue)
	})
}

// TestRecognizeCanonical_MultiEdit MultiEdit 多个 sub-edit 合并到单个 file
// patch 上。
func TestRecognizeCanonical_MultiEdit(t *testing.T) {
	Convey("MultiEdit 两个 sub-edit → 单个 file patch", t, func() {
		raw := json.RawMessage(`{"file_path":"/x.go","edits":[` +
			`{"old_string":"a","new_string":"A"},` +
			`{"old_string":"b","new_string":"B"}` +
			`]}`)
		r := recognizeCanonical("MultiEdit", raw)
		fe, ok := r.(canonical.FileEdit)
		So(ok, ShouldBeTrue)
		So(len(fe.Files), ShouldEqual, 1)
		So(fe.Files[0].Path, ShouldEqual, "/x.go")
		// MultiEdit 永远 ReplaceAll=false
		So(fe.Files[0].ReplaceAll, ShouldBeFalse)
	})

	Convey("MultiEdit 空 edits → nil", t, func() {
		raw := json.RawMessage(`{"file_path":"/x.go","edits":[]}`)
		So(recognizeCanonical("MultiEdit", raw), ShouldBeNil)
	})
}

// TestRecognizeCanonical_TodoWrite TodoWrite → canonical.PlanUpdate,Status 文案
// 透传(claudecode 用 "in_progress",canonical enum 是 "inProgress" —— bridge
// 不做归一化,Plan B translator 负责)。
func TestRecognizeCanonical_TodoWrite(t *testing.T) {
	Convey("TodoWrite todos[] → canonical.PlanUpdate.Steps", t, func() {
		raw := json.RawMessage(`{"todos":[` +
			`{"id":"t1","content":"inspect","status":"completed"},` +
			`{"id":"t2","content":"report","status":"in_progress"}` +
			`]}`)
		r := recognizeCanonical("TodoWrite", raw)
		pu, ok := r.(canonical.PlanUpdate)
		So(ok, ShouldBeTrue)
		So(len(pu.Steps), ShouldEqual, 2)
		So(pu.Steps[0].ID, ShouldEqual, "t1")
		So(pu.Steps[0].Step, ShouldEqual, "inspect")
		So(string(pu.Steps[0].Status), ShouldEqual, "completed")
		So(pu.Steps[1].ID, ShouldEqual, "t2")
		// 透传原 snake_case;canonical enum 文案对齐留给上层
		So(string(pu.Steps[1].Status), ShouldEqual, "in_progress")
	})

	Convey("TodoWrite todos 全是非 map 元素 → nil(没有有效 step)", t, func() {
		raw := json.RawMessage(`{"todos":["bad","data"]}`)
		So(recognizeCanonical("TodoWrite", raw), ShouldBeNil)
	})
}

// TestRecognizeCanonical_AgentSpawn Task 工具→canonical.AgentSpawn,只填静态字段
// (description/subagent_type/prompt);运行时累计态(toolUses/totalTokens/durationMs/
// lastToolName/status)由 SubagentStarted/Progress/Done 经 SubagentStateBlock 维护,
// 前端 AgentSpawnCard 读 toolBlock.subagent overlay 上来。
func TestRecognizeCanonical_AgentSpawn(t *testing.T) {
	Convey("Task 三字段齐全 → canonical.AgentSpawn", t, func() {
		raw := json.RawMessage(`{"description":"review PR","subagent_type":"code-reviewer","prompt":"please review the diff"}`)
		r := recognizeCanonical("Task", raw)
		as, ok := r.(canonical.AgentSpawn)
		So(ok, ShouldBeTrue)
		So(as.TaskDescription, ShouldEqual, "review PR")
		So(as.SubagentType, ShouldEqual, "code-reviewer")
		So(as.Prompt, ShouldEqual, "please review the diff")
		// 运行时字段不在 translator 填,留给 view overlay
		So(as.ToolUses, ShouldEqual, 0)
		So(as.TotalTokens, ShouldEqual, 0)
		So(as.Status, ShouldEqual, "")
	})

	Convey("Task 仅 description+prompt(无显式 subagent_type)仍识别", t, func() {
		raw := json.RawMessage(`{"description":"do thing","prompt":"detail"}`)
		r := recognizeCanonical("Task", raw)
		as, ok := r.(canonical.AgentSpawn)
		So(ok, ShouldBeTrue)
		So(as.TaskDescription, ShouldEqual, "do thing")
		So(as.SubagentType, ShouldEqual, "")
		So(as.Prompt, ShouldEqual, "detail")
	})

	Convey("Task 三字段全空 → nil(走 raw 路径)", t, func() {
		raw := json.RawMessage(`{}`)
		So(recognizeCanonical("Task", raw), ShouldBeNil)
	})

	Convey("工具名 \"Agent\"(新版 claudecode CLI testdata 命名)同样识别", t, func() {
		raw := json.RawMessage(`{"description":"probe","subagent_type":"general-purpose","prompt":"do it"}`)
		r := recognizeCanonical("Agent", raw)
		as, ok := r.(canonical.AgentSpawn)
		So(ok, ShouldBeTrue)
		So(as.TaskDescription, ShouldEqual, "probe")
		So(as.SubagentType, ShouldEqual, "general-purpose")
	})

	Convey("工具名大小写不敏感 (\"task\"/\"agent\"/\"AGENT\")", t, func() {
		raw := json.RawMessage(`{"description":"x"}`)
		So(recognizeCanonical("task", raw), ShouldNotBeNil)
		So(recognizeCanonical("agent", raw), ShouldNotBeNil)
		So(recognizeCanonical("AGENT", raw), ShouldNotBeNil)
	})
}

// TestTranslate_PreToolUse_TaskCanonical 端到端:Task PreToolUse → ToolCall.Canonical
// = canonical.AgentSpawn,且 ParentToolCallID 透传(嵌套派遣场景)。
func TestTranslate_PreToolUse_TaskCanonical(t *testing.T) {
	Convey("Task PreToolUse → ToolCall.Canonical = AgentSpawn", t, func() {
		out, _, _ := translate(claudecode.Event{
			Kind: claudecode.EventPreToolUse,
			Tool: &claudecode.ToolEvent{
				ID:    "tu-task",
				Name:  "Task",
				Input: json.RawMessage(`{"description":"review","subagent_type":"general-purpose","prompt":"check the file"}`),
			},
		})
		So(len(out), ShouldEqual, 1)
		tc, ok := out[0].(agentruntime.ToolCall)
		So(ok, ShouldBeTrue)
		So(tc.Name, ShouldEqual, "Task")
		as, ok := tc.Canonical.(canonical.AgentSpawn)
		So(ok, ShouldBeTrue)
		So(as.TaskDescription, ShouldEqual, "review")
		So(as.SubagentType, ShouldEqual, "general-purpose")
		So(as.Prompt, ShouldEqual, "check the file")
	})
}

// TestRecognizeCanonical_NilGuards 防御 raw input 各种异常输入。
func TestRecognizeCanonical_NilGuards(t *testing.T) {
	Convey("空 raw → nil", t, func() {
		So(recognizeCanonical("Write", nil), ShouldBeNil)
		So(recognizeCanonical("Write", json.RawMessage{}), ShouldBeNil)
	})

	Convey("非 JSON 对象 → nil", t, func() {
		So(recognizeCanonical("Write", json.RawMessage(`not json`)), ShouldBeNil)
		So(recognizeCanonical("Write", json.RawMessage(`[1,2]`)), ShouldBeNil)
	})

	Convey("未识别工具名 → nil(走 raw 路径)", t, func() {
		So(recognizeCanonical("Bash", json.RawMessage(`{"command":"ls"}`)), ShouldBeNil)
		So(recognizeCanonical("Read", json.RawMessage(`{"file_path":"/x"}`)), ShouldBeNil)
	})
}

// TestTranslate_PreToolUse_AttachesCanonical 端到端验证:translator emit ToolCall
// 时 Canonical 字段被赋值(Write 案例)。
func TestTranslate_PreToolUse_AttachesCanonical(t *testing.T) {
	Convey("Write PreToolUse → ToolCall.Canonical = FileWrite", t, func() {
		out, _, _ := translate(claudecode.Event{
			Kind: claudecode.EventPreToolUse,
			Tool: &claudecode.ToolEvent{
				ID:    "tu-1",
				Name:  "Write",
				Input: json.RawMessage(`{"file_path":"/x.go","content":"hi"}`),
			},
		})
		So(len(out), ShouldEqual, 1)
		tc, ok := out[0].(agentruntime.ToolCall)
		So(ok, ShouldBeTrue)
		So(tc.ID, ShouldEqual, "tu-1")
		So(tc.Name, ShouldEqual, "Write")
		fw, ok := tc.Canonical.(canonical.FileWrite)
		So(ok, ShouldBeTrue)
		So(fw.Path, ShouldEqual, "/x.go")
		So(fw.Content, ShouldEqual, "hi")
	})

	Convey("非 canonical 工具(Bash)→ Canonical=nil,走 raw", t, func() {
		out, _, _ := translate(claudecode.Event{
			Kind: claudecode.EventPreToolUse,
			Tool: &claudecode.ToolEvent{
				ID:    "tu-2",
				Name:  "Bash",
				Input: json.RawMessage(`{"command":"ls"}`),
			},
		})
		tc := out[0].(agentruntime.ToolCall)
		So(tc.Canonical, ShouldBeNil)
	})
}

// TestTranslate_PostToolUse_ParentLinkage subagent 内层 tool_result 必须保留
// ParentToolCallID(对应外层 Agent.tool_use_id),前端把子卡归集到父
// SubagentInvocationCard。
func TestTranslate_PostToolUse_ParentLinkage(t *testing.T) {
	Convey("PostToolUse ParentToolUseID 透传到 ToolResult.ParentToolCallID", t, func() {
		out, _, _ := translate(claudecode.Event{
			Kind:            claudecode.EventPostToolUse,
			ParentToolUseID: "agent-outer",
			Tool: &claudecode.ToolEvent{
				ID:       "inner-tu",
				Name:     "Bash",
				Response: "ok",
			},
		})
		So(len(out), ShouldEqual, 1)
		tr, ok := out[0].(agentruntime.ToolResult)
		So(ok, ShouldBeTrue)
		So(tr.ToolCallID, ShouldEqual, "inner-tu")
		So(tr.ParentToolCallID, ShouldEqual, "agent-outer")
		So(tr.IsError, ShouldBeFalse)
	})
}

// TestTranslate_CompactBoundary EventCompactBoundary → agentruntime.CompactBoundary
// 透传完整 metadata (Pre/PostTokens / Trigger / DurationMs),nil Compact 不 panic 且 emit 零值。
func TestTranslate_CompactBoundary(t *testing.T) {
	Convey("EventCompactBoundary with full metadata → CompactBoundary{...}", t, func() {
		out, _, _ := translate(claudecode.Event{
			Kind: claudecode.EventCompactBoundary,
			Compact: &claudecode.CompactEvent{
				PreTokens: 30117, PostTokens: 2697, Trigger: "manual", DurationMs: 20696,
			},
		})
		So(len(out), ShouldEqual, 1)
		cb, ok := out[0].(agentruntime.CompactBoundary)
		So(ok, ShouldBeTrue)
		So(cb.PreTokens, ShouldEqual, 30117)
		So(cb.PostTokens, ShouldEqual, 2697)
		So(cb.Trigger, ShouldEqual, "manual")
		So(cb.DurationMs, ShouldEqual, 20696)
	})

	Convey("EventCompactBoundary nil Compact → CompactBoundary{} 零值", t, func() {
		out, _, _ := translate(claudecode.Event{Kind: claudecode.EventCompactBoundary})
		So(len(out), ShouldEqual, 1)
		cb, ok := out[0].(agentruntime.CompactBoundary)
		So(ok, ShouldBeTrue)
		So(cb.PreTokens, ShouldEqual, 0)
		So(cb.PostTokens, ShouldEqual, 0)
		So(cb.Trigger, ShouldEqual, "")
		So(cb.DurationMs, ShouldEqual, 0)
	})
}

// TestTranslate_RuntimeStatus EventStatus → RuntimeStatus 透传字段。空 Status 不 emit
// (静默忽略,与 PermissionModeChanged 同款守门规则:无信号不要伪造事件)。
func TestTranslate_RuntimeStatus(t *testing.T) {
	Convey("EventStatus compacting → RuntimeStatus{Status:\"compacting\"}", t, func() {
		out, _, _ := translate(claudecode.Event{
			Kind:   claudecode.EventStatus,
			Status: "compacting",
		})
		So(len(out), ShouldEqual, 1)
		rs, ok := out[0].(agentruntime.RuntimeStatus)
		So(ok, ShouldBeTrue)
		So(rs.Status, ShouldEqual, "compacting")
	})

	Convey("EventStatus 空 Status → 不 emit", t, func() {
		out, _, _ := translate(claudecode.Event{Kind: claudecode.EventStatus})
		So(out, ShouldBeNil)
	})
}

// TestTranslate_InitEmitsContextWindowFromCatalog —— claudecode CLI 在 system.init
// 帧告诉我们本轮用的是哪个模型。Claude Code SDK 不直接报上下文窗口大小,需要 translator
// 拿 model 名查 cago llmcatalog 兜底,然后 emit ContextWindowUpdated 把窗口大小推给
// chat_svc 实时刷新前端进度条总量,不必等 EventDone 才知道。
//
// 命中已知模型 → emit ContextWindowUpdated{Tokens: <catalog 窗口大小>}。
func TestTranslate_InitEmitsContextWindowFromCatalog(t *testing.T) {
	Convey("EventInit 已知 model → ContextWindowUpdated{Tokens: catalog window}", t, func() {
		out, _, _ := translate(claudecode.Event{
			Kind:  claudecode.EventInit,
			Model: "claude-sonnet-4-6",
		})
		So(len(out), ShouldEqual, 1)
		cw, ok := out[0].(agentruntime.ContextWindowUpdated)
		So(ok, ShouldBeTrue)
		So(cw.Tokens, ShouldBeGreaterThan, 0)
	})
}

// TestTranslate_InitUnknownModelNoEmit —— catalog miss 时不 emit ContextWindowUpdated
// (Tokens=0 会让前端"显示进度条但分母是 0"更难看)。下游 chat_svc resolveContextWindow*
// 仍会用 provider.ContextWindow / provider.Model 兜底,不依赖本事件。
func TestTranslate_InitUnknownModelNoEmit(t *testing.T) {
	Convey("EventInit 未知 model → 不 emit", t, func() {
		out, _, _ := translate(claudecode.Event{
			Kind:  claudecode.EventInit,
			Model: "totally-unknown-xyz-model",
		})
		So(out, ShouldBeNil)
	})

	Convey("EventInit 空 model → 不 emit (防御:pkg/claudecode 本身已经过滤,这里再守一道)", t, func() {
		out, _, _ := translate(claudecode.Event{Kind: claudecode.EventInit})
		So(out, ShouldBeNil)
	})
}

// TestTranslate_Usage_PerCallAnthropicFamily TotalInputTokens 按 Anthropic family
// 聚合:prompt + cached + cacheCreation。event.go:109 documented contract,
// 前端不再做家族数学。
func TestTranslate_Usage_PerCallAnthropicFamily(t *testing.T) {
	Convey("EventUsage → UsageUpdate.TotalInputTokens 按 Anthropic family 聚合", t, func() {
		out, _, _ := translate(claudecode.Event{
			Kind: claudecode.EventUsage,
			Usage: provider.Usage{
				PromptTokens:        1000,
				CachedTokens:        500,
				CacheCreationTokens: 200,
				CompletionTokens:    100,
			},
		})
		So(len(out), ShouldEqual, 1)
		uu, ok := out[0].(agentruntime.UsageUpdate)
		So(ok, ShouldBeTrue)
		So(uu.TotalInputTokens, ShouldEqual, 1700) // 1000 + 500 + 200
		So(uu.Usage.PromptTokens, ShouldEqual, 1000)
	})
}
