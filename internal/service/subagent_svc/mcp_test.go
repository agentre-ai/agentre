package subagent_svc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/service/subagent_svc/mock_subagent_svc"
)

func TestMCP_TokenRoundTrip(t *testing.T) {
	h := newSubagentMCP(&subagentSvc{chains: map[int64][]int64{}})
	tok := h.MintToken(7, 42)
	ref, ok := h.lookup(tok)
	if !ok || ref.agentID != 7 || ref.sessionID != 42 {
		t.Fatalf("roundtrip failed: %+v ok=%v", ref, ok)
	}
	if _, ok := h.lookup(tok + "x"); ok {
		t.Fatal("tampered token should fail")
	}
}

func TestMCP_ToolsList(t *testing.T) {
	h := newSubagentMCP(&subagentSvc{chains: map[int64][]int64{}})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/mcp/subagent/", strings.NewReader(`{"id":1,"method":"tools/list"}`)))
	var resp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Result.Tools) != 2 {
		t.Fatalf("want 2 tools, got %d", len(resp.Result.Tools))
	}
}

func TestMCP_AgentList(t *testing.T) {
	ctrl := gomock.NewController(t)
	agents := mock_subagent_svc.NewMockAgentGateway(ctrl)
	svc := &subagentSvc{agents: agents, chains: map[int64][]int64{}}
	agents.EXPECT().Find(gomock.Any(), int64(7)).Return(enabledAgent(7), nil)
	agents.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{
		{ID: 1, Name: "Reviewer", Description: "审查代码"},
		{ID: 2, Name: "Writer"},
	}, nil)

	h := newSubagentMCP(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/subagent/", strings.NewReader(`{"id":1,"method":"tools/call","params":{"name":"agent_list"}}`))
	req.Header.Set("Authorization", "Bearer "+h.MintToken(7, 42))
	h.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), "Reviewer") {
		t.Fatalf("agent_list missing agent: %s", rr.Body.String())
	}
}

func TestMCP_ForbiddenWhenDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	agents := mock_subagent_svc.NewMockAgentGateway(ctrl)
	svc := &subagentSvc{agents: agents, chains: map[int64][]int64{}}
	agents.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{ID: 7}, nil)

	h := newSubagentMCP(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/subagent/", strings.NewReader(`{"id":1,"method":"tools/call","params":{"name":"agent_list"}}`))
	req.Header.Set("Authorization", "Bearer "+h.MintToken(7, 42))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rr.Code)
	}
}
