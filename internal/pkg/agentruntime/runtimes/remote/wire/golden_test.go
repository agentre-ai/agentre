// golden_test.go 生成 wire 协议的黄金样本(浏览器侧 TS 编解码的对照基准)。
//
// 黄金样本的用途是「与 Go 侧逐字段同构」:agentre-server/frontend 的 vitest 读同一批
// 帧,断言 TS 编解码解出的结构与这里用真实 Go marshaler 序列化出来的逐字节一致。
// 生成器住在本包(wire 类型旁),产物提交到 agentre-server/frontend/src/__tests__/
// fixtures/wire/ 供 vitest 读 —— 跨仓资产,两边各自提交。
//
// 本测试自己同时充当自检:每一条样本都验证「确定性」(再 marshal 一次逐字节相同,
// Go 的 map key 排序保证稳定)与「往返不丢字段」(map 形态解析再序列化逐字节相同)。
// 带上 WIRE_GOLDEN_DIR 环境变量运行即把样本写成文件,不带则只自检、不写盘,让
// `go test ./...` 保持干净:
//
//	WIRE_GOLDEN_DIR=/path/to/agentre-server/frontend/src/__tests__/fixtures/wire \
//	  GOWORK=off go test ./internal/pkg/agentruntime/runtimes/remote/wire/ -run TestGoldenSamples
//
// 命名约定:每条样本的名字就是 agentre-server 里那个 .json 文件的 basename,
// vitest 直接 import 同名文件。
package wire

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
)

// goldenFrame 一条黄金样本:名字 + 用真实 Go marshaler 序列化出来的帧。
// extraKeys 是注入的未知字段键名,自检时断言它们在序列化结果里确实存在。
type goldenFrame struct {
	name      string
	body      any
	extraKeys []string
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// injectUnknown 把既有帧(marshal 成 map)加上未知字段,模拟老版本 agentred /
// 未来扩展在帧里多带了 TS codec 不认识的键 —— 验证「未知字段不丢弃」。
func injectUnknown(t *testing.T, name string, body any, extra map[string]any) goldenFrame {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	for k, v := range extra {
		m[k] = v
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	return goldenFrame{name: name, body: m, extraKeys: keys}
}

// buildGoldenFrames 用真实 Go 结构体 + 真实 Go marshaler 组装全部黄金样本。
// 新增 wire 帧类型时,在这里加一条样本(以及 agentre-server 侧对应的 TS 解码断言)。
func buildGoldenFrames(t *testing.T) []goldenFrame {
	t.Helper()

	const (
		sid       = int64(42)
		agentID   = int64(7)
		title     = "重构登录页"
		agentSync = "01JZ7W2A8KZ4R5T6Y7U8I9O0P1Q"
		provSess  = "sess_abc123"
	)

	// 一条带 R7 + 决策 8 全字段的会话(浏览器看到的「新」会话)。
	newSummary := SessionSummary{
		SessionID:         sid,
		AgentID:           agentID,
		Title:             title,
		AgentSyncID:       agentSync,
		ProviderSessionID: provSess,
		Cwd:               "/home/agent/proj",
		BackendType:       "claudecode",
		LifecycleState:    SessionLifecycleRunning,
		WaitingForInput:   true,
		LatestSeq:         12,
	}
	// 一条老会话:R7 未到达,标题 / Agent 标识 / provider_session_id 如实留空
	// (omitempty 直接省略键,不填占位名)。
	legacySummary := SessionSummary{
		SessionID:      8,
		PeerFingerprint: "fp-desktop",
		AgentID:         3,
		Cwd:             "/var/proj",
		BackendType:     "codex",
		LifecycleState:  SessionLifecycleIdle,
		LatestSeq:       5,
	}

	// 实时通知帧(EventFrame)的 event 载荷:真实 agentruntime 事件走它的
	// MarshalJSON(拍平成 {"kind":...}),与 daemon handlers/runtime.go 同一来源。
	textDelta := mustJSON(t, agentruntime.TextDelta{Text: "你好"})

	runAck := RunAck{
		SessionID:            sid,
		ProviderSessionID:    provSess,
		LaunchPermissionMode: "default",
		ProviderFallbackKey:  "key-fallback",
	}
	runResultDone := RunResultDoneFrame{
		SessionID:         sid,
		ProviderSessionID: provSess,
		Usage: &UsageWire{
			PromptTokens:        100,
			CompletionTokens:    50,
			ReasoningTokens:     10,
			CachedTokens:        5,
			CacheCreationTokens: 2,
			TotalTokens:         155,
		},
		UserAnchor:    "anchor-1",
		Model:         "claude-sonnet-4-5",
		ContextWindow: 200000,
		TurnToken:     9,
		Seq:           12,
	}

	return []goldenFrame{
		{
			name: "run-params",
			body: RunParams{
				Backend:           json.RawMessage(`{"backendType":"claudecode"}`),
				AgentID:           agentID,
				SessionID:         sid,
				Cwd:               "/home/agent/proj",
				Title:             title,
				AgentSyncID:       agentSync,
				SystemPrompt:      "你是 AgentRe 的 Agent。",
				ProviderSessionID: provSess,
				UserText:          "把登录按钮改成蓝色",
				History: []HistoryMessageWire{
					{Role: "user", Blocks: []cagoblocks.StoredBlock{
						{Type: "text", Data: json.RawMessage(`"上一轮的上下文"`)},
					}},
				},
				PermissionMode:    "default",
				CollaborationMode: "manual",
				MCPServers: []agentruntime.MCPServerSpec{
					{
						Name:    "org",
						URL:     "http://127.0.0.1:8899/mcp/org/",
						Headers: map[string]string{"Authorization": "Bearer tok"},
						Tools:   []string{"mcp__org__list"},
					},
				},
				EnabledPlugins: map[string]bool{"auto-continue": true, "dangerous": false},
				LLMProviderKey: "11111111-2222-3333-4444-555555555555",
				SourceDevice:   "fp-web-1",
				SourceDeviceName: "Chrome · macOS",
			},
		},
		{name: "run-ack", body: runAck},
		{name: "session-summary", body: newSummary},
		{name: "session-summary-legacy", body: legacySummary},
		{name: "session-list-result", body: SessionListResult{Sessions: []SessionSummary{newSummary, legacySummary}}},
		{name: "session-pull-params", body: SessionPullParams{SessionID: sid, Cursor: 0, Limit: DefaultSessionPullLimit}},
		{
			name: "session-pull-result",
			body: SessionPullResult{
				Notifications: []JournaledNotification{
					// 日志行上的 params 不含 seq —— seq 是日志行自己的列,补齐端盖上去。
					{Seq: 11, Method: NotifyEvent, Params: mustJSON(t, EventFrame{SessionID: sid, Event: textDelta})},
					{Seq: 12, Method: NotifyRunResultDone, Params: mustJSON(t, runResultDone)},
				},
				Cursor:    12,
				HasMore:   false,
				OldestSeq: 1,
			},
		},
		{
			name: "journaled-notification",
			body: JournaledNotification{
				Seq:    11,
				Method: NotifyEvent,
				Params: mustJSON(t, EventFrame{SessionID: sid, Event: textDelta}),
			},
		},
		{name: "session-attach-params", body: SessionAttachParams{SessionID: sid}},
		{
			name: "session-attach-result",
			body: SessionAttachResult{
				SessionID:      sid,
				BackendType:    "claudecode",
				LifecycleState: SessionLifecycleRunning,
				LatestSeq:      12,
			},
		},
		{name: "session-pending-waiters-params", body: SessionPendingWaitersParams{SessionID: sid}},
		{
			name: "session-pending-waiters-result",
			body: SessionPendingWaitersResult{
				ToolPermissions: []agentruntime.PendingToolPermission{
					{RequestID: "perm-1", ToolName: "Bash", Input: json.RawMessage(`{"command":"ls -la"}`)},
				},
				AskUserQuestions: []agentruntime.PendingAskUserQuestion{
					{RequestID: "ask-1", Questions: []agentruntime.AskQuestion{
						{ID: "q1", Question: "确认继续执行？", Header: "确认", Options: []agentruntime.AskOption{{Label: "继续", Description: "继续执行"}}},
					}},
				},
			},
		},
		{name: "event-frame", body: EventFrame{SessionID: sid, Event: textDelta, Seq: 11}},
		{name: "run-result-done-frame", body: runResultDone},
		{
			name: "usage-wire",
			body: UsageWire{
				PromptTokens:        100,
				CompletionTokens:    50,
				ReasoningTokens:     10,
				CachedTokens:        5,
				CacheCreationTokens: 2,
				TotalTokens:         155,
			},
		},
		{name: "autonomous-turn-started", body: AutonomousTurnStartedFrame{SessionID: sid, Trigger: "auto", TurnToken: 9, Seq: 13}},
		// 带未知字段的帧:验证 TS 解码不丢弃。
		injectUnknown(t, "run-params-extra", RunParams{
			Backend:         json.RawMessage(`{"backendType":"claudecode"}`),
			AgentID:         agentID,
			SessionID:       sid,
			Title:           title,
			AgentSyncID:     agentSync,
			SourceDevice:    "fp-web-1",
			SourceDeviceName: "Chrome · macOS",
		}, map[string]any{"futureField": map[string]any{"nested": true}, "clientNote": "来自浏览器的自定义字段"}),
		injectUnknown(t, "session-pull-result-extra", SessionPullResult{
			Notifications: []JournaledNotification{
				{Seq: 1, Method: NotifyEvent, Params: mustJSON(t, EventFrame{SessionID: sid, Event: textDelta, Seq: 1})},
			},
			Cursor:    1,
			HasMore:   true,
			OldestSeq: 1,
		}, map[string]any{"serverVersion": "1.2.3"}),
		// JSON-RPC 信封(daemon/rpc.Frame 同 shape;wire 包不反向依赖 daemon,这里用
		// encoding/json 直接组装 —— 帧体仍是上面真实 marshaler 的字节)。
		{
			name: "frame-envelope-request",
			body: map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  MethodSessionPull,
				"params":  mustJSON(t, SessionPullParams{SessionID: sid, Cursor: 0, Limit: DefaultSessionPullLimit}),
			},
		},
		{
			name: "frame-envelope-response",
			body: map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  mustJSON(t, SessionListResult{Sessions: []SessionSummary{newSummary}}),
			},
		},
		{
			name: "frame-envelope-notification",
			body: map[string]any{
				"jsonrpc": "2.0",
				"method":  NotifyEvent,
				"params":  mustJSON(t, EventFrame{SessionID: sid, Event: textDelta, Seq: 11}),
			},
		},
		{
			name: "frame-envelope-error",
			body: map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"error":   map[string]any{"code": ErrCodeSessionNotFound, "message": "session not found"},
			},
		},
	}
}

// TestGoldenSamples 验证每条黄金样本的确定性 + 往返不丢字段;带上
// WIRE_GOLDEN_DIR 时把样本写成文件(agentre-server/frontend 的 fixtures)。
func TestGoldenSamples(t *testing.T) {
	dir := os.Getenv("WIRE_GOLDEN_DIR")
	if dir == "" {
		t.Log("WIRE_GOLDEN_DIR 未设置:只做自检,不写盘")
	}

	for _, gf := range buildGoldenFrames(t) {
		gf := gf
		t.Run(gf.name, func(t *testing.T) {
			b, err := json.MarshalIndent(gf.body, "", "  ")
			require.NoError(t, err)

			// 确定性:同一帧再 marshal 一次逐字节相同(Go 的 map key 排序保证稳定)。
			b2, err := json.MarshalIndent(gf.body, "", "  ")
			require.NoError(t, err)
			require.Equal(t, string(b), string(b2), "帧 %s 序列化不确定", gf.name)

			// 往返:map 形态解析再序列化再解析,两次解析的结构逐字段相同
			// (不丢字段,含未知字段)。比较解析后的结构而不是字节 —— struct 按声明序
			// 序列化、map 按键排序,逐字节比会误报"字段丢失"。
			var m map[string]any
			require.NoError(t, json.Unmarshal(b, &m), "帧 %s 不是合法 JSON", gf.name)
			b3, err := json.MarshalIndent(m, "", "  ")
			require.NoError(t, err)
			var m3 map[string]any
			require.NoError(t, json.Unmarshal(b3, &m3), "帧 %s 往返后不是合法 JSON", gf.name)
			require.Equal(t, m, m3, "帧 %s 往返后字段丢失", gf.name)

			// 注入的未知字段确实存在。
			for _, k := range gf.extraKeys {
				_, ok := m[k]
				require.True(t, ok, "帧 %s 应含未知字段 %q", gf.name, k)
			}

			if dir != "" {
				require.NoError(t, os.MkdirAll(dir, 0o755), "创建黄金样本目录")
				require.NoError(t, os.WriteFile(filepath.Join(dir, gf.name+".json"), append(b, '\n'), 0o644), "写黄金样本")
			}
		})
	}
}
