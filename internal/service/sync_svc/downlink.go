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
	var losses []*mergeLoss
	for page := 0; page < maxPullPages; page++ {
		p, err := transport.SyncPull(ctx, cursor, pullLimit)
		if err != nil {
			return err
		}
		for i := range p.Items {
			in := inboundOf(p.Items[i])
			loss, err := s.captureMergeLoss(ctx, in)
			if err != nil {
				return err
			}
			if err := s.applyInbound(ctx, accountID, in); err != nil {
				return err
			}
			if loss != nil {
				losses = append(losses, loss)
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
	if err := s.recordMergeLosses(ctx, accountID, losses); err != nil {
		return err
	}
	if err := s.gcDeferred(ctx, accountID); err != nil {
		return err
	}
	return s.gcLostChanges(ctx, accountID)
}

// naturalKeyed 是「账号内除同步标识之外还有一个自然键」的对象类型——今天只有
// 路径记录（项目同步标识, agentred 指纹）（决策 26）。R4b 的合并落败判定要问它：
// 这个自然键上现在站着的是哪一个同步标识。
type naturalKeyed interface {
	syncIDAtNaturalKey(ctx context.Context, in *inbound) (string, error)
}

// mergeLoss 是一条**可能**因自然键合并而被删掉的路径记录（R4b）。
type mergeLoss struct {
	kind          string
	syncID        string
	projectSyncID string
	fingerprint   string
	payload       json.RawMessage
}

// captureMergeLoss 在墓碑落地**之前**留住这一行的正文与自然键。
//
// 落败那一份的主人是另一台桌面端：server 只把 MergedSyncID 回给推上来的那一端，
// 它这边收到的仅仅是一份墓碑，和一次普通的远端删除长得一模一样。要分辨只能等
// 胜者也落地——所以这里只做「留证」，判定推迟到整轮下行结束（recordMergeLosses）。
func (s *service) captureMergeLoss(ctx context.Context, in *inbound) (*mergeLoss, error) {
	if !in.Deleted {
		return nil, nil
	}
	ad, ok := s.adapters[in.Kind].(naturalKeyed)
	if !ok || ad == nil {
		return nil, nil
	}
	out, err := s.adapters[in.Kind].load(ctx, in.SyncID)
	if err != nil || out == nil {
		return nil, err
	}
	return &mergeLoss{
		kind: in.Kind, syncID: in.SyncID,
		projectSyncID: out.ProjectSyncID, fingerprint: out.AgentredFingerprint,
		payload: out.Payload,
	}, nil
}

// recordMergeLosses 落实 R4b 在**落败方**这一端的那一半：整轮下行结束后，如果那个
// 自然键上站着的已经是**另一个**同步标识的活行，本端刚被删掉的这一份就是被合并
// 掉的，按 R5 记一条「被覆盖」。
//
// 判定放在整轮之后而不是逐条：server 一次合并写两行——墓碑拿较小版本、胜者拿较大
// 版本，胜者可能落在下一页。自然键上没有别人站着就只是一次普通的远端删除，什么都
// 不记（「列表为空是常态」）。
func (s *service) recordMergeLosses(ctx context.Context, accountID int64, losses []*mergeLoss) error {
	for _, loss := range losses {
		ad, ok := s.adapters[loss.kind].(naturalKeyed)
		if !ok {
			continue
		}
		holder, err := ad.syncIDAtNaturalKey(ctx, &inbound{
			Kind: loss.kind, ProjectSyncID: loss.projectSyncID, AgentredFingerprint: loss.fingerprint,
		})
		if err != nil {
			return err
		}
		if holder == "" || holder == loss.syncID {
			continue
		}
		logger.Ctx(ctx).Info("sync_svc.recordMergeLosses: row lost the natural key merge",
			zap.String("kind", loss.kind), zap.String("syncId", loss.syncID),
			zap.String("keptSyncId", holder))
		if err := s.recordLostChange(ctx, accountID, &syncqueue_entity.LostChange{
			EntityType:          loss.kind,
			EntitySyncID:        loss.syncID,
			Reason:              syncqueue_entity.ReasonOverwritten,
			PayloadJSON:         string(loss.payload),
			ProjectSyncID:       loss.projectSyncID,
			AgentredFingerprint: loss.fingerprint,
			OccurredAt:          s.now(),
		}); err != nil {
			return err
		}
	}
	return nil
}

// gcLostChanges 「没能同步的改动」保留 30 天（R5、决策 5），到期回收。与暂缓行的
// 回收同一个窗口、同一个节奏——两者都是 30 天承诺的一半，缺哪一半那句承诺都不成立。
func (s *service) gcLostChanges(ctx context.Context, accountID int64) error {
	rows, err := syncqueue_repo.LostChange().ListByAccount(ctx, accountID)
	if err != nil {
		return err
	}
	cutoff := s.now() - TombstoneWindow.Milliseconds()
	for _, row := range rows {
		if row.Createtime > cutoff {
			continue
		}
		if err := syncqueue_repo.LostChange().Delete(ctx, row.ID); err != nil {
			return err
		}
		logger.Ctx(ctx).Info("sync_svc.gcLostChanges: lost change expired",
			zap.String("kind", row.EntityType), zap.String("syncId", row.EntitySyncID),
			zap.String("reason", row.Reason))
	}
	return nil
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
		lost := &syncqueue_entity.LostChange{
			EntityType:   row.EntityType,
			EntitySyncID: row.EntitySyncID,
			Reason:       syncqueue_entity.ReasonDiscarded,
			OccurredAt:   s.now(),
		}
		// 入站队列存的是整个 inbound 信封；列表要展示与恢复的是**正文**，恢复走的
		// 又是 adapter.apply——把信封喂给它只会解出一个空对象。这里拆一次信封，
		// 顺带留住不在正文里的那两项自然键（R5a）。
		var env inbound
		if err := json.Unmarshal([]byte(row.PayloadJSON), &env); err == nil {
			lost.PayloadJSON = string(env.Payload)
			lost.ProjectSyncID, lost.AgentredFingerprint = env.ProjectSyncID, env.AgentredFingerprint
			lost.BaseVersion = env.Version
		} else {
			lost.PayloadJSON = row.PayloadJSON
		}
		if err := s.recordLostChange(ctx, accountID, lost); err != nil {
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
