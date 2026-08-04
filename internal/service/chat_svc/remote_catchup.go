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
	if len(ready) == 0 {
		return
	}
	if err := rt.CatchUpSessions(ctx, ready); err != nil {
		logger.Ctx(ctx).Warn("chat_svc.catchUpDevice: catch-up failed",
			zap.Int64("deviceId", deviceID), zap.Int64s("sessionIds", ready), zap.Error(err))
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
