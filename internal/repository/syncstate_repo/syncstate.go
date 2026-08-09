// Package syncstate_repo 提供账号级七张表**同步元数据列**的统一访问。
//
// 为什么是一个包而不是七套方法：六列（sync_id / sync_account_id / sync_version /
// sync_updated_at / sync_origin / sync_deleted_at）由 syncmeta_entity.SyncMeta 匿名
// 内嵌进七张表，同名同型——这正是它们能被一套 SQL 覆盖的原因。业务列仍然只由各自
// 域的仓储写；这里只碰同步列，以及「按同步标识找到本机那一行」这一个跨域查询。
//
// 表名一律走 tableOf 的白名单，绝不接受调用方给的任意串。
package syncstate_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/cago-frame/cago/database/db"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/syncmeta_entity"
	"github.com/agentre-ai/agentre/internal/pkg/syncwire"
)

//go:generate mockgen -source syncstate.go -destination mock_syncstate_repo/mock_syncstate.go

// ErrUnknownKind 表示调用方给了一个不属于同步组的对象类型。
var ErrUnknownKind = errors.New("syncstate: unknown sync kind")

// SyncStateRepo 账号级七张表同步元数据列的访问接口。
type SyncStateRepo interface {
	// FindLocalID 按同步标识取本机自增主键；查不到返回 (0, nil)。
	// 只对有 id 列的对象类型可用——成员关系（project_agents）是联合主键，
	// 它也从不被别的行引用。
	FindLocalID(ctx context.Context, kind, syncID string) (int64, error)
	// FindVersion 按同步标识取本机存着的同步版本号与墓碑标记；查不到返回 found=false。
	FindVersion(ctx context.Context, kind, syncID string) (version int64, deleted bool, found bool, err error)
	// FindRow 按同步标识把整行读进 dest（不过滤 status —— 墓碑行也要读得到）。
	// 查不到返回 false。
	FindRow(ctx context.Context, kind, syncID string, dest any) (bool, error)
	// SaveMeta 按同步标识写回六列同步元数据，不碰任何业务列。
	SaveMeta(ctx context.Context, kind, syncID string, meta syncmeta_entity.SyncMeta) error
}

var defaultSyncState SyncStateRepo

// SyncState 取默认仓储单例。
func SyncState() SyncStateRepo { return defaultSyncState }

// RegisterSyncState 注入仓储实现，由 bootstrap 调用一次。
func RegisterSyncState(impl SyncStateRepo) { defaultSyncState = impl }

// NewSyncState 构造默认 GORM 实现。
func NewSyncState() SyncStateRepo { return &syncStateRepo{} }

type syncStateRepo struct{}

// tableOf 把同步组的对象类型映射到本机表名。白名单之外一律报错——表名会被拼进
// SQL，绝不接受调用方给的任意串。
func tableOf(kind string) (string, error) {
	switch kind {
	case syncwire.KindProject:
		return "projects", nil
	case syncwire.KindDepartment:
		return "departments", nil
	case syncwire.KindAgent:
		return "agents", nil
	case syncwire.KindAgentBackend:
		return "agent_backends", nil
	case syncwire.KindAgentExecTarget:
		return "agent_exec_targets", nil
	case syncwire.KindProjectAgent:
		return "project_agents", nil
	case syncwire.KindProjectLocation:
		return "project_locations", nil
	}
	return "", fmt.Errorf("%w: %s", ErrUnknownKind, kind)
}

func (r *syncStateRepo) FindLocalID(ctx context.Context, kind, syncID string) (int64, error) {
	table, err := tableOf(kind)
	if err != nil {
		return 0, err
	}
	if kind == syncwire.KindProjectAgent {
		return 0, fmt.Errorf("%w: %s has no auto-increment id", ErrUnknownKind, kind)
	}
	if syncID == "" {
		return 0, nil
	}
	var id int64
	err = db.Ctx(ctx).Table(table).
		Where("sync_id = ?", syncID).
		Select("id").Row().Scan(&id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || isNoRows(err) {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

func (r *syncStateRepo) FindVersion(ctx context.Context, kind, syncID string) (int64, bool, bool, error) {
	table, err := tableOf(kind)
	if err != nil {
		return 0, false, false, err
	}
	if syncID == "" {
		return 0, false, false, nil
	}
	var version, deletedAt int64
	err = db.Ctx(ctx).Table(table).
		Where("sync_id = ?", syncID).
		Select("sync_version", "sync_deleted_at").Row().Scan(&version, &deletedAt)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || isNoRows(err) {
			return 0, false, false, nil
		}
		return 0, false, false, err
	}
	return version, deletedAt > 0, true, nil
}

func (r *syncStateRepo) FindRow(ctx context.Context, kind, syncID string, dest any) (bool, error) {
	if _, err := tableOf(kind); err != nil {
		return false, err
	}
	if syncID == "" {
		return false, nil
	}
	err := db.Ctx(ctx).Where("sync_id = ?", syncID).Take(dest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *syncStateRepo) SaveMeta(ctx context.Context, kind, syncID string, meta syncmeta_entity.SyncMeta) error {
	table, err := tableOf(kind)
	if err != nil {
		return err
	}
	if syncID == "" {
		return nil
	}
	return db.Ctx(ctx).Table(table).
		Where("sync_id = ?", syncID).
		Updates(map[string]any{
			"sync_account_id": meta.SyncAccountID,
			"sync_version":    meta.SyncVersion,
			"sync_updated_at": meta.SyncUpdatedAt,
			"sync_origin":     meta.SyncOrigin,
			"sync_deleted_at": meta.SyncDeletedAt,
		}).Error
}

// isNoRows 兼容 database/sql 的 sql.ErrNoRows —— Row().Scan 不经过 GORM 的错误翻译。
func isNoRows(err error) bool {
	return err != nil && err.Error() == "sql: no rows in result set"
}
