package piagent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStream_RawSinkReceivesFrames 校验 pi-agent 子进程普通原始 stdout JSON-RPC 帧
// 会喂给 rawSink —— debug 级原始帧转储底座。走 Text → Stream → startRPC
// 真实注入路径,而非直接 newStream,确保 Client.rawSink 真的接到 rpcProcess。
func TestStream_RawSinkReceivesFrames(t *testing.T) {
	runner := &fakeRunner{process: newFakeProcess(t)}
	runner.process.stdout = strings.NewReader(strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"test-native-session"}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"pong"}}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		"",
	}, "\n"))
	runner.process.finishOnSignal(interruptExitError(t))

	var mu sync.Mutex
	var got []string
	client := New(
		WithRPCProcessRunnerForTesting(runner),
		WithKillGrace(time.Second),
		WithRawSink(func(b []byte) {
			mu.Lock()
			got = append(got, string(b))
			mu.Unlock()
		}),
	)

	text, err := client.Text(context.Background(), "ping")
	require.NoError(t, err)
	assert.Equal(t, "pong", text)

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(got, "\n")
	assert.Contains(t, joined, `"command":"prompt"`)
	assert.Contains(t, joined, `"type":"agent_end"`)
	assert.Contains(t, joined, `"type":"agent_settled"`)
}

func TestStream_RawSinkOmitsSessionEntryPayloads(t *testing.T) {
	// Given get_entries responses contain historical prompts and image data,
	// When raw diagnostics are enabled for an anchor-tracked turn,
	// Then those sensitive payloads never reach the raw sink while ordinary frames still do.
	script := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-1"}}`,
		`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"user","content":"historical-secret-prompt","images":[{"data":"secret-image-data"}]}}],"leafId":"before-leaf"}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"user","content":"historical-secret-prompt"}},{"type":"message","id":"turn-user","parentId":"before-leaf","message":{"role":"user","content":"current-secret-prompt"}}],"leafId":"turn-user"}}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, _, _ := newSingleProcessCaptureClient(script)

	var mu sync.Mutex
	var got []string
	client.rawSink = func(line []byte) {
		mu.Lock()
		got = append(got, string(line))
		mu.Unlock()
	}

	stream, err := client.Stream(context.Background(), "current-secret-prompt", RunCaptureUserAnchor())
	require.NoError(t, err)
	for stream.Next() {
	}

	mu.Lock()
	joined := strings.Join(got, "\n")
	mu.Unlock()
	assert.NotContains(t, joined, `"command":"get_entries"`)
	assert.NotContains(t, joined, "historical-secret-prompt")
	assert.NotContains(t, joined, "current-secret-prompt")
	assert.NotContains(t, joined, "secret-image-data")
	assert.Contains(t, joined, `"command":"prompt"`)
	assert.Contains(t, joined, `"command":"get_session_stats"`)
}
