package chat_svc

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc/mock_remote_device_svc"
)

// recordingEmitter 收下 emit 出去的会话级事件。
type recordingEmitter struct {
	mu  sync.Mutex
	got []emittedEvent
}

type emittedEvent struct {
	Name    string
	Payload ChatStreamEvent
}

func (e *recordingEmitter) Emit(_ context.Context, name string, payload any) {
	ev, ok := payload.(ChatStreamEvent)
	if !ok {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.got = append(e.got, emittedEvent{Name: name, Payload: ev})
}

func (e *recordingEmitter) events() []emittedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]emittedEvent(nil), e.got...)
}

// Given 某 device 的池化连接断了,When *remote.Runtime 经重连端口要一条新连接,
// Then chat_svc 从池里再借一条、换进同一个 cache entry 并归还旧的。
//
// entry 必须**留在** cache 里:摘掉它,下一轮 borrow 会为同一台设备造出第二个
// *remote.Runtime,而两个 runtime 会在同一条池化连接上抢注同名 handler —— 在飞
// 会话的事件从此被路由到不认识它的那个,静默丢弃。
func TestReconnectRemote_SwapsLeaseAndKeepsCacheEntry(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	pool := mock_remote_device_svc.NewMockConnPool(ctrl)
	lease1 := mock_remote_device_svc.NewMockLease(ctrl)
	lease2 := mock_remote_device_svc.NewMockLease(ctrl)
	client1, client2 := &noopDaemonClient{}, &noopDaemonClient{}
	for _, p := range []struct {
		lease  *mock_remote_device_svc.MockLease
		client *noopDaemonClient
	}{{lease1, client1}, {lease2, client2}} {
		p.lease.EXPECT().Client().Return(p.client).AnyTimes()
		p.lease.EXPECT().Closed().Return(make(chan struct{})).AnyTimes()
	}
	gomock.InOrder(
		pool.EXPECT().Borrow(gomock.Any(), int64(7)).Return(lease1, nil),
		pool.EXPECT().Borrow(gomock.Any(), int64(7)).Return(lease2, nil),
	)
	lease1.EXPECT().Release().Times(1)
	lease2.EXPECT().Release().AnyTimes()

	svc := &chatSvc{emitter: NoopEmitter{}}
	svc.setConnPoolForTest(pool)

	be := &agent_backend_entity.AgentBackend{DeviceID: "7"}
	_, err := svc.borrowRemoteRuntime(context.Background(), be, 100)
	require.NoError(t, err)

	svc.remoteMu.Lock()
	entry := svc.remoteCache[7]
	svc.remoteMu.Unlock()
	require.NotNil(t, entry)

	got, _, err := svc.reconnectRemote(context.Background(), 7, entry)
	require.NoError(t, err)
	assert.Same(t, client2, got, "重连必须交出新 lease 的连接")

	svc.remoteMu.Lock()
	still := svc.remoteCache[7]
	swapped := entry.lease
	svc.remoteMu.Unlock()
	assert.Same(t, entry, still, "重连后 cache entry 必须还在")
	assert.Same(t, lease2, swapped, "entry 必须持有新 lease")
}

// Given 一条远端会话的连接态发生变化,When runtime 播报,Then chat_svc 在会话级
// 流上 emit 一条 connection_state —— 连接态是运行态之上的修饰,不进 AgentStatus。
func TestRemoteConnState_EmitsSessionLevelStreamEvent(t *testing.T) {
	rec := &recordingEmitter{}
	svc := &chatSvc{emitter: rec}

	svc.onRemoteConnState(100, remote.SessionConnState{State: remote.ConnStateReconnecting})
	svc.onRemoteConnState(100, remote.SessionConnState{
		State: remote.ConnStateConnected, Replayed: 4, PendingDecisions: 1,
	})

	got := rec.events()
	require.Len(t, got, 2)
	assert.Equal(t, ConnStateStreamName(100), got[0].Name)
	assert.Equal(t, StreamConnectionState, got[0].Payload.Kind)
	assert.Equal(t, string(remote.ConnStateReconnecting), got[0].Payload.ConnectionState)

	assert.Equal(t, string(remote.ConnStateConnected), got[1].Payload.ConnectionState)
	assert.Equal(t, 4, got[1].Payload.CaughtUpCount)
	assert.Equal(t, 1, got[1].Payload.PendingDecisions)
}
