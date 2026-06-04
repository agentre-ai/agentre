// Package group_svc 提供群聊编排应用服务(架在 chat_svc 之上)。
package group_svc

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"agentre/internal/model/entity/group_entity"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/group_repo"
)

// Emitter 群事件出口(由 app 层注入 → wailsruntime.EventsEmit)。
type Emitter interface {
	Emit(ctx context.Context, name string, payload any)
}

type EmitterFunc func(ctx context.Context, name string, payload any)

func (f EmitterFunc) Emit(ctx context.Context, name string, payload any) {
	if f != nil {
		f(ctx, name, payload)
	}
}

type NoopEmitter struct{}

func (NoopEmitter) Emit(context.Context, string, any) {}

// scheduler 群运行态占位(Task C5 实现真正的并发 fan-out 调度器)。
type scheduler struct{}

// GroupSvc 群聊编排服务。
type GroupSvc interface {
	ListGroups(ctx context.Context) ([]*group_entity.Group, error)
	CreateGroup(ctx context.Context, req *CreateGroupRequest) (*GroupDetail, error)
	LoadGroup(ctx context.Context, id int64) (*GroupDetail, error)
	AddGroupMember(ctx context.Context, groupID, agentID int64) (*group_entity.GroupMember, error)
	RemoveGroupMember(ctx context.Context, memberID int64) error
	SendGroupMessage(ctx context.Context, req *SendGroupMessageRequest) error
}

type groupSvc struct {
	gw         ChatGateway
	emitter    Emitter
	now        func() int64
	names      func(ctx context.Context, agentID int64) string // agent id -> 展示名
	mu         sync.Mutex                                      // 保护 schedulers
	schedulers map[int64]*scheduler                            // groupID -> 运行态(Task C5)
}

var defaultGroup GroupSvc = newGroupSvc(chatSvcGateway{}, NoopEmitter{})

func Default() GroupSvc     { return defaultGroup }
func SetDefault(s GroupSvc) { defaultGroup = s }
func SetEmitter(e Emitter) {
	if g, ok := defaultGroup.(*groupSvc); ok && e != nil {
		g.emitter = e
	}
}

func newGroupSvc(gw ChatGateway, e Emitter) *groupSvc {
	return &groupSvc{
		gw:         gw,
		emitter:    e,
		now:        func() int64 { return time.Now().UnixMilli() },
		names:      defaultNameResolver,
		schedulers: map[int64]*scheduler{},
	}
}

// defaultNameResolver 把 agent id 解析成展示名(找不到/出错返回空串)。
func defaultNameResolver(ctx context.Context, agentID int64) string {
	a, err := agent_repo.Agent().Find(ctx, agentID)
	if err != nil || a == nil {
		return ""
	}
	return a.Name
}

// NewForTest 注入 mock 网关构造服务(单测用)。
func NewForTest(gw ChatGateway) GroupSvc { return newGroupSvc(gw, NoopEmitter{}) }

// NewForTestWithNames 注入 mock 网关 + 固定名字表构造服务(单测用)。
func NewForTestWithNames(gw ChatGateway, names map[int64]string) GroupSvc {
	s := newGroupSvc(gw, NoopEmitter{})
	s.names = func(_ context.Context, id int64) string { return names[id] }
	return s
}

func (s *groupSvc) ListGroups(ctx context.Context) ([]*group_entity.Group, error) {
	return group_repo.Group().List(ctx)
}

func (s *groupSvc) CreateGroup(ctx context.Context, req *CreateGroupRequest) (*GroupDetail, error) {
	g := &group_entity.Group{
		Title:              req.Title,
		CoordinatorAgentID: req.CoordinatorAgentID,
		DepartmentID:       req.DepartmentID,
		ProjectID:          req.ProjectID,
		RunStatus:          group_entity.RunIdle,
		Status:             consts.ACTIVE,
	}
	if err := g.Check(ctx); err != nil {
		return nil, err
	}
	if !s.backendSupportsGroup(ctx, req.CoordinatorAgentID) {
		return nil, i18n.NewError(ctx, code.GroupBackendUnsupported)
	}
	if err := group_repo.Group().Create(ctx, g); err != nil {
		return nil, err
	}
	if _, err := s.ensureMember(ctx, g, req.CoordinatorAgentID, group_entity.RoleCoordinator); err != nil {
		return nil, err
	}
	logger.Ctx(ctx).Info("group_svc.CreateGroup: created",
		zap.Int64("groupID", g.ID), zap.Int64("coordinatorAgentID", req.CoordinatorAgentID))
	return s.LoadGroup(ctx, g.ID)
}

// ensureMember 幂等地把 agent 加入群(建 member + backing session)。
// 已存在且 active → 直接返回; 已存在但 left → 复活(Update); 不存在 → 新建(Create)。
func (s *groupSvc) ensureMember(ctx context.Context, g *group_entity.Group, agentID int64, role string) (*group_entity.GroupMember, error) {
	existing, err := group_repo.Member().FindByGroupAndAgent(ctx, g.ID, agentID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.IsActive() {
		return existing, nil
	}
	sessID, err := s.gw.EnsureGroupMemberSession(ctx, agentID, g.ProjectID, g.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		m := &group_entity.GroupMember{
			GroupID:          g.ID,
			AgentID:          agentID,
			BackingSessionID: sessID,
			Role:             role,
			Status:           group_entity.MemberActive,
			JoinedAt:         s.now(),
		}
		if err := group_repo.Member().Create(ctx, m); err != nil {
			return nil, err
		}
		return m, nil
	}
	// existing != nil && !existing.IsActive(): 之前离开过的成员复活。
	// group_members 有 UNIQUE(group_id, agent_id), 必须 Update 而非 Create。
	existing.BackingSessionID = sessID
	existing.Role = role
	existing.Status = group_entity.MemberActive
	existing.JoinedAt = s.now()
	if err := group_repo.Member().Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *groupSvc) LoadGroup(ctx context.Context, id int64) (*GroupDetail, error) {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, i18n.NewError(ctx, code.GroupNotFound)
	}
	members, err := group_repo.Member().ListByGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	msgs, err := group_repo.Message().ListByGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	return &GroupDetail{Group: g, Members: members, Messages: msgs}, nil
}

func (s *groupSvc) AddGroupMember(ctx context.Context, groupID, agentID int64) (*group_entity.GroupMember, error) {
	g, err := group_repo.Group().Find(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, i18n.NewError(ctx, code.GroupNotFound)
	}
	members, err := group_repo.Member().ListByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if len(members) >= maxMembers {
		return nil, i18n.NewError(ctx, code.GroupMemberLimit)
	}
	if !s.backendSupportsGroup(ctx, agentID) {
		return nil, i18n.NewError(ctx, code.GroupBackendUnsupported)
	}
	return s.ensureMember(ctx, g, agentID, group_entity.RoleMember)
}

// backendSupportsGroup 门控成员后端是否支持群聊(必须声明 CapMCPTools 才能被注入 group_send tool)。
func (s *groupSvc) backendSupportsGroup(ctx context.Context, agentID int64) bool {
	ok, err := s.gw.AgentBackendHasCapability(ctx, agentID, capability.CapMCPTools)
	if err != nil {
		logger.Ctx(ctx).Warn("group_svc.backendSupportsGroup: capability probe failed",
			zap.Int64("agentID", agentID), zap.Error(err))
		return false
	}
	return ok
}

func (s *groupSvc) RemoveGroupMember(ctx context.Context, memberID int64) error {
	m, err := group_repo.Member().Find(ctx, memberID)
	if err != nil {
		return err
	}
	if m == nil {
		return i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	m.Status = group_entity.MemberLeft
	return group_repo.Member().Update(ctx, m)
}

// SendGroupMessage 把一条用户消息投入群: 解析收件人 → 落 group_message → 入队 agent 收件人。
func (s *groupSvc) SendGroupMessage(ctx context.Context, req *SendGroupMessageRequest) error {
	g, err := group_repo.Group().Find(ctx, req.GroupID)
	if err != nil {
		return err
	}
	if g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return err
	}
	recipientIDs, toUser := s.resolveRecipientsFromRequest(req)
	if len(recipientIDs) == 0 && !toUser { // 用户没选收件人 → 默认投协调者(spec §17)
		for _, m := range members {
			if m.IsCoordinator() {
				recipientIDs = []int64{m.ID}
				break
			}
		}
	}
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindUser, 0, req.Text, recipientIDs, toUser, 0); err != nil {
		return err
	}
	// 用户发言重置 round_count(仅 UI 计数)
	g.RoundCount = 0
	_ = group_repo.Group().Update(ctx, g)
	logger.Ctx(ctx).Info("group_svc.SendGroupMessage: sent",
		zap.Int64("groupID", g.ID), zap.Int64s("recipientMemberIDs", recipientIDs), zap.Bool("toUser", toUser))
	// 把 agent 收件人入队 + 踢调度器(C5 实现真逻辑; 本 Task 占位)
	s.enqueueDeliveries(g.ID, recipientIDs, req.Text, "你")
	s.kick(ctx, g.ID)
	return nil
}

// resolveRecipientsFromRequest: 用户消息收件人已由前端解析成结构化字段; 后端不做文本 mention 解析。
func (s *groupSvc) resolveRecipientsFromRequest(req *SendGroupMessageRequest) ([]int64, bool) {
	return req.RecipientMemberIDs, req.ToUser
}

func (s *groupSvc) persistMessage(ctx context.Context, g *group_entity.Group, kind string, senderMemberID int64, content string, recipients []int64, toUser bool, sourceMsgID int64) (*group_entity.GroupMessage, error) {
	seq, err := group_repo.Message().NextSeq(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	m := &group_entity.GroupMessage{
		GroupID:         g.ID,
		Seq:             seq,
		SenderKind:      kind,
		SenderMemberID:  senderMemberID,
		ToUser:          toUser,
		Content:         content,
		SourceMessageID: sourceMsgID,
		Createtime:      s.now(),
	}
	m.SetRecipients(recipients)
	if err := group_repo.Message().Create(ctx, m); err != nil {
		return nil, err
	}
	s.emitter.Emit(ctx, groupEventName(g.ID), map[string]any{"kind": "message", "message": m})
	return m, nil
}

func groupEventName(groupID int64) string { return "group:event:" + strconv.FormatInt(groupID, 10) }

// enqueueDeliveries / kick 是调度占位; C5 实现真正的并发 fan-out 调度。
func (s *groupSvc) enqueueDeliveries(groupID int64, recipientIDs []int64, content, fromName string) {}
func (s *groupSvc) kick(ctx context.Context, groupID int64)                                         {}
