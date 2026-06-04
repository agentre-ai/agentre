package handlers_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"agentre/internal/daemon/handlers"
	"agentre/internal/pkg/pty"
	"agentre/pkg/agentred/protocol"

	"github.com/stretchr/testify/require"
)

// TestTerminal_Pump_EmitsBase64SurvivingJSON is the remote-side regression for the
// garbled-terminal bug: the daemon serializes TerminalDataEvent to JSON over the
// WebSocket, so a multi-byte UTF-8 char ('─' = E2 94 80) split across two PTY
// reads gets its half-rune bytes rewritten to U+FFFD if shipped as a raw string.
// The data must be base64-encoded so it survives the JSON hop intact.
func TestTerminal_Pump_EmitsBase64SurvivingJSON(t *testing.T) {
	full := []byte("─") // E2 94 80
	data := make(chan []byte, 2)
	data <- full[:1] // E2
	data <- full[1:] // 94 80
	close(data)
	exit := make(chan pty.ExitInfo, 1)
	exit <- pty.ExitInfo{Code: 0, Reason: "natural"}
	close(exit)

	rec := &recordingEmitter{}
	h := handlers.NewTerminalHandlers(&fakeTermBackend{h: &fakeTermHandle{data: data, exit: exit}}, rec)
	_, err := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})
	require.NoError(t, err)

	deadline := time.Now().Add(2 * time.Second)
	for !sawDaemonExit(rec.snapshot()) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	require.True(t, sawDaemonExit(rec.snapshot()), "daemon never emitted exit")

	var got []byte
	for _, e := range rec.snapshot() {
		if e.Name != handlers.EventNameTerminalData {
			continue
		}
		wire, err := json.Marshal(e.Payload) // what the WebSocket serializes
		require.NoError(t, err)
		var ev protocol.TerminalDataEvent
		require.NoError(t, json.Unmarshal(wire, &ev))
		dec, err := base64.StdEncoding.DecodeString(ev.Data)
		require.NoErrorf(t, err, "daemon data %q must be base64-encoded raw bytes", ev.Data)
		got = append(got, dec...)
	}
	require.Equal(t, full, got, "split multibyte char must survive the daemon→desktop JSON hop")
}
