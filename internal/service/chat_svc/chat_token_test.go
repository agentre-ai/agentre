package chat_svc

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/httpgateway"
)

// recordingGateway is a TokenIssuer that records the TTLs it was asked to issue
// with, hands back a distinct token per call, and records revocations — enough
// to assert the chat hook token is permanent + stable per session.
type recordingGateway struct {
	mu      sync.Mutex
	issued  int
	ttls    []time.Duration
	revoked []string
}

func (g *recordingGateway) IssueToken(_ context.Context, _ *agent_backend_entity.AgentBackend, ttl time.Duration) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.issued++
	g.ttls = append(g.ttls, ttl)
	return fmt.Sprintf("tok-%d", g.issued), nil
}
func (g *recordingGateway) RevokeToken(t string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.revoked = append(g.revoked, t)
}
func (g *recordingGateway) URL() string { return "http://gw" }
func (g *recordingGateway) Status() httpgateway.GatewayStatus {
	return httpgateway.GatewayStatus{State: "running"}
}

func claudeBackendFixture() *agent_backend_entity.AgentBackend {
	return &agent_backend_entity.AgentBackend{ID: 7, Type: string(agent_backend_entity.TypeClaudeCode)}
}

// TestSignChatTokenFor_PermanentAndStablePerSession reproduces the root cause of
// "steer 等整轮才发出去 on long sessions": the gateway token is baked into the
// persistent claude subprocess at spawn and never refreshed on turn reuse, so it
// MUST be permanent (ttl=0) and stable across turns. A finite TTL (the old
// 15-minute chatTokenTTL) expires while the subprocess is still alive → the
// PostToolUse hook gets 401 on /hook/v1/inbox → the SteerInbox is never drained
// mid-turn.
func TestSignChatTokenFor_PermanentAndStablePerSession(t *testing.T) {
	gw := &recordingGateway{}
	s := &chatSvc{gateway: gw}
	be := claudeBackendFixture()

	url1, tok1 := s.signChatTokenFor(context.Background(), be, 42)
	_, tok2 := s.signChatTokenFor(context.Background(), be, 42) // next turn, same session

	if url1 == "" || tok1 == "" {
		t.Fatalf("expected signed url+token, got url=%q tok=%q", url1, tok1)
	}
	if tok1 != tok2 {
		t.Fatalf("hook token must be STABLE across turns for one session: turn1=%q turn2=%q", tok1, tok2)
	}
	if gw.issued != 1 {
		t.Fatalf("must mint exactly ONE token per session (reuse across turns); minted %d", gw.issued)
	}
	for _, ttl := range gw.ttls {
		if ttl != 0 {
			t.Fatalf("hook token must be PERMANENT (ttl=0) to outlive the cached subprocess; got ttl=%v", ttl)
		}
	}
}

// TestSignChatTokenFor_DistinctPerSession guards that the per-session cache keys
// correctly — different sessions must not share a token.
func TestSignChatTokenFor_DistinctPerSession(t *testing.T) {
	gw := &recordingGateway{}
	s := &chatSvc{gateway: gw}
	be := claudeBackendFixture()

	_, a := s.signChatTokenFor(context.Background(), be, 1)
	_, b := s.signChatTokenFor(context.Background(), be, 2)
	if a == b {
		t.Fatalf("distinct sessions must get distinct tokens: s1=%q s2=%q", a, b)
	}
}

// TestRevokeChatToken_RevokesAndEvicts: tearing a session down (Delete →
// CloseSession) revokes its permanent token and drops the cache entry, so a
// future same-id session re-mints a fresh one (no stale-token reuse, no leak).
func TestRevokeChatToken_RevokesAndEvicts(t *testing.T) {
	gw := &recordingGateway{}
	s := &chatSvc{gateway: gw}
	be := claudeBackendFixture()

	_, tok := s.signChatTokenFor(context.Background(), be, 42)
	s.revokeChatToken(42)

	if len(gw.revoked) != 1 || gw.revoked[0] != tok {
		t.Fatalf("revokeChatToken must revoke session token %q; revoked=%v", tok, gw.revoked)
	}
	_, tok2 := s.signChatTokenFor(context.Background(), be, 42)
	if tok2 == tok {
		t.Fatalf("after revoke the re-signed token must be fresh; got same %q", tok2)
	}
	if gw.issued != 2 {
		t.Fatalf("expected a re-mint after revoke; issued=%d", gw.issued)
	}
}
