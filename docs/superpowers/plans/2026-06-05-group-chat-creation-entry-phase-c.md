# Group Chat — Phase C: `group_invite` MCP Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the implicit `@mention` auto-recruit in group chat with an explicit, coordinator-only `group_invite` MCP tool that pulls department agents into the group.

**Architecture:** Mirror the existing `group_send` MCP-over-HTTP pipeline. The coordinator's claude-code turn gets a second MCP tool (`group_invite`) injected; calling it routes (bearer → member) to a new `group_svc.HandleInvite`, which gates on coordinator role, resolves invitees within the group's department pool, adds them via the idempotent `ensureMember`, and persists a system "X joined" message. The `@mention` recruit path is deleted. The group's `DepartmentID` (which defines the recruitable pool) is finally derived from the coordinator at create time — the deferred prerequisite.

**Tech Stack:** Go 1.26, cago, gomock + goconvey, MCP-over-HTTP (JSON-RPC), claude-code runtime adapter.

**Spec:** [`docs/superpowers/specs/2026-06-05-group-chat-creation-entry-design.md`](../specs/2026-06-05-group-chat-creation-entry-design.md) §4.5 + §3 (Phase C seam list) + §6 (Phase C test plan).

**Hard constraints (do not violate):**
- Strict TDD: write the failing test, run it, watch it fail for the right reason, then implement.
- **NEVER touch `pkg/claudecode/*`** (those are unrelated parallel dirty files). The runtime adapter we edit is `internal/pkg/agentruntime/runtimes/claudecode/session.go` — a *different* file.
- Always `git add` explicit file paths, never `git add -A`/`.`. Other dirty files in the tree (`chat-panel*`, `command-palette*`, `pkg/codex/*`, `docs/agent-backend.md`) must stay unstaged.
- Append-only error codes (iota), no renumbering. No `backendType=="claudecode"` literals.
- Run focused backend tests with `go test ./internal/...`; the repo's `make test-backend` excludes `/frontend/`.

---

## File Structure

| File | Responsibility | Change |
| --- | --- | --- |
| `internal/service/group_svc/group.go` | `CreateGroup` department derivation; `HandleInvite`; `buildGroupMCP` (coordinator tool); `buildGroupSystemPrompt` (coordinator roster) | Modify |
| `internal/service/group_svc/types.go` | `InviteResult` struct | Modify |
| `internal/service/group_svc/mcp.go` | `groupInviteToolSchema()`, `invite` callback, `tools/list` + `tools/call` routing | Modify |
| `internal/service/group_svc/ingest.go` | Delete `maybeRecruit`/`recruitableAgentByName`; `resolveMentionNames` log-only | Modify |
| `internal/pkg/code/code.go` + `zh_cn.go` + `en.go` | `GroupInviteForbidden` error code + messages | Modify |
| `internal/pkg/agentruntime/runner.go` | `MCPServerSpec.Tools []string` | Modify |
| `internal/pkg/agentruntime/runtimes/claudecode/session.go` | `buildMcpConfigJSON` reads `spec.Tools` | Modify |
| `internal/pkg/agentruntime/runtimes/claudecode/session_test.go` | update existing fixture to set `Tools` | Modify |
| `internal/service/group_svc/group_test.go` / `mcp_test.go` / `ingest_test.go` | new tests | Modify/Create |

---

## Task 1: Derive `DepartmentID` from coordinator in `CreateGroup` (Phase C prerequisite)

The recruitable pool is `agent_repo.ListByDepartment(g.DepartmentID)`. The UI always sends `DepartmentID: 0`, so without this, the pool is always empty and `group_invite` resolves nothing.

**Files:**
- Modify: `internal/service/group_svc/group.go` (CreateGroup, ~line 125-133)
- Test: `internal/service/group_svc/group_test.go`

- [ ] **Step 1: Write the failing test** — append inside the existing `func TestGroupSvc_CreateGroup_AddsInitialMembers(t *testing.T)` (it already wires `group_repo` + gateway mocks; this Convey adds an `agent_repo` mock). Add `"agentre/internal/repository/agent_repo"` and `"agentre/internal/repository/agent_repo/mock_agent_repo"` and `"agentre/internal/model/entity/agent_entity"` to the test imports if missing.

```go
	Convey("DepartmentID==0 时从协调者 agent 派生部门", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		agent_repo.RegisterAgent(agentRepo)

		// 协调者(1) 属于部门 42 → 派生到群。
		agentRepo.EXPECT().Find(gomock.Any(), int64(1)).Return(
			&agent_entity.Agent{ID: 1, DepartmentID: 42, Status: consts.ACTIVE}, nil)
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(1), capability.CapMCPTools).Return(true, nil)
		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, g *group_entity.Group) error {
				So(g.DepartmentID, ShouldEqual, 42) // ← 派生断言
				g.ID = 5
				return nil
			})
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(1)).Return(nil, nil)
		gw.EXPECT().EnsureGroupMemberSession(gomock.Any(), int64(1), int64(0), int64(5)).Return(int64(11), nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(&group_entity.Group{ID: 5}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)

		svc := group_svc.NewForTest(gw)
		_, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{
			Title: "支付小队", CoordinatorAgentID: 1, DepartmentID: 0,
		})
		So(err, ShouldBeNil)
	})
```

- [ ] **Step 2: Run the test, watch it fail**

Run: `go test ./internal/service/group_svc/ -run TestGroupSvc_CreateGroup_AddsInitialMembers`
Expected: FAIL — either `So(g.DepartmentID, ShouldEqual, 42)` fails (got 0) or an "unexpected call to AgentRepo.Find" because CreateGroup never looks up the coordinator.

- [ ] **Step 3: Implement** — in `internal/service/group_svc/group.go`, replace the start of `CreateGroup` (the `g := &group_entity.Group{...}` block) so the department is derived BEFORE constructing the group:

```go
func (s *groupSvc) CreateGroup(ctx context.Context, req *CreateGroupRequest) (*GroupDetail, error) {
	departmentID := req.DepartmentID
	if departmentID == 0 {
		// 从协调者 agent 派生部门：决定 group_invite 的可招募池(部门内 agent)。
		coordinator, err := agent_repo.Agent().Find(ctx, req.CoordinatorAgentID)
		if err != nil {
			return nil, err
		}
		if coordinator != nil {
			departmentID = coordinator.DepartmentID
		}
	}
	g := &group_entity.Group{
		Title:              req.Title,
		CoordinatorAgentID: req.CoordinatorAgentID,
		DepartmentID:       departmentID,
		ProjectID:          req.ProjectID,
		RunStatus:          group_entity.RunIdle,
		Status:             consts.ACTIVE,
	}
```

(The file already imports `agent_repo` via `ingest.go` in the same package; if `group.go` itself lacks the import, add `"agentre/internal/repository/agent_repo"`.)

- [ ] **Step 4: Run the test, watch it pass**

Run: `go test ./internal/service/group_svc/ -run TestGroupSvc_CreateGroup`
Expected: PASS (all CreateGroup subtests, including the existing ones — they pass `DepartmentID: 0` but their coordinator `Find` now must be stubbed; **if a pre-existing CreateGroup test now fails on an unexpected `AgentRepo.Find` call, add `agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl); agent_repo.RegisterAgent(agentRepo); agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).Return(&agent_entity.Agent{Status: consts.ACTIVE}, nil).AnyTimes()` to that test's setup**).

- [ ] **Step 5: Run race + lint, then commit**

```bash
go test -race ./internal/service/group_svc/
golangci-lint run ./internal/service/group_svc/...
git add internal/service/group_svc/group.go internal/service/group_svc/group_test.go
git commit -m "✨ group_svc: CreateGroup 从协调者派生 DepartmentID(group_invite 招募池前置)"
```

---

## Task 2: `HandleInvite` service method + `GroupInviteForbidden` code

The core service entry the MCP tool calls. Coordinator-gated; resolves invitees within the department pool; idempotent add; system message; `maxMembers` enforced.

**Files:**
- Modify: `internal/pkg/code/code.go`, `internal/pkg/code/zh_cn.go`, `internal/pkg/code/en.go`
- Modify: `internal/service/group_svc/types.go` (InviteResult)
- Modify: `internal/service/group_svc/group.go` (HandleInvite)
- Test: `internal/service/group_svc/group_test.go`

- [ ] **Step 1: Add the error code** (no separate test — exercised by Step 3's non-coordinator test).

In `internal/pkg/code/code.go`, inside the `Group 群聊编排 19000~19999` const block, append after `GroupBackendUnsupported`:

```go
	GroupBackendUnsupported                 // 该 agent 的后端不支持群聊(缺 CapMCPTools)
	GroupInviteForbidden                    // 非协调者调用 group_invite / 被邀请人不在招募池
```

In `internal/pkg/code/zh_cn.go`, after the `GroupBackendUnsupported:` line:

```go
	GroupInviteForbidden:     "只有协调者能邀请成员",
```

In `internal/pkg/code/en.go`, after the `GroupBackendUnsupported:` line:

```go
	GroupInviteForbidden:     "Only the coordinator can invite members",
```

- [ ] **Step 2: Add `InviteResult`** to `internal/service/group_svc/types.go`:

```go
// InviteResult 是 group_invite 成功拉入的一个成员(id + 显示名),回给协调者 turn。
type InviteResult struct {
	AgentID int64
	Name    string
}
```

- [ ] **Step 3: Write the failing test** — append to `internal/service/group_svc/group_test.go`. Three Convey blocks: coordinator success, non-coordinator forbidden, pool-outside skip.

```go
func TestGroupSvc_HandleInvite(t *testing.T) {
	Convey("协调者邀请部门内 agent → 入群 + 落 system 消息 + 返回结果", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		agent_repo.RegisterAgent(agentRepo)

		// caller(member 100, agent 1) 是协调者。
		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(
			&group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive}, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(
			&group_entity.Group{ID: 5, DepartmentID: 42, Status: consts.ACTIVE}, nil)
		// 已有成员:仅协调者(未满)。
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(
			[]*group_entity.GroupMember{{ID: 100, AgentID: 1, Role: group_entity.RoleCoordinator}}, nil).AnyTimes()
		// 部门 42 的招募池含 agent 2(Bob)。
		agentRepo.EXPECT().ListByDepartment(gomock.Any(), int64(42)).Return(
			[]*agent_entity.Agent{{ID: 2, Name: "Bob", Status: consts.ACTIVE}}, nil)
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(2), capability.CapMCPTools).Return(true, nil)
		// ensureMember(2) → 新建。
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(2)).Return(nil, nil)
		gw.EXPECT().EnsureGroupMemberSession(gomock.Any(), int64(2), int64(0), int64(5)).Return(int64(22), nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		// system "Bob 加入了群聊" 消息落库(seq + create)。
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.SenderKind, ShouldEqual, group_entity.SenderKindSystem)
				return nil
			})

		svc := group_svc.NewForTest(gw)
		results, err := svc.HandleInvite(ctx, 100, []string{"Bob"}, nil, "需要后端支援")
		So(err, ShouldBeNil)
		So(len(results), ShouldEqual, 1)
		So(results[0].AgentID, ShouldEqual, 2)
		So(results[0].Name, ShouldEqual, "Bob")
	})

	Convey("非协调者调用 → GroupInviteForbidden", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterMember(memberRepo)

		memberRepo.EXPECT().Find(gomock.Any(), int64(101)).Return(
			&group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 9, Role: group_entity.RoleMember, Status: group_entity.MemberActive}, nil)

		svc := group_svc.NewForTest(gw)
		_, err := svc.HandleInvite(ctx, 101, []string{"Bob"}, nil, "")
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupInviteForbidden)
	})

	Convey("被邀请人不在部门招募池 → 跳过,返回空", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		agent_repo.RegisterAgent(agentRepo)

		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(
			&group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive}, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(
			&group_entity.Group{ID: 5, DepartmentID: 42, Status: consts.ACTIVE}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(
			[]*group_entity.GroupMember{{ID: 100, AgentID: 1, Role: group_entity.RoleCoordinator}}, nil).AnyTimes()
		agentRepo.EXPECT().ListByDepartment(gomock.Any(), int64(42)).Return(
			[]*agent_entity.Agent{{ID: 2, Name: "Bob", Status: consts.ACTIVE}}, nil)

		svc := group_svc.NewForTest(gw)
		results, err := svc.HandleInvite(ctx, 100, []string{"Stranger"}, nil, "")
		So(err, ShouldBeNil)
		So(len(results), ShouldEqual, 0)
	})
}
```

- [ ] **Step 4: Run the test, watch it fail**

Run: `go test ./internal/service/group_svc/ -run TestGroupSvc_HandleInvite`
Expected: FAIL — `svc.HandleInvite` undefined / `code.GroupInviteForbidden` undefined.

- [ ] **Step 5: Implement `HandleInvite`** in `internal/service/group_svc/group.go`:

```go
// HandleInvite 是 group_invite MCP tool 的服务端入口:协调者把部门招募池内的 agent 拉进群。
// callerMemberID=调用成员(必须是协调者); names/ids 二选一指定被邀请 agent; reason 仅日志。
// 逐个经 backendSupportsGroup + maxMembers 门控,幂等 ensureMember,落 system "X 加入了群聊"。
func (s *groupSvc) HandleInvite(ctx context.Context, callerMemberID int64, names []string, ids []int64, reason string) ([]InviteResult, error) {
	caller, err := group_repo.Member().Find(ctx, callerMemberID)
	if err != nil || caller == nil {
		return nil, i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	if !caller.IsCoordinator() {
		return nil, i18n.NewError(ctx, code.GroupInviteForbidden)
	}
	mu := s.ingestMu(caller.GroupID)
	mu.Lock()
	defer mu.Unlock()

	g, err := group_repo.Group().Find(ctx, caller.GroupID)
	if err != nil || g == nil {
		return nil, i18n.NewError(ctx, code.GroupNotFound)
	}
	pool, err := agent_repo.Agent().ListByDepartment(ctx, g.DepartmentID)
	if err != nil {
		return nil, err
	}
	// 在招募池内按 id / 名解析(去重)。
	wantIDs := map[int64]bool{}
	for _, id := range ids {
		wantIDs[id] = true
	}
	wantNames := map[string]bool{}
	for _, n := range names {
		wantNames[n] = true
	}
	var targets []*agent_entity.Agent
	seen := map[int64]bool{}
	for _, a := range pool {
		if !a.IsActive() || seen[a.ID] {
			continue
		}
		if wantIDs[a.ID] || wantNames[a.Name] {
			seen[a.ID] = true
			targets = append(targets, a)
		}
	}

	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	memberCount := len(members)
	results := []InviteResult{}
	for _, a := range targets {
		if a.ID == g.CoordinatorAgentID {
			continue
		}
		if memberCount >= maxMembers {
			return nil, i18n.NewError(ctx, code.GroupMemberLimit)
		}
		if !s.backendSupportsGroup(ctx, a.ID) {
			logger.Ctx(ctx).Info("group_svc.HandleInvite: backend lacks CapMCPTools, skip",
				zap.Int64("agentId", a.ID), zap.String("reason", reason))
			continue
		}
		m, err := s.ensureMember(ctx, g, a.ID, group_entity.RoleMember)
		if err != nil {
			return nil, err
		}
		if !m.IsActive() {
			continue
		}
		memberCount++
		if _, err := s.persistMessage(ctx, g, group_entity.SenderKindSystem, 0, a.Name+" 加入了群聊", nil, false, 0); err != nil {
			logger.Ctx(ctx).Warn("group_svc.HandleInvite: system message persist failed", zap.Error(err))
		}
		results = append(results, InviteResult{AgentID: a.ID, Name: a.Name})
	}
	logger.Ctx(ctx).Info("group_svc.HandleInvite: invited",
		zap.Int64("groupId", g.ID), zap.Int("count", len(results)))
	return results, nil
}
```

Ensure `group.go` imports `"agentre/internal/model/entity/agent_entity"` (for the targets slice type) — add it if missing.

- [ ] **Step 6: Run the test, watch it pass**

Run: `go test ./internal/service/group_svc/ -run TestGroupSvc_HandleInvite`
Expected: PASS (3 subtests).

- [ ] **Step 7: Run race + lint + i18n completeness, then commit**

```bash
go test -race ./internal/service/group_svc/ ./internal/pkg/code/
golangci-lint run ./internal/service/group_svc/... ./internal/pkg/code/...
git add internal/pkg/code/code.go internal/pkg/code/zh_cn.go internal/pkg/code/en.go internal/service/group_svc/group.go internal/service/group_svc/types.go internal/service/group_svc/group_test.go
git commit -m "✨ group_svc: HandleInvite(协调者门控+部门池解析+幂等入群+system 消息)"
```

---

## Task 3: `group_invite` MCP tool schema + routing

Wire the tool into the MCP-over-HTTP handler so a coordinator's claude turn can call it.

**Files:**
- Modify: `internal/service/group_svc/mcp.go`
- Modify: `internal/service/group_svc/group.go` (`newGroupSvc` wiring)
- Test: `internal/service/group_svc/mcp_test.go`

- [ ] **Step 1: Write the failing test** — append to `internal/service/group_svc/mcp_test.go` (look at the existing tests there for the exact request-construction helper; mirror it). Two cases: `tools/list` advertises `group_invite`; `tools/call` for `group_invite` routes to the invite callback with parsed args.

```go
func TestGroupMCP_ToolsList_IncludesInvite(t *testing.T) {
	Convey("tools/list 同时广告 group_send 与 group_invite", t, func() {
		h := newGroupMCP(nil)
		rr := httptest.NewRecorder()
		body := `{"id":1,"method":"tools/list"}`
		req := httptest.NewRequest(http.MethodPost, "/mcp/group/", strings.NewReader(body))
		h.ServeHTTP(rr, req)
		So(rr.Body.String(), ShouldContainSubstring, "group_send")
		So(rr.Body.String(), ShouldContainSubstring, "group_invite")
	})
}

func TestGroupMCP_ToolsCall_RoutesInvite(t *testing.T) {
	Convey("tools/call group_invite → invite 回调收到解析后的 names/ids/reason", t, func() {
		var gotMember int64
		var gotNames []string
		var gotReason string
		h := newGroupMCP(nil)
		h.invite = func(_ context.Context, memberID int64, names []string, ids []int64, reason string) ([]InviteResult, error) {
			gotMember, gotNames, gotReason = memberID, names, reason
			return []InviteResult{{AgentID: 2, Name: "Bob"}}, nil
		}
		tok := h.MintToken(5, 100)
		rr := httptest.NewRecorder()
		body := `{"id":1,"method":"tools/call","params":{"name":"group_invite","arguments":{"agentNames":["Bob"],"reason":"需要支援"}}}`
		req := httptest.NewRequest(http.MethodPost, "/mcp/group/", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		h.ServeHTTP(rr, req)
		So(gotMember, ShouldEqual, 100)
		So(gotNames, ShouldResemble, []string{"Bob"})
		So(gotReason, ShouldEqual, "需要支援")
		So(rr.Body.String(), ShouldContainSubstring, "Bob")
	})
}
```

(If `mcp_test.go` does not exist, create it with package `group_svc` and imports: `context`, `net/http`, `net/http/httptest`, `strings`, `testing`, and goconvey `.`.)

- [ ] **Step 2: Run the test, watch it fail**

Run: `go test ./internal/service/group_svc/ -run TestGroupMCP`
Expected: FAIL — `h.invite` field undefined; `tools/list` lacks `group_invite`; `group_invite` returns "unknown tool".

- [ ] **Step 3: Implement** in `internal/service/group_svc/mcp.go`.

(a) Add the schema function:

```go
func groupInviteToolSchema() map[string]any {
	return map[string]any{
		"name":        "group_invite",
		"description": "把本部门的 Agent 拉进当前群聊。只有协调者可调用。agentNames 或 agentIds 二选一。",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agentNames": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "被邀请成员显示名"},
				"agentIds":   map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "被邀请成员 agent id"},
				"reason":     map[string]any{"type": "string", "description": "邀请理由(可选)"},
			},
		},
	}
}
```

(b) Add the `invite` callback field to the `groupMCP` struct (next to `ingest`):

```go
type groupMCP struct {
	mu     sync.Mutex
	tokens map[string]memberRef
	ingest func(ctx context.Context, memberID int64, body string, mentions []string) error
	invite func(ctx context.Context, memberID int64, names []string, ids []int64, reason string) ([]InviteResult, error)
	newTok func() string
}
```

(c) Extend the decoded `Arguments` struct in `ServeHTTP` to carry invite fields (alongside `Body`/`Mentions`):

```go
			Arguments       struct {
				Body       string   `json:"body"`
				Mentions   []string `json:"mentions"`
				AgentNames []string `json:"agentNames"`
				AgentIDs   []int64  `json:"agentIds"`
				Reason     string   `json:"reason"`
			} `json:"arguments"`
```

(d) `tools/list` advertises both:

```go
	case "tools/list":
		writeRPCResult(w, rpc.ID, map[string]any{"tools": []any{groupSendToolSchema(), groupInviteToolSchema()}})
```

(e) In `tools/call`, after the bearer lookup, replace the single-tool `if rpc.Params.Name != "group_send"` guard with a switch over both tools:

```go
	case "tools/call":
		ref, ok := h.lookup(bearer(r))
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch rpc.Params.Name {
		case "group_send":
			if h.ingest == nil {
				writeRPCError(w, rpc.ID, -32000, "ingest not wired")
				return
			}
			if err := h.ingest(r.Context(), ref.memberID, rpc.Params.Arguments.Body, rpc.Params.Arguments.Mentions); err != nil {
				writeRPCError(w, rpc.ID, -32000, err.Error())
				return
			}
			writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": "sent"}}})
		case "group_invite":
			if h.invite == nil {
				writeRPCError(w, rpc.ID, -32000, "invite not wired")
				return
			}
			results, err := h.invite(r.Context(), ref.memberID, rpc.Params.Arguments.AgentNames, rpc.Params.Arguments.AgentIDs, rpc.Params.Arguments.Reason)
			if err != nil {
				writeRPCError(w, rpc.ID, -32000, err.Error())
				return
			}
			names := make([]string, 0, len(results))
			for _, x := range results {
				names = append(names, x.Name)
			}
			writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": "invited: " + strings.Join(names, ", ")}}})
		default:
			writeRPCError(w, rpc.ID, -32601, "unknown tool")
		}
```

(f) Wire the callback in `newGroupSvc` (`internal/service/group_svc/group.go`), next to `s.mcp.ingest = s.IngestAgentMessage`:

```go
	s.mcp.ingest = s.IngestAgentMessage
	s.mcp.invite = s.HandleInvite
```

- [ ] **Step 4: Run the test, watch it pass**

Run: `go test ./internal/service/group_svc/ -run TestGroupMCP`
Expected: PASS.

- [ ] **Step 5: Run race + lint, then commit**

```bash
go test -race ./internal/service/group_svc/
golangci-lint run ./internal/service/group_svc/...
git add internal/service/group_svc/mcp.go internal/service/group_svc/group.go internal/service/group_svc/mcp_test.go
git commit -m "✨ group_svc(mcp): group_invite tool schema + tools/call 路由 → HandleInvite"
```

---

## Task 4: Coordinator-only tool injection (`MCPServerSpec.Tools`)

Make `buildGroupMCP` declare which tools a member's turn may call; only the coordinator gets `group_invite`. The runtime adapter reads that list instead of hardcoding `group_send`.

**Files:**
- Modify: `internal/pkg/agentruntime/runner.go` (MCPServerSpec)
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/session.go` (buildMcpConfigJSON)
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/session_test.go` (existing fixture)
- Modify: `internal/service/group_svc/group.go` (buildGroupMCP)
- Test: `internal/service/group_svc/group_test.go` (buildGroupMCP role gating)

- [ ] **Step 1: Add the `Tools` field** to `internal/pkg/agentruntime/runner.go` `MCPServerSpec`:

```go
type MCPServerSpec struct {
	Name    string            // server 名; claude tool 暴露为 mcp__<Name>__<tool>
	URL     string            // http MCP endpoint(如 http://127.0.0.1:<port>/mcp/group/)
	Headers map[string]string // 鉴权/scope header(如 {"Authorization":"Bearer <token>"})
	Tools   []string          // 该 server 允许的 tool 名(进 --allowedTools 为 mcp__<Name>__<tool>)
}
```

- [ ] **Step 2: Update the existing runtime test to the new contract (failing test).** In `internal/pkg/agentruntime/runtimes/claudecode/session_test.go`, the existing spec around line 23 has no `Tools`; set it and add an assertion that a non-listed tool is NOT present. Change the `MCPServers` fixture to:

```go
				MCPServers: []agentruntime.MCPServerSpec{{
					Name:  "group",
					URL:   "http://127.0.0.1:9/mcp/group/",
					Tools: []string{"group_send", "group_invite"},
				}},
```

and add, next to the existing `ShouldContain` assertion:

```go
		Convey("Then --allowedTools 含 mcp__group__group_send", func() {
			So(c.AllowedTools(), ShouldContain, "mcp__group__group_send")
			So(c.AllowedTools(), ShouldContain, "mcp__group__group_invite")
		})
```

- [ ] **Step 3: Run the test, watch it fail**

Run: `go test ./internal/pkg/agentruntime/runtimes/claudecode/ -run TestCcBuildClientOpts` (use the actual test name in `session_test.go` — find it with `grep "func Test" internal/pkg/agentruntime/runtimes/claudecode/session_test.go`).
Expected: FAIL — `mcp__group__group_invite` not in AllowedTools (buildMcpConfigJSON still hardcodes only `group_send`).

- [ ] **Step 4: Implement** — in `internal/pkg/agentruntime/runtimes/claudecode/session.go`, change `buildMcpConfigJSON` to emit one allowed-tool per declared tool:

```go
	for _, s := range specs {
		servers[s.Name] = mcpServer{Type: "http", URL: s.URL, Headers: s.Headers}
		for _, tool := range s.Tools {
			allow = append(allow, "mcp__"+s.Name+"__"+tool)
		}
	}
```

- [ ] **Step 5: Run the runtime test, watch it pass**

Run: `go test ./internal/pkg/agentruntime/runtimes/claudecode/`
Expected: PASS.

- [ ] **Step 6: Write the failing test for `buildGroupMCP` role gating** — append to `internal/service/group_svc/group_test.go`:

```go
func TestGroupSvc_BuildGroupMCP_CoordinatorGetsInvite(t *testing.T) {
	Convey("buildGroupMCP: 协调者拿 group_send+group_invite, 普通成员只拿 group_send", t, func() {
		svc := group_svc.NewForTest(nil)
		g := &group_entity.Group{ID: 5}
		coordTools := group_svc.BuildGroupMCPTools(svc, g, &group_entity.GroupMember{ID: 1, Role: group_entity.RoleCoordinator})
		memberTools := group_svc.BuildGroupMCPTools(svc, g, &group_entity.GroupMember{ID: 2, Role: group_entity.RoleMember})
		So(coordTools, ShouldContain, "group_send")
		So(coordTools, ShouldContain, "group_invite")
		So(memberTools, ShouldContain, "group_send")
		So(memberTools, ShouldNotContain, "group_invite")
	})
}
```

Add a tiny test-only exported helper in `internal/service/group_svc/export_test_helpers.go` (or wherever `NewForTest` lives) that returns the Tools of the built spec, so the unexported `buildGroupMCP` is testable from the `group_svc` package test. Since this test is in package `group_svc_test`, instead make the assertion against `buildGroupMCP` directly by putting the test in package `group_svc` (internal test), or expose:

```go
// BuildGroupMCPTools 仅测试用:返回某成员 buildGroupMCP 出来的 server 的 Tools。
func BuildGroupMCPTools(s GroupSvc, g *group_entity.Group, m *group_entity.GroupMember) []string {
	specs := s.(*groupSvc).buildGroupMCP(g, m)
	if len(specs) == 0 {
		return nil
	}
	return specs[0].Tools
}
```

(Place this in a non-`_test.go` file guarded by nothing special, OR simpler: write `TestGroupSvc_BuildGroupMCP_CoordinatorGetsInvite` in package `group_svc` (internal) and call `s.buildGroupMCP(...)` directly. Prefer the internal-package test — no production export needed.)

**Internal-package variant (preferred):** create `internal/service/group_svc/buildmcp_internal_test.go` with `package group_svc`:

```go
package group_svc

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/model/entity/group_entity"
)

func TestBuildGroupMCP_CoordinatorGetsInvite(t *testing.T) {
	Convey("协调者 spec.Tools 含 group_invite, 普通成员不含", t, func() {
		s := newGroupSvc(nil, nil)
		g := &group_entity.Group{ID: 5}
		coord := s.buildGroupMCP(g, &group_entity.GroupMember{ID: 1, Role: group_entity.RoleCoordinator})
		member := s.buildGroupMCP(g, &group_entity.GroupMember{ID: 2, Role: group_entity.RoleMember})
		So(coord[0].Tools, ShouldContain, "group_send")
		So(coord[0].Tools, ShouldContain, "group_invite")
		So(member[0].Tools, ShouldContain, "group_send")
		So(member[0].Tools, ShouldNotContain, "group_invite")
	})
}
```

(Discard the exported-helper approach above; the internal test is cleaner.)

- [ ] **Step 7: Run, watch it fail**

Run: `go test ./internal/service/group_svc/ -run TestBuildGroupMCP`
Expected: FAIL — `coord[0].Tools` is nil (buildGroupMCP doesn't set Tools yet).

- [ ] **Step 8: Implement** — in `internal/service/group_svc/group.go`, update `buildGroupMCP`:

```go
func (s *groupSvc) buildGroupMCP(g *group_entity.Group, m *group_entity.GroupMember) []agentruntime.MCPServerSpec {
	tok := s.mcp.MintToken(g.ID, m.ID)
	tools := []string{"group_send"}
	if m.IsCoordinator() {
		tools = append(tools, "group_invite")
	}
	return []agentruntime.MCPServerSpec{{
		Name:    "group",
		URL:     s.gatewayBaseURL + "/mcp/group/",
		Headers: map[string]string{"Authorization": "Bearer " + tok},
		Tools:   tools,
	}}
}
```

- [ ] **Step 9: Run both tests, watch them pass**

Run: `go test ./internal/service/group_svc/ -run TestBuildGroupMCP && go test ./internal/pkg/agentruntime/runtimes/claudecode/`
Expected: PASS.

- [ ] **Step 10: Run race + lint, then commit**

```bash
go test -race ./internal/service/group_svc/ ./internal/pkg/agentruntime/...
golangci-lint run ./internal/service/group_svc/... ./internal/pkg/agentruntime/...
git add internal/pkg/agentruntime/runner.go internal/pkg/agentruntime/runtimes/claudecode/session.go internal/pkg/agentruntime/runtimes/claudecode/session_test.go internal/service/group_svc/group.go internal/service/group_svc/buildmcp_internal_test.go
git commit -m "✨ allowedTools 按角色:协调者 turn 才注入 mcp__group__group_invite(MCPServerSpec.Tools)"
```

---

## Task 5: Coordinator system-prompt — `group_invite` usage + recruitable roster

Tell the coordinator how to use `group_invite` and list who it can invite (department agents not yet in the group that support `CapMCPTools`).

**Files:**
- Modify: `internal/service/group_svc/group.go` (buildGroupSystemPrompt)
- Test: `internal/service/group_svc/group_test.go` (or the internal test file from Task 4)

- [ ] **Step 1: Write the failing test** — internal-package test (`package group_svc`) so it can call `buildGroupSystemPrompt` directly and stub `agent_repo`. Add to `internal/service/group_svc/buildmcp_internal_test.go`:

```go
func TestBuildGroupSystemPrompt_CoordinatorRoster(t *testing.T) {
	Convey("协调者 prompt 含 group_invite 用法 + 可招募 roster(部门内未进群的支持 agent)", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)

		// 部门 42 有 Bob(已进群) + Carol(未进群,可招募)。
		agentRepo.EXPECT().ListByDepartment(gomock.Any(), int64(42)).Return(
			[]*agent_entity.Agent{
				{ID: 2, Name: "Bob", Status: consts.ACTIVE},
				{ID: 3, Name: "Carol", Status: consts.ACTIVE},
			}, nil).AnyTimes()
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(3), capability.CapMCPTools).Return(true, nil).AnyTimes()

		s := newGroupSvc(gw, nil)
		g := &group_entity.Group{ID: 5, Title: "队", DepartmentID: 42, CoordinatorAgentID: 1}
		members := []*group_entity.GroupMember{
			{ID: 1, AgentID: 1, Role: group_entity.RoleCoordinator},
			{ID: 2, AgentID: 2, Role: group_entity.RoleMember},
		}
		coord := members[0]
		member := members[1]
		coordPrompt := s.buildGroupSystemPrompt(g, members, coord)
		memberPrompt := s.buildGroupSystemPrompt(g, members, member)
		So(coordPrompt, ShouldContainSubstring, "group_invite")
		So(coordPrompt, ShouldContainSubstring, "Carol") // 可招募
		So(memberPrompt, ShouldNotContainSubstring, "group_invite")
	})
}
```

(`s.names` resolves agent display names via `defaultNameResolver`; in this unit test the roster listing reads names from `agent_repo.ListByDepartment` results directly, so no extra name stubbing is needed for the roster line.)

- [ ] **Step 2: Run, watch it fail**

Run: `go test ./internal/service/group_svc/ -run TestBuildGroupSystemPrompt`
Expected: FAIL — prompt has the old `@mention` recruit text, not `group_invite`, and no roster.

- [ ] **Step 3: Implement** — in `internal/service/group_svc/group.go`, replace the coordinator branch in `buildGroupSystemPrompt`. Replace:

```go
	if me.IsCoordinator() {
		b.WriteString("\n作为协调者，mentions 里写一个本部门、尚未进群的同事名字即可把 ta 拉进群。")
	}
```

with:

```go
	if me.IsCoordinator() {
		b.WriteString("\n作为协调者，调用 `group_invite` 工具邀请本部门同事进群：agentNames 填显示名数组（或 agentIds 填 id），reason 可选。")
		if roster := s.recruitableRoster(context.Background(), g, members); roster != "" {
			b.WriteString("\n可招募同事：" + roster)
		}
	}
```

Add the helper at the end of `group.go`:

```go
// recruitableRoster 列出部门内、尚未进群、且后端支持 CapMCPTools 的 agent(名字·id),
// 供协调者 system prompt 提示可 group_invite 的对象。空字符串=没有可招募对象。
func (s *groupSvc) recruitableRoster(ctx context.Context, g *group_entity.Group, members []*group_entity.GroupMember) string {
	pool, err := agent_repo.Agent().ListByDepartment(ctx, g.DepartmentID)
	if err != nil {
		return ""
	}
	inGroup := map[int64]bool{}
	for _, m := range members {
		inGroup[m.AgentID] = true
	}
	var parts []string
	for _, a := range pool {
		if !a.IsActive() || inGroup[a.ID] {
			continue
		}
		if !s.backendSupportsGroup(ctx, a.ID) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s(id=%d)", a.Name, a.ID))
	}
	return strings.Join(parts, "、")
}
```

- [ ] **Step 4: Run, watch it pass**

Run: `go test ./internal/service/group_svc/ -run TestBuildGroupSystemPrompt`
Expected: PASS.

- [ ] **Step 5: Run race + lint, then commit**

```bash
go test -race ./internal/service/group_svc/
golangci-lint run ./internal/service/group_svc/...
git add internal/service/group_svc/group.go internal/service/group_svc/buildmcp_internal_test.go
git commit -m "✨ group_svc: 协调者 system prompt 增 group_invite 用法 + 可招募 roster"
```

---

## Task 6: Retire `@mention` auto-recruit

Now that `group_invite` is the explicit path, delete the implicit `@mention` recruit so a coordinator mentioning a non-member no longer silently joins them.

**Files:**
- Modify: `internal/service/group_svc/ingest.go` (delete `maybeRecruit` + `recruitableAgentByName`; `resolveMentionNames` log-only)
- Test: `internal/service/group_svc/ingest_test.go`

- [ ] **Step 1: Write the failing regression test** — create `internal/service/group_svc/ingest_test.go` (package `group_svc_test`):

```go
func TestIngestAgentMessage_MentionDoesNotAutoRecruit(t *testing.T) {
	Convey("协调者 @ 一个未进群的部门同事 → 不再自动入群(需 group_invite)", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		// 注意:不注册 agent_repo / 不 EXPECT ListByDepartment → 若 ingest 仍尝试招募会 panic/fail。

		// sender(member 100, agent 1)=协调者。
		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(
			&group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive}, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(
			&group_entity.Group{ID: 5, DepartmentID: 42, Status: consts.ACTIVE}, nil)
		members := []*group_entity.GroupMember{{ID: 100, AgentID: 1, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive}}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(members, nil).AnyTimes()
		// round_count 更新 + fallback(回用户)消息落库 — 允许但不强制具体次数。
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil).AnyTimes()
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()
		// 关键:NO memberRepo.Create / NO EnsureGroupMemberSession → 一旦自动入群即测试失败。

		svc := group_svc.NewForTest(gw)
		err := svc.IngestAgentMessage(ctx, 100, "hi @Stranger", []string{"Stranger"})
		So(err, ShouldBeNil)
	})
}
```

- [ ] **Step 2: Run, watch it fail**

Run: `go test ./internal/service/group_svc/ -run TestIngestAgentMessage_MentionDoesNotAutoRecruit`
Expected: FAIL — `maybeRecruit` calls `agent_repo.Agent().ListByDepartment` (agent_repo not registered → nil deref / unexpected mock call), proving the auto-recruit path still runs.

- [ ] **Step 3: Implement the retirement** — in `internal/service/group_svc/ingest.go`:

(a) Delete the entire `maybeRecruit` function (the `func (s *groupSvc) maybeRecruit(...)` block).

(b) Delete the entire `recruitableAgentByName` function.

(c) In `resolveMentionNames`, replace the `case sender.IsCoordinator():` branch (which calls `maybeRecruit`) so the coordinator case folds into log-only. The `switch` becomes:

```go
	for _, name := range names {
		switch {
		case name == "用户" || name == "你":
			toUser = true
		case byName[name] != 0 && byName[name] != sender.ID:
			ids = append(ids, byName[name])
		case byName[name] == sender.ID:
			// 自己 mention 自己 → 忽略
		default:
			// 未进群的名字不再自动招募;协调者改用 group_invite 工具。仅记日志。
			logger.Ctx(ctx).Info("group_svc.resolveMentionNames: unresolved mention (use group_invite)",
				zap.String("name", name), zap.Int64("groupId", g.ID))
		}
	}
```

(d) If deleting the functions leaves `agent_repo` unused in `ingest.go`, remove the now-unused import (the compiler will flag it). **Note:** `agent_repo` is still used by `group.go` (Tasks 1, 2, 5), so the package still imports it elsewhere — only remove from `ingest.go`'s import block if `ingest.go` had its own import line and no longer references it.

- [ ] **Step 4: Run, watch it pass**

Run: `go test ./internal/service/group_svc/ -run TestIngestAgentMessage_MentionDoesNotAutoRecruit`
Expected: PASS.

- [ ] **Step 5: Run the whole package + race + lint, then commit**

```bash
go test -race ./internal/service/group_svc/
golangci-lint run ./internal/service/group_svc/...
git add internal/service/group_svc/ingest.go internal/service/group_svc/ingest_test.go
git commit -m "♻️ group_svc: 退役 @mention 自动招募(改 group_invite),mention 未解析仅记日志"
```

---

## Task 7: Full Phase C verification

**Files:** none (verification only)

- [ ] **Step 1: Backend race tests (touched packages)**

Run:
```bash
go test -race ./internal/service/group_svc/... ./internal/pkg/agentruntime/... ./internal/pkg/code/... ./internal/app/...
```
Expected: all `ok`.

- [ ] **Step 2: golangci-lint v2**

Run:
```bash
golangci-lint run ./internal/service/group_svc/... ./internal/pkg/agentruntime/... ./internal/pkg/code/...
```
Expected: `0 issues.`

- [ ] **Step 3: Confirm no `pkg/claudecode/*` or other parallel dirty files were staged**

Run: `git status --short`
Expected: the parallel dirty files (`chat-panel*`, `command-palette*`, `pkg/claudecode/*`, `pkg/codex/*`, `docs/agent-backend.md`, `background-tasks-panel-design.md`) are still listed as **unstaged/modified** and were never committed in Phase C.

- [ ] **Step 4: Grep that the retired symbols are gone**

Run: `grep -rn "maybeRecruit\|recruitableAgentByName" internal/`
Expected: no matches (fully removed).

---

## Self-Review (spec coverage)

| Spec §4.5 / §3 requirement | Task |
| --- | --- |
| `group_invite` tool schema (`agentNames?`/`agentIds?`/`reason?`) | Task 3 |
| `ServeHTTP` invite branch (bearer → member → invite callback) | Task 3 |
| `HandleInvite`: coordinator gating → pool resolve → backend+maxMembers gate → `ensureMember` → system "X 加入了群聊" → return id+name | Task 2 |
| `GroupInviteForbidden` error code + i18n | Task 2 |
| allowedTools: `group_send` all members, `group_invite` coordinator-only | Task 4 |
| `buildGroupSystemPrompt`: coordinator suffix + recruitable roster | Task 5 |
| Retire `@mention` recruit (`maybeRecruit`/`recruitableAgentByName`); `resolveMentionNames` log-only; preserve `applyFallback`/`lastSenderMemberID` | Task 6 |
| Department derivation (recruit-pool prerequisite) | Task 1 |
| `make test-backend` race + golangci-lint 0 issues | Task 7 |

**Out of scope (per spec §8):** roster manual-invite picker wiring, command-palette "new group" source, in-transcript tool approval. No frontend changes in Phase C.

**Type consistency check:** `HandleInvite(ctx, callerMemberID int64, names []string, ids []int64, reason string) ([]InviteResult, error)` — same signature used in the `groupMCP.invite` field (Task 3), the `s.mcp.invite = s.HandleInvite` wiring (Task 3), and the test (Task 2). `InviteResult{AgentID int64; Name string}` consistent across Tasks 2–3. `MCPServerSpec.Tools []string` consistent across Tasks 4. `buildGroupMCP` returns `[]agentruntime.MCPServerSpec` with `Tools` set (Task 4) and is read by `buildMcpConfigJSON` (Task 4).
