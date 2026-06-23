package group_svc

import (
	"context"
	"errors"
	"sync"

	"github.com/cago-frame/cago/pkg/gogo"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

// 成员级运行态:区别于 GroupMember.Status 的"成员身份"(active/left)。roster 用它
// 显示某成员此刻是否真的在跑一轮 turn。源头是调度器的 inflight 集合(= turn 生命周期)。
const (
	RunStateRunning = "running"
	RunStateIdle    = "idle"
)

// delivery 是投给某成员的一条待消费消息(正文 + 自然抬头来源名)。
// attempts 记录已失败的投递次数, Send 瞬时失败回队重试用。
// source 是触发这条投递的来源成员 id(0=用户), 收件成员无 mention 回复时回它。
type delivery struct {
	fromName string
	content  string
	attempts int
	source   int64
}

// maxDeliveryAttempts: Send 瞬时失败(SQLITE_BUSY / 锁竞争等)最多投这么多次,
// 超过即丢弃 —— 防 kick→launchDelivery→kick 无限循环。
const maxDeliveryAttempts = 2

// scheduler 是每个群的运行态: 每成员 pending FIFO + 在跑成员集合。
type scheduler struct {
	mu             sync.Mutex
	pending        map[int64][]delivery // memberID -> FIFO
	inflight       map[int64]bool       // memberID -> 是否有在跑 turn
	inflightSource map[int64]int64      // memberID -> 触发本轮的来源成员 id(0=用户)
	generation     uint64               // stop/delete increments this to cancel launches outside the lock
}

func newScheduler() *scheduler {
	return &scheduler{pending: map[int64][]delivery{}, inflight: map[int64]bool{}, inflightSource: map[int64]int64{}}
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
// source 是发出这条消息的成员 id(0=用户), 随 delivery 传递到 inflightSource。
func (s *groupSvc) enqueueDeliveries(groupID int64, recipientIDs []int64, content, fromName string, source int64) {
	if len(recipientIDs) == 0 {
		return
	}
	sc := s.schedulerFor(groupID)
	sc.mu.Lock()
	for _, mid := range recipientIDs {
		sc.pending[mid] = append(sc.pending[mid], delivery{fromName: fromName, content: content, source: source})
	}
	sc.mu.Unlock()
}

// markDone 释放某成员的 inflight 槽。
func (s *groupSvc) markDone(groupID, memberID int64) {
	sc := s.schedulerFor(groupID)
	sc.mu.Lock()
	delete(sc.inflight, memberID)
	delete(sc.inflightSource, memberID)
	sc.mu.Unlock()
}

// turnSource 给出某成员当前在跑 turn 的触发来源(0=用户)。ok=false 表示该成员
// 没有在跑的 turn(无来源可查)。IngestAgentMessage 的 fallback 用它把无 mention
// 的回复路由回触发者。锁序: 调用方持 ingestMu → 此处取 sc.mu, 与 kick 同向。
func (s *groupSvc) turnSource(groupID, memberID int64) (int64, bool) {
	sc := s.schedulerFor(groupID)
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if !sc.inflight[memberID] {
		return 0, false
	}
	return sc.inflightSource[memberID], true
}

// memberRunStates 按调度器在跑集合给出 memberID -> RunStateRunning/RunStateIdle 快照。
// LoadGroup 用它回填 GroupDetail.MemberRunStates,让 roster 在打开/重载时立即正确。
func (s *groupSvc) memberRunStates(groupID int64, members []*group_entity.GroupMember) map[int64]string {
	sc := s.schedulerFor(groupID)
	sc.mu.Lock()
	defer sc.mu.Unlock()
	out := make(map[int64]string, len(members))
	for _, m := range members {
		if sc.inflight[m.ID] {
			out[m.ID] = RunStateRunning
		} else {
			out[m.ID] = RunStateIdle
		}
	}
	return out
}

// emitMemberRunState 推一条成员级运行态变更给前端(roster 据此翻状态点)。
// 与 member_updated(成员身份)分开,避免覆盖彼此字段。同步发到全局频道
// (GroupRunStateEventName),带 backingSessionID 让侧栏把成员运行态映射到
// 该 backing session 的会话行。
func (s *groupSvc) emitMemberRunState(ctx context.Context, groupID, memberID, sessionID int64, runState string) {
	payload := map[string]any{
		"kind":             "member_run_state",
		"groupID":          groupID,
		"memberID":         memberID,
		"runState":         runState,
		"backingSessionID": sessionID,
	}
	s.emitter.Emit(ctx, groupEventName(groupID), payload)
	s.emitter.Emit(ctx, GroupRunStateEventName, payload)
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
	sc.inflightSource = map[int64]int64{}
	sc.generation++
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
		// inflight 已清, 但订阅方(roster / 侧栏)看不见内部状态 —— 不补 idle 事件
		// 会卡 running 直到 reload。
		s.emitMemberRunState(ctx, groupID, mid, sessByMember[mid], RunStateIdle)
	}
}

type launchItem struct {
	d          delivery
	m          *group_entity.GroupMember
	generation uint64
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
		sc.inflightSource[mid] = d.source
		launches = append(launches, launchItem{d: d, m: m, generation: sc.generation})
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
		s.launchDelivery(g, members, l.d, l.m, l.generation)
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
	s.emitRunStatus(ctx, g.ID, status)
}

// launchDelivery 订阅成员 turn 生命周期 → Send(带 group MCP tool + 群 system prompt) → 后台等 turn 结束。
func (s *groupSvc) launchDelivery(g *group_entity.Group, members []*group_entity.GroupMember, d delivery, m *group_entity.GroupMember, generation uint64) {
	bg := context.Background()
	sessionID, err := s.ensureBackingSession(bg, g, m)
	if err != nil {
		logger.Ctx(bg).Warn("group_svc.launchDelivery: ensure backing session failed", zap.Int64("memberId", m.ID), zap.Error(err))
		s.markDone(m.GroupID, m.ID)
		s.kick(bg, m.GroupID)
		return
	}
	req := &chat_svc.SendRequest{
		SessionID:          sessionID,
		AgentID:            m.AgentID,
		Text:               "(来自 " + d.fromName + ")\n" + d.content,
		MCPServers:         s.buildGroupMCP(g, m),
		SystemPromptSuffix: s.buildGroupSystemPrompt(g, members, m),
		// 群成员轮由 scheduler 发起(非查看者): 经会话级旁路把 per-turn 流名推给该 backing
		// session 已打开(可能在后台)的 ChatPanel, 让它翻 running + openStream 实时接流 ——
		// 否则该 tab 已开但本轮非它发起, 拿不到流名, 表现为"没反应/不转圈"。
		EmitTurnStartedBypass: true,
	}
	sc := s.schedulerFor(m.GroupID)
	sc.mu.Lock()
	if sc.generation != generation || !sc.inflight[m.ID] {
		sc.mu.Unlock()
		s.markDone(m.GroupID, m.ID)
		s.kick(bg, m.GroupID)
		return
	}
	ch, cancel := s.gw.ObserveTurn(sessionID)
	_, err = s.gw.Send(bg, req)
	sc.mu.Unlock()
	if err != nil {
		cancel()
		// Send 瞬时失败: delivery 回队队首重试(attempts 封顶 maxDeliveryAttempts,
		// 超限丢弃, 仅 Warn)。generation 校验防 Stop/Delete 后复活已清空的队列。
		d.attempts++
		sc.mu.Lock()
		if sc.generation == generation && d.attempts < maxDeliveryAttempts {
			sc.pending[m.ID] = append([]delivery{d}, sc.pending[m.ID]...)
		} else {
			logger.Ctx(bg).Warn("group_svc.launchDelivery: send failed, delivery dropped",
				zap.Int64("memberId", m.ID), zap.Int("attempts", d.attempts), zap.Error(err))
		}
		sc.mu.Unlock()
		// 此处同步 kick→launchDelivery→kick 的递归有界: 每次 kick 对每个成员至多弹一条,
		// 深度受「当前 ready 成员数 × maxDeliveryAttempts」约束(成员上限 ~8), 不会失控。
		s.markDone(m.GroupID, m.ID)
		s.kick(bg, m.GroupID)
		return
	}
	// 成员 turn 已真正起步:推 running 让正在看群的 roster 立刻翻状态点(已订阅
	// group:event 的群视图收得到;中途打开群的走 LoadGroup 快照回填)。
	s.emitMemberRunState(bg, m.GroupID, m.ID, sessionID, RunStateRunning)
	gogo.Go(func() error {
		defer cancel()
		res := <-ch
		s.handleTurnResult(context.Background(), m.GroupID, m, res)
		return nil
	}, gogo.WithIgnorePanic())
}

func (s *groupSvc) ensureBackingSession(ctx context.Context, g *group_entity.Group, m *group_entity.GroupMember) (int64, error) {
	if m.BackingSessionID > 0 {
		return m.BackingSessionID, nil
	}
	resp, err := s.gw.EnsureSession(ctx, &chat_svc.EnsureSessionRequest{
		Purpose:   chat_svc.SessionPurposeGroupMember,
		AgentID:   m.AgentID,
		ProjectID: g.ProjectID,
		GroupID:   g.ID,
		Title:     s.memberSessionTitle(ctx, g, m.AgentID),
	})
	if err != nil {
		return 0, err
	}
	if resp == nil || resp.SessionID <= 0 {
		return 0, errors.New("empty session id")
	}
	m.BackingSessionID = resp.SessionID
	if err := group_repo.Member().Update(ctx, m); err != nil {
		return 0, err
	}
	s.emitter.Emit(ctx, groupEventName(m.GroupID), map[string]any{
		"kind":   "member_updated",
		"member": toGroupMemberEvent(m),
	})
	return resp.SessionID, nil
}

// handleTurnResult 仅管生命周期: 释放 inflight 槽 + kick(填新槽; 全空则 quiesce → waiting_user)。
// 不解析文本/不落消息 —— 成员发言来自 turn 进行中的 group_send tool 调用(IngestAgentMessage)。
func (s *groupSvc) handleTurnResult(ctx context.Context, groupID int64, m *group_entity.GroupMember, res chat_svc.TurnResult) {
	s.markDone(groupID, m.ID)
	// 槽已释放:推 idle 让 roster 翻回空转点。出错也算这一轮结束(错误详情在该成员的
	// backing session 里),roster 只表达"在跑/没跑"。下一条 pending 会由 kick 再翻 running。
	s.emitMemberRunState(ctx, groupID, m.ID, m.BackingSessionID, RunStateIdle)
	if res.Err != nil {
		logger.Ctx(ctx).Warn("group_svc.handleTurnResult: member turn error", zap.Int64("memberId", m.ID), zap.Error(res.Err))
	}
	s.kick(ctx, groupID)
}
