package group_svc

import (
	"context"
	"sync"

	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"agentre/internal/model/entity/group_entity"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/group_repo"
)

// ingestMu 返回某 group 的 per-group 锁(sync.Map 懒建), 串行化 IngestAgentMessage 的 seq/round_count 临界区。
func (s *groupSvc) ingestMu(groupID int64) *sync.Mutex {
	v, _ := s.ingestLocks.LoadOrStore(groupID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// IngestAgentMessage 是 group_send MCP tool 的服务端入口(MCP handler 调用, 成员 turn 进行中可多次)。
// memberID=发送成员; body=正文; mentions=收件成员显示名(+ "用户"/"你")。
func (s *groupSvc) IngestAgentMessage(ctx context.Context, memberID int64, body string, mentions []string) error {
	sender, err := group_repo.Member().Find(ctx, memberID)
	if err != nil || sender == nil {
		return i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	// per-group 串行化「解析→分配 seq→落库→入队」(防 NextSeq 重号 / round_count 丢更新)。
	mu := s.ingestMu(sender.GroupID)
	mu.Lock()
	defer mu.Unlock()

	g, err := group_repo.Group().Find(ctx, sender.GroupID)
	if err != nil || g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return err
	}
	recipientIDs, toUser := s.resolveMentionNames(ctx, g, members, sender, mentions)
	recipientIDs, toUser = s.applyFallback(ctx, g, sender, recipientIDs, toUser)

	g.RoundCount++
	if err := group_repo.Group().Update(ctx, g); err != nil {
		logger.Ctx(ctx).Warn("group_svc.IngestAgentMessage: round_count update failed", zap.Int64("groupId", g.ID), zap.Error(err))
	}
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindAgent, sender.ID, body, recipientIDs, toUser, 0); err != nil {
		logger.Ctx(ctx).Warn("group_svc.IngestAgentMessage: persist failed", zap.Error(err))
	}
	s.enqueueDeliveries(g.ID, recipientIDs, body, s.names(ctx, sender.AgentID))
	s.kick(ctx, g.ID)
	return nil
}

// resolveMentionNames 把成员显示名解析成 member id(+ 是否 @用户)。剔除自我 mention(防自循环)。
// 未进群的名字不再自动招募(已退役 @mention 招募);协调者改用 group_invite 工具,仅记日志。
func (s *groupSvc) resolveMentionNames(ctx context.Context, g *group_entity.Group, members []*group_entity.GroupMember, sender *group_entity.GroupMember, names []string) ([]int64, bool) {
	byName := map[string]int64{}
	for _, m := range members {
		if n := s.names(ctx, m.AgentID); n != "" {
			byName[n] = m.ID
		}
	}
	toUser := false
	ids := []int64{}
	for _, name := range names {
		switch {
		case name == "用户" || name == "你":
			toUser = true
		case byName[name] != 0 && byName[name] != sender.ID:
			ids = append(ids, byName[name])
		case byName[name] == sender.ID:
			// 自己 mention 自己 → 忽略
		default:
			// 未进群的名字不再自动招募;协调者改用 group_invite 工具。仅记日志。
			logger.Ctx(ctx).Info("group_svc.resolveMentionNames: unresolved mention (use group_invite)",
				zap.String("name", name), zap.Int64("groupId", g.ID))
		}
	}
	return ids, toUser
}

// applyFallback: 无任何 agent 收件人也不 @用户 → 回上一个发送者; 仍没有 → 回用户(quiesce)。
func (s *groupSvc) applyFallback(ctx context.Context, g *group_entity.Group, sender *group_entity.GroupMember, ids []int64, toUser bool) ([]int64, bool) {
	if len(ids) > 0 || toUser {
		return ids, toUser
	}
	if prev := s.lastSenderMemberID(ctx, g.ID, sender.ID); prev > 0 {
		return []int64{prev}, false
	}
	return ids, true
}

// lastSenderMemberID 取最近一条非自己发的 group_message 的 sender_member_id(反向扫)。0=没有。
func (s *groupSvc) lastSenderMemberID(ctx context.Context, groupID, excludeMemberID int64) int64 {
	msgs, err := group_repo.Message().ListByGroup(ctx, groupID)
	if err != nil {
		return 0
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if mid := msgs[i].SenderMemberID; mid > 0 && mid != excludeMemberID {
			return mid
		}
	}
	return 0
}
