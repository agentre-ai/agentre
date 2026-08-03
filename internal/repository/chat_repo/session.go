// Package chat_repo 提供 chat session / message 的持久化访问。
package chat_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
)

//go:generate mockgen -source session.go -destination mock_chat_repo/mock_session.go

type SessionRepo interface {
	Find(ctx context.Context, id int64) (*chat_entity.Session, error)
	ListByAgent(ctx context.Context, agentID int64, limit int) ([]*chat_entity.Session, error)
	ListByAgentIncludingGroups(ctx context.Context, agentID int64, limit int) ([]*chat_entity.Session, error)
	ListByAgentPaged(ctx context.Context, agentID int64, offset, limit int) ([]*chat_entity.Session, error)
	ListByAgentPagedIncludingGroups(ctx context.Context, agentID int64, offset, limit int) ([]*chat_entity.Session, error)
	ListIDsByAgents(ctx context.Context, agentIDs []int64) (map[int64][]int64, error)
	ListIDsByAgentsIncludingGroups(ctx context.Context, agentIDs []int64) (map[int64][]int64, error)
	ListAttentionByAgent(ctx context.Context, agentID int64, limit int) ([]*chat_entity.Session, error)
	ListAttentionByAgentIncludingGroups(ctx context.Context, agentID int64, limit int) ([]*chat_entity.Session, error)
	ListByProject(ctx context.Context, projectID int64) ([]*chat_entity.Session, error)
	CountByAgent(ctx context.Context, agentID int64) (int64, error)
	CountByAgentIncludingGroups(ctx context.Context, agentID int64) (int64, error)
	CountByAgents(ctx context.Context, agentIDs []int64) (map[int64]int64, error)
	CountByAgentsIncludingGroups(ctx context.Context, agentIDs []int64) (map[int64]int64, error)
	CountRunningByAgents(ctx context.Context, agentIDs []int64) (map[int64]int, error)
	CountActiveByProject(ctx context.Context, projectID int64, agentStatuses []string) (int64, error)
	CountActive(ctx context.Context, agentStatuses []string) (int64, error)
	Create(ctx context.Context, s *chat_entity.Session) error
	Update(ctx context.Context, s *chat_entity.Session) error
	UpdatePermissionMode(ctx context.Context, sessionID int64, mode string) error
	// UpdatePermissionModeAtLaunch sets the launched-mode snapshot for a session.
	// Called by the claudecode runner after spawning the CLI subprocess. Never
	// invoked through the user-facing SetPermissionMode IPC — that one only
	// touches permission_mode.
	UpdatePermissionModeAtLaunch(ctx context.Context, sessionID int64, mode string) error
	// UpdateExecDaemon 记录执行该会话的配对 daemon(paired_agentreds.id)及其实例标识
	// (sha256:<hex>)。deviceID=0 + 空标识表示回到本机执行。实例标识变了(改绑到别的
	// daemon / 改回本机)时,event_cursor 在同一条语句里归零 —— 游标只在它所属的那条
	// 通知日志里有意义,不能跟着会话漂到另一台 daemon 上。标识不变则原样保留游标。
	UpdateExecDaemon(ctx context.Context, sessionID int64, deviceID int64, daemonFingerprint string) error
	// UpdateEventCursor 记录桌面端已消费到的 daemon 通知 seq。只碰这一列,执行位置与
	// 实例标识由 UpdateExecDaemon 负责。daemonFingerprint 是 seq 所属的那条通知日志的
	// daemon 实例标识,进 WHERE 做守卫:会话已改绑后老连接迟到的写入落空(不报错,同
	// MarkRead 的「写不进也算成功」),下次重连至多重复拉取,而不会跳过新日志的开头。
	UpdateEventCursor(ctx context.Context, sessionID int64, daemonFingerprint string, seq int64) error
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

// nonSubagentScope 排除子 agent 委派会话(purpose='subagent_call')。这类会话由 agent_call
// 同步委派出来、一次性隔离, 不是用户顶层会话, 不应出现在任何 agent/项目的会话列表或计数里。
// 本 scope 必须无条件挂在每个列表/计数查询上, 否则它会从侧栏(走 IncludingGroups 变体)漏出来。
func nonSubagentScope(db *gorm.DB) *gorm.DB {
	return db.Where("purpose <> ?", chat_entity.SessionPurposeSubagent)
}

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
	return r.listByAgent(ctx, agentID, limit, true)
}

func (r *sessionRepo) ListByAgentIncludingGroups(ctx context.Context, agentID int64, limit int) ([]*chat_entity.Session, error) {
	return r.listByAgent(ctx, agentID, limit, false)
}

func (r *sessionRepo) listByAgent(ctx context.Context, agentID int64, limit int, ordinaryOnly bool) ([]*chat_entity.Session, error) {
	if limit <= 0 {
		limit = 5
	}
	var rows []*chat_entity.Session
	q := db.Ctx(ctx).
		Where("agent_id = ? AND status = ?", agentID, consts.ACTIVE).
		Scopes(nonSubagentScope)
	err := q.
		Order("last_message_at DESC, id DESC").
		Limit(limit).
		Find(&rows).Error
	applySessionDerivedFields(rows)
	return rows, err
}

// ListByAgentPaged 按 last_message_at DESC 翻页返回 agent 的未删除会话。
// 服务层负责对 offset/limit 做边界裁剪；repo 只忠实按参数查。
func (r *sessionRepo) ListByAgentPaged(ctx context.Context, agentID int64, offset, limit int) ([]*chat_entity.Session, error) {
	return r.listByAgentPaged(ctx, agentID, offset, limit, true)
}

func (r *sessionRepo) ListByAgentPagedIncludingGroups(ctx context.Context, agentID int64, offset, limit int) ([]*chat_entity.Session, error) {
	return r.listByAgentPaged(ctx, agentID, offset, limit, false)
}

func (r *sessionRepo) listByAgentPaged(ctx context.Context, agentID int64, offset, limit int, ordinaryOnly bool) ([]*chat_entity.Session, error) {
	var rows []*chat_entity.Session
	q := db.Ctx(ctx).
		Where("agent_id = ? AND status = ?", agentID, consts.ACTIVE).
		Scopes(nonSubagentScope)
	err := q.
		Order("last_message_at DESC, id DESC").
		Offset(offset).
		Limit(limit).
		Find(&rows).Error
	applySessionDerivedFields(rows)
	return rows, err
}

func (r *sessionRepo) ListIDsByAgents(ctx context.Context, agentIDs []int64) (map[int64][]int64, error) {
	return r.listIDsByAgents(ctx, agentIDs, true)
}

func (r *sessionRepo) ListIDsByAgentsIncludingGroups(ctx context.Context, agentIDs []int64) (map[int64][]int64, error) {
	return r.listIDsByAgents(ctx, agentIDs, false)
}

func (r *sessionRepo) listIDsByAgents(ctx context.Context, agentIDs []int64, ordinaryOnly bool) (map[int64][]int64, error) {
	out := make(map[int64][]int64, len(agentIDs))
	if len(agentIDs) == 0 {
		return out, nil
	}
	rows := []struct {
		AgentID int64 `gorm:"column:agent_id"`
		ID      int64 `gorm:"column:id"`
	}{}
	q := db.Ctx(ctx).
		Table("chat_sessions").
		Select("agent_id, id").
		Where("agent_id IN ? AND status = ?", agentIDs, consts.ACTIVE).
		Scopes(nonSubagentScope)
	err := q.
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
	return r.listAttentionByAgent(ctx, agentID, limit, true)
}

func (r *sessionRepo) ListAttentionByAgentIncludingGroups(ctx context.Context, agentID int64, limit int) ([]*chat_entity.Session, error) {
	return r.listAttentionByAgent(ctx, agentID, limit, false)
}

func (r *sessionRepo) listAttentionByAgent(ctx context.Context, agentID int64, limit int, ordinaryOnly bool) ([]*chat_entity.Session, error) {
	var rows []*chat_entity.Session
	q := db.Ctx(ctx).
		Where("agent_id = ? AND status = ? AND agent_status IN ?",
			agentID, consts.ACTIVE, []string{"running", "waiting", "error"}).
		Scopes(nonSubagentScope)
	err := q.
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
	return r.countByAgents(ctx, agentIDs, true)
}

func (r *sessionRepo) CountByAgentsIncludingGroups(ctx context.Context, agentIDs []int64) (map[int64]int64, error) {
	return r.countByAgents(ctx, agentIDs, false)
}

func (r *sessionRepo) countByAgents(ctx context.Context, agentIDs []int64, ordinaryOnly bool) (map[int64]int64, error) {
	out := make(map[int64]int64, len(agentIDs))
	if len(agentIDs) == 0 {
		return out, nil
	}
	rows := []struct {
		AgentID int64 `gorm:"column:agent_id"`
		N       int64 `gorm:"column:n"`
	}{}
	q := db.Ctx(ctx).
		Table("chat_sessions").
		Select("agent_id, COUNT(*) AS n").
		Where("agent_id IN ? AND status = ?", agentIDs, consts.ACTIVE).
		Scopes(nonSubagentScope)
	err := q.
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
	return r.countByAgent(ctx, agentID, true)
}

func (r *sessionRepo) CountByAgentIncludingGroups(ctx context.Context, agentID int64) (int64, error) {
	return r.countByAgent(ctx, agentID, false)
}

func (r *sessionRepo) countByAgent(ctx context.Context, agentID int64, ordinaryOnly bool) (int64, error) {
	var n int64
	q := db.Ctx(ctx).
		Model(&chat_entity.Session{}).
		Where("agent_id = ? AND status = ?", agentID, consts.ACTIVE).
		Scopes(nonSubagentScope)
	err := q.Count(&n).Error
	return n, err
}

// CountRunningByAgents 统计每个 agent 处在 "running" 状态的未删除会话数,
// 用于侧栏判断 agent 是否真的正在跑 turn(对应 UI 上的"运行中"呼吸灯)。
// 注意:不要把 consts.ACTIVE(软删除位)误用为"运行中"语义 —— 那会让任何有历史会话的
// agent 一直亮灯。真实"是否在跑"由 chat_sessions.agent_status 表达。
//
// 挂 nonSubagentScope: 子 agent 委派会话从侧栏隐藏、点不进去,让它点亮呼吸灯会留下
// 「亮灯却无会话可看」的死角,故运行中的子 agent 轮不计入呼吸灯。
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
		Scopes(nonSubagentScope).
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
// 子 agent 委派会话(purpose=subagent_call)仍被 nonSubagentScope 排除。
func (r *sessionRepo) ListByProject(ctx context.Context, projectID int64) ([]*chat_entity.Session, error) {
	var rows []*chat_entity.Session
	err := db.Ctx(ctx).
		Where("project_id = ? AND status = ?", projectID, consts.ACTIVE).
		Scopes(nonSubagentScope).
		Order("last_message_at DESC, id DESC").
		Find(&rows).Error
	applySessionDerivedFields(rows)
	return rows, err
}

// CountActiveByProject 统计项目下 status=ACTIVE 且 agent_status 在指定集合内的会话数。
// project_svc.Delete 用它做守门：还有 running/waiting 会话时拒绝删项目。
// 子 agent 委派会话仍被 nonSubagentScope 排除。
func (r *sessionRepo) CountActiveByProject(ctx context.Context, projectID int64, agentStatuses []string) (int64, error) {
	q := db.Ctx(ctx).
		Model(&chat_entity.Session{}).
		Where("project_id = ? AND status = ?", projectID, consts.ACTIVE)
	if len(agentStatuses) > 0 {
		q = q.Where("agent_status IN ?", agentStatuses)
	}
	q = q.Scopes(nonSubagentScope)
	var n int64
	err := q.Count(&n).Error
	return n, err
}

// CountActive 统计 status=ACTIVE 且 agent_status 在指定集合内的会话总数(跨所有 agent/项目)。
// 退出二次确认用它判断是否还有进行中的会话:agentStatuses 传 {"running","waiting"}。
func (r *sessionRepo) CountActive(ctx context.Context, agentStatuses []string) (int64, error) {
	var n int64
	err := db.Ctx(ctx).
		Model(&chat_entity.Session{}).
		Where("status = ? AND agent_status IN ?", consts.ACTIVE, agentStatuses).
		Scopes(nonSubagentScope).
		Count(&n).Error
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

func (r *sessionRepo) UpdateExecDaemon(ctx context.Context, sessionID int64, deviceID int64, daemonFingerprint string) error {
	return db.Ctx(ctx).Model(&chat_entity.Session{}).
		Where("id = ? AND status = ?", sessionID, consts.ACTIVE).
		Updates(map[string]any{
			"exec_device_id":          deviceID,
			"exec_daemon_fingerprint": daemonFingerprint,
			// 换了一台 daemon 实例(含改回本机的空标识)就在同一条语句里把游标归零:
			// 老游标指的是老 daemon 通知日志里的位置,留着会被下次 LoadCursor 当成对新
			// daemon 有效。SQL 的 SET 右值一律读改写前的行值,所以这里比的是老标识。
			"event_cursor": gorm.Expr(
				"CASE WHEN exec_daemon_fingerprint = ? THEN event_cursor ELSE 0 END", daemonFingerprint),
			"updatetime": time.Now().UnixMilli(),
		}).Error
}

func (r *sessionRepo) UpdateEventCursor(ctx context.Context, sessionID int64, daemonFingerprint string, seq int64) error {
	return db.Ctx(ctx).Model(&chat_entity.Session{}).
		Where("id = ? AND status = ? AND exec_daemon_fingerprint = ?", sessionID, consts.ACTIVE, daemonFingerprint).
		Updates(map[string]any{
			"event_cursor": seq,
			"updatetime":   time.Now().UnixMilli(),
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
