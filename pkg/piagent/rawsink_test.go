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

// TestStream_RawSinkReceivesFrames 校验 pi-agent 子进程每行原始 stdout(未解析的
// JSON-RPC 帧)都喂给 rawSink —— debug 级原始帧转储底座。走 Text → Stream → startRPC
// 真实注入路径,而非直接 newStream,确保 Client.rawSink 真的接到 rpcProcess。
func TestStream_RawSinkReceivesFrames(t *testing.T) {
	runner := &fakeRunner{process: newFakeProcess(t)}
	runner.process.stdout = strings.NewReader(strings.Join([]string{
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
