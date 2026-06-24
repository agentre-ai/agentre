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
