package chat_svc

import (
	"context"
	"errors"
	"fmt"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
)

// ConnStateEventPrefix 是会话级「连接态」流的前缀。连接态是运行态之上的一层修饰,
// 不是第五种 AgentStatus,所以它走自己的会话级流,而不是塞进 per-turn 的运行流:
// 断连恰恰发生在 per-turn 流断掉的时候,塞进去就没人收得到。
const ConnStateEventPrefix = "chat:conn"

// ConnStateStreamName 返回某会话的连接态流名:"chat:conn:<sessionID>"。
// 前端在挂载会话时常驻订阅它。
func ConnStateStreamName(sessionID int64) string {
	return fmt.Sprintf("%s:%d", ConnStateEventPrefix, sessionID)
}

// onRemoteConnState 是 remote.ConnStateObserver 的实现:把会话级连接态播到前端。
func (s *chatSvc) onRemoteConnState(sessionID int64, st remote.SessionConnState) {
	s.emitter.Emit(context.Background(), ConnStateStreamName(sessionID), ChatStreamEvent{
		Kind:             StreamConnectionState,
		ConnectionState:  string(st.State),
		CaughtUpCount:    st.Replayed,
		PendingDecisions: st.PendingDecisions,
	})
}

// reconnectRemote 是注入给 *remote.Runtime 的重连端口:重新从池里借一条已鉴权连接,
// 换进同一个 cache entry,并归还旧的那条。
//
// entry 必须留在 cache 里(断连时 watchLeaseClosed 可能已经把它摘掉了,这里装回去):
// 摘掉它,下一轮 borrow 会为同一台设备造出第二个 *remote.Runtime,而两个 runtime 会在
// 同一条池化连接上抢注同名 handler —— 在飞会话的事件从此被路由到不认识它的那个,静默
// 丢弃,既没有错误也没有 seq 跳号。
func (s *chatSvc) reconnectRemote(
	ctx context.Context, deviceID int64, entry *remoteRuntimeEntry,
) (agentruntime.DaemonClientPort, string, error) {
	lease, err := s.pool().Borrow(ctx, deviceID)
	if err != nil {
		return nil, "", terminalBorrowError(err)
	}
	fp := s.daemonFingerprint(ctx, deviceID)

	s.remoteMu.Lock()
	old := entry.lease
	entry.lease = lease
	if cur, ok := s.remoteCache[deviceID]; !ok || cur == entry {
		if s.remoteCache == nil {
			s.remoteCache = map[int64]*remoteRuntimeEntry{}
		}
		s.remoteCache[deviceID] = entry
	}
	s.remoteMu.Unlock()

	if old != nil {
		old.Release()
	}
	go s.watchLeaseClosed(deviceID, entry, lease)
	logger.Ctx(ctx).Info("chat_svc.reconnectRemote: re-leased daemon connection",
		zap.Int64("deviceId", deviceID))
	return lease.Client(), fp, nil
}

// terminalBorrowError 把「重试也没用」的借用失败翻成 remote.ErrReconnectAbandoned:
// 设备已被解除配对、凭据已撤销、App 正在退出。剩下的(拨不通、超时)保持原样,让重连
// 按退避表继续试。
func terminalBorrowError(err error) error {
	switch {
	case errors.Is(err, remote_device_svc.ErrDeviceNotFound),
		errors.Is(err, remote_device_svc.ErrDeviceUnauthorized),
		errors.Is(err, remote_device_svc.ErrPoolClosed):
		return fmt.Errorf("%w: %w", remote.ErrReconnectAbandoned, err)
	default:
		return err
	}
}

// daemonFingerprint 取该配对设备的 daemon 实例标识(sha256:<hex>)。取不到返回空串 ——
// 空标识让游标端口一律判失效(chat_entity.Session.CursorValidFor),补齐因此退化成
// 「断连即终止」,而不是拿一个来路不明的标识去拉别人的日志。
func (s *chatSvc) daemonFingerprint(ctx context.Context, deviceID int64) string {
	rds := remote_device_svc.Default()
	if rds == nil {
		return ""
	}
	view, err := rds.Get(ctx, deviceID)
	if err != nil || view == nil {
		logger.Ctx(ctx).Warn("chat_svc.daemonFingerprint: device lookup failed",
			zap.Int64("deviceId", deviceID), zap.Error(err))
		return ""
	}
	return view.DaemonFingerprint
}

// recordExecDaemon 把「这条会话跑在哪台 daemon 的哪个实例上」写进会话行。
//
// 游标的读写守卫全靠这一对 (设备, 实例标识):不写,LoadCursor 永远判失效,断连补齐
// 就退化回「断连即终止」。标识为空说明这台设备还没配对出实例标识,此时没有可记录的
// 身份,写进去等于给游标伪造一个来路不明的归属。
func (s *chatSvc) recordExecDaemon(ctx context.Context, sessionID, deviceID int64, fingerprint string) {
	if fingerprint == "" || sessionID <= 0 {
		return
	}
	if err := chat_repo.Session().UpdateExecDaemon(ctx, sessionID, deviceID, fingerprint); err != nil {
		logger.Ctx(ctx).Warn("chat_svc.recordExecDaemon: persist exec daemon failed",
			zap.Int64("sessionId", sessionID), zap.Int64("deviceId", deviceID), zap.Error(err))
	}
}
