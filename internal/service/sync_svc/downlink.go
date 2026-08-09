package sync_svc

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/syncmeta_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/syncqueue_entity"
	"github.com/agentre-ai/agentre/internal/repository/syncqueue_repo"
	"github.com/agentre-ai/agentre/internal/repository/syncstate_repo"
)

// pull 按本端游标增量下行（R3：30 秒一轮）。
func (s *service) pull(ctx context.Context, accountID int64) error {
	st, err := s.loadCursor(ctx, accountID)
	if err != nil {
		return err
	}
	return s.pullFrom(ctx, accountID, st.Cursor)
}

// pullFrom 从给定游标开始翻页下行；cursor = 0 就是一份全量快照（R6a 的重同步）。
//
// 每一页落地完就推进游标：中途失败下次从这里继续，已经落地的行不会再来一遍
// （即便来了也被版本守卫挡掉）。
func (s *service) pullFrom(ctx context.Context, accountID, cursor int64) error {
	transport := s.getTransport()
	if transport == nil {
		return nil
	}
	for page := 0; page < maxPullPages; page++ {
		p, err := transport.SyncPull(ctx, cursor, pullLimit)
		if err != nil {
			return err
		}
		for i := range p.Items {
			in := inboundOf(p.Items[i])
			if err := s.applyInbound(ctx, accountID, in); err != nil {
				return err
			}
		}
		cursor = p.NextCursor
		if err := s.saveCursor(ctx, cursorState{
			AccountID: accountID, Cursor: cursor, LastSuccessAt: s.now(),
		}); err != nil {
			return err
		}
		if !p.HasMore {
			break
		}
	}
	if err := s.replayDeferred(ctx, accountID); err != nil {
		return err
	}
	return s.gcDeferred(ctx, accountID)
}

// applyInbound 落地一条下行项。
//
// 两道闸：**版本守卫**（本机已有同版本或更新的版本就不再落——重复投递只应用一次，
// 任意到达顺序下结果相同，R4/R7）与**引用守卫**（引用目标还没到就暂缓落地，绝不写
// 悬空引用，R2a）。
func (s *service) applyInbound(ctx context.Context, accountID int64, in *inbound) error {
	ad := s.adapters[in.Kind]
	if ad == nil {
		return nil
	}
	version, _, found, err := syncstate_repo.SyncState().FindVersion(ctx, in.Kind, in.SyncID)
	if err != nil {
		return err
	}
	if found && version >= in.Version {
		return nil
	}
	if in.Deleted && !found {
		// 本机从来没有这一行：墓碑没有可删的东西，也不必为它等引用目标到达
		// ——把删除挂进暂缓队列只会白等 30 天（R2a/R6）。
		return nil
	}

	resolved, missing, err := resolveRefs(ctx, ad.refs(in))
	if errors.Is(err, errRefMissing) {
		return s.defer_(ctx, accountID, in, missing.key())
	}
	if err != nil {
		return err
	}

	if in.Deleted {
		err = ad.remove(ctx, in)
	} else {
		err = ad.apply(ctx, in, resolved)
	}
	if errors.Is(err, errRefMissing) {
		return s.defer_(ctx, accountID, in, "")
	}
	if err != nil {
		return err
	}

	return syncstate_repo.SyncState().SaveMeta(ctx, in.Kind, in.SyncID, syncmeta_entity.SyncMeta{
		SyncID:        in.SyncID,
		SyncAccountID: accountID,
		SyncVersion:   in.Version,
		SyncUpdatedAt: in.UpdatedAt,
		SyncOrigin:    strconv.FormatInt(in.SourceDeviceID, 10),
		SyncDeletedAt: deletedAtOf(in, s.now()),
	})
}

func deletedAtOf(in *inbound, now int64) int64 {
	if in.Deleted {
		return now
	}
	return 0
}

// defer_ 把一条暂缓落地的行存进入站队列（R2a）：保留 30 天，等引用目标到达后完成。
// 同一个同步标识只留最新的一份。
func (s *service) defer_(ctx context.Context, accountID int64, in *inbound, missing string) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	rows, err := syncqueue_repo.InboundQueue().ListByAccount(ctx, accountID)
	if err != nil {
		return err
	}
	receivedAt := s.now()
	for _, row := range rows {
		if row.EntityType == in.Kind && row.EntitySyncID == in.SyncID {
			// 保留最早一次收到的时间：30 天窗口从「第一次等不到」开始算。
			if row.ReceivedAt > 0 && row.ReceivedAt < receivedAt {
				receivedAt = row.ReceivedAt
			}
			if err := syncqueue_repo.InboundQueue().Delete(ctx, row.ID); err != nil {
				return err
			}
		}
	}
	logger.Ctx(ctx).Debug("sync_svc.defer: reference has not arrived, holding row",
		zap.String("kind", in.Kind), zap.String("syncId", in.SyncID),
		zap.String("missingRef", missing))
	return syncqueue_repo.InboundQueue().Create(ctx, &syncqueue_entity.InboundQueueItem{
		SyncAccountID: accountID,
		EntityType:    in.Kind,
		EntitySyncID:  in.SyncID,
		PayloadJSON:   string(body),
		MissingSyncID: missing,
		ReceivedAt:    receivedAt,
	})
}

// replayDeferred 重试暂缓落地的行：引用目标可能刚刚随这一轮下行到达。一轮里只要
// 有一条落地成功，就再来一轮——A 依赖 B、B 依赖 C 的链条靠这个补齐。
func (s *service) replayDeferred(ctx context.Context, accountID int64) error {
	for round := 0; round < 8; round++ {
		rows, err := syncqueue_repo.InboundQueue().ListByAccount(ctx, accountID)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		progressed := false
		for _, row := range rows {
			in := &inbound{}
			if err := json.Unmarshal([]byte(row.PayloadJSON), in); err != nil {
				// 存坏了的行没有重试价值，直接丢。
				if derr := syncqueue_repo.InboundQueue().Delete(ctx, row.ID); derr != nil {
					return derr
				}
				continue
			}
			ad := s.adapters[in.Kind]
			if ad == nil {
				continue
			}
			resolved, _, rerr := resolveRefs(ctx, ad.refs(in))
			if errors.Is(rerr, errRefMissing) {
				continue
			}
			if rerr != nil {
				return rerr
			}
			var aerr error
			if in.Deleted {
				aerr = ad.remove(ctx, in)
			} else {
				aerr = ad.apply(ctx, in, resolved)
			}
			if errors.Is(aerr, errRefMissing) {
				continue
			}
			if aerr != nil {
				return aerr
			}
			if err := syncstate_repo.SyncState().SaveMeta(ctx, in.Kind, in.SyncID, syncmeta_entity.SyncMeta{
				SyncID:        in.SyncID,
				SyncAccountID: accountID,
				SyncVersion:   in.Version,
				SyncUpdatedAt: in.UpdatedAt,
				SyncOrigin:    strconv.FormatInt(in.SourceDeviceID, 10),
				SyncDeletedAt: deletedAtOf(in, s.now()),
			}); err != nil {
				return err
			}
			if err := syncqueue_repo.InboundQueue().Delete(ctx, row.ID); err != nil {
				return err
			}
			progressed = true
		}
		if !progressed {
			return nil
		}
	}
	return nil
}

// gcDeferred 超过 30 天仍然等不到引用目标的行整行丢弃，并以「引用丢失」进 R5 的
// 列表（R2a）。
func (s *service) gcDeferred(ctx context.Context, accountID int64) error {
	rows, err := syncqueue_repo.InboundQueue().ListByAccount(ctx, accountID)
	if err != nil {
		return err
	}
	cutoff := s.now() - TombstoneWindow.Milliseconds()
	for _, row := range rows {
		if row.ReceivedAt > cutoff {
			continue
		}
		if err := s.recordLostChange(ctx, accountID, &syncqueue_entity.LostChange{
			EntityType:   row.EntityType,
			EntitySyncID: row.EntitySyncID,
			Reason:       syncqueue_entity.ReasonDiscarded,
			PayloadJSON:  row.PayloadJSON,
			OccurredAt:   s.now(),
		}); err != nil {
			return err
		}
		if err := syncqueue_repo.InboundQueue().Delete(ctx, row.ID); err != nil {
			return err
		}
		logger.Ctx(ctx).Info("sync_svc.gcDeferred: deferred row expired",
			zap.String("kind", row.EntityType), zap.String("syncId", row.EntitySyncID))
	}
	return nil
}
