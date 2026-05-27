package chat_svc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/cago/pkg/utils/httputils"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"

	daemonrpc "agentre/internal/daemon/rpc"
	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/model/entity/project_location_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/runtimes/remote"
	"agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/project_location_repo"
	"agentre/internal/repository/project_location_repo/mock_project_location_repo"
	chatblocks "agentre/internal/service/chat_svc/blocks"
	"agentre/internal/service/remote_device_svc/mock_remote_device_svc"
)

func TestToChatMessage_BlockTypes(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		blocks.TextBlock{Text: "hello"},
		blocks.ThinkingBlock{Text: "let me think"},
		blocks.ToolUseBlock{ID: "toolu_1", Name: "shell", Input: map[string]any{"cmd": "ls"}},
		blocks.ToolResultBlock{ToolUseID: "toolu_1", Content: []blocks.ContentBlock{blocks.TextBlock{Text: "file.txt"}}},
		PlanBlock{Text: "Plan\n- [x] Inspect files"},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 5)

	assert.Equal(t, "text", cm.Blocks[0].Type)
	assert.Equal(t, "hello", cm.Blocks[0].Text)

	assert.Equal(t, "thinking", cm.Blocks[1].Type)
	assert.Equal(t, "let me think", cm.Blocks[1].Text)

	assert.Equal(t, "tool_use", cm.Blocks[2].Type)
	assert.Equal(t, "toolu_1", cm.Blocks[2].ToolUseID)
	assert.Equal(t, "shell", cm.Blocks[2].ToolName)
	assert.Equal(t, "ls", cm.Blocks[2].ToolInput["cmd"])

	assert.Equal(t, "tool_result", cm.Blocks[3].Type)
	assert.Equal(t, "toolu_1", cm.Blocks[3].ToolUseID)
	assert.Equal(t, "file.txt", cm.Blocks[3].Text)
	assert.False(t, cm.Blocks[3].IsError)

	assert.Equal(t, "plan", cm.Blocks[4].Type)
	assert.Contains(t, cm.Blocks[4].Text, "Inspect files")
}

// 历史:ToolResultMetaBlock 已整删,meta 字段改走 raw tool_result.Meta 字节透传
// (StreamToolResult 事件的 toolResultMeta 字段),不再独立 block;原先的
// TestToChatMessage_ToolResultWithMeta / OrphanToolResultMetaIsDropped 一并移除。

func TestToChatMessage_TokenFields(t *testing.T) {
	m := &chat_entity.Message{
		ID: 1, SessionID: 9, Role: "assistant", BlocksJSON: "[]",
		Model:               "claude-sonnet-4-6",
		PromptTokens:        100,
		CompletionTokens:    50,
		CachedTokens:        30,
		CacheCreationTokens: 20,
		ReasoningTokens:     10,
		DurationMs:          1234,
	}
	cm, err := toChatMessage(m)
	require.NoError(t, err)
	assert.Equal(t, 100, cm.PromptTokens)
	assert.Equal(t, 50, cm.CompletionTokens)
	assert.Equal(t, 30, cm.CachedTokens)
	assert.Equal(t, 20, cm.CacheCreationTokens)
	assert.Equal(t, 10, cm.ReasoningTokens)
	assert.Equal(t, 1234, cm.DurationMs)
}

// TestToChatMessage_NestedToolUse pins replay 把 subagent 内层 ToolUse 投影成
// type=tool_use + ParentToolCallID(json: parentToolUseId)。前端 chat.tsx
// collectChildren 据此把它从主流程移走、挂到外层 AgentSpawnCard.childBlocks。
// canonical 故意不算 —— 内层是被父 agent.spawn 包住的 step。
func TestToChatMessage_NestedToolUse(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		chatblocks.NestedToolUseBlock{
			ID:               "nested-1",
			Name:             "Read",
			Input:            map[string]any{"file_path": "/x.go"},
			ParentToolCallID: "task-outer-1",
		},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 1)
	assert.Equal(t, "tool_use", cm.Blocks[0].Type)
	assert.Equal(t, "nested-1", cm.Blocks[0].ToolUseID)
	assert.Equal(t, "Read", cm.Blocks[0].ToolName)
	assert.Equal(t, "/x.go", cm.Blocks[0].ToolInput["file_path"])
	assert.Equal(t, "task-outer-1", cm.Blocks[0].ParentToolCallID)
	assert.Nil(t, cm.Blocks[0].Canonical, "内层 step 不走 canonical 路由,由父 agent.spawn 接管")
}

// TestToChatMessage_NestedToolResult 同上,镜像 NestedToolResultBlock 路径:
// ToolUseID = ToolCallID、Content 拍平进 Text、ParentToolCallID 透传。
func TestToChatMessage_NestedToolResult(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		chatblocks.NestedToolResultBlock{
			ToolCallID:       "nested-1",
			Content:          "hello\n",
			IsError:          true,
			ParentToolCallID: "task-outer-1",
		},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 1)
	assert.Equal(t, "tool_result", cm.Blocks[0].Type)
	assert.Equal(t, "nested-1", cm.Blocks[0].ToolUseID)
	assert.Equal(t, "hello\n", cm.Blocks[0].Text)
	assert.True(t, cm.Blocks[0].IsError)
	assert.Equal(t, "task-outer-1", cm.Blocks[0].ParentToolCallID)
}

// TestToChatMessage_SkipsSubagentStateAndPermissionModeChange pins 两个无 UI 元素
// 的 ToUI block 在 replay 时被 skip(不下行成 type=unknown 让前端渲染 debug 卡)。
// SubagentStateBlock 是累计态(tokens/duration/status),前端 AgentSpawnCard
// 由外层 Task tool 的 canonical.agentSpawn 读 —— live 路径靠 dispatcher_emitter
// 注入,replay 不重算。PermissionModeChangeBlock 是审计 block。
func TestToChatMessage_SkipsSubagentStateAndPermissionModeChange(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		blocks.TextBlock{Text: "before"},
		chatblocks.SubagentStateBlock{ParentToolCallID: "outer", TotalTokens: 123, Status: "completed"},
		chatblocks.PermissionModeChangeBlock{From: "default", To: "plan", At: 1000},
		blocks.TextBlock{Text: "after"},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 2, "skip 后只剩 2 条 text,不能出现 type=unknown 兜底卡")
	assert.Equal(t, "text", cm.Blocks[0].Type)
	assert.Equal(t, "before", cm.Blocks[0].Text)
	assert.Equal(t, "text", cm.Blocks[1].Type)
	assert.Equal(t, "after", cm.Blocks[1].Text)
}

func TestToChatMessage_UnknownBlockFallback(t *testing.T) {
	m := &chat_entity.Message{ID: 1, SessionID: 9, Role: "assistant"}
	// NoticeBlock has Audience=ToUI; toChatMessage doesn't have a dedicated case for it,
	// so it should fall through to the "unknown" branch with the kind preserved.
	require.NoError(t, m.SetBlocks([]blocks.ContentBlock{
		blocks.NoticeBlock{Level: "info", Text: "hi"},
	}))

	cm, err := toChatMessage(m)
	require.NoError(t, err)
	require.Len(t, cm.Blocks, 1)
	assert.Equal(t, "unknown", cm.Blocks[0].Type)
	assert.Equal(t, "notice", cm.Blocks[0].Raw["kind"])
}

func TestAskQuestionsToDTO_PreservesRequestUserInputMetadata(t *testing.T) {
	got := askQuestionsToDTO([]agentruntime.AskQuestion{{
		ID:          "target",
		Question:    "Which target?",
		Header:      "Target",
		MultiSelect: false,
		IsOther:     true,
		IsSecret:    true,
		Options: []agentruntime.AskOption{{
			Label:       "backend",
			Description: "Backend only.",
			Preview:     "go test ./...",
		}},
	}})

	require.Len(t, got, 1)
	assert.Equal(t, "target", got[0].ID)
	assert.Equal(t, "Which target?", got[0].Question)
	assert.Equal(t, "Target", got[0].Header)
	assert.False(t, got[0].MultiSelect)
	assert.True(t, got[0].IsOther)
	assert.True(t, got[0].IsSecret)
	require.Len(t, got[0].Options, 1)
	assert.Equal(t, "backend", got[0].Options[0].Label)
	assert.Equal(t, "Backend only.", got[0].Options[0].Description)
	assert.Equal(t, "go test ./...", got[0].Options[0].Preview)
}

func TestCreatePermissionMode_DefaultFallback(t *testing.T) {
	convey.Convey("createPermissionMode 在 raw 空串时回落到 backend.DefaultPermissionMode", t, func() {
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type:                  string(agent_backend_entity.TypeClaudeCode),
			DefaultPermissionMode: "plan",
		}
		mode, err := createPermissionMode(ctx, be, "")
		assert.NoError(t, err)
		assert.Equal(t, "plan", mode)
	})

	convey.Convey("createPermissionMode 在 raw 与 backend default 都空时返回空串", t, func() {
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type: string(agent_backend_entity.TypeClaudeCode),
		}
		mode, err := createPermissionMode(ctx, be, "")
		assert.NoError(t, err)
		assert.Equal(t, "", mode)
	})

	convey.Convey("createPermissionMode 在 raw 非空时不受 backend default 干扰", t, func() {
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type:                  string(agent_backend_entity.TypeClaudeCode),
			DefaultPermissionMode: "plan",
		}
		mode, err := createPermissionMode(ctx, be, "bypassPermissions")
		assert.NoError(t, err)
		assert.Equal(t, "bypassPermissions", mode)
	})
}

// TestCreatePermissionMode_BypassDefaultStartsInPlan 覆盖 claudecode agent 配
// DefaultPermissionMode=bypassPermissions 时, 新会话以 plan 起手的派生规则。
//
// session.PermissionMode 留 plan 是为了让前端 pill 显示 Plan + 让用户先做计划,
// 真实 CLI 启动仍按 bypassPermissions(在 claudecode runtime 的 resolveLaunchMode
// 强制), 这条规则与 spawn-after SetPermissionMode 同步链共同支撑「先 plan 后
// bypass」工作流。
func TestCreatePermissionMode_BypassDefaultStartsInPlan(t *testing.T) {
	convey.Convey("Given claudecode + DefaultPermissionMode=bypass, When raw 空, Then 返 plan 起手", t, func() {
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type:                  string(agent_backend_entity.TypeClaudeCode),
			DefaultPermissionMode: "bypassPermissions",
		}
		mode, err := createPermissionMode(ctx, be, "")
		assert.NoError(t, err)
		assert.Equal(t, "plan", mode)
	})

	convey.Convey("Given claudecode + bypass default, When raw 显式非空, Then 尊重 raw 不强切 plan", t, func() {
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type:                  string(agent_backend_entity.TypeClaudeCode),
			DefaultPermissionMode: "bypassPermissions",
		}
		mode, err := createPermissionMode(ctx, be, "acceptEdits")
		assert.NoError(t, err)
		assert.Equal(t, "acceptEdits", mode)
	})

	convey.Convey("Given non-claudecode backend + bypass default, When raw 空, Then 不触发 plan 起手 (规则仅对 claudecode 生效)", t, func() {
		// codex / builtin 不应被这条规则影响; entity.Check 实际禁止非 claudecode 配
		// bypass, 这里用直接构造的实体跨过校验是为了断言推断分支的 backend 类型门禁。
		ctx := context.Background()
		be := &agent_backend_entity.AgentBackend{
			Type:                  string(agent_backend_entity.TypeCodex),
			DefaultPermissionMode: "bypassPermissions",
		}
		mode, err := createPermissionMode(ctx, be, "")
		// codex 不允许 bypassPermissions, validate 会回 ChatPermissionModeInvalid;
		// 关键是这里没有走 plan 分支, 错误从 validateRequestedPermissionMode 抛出。
		assert.Error(t, err)
		assert.Equal(t, "", mode)
	})
}

// TestResolveSessionCwd_LocalUsesCwdResolver 验证 be.IsLocal() 时走注入的 CwdResolver 回调。
func TestResolveSessionCwd_LocalUsesCwdResolver(t *testing.T) {
	prev := resolveCwdFn
	t.Cleanup(func() { resolveCwdFn = prev })
	resolveCwdFn = func(ctx context.Context, s *chat_entity.Session) (string, error) {
		return "/Users/me/proj", nil
	}
	sess := &chat_entity.Session{ID: 1, ProjectID: 10, AgentID: 7}
	be := &agent_backend_entity.AgentBackend{DeviceID: ""} // local
	cwd, err := resolveSessionCwd(context.Background(), sess, be)
	require.NoError(t, err)
	assert.Equal(t, "/Users/me/proj", cwd)
}

// TestResolveSessionCwd_NilBackendUsesCwdResolver 验证 be 为 nil 时（back-compat）也走 CwdResolver。
func TestResolveSessionCwd_NilBackendUsesCwdResolver(t *testing.T) {
	prev := resolveCwdFn
	t.Cleanup(func() { resolveCwdFn = prev })
	resolveCwdFn = func(ctx context.Context, s *chat_entity.Session) (string, error) {
		return "/local", nil
	}
	sess := &chat_entity.Session{ID: 1, ProjectID: 10}
	cwd, err := resolveSessionCwd(context.Background(), sess, nil)
	require.NoError(t, err)
	assert.Equal(t, "/local", cwd)
}

// TestResolveSessionCwd_RemoteHitsProjectLocation 验证 be.IsRemote() 时查 project_location_repo。
func TestResolveSessionCwd_RemoteHitsProjectLocation(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	prevRepo := project_location_repo.ProjectLocation()
	mockRepo := mock_project_location_repo.NewMockProjectLocationRepo(ctrl)
	project_location_repo.RegisterProjectLocation(mockRepo)
	t.Cleanup(func() { project_location_repo.RegisterProjectLocation(prevRepo) })

	mockRepo.EXPECT().FindByProjectAndDevice(gomock.Any(), int64(10), "7").Return(
		&project_location_entity.ProjectLocation{ID: 42, ProjectID: 10, DeviceID: "7", Path: "/home/me/proj"}, nil,
	)

	sess := &chat_entity.Session{ID: 1, ProjectID: 10}
	be := &agent_backend_entity.AgentBackend{DeviceID: "7"} // remote
	cwd, err := resolveSessionCwd(context.Background(), sess, be)
	require.NoError(t, err)
	assert.Equal(t, "/home/me/proj", cwd)
}

// TestResolveSessionCwd_RemoteFreeSessionSkipsRepo 验证 ProjectID=0（自由会话）+ 远端 backend
// 时直接返回 ("", nil)，把 cwd 兜底权下放给远端 daemon 的 runtime（cwd=="" → AgentCwd）。
// 关键约束：根本不能去查 project_location_repo —— mockRepo 没设 EXPECT，被调用就会 fail。
func TestResolveSessionCwd_RemoteFreeSessionSkipsRepo(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	prevRepo := project_location_repo.ProjectLocation()
	mockRepo := mock_project_location_repo.NewMockProjectLocationRepo(ctrl)
	project_location_repo.RegisterProjectLocation(mockRepo)
	t.Cleanup(func() { project_location_repo.RegisterProjectLocation(prevRepo) })

	sess := &chat_entity.Session{ID: 1, ProjectID: 0, AgentID: 7}
	be := &agent_backend_entity.AgentBackend{DeviceID: "7"} // remote
	cwd, err := resolveSessionCwd(context.Background(), sess, be)
	require.NoError(t, err)
	assert.Equal(t, "", cwd)
}

// TestResolveSessionCwd_RemoteMissingLocation 验证远端找不到记录时返回 ProjectLocationMissing 错误。
func TestResolveSessionCwd_RemoteMissingLocation(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	prevRepo := project_location_repo.ProjectLocation()
	mockRepo := mock_project_location_repo.NewMockProjectLocationRepo(ctrl)
	project_location_repo.RegisterProjectLocation(mockRepo)
	t.Cleanup(func() { project_location_repo.RegisterProjectLocation(prevRepo) })

	mockRepo.EXPECT().FindByProjectAndDevice(gomock.Any(), int64(10), "7").Return(nil, gorm.ErrRecordNotFound)

	sess := &chat_entity.Session{ID: 1, ProjectID: 10}
	be := &agent_backend_entity.AgentBackend{DeviceID: "7"}
	_, err := resolveSessionCwd(context.Background(), sess, be)
	var httpErr *httputils.Error
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, code.ProjectLocationMissing, httpErr.Code)
}

// ── noopDaemonClient ─────────────────────────────────────────────────────────

type noopDaemonClient struct{}

func (*noopDaemonClient) Call(_ context.Context, _ string, _, _ any) error { return nil }
func (*noopDaemonClient) Notify(_ string, _ any) error                     { return nil }
func (*noopDaemonClient) Handle(_ string, _ func(context.Context, json.RawMessage) (any, error)) {
}
func (*noopDaemonClient) Closed() <-chan struct{} { return nil }
func (*noopDaemonClient) Close() error            { return nil }

// recordingDaemonClient counts every Call invocation per method — used to
// assert that borrowRemoteRuntime issues exactly one runtime.capabilities
// prefetch on the cold path and zero on cache hits.
type recordingDaemonClient struct {
	mu    sync.Mutex
	calls map[string]int
}

func newRecordingDaemonClient() *recordingDaemonClient {
	return &recordingDaemonClient{calls: map[string]int{}}
}

func (c *recordingDaemonClient) Call(_ context.Context, method string, _, _ any) error {
	c.mu.Lock()
	c.calls[method]++
	c.mu.Unlock()
	return nil
}
func (*recordingDaemonClient) Notify(_ string, _ any) error { return nil }
func (*recordingDaemonClient) Handle(_ string, _ func(context.Context, json.RawMessage) (any, error)) {
}
func (*recordingDaemonClient) Closed() <-chan struct{} { return nil }
func (*recordingDaemonClient) Close() error            { return nil }

func (c *recordingDaemonClient) count(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[method]
}

// poolLeaseMocks 把 Pool/Lease/Client 三件套打包,简化各远端缓存测试的注入。
type poolLeaseMocks struct {
	pool   *mock_remote_device_svc.MockConnPool
	lease  *mock_remote_device_svc.MockLease
	client *noopDaemonClient
}

// installMockPool 构造一个 ConnPool / Lease / DaemonClientPort 三件套并注入 svc。
// Pool.Borrow 默认返同一个 Lease;Closed() 返一个永不关的 chan;Release() AnyTimes。
func installMockPool(t *testing.T, ctrl *gomock.Controller, svc *chatSvc, deviceID int64) *poolLeaseMocks {
	t.Helper()
	m := &poolLeaseMocks{
		pool:   mock_remote_device_svc.NewMockConnPool(ctrl),
		lease:  mock_remote_device_svc.NewMockLease(ctrl),
		client: &noopDaemonClient{},
	}
	m.pool.EXPECT().Borrow(gomock.Any(), deviceID).Return(m.lease, nil).AnyTimes()
	m.lease.EXPECT().Client().Return(m.client).AnyTimes()
	m.lease.EXPECT().Closed().Return(make(chan struct{})).AnyTimes()
	m.lease.EXPECT().Release().AnyTimes()
	svc.setConnPoolForTest(m.pool)
	return m
}

// TestBorrowRemoteRuntime_SharesConnAcrossSessions verifies the refcount cache:
// 同一 device 多次借出返回同一 *remote.Runtime 实例;release 减计数,归零摘出 map。
func TestBorrowRemoteRuntime_SharesConnAcrossSessions(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	svc := &chatSvc{}
	installMockPool(t, ctrl, svc, 7)

	be := &agent_backend_entity.AgentBackend{DeviceID: "7"}

	r1, err := svc.borrowRemoteRuntime(context.Background(), be, 100)
	require.NoError(t, err)
	r2, err := svc.borrowRemoteRuntime(context.Background(), be, 101)
	require.NoError(t, err)
	assert.Same(t, r1, r2)

	assert.Equal(t, 2, svc.remoteRuntimeCount(7))

	svc.releaseRemoteRuntime(7, 100)
	assert.Equal(t, 1, svc.remoteRuntimeCount(7))

	svc.releaseRemoteRuntime(7, 101)
	assert.Equal(t, 0, svc.remoteRuntimeCount(7))
}

// TestBorrowRemoteRuntime_PrefetchesCapabilities_OncePerDevice 钉死 Plan B
// 行为:cold path borrow 时同步发一发 runtime.capabilities,缓存到 *remote.Runtime
// 内;同 device 后续 borrow 命中 cache,不再发 RPC。
func TestBorrowRemoteRuntime_PrefetchesCapabilities_OncePerDevice(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	rec := newRecordingDaemonClient()
	pool := mock_remote_device_svc.NewMockConnPool(ctrl)
	lease := mock_remote_device_svc.NewMockLease(ctrl)
	pool.EXPECT().Borrow(gomock.Any(), int64(7)).Return(lease, nil).AnyTimes()
	lease.EXPECT().Client().Return(rec).AnyTimes()
	lease.EXPECT().Closed().Return(make(chan struct{})).AnyTimes()
	lease.EXPECT().Release().AnyTimes()

	svc := &chatSvc{}
	svc.setConnPoolForTest(pool)

	be := &agent_backend_entity.AgentBackend{
		Type:     string(agent_backend_entity.TypeClaudeCode),
		DeviceID: "7",
	}
	_, err := svc.borrowRemoteRuntime(context.Background(), be, 100)
	require.NoError(t, err)
	assert.Equal(t, 1, rec.count(wire.MethodCapabilities), "cold borrow must prefetch capabilities once")

	// Second borrow same device → cache hit, no extra RPC.
	_, err = svc.borrowRemoteRuntime(context.Background(), be, 101)
	require.NoError(t, err)
	assert.Equal(t, 1, rec.count(wire.MethodCapabilities), "cache hit must not re-prefetch")
}

// TestBorrowRemoteRuntime_InvalidDevice 当 be.DeviceIDInt() 解析失败时立即返回
// AgentBackendInvalidDevice — 不去摸 Pool。
func TestBorrowRemoteRuntime_InvalidDevice(t *testing.T) {
	svc := &chatSvc{}
	be := &agent_backend_entity.AgentBackend{DeviceID: "not-a-number"}
	_, err := svc.borrowRemoteRuntime(context.Background(), be, 100)
	var httpErr *httputils.Error
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, code.AgentBackendInvalidDevice, httpErr.Code)
}

// TestBorrowRemoteRuntime_DialFailure 当 Pool.Borrow 失败时返回 RemoteRunnerDialFailed,
// 且不在 cache 留下条目(防止下次 borrow 复用坏 entry)。
func TestBorrowRemoteRuntime_DialFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockPool := mock_remote_device_svc.NewMockConnPool(ctrl)
	mockPool.EXPECT().Borrow(gomock.Any(), int64(7)).Return(nil, errors.New("boom"))

	svc := &chatSvc{}
	svc.setConnPoolForTest(mockPool)

	be := &agent_backend_entity.AgentBackend{DeviceID: "7"}
	_, err := svc.borrowRemoteRuntime(context.Background(), be, 100)
	var httpErr *httputils.Error
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, code.RemoteRunnerDialFailed, httpErr.Code)

	assert.Equal(t, 0, svc.remoteRuntimeCount(7))
}

func TestMapTurnError_RemoteProviderMissing(t *testing.T) {
	svc := &chatSvc{}
	err := svc.mapTurnError(context.Background(), nil, &agent_backend_entity.AgentBackend{
		LLMProviderKey: "provider-key-1",
	}, &daemonrpc.Error{
		Code:    daemonrpc.ErrProviderMissing.Code,
		Message: "LLM provider provider-key-1 not configured",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "远端 agentred 未配置")
	assert.Contains(t, err.Error(), "provider-key-1")
}

// TestSelectRunner_LocalReturnsRegistry verifies local backend → agentruntime.For.
func TestSelectRunner_LocalReturnsRegistry(t *testing.T) {
	svc := &chatSvc{}
	be := &agent_backend_entity.AgentBackend{
		Type:     string(agent_backend_entity.TypeClaudeCode),
		DeviceID: "", // local
	}
	runner, err := svc.selectRunner(context.Background(), be, 100)
	require.NoError(t, err)
	require.NotNil(t, runner)
	// 是 *remote.Runtime 则说明走错了分支
	_, isRemote := runner.(*remote.Runtime)
	assert.False(t, isRemote, "local backend should not return *remote.Runtime")
}

// TestSelectRunner_RemoteBorrows verifies remote backend → borrowRemoteRuntime cache.
func TestSelectRunner_RemoteBorrows(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	svc := &chatSvc{}
	installMockPool(t, ctrl, svc, 7)

	be := &agent_backend_entity.AgentBackend{
		Type:     string(agent_backend_entity.TypeClaudeCode),
		DeviceID: "7",
	}
	runner, err := svc.selectRunner(context.Background(), be, 100)
	require.NoError(t, err)
	_, isRemote := runner.(*remote.Runtime)
	assert.True(t, isRemote)
	assert.Equal(t, 1, svc.remoteRuntimeCount(7))
}

// TestSelectRunner_RemoteIdempotent same sessionID → same instance + no refcount inflation.
func TestSelectRunner_RemoteIdempotent(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	svc := &chatSvc{}
	installMockPool(t, ctrl, svc, 7)

	be := &agent_backend_entity.AgentBackend{
		Type:     string(agent_backend_entity.TypeClaudeCode),
		DeviceID: "7",
	}
	r1, err := svc.selectRunner(context.Background(), be, 100)
	require.NoError(t, err)
	r2, err := svc.selectRunner(context.Background(), be, 100) // same sessionID
	require.NoError(t, err)
	assert.Same(t, r1, r2)
	assert.Equal(t, 1, svc.remoteRuntimeCount(7), "same sessionID must not inflate refcount")
}

func TestToolUseToChatBlock_Canonical(t *testing.T) {
	convey.Convey("Edit → Canonical FileEdit", t, func() {
		cb := toolUseToChatBlock("tu-1", "Edit", map[string]any{
			"file_path":  "/x.go",
			"old_string": "a\n",
			"new_string": "b\n",
		})
		convey.So(cb.Canonical, convey.ShouldNotBeNil)
		convey.So(string(cb.Canonical.Kind), convey.ShouldEqual, "file.edit")
		convey.So(cb.Canonical.FileEdit, convey.ShouldNotBeNil)
		convey.So(cb.Canonical.FileEdit.Files[0].Path, convey.ShouldEqual, "/x.go")
	})

	convey.Convey("file_change → Canonical FileEdit", t, func() {
		cb := toolUseToChatBlock("tu-2", "file_change", map[string]any{
			"changes": []any{
				map[string]any{"path": "a.go", "kind": "update", "diff": "@@ -1,1 +1,1 @@\n-a\n+A\n"},
			},
		})
		convey.So(cb.Canonical, convey.ShouldNotBeNil)
		convey.So(string(cb.Canonical.Kind), convey.ShouldEqual, "file.edit")
		convey.So(cb.Canonical.FileEdit, convey.ShouldNotBeNil)
	})

	convey.Convey("Write → Canonical FileWrite", t, func() {
		cb := toolUseToChatBlock("tu-3", "Write", map[string]any{
			"file_path": "/x.go",
			"content":   "hello\n",
		})
		convey.So(cb.Canonical, convey.ShouldNotBeNil)
		convey.So(string(cb.Canonical.Kind), convey.ShouldEqual, "file.write")
		convey.So(cb.Canonical.FileWrite, convey.ShouldNotBeNil)
		convey.So(cb.Canonical.FileWrite.Path, convey.ShouldEqual, "/x.go")
	})

	convey.Convey("Bash → Canonical=nil(走 RawToolCard 兜底)", t, func() {
		cb := toolUseToChatBlock("tu-4", "Bash", map[string]any{"command": "ls"})
		convey.So(cb.Canonical, convey.ShouldBeNil)
	})
}
