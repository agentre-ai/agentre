package group_svc

import (
	"context"
	"sync"

	"github.com/cago-frame/cago/pkg/gogo"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"agentre/internal/model/entity/group_entity"
	"agentre/internal/repository/group_repo"
	"agentre/internal/service/chat_svc"
)

// delivery 是投给某成员的一条待消费消息(正文 + 自然抬头来源名)。
type delivery struct {
	fromName string
	content  string
}

// scheduler 是每个群的运行态: 每成员 pending FIFO + 在跑成员集合。
type scheduler struct {
	mu       sync.Mutex
	pending  map[int64][]delivery // memberID -> FIFO
	inflight map[int64]bool       // memberID -> 是否有在跑 turn
}

func newScheduler() *scheduler {
	return &scheduler{pending: map[int64][]delivery{}, inflight: map[int64]bool{}}
}

// schedulerFor 懒建并返回某群的调度器(s.mu 保护 s.schedulers)。
func (s *groupSvc) schedulerFor(groupID int64) *scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc := s.schedulers[groupID]
	if sc == nil {
		sc = newScheduler()
		s.schedulers[groupID] = sc
	}
	return sc
}

// enqueueDeliveries 把同一条消息追加到每个收件成员的 pending FIFO。
func (s *groupSvc) enqueueDeliveries(groupID int64, recipientIDs []int64, content, fromName string) {
	if len(recipientIDs) == 0 {
		return
	}
	sc := s.schedulerFor(groupID)
	sc.mu.Lock()
	for _, mid := range recipientIDs {
		sc.pending[mid] = append(sc.pending[mid], delivery{fromName: fromName, content: content})
	}
	sc.mu.Unlock()
}

// markDone 释放某成员的 inflight 槽。
func (s *groupSvc) markDone(groupID, memberID int64) {
	sc := s.schedulerFor(groupID)
	sc.mu.Lock()
	delete(sc.inflight, memberID)
	sc.mu.Unlock()
}

// stopAll 清空某群队列 + 对每个在跑成员的 backing session 调 gw.Stop(中止 turn)。
func (s *groupSvc) stopAll(ctx context.Context, groupID int64) {
	sc := s.schedulerFor(groupID)
	sc.mu.Lock()
	inflight := make([]int64, 0, len(sc.inflight))
	for mid := range sc.inflight {
		inflight = append(inflight, mid)
	}
	sc.pending = map[int64][]delivery{}
	sc.inflight = map[int64]bool{}
	sc.mu.Unlock()

	members, _ := group_repo.Member().ListByGroup(ctx, groupID)
	sessByMember := make(map[int64]int64, len(members))
	for _, m := range members {
		sessByMember[m.ID] = m.BackingSessionID
	}
	for _, mid := range inflight {
		if sid := sessByMember[mid]; sid > 0 {
			if _, err := s.gw.Stop(ctx, &chat_svc.StopRequest{SessionID: sid}); err != nil {
				logger.Ctx(ctx).Warn("group_svc.stopAll: stop failed", zap.Int64("sessionId", sid), zap.Error(err))
			}
		}
	}
}

type launchItem struct {
	d delivery
	m *group_entity.GroupMember
}

// kick 给「有 pending 且未在跑」的活跃成员各起一个 turn(跨成员并发),
// 并根据是否还有 work 维护 run_status(有 work→running; 全空→waiting_user 静默)。
// paused/error(CanAdvance=false) 时直接返回, 不填新槽。
func (s *groupSvc) kick(ctx context.Context, groupID int64) {
	g, err := group_repo.Group().Find(ctx, groupID)
	if err != nil || g == nil || !g.CanAdvance() {
		return
	}
	members, err := group_repo.Member().ListByGroup(ctx, groupID)
	if err != nil {
		return
	}
	memberByID := make(map[int64]*group_entity.GroupMember, len(members))
	for _, m := range members {
		memberByID[m.ID] = m
	}

	sc := s.schedulerFor(groupID)
	var launches []launchItem
	sc.mu.Lock()
	for mid, q := range sc.pending {
		if len(q) == 0 || sc.inflight[mid] {
			continue
		}
		m := memberByID[mid]
		if m == nil || !m.IsActive() { // 成员已离群 → 丢弃其队列
			delete(sc.pending, mid)
			continue
		}
		d := q[0]
		rest := q[1:]
		if len(rest) == 0 {
			delete(sc.pending, mid)
		} else {
			sc.pending[mid] = rest
		}
		sc.inflight[mid] = true
		launches = append(launches, launchItem{d: d, m: m})
	}
	hasPending := false
	for _, q := range sc.pending {
		if len(q) > 0 {
			hasPending = true
			break
		}
	}
	desired := group_entity.RunWaitingUser
	if len(sc.inflight) > 0 || hasPending {
		desired = group_entity.RunRunning
	}
	// 状态转移在 sc.mu 内做: 测试里 g 是 mock 共享指针, 锁内写防 -race; 生产里每次 Find 出独立 g, 锁无害。
	s.transitionRunStatus(ctx, g, desired)
	sc.mu.Unlock()

	for _, l := range launches { // launchDelivery 在锁外(内部 Send 不可在锁内)
		s.launchDelivery(g, members, l.d, l.m)
	}
}

// transitionRunStatus 仅在状态变化时落库 + emit。调用方持 sc.mu(见 kick)。
func (s *groupSvc) transitionRunStatus(ctx context.Context, g *group_entity.Group, status string) {
	if g.RunStatus == status {
		return
	}
	g.RunStatus = status
	if err := group_repo.Group().Update(ctx, g); err != nil {
		logger.Ctx(ctx).Warn("group_svc.transitionRunStatus: update failed",
			zap.Int64("groupId", g.ID), zap.String("status", status), zap.Error(err))
		return
	}
	s.emitter.Emit(ctx, groupEventName(g.ID), map[string]any{"kind": "run_status", "runStatus": status})
}

// launchDelivery 订阅成员 turn 生命周期 → Send(带 group MCP tool + 群 system prompt) → 后台等 turn 结束。
func (s *groupSvc) launchDelivery(g *group_entity.Group, members []*group_entity.GroupMember, d delivery, m *group_entity.GroupMember) {
	ch, cancel := s.gw.ObserveTurn(m.BackingSessionID)
	bg := context.Background()
	req := &chat_svc.SendRequest{
		SessionID:          m.BackingSessionID,
		AgentID:            m.AgentID,
		Text:               "(来自 " + d.fromName + ")\n" + d.content,
		MCPServers:         s.buildGroupMCP(g, m),
		SystemPromptSuffix: s.buildGroupSystemPrompt(g, members, m),
	}
	if _, err := s.gw.Send(bg, req); err != nil {
		cancel()
		// Send 瞬时失败: 已从 pending 弹出的这条 delivery 被有意丢弃(仅 Warn, 不回队) ——
		// 避免无限重试; MVP 可接受。释放槽后再 kick 让其它 ready 成员/后续 delivery 继续。
		logger.Ctx(bg).Warn("group_svc.launchDelivery: send failed", zap.Int64("memberId", m.ID), zap.Error(err))
		s.markDone(m.GroupID, m.ID)
		// 此处同步 kick→launchDelivery→kick 的递归有界: 每次 kick 对每个成员至多弹一条,
		// 深度受「当前 ready 成员数」约束(成员上限 ~8), 不会失控。
		s.kick(bg, m.GroupID)
		return
	}
	gogo.Go(func() error {
		defer cancel()
		res := <-ch
		s.handleTurnResult(context.Background(), m.GroupID, m, res)
		return nil
	}, gogo.WithIgnorePanic())
}

// handleTurnResult 仅管生命周期: 释放 inflight 槽 + kick(填新槽; 全空则 quiesce → waiting_user)。
// 不解析文本/不落消息 —— 成员发言来自 turn 进行中的 group_send tool 调用(IngestAgentMessage)。
func (s *groupSvc) handleTurnResult(ctx context.Context, groupID int64, m *group_entity.GroupMember, res chat_svc.TurnResult) {
	s.markDone(groupID, m.ID)
	if res.Err != nil {
		logger.Ctx(ctx).Warn("group_svc.handleTurnResult: member turn error", zap.Int64("memberId", m.ID), zap.Error(res.Err))
	}
	s.kick(ctx, groupID)
}
