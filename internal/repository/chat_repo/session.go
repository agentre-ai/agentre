// Package chat_repo 提供 chat session / message 的持久化访问。
package chat_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"agentre/internal/model/entity/chat_entity"
)

//go:generate mockgen -source session.go -destination mock_chat_repo/mock_session.go

type SessionRepo interface {
	Find(ctx context.Context, id int64) (*chat_entity.Session, error)
	ListByAgent(ctx context.Context, agentID int64, limit int) ([]*chat_entity.Session, error)
	ListByAgentPaged(ctx context.Context, agentID int64, offset, limit int) ([]*chat_entity.Session, error)
	ListIDsByAgents(ctx context.Context, agentIDs []int64) (map[int64][]int64, error)
	ListAttentionByAgent(ctx context.Context, agentID int64, limit int) ([]*chat_entity.Session, error)
	ListByProject(ctx context.Context, projectID int64) ([]*chat_entity.Session, error)
	CountByAgent(ctx context.Context, agentID int64) (int64, error)
	CountByAgents(ctx context.Context, agentIDs []int64) (map[int64]int64, error)
	CountRunningByAgents(ctx context.Context, agentIDs []int64) (map[int64]int, error)
	CountActiveByProject(ctx context.Context, projectID int64, agentStatuses []string) (int64, error)
	Create(ctx context.Context, s *chat_entity.Session) error
	Update(ctx context.Context, s *chat_entity.Session) error
	UpdatePermissionMode(ctx context.Context, sessionID int64, mode string) error
	// UpdatePermissionModeAtLaunch sets the launched-mode snapshot for a session.
	// Called by the claudecode runner after spawning the CLI subprocess. Never
	// invoked through the user-facing SetPermissionMode IPC — that one only
	// touches permission_mode.
	UpdatePermissionModeAtLaunch(ctx context.Context, sessionID int64, mode string) error
	// MarkRead 单调推进 last_read_at: 仅当 ts 严格大于当前值时写入。
	// 避免 stream-done 与 LoadSession 乱序时把已读时间冲回旧值。
	// 会话不存在 / 已软删 / ts 不更新 都算成功（不返回 ErrRecordNotFound）。
	MarkRead(ctx context.Context, sessionID int64, ts int64) error
	SoftDelete(ctx context.Context, id int64) error
	// ResetActiveSessions 启动期把所有 agent_status IN ('running','waiting') 且
	// 未软删除的 session 翻成 'error'。app crash / 强行
	// 重启 / wails dev hot-reload 都会留下 turn goroutine 死了但 DB 状态没收
	// 尾的"重启遗孤",前端 sidebar 会一直亮"运行中"。该清理不能在 bootstrap.Init
	// 里直接调用；主 Wails 实例 Startup 后再调,确保第二实例不会误伤仍在运行的 turn。
	// 返回受影响行数,仅供日志使用。
	ResetActiveSessions(ctx context.Context) (int64, error)
}

var defaultSession SessionRepo

func Session() SessionRepo             { return defaultSession }
func RegisterSession(impl SessionRepo) { defaultSession = impl }
func NewSession() SessionRepo          { return &sessionRepo{} }

type sessionRepo struct{}

func (r *sessionRepo) Find(ctx context.Context, id int64) (*chat_entity.Session, error) {
	out := &chat_entity.Session{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out.ApplyDerivedFields()
	return out, nil
}

func (r *sessionRepo) ListByAgent(ctx context.Context, agentID int64, limit int) ([]*chat_entity.Session, error) {
	if limit <= 0 {
		limit = 5
	}
	var rows []*chat_entity.Session
	err := db.Ctx(ctx).
		Where("agent_id = ? AND status = ?", agentID, consts.ACTIVE).
		Order("last_message_at DESC, id DESC").
		Limit(limit).
		Find(&rows).Error
	applySessionDerivedFields(rows)
	return rows, err
}

// ListByAgentPaged 按 last_message_at DESC 翻页返回 agent 的未删除会话。
// 服务层负责对 offset/limit 做边界裁剪；repo 只忠实按参数查。
func (r *sessionRepo) ListByAgentPaged(ctx context.Context, agentID int64, offset, limit int) ([]*chat_entity.Session, error) {
	var rows []*chat_entity.Session
	err := db.Ctx(ctx).
		Where("agent_id = ? AND status = ?", agentID, consts.ACTIVE).
		Order("last_message_at DESC, id DESC").
		Offset(offset).
		Limit(limit).
		Find(&rows).Error
	applySessionDerivedFields(rows)
	return rows, err
}

func (r *sessionRepo) ListIDsByAgents(ctx context.Context, agentIDs []int64) (map[int64][]int64, error) {
	out := make(map[int64][]int64, len(agentIDs))
	if len(agentIDs) == 0 {
		return out, nil
	}
	rows := []struct {
		AgentID int64 `gorm:"column:agent_id"`
		ID      int64 `gorm:"column:id"`
	}{}
	err := db.Ctx(ctx).
		Table("chat_sessions").
		Select("agent_id, id").
		Where("agent_id IN ? AND status = ?", agentIDs, consts.ACTIVE).
		Order("agent_id ASC, last_message_at DESC, id DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.AgentID] = append(out[row.AgentID], row.ID)
	}
	return out, nil
}

// ListAttentionByAgent 给 sidebar 折叠态的 attention bubble 用：返回该 agent 下
// 当前需要用户关注的会话 —— 跑步中、等待用户输入/审批、或出错的。
// 按 last_message_at DESC 排序；limit 由 service 传入（典型 20，防止异常数据撑爆 UI）。
func (r *sessionRepo) ListAttentionByAgent(ctx context.Context, agentID int64, limit int) ([]*chat_entity.Session, error) {
	var rows []*chat_entity.Session
	err := db.Ctx(ctx).
		Where("agent_id = ? AND status = ? AND agent_status IN ?",
			agentID, consts.ACTIVE, []string{"running", "waiting", "error"}).
		Order("last_message_at DESC, id DESC").
		Limit(limit).
		Find(&rows).Error
	applySessionDerivedFields(rows)
	return rows, err
}

// CountByAgents 批量统计每个 agent 的未删除会话数。
// 用于 ListAgents 一次把侧栏「查看全部 N 个会话」需要的总数都查出来，
// 避免每个 agent 单独发一条 COUNT。
func (r *sessionRepo) CountByAgents(ctx context.Context, agentIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(agentIDs))
	if len(agentIDs) == 0 {
		return out, nil
	}
	rows := []struct {
		AgentID int64 `gorm:"column:agent_id"`
		N       int64 `gorm:"column:n"`
	}{}
	err := db.Ctx(ctx).
		Table("chat_sessions").
		Select("agent_id, COUNT(*) AS n").
		Where("agent_id IN ? AND status = ?", agentIDs, consts.ACTIVE).
		Group("agent_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.AgentID] = row.N
	}
	return out, nil
}

// CountByAgent 给 popover 拼 hasMore / "已加载 X / Y" 用。
func (r *sessionRepo) CountByAgent(ctx context.Context, agentID int64) (int64, error) {
	var n int64
	err := db.Ctx(ctx).
		Model(&chat_entity.Session{}).
		Where("agent_id = ? AND status = ?", agentID, consts.ACTIVE).
		Count(&n).Error
	return n, err
}

// CountRunningByAgents 统计每个 agent 处在 "running" 状态的未删除会话数,
// 用于侧栏判断 agent 是否真的正在跑 turn(对应 UI 上的"运行中"呼吸灯)。
// 注意:不要把 consts.ACTIVE(软删除位)误用为"运行中"语义 —— 那会让任何有历史会话的
// agent 一直亮灯。真实"是否在跑"由 chat_sessions.agent_status 表达。
func (r *sessionRepo) CountRunningByAgents(ctx context.Context, agentIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(agentIDs))
	if len(agentIDs) == 0 {
		return out, nil
	}
	rows := []struct {
		AgentID int64 `gorm:"column:agent_id"`
		N       int   `gorm:"column:n"`
	}{}
	err := db.Ctx(ctx).
		Table("chat_sessions").
		Select("agent_id, COUNT(*) AS n").
		Where("agent_id IN ? AND agent_status = ? AND status = ?", agentIDs, "running", consts.ACTIVE).
		Group("agent_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.AgentID] = row.N
	}
	return out, nil
}

// ListByProject 返回该项目下的全部未软删除会话，按 last_message_at DESC 排。
// 项目页 ChatProjectList 用它把 sessions 挂在 ProjectCard 下。
func (r *sessionRepo) ListByProject(ctx context.Context, projectID int64) ([]*chat_entity.Session, error) {
	var rows []*chat_entity.Session
	err := db.Ctx(ctx).
		Where("project_id = ? AND status = ?", projectID, consts.ACTIVE).
		Order("last_message_at DESC, id DESC").
		Find(&rows).Error
	applySessionDerivedFields(rows)
	return rows, err
}

// CountActiveByProject 统计项目下 status=ACTIVE 且 agent_status 在指定集合内的会话数。
// project_svc.Delete 用它做守门：还有 running/waiting 会话时拒绝删项目。
func (r *sessionRepo) CountActiveByProject(ctx context.Context, projectID int64, agentStatuses []string) (int64, error) {
	q := db.Ctx(ctx).
		Model(&chat_entity.Session{}).
		Where("project_id = ? AND status = ?", projectID, consts.ACTIVE)
	if len(agentStatuses) > 0 {
		q = q.Where("agent_status IN ?", agentStatuses)
	}
	var n int64
	err := q.Count(&n).Error
	return n, err
}

func (r *sessionRepo) Create(ctx context.Context, s *chat_entity.Session) error {
	now := time.Now().UnixMilli()
	if s.Createtime == 0 {
		s.Createtime = now
	}
	s.Updatetime = now
	err := db.Ctx(ctx).Create(s).Error
	s.ApplyDerivedFields()
	return err
}

func (r *sessionRepo) Update(ctx context.Context, s *chat_entity.Session) error {
	s.Updatetime = time.Now().UnixMilli()
	// Both permission_mode and permission_mode_at_launch are written via
	// dedicated single-column updates; omit them here so callers updating
	// status/timestamps don't clobber a concurrent mode switch or the
	// launched-mode snapshot.
	err := db.Ctx(ctx).Omit("permission_mode", "permission_mode_at_launch").Save(s).Error
	s.ApplyDerivedFields()
	return err
}

func (r *sessionRepo) UpdatePermissionMode(ctx context.Context, sessionID int64, mode string) error {
	return db.Ctx(ctx).Model(&chat_entity.Session{}).
		Where("id = ? AND status = ?", sessionID, consts.ACTIVE).
		Updates(map[string]any{
			"permission_mode": mode,
			"updatetime":      time.Now().UnixMilli(),
		}).Error
}

func (r *sessionRepo) UpdatePermissionModeAtLaunch(ctx context.Context, sessionID int64, mode string) error {
	return db.Ctx(ctx).Model(&chat_entity.Session{}).
		Where("id = ? AND status = ?", sessionID, consts.ACTIVE).
		Updates(map[string]any{
			"permission_mode_at_launch": mode,
			"updatetime":                time.Now().UnixMilli(),
		}).Error
}

func (r *sessionRepo) MarkRead(ctx context.Context, sessionID int64, ts int64) error {
	return db.Ctx(ctx).Model(&chat_entity.Session{}).
		Where("id = ? AND status = ? AND last_read_at < ?", sessionID, consts.ACTIVE, ts).
		Updates(map[string]any{
			"last_read_at": ts,
			"updatetime":   time.Now().UnixMilli(),
		}).Error
}

func (r *sessionRepo) ResetActiveSessions(ctx context.Context) (int64, error) {
	res := db.Ctx(ctx).Model(&chat_entity.Session{}).
		Where("agent_status IN ? AND status = ?", []string{"running", "waiting"}, consts.ACTIVE).
		Updates(map[string]any{
			"agent_status": "error",
			"updatetime":   time.Now().UnixMilli(),
		})
	return res.RowsAffected, res.Error
}

func (r *sessionRepo) SoftDelete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&chat_entity.Session{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":     consts.DELETE,
			"updatetime": time.Now().UnixMilli(),
		}).Error
}

func applySessionDerivedFields(rows []*chat_entity.Session) {
	for _, row := range rows {
		row.ApplyDerivedFields()
	}
}
