package claudecode

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseControlRequest(t *testing.T) {
	t.Run("can_use_tool decodes id/name/input", func(t *testing.T) {
		line := []byte(`{"type":"control_request","request_id":"req-abc","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"Q?","options":[{"label":"x"}]}]}}}`)
		got, ok := parseControlRequest(line)
		require.True(t, ok)
		require.NotNil(t, got)
		assert.Equal(t, "req-abc", got.RequestID)
		assert.Equal(t, "AskUserQuestion", got.ToolName)
		assert.NotEmpty(t, got.Input)

		var input map[string]any
		require.NoError(t, json.Unmarshal(got.Input, &input))
		_, has := input["questions"]
		assert.True(t, has)
	})

	t.Run("non can_use_tool subtype ignored", func(t *testing.T) {
		// outgoing interrupt control_response from us echoed back — not our concern.
		line := []byte(`{"type":"control_request","request_id":"r1","request":{"subtype":"interrupt"}}`)
		got, ok := parseControlRequest(line)
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("missing request_id rejected", func(t *testing.T) {
		line := []byte(`{"type":"control_request","request":{"subtype":"can_use_tool","tool_name":"X","input":{}}}`)
		got, ok := parseControlRequest(line)
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("malformed JSON rejected", func(t *testing.T) {
		got, ok := parseControlRequest([]byte(`{not json`))
		assert.False(t, ok)
		assert.Nil(t, got)
	})
}

func TestSessionRespondToControl_AllowWithUpdatedInput(t *testing.T) {
	pr, pw := io.Pipe()
	captured := captureStdin(t, pr)
	s := &Session{proc: &process{stdin: pw}}

	go func() {
		err := s.RespondToControl(context.Background(), "req-abc", PermissionResult{
			Behavior: "allow",
			UpdatedInput: map[string]any{
				"questions": []any{map[string]any{"question": "Q?"}},
				"answers":   map[string]any{"Q?": "X"},
			},
		})
		assert.NoError(t, err)
		_ = pw.Close()
	}()

	line := <-captured

	var env map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &env))
	assert.Equal(t, "control_response", env["type"])

	resp, ok := env["response"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "success", resp["subtype"])
	assert.Equal(t, "req-abc", resp["request_id"])

	inner, ok := resp["response"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "allow", inner["behavior"])

	updated, ok := inner["updatedInput"].(map[string]any)
	require.True(t, ok)
	answers, ok := updated["answers"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "X", answers["Q?"])
}

func TestSessionRespondToControl_DenyWithMessage(t *testing.T) {
	pr, pw := io.Pipe()
	captured := captureStdin(t, pr)
	s := &Session{proc: &process{stdin: pw}}

	go func() {
		err := s.RespondToControl(context.Background(), "req-deny", PermissionResult{
			Behavior: "deny",
			Message:  "user skipped",
		})
		assert.NoError(t, err)
		_ = pw.Close()
	}()

	line := <-captured

	var env map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &env))
	resp := env["response"].(map[string]any)
	inner := resp["response"].(map[string]any)
	assert.Equal(t, "deny", inner["behavior"])
	assert.Equal(t, "user skipped", inner["message"])
	_, hasUpdated := inner["updatedInput"]
	assert.False(t, hasUpdated, "deny should not carry updatedInput")
}

func TestSessionRespondToControl_AllowWithUpdatedPermissions(t *testing.T) {
	pr, pw := io.Pipe()
	captured := captureStdin(t, pr)
	s := &Session{proc: &process{stdin: pw}}

	go func() {
		err := s.RespondToControl(context.Background(), "req-perm", PermissionResult{
			Behavior:     "allow",
			UpdatedInput: map[string]any{"command": "ls"},
			UpdatedPermissions: []PermissionUpdate{
				{
					Type:        "addRules",
					Rules:       []PermissionRule{{ToolName: "Bash"}},
					Behavior:    "allow",
					Destination: "session",
				},
			},
		})
		assert.NoError(t, err)
		_ = pw.Close()
	}()

	line := <-captured
	var env map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &env))
	inner := env["response"].(map[string]any)["response"].(map[string]any)
	assert.Equal(t, "allow", inner["behavior"])

	perms, ok := inner["updatedPermissions"].([]any)
	require.True(t, ok)
	require.Len(t, perms, 1)
	p := perms[0].(map[string]any)
	assert.Equal(t, "addRules", p["type"])
	assert.Equal(t, "allow", p["behavior"])
	assert.Equal(t, "session", p["destination"])

	rules, ok := p["rules"].([]any)
	require.True(t, ok)
	require.Len(t, rules, 1)
	rule := rules[0].(map[string]any)
	assert.Equal(t, "Bash", rule["toolName"], "PermissionRule must serialize ToolName as camelCase toolName")
}

func TestSessionRespondToControl_AllowRequiresUpdatedInput(t *testing.T) {
	s := &Session{proc: &process{stdin: nopWriteCloser{}}}
	err := s.RespondToControl(context.Background(), "req-x", PermissionResult{Behavior: "allow"})
	assert.Error(t, err, "allow without UpdatedInput must be rejected (CLI Zod schema requires updatedInput: record)")
}

func TestSessionRespondToControl_RejectsInvalidBehavior(t *testing.T) {
	s := &Session{proc: &process{stdin: nopWriteCloser{}}}
	err := s.RespondToControl(context.Background(), "req-x", PermissionResult{Behavior: "maybe"})
	assert.Error(t, err)
}

func TestSessionRespondToControl_RejectsEmptyRequestID(t *testing.T) {
	s := &Session{proc: &process{stdin: nopWriteCloser{}}}
	err := s.RespondToControl(context.Background(), "", PermissionResult{Behavior: "allow"})
	assert.Error(t, err)
}

func TestSessionParseLine_RoutesControlRequestAsEvent(t *testing.T) {
	s := &Session{}
	line := []byte(`{"type":"control_request","request_id":"req-1","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"A?","options":[{"label":"x"}]}]}}}`)
	events, done := s.parseLine(line)
	assert.False(t, done)
	require.Len(t, events, 1)
	assert.Equal(t, EventControlRequest, events[0].Kind)
	require.NotNil(t, events[0].ControlRequest)
	assert.Equal(t, "req-1", events[0].ControlRequest.RequestID)
	assert.Equal(t, "AskUserQuestion", events[0].ControlRequest.ToolName)
}

// captureStdin reads newline-delimited frames from a pipe reader and surfaces
// each as a string on the returned channel.
func captureStdin(t *testing.T, r io.Reader) <-chan string {
	t.Helper()
	out := make(chan string, 4)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(out)
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				for {
					idx := indexNewline(buf)
					if idx < 0 {
						break
					}
					out <- strings.TrimRight(string(buf[:idx]), "\r\n")
					buf = buf[idx+1:]
				}
			}
			if err != nil {
				if len(buf) > 0 {
					out <- string(buf)
				}
				return
			}
		}
	}()
	t.Cleanup(func() { wg.Wait() })
	return out
}

func indexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }
