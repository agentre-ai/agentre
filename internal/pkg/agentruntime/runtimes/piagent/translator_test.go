package piagent

import (
	"errors"
	"testing"

	"github.com/cago-frame/agents/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentre/internal/pkg/agentruntime"
	pkgpi "agentre/pkg/piagent"
)

func TestTranslate_TextAndThinking(t *testing.T) {
	out, usage, err := translate(pkgpi.Event{Kind: pkgpi.EventTextDelta, Text: "hello"})
	require.NoError(t, err)
	require.Nil(t, usage)
	require.Len(t, out, 1)
	assert.Equal(t, agentruntime.TextDelta{Text: "hello"}, out[0])

	out, usage, err = translate(pkgpi.Event{Kind: pkgpi.EventThinkingDelta, Text: "think"})
	require.NoError(t, err)
	require.Nil(t, usage)
	require.Len(t, out, 1)
	assert.Equal(t, agentruntime.ThinkingDelta{Text: "think"}, out[0])
}

func TestTranslate_ToolEvents(t *testing.T) {
	out, usage, err := translate(pkgpi.Event{
		Kind: pkgpi.EventPreToolUse,
		Tool: pkgpi.ToolEvent{ID: "tool-1", Name: "bash", Input: []byte(`{"command":"pwd"}`)},
	})
	require.NoError(t, err)
	require.Nil(t, usage)
	require.Len(t, out, 1)
	call, ok := out[0].(agentruntime.ToolCall)
	require.True(t, ok)
	assert.Equal(t, "tool-1", call.ID)
	assert.Equal(t, "bash", call.Name)
	assert.JSONEq(t, `{"command":"pwd"}`, string(call.Input))

	out, usage, err = translate(pkgpi.Event{
		Kind: pkgpi.EventPostToolUse,
		Tool: pkgpi.ToolEvent{ID: "tool-1", Content: "done", IsError: true},
	})
	require.NoError(t, err)
	require.Nil(t, usage)
	require.Len(t, out, 1)
	res, ok := out[0].(agentruntime.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "tool-1", res.ToolCallID)
	assert.Equal(t, "done", res.Content)
	assert.True(t, res.IsError)
}

func TestTranslate_Usage(t *testing.T) {
	out, usage, err := translate(pkgpi.Event{
		Kind: pkgpi.EventUsage,
		Usage: provider.Usage{
			PromptTokens:        10,
			CompletionTokens:    3,
			CachedTokens:        2,
			CacheCreationTokens: 4,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, usage)
	require.Len(t, out, 1)
	update, ok := out[0].(agentruntime.UsageUpdate)
	require.True(t, ok)
	assert.Equal(t, 16, update.TotalInputTokens)
	assert.Equal(t, 3, update.Usage.CompletionTokens)
}

func TestTranslate_ContextWindow(t *testing.T) {
	out, usage, err := translate(pkgpi.Event{Kind: pkgpi.EventContextWindow, ContextWindow: 200000})
	require.NoError(t, err)
	require.Nil(t, usage)
	require.Len(t, out, 1)
	assert.Equal(t, agentruntime.ContextWindowUpdated{Tokens: 200000}, out[0])

	out, usage, err = translate(pkgpi.Event{Kind: pkgpi.EventContextWindow})
	require.NoError(t, err)
	require.Nil(t, usage)
	assert.Empty(t, out)
}

func TestTranslate_RuntimeStatusAndCompactBoundary(t *testing.T) {
	out, usage, err := translate(pkgpi.Event{Kind: pkgpi.EventRuntimeStatus, Text: "compacting"})
	require.NoError(t, err)
	require.Nil(t, usage)
	require.Len(t, out, 1)
	assert.Equal(t, agentruntime.RuntimeStatus{Status: "compacting"}, out[0])

	out, usage, err = translate(pkgpi.Event{Kind: pkgpi.EventCompactBoundary})
	require.NoError(t, err)
	require.Nil(t, usage)
	require.Len(t, out, 1)
	assert.Equal(t, agentruntime.CompactBoundary{Trigger: "manual"}, out[0])
}

func TestTranslate_ErrorAndDone(t *testing.T) {
	boom := errors.New("boom")
	out, usage, err := translate(pkgpi.Event{Kind: pkgpi.EventError, Err: boom})
	require.ErrorIs(t, err, boom)
	require.Nil(t, usage)
	assert.Empty(t, out)

	out, usage, err = translate(pkgpi.Event{Kind: pkgpi.EventDone})
	require.NoError(t, err)
	require.Nil(t, usage)
	require.Len(t, out, 1)
	_, ok := out[0].(agentruntime.Done)
	assert.True(t, ok)
}
