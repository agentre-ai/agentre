//go:build e2e

package fake

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
)

func TestRun_EchoesPromptThenDone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r := New()
	events, result, err := r.Run(ctx, agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{ID: 1, Type: string(agent_backend_entity.TypeClaudeCode)},
		SessionID: 42,
		UserText:  "ping",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	var text string
	var sawDone bool
	for ev := range events {
		switch e := ev.(type) {
		case agentruntime.TextDelta:
			text += e.Text
		case agentruntime.Done:
			sawDone = true
		}
	}

	assert.Equal(t, ReplyPrefix+"ping", text)
	assert.True(t, sawDone)
	assert.Equal(t, "e2e-fake-42", result.ProviderSessionID)
	assert.Equal(t, "e2e-fake-model", result.Model)
}

func TestRun_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before draining

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{SessionID: 7, UserText: "hello world this is a long enough prompt to span several chunks"})
	require.NoError(t, err)

	// Draining a pre-cancelled run must terminate (channel closes) without hanging.
	for range events { //nolint:revive // draining
	}
}

func TestRun_HonorsChunkDelayEnv(t *testing.T) {
	t.Setenv("AGENTRE_E2E_FAKE_CHUNK_DELAY_MS", "25")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID: 7,
		UserText:  "hello world this is long enough to span chunks",
	})
	require.NoError(t, err)

	first := <-events
	_, ok := first.(agentruntime.TextDelta)
	require.True(t, ok)
	start := time.Now()

	second := <-events
	_, ok = second.(agentruntime.TextDelta)
	require.True(t, ok)
	assert.GreaterOrEqual(t, time.Since(start), 20*time.Millisecond)
}

// 流程建指令:e2e-workflow-create:<name>,取指令所在行;空段/无指令 → !ok。
func TestParseWorkflowCreateDirective(t *testing.T) {
	name, ok := parseWorkflowCreateDirective("(来自 用户)\ne2e-workflow-create:评审流程")
	require.True(t, ok)
	assert.Equal(t, "评审流程", name)

	for _, bad := range []string{"无指令", "e2e-workflow-create:", "e2e-workflow-create:   "} {
		_, ok := parseWorkflowCreateDirective(bad)
		assert.False(t, ok, "input=%q", bad)
	}
}

// 单聊轮注入 workflow 工具 + e2e-workflow-create 指令 → fake 调一次 workflow_create
// (挂起等审批由 svc 侧负责,这里 server 即时应答)。
func TestRun_PostsWorkflowCreateOnDirective(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv, snapshot := toolCaptureServer(t)

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID: 25,
		UserText:  "e2e-workflow-create:评审流程",
		MCPServers: []agentruntime.MCPServerSpec{{
			Name:    "workflow",
			URL:     srv.URL + "/mcp/workflow/",
			Headers: map[string]string{"Authorization": "Bearer tok"},
			Tools:   []string{"workflow_create"},
		}},
	})
	require.NoError(t, err)
	for range events { //nolint:revive // draining
	}

	calls := snapshot()
	require.Len(t, calls["workflow_create"], 1)
	args := calls["workflow_create"][0]
	assert.Equal(t, "评审流程", args["name"])
	assert.Equal(t, "e2e-workflow-content: 评审流程", args["content"])
}

// CapMCPTools 必须声明,才能让 MCP 工具注入接缝(org/subagent/orchestrate)在 e2e 里生效。
func TestCapabilities_DeclaresMCPTools(t *testing.T) {
	caps := New().Capabilities()
	assert.True(t, caps.Has(capability.CapMCPTools))
	assert.True(t, caps.Has(capability.CapAbort))
}

// System prompt 断言指令只服务本地 e2e:用真实 RunRequest.SystemPrompt 证明 workflow /
// handoff 等主持人提示确实注入,再经普通 fake 回复暴露成 UI/DB 可观测文本。
func TestRun_ReportsSystemPromptNeedle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID:      20,
		UserText:       "(来自 用户)\ne2e-assert-system:E2E_WORKFLOW_SENTINEL",
		SystemPrompt:   "主持人流程:E2E_WORKFLOW_SENTINEL; .agentre/handoff/5/",
		MCPServers:     nil,
		PermissionMode: "",
	})
	require.NoError(t, err)

	var text string
	for ev := range events {
		if delta, ok := ev.(agentruntime.TextDelta); ok {
			text += delta.Text
		}
	}
	assert.Contains(t, text, "e2e-system-ok:E2E_WORKFLOW_SENTINEL")
}

func TestRun_ReportsMissingSystemPromptNeedle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID:    21,
		UserText:     "(来自 用户)\ne2e-assert-system:E2E_WORKFLOW_SENTINEL",
		SystemPrompt: "主持人流程:别的内容",
	})
	require.NoError(t, err)

	var text string
	for ev := range events {
		if delta, ok := ev.(agentruntime.TextDelta); ok {
			text += delta.Text
		}
	}
	assert.Contains(t, text, "e2e-system-missing:E2E_WORKFLOW_SENTINEL")
}

// e2e-ask:<question> → fake emit 一条未答的 UserAskRequest(带问题/选项)后 Done。
// chat_svc finalize 据此把 ask 标 expired(失效终态 e2e 的产出端)。
func TestRun_EmitsUserAskRequestOnDirective(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID: 30,
		UserText:  "(来自 用户)\ne2e-ask:要继续吗?",
	})
	require.NoError(t, err)

	var ask *agentruntime.UserAskRequest
	var sawDone bool
	for ev := range events {
		switch e := ev.(type) {
		case agentruntime.UserAskRequest:
			cp := e
			ask = &cp
		case agentruntime.Done:
			sawDone = true
		}
	}
	require.NotNil(t, ask, "must emit a UserAskRequest")
	require.Len(t, ask.Questions, 1)
	assert.Equal(t, "要继续吗?", ask.Questions[0].Question)
	assert.NotEmpty(t, ask.RequestID)
	assert.True(t, sawDone, "Done must follow the ask (turn finalizes unanswered)")
}

// 无 e2e-ask 指令 → 不 emit UserAskRequest(普通回显轮不应误触发失效卡)。
func TestRun_NoUserAskWithoutDirective(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{SessionID: 31, UserText: "ping"})
	require.NoError(t, err)
	for ev := range events {
		if _, ok := ev.(agentruntime.UserAskRequest); ok {
			t.Fatal("must not emit UserAskRequest without e2e-ask directive")
		}
	}
}

// e2e-hook-create:<name> + 注入 hook 工具 → fake 调一次 hook_create(必填四段齐全)。
func TestRun_PostsHookCreateOnDirective(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv, snapshot := toolCaptureServer(t)

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID: 26,
		UserText:  "e2e-hook-create:夜间巡检",
		MCPServers: []agentruntime.MCPServerSpec{{
			Name:    "hook",
			URL:     srv.URL + "/mcp/hook/",
			Headers: map[string]string{"Authorization": "Bearer tok"},
			Tools:   []string{"hook_create"},
		}},
	})
	require.NoError(t, err)
	for range events { //nolint:revive // draining
	}

	calls := snapshot()
	require.Len(t, calls["hook_create"], 1)
	args := calls["hook_create"][0]
	assert.Equal(t, "夜间巡检", args["name"])
	assert.Equal(t, "bash", args["interpreter"])
	assert.NotEmpty(t, args["command"])
	assert.NotEmpty(t, args["scheduleExpr"])
}

// 子任务结算回报续轮:leader 收到 <dispatch_done…> 轻量通知(切片 A 回报分层信封,取代旧
// 「【子任务」纯文本)+ 注入 finish 工具 → fake 自动调一次 finish 收口。守护:reportToParent
// 改信封后编排 e2e 链路仍能自动收口(旧「【子任务」检测已随之更新)。
func TestRun_AutoFinishesOnTaskDoneEnvelope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv, snapshot := toolCaptureServer(t)

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID: 40,
		UserText:  `<dispatch_done dispatch_id="2" agent="3" call_seq="1">e2e-fake-reply: 子活干完了(read(dispatch_id=2) 看全文)</dispatch_done>`,
		MCPServers: []agentruntime.MCPServerSpec{{
			Name:    "orchestrate",
			URL:     srv.URL + "/mcp/orchestrate/",
			Headers: map[string]string{"Authorization": "Bearer tok"},
			Tools:   []string{"dispatch", "finish"},
		}},
	})
	require.NoError(t, err)
	for range events { //nolint:revive // draining
	}

	calls := snapshot()
	require.Len(t, calls["finish"], 1)
	assert.Equal(t, "e2e-orchestration-complete", calls["finish"][0]["summary"])
}

// 子任务技术崩溃回报续轮:leader 收到 <dispatch_error…> 通知 → fake 同样自动收口(与 done 同路)。
func TestRun_AutoFinishesOnTaskErrorEnvelope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv, snapshot := toolCaptureServer(t)

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID: 41,
		UserText:  `<dispatch_error dispatch_id="2" agent="3" reason="运行时崩溃">(read(dispatch_id=2) 看详情)</dispatch_error>`,
		MCPServers: []agentruntime.MCPServerSpec{{
			Name:    "orchestrate",
			URL:     srv.URL + "/mcp/orchestrate/",
			Headers: map[string]string{"Authorization": "Bearer tok"},
			Tools:   []string{"dispatch", "finish"},
		}},
	})
	require.NoError(t, err)
	for range events { //nolint:revive // draining
	}

	calls := snapshot()
	require.Len(t, calls["finish"], 1)
}

// e2e-orch-ask:<agent>:<question> + 注入 orchestrate 工具 → fake 调一次 ask。
func TestRun_PostsOrchAskOnDirective(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv, snapshot := toolCaptureServer(t)

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID: 27,
		UserText:  "e2e-orch-ask:E2E Member:你完成了吗",
		MCPServers: []agentruntime.MCPServerSpec{{
			Name:    "orchestrate",
			URL:     srv.URL + "/mcp/orchestrate/",
			Headers: map[string]string{"Authorization": "Bearer tok"},
			Tools:   []string{"ask", "reply", "dispatch", "finish"},
		}},
	})
	require.NoError(t, err)
	for range events { //nolint:revive // draining
	}

	calls := snapshot()
	require.Len(t, calls["ask"], 1)
	assert.Equal(t, "E2E Member", calls["ask"][0]["agent"])
	assert.Equal(t, "你完成了吗", calls["ask"][0]["question"])
	assert.Empty(t, calls["reply"], "asker must not also reply")
}

// 收到注入的「【收到提问 ask_id=X】」(普通问题体)→ fake 调 reply 带回 ask_id。
func TestRun_PostsReplyOnInjectedAsk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv, snapshot := toolCaptureServer(t)

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID: 28,
		UserText:  "【收到提问 ask_id=abc-123】你完成了吗\n请调用 reply(ask_id=\"abc-123\", answer=...) 回复。",
		MCPServers: []agentruntime.MCPServerSpec{{
			Name:    "orchestrate",
			URL:     srv.URL + "/mcp/orchestrate/",
			Headers: map[string]string{"Authorization": "Bearer tok"},
			Tools:   []string{"ask", "reply"},
		}},
	})
	require.NoError(t, err)
	for range events { //nolint:revive // draining
	}

	calls := snapshot()
	require.Len(t, calls["reply"], 1)
	assert.Equal(t, "abc-123", calls["reply"][0]["ask_id"])
	assert.NotEmpty(t, calls["reply"][0]["answer"])
	assert.Empty(t, calls["ask"], "plain injected question must reply, not ask back")
}

// 死锁场景:注入的问题体本身又是 e2e-orch-ask 指令 → fake 优先 ask 回去,不 reply。
func TestRun_AsksBackWhenInjectedQuestionIsAskDirective(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv, snapshot := toolCaptureServer(t)

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID: 29,
		UserText:  "【收到提问 ask_id=xyz-9】e2e-orch-ask:CEO 助手:回环\n请调用 reply(...) 回复。",
		MCPServers: []agentruntime.MCPServerSpec{{
			Name:    "orchestrate",
			URL:     srv.URL + "/mcp/orchestrate/",
			Headers: map[string]string{"Authorization": "Bearer tok"},
			Tools:   []string{"ask", "reply"},
		}},
	})
	require.NoError(t, err)
	for range events { //nolint:revive // draining
	}

	calls := snapshot()
	require.Len(t, calls["ask"], 1, "ask back to form the cycle")
	assert.Equal(t, "CEO 助手", calls["ask"][0]["agent"])
	assert.Empty(t, calls["reply"], "deadlock path must not reply")
}

// dispatch 与 ask 互斥:dispatch 的 brief 里嵌了 e2e-orch-ask 指令时,本轮只 dispatch,不对自己
// ask(否则死锁构造里 leader 会自问、污染等待图)。
func TestRun_DispatchSuppressesAskSameTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv, snapshot := toolCaptureServer(t)

	r := New()
	events, _, err := r.Run(ctx, agentruntime.RunRequest{
		SessionID: 32,
		UserText:  "e2e-orch-dispatch:E2E Member:e2e-orch-ask:CEO 助手:回环",
		MCPServers: []agentruntime.MCPServerSpec{{
			Name:    "orchestrate",
			URL:     srv.URL + "/mcp/orchestrate/",
			Headers: map[string]string{"Authorization": "Bearer tok"},
			Tools:   []string{"dispatch", "ask", "reply", "finish"},
		}},
	})
	require.NoError(t, err)
	for range events { //nolint:revive // draining
	}

	calls := snapshot()
	require.Len(t, calls["dispatch"], 1)
	assert.Equal(t, "E2E Member", calls["dispatch"][0]["agent"])
	assert.Empty(t, calls["ask"], "dispatch turn must not also ask (mutual exclusion)")
}

// parseInjectedAskID:从注入抬头解出 ask_id;提问方自身指令不含 ask_id= → !ok。
func TestParseInjectedAskID(t *testing.T) {
	id, ok := parseInjectedAskID("【收到提问 ask_id=abc-123】问题")
	require.True(t, ok)
	assert.Equal(t, "abc-123", id)

	for _, bad := range []string{"e2e-orch-ask:X:q", "无 ask 标记", "ask_id="} {
		_, ok := parseInjectedAskID(bad)
		assert.False(t, ok, "input=%q", bad)
	}
}

// toolCaptureServer 收集本轮 fake 发出的全部 tools/call,按 tool 名归档参数。
func toolCaptureServer(t *testing.T) (*httptest.Server, func() map[string][]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	calls := map[string][]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var rpc struct {
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		require.NoError(t, json.Unmarshal(b, &rpc))
		mu.Lock()
		calls[rpc.Params.Name] = append(calls[rpc.Params.Name], rpc.Params.Arguments)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string][]map[string]any {
		mu.Lock()
		defer mu.Unlock()
		out := map[string][]map[string]any{}
		for k, v := range calls {
			out[k] = append([]map[string]any(nil), v...)
		}
		return out
	}
}
