package chat_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
)

// CatchUpRemoteSessions 是桌面 App 启动后的远端补齐入口:把「这段时间远端上发生的
// 全部内容」接回来。
//
// 它是 exec_device_id 的**唯一读方**。在此之前那一列只写不读:remote.Runtime 的补齐
// 只覆盖本进程内在飞的轮次,而 App 刚启动时那个集合必然是空的 —— 一条会话都不会被
// 补齐,重放上来的内容也没有落点。于是「合上笔记本、关掉 App,回来看到这段时间的
// 全部内容」这条用户故事整个不成立。
//
// 按设备分组是硬的:一台 daemon 一条池化连接、一次会话清单,再对该清单上真正落下
// 内容的会话逐条走补齐三步。某台 daemon 不在线只跳过它 —— 关着的远端盒子是常态,
// 不该让另一台上的会话跟着补不成。
func (s *chatSvc) CatchUpRemoteSessions(ctx context.Context) error {
	sessions, err := chat_repo.Session().ListRemoteExecSessions(ctx)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.CatchUpRemoteSessions: list remote sessions failed", zap.Error(err))
		return err
	}
	if len(sessions) == 0 {
		return nil
	}
	byDevice := map[int64][]*chat_entity.Session{}
	order := make([]int64, 0, len(sessions))
	for _, sess := range sessions {
		if _, seen := byDevice[sess.ExecDeviceID]; !seen {
			order = append(order, sess.ExecDeviceID)
		}
		byDevice[sess.ExecDeviceID] = append(byDevice[sess.ExecDeviceID], sess)
	}
	logger.Ctx(ctx).Info("chat_svc.CatchUpRemoteSessions: catching up remote sessions",
		zap.Int("sessions", len(sessions)), zap.Int("devices", len(order)))
	for _, deviceID := range order {
		s.catchUpDevice(ctx, deviceID, byDevice[deviceID])
	}
	return nil
}

// catchUpDevice 补齐一台 daemon 上的会话。
func (s *chatSvc) catchUpDevice(ctx context.Context, deviceID int64, sessions []*chat_entity.Session) {
	sids := make([]int64, 0, len(sessions))
	for _, sess := range sessions {
		sids = append(sids, sess.ID)
	}
	rt, _, err := s.remoteRuntimeForDevice(ctx, deviceID, sids)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.catchUpDevice: daemon unreachable, skipping catch-up",
			zap.Int64("deviceId", deviceID), zap.Int64s("sessionIds", sids), zap.Error(err))
		// 连不上就没有判据:按今天的语义收尾,不把它们永远留在 running 上转圈。
		s.failSessionsNotLiveOnDaemon(ctx, sids, nil)
		return
	}
	// 消费方必须在补齐**之前**接上:重放出来的内容以「没有 user 行的一轮」交付,
	// 没人 drain 就会把通知读循环顶住,内容也永远进不了转录。
	ready := make([]int64, 0, len(sessions))
	for _, sess := range sessions {
		if !s.watchCatchUpTurns(ctx, sess, rt) {
			continue
		}
		ready = append(ready, sess.ID)
	}
	live, err := rt.CatchUpSessions(ctx, ready)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.catchUpDevice: catch-up failed",
			zap.Int64("deviceId", deviceID), zap.Int64s("sessionIds", ready), zap.Error(err))
	}
	s.failSessionsNotLiveOnDaemon(ctx, sids, live)
}

// failSessionsNotLiveOnDaemon 把这台 daemon 上「它自己都说不在跑」的那些会话收尾成
// error(只动还停在 running / waiting 的行)。
//
// 这是启动期残留清理在远端会话那一半:bootstrap.ResetStaleActiveSessions 已经不碰远端
// 会话了 —— 在连上 daemon 之前无从判断,一律翻 error 就是报假失败,而一条在桌面端离线
// 期间一个字都没产出的会话不会被重放改写,那条假失败会永久留在界面上(R4:断连不终止
// 会话)。判据只能来自 daemon 交回的生命周期。
//
// 顺序上放在补齐之后是有意的:有内容可重放的会话由 driveAutonomousTurn 在重放收尾时写
// 自己的终态(idle / error),那一次写发生在这之后,是最终值;这里这一笔至多在中途把它
// 从 running 挪到 error,再被重放的终态覆盖。反过来先写就会被半途的 running 覆盖住。
func (s *chatSvc) failSessionsNotLiveOnDaemon(ctx context.Context, all, live []int64) {
	if len(all) == 0 {
		return
	}
	liveSet := make(map[int64]struct{}, len(live))
	for _, sid := range live {
		liveSet[sid] = struct{}{}
	}
	stale := make([]int64, 0, len(all))
	for _, sid := range all {
		if _, ok := liveSet[sid]; ok {
			continue
		}
		stale = append(stale, sid)
	}
	if len(stale) == 0 {
		return
	}
	n, err := chat_repo.Session().ResetActiveSessionsByIDs(ctx, stale)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.failSessionsNotLiveOnDaemon: reset failed",
			zap.Int64s("sessionIds", stale), zap.Error(err))
		return
	}
	if n > 0 {
		logger.Ctx(ctx).Info("chat_svc.failSessionsNotLiveOnDaemon: ended sessions the daemon is not running",
			zap.Int64s("sessionIds", stale), zap.Int64("rows", n))
	}
}

// watchCatchUpTurns 给一条待补齐的会话接上轮次消费方(与自主续轮同一条:补齐重放出来
// 的内容与自主续轮是同一种东西 —— 一轮没有 user 行的 assistant 轮,driveAutonomousTurn
// 已经会把它落成消息)。解析不出后端就跳过这条会话:没有后端就没法落库,补齐它只会
// 把内容重放进一个没人收的 channel。
func (s *chatSvc) watchCatchUpTurns(ctx context.Context, sess *chat_entity.Session, src agentruntime.AutonomousTurnSource) bool {
	be := s.sessionBackend(ctx, sess)
	if be == nil {
		logger.Ctx(ctx).Warn("chat_svc.watchCatchUpTurns: backend unresolved, skipping session",
			zap.Int64("sessionId", sess.ID), zap.Int64("agentId", sess.AgentID))
		return false
	}
	s.startAutonomousWatcher(sess.ID, be, src)
	return true
}

// sessionBackend 解析某会话此刻挂在哪个 agent backend 上。解析不出返回 nil。
func (s *chatSvc) sessionBackend(ctx context.Context, sess *chat_entity.Session) *agent_backend_entity.AgentBackend {
	if sess == nil {
		return nil
	}
	a, err := agent_repo.Agent().Find(ctx, sess.AgentID)
	if err != nil || a == nil || a.AgentBackendID <= 0 {
		return nil
	}
	be, err := agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID)
	if err != nil {
		return nil
	}
	return be
}
