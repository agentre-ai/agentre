package chat_svc

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo/mock_agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo/mock_chat_repo"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc/mock_remote_device_svc"
)

// scriptedDaemonClient 是一条可脚本化的 daemon 连接:按 method 应答,并记下调用顺序。
type scriptedDaemonClient struct {
	mu     sync.Mutex
	calls  []string
	params map[string][]any
	script func(method string, params, result any) error
}

func newScriptedDaemonClient(script func(method string, params, result any) error) *scriptedDaemonClient {
	return &scriptedDaemonClient{params: map[string][]any{}, script: script}
}

func (c *scriptedDaemonClient) Call(_ context.Context, method string, params, result any) error {
	c.mu.Lock()
	c.calls = append(c.calls, method)
	c.params[method] = append(c.params[method], params)
	c.mu.Unlock()
	return c.script(method, params, result)
}

func (*scriptedDaemonClient) Notify(_ string, _ any) error { return nil }
func (*scriptedDaemonClient) Handle(_ string, _ func(context.Context, json.RawMessage) (any, error)) {
}
func (*scriptedDaemonClient) Closed() <-chan struct{} { return nil }
func (*scriptedDaemonClient) Close() error            { return nil }

func (c *scriptedDaemonClient) countOf(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.params[method])
}

func (c *scriptedDaemonClient) attachedSessions() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]int64, 0, len(c.params[wire.MethodSessionAttach]))
	for _, p := range c.params[wire.MethodSessionAttach] {
		out = append(out, p.(wire.SessionAttachParams).SessionID)
	}
	return out
}

// Given 桌面 App 退出后重开,库里两条会话记着跑在同一台配对 daemon 上(一条在这段
// 时间里又产生了内容,一条没有),When 启动补齐,Then 按 exec_device_id 连回那台
// daemon,先取会话清单与状态,再只对真正落下内容的那条走 attach → pull → 待决策清单。
//
// exec_device_id 在此之前是一列只写数据:写进去、再没人读。桌面端因此在重启后不知道
// 「该连谁」,补齐三步无从发起 —— 用户故事「退出桌面 App 后下次打开看到这段时间发生
// 的全部内容」直接不成立(catchUpAll 只遍历本进程在飞的轮次,而刚启动时它必然是空的)。
func TestCatchUpRemoteSessions_ConnectsByExecDeviceAndRunsThreeSteps(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		deviceID int64 = 7
		behind   int64 = 100 // 游标 17,daemon 上已到 20
		caughtUp int64 = 101 // 游标 5,daemon 上也是 5
	)
	const fp = "sha256:beef"

	client := newScriptedDaemonClient(func(method string, params, result any) error {
		switch method {
		case wire.MethodSessionList:
			*(result.(*wire.SessionListResult)) = wire.SessionListResult{Sessions: []wire.SessionSummary{
				{SessionID: behind, LifecycleState: wire.SessionLifecycleRunning, LatestSeq: 20},
				{SessionID: caughtUp, LifecycleState: wire.SessionLifecycleIdle, LatestSeq: 5},
			}}
		case wire.MethodSessionAttach:
			p := params.(wire.SessionAttachParams)
			*(result.(*wire.SessionAttachResult)) = wire.SessionAttachResult{
				SessionID: p.SessionID, LifecycleState: wire.SessionLifecycleRunning, LatestSeq: 20,
			}
		case wire.MethodSessionPull:
			p := params.(wire.SessionPullParams)
			*(result.(*wire.SessionPullResult)) = wire.SessionPullResult{Cursor: p.Cursor}
		case wire.MethodSessionPendingWaiters:
			*(result.(*wire.SessionPendingWaitersResult)) = wire.SessionPendingWaitersResult{}
		}
		return nil
	})

	pool := mock_remote_device_svc.NewMockConnPool(ctrl)
	lease := mock_remote_device_svc.NewMockLease(ctrl)
	lease.EXPECT().Client().Return(client).AnyTimes()
	lease.EXPECT().Closed().Return(make(chan struct{})).AnyTimes()
	lease.EXPECT().Release().AnyTimes()
	pool.EXPECT().Borrow(gomock.Any(), deviceID).Return(lease, nil).Times(1)

	rds := mock_remote_device_svc.NewMockRemoteDeviceSvc(ctrl)
	rds.EXPECT().Get(gomock.Any(), deviceID).
		Return(&remote_device_svc.DeviceView{ID: deviceID, DaemonFingerprint: fp}, nil).AnyTimes()
	// 补齐一路都要探这台 daemon 认不认补齐族 RPC(R18):这台认得,于是设备状态上
	// 那条「版本过旧」被撤下来。
	rds.EXPECT().RecordDaemonOutdated(deviceID, false).AnyTimes()
	prevSvc := remote_device_svc.Default()
	remote_device_svc.SetDefault(rds)
	t.Cleanup(func() { remote_device_svc.SetDefault(prevSvc) })

	rows := []*chat_entity.Session{
		{ID: behind, AgentID: 9, Status: consts.ACTIVE, ExecDeviceID: deviceID, ExecDaemonFingerprint: fp, EventCursor: 17},
		{ID: caughtUp, AgentID: 9, Status: consts.ACTIVE, ExecDeviceID: deviceID, ExecDaemonFingerprint: fp, EventCursor: 5},
	}
	sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
	sessRepo.EXPECT().ListRemoteExecSessions(gomock.Any()).Return(rows, nil)
	// 游标端口经 chat_sessions 读写(session_cursor.go),补齐一路都会问到它。
	for _, row := range rows {
		sessRepo.EXPECT().Find(gomock.Any(), row.ID).Return(row, nil).AnyTimes()
	}
	sessRepo.EXPECT().UpdateEventCursor(gomock.Any(), gomock.Any(), fp, gomock.Any()).Return(nil).AnyTimes()

	agtRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
	agtRepo.EXPECT().Find(gomock.Any(), int64(9)).
		Return(&agent_entity.Agent{ID: 9, AgentBackendID: 3}, nil).AnyTimes()
	beRepo := mock_agent_backend_repo.NewMockAgentBackendRepo(ctrl)
	beRepo.EXPECT().Find(gomock.Any(), int64(3)).Return(&agent_backend_entity.AgentBackend{
		ID: 3, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "7",
	}, nil).AnyTimes()

	prevSess, prevAgent, prevBE := chat_repo.Session(), agent_repo.Agent(), agent_backend_repo.AgentBackend()
	chat_repo.RegisterSession(sessRepo)
	agent_repo.RegisterAgent(agtRepo)
	agent_backend_repo.RegisterAgentBackend(beRepo)
	t.Cleanup(func() {
		chat_repo.RegisterSession(prevSess)
		agent_repo.RegisterAgent(prevAgent)
		agent_backend_repo.RegisterAgentBackend(prevBE)
	})

	svc := NewChat(NoopEmitter{}).(*chatSvc)
	svc.setConnPoolForTest(pool)

	require.NoError(t, svc.CatchUpRemoteSessions(context.Background()))

	assert.Equal(t, 1, client.countOf(wire.MethodSessionList),
		"会话清单是补齐的第一步,每台 daemon 问一次")
	assert.Equal(t, []int64{behind}, client.attachedSessions(),
		"只有真正落下内容的那条才发 attach —— 清单交回的 latestSeq 就是用来分辨这件事的")
	assert.Equal(t, 1, client.countOf(wire.MethodSessionPull))
	assert.Equal(t, 1, client.countOf(wire.MethodSessionPendingWaiters),
		"待决策清单是第三步:断连前就阻塞、宣告事件早在游标之前的那些只能靠它找回来")

	// 补齐重放出来的内容以「没有 user 行的一轮」交付,消费方必须在补齐**之前**装好:
	// 没人 drain 会把通知读循环顶住,内容也永远进不了转录。
	_, watching := svc.autoWatchers.Load(behind)
	assert.True(t, watching, "补齐的会话必须先接上轮次消费方,重放内容才有落点")
}

// Given 某台配对 daemon 此刻不在线,When 启动补齐,Then 跳过它继续补别的设备,
// 整体不报错 —— 关着的远端盒子是常态,不该让另一台上的会话跟着补不成。
func TestCatchUpRemoteSessions_OfflineDaemonDoesNotBlockOthers(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	pool := mock_remote_device_svc.NewMockConnPool(ctrl)
	pool.EXPECT().Borrow(gomock.Any(), int64(7)).Return(nil, assertDialErr).Times(1)

	sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
	sessRepo.EXPECT().ListRemoteExecSessions(gomock.Any()).Return([]*chat_entity.Session{
		{ID: 100, AgentID: 9, Status: consts.ACTIVE, ExecDeviceID: 7, ExecDaemonFingerprint: "sha256:beef"},
	}, nil)
	prevSess := chat_repo.Session()
	chat_repo.RegisterSession(sessRepo)
	t.Cleanup(func() { chat_repo.RegisterSession(prevSess) })

	svc := NewChat(NoopEmitter{}).(*chatSvc)
	svc.setConnPoolForTest(pool)

	assert.NoError(t, svc.CatchUpRemoteSessions(context.Background()))
}

type dialErr string

func (e dialErr) Error() string { return string(e) }

const assertDialErr = dialErr("daemon offline")
