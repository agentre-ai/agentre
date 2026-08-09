package sync_svc

import (
	"context"
	"errors"
	"strconv"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/syncmeta_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/syncqueue_entity"
	"github.com/agentre-ai/agentre/internal/pkg/syncwire"
	"github.com/agentre-ai/agentre/internal/repository/syncqueue_repo"
	"github.com/agentre-ai/agentre/internal/repository/syncstate_repo"
)

// pending 是折叠后的一条待上行改动：同一行在队列里的多次改动只推最后一次，
// 被它盖掉的队列行一起出队（R7：不丢失、不重复应用）。
type pending struct {
	kind        string
	syncID      string
	op          string
	baseVersion int64
	queueIDs    []int64
}

// flush 把出站队列推上去（R3 的上行、R7 的补齐）。
//
// 队列按入队顺序上行，一个批次一次请求——server 按顺序逐条处理，父行先落地、
// 子行后落地的次序因此在对端也成立。
func (s *service) flush(ctx context.Context, accountID, deviceID int64) error {
	rows, err := syncqueue_repo.OutboundQueue().ListByAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	items := collapseQueue(rows)

	for start := 0; start < len(items); start += pushBatch {
		end := start + pushBatch
		if end > len(items) {
			end = len(items)
		}
		if err := s.pushBatch(ctx, accountID, deviceID, items[start:end], true); err != nil {
			return err
		}
	}
	return nil
}

// collapseQueue 按 (对象类型, 同步标识) 折叠队列：保留最后一次操作，按第一次出现的
// 次序排列——先建的行仍然先上行，它的引用目标因此在对端先落地。
func collapseQueue(rows []*syncqueue_entity.OutboundQueueItem) []*pending {
	index := make(map[string]*pending, len(rows))
	order := make([]*pending, 0, len(rows))
	for _, row := range rows {
		key := row.EntityType + ":" + row.EntitySyncID
		p := index[key]
		if p == nil {
			p = &pending{
				kind: row.EntityType, syncID: row.EntitySyncID,
				op: row.Op, baseVersion: row.BaseVersion,
			}
			index[key] = p
			order = append(order, p)
		}
		p.op = row.Op
		p.queueIDs = append(p.queueIDs, row.ID)
	}
	return order
}

// pushBatch 推一批并落实应答。allowResync 为 true 时，遇上「超窗口」先做一次全量
// 重同步再重试（R6a）；重试那一次不再允许递归重同步。
func (s *service) pushBatch(
	ctx context.Context, accountID, deviceID int64, batch []*pending, allowResync bool,
) error {
	items := make([]syncwire.PushItem, 0, len(batch))
	kept := make([]*pending, 0, len(batch))
	var doneQueueIDs []int64

	for _, p := range batch {
		item, ok, err := s.buildPushItem(ctx, p)
		if err != nil {
			return err
		}
		if !ok {
			// 本机已经没有这一行、或它翻不成一份能过机的载荷：出队，不上行。
			doneQueueIDs = append(doneQueueIDs, p.queueIDs...)
			continue
		}
		items = append(items, *item)
		kept = append(kept, p)
	}
	if err := s.dropQueueRows(ctx, doneQueueIDs); err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	results, err := s.getTransport().SyncPush(ctx, items)
	if err != nil {
		if allowResync && errors.Is(err, syncwire.ErrResyncRequired) {
			logger.Ctx(ctx).Info("sync_svc.pushBatch: resync required, pulling full snapshot")
			if rerr := s.resync(ctx, accountID); rerr != nil {
				return rerr
			}
			survivors, serr := s.reevaluateAfterResync(ctx, accountID, kept)
			if serr != nil {
				return serr
			}
			if len(survivors) == 0 {
				return nil
			}
			return s.pushBatch(ctx, accountID, deviceID, survivors, false)
		}
		return err
	}

	byKey := make(map[string]syncwire.PushResult, len(results))
	for _, r := range results {
		byKey[r.Kind+":"+r.SyncID] = r
	}
	for i, p := range kept {
		res, ok := byKey[p.kind+":"+p.syncID]
		if !ok {
			continue
		}
		if err := s.applyPushResult(ctx, accountID, deviceID, p, items[i], res); err != nil {
			return err
		}
		if err := s.dropQueueRows(ctx, p.queueIDs); err != nil {
			return err
		}
	}
	return nil
}

// buildPushItem 把一条待上行改动翻成上行项。第二个返回值为 false 表示这一条不该发。
func (s *service) buildPushItem(ctx context.Context, p *pending) (*syncwire.PushItem, bool, error) {
	ad := s.adapters[p.kind]
	if ad == nil {
		return nil, false, nil
	}
	if p.op == OpDelete {
		// 墓碑不带正文：本地行可能已经软删，读不回来也不需要。
		return &syncwire.PushItem{
			Kind: p.kind, SyncID: p.syncID, BaseVersion: p.baseVersion,
			UpdatedAt: s.now(), Deleted: true,
		}, true, nil
	}
	out, err := ad.load(ctx, p.syncID)
	if err != nil {
		return nil, false, err
	}
	if out == nil {
		return nil, false, nil
	}
	// 上行前的守卫：载荷里绝不出现本地自增 ID 或 provider 正文（R2、决策 6）。
	if err := syncwire.GuardPayload(out.Payload); err != nil {
		logger.Ctx(ctx).Error("sync_svc.buildPushItem: payload rejected by guard",
			zap.String("kind", p.kind), zap.String("syncId", p.syncID), zap.Error(err))
		return nil, false, nil
	}
	return &syncwire.PushItem{
		Kind:                p.kind,
		SyncID:              p.syncID,
		BaseVersion:         out.BaseVersion,
		UpdatedAt:           out.UpdatedAt,
		AgentredFingerprint: out.AgentredFingerprint,
		ProjectSyncID:       out.ProjectSyncID,
		Payload:             out.Payload,
	}, true, nil
}

// applyPushResult 落实一条应答。
//
//   - accepted：把 server 分配的版本写回本行，它就是下一次上行的基版本（R4a）。
//   - conflict：本次上行照常生效（R4 后到者胜），但被覆盖的那一版按 R5 落一条
//     「被覆盖」记录。
//   - rejected / deleted：对象在 server 上已是墓碑，删除不被复活（R6）——本机跟着
//     落墓碑，内容留进 R5 的列表，界面据此给「按这份内容新建」的出路（R5a）。
//   - 路径记录被自然键合并掉时（R4b），落败的那一份同样进列表。
func (s *service) applyPushResult(
	ctx context.Context, accountID, deviceID int64,
	p *pending, item syncwire.PushItem, res syncwire.PushResult,
) error {
	now := s.now()
	origin := strconv.FormatInt(deviceID, 10)

	if res.Status == syncwire.PushStatusRejected {
		if res.Reason == syncwire.PushRejectReasonDeleted {
			return s.acceptRemoteTombstone(ctx, accountID, p, item, res, now)
		}
		logger.Ctx(ctx).Warn("sync_svc.applyPushResult: item rejected",
			zap.String("kind", p.kind), zap.String("syncId", p.syncID),
			zap.String("reason", res.Reason))
		return nil
	}

	meta := syncmeta_entity.SyncMeta{
		SyncID:        p.syncID,
		SyncAccountID: accountID,
		SyncVersion:   res.Version,
		SyncUpdatedAt: item.UpdatedAt,
		SyncOrigin:    origin,
	}
	if p.op == OpDelete {
		meta.SyncDeletedAt = now
	}
	if err := syncstate_repo.SyncState().SaveMeta(ctx, p.kind, p.syncID, meta); err != nil {
		return err
	}

	if res.Status == syncwire.PushStatusConflict {
		if err := s.recordLostChange(ctx, accountID, &syncqueue_entity.LostChange{
			EntityType:   p.kind,
			EntitySyncID: p.syncID,
			BaseVersion:  res.OverwrittenVersion,
			Reason:       syncqueue_entity.ReasonOverwritten,
			PayloadJSON:  string(item.Payload),
			OriginDevice: strconv.FormatInt(res.OverwrittenDeviceID, 10),
			OccurredAt:   now,
		}); err != nil {
			return err
		}
	}
	// R4b：本次上行的这一份在自然键上落败，已经被 server 落成墓碑。
	if res.MergedSyncID == p.syncID {
		if err := s.acceptRemoteTombstone(ctx, accountID, p, item, res, now); err != nil {
			return err
		}
	}
	return nil
}

// acceptRemoteTombstone server 上这一行已是墓碑：本机跟着落墓碑（删除不复活，R6），
// 本次没能生效的内容进 R5 的列表。
func (s *service) acceptRemoteTombstone(
	ctx context.Context, accountID int64,
	p *pending, item syncwire.PushItem, res syncwire.PushResult, now int64,
) error {
	if ad := s.adapters[p.kind]; ad != nil {
		if err := ad.remove(ctx, &inbound{Kind: p.kind, SyncID: p.syncID}); err != nil {
			return err
		}
	}
	if err := syncstate_repo.SyncState().SaveMeta(ctx, p.kind, p.syncID, syncmeta_entity.SyncMeta{
		SyncID:        p.syncID,
		SyncAccountID: accountID,
		SyncVersion:   res.Version,
		SyncUpdatedAt: item.UpdatedAt,
		SyncDeletedAt: now,
	}); err != nil {
		return err
	}
	return s.recordLostChange(ctx, accountID, &syncqueue_entity.LostChange{
		EntityType:   p.kind,
		EntitySyncID: p.syncID,
		BaseVersion:  res.Version,
		Reason:       syncqueue_entity.ReasonRejected,
		PayloadJSON:  string(item.Payload),
		OccurredAt:   now,
	})
}

// reevaluateAfterResync 落实 R6a：重同步之后，队列里的每一条按基版本判去留。
//
//   - 基版本为空 → 照常上行（本端离线期间新建的行，不是复活）。
//   - 基版本非空且与快照里该行的当前版本不符 → 拦下，以「超时未上传」进 R5 列表。
//   - 基版本非空但该同步标识在快照里已不存在（墓碑都回收了）→ 同上。这一条正是
//     复活风险本身。
func (s *service) reevaluateAfterResync(
	ctx context.Context, accountID int64, batch []*pending,
) ([]*pending, error) {
	survivors := make([]*pending, 0, len(batch))
	for _, p := range batch {
		if p.baseVersion == 0 {
			survivors = append(survivors, p)
			continue
		}
		version, _, found, err := syncstate_repo.SyncState().FindVersion(ctx, p.kind, p.syncID)
		if err != nil {
			return nil, err
		}
		if found && version == p.baseVersion {
			survivors = append(survivors, p)
			continue
		}
		payload := ""
		if item, ok, berr := s.buildPushItem(ctx, p); berr == nil && ok {
			payload = string(item.Payload)
		}
		if err := s.recordLostChange(ctx, accountID, &syncqueue_entity.LostChange{
			EntityType:   p.kind,
			EntitySyncID: p.syncID,
			BaseVersion:  p.baseVersion,
			Reason:       syncqueue_entity.ReasonRejected,
			PayloadJSON:  payload,
			OccurredAt:   s.now(),
		}); err != nil {
			return nil, err
		}
		if err := s.dropQueueRows(ctx, p.queueIDs); err != nil {
			return nil, err
		}
	}
	return survivors, nil
}

// resync 拉一份全量快照并以之为准（R6a）。
func (s *service) resync(ctx context.Context, accountID int64) error {
	return s.pullFrom(ctx, accountID, 0)
}

func (s *service) dropQueueRows(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		if err := syncqueue_repo.OutboundQueue().Delete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *service) recordLostChange(ctx context.Context, accountID int64, row *syncqueue_entity.LostChange) error {
	row.SyncAccountID = accountID
	if row.Createtime == 0 {
		row.Createtime = s.now()
	}
	if row.OccurredAt == 0 {
		row.OccurredAt = row.Createtime
	}
	return syncqueue_repo.LostChange().Create(ctx, row)
}
