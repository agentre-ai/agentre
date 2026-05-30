package piagent

import (
	"testing"

	"github.com/cago-frame/agents/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentre/internal/pkg/agentruntime"
	pkgpi "agentre/pkg/piagent"
)

// Pi 在 usage 事件里上报真实模型 id（如 gpt-5.5(xhigh)）。runtime 必须把它写进
// RunResult.Model，否则 chat_svc 落库的 assistant 消息模型为空（piagent 不绑
// provider，初始 result.Model 就是空串）。
func TestDrainStream_SurfacesObservedModel(t *testing.T) {
	result := &agentruntime.RunResult{}
	out := make(chan agentruntime.Event, 16)
	drainStream(&scriptStream{events: []pkgpi.Event{
		{Kind: pkgpi.EventUsage, Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 2}, Model: "gpt-5.5(xhigh)"},
		{Kind: pkgpi.EventDone},
	}}, out, result, nil)
	close(out)

	assert.Equal(t, "gpt-5.5(xhigh)", result.Model)
}

// scriptStream 把一串预置 pkgpi.Event 当成 Pi 流回放，用于驱动 drainStream。
type scriptStream struct {
	events []pkgpi.Event
	i      int
	err    error
}

func (s *scriptStream) Next() bool {
	if s.i >= len(s.events) {
		return false
	}
	s.i++
	return true
}
func (s *scriptStream) Event() pkgpi.Event { return s.events[s.i-1] }
func (s *scriptStream) SessionID() string  { return "" }
func (s *scriptStream) Err() error         { return s.err }

func collectSteerConsumed(events []pkgpi.Event, active *activeSession) []agentruntime.SteerConsumed {
	out := make(chan agentruntime.Event, 64)
	drainStream(&scriptStream{events: events}, out, &agentruntime.RunResult{}, active)
	close(out)
	var consumed []agentruntime.SteerConsumed
	for ev := range out {
		if sc, ok := ev.(agentruntime.SteerConsumed); ok {
			consumed = append(consumed, sc)
		}
	}
	return consumed
}

// 只有 Pi 真正把 steer 注入进对话（回显成 user message → EventUserMessage）才算
// consumed。助手输出的 text delta 哪怕文字恰好等于 steer 文本，也不能误判为已消费。
func TestDrainStream_AssistantTextEqualToSteerDoesNotConsume(t *testing.T) {
	active := &activeSession{pending: []agentruntime.ConsumedSteer{{QueuedID: "q1", Text: "yes"}}}

	consumed := collectSteerConsumed([]pkgpi.Event{
		{Kind: pkgpi.EventTextDelta, Text: "yes"},
		{Kind: pkgpi.EventDone},
	}, active)

	assert.Empty(t, consumed, "assistant text must not consume a pending steer")
	assert.Len(t, active.pending, 1, "pending steer stays until真正注入")
}

// EventUserMessage（steer 注入回显）命中 pending steer → emit SteerConsumed，
// 并把它从 pending 移除。
func TestDrainStream_UserEchoConsumesPendingSteer(t *testing.T) {
	active := &activeSession{pending: []agentruntime.ConsumedSteer{{QueuedID: "q1", Text: "now do X"}}}

	consumed := collectSteerConsumed([]pkgpi.Event{
		{Kind: pkgpi.EventUserMessage, Text: "now do X"},
		{Kind: pkgpi.EventTextDelta, Text: "ok"},
		{Kind: pkgpi.EventDone},
	}, active)

	require.Len(t, consumed, 1)
	require.Len(t, consumed[0].Steers, 1)
	assert.Equal(t, "q1", consumed[0].Steers[0].QueuedID)
	assert.Equal(t, "now do X", consumed[0].Steers[0].Text)
	assert.Empty(t, active.pending)
}

// 未匹配 pending 的 user echo（例如首条 prompt 回显）不产生 SteerConsumed。
func TestDrainStream_UnmatchedUserEchoIsNoop(t *testing.T) {
	active := &activeSession{}

	consumed := collectSteerConsumed([]pkgpi.Event{
		{Kind: pkgpi.EventUserMessage, Text: "original prompt"},
		{Kind: pkgpi.EventDone},
	}, active)

	assert.Empty(t, consumed)
}
