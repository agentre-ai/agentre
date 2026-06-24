package orch_svc_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/service/orch_svc"
)

func TestMCP_ToolsList(t *testing.T) {
	h := orch_svc.Default().MCPHandler()
	tok := orch_svc.Default().MintToken(2, 500)

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp/orchestrate/", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	out := rw.Body.String()
	for _, name := range []string{"dispatch", "ask", "send", "finish", "agent_list", "reply"} {
		assert.Contains(t, out, `"`+name+`"`)
	}
}

func TestMCP_RejectsBadToken(t *testing.T) {
	h := orch_svc.Default().MCPHandler()
	req := httptest.NewRequest(http.MethodPost, "/mcp/orchestrate/", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer garbage")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	assert.Equal(t, http.StatusForbidden, rw.Code)
}
