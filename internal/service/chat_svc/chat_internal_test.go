package chat_svc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/utils/httputils"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"

	daemonrpc "github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/project_location_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/project_location_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_location_repo/mock_project_location_repo"
	chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc/mock_remote_device_svc"
)

func TestToChatMessage_BlockTypes(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		blocks.TextBlock{Text: "hello"},
		blocks.ThinkingBlock{Text: "let me think"},
		blocks.ToolUseBlock{ID: "toolu_1", Name: "shell", Input: map[string]any{"cmd": "ls"}},
		blocks.ToolResultBlock{ToolUseID: "toolu_1", Content: []blocks.ContentBlock{blocks.TextBlock{Text: "file.txt"}}},
		PlanBlock{Text: "Plan\n- [x] Inspect files"},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 5)

	assert.Equal(t, "text", cm.Blocks[0].Type)
	assert.Equal(t, "hello", cm.Blocks[0].Text)

	assert.Equal(t, "thinking", cm.Blocks[1].Type)
	assert.Equal(t, "let me think", cm.Blocks[1].Text)

	assert.Equal(t, "tool_use", cm.Blocks[2].Type)
	assert.Equal(t, "toolu_1", cm.Blocks[2].ToolUseID)
	assert.Equal(t, "shell", cm.Blocks[2].ToolName)
	assert.Equal(t, "ls", cm.Blocks[2].ToolInput["cmd"])

	assert.Equal(t, "tool_result", cm.Blocks[3].Type)
	assert.Equal(t, "toolu_1", cm.Blocks[3].ToolUseID)
	assert.Equal(t, "file.txt", cm.Blocks[3].Text)
	assert.False(t, cm.Blocks[3].IsError)

	assert.Equal(t, "plan", cm.Blocks[4].Type)
	assert.Contains(t, cm.Blocks[4].Text, "Inspect files")
}

// TestToChatMessage_ToolApprovalBlock 验证 ToolApprovalBlock 经 toChatMessage 投影成
// type="tool_approval" + ToolApproval 字段保真(含 ToolKey)。
func TestToChatMessage_ToolApprovalBlock(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		chatblocks.ToolApprovalBlock{
			ToolKey:   "org",
			RequestID: "org-req-42",
			ToolName:  "org_invite",
			ToolInput: map[string]any{"user_id": "u-99"},
			Status:    "pending",
		},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 1)
	assert.Equal(t, "tool_approval", cm.Blocks[0].Type)
	require.NotNil(t, cm.Blocks[0].ToolApproval)
	assert.Equal(t, "org", cm.Blocks[0].ToolApproval.ToolKey)
	assert.Equal(t, "org-req-42", cm.Blocks[0].ToolApproval.RequestID)
	assert.Equal(t, "org_invite", cm.Blocks[0].ToolApproval.ToolName)
	assert.Equal(t, "u-99", cm.Blocks[0].ToolApproval.ToolInput["user_id"])
	assert.Equal(t, "pending", cm.Blocks[0].ToolApproval.Status)
}

// 历史:ToolResultMetaBlock 已整删,meta 字段改走 raw tool_result.Meta 字节透传
// (StreamToolResult 事件的 toolResultMeta 字段),不再独立 block;原先的
// TestToChatMessage_ToolResultWithMeta / OrphanToolResultMetaIsDropped 一并移除。

func TestToChatMessage_TokenFields(t *testing.T) {
	m := &chat_entity.Message{
		ID: 1, SessionID: 9, Role: "assistant", BlocksJSON: "[]",
		Model:               "claude-sonnet-4-6",
		PromptTokens:        100,
		CompletionTokens:    50,
		CachedTokens:        30,
		CacheCreationTokens: 20,
		ReasoningTokens:     10,
		DurationMs:          1234,
	}
	cm, err := toChatMessage(m)
	require.NoError(t, err)
	assert.Equal(t, 100, cm.PromptTokens)
	assert.Equal(t, 50, cm.CompletionTokens)
	assert.Equal(t, 30, cm.CachedTokens)
	assert.Equal(t, 20, cm.CacheCreationTokens)
	assert.Equal(t, 10, cm.ReasoningTokens)
	assert.Equal(t, 1234, cm.DurationMs)
}

// TestToChatMessage_NestedToolUse pins replay 把 subagent 内层 ToolUse 投影成
// type=tool_use + ParentToolCallID(json: parentToolUseId)。前端 chat.tsx
// collectChildren 据此把它从主流程移走、挂到外层 AgentSpawnCard.childBlocks。
// canonical 故意不算 —— 内层是被父 agent.spawn 包住的 step。
func TestToChatMessage_NestedToolUse(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		chatblocks.NestedToolUseBlock{
			ID:               "nested-1",
			Name:             "Read",
			Input:            map[string]any{"file_path": "/x.go"},
			ParentToolCallID: "task-outer-1",
			SubagentRunID:    "run-1",
		},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 1)
	assert.Equal(t, "tool_use", cm.Blocks[0].Type)
	assert.Equal(t, "nested-1", cm.Blocks[0].ToolUseID)
	assert.Equal(t, "Read", cm.Blocks[0].ToolName)
	assert.Equal(t, "/x.go", cm.Blocks[0].ToolInput["file_path"])
	assert.Equal(t, "task-outer-1", cm.Blocks[0].ParentToolCallID)
	assert.Equal(t, "run-1", cm.Blocks[0].SubagentRunID)
	assert.Nil(t, cm.Blocks[0].Canonical, "内层 step 不走 canonical 路由,由父 agent.spawn 接管")
}

// TestToChatMessage_NestedToolResult 同上,镜像 NestedToolResultBlock 路径:
// ToolUseID = ToolCallID、Content 拍平进 Text、ParentToolCallID 透传。
func TestToChatMessage_NestedToolResult(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		chatblocks.NestedToolResultBlock{
			ToolCallID:       "nested-1",
			Content:          "hello\n",
			IsError:          true,
			ParentToolCallID: "task-outer-1",
			SubagentRunID:    "run-1",
		},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 1)
	assert.Equal(t, "tool_result", cm.Blocks[0].Type)
	assert.Equal(t, "nested-1", cm.Blocks[0].ToolUseID)
	assert.Equal(t, "hello\n", cm.Blocks[0].Text)
	assert.True(t, cm.Blocks[0].IsError)
	assert.Equal(t, "task-outer-1", cm.Blocks[0].ParentToolCallID)
	assert.Equal(t, "run-1", cm.Blocks[0].SubagentRunID)
}

// TestToChatMessage_SkipsSubagentStateAndPermissionModeChange pins 两个无 UI 元素
// 的 ToUI block 在 replay 时被 skip(不下行成 type=unknown 让前端渲染 debug 卡)。
// SubagentStateBlock 是累计态(tokens/duration/status),前端 AgentSpawnCard
// 由外层 Task tool 的 canonical.agentSpawn 读 —— live 路径靠 dispatcher_emitter
// 注入,replay 不重算。PermissionModeChangeBlock 是审计 block。
func TestToChatMessage_SkipsSubagentStateAndPermissionModeChange(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		blocks.TextBlock{Text: "before"},
		chatblocks.SubagentStateBlock{ParentToolCallID: "outer", TotalTokens: 123, Status: "completed"},
		chatblocks.PermissionModeChangeBlock{From: "default", To: "plan", At: 1000},
		blocks.TextBlock{Text: "after"},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 2, "skip 后只剩 2 条 text,不能出现 type=unknown 兜底卡")
	assert.Equal(t, "text", cm.Blocks[0].Type)
	assert.Equal(t, "before", cm.Blocks[0].Text)
	assert.Equal(t, "text", cm.Blocks[1].Type)
	assert.Equal(t, "after", cm.Blocks[1].Text)
}

// TestToChatMessage_SubagentStateMergedOntoToolUseBlock 回归后台任务跨轮/重载后
// 不可见的问题:SubagentStateBlock 的元数据必须附到匹配的 tool_use ChatBlock 的
// .Subagent 字段上,不能再作为独立 block 下行前端(否则出 debug 卡)。
func TestToChatMessage_SubagentStateMergedOntoToolUseBlock(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		blocks.ToolUseBlock{ID: "tu1", Name: "Bash", Input: map[string]any{"command": "sleep 20"}},
		chatblocks.SubagentStateBlock{
			ParentToolCallID: "tu1",
			Kind:             "local_bash",
			Description:      "sleep 20",
			Status:           "running",
			TaskID:           "task-abc",
			TotalTokens:      100,
			DurationMs:       500,
			LastToolName:     "computer",
			ToolUses:         3,
			Model:            "claude-haiku-4-5-20251001",
		},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	// 只有 1 个 block(tool_use),不能有独立的 subagent_state 或 unknown 卡。
	require.Len(t, cm.Blocks, 1, "SubagentStateBlock 不能作为独立 block 下行,只能合入 tool_use")

	tb := cm.Blocks[0]
	assert.Equal(t, "tool_use", tb.Type)
	assert.Equal(t, "tu1", tb.ToolUseID)

	require.NotNil(t, tb.Subagent, "tool_use 块必须携带 .Subagent 元数据")
	assert.Equal(t, "local_bash", tb.Subagent.Kind)
	assert.Equal(t, "sleep 20", tb.Subagent.TaskDescription)
	assert.Equal(t, "running", tb.Subagent.Status)
	assert.Equal(t, "task-abc", tb.Subagent.TaskID)
	assert.Equal(t, 100, tb.Subagent.TotalTokens)
	assert.Equal(t, 500, tb.Subagent.DurationMs)
	assert.Equal(t, "computer", tb.Subagent.LastToolName)
	assert.Equal(t, 3, tb.Subagent.ToolUses)
	// R6:replay 路径下模型须与流式期间一致 —— 随 SubagentStateBlock.Model 一起投影。
	assert.Equal(t, "claude-haiku-4-5-20251001", tb.Subagent.Model)
}

// TestToChatMessage_SubagentStateWithNoMatchingToolUse 无匹配 tool_use 时
// SubagentStateBlock 仍然被 skip(不产生独立 block)。
func TestToChatMessage_SubagentStateWithNoMatchingToolUse(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		blocks.TextBlock{Text: "hello"},
		chatblocks.SubagentStateBlock{
			ParentToolCallID: "no-match",
			Kind:             "local_bash",
			Status:           "completed",
		},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 1, "无匹配 tool_use 时 SubagentStateBlock 仍 skip")
	assert.Equal(t, "text", cm.Blocks[0].Type)
}

func TestToChatMessage_NormalizedPiReplayPreservesGrouping(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	runs := []agentruntime.SubagentRun{
		{ID: "run-0", Index: 0, Agent: "scout", Task: "inspect", RequestedModel: "small", Model: "observed", Status: "completed", Summary: "done"},
		{ID: "run-1", Index: 1, Agent: "worker", Task: "test", Status: "running", LastToolName: "bash"},
	}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		blocks.ToolUseBlock{ID: "outer", Name: "Vendor__SubAgent", Input: map[string]any{"tasks": []any{}}},
		chatblocks.SubagentStateBlock{ParentToolCallID: "outer", Mode: "parallel", Runs: runs, Status: "running"},
		chatblocks.NestedToolUseBlock{ID: "child-0", Name: "Read", ParentToolCallID: "outer", SubagentRunID: "run-0"},
		chatblocks.NestedToolUseBlock{ID: "child-unknown", Name: "Bash", ParentToolCallID: "outer"},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 3)
	outer := cm.Blocks[0]
	require.NotNil(t, outer.Subagent)
	assert.Equal(t, "parallel", outer.Subagent.Mode)
	assert.Equal(t, runs, outer.Subagent.Runs)
	require.NotNil(t, outer.Canonical)
	require.NotNil(t, outer.Canonical.AgentSpawn)
	assert.Equal(t, "parallel", outer.Canonical.AgentSpawn.Mode)
	require.Len(t, outer.Canonical.AgentSpawn.Runs, 2)
	assert.Equal(t, "small", outer.Canonical.AgentSpawn.Runs[0].RequestedModel)
	assert.Equal(t, "run-0", cm.Blocks[1].SubagentRunID)
	assert.Empty(t, cm.Blocks[2].SubagentRunID, "missing run ID must survive as an unassigned fallback step")
}

func TestConvertOldEventToNew_PreservesSubagentRunID(t *testing.T) {
	call := convertOldEventToNew(agentruntime.RuntimeEvent{
		Kind: agentruntime.EventToolUseStart,
		ToolUse: &agentruntime.ToolUseEvent{
			ID: "child", ParentToolCallID: "outer", SubagentRunID: "run-1",
		},
	}).(agentruntime.ToolCall)
	assert.Equal(t, "run-1", call.SubagentRunID)

	result := convertOldEventToNew(agentruntime.RuntimeEvent{
		Kind: agentruntime.EventToolResult,
		ToolResult: &agentruntime.ToolResultEvent{
			ToolUseID: "child", ParentToolCallID: "outer", SubagentRunID: "run-1",
		},
	}).(agentruntime.ToolResult)
	assert.Equal(t, "run-1", result.SubagentRunID)
}

func TestToChatMessage_NoticeBlockProjection(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		blocks.NoticeBlock{Level: "info", Text: "hi"},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 1)
	assert.Equal(t, "notice", cm.Blocks[0].Type)
	assert.Equal(t, "info", cm.Blocks[0].Level)
	// 非结构化文本(旧数据 / 其它来源的 notice)原样渲染 Text,不带模型字段。
	assert.Equal(t, "hi", cm.Blocks[0].Text)
	assert.Empty(t, cm.Blocks[0].SelectedModel)
	assert.Empty(t, cm.Blocks[0].ActualModel)
}

func TestToChatMessage_NoticeBlockProjectionDecodesStructuredPayload(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		blocks.NoticeBlock{Level: "info", Text: `{"selected":"selected-model","actual":"actual-model"}`},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 1)
	assert.Equal(t, "notice", cm.Blocks[0].Type)
	assert.Equal(t, "info", cm.Blocks[0].Level)
	assert.Equal(t, "selected-model", cm.Blocks[0].SelectedModel)
	assert.Equal(t, "actual-model", cm.Blocks[0].ActualModel)
	// 结构化负载不把原始 JSON 泄漏给前端 —— 前端用 SelectedModel/ActualModel 走 t() 渲染。
	assert.Empty(t, cm.Blocks[0].Text)
}

func TestAskQuestionsToDTO_PreservesRequestUserInputMetadata(t *testing.T) {
	got := chatblocks.QuestionsFromRuntime([]agentruntime.AskQuestion{{
		ID:          "target",
		Question:    "Which target?",
		Header:      "Target",
		MultiSelect: false,
		IsOther:     true,
		IsSecret:    true,
		Options: []agentruntime.AskOption{{
			Label:       "backend",
			Description: "Backend only.",
			Preview:     "go test ./...",
		}},
	}})

	require.Len(t, got, 1)
	assert.Equal(t, "target", got[0].ID)
	assert.Equal(t, "Which target?", got[0].Question)
	assert.Equal(t, "Target", got[0].Header)
	assert.False(t, got[0].MultiSelect)
	assert.True(t, got[0].IsOther)
	assert.True(t, got[0].IsSecret)
	require.Len(t, got[0].Options, 1)
	assert.Equal(t, "backend", got[0].Options[0].Label)
	assert.Equal(t, "Backend only.", got[0].Options[0].Description)
	assert.Equal(t, "go test ./...", got[0].Options[0].Preview)
}

func TestCreatePermissionMode_DefaultFallback(t *testing.T) {
	convey.Convey("createPermissionMode 在 raw 空串时回落到 backend.DefaultPermissionMode", t, func() {
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type:                  string(agent_backend_entity.TypeClaudeCode),
			DefaultPermissionMode: "plan",
		}
		mode, err := createPermissionMode(ctx, be, "", true)
		assert.NoError(t, err)
		assert.Equal(t, "plan", mode)
	})

	convey.Convey("createPermissionMode 在 raw 与 backend default 都空时返回空串", t, func() {
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type: string(agent_backend_entity.TypeClaudeCode),
		}
		mode, err := createPermissionMode(ctx, be, "", true)
		assert.NoError(t, err)
		assert.Equal(t, "", mode)
	})

	convey.Convey("createPermissionMode 在 raw 非空时不受 backend default 干扰", t, func() {
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type:                  string(agent_backend_entity.TypeClaudeCode),
			DefaultPermissionMode: "plan",
		}
		mode, err := createPermissionMode(ctx, be, "bypassPermissions", true)
		assert.NoError(t, err)
		assert.Equal(t, "bypassPermissions", mode)
	})
}

// TestCreatePermissionMode_BypassDefaultStartsInPlan 覆盖 claudecode agent 配
// DefaultPermissionMode=bypassPermissions 时, 新会话以 plan 起手的派生规则。
//
// session.PermissionMode 留 plan 是为了让前端 pill 显示 Plan + 让用户先做计划,
// 真实 CLI 启动仍按 bypassPermissions(在 claudecode runtime 的 resolveLaunchMode
// 强制), 这条规则与 spawn-after SetPermissionMode 同步链共同支撑「先 plan 后
// bypass」工作流。
func TestCreatePermissionMode_BypassDefaultStartsInPlan(t *testing.T) {
	convey.Convey("Given claudecode + DefaultPermissionMode=bypass, When raw 空, Then 返 plan 起手", t, func() {
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type:                  string(agent_backend_entity.TypeClaudeCode),
			DefaultPermissionMode: "bypassPermissions",
		}
		mode, err := createPermissionMode(ctx, be, "", true)
		assert.NoError(t, err)
		assert.Equal(t, "plan", mode)
	})

	convey.Convey("Given claudecode + bypass default, When planFirst=false (自律会话), Then 直接落 bypass 不强切 plan", t, func() {
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type:                  string(agent_backend_entity.TypeClaudeCode),
			DefaultPermissionMode: "bypassPermissions",
		}
		mode, err := createPermissionMode(ctx, be, "", false)
		assert.NoError(t, err)
		assert.Equal(t, "bypassPermissions", mode)
	})

	convey.Convey("Given claudecode + bypass default, When raw 显式非空, Then 尊重 raw 不强切 plan", t, func() {
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type:                  string(agent_backend_entity.TypeClaudeCode),
			DefaultPermissionMode: "bypassPermissions",
		}
		mode, err := createPermissionMode(ctx, be, "acceptEdits", true)
		assert.NoError(t, err)
		assert.Equal(t, "acceptEdits", mode)
	})

	convey.Convey("Given non-claudecode backend + bypass default, When raw 空, Then 不触发 plan 起手 (规则仅对 claudecode 生效)", t, func() {
		// codex / builtin 不应被这条规则影响; entity.Check 实际禁止非 claudecode 配
		// bypass, 这里用直接构造的实体跨过校验是为了断言推断分支的 backend 类型门禁。
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type:                  string(agent_backend_entity.TypeCodex),
			DefaultPermissionMode: "bypassPermissions",
		}
		mode, err := createPermissionMode(ctx, be, "", true)
		// codex 不允许 bypassPermissions, validate 会回 ChatPermissionModeInvalid;
		// 关键是这里没有走 plan 分支, 错误从 validateRequestedPermissionMode 抛出。
		assert.Error(t, err)
		assert.Equal(t, "", mode)
	})
}

// TestResolveSessionCwd_LocalUsesCwdResolver 验证 be.IsLocal() 时走注入的 CwdResolver 回调。
func TestResolveSessionCwd_LocalUsesCwdResolver(t *testing.T) {
	prev := resolveCwdFn
	t.Cleanup(func() { resolveCwdFn = prev })
	resolveCwdFn = func(ctx context.Context, s *chat_entity.Session) (string, error) {
		return "/Users/me/proj", nil
	}
	sess := &chat_entity.Session{ID: 1, ProjectID: 10, AgentID: 7}
	be := &agent_backend_entity.AgentBackend{DeviceID: ""} // local
	cwd, err := resolveSessionCwd(context.Background(), sess, be)
	require.NoError(t, err)
	assert.Equal(t, "/Users/me/proj", cwd)
}

// TestResolveSessionCwd_NilBackendUsesCwdResolver 验证 be 为 nil 时（back-compat）也走 CwdResolver。
func TestResolveSessionCwd_NilBackendUsesCwdResolver(t *testing.T) {
	prev := resolveCwdFn
	t.Cleanup(func() { resolveCwdFn = prev })
	resolveCwdFn = func(ctx context.Context, s *chat_entity.Session) (string, error) {
		return "/local", nil
	}
	sess := &chat_entity.Session{ID: 1, ProjectID: 10}
	cwd, err := resolveSessionCwd(context.Background(), sess, nil)
	require.NoError(t, err)
	assert.Equal(t, "/local", cwd)
}

// TestResolveSessionCwd_RemoteHitsProjectLocation 验证 be.IsRemote() 时查 project_location_repo。
func TestResolveSessionCwd_RemoteHitsProjectLocation(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	prevRepo := project_location_repo.ProjectLocation()
	mockRepo := mock_project_location_repo.NewMockProjectLocationRepo(ctrl)
	project_location_repo.RegisterProjectLocation(mockRepo)
	t.Cleanup(func() { project_location_repo.RegisterProjectLocation(prevRepo) })

	mockRepo.EXPECT().FindByProjectAndDevice(gomock.Any(), int64(10), "7").Return(
		&project_location_entity.ProjectLocation{ID: 42, ProjectID: 10, DeviceID: "7", Path: "/home/me/proj"}, nil,
	)

	sess := &chat_entity.Session{ID: 1, ProjectID: 10}
	be := &agent_backend_entity.AgentBackend{DeviceID: "7"} // remote
	cwd, err := resolveSessionCwd(context.Background(), sess, be)
	require.NoError(t, err)
	assert.Equal(t, "/home/me/proj", cwd)
}

// TestResolveSessionCwd_RemoteFreeSessionSkipsRepo 验证 ProjectID=0（自由会话）+ 远端 backend
// 时直接返回 ("", nil)，把 cwd 兜底权下放给远端 daemon 的 runtime（cwd=="" → AgentCwd）。
// 关键约束：根本不能去查 project_location_repo —— mockRepo 没设 EXPECT，被调用就会 fail。
func TestResolveSessionCwd_RemoteFreeSessionSkipsRepo(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	prevRepo := project_location_repo.ProjectLocation()
	mockRepo := mock_project_location_repo.NewMockProjectLocationRepo(ctrl)
	project_location_repo.RegisterProjectLocation(mockRepo)
	t.Cleanup(func() { project_location_repo.RegisterProjectLocation(prevRepo) })

	sess := &chat_entity.Session{ID: 1, ProjectID: 0, AgentID: 7}
	be := &agent_backend_entity.AgentBackend{DeviceID: "7"} // remote
	cwd, err := resolveSessionCwd(context.Background(), sess, be)
	require.NoError(t, err)
	assert.Equal(t, "", cwd)
}

// TestResolveSessionCwd_RemoteMissingLocation 验证远端找不到记录时返回 ProjectLocationMissing 错误。
func TestResolveSessionCwd_RemoteMissingLocation(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	prevRepo := project_location_repo.ProjectLocation()
	mockRepo := mock_project_location_repo.NewMockProjectLocationRepo(ctrl)
	project_location_repo.RegisterProjectLocation(mockRepo)
	t.Cleanup(func() { project_location_repo.RegisterProjectLocation(prevRepo) })

	mockRepo.EXPECT().FindByProjectAndDevice(gomock.Any(), int64(10), "7").Return(nil, gorm.ErrRecordNotFound)

	sess := &chat_entity.Session{ID: 1, ProjectID: 10}
	be := &agent_backend_entity.AgentBackend{DeviceID: "7"}
	_, err := resolveSessionCwd(context.Background(), sess, be)
	var httpErr *httputils.Error
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, code.ProjectLocationMissing, httpErr.Code)
}

// TestResolveSessionCwd_LocalPropagatesLocalPathMissing 验证 R10:CwdResolver
// (project_svc.ResolveSessionCwd)对「本机未配置路径」返回的确定错误经
// resolveSessionCwd 原样透出 —— 不折叠成 ProjectLocationMissing / WorkspaceFsNoCwd,
// 也不是 ("", nil)。chat_svc/chat.go 的全部读取点都经这条路径取 cwd,因此这里
// 通过即代表它们随解析点自动生效(R11)。
func TestResolveSessionCwd_LocalPropagatesLocalPathMissing(t *testing.T) {
	prev := resolveCwdFn
	t.Cleanup(func() { resolveCwdFn = prev })
	resolveCwdFn = func(ctx context.Context, s *chat_entity.Session) (string, error) {
		return "", i18n.NewError(ctx, code.ProjectLocalPathMissing)
	}
	sess := &chat_entity.Session{ID: 1, ProjectID: 10, AgentID: 7}
	be := &agent_backend_entity.AgentBackend{DeviceID: ""} // local
	cwd, err := resolveSessionCwd(context.Background(), sess, be)
	require.Error(t, err)
	assert.Equal(t, "", cwd)
	var httpErr *httputils.Error
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, code.ProjectLocalPathMissing, httpErr.Code)
	assert.NotEqual(t, code.ProjectLocationMissing, httpErr.Code)
	assert.NotEqual(t, code.WorkspaceFsNoCwd, httpErr.Code)
}

// TestCwdUnavailableReasonFor 锁住 R10 的分类表：三种"没有 cwd"必须映射到三个
// 彼此可区分的取值，且未知/无归类原因的错误落空串兜底，不冒充第四种状态。
func TestCwdUnavailableReasonFor(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "local-path-missing",
		cwdUnavailableReasonFor(i18n.NewError(ctx, code.ProjectLocalPathMissing)))
	assert.Equal(t, "location-missing",
		cwdUnavailableReasonFor(i18n.NewError(ctx, code.ProjectLocationMissing)))
	assert.Equal(t, "", cwdUnavailableReasonFor(i18n.NewError(ctx, code.WorkspaceFsNoCwd)))
	assert.Equal(t, "", cwdUnavailableReasonFor(errors.New("unrelated failure")))
	assert.Equal(t, "", cwdUnavailableReasonFor(nil))
}

// ── noopDaemonClient ─────────────────────────────────────────────────────────

type noopDaemonClient struct{}

func (*noopDaemonClient) Call(_ context.Context, _ string, _, _ any) error { return nil }
func (*noopDaemonClient) Notify(_ string, _ any) error                     { return nil }
func (*noopDaemonClient) Handle(_ string, _ func(context.Context, json.RawMessage) (any, error)) {
}
func (*noopDaemonClient) Closed() <-chan struct{} { return nil }
func (*noopDaemonClient) Close() error            { return nil }

// recordingDaemonClient counts every Call invocation per method — used to
// assert that borrowRemoteRuntime issues exactly one runtime.capabilities
// prefetch on the cold path and zero on cache hits.
type recordingDaemonClient struct {
	mu    sync.Mutex
	calls map[string]int
	queue map[string][]func(params, result any) error
}

func newRecordingDaemonClient() *recordingDaemonClient {
	return &recordingDaemonClient{calls: map[string]int{}, queue: map[string][]func(params, result any) error{}}
}

func (c *recordingDaemonClient) Call(_ context.Context, method string, params, result any) error {
	c.mu.Lock()
	c.calls[method]++
	var fn func(params, result any) error
	if xs := c.queue[method]; len(xs) > 0 {
		fn = xs[0]
		c.queue[method] = xs[1:]
	}
	c.mu.Unlock()
	if fn != nil {
		return fn(params, result)
	}
	return nil
}
func (*recordingDaemonClient) Notify(_ string, _ any) error { return nil }
func (*recordingDaemonClient) Handle(_ string, _ func(context.Context, json.RawMessage) (any, error)) {
}
func (*recordingDaemonClient) Closed() <-chan struct{} { return nil }
func (*recordingDaemonClient) Close() error            { return nil }

func (c *recordingDaemonClient) count(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[method]
}

func (c *recordingDaemonClient) expect(method string, fn func(params, result any) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queue[method] = append(c.queue[method], fn)
}

// poolLeaseMocks 把 Pool/Lease/Client 三件套打包,简化各远端缓存测试的注入。
type poolLeaseMocks struct {
	pool   *mock_remote_device_svc.MockConnPool
	lease  *mock_remote_device_svc.MockLease
	client *noopDaemonClient
}

// installMockPool 构造一个 ConnPool / Lease / DaemonClientPort 三件套并注入 svc。
// Pool.Borrow 默认返同一个 Lease;Closed() 返一个永不关的 chan;Release() AnyTimes。
func installMockPool(t *testing.T, ctrl *gomock.Controller, svc *chatSvc, deviceID int64) *poolLeaseMocks {
	t.Helper()
	m := &poolLeaseMocks{
		pool:   mock_remote_device_svc.NewMockConnPool(ctrl),
		lease:  mock_remote_device_svc.NewMockLease(ctrl),
		client: &noopDaemonClient{},
	}
	m.pool.EXPECT().Borrow(gomock.Any(), deviceID).Return(m.lease, nil).AnyTimes()
	m.lease.EXPECT().Client().Return(m.client).AnyTimes()
	m.lease.EXPECT().Closed().Return(make(chan struct{})).AnyTimes()
	m.lease.EXPECT().Release().AnyTimes()
	svc.setConnPoolForTest(m.pool)
	return m
}

// TestBorrowRemoteRuntime_SharesConnAcrossSessions verifies the refcount cache:
// 同一 device 多次借出返回同一 *remote.Runtime 实例;release 减计数,归零摘出 map。
func TestBorrowRemoteRuntime_SharesConnAcrossSessions(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	svc := &chatSvc{}
	installMockPool(t, ctrl, svc, 7)

	be := &agent_backend_entity.AgentBackend{DeviceID: "7"}

	r1, err := svc.borrowRemoteRuntime(context.Background(), be, 100)
	require.NoError(t, err)
	r2, err := svc.borrowRemoteRuntime(context.Background(), be, 101)
	require.NoError(t, err)
	assert.Same(t, r1, r2)

	assert.Equal(t, 2, svc.remoteRuntimeCount(7))

	svc.releaseRemoteRuntime(7, 100)
	assert.Equal(t, 1, svc.remoteRuntimeCount(7))

	svc.releaseRemoteRuntime(7, 101)
	assert.Equal(t, 0, svc.remoteRuntimeCount(7))
}

// TestBorrowRemoteRuntime_PrefetchesCapabilities_OncePerDevice 钉死 Plan B
// 行为:cold path borrow 时同步发一发 runtime.capabilities,缓存到 *remote.Runtime
// 内;同 device 后续 borrow 命中 cache,不再发 RPC。
func TestBorrowRemoteRuntime_PrefetchesCapabilities_OncePerDevice(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	rec := newRecordingDaemonClient()
	pool := mock_remote_device_svc.NewMockConnPool(ctrl)
	lease := mock_remote_device_svc.NewMockLease(ctrl)
	pool.EXPECT().Borrow(gomock.Any(), int64(7)).Return(lease, nil).AnyTimes()
	lease.EXPECT().Client().Return(rec).AnyTimes()
	lease.EXPECT().Closed().Return(make(chan struct{})).AnyTimes()
	lease.EXPECT().Release().AnyTimes()

	svc := &chatSvc{}
	svc.setConnPoolForTest(pool)

	be := &agent_backend_entity.AgentBackend{
		Type:     string(agent_backend_entity.TypeClaudeCode),
		DeviceID: "7",
	}
	_, err := svc.borrowRemoteRuntime(context.Background(), be, 100)
	require.NoError(t, err)
	assert.Equal(t, 1, rec.count(wire.MethodCapabilities), "cold borrow must prefetch capabilities once")

	// Second borrow same device → cache hit, no extra RPC.
	_, err = svc.borrowRemoteRuntime(context.Background(), be, 101)
	require.NoError(t, err)
	assert.Equal(t, 1, rec.count(wire.MethodCapabilities), "cache hit must not re-prefetch")
}

func TestGoal_RemoteReleasesRuntimeAfterOneShotRPC(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	rec := newRecordingDaemonClient()
	rec.expect(wire.MethodCapabilities, func(_, result any) error {
		*(result.(*wire.CapabilitiesResult)) = wire.CapabilitiesResult{
			Capabilities: capability.Capabilities{Set: map[capability.Capability]bool{capability.CapGoal: true}},
		}
		return nil
	})
	rec.expect(wire.MethodSetGoal, func(params, result any) error {
		gp, ok := params.(wire.GoalParams)
		require.True(t, ok, "expected wire.GoalParams, got %T", params)
		assert.Equal(t, int64(100), gp.SessionID)
		assert.Equal(t, "codex-thread-123", gp.ProviderSessionID)
		*(result.(*wire.GoalResult)) = wire.GoalResult{Goal: &agentruntime.Goal{
			ThreadID:  "codex-thread-123",
			Objective: "ship remote goal",
			Status:    "active",
		}}
		return nil
	})

	pool := mock_remote_device_svc.NewMockConnPool(ctrl)
	lease := mock_remote_device_svc.NewMockLease(ctrl)
	pool.EXPECT().Borrow(gomock.Any(), int64(7)).Return(lease, nil)
	lease.EXPECT().Client().Return(rec).AnyTimes()
	lease.EXPECT().Closed().Return(make(chan struct{})).AnyTimes()
	lease.EXPECT().Release().AnyTimes()

	svc := &chatSvc{}
	svc.setConnPoolForTest(pool)
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, nil)
	t.Cleanup(restore)

	be := &agent_backend_entity.AgentBackend{
		ID:       12,
		Type:     string(agent_backend_entity.TypeCodex),
		DeviceID: "7",
		Status:   1,
	}
	sess := &chat_entity.Session{ID: 100, AgentID: 7, ProviderSessionID: "codex-thread-123"}
	objective := "ship remote goal"
	status := "active"

	resp, release, err := svc.setGoalOnSession(context.Background(), sess, &agent_entity.Agent{ID: 7}, be, nil, &SetGoalRequest{
		SessionID: 100,
		Objective: &objective,
		Status:    &status,
	})
	require.NoError(t, err)
	defer release()
	require.NotNil(t, resp.Goal)
	assert.Equal(t, "ship remote goal", resp.Goal.Objective)

	release()
	assert.Equal(t, 0, svc.remoteRuntimeCount(7), "one-shot remote goal RPC must release its remote runtime lease")
	assert.Equal(t, 1, rec.count(wire.MethodSetGoal))
}

// TestBorrowRemoteRuntime_InvalidDevice 当 be.DeviceIDInt() 解析失败时立即返回
// AgentBackendInvalidDevice — 不去摸 Pool。
func TestBorrowRemoteRuntime_InvalidDevice(t *testing.T) {
	svc := &chatSvc{}
	be := &agent_backend_entity.AgentBackend{DeviceID: "not-a-number"}
	_, err := svc.borrowRemoteRuntime(context.Background(), be, 100)
	var httpErr *httputils.Error
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, code.AgentBackendInvalidDevice, httpErr.Code)
}

// TestBorrowRemoteRuntime_DialFailure 当 Pool.Borrow 失败时返回 RemoteRunnerDialFailed,
// 且不在 cache 留下条目(防止下次 borrow 复用坏 entry)。
func TestBorrowRemoteRuntime_DialFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockPool := mock_remote_device_svc.NewMockConnPool(ctrl)
	mockPool.EXPECT().Borrow(gomock.Any(), int64(7)).Return(nil, errors.New("boom"))

	svc := &chatSvc{}
	svc.setConnPoolForTest(mockPool)

	be := &agent_backend_entity.AgentBackend{DeviceID: "7"}
	_, err := svc.borrowRemoteRuntime(context.Background(), be, 100)
	var httpErr *httputils.Error
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, code.RemoteRunnerDialFailed, httpErr.Code)

	assert.Equal(t, 0, svc.remoteRuntimeCount(7))
}

func TestMapTurnError_RemoteProviderMissing(t *testing.T) {
	svc := &chatSvc{}
	err := svc.mapTurnError(context.Background(), nil, &agent_backend_entity.AgentBackend{
		LLMProviderKey: "provider-key-1",
	}, &daemonrpc.Error{
		Code:    daemonrpc.ErrProviderMissing.Code,
		Message: "LLM provider provider-key-1 not configured",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "远端 agentred 未配置")
	assert.Contains(t, err.Error(), "provider-key-1")
}

// TestSelectRunner_LocalReturnsRegistry verifies local backend → agentruntime.For.
func TestSelectRunner_LocalReturnsRegistry(t *testing.T) {
	svc := &chatSvc{}
	be := &agent_backend_entity.AgentBackend{
		Type:     string(agent_backend_entity.TypeClaudeCode),
		DeviceID: "", // local
	}
	runner, err := svc.selectRunner(context.Background(), be, 100)
	require.NoError(t, err)
	require.NotNil(t, runner)
	// 是 *remote.Runtime 则说明走错了分支
	_, isRemote := runner.(*remote.Runtime)
	assert.False(t, isRemote, "local backend should not return *remote.Runtime")
}

// TestSelectRunner_RemoteBorrows verifies remote backend → borrowRemoteRuntime cache.
func TestSelectRunner_RemoteBorrows(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	svc := &chatSvc{}
	installMockPool(t, ctrl, svc, 7)

	be := &agent_backend_entity.AgentBackend{
		Type:     string(agent_backend_entity.TypeClaudeCode),
		DeviceID: "7",
	}
	runner, err := svc.selectRunner(context.Background(), be, 100)
	require.NoError(t, err)
	_, isRemote := runner.(*remote.Runtime)
	assert.True(t, isRemote)
	assert.Equal(t, 1, svc.remoteRuntimeCount(7))
}

// TestSelectRunner_RemoteIdempotent same sessionID → same instance + no refcount inflation.
func TestSelectRunner_RemoteIdempotent(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	svc := &chatSvc{}
	installMockPool(t, ctrl, svc, 7)

	be := &agent_backend_entity.AgentBackend{
		Type:     string(agent_backend_entity.TypeClaudeCode),
		DeviceID: "7",
	}
	r1, err := svc.selectRunner(context.Background(), be, 100)
	require.NoError(t, err)
	r2, err := svc.selectRunner(context.Background(), be, 100) // same sessionID
	require.NoError(t, err)
	assert.Same(t, r1, r2)
	assert.Equal(t, 1, svc.remoteRuntimeCount(7), "same sessionID must not inflate refcount")
}

func TestToolUseToChatBlock_Canonical(t *testing.T) {
	convey.Convey("Edit → Canonical FileEdit", t, func() {
		cb := toolUseToChatBlock("tu-1", "Edit", map[string]any{
			"file_path":  "/x.go",
			"old_string": "a\n",
			"new_string": "b\n",
		})
		convey.So(cb.Canonical, convey.ShouldNotBeNil)
		convey.So(string(cb.Canonical.Kind), convey.ShouldEqual, "file.edit")
		convey.So(cb.Canonical.FileEdit, convey.ShouldNotBeNil)
		convey.So(cb.Canonical.FileEdit.Files[0].Path, convey.ShouldEqual, "/x.go")
	})

	convey.Convey("file_change → Canonical FileEdit", t, func() {
		cb := toolUseToChatBlock("tu-2", "file_change", map[string]any{
			"changes": []any{
				map[string]any{"path": "a.go", "kind": "update", "diff": "@@ -1,1 +1,1 @@\n-a\n+A\n"},
			},
		})
		convey.So(cb.Canonical, convey.ShouldNotBeNil)
		convey.So(string(cb.Canonical.Kind), convey.ShouldEqual, "file.edit")
		convey.So(cb.Canonical.FileEdit, convey.ShouldNotBeNil)
	})

	convey.Convey("Write → Canonical FileWrite", t, func() {
		cb := toolUseToChatBlock("tu-3", "Write", map[string]any{
			"file_path": "/x.go",
			"content":   "hello\n",
		})
		convey.So(cb.Canonical, convey.ShouldNotBeNil)
		convey.So(string(cb.Canonical.Kind), convey.ShouldEqual, "file.write")
		convey.So(cb.Canonical.FileWrite, convey.ShouldNotBeNil)
		convey.So(cb.Canonical.FileWrite.Path, convey.ShouldEqual, "/x.go")
	})

	convey.Convey("Bash → Canonical=nil(走 RawToolCard 兜底)", t, func() {
		cb := toolUseToChatBlock("tu-4", "Bash", map[string]any{"command": "ls"})
		convey.So(cb.Canonical, convey.ShouldBeNil)
	})
}

// TestEventShowsProgressAfterError_SubagentModel 覆盖 wrap-up 复审第二轮 Finding 2:
// eventShowsProgressAfterError 是「错误后收到哪些事件才清除 streamStopErr」的跨切面
// 注册表,agentruntime.SubagentModel 漏登记 —— 瞬时 API 错误置上 streamStopErr 后,
// 子代理下一帧内部 assistant 到达时该事件会在 runTurn 循环里被 continue 掉(既不清
// 错误也不应用),要等随后的 ToolCall/SubagentProgress 才能自愈,凭空多一帧延迟。
func TestEventShowsProgressAfterError_SubagentModel(t *testing.T) {
	convey.Convey("SubagentModel 事件应被视为错误后的进度,从而清除 streamStopErr", t, func() {
		ev := agentruntime.SubagentModel{ToolCallID: "task-1", Model: "claude-haiku-4-5-20251001"}
		convey.So(eventShowsProgressAfterError(ev), convey.ShouldBeTrue)
	})
}
