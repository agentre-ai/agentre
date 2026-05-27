package rpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvelope_RequestRoundTrip(t *testing.T) {
	req := Frame{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"req-1"`),
		Method:  "chat.start",
		Params:  json.RawMessage(`{"workdir":"/tmp"}`),
	}
	b, err := json.Marshal(req)
	require.NoError(t, err)

	var parsed Frame
	require.NoError(t, json.Unmarshal(b, &parsed))
	assert.Equal(t, "chat.start", parsed.Method)
	assert.True(t, parsed.IsRequest())
	assert.False(t, parsed.IsNotification())
	assert.False(t, parsed.IsResponse())
}

func TestEnvelope_NotificationHasNoID(t *testing.T) {
	n := Frame{JSONRPC: "2.0", Method: "chat.event", Params: json.RawMessage(`{}`)}
	b, _ := json.Marshal(n)
	assert.NotContains(t, string(b), `"id"`)

	var parsed Frame
	require.NoError(t, json.Unmarshal(b, &parsed))
	assert.True(t, parsed.IsNotification())
}

func TestEnvelope_Response(t *testing.T) {
	r := Frame{JSONRPC: "2.0", ID: json.RawMessage(`42`), Result: json.RawMessage(`true`)}
	assert.True(t, r.IsResponse())

	e := Frame{JSONRPC: "2.0", ID: json.RawMessage(`42`), Error: &Error{Code: -32601, Message: "Method not found"}}
	assert.True(t, e.IsResponse())
	assert.True(t, e.IsError())
}

func TestStdErrors(t *testing.T) {
	assert.Equal(t, -32601, ErrMethodNotFound.Code)
	assert.Equal(t, -32001, ErrUnauthorized.Code)
	assert.Equal(t, -32004, ErrPairing.Code)
}
