// Package group_repo 提供群聊 Group / Member / Message 的持久化访问。
package group_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"agentre/internal/model/entity/group_entity"
)

//go:generate mockgen -source group.go -destination mock_group_repo/mock_group.go

// GroupRepo 群房间仓储。
type GroupRepo interface {
	Create(ctx context.Context, g *group_entity.Group) error
	Update(ctx context.Context, g *group_entity.Group) error
	SetPinned(ctx context.Context, id int64, pinned bool) error
	Find(ctx context.Context, id int64) (*group_entity.Group, error)
	List(ctx context.Context) ([]*group_entity.Group, error)
}

// GroupMemberRepo 群成员仓储。
type GroupMemberRepo interface {
	Create(ctx context.Context, m *group_entity.GroupMember) error
	Update(ctx context.Context, m *group_entity.GroupMember) error
	Find(ctx context.Context, id int64) (*group_entity.GroupMember, error)
	FindByGroupAndAgent(ctx context.Context, groupID, agentID int64) (*group_entity.GroupMember, error)
	ListByGroup(ctx context.Context, groupID int64) ([]*group_entity.GroupMember, error)
}

// GroupMessageRepo 群消息仓储。
type GroupMessageRepo interface {
	Create(ctx context.Context, m *group_entity.GroupMessage) error
	ListByGroup(ctx context.Context, groupID int64) ([]*group_entity.GroupMessage, error)
	NextSeq(ctx context.Context, groupID int64) (int, error)
}

var (
	defaultGroup   GroupRepo
	defaultMember  GroupMemberRepo
	defaultMessage GroupMessageRepo
)

func Group() GroupRepo                      { return defaultGroup }
func Member() GroupMemberRepo               { return defaultMember }
func Message() GroupMessageRepo             { return defaultMessage }
func RegisterGroup(impl GroupRepo)          { defaultGroup = impl }
func RegisterMember(impl GroupMemberRepo)   { defaultMember = impl }
func RegisterMessage(impl GroupMessageRepo) { defaultMessage = impl }
func NewGroup() GroupRepo                   { return &groupRepo{} }
func NewMember() GroupMemberRepo            { return &memberRepo{} }
func NewMessage() GroupMessageRepo          { return &messageRepo{} }

type groupRepo struct{}
type memberRepo struct{}
type messageRepo struct{}

func (r *groupRepo) Create(ctx context.Context, g *group_entity.Group) error {
	now := time.Now().UnixMilli()
	if g.Createtime == 0 {
		g.Createtime = now
	}
	g.Updatetime = now
	return db.Ctx(ctx).Create(g).Error
}

func (r *groupRepo) Update(ctx context.Context, g *group_entity.Group) error {
	g.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Model(&group_entity.Group{}).
		Where("id = ? AND status = ?", g.ID, consts.ACTIVE).
		Updates(map[string]any{
			"title":       g.Title,
			"run_status":  g.RunStatus,
			"round_count": g.RoundCount,
			"status":      g.Status,
			"updatetime":  g.Updatetime,
		}).Error
}

// SetPinned 切换群置顶。顺带 bump updatetime, 让刚置顶的群在混排活跃度排序里浮上来。
func (r *groupRepo) SetPinned(ctx context.Context, id int64, pinned bool) error {
	return db.Ctx(ctx).Model(&group_entity.Group{}).
		Where("id = ? AND status = ?", id, consts.ACTIVE).
		Updates(map[string]any{
			"pinned":     pinned,
			"updatetime": time.Now().UnixMilli(),
		}).Error
}

func (r *groupRepo) Find(ctx context.Context, id int64) (*group_entity.Group, error) {
	out := &group_entity.Group{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *groupRepo) List(ctx context.Context) ([]*group_entity.Group, error) {
	var rows []*group_entity.Group
	err := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).Order("updatetime DESC, id DESC").Find(&rows).Error
	return rows, err
}

func (r *memberRepo) Create(ctx context.Context, m *group_entity.GroupMember) error {
	if m.JoinedAt == 0 {
		m.JoinedAt = time.Now().UnixMilli()
	}
	return db.Ctx(ctx).Create(m).Error
}

func (r *memberRepo) Update(ctx context.Context, m *group_entity.GroupMember) error {
	return db.Ctx(ctx).Model(&group_entity.GroupMember{}).
		Where("id = ?", m.ID).
		Updates(map[string]any{
			"backing_session_id": m.BackingSessionID,
			"role":               m.Role,
			"status":             m.Status,
		}).Error
}

func (r *memberRepo) Find(ctx context.Context, id int64) (*group_entity.GroupMember, error) {
	out := &group_entity.GroupMember{}
	err := db.Ctx(ctx).Where("id = ?", id).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *memberRepo) FindByGroupAndAgent(ctx context.Context, groupID, agentID int64) (*group_entity.GroupMember, error) {
	out := &group_entity.GroupMember{}
	err := db.Ctx(ctx).Where("group_id = ? AND agent_id = ?", groupID, agentID).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *memberRepo) ListByGroup(ctx context.Context, groupID int64) ([]*group_entity.GroupMember, error) {
	var rows []*group_entity.GroupMember
	err := db.Ctx(ctx).Where("group_id = ? AND status = ?", groupID, group_entity.MemberActive).
		Order("id ASC").Find(&rows).Error
	return rows, err
}

func (r *messageRepo) Create(ctx context.Context, m *group_entity.GroupMessage) error {
	if m.Createtime == 0 {
		m.Createtime = time.Now().UnixMilli()
	}
	return db.Ctx(ctx).Create(m).Error
}

func (r *messageRepo) ListByGroup(ctx context.Context, groupID int64) ([]*group_entity.GroupMessage, error) {
	var rows []*group_entity.GroupMessage
	err := db.Ctx(ctx).Where("group_id = ?", groupID).Order("seq ASC, id ASC").Find(&rows).Error
	return rows, err
}

func (r *messageRepo) NextSeq(ctx context.Context, groupID int64) (int, error) {
	var maxSeq int
	err := db.Ctx(ctx).Model(&group_entity.GroupMessage{}).
		Where("group_id = ?", groupID).
		Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error
	if err != nil {
		return 0, err
	}
	return maxSeq + 1, nil
}
