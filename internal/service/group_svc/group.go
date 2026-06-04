// Package group_svc 提供群聊编排应用服务(架在 chat_svc 之上)。
package group_svc

import (
	"context"
	"sync"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"agentre/internal/model/entity/group_entity"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/pkg/code"
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
}

type groupSvc struct {
	gw         ChatGateway
	emitter    Emitter
	now        func() int64
	mu         sync.Mutex           // 保护 schedulers
	schedulers map[int64]*scheduler // groupID -> 运行态(Task C5)
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
		schedulers: map[int64]*scheduler{},
	}
}

// NewForTest 注入 mock 网关构造服务(单测用)。
func NewForTest(gw ChatGateway) GroupSvc { return newGroupSvc(gw, NoopEmitter{}) }

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
