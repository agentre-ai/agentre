package piagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamEmitsContextWindowFromSessionStats(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[]}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":1234,"contextWindow":1050000,"percent":0.12}}}`,
		"",
	}, "\n")
	client, proc := newCaptureClient(script)

	s, err := client.Stream(context.Background(), "hi")
	require.NoError(t, err)

	var kinds []EventKind
	var windows []int
	for s.Next() {
		ev := s.Event()
		kinds = append(kinds, ev.Kind)
		if ev.Kind == EventContextWindow {
			windows = append(windows, ev.ContextWindow)
		}
	}

	require.NotEmpty(t, kinds)
	assert.Equal(t, EventDone, kinds[len(kinds)-1])
	assert.Equal(t, []int{1_050_000}, windows)

	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 2)
	assert.Equal(t, "prompt", frames[0]["type"])
	assert.Equal(t, "get_session_stats", frames[1]["type"])
}

func TestCompactStreamEmitsContextWindowFromSessionStats(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"compaction_start","reason":"manual"}`,
		`{"type":"compaction_end","reason":"manual","result":{"summary":"done"}}`,
		`{"type":"response","command":"compact","success":true,"data":{"summary":"done"}}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":null,"contextWindow":200000,"percent":null}}}`,
		"",
	}, "\n")
	client, proc := newCaptureClient(script)

	s, err := client.Compact(context.Background(), "/data/pi-sessions/agentre-7.jsonl")
	require.NoError(t, err)

	var kinds []EventKind
	var windows []int
	for s.Next() {
		ev := s.Event()
		kinds = append(kinds, ev.Kind)
		if ev.Kind == EventContextWindow {
			windows = append(windows, ev.ContextWindow)
		}
	}

	assert.Contains(t, kinds, EventCompactBoundary)
	require.NotEmpty(t, kinds)
	assert.Equal(t, EventDone, kinds[len(kinds)-1])
	assert.Equal(t, []int{200_000}, windows)

	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 2)
	assert.Equal(t, "compact", frames[0]["type"])
	assert.Equal(t, "get_session_stats", frames[1]["type"])
}

func stdinFrames(t *testing.T, raw string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	frames := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var frame map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &frame))
		frames = append(frames, frame)
	}
	return frames
}
