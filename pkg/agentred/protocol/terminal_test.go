package protocol_test

import (
	"encoding/json"
	"testing"

	"agentre/pkg/agentred/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTerminalOpenParams_Roundtrip(t *testing.T) {
	in := protocol.TerminalOpenParams{
		SessionID: 42,
		Cwd:       "/home/me",
		Shell:     "/bin/zsh",
		Env:       []string{"TERM=xterm-256color"},
		Cols:      120,
		Rows:      30,
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out protocol.TerminalOpenParams
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in, out)
}

func TestTerminalExitEvent_ReasonString(t *testing.T) {
	ev := protocol.TerminalExitEvent{TerminalID: "abc", Code: 137, Reason: "killed", Msg: "sighup"}
	b, err := json.Marshal(ev)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"reason":"killed"`)
}
