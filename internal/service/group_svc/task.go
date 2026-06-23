package group_svc

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
)

// lockGroupForCaller 是三个任务 handler 共用的 prologue:校验 caller 活跃 → 取 per-group
// 锁(与 IngestAgentMessage 共用,task_no 与 seq 同纪律,不重号,spec §5)→ 取群。
// 成功时返回 unlock 由调用方 defer;失败路径内部已解锁。
func (s *groupSvc) lockGroupForCaller(ctx context.Context, callerMemberID int64) (*group_entity.GroupMember, *group_entity.Group, func(), error) {
	caller, err := group_repo.Member().Find(ctx, callerMemberID)
	if err != nil || caller == nil || !caller.IsActive() {
		return nil, nil, nil, i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	mu := s.ingestMu(caller.GroupID)
	mu.Lock()
	g, err := group_repo.Group().Find(ctx, caller.GroupID)
	if err != nil || g == nil {
		mu.Unlock()
		return nil, nil, nil, i18n.NewError(ctx, code.GroupNotFound)
	}
	return caller, g, mu.Unlock, nil
}

// HandleTaskCreate 是 group_task_create MCP tool 的服务端入口:建卡即派活。
// 落卡 + 落一条 task_event=created 的群消息投递给执行人(复用消息管线触发其轮次)。
func (s *groupSvc) HandleTaskCreate(ctx context.Context, callerMemberID int64, assigneeName, title, brief string, parentTaskNo int) (*group_entity.GroupTask, error) {
	caller, g, unlock, err := s.lockGroupForCaller(ctx, callerMemberID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	assignee := s.memberByName(ctx, members, assigneeName)
	if assignee == nil {
		return nil, i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	if assignee.ID == caller.ID {
		// 防自循环:与 ingest 丢弃自我 mention 同一纪律;给自己派活会让 created 消息再 kick 自己。
		// 专用错误码:文案经 err.Error() 透传回 MCP 调用方,模型能读懂并改为直接执行。
		return nil, i18n.NewError(ctx, code.GroupTaskSelfAssign)
	}
	no, err := group_repo.Task().NextTaskNo(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	if parentTaskNo < 0 {
		parentTaskNo = 0
	}
	t := &group_entity.GroupTask{
		GroupID: g.ID, TaskNo: no,
		Title: strings.TrimSpace(title), Brief: strings.TrimSpace(brief),
		CreatorMemberID: caller.ID, AssigneeMemberID: assignee.ID,
		Status: group_entity.TaskStatusOpen, ParentTaskNo: parentTaskNo,
		Createtime: s.now(), Updatetime: s.now(),
	}
	if err := t.Check(ctx); err != nil {
		return nil, err
	}
	if err := group_repo.Task().Create(ctx, t); err != nil {
		return nil, err
	}
	content := fmt.Sprintf("任务 #%d：%s\n%s", no, t.Title, t.Brief)
	s.bumpRound(ctx, g)
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindAgent, caller.ID, content, []int64{assignee.ID}, false, 0, t.ID, group_entity.TaskEventCreated); err != nil {
		logger.Ctx(ctx).Warn("group_svc.HandleTaskCreate: persist failed", zap.Error(err))
	}
	s.emitTaskUpdated(ctx, t)
	s.enqueueDeliveries(g.ID, []int64{assignee.ID}, content, s.memberDisplayName(ctx, caller), caller.ID)
	s.kick(ctx, g.ID)
	logger.Ctx(ctx).Info("group_svc.HandleTaskCreate: created",
		zap.Int64("groupId", g.ID), zap.Int("taskNo", no), zap.Int64("assignee", assignee.ID))
	return t, nil
}

// HandleTaskComplete 是 group_task_complete MCP tool 的服务端入口:仅执行人可交付,
// result 必填(软验收门)。completed 消息投回建卡人;建卡人已离群或自交付(creator==caller,
// 防自循环)时走 applyFallback 既有回退链(来源/最近发言者/用户)。
func (s *groupSvc) HandleTaskComplete(ctx context.Context, callerMemberID int64, taskNo int, result string) (*group_entity.GroupTask, error) {
	result = strings.TrimSpace(result)
	if result == "" {
		return nil, i18n.NewError(ctx, code.GroupTaskResultRequired)
	}
	caller, g, unlock, err := s.lockGroupForCaller(ctx, callerMemberID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	t, err := group_repo.Task().FindByGroupAndNo(ctx, g.ID, taskNo)
	if err != nil || t == nil {
		return nil, i18n.NewError(ctx, code.GroupTaskNotFound)
	}
	if !t.IsOpen() {
		return nil, i18n.NewError(ctx, code.GroupTaskClosed)
	}
	if !t.CanComplete(caller.ID) {
		return nil, i18n.NewError(ctx, code.GroupTaskForbidden)
	}
	t.Status = group_entity.TaskStatusDone
	t.Result = result
	t.Updatetime = s.now()
	if err := group_repo.Task().Update(ctx, t); err != nil {
		return nil, err
	}
	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	// 默认投回建卡人;建卡人已离群或就是 caller 自己(投自己会再 kick 自己起轮,
	// 与 HandleTaskCreate 拒自派活同纪律) → 复用 applyFallback 既有回退链。
	var recipients []int64
	toUser := false
	// creator.ID != caller.ID 当前 MCP 路径不可达(建卡拒自派活 + 仅执行人可交付 ⇒
	// creator==caller 蕴含自派活):防御性守卫,等 reassign / 其它建卡路径出现才会武装。
	if creator := activeMemberByID(members, t.CreatorMemberID); creator != nil && creator.ID != caller.ID {
		recipients = []int64{creator.ID}
	} else {
		recipients, toUser = s.applyFallback(ctx, g, caller, members, nil, false)
	}
	content := fmt.Sprintf("任务 #%d 已完成\n%s", t.TaskNo, result)
	s.bumpRound(ctx, g)
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindAgent, caller.ID, content, recipients, toUser, 0, t.ID, group_entity.TaskEventCompleted); err != nil {
		logger.Ctx(ctx).Warn("group_svc.HandleTaskComplete: persist failed", zap.Error(err))
	}
	s.emitTaskUpdated(ctx, t)
	s.enqueueDeliveries(g.ID, recipients, content, s.memberDisplayName(ctx, caller), caller.ID)
	s.kick(ctx, g.ID)
	logger.Ctx(ctx).Info("group_svc.HandleTaskComplete: done",
		zap.Int64("groupId", g.ID), zap.Int("taskNo", taskNo),
		zap.Int64s("recipients", recipients), zap.Bool("toUser", toUser))
	return t, nil
}

// HandleTaskCancel 是 group_task_cancel MCP tool 的服务端入口:仅建卡人或主持人。
func (s *groupSvc) HandleTaskCancel(ctx context.Context, callerMemberID int64, taskNo int, reason string) (*group_entity.GroupTask, error) {
	caller, g, unlock, err := s.lockGroupForCaller(ctx, callerMemberID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	t, err := group_repo.Task().FindByGroupAndNo(ctx, g.ID, taskNo)
	if err != nil || t == nil {
		return nil, i18n.NewError(ctx, code.GroupTaskNotFound)
	}
	if !t.IsOpen() {
		return nil, i18n.NewError(ctx, code.GroupTaskClosed)
	}
	if !t.CanCancel(caller.ID, caller.IsHost()) {
		return nil, i18n.NewError(ctx, code.GroupTaskForbidden)
	}
	t.Status = group_entity.TaskStatusCanceled
	t.Updatetime = s.now()
	if err := group_repo.Task().Update(ctx, t); err != nil {
		return nil, err
	}
	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	// 通知执行人与建卡人(去掉调用者自己、去重、过滤已离群)。
	notify := []int64{}
	for _, id := range []int64{t.AssigneeMemberID, t.CreatorMemberID} {
		if id == caller.ID {
			continue
		}
		// slices.Contains 去重当前不可达(建卡拒自派活 ⇒ creator≠assignee):防御性守卫,
		// 等 reassign / 其它建卡路径出现才会武装(与 HandleTaskComplete 的同类守卫同理)。
		if m := activeMemberByID(members, id); m != nil && !slices.Contains(notify, id) {
			notify = append(notify, id)
		}
	}
	content := fmt.Sprintf("任务 #%d 已取消", t.TaskNo)
	if r := strings.TrimSpace(reason); r != "" {
		content += "：" + r
	}
	s.bumpRound(ctx, g)
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindAgent, caller.ID, content, notify, false, 0, t.ID, group_entity.TaskEventCanceled); err != nil {
		logger.Ctx(ctx).Warn("group_svc.HandleTaskCancel: persist failed", zap.Error(err))
	}
	s.emitTaskUpdated(ctx, t)
	s.enqueueDeliveries(g.ID, notify, content, s.memberDisplayName(ctx, caller), caller.ID)
	s.kick(ctx, g.ID)
	logger.Ctx(ctx).Info("group_svc.HandleTaskCancel: canceled",
		zap.Int64("groupId", g.ID), zap.Int("taskNo", taskNo), zap.Int64s("notify", notify))
	return t, nil
}

// cancelTasksOfLeftMember 成员离群级联:其名下 open 任务全部取消 + 落 system 消息(spec §5)。
// 在 ingestMu 内调用(persistMessage 需要 seq 纪律)。
func (s *groupSvc) cancelTasksOfLeftMember(ctx context.Context, groupID, memberID int64) {
	g, err := group_repo.Group().Find(ctx, groupID)
	if err != nil || g == nil {
		logger.Ctx(ctx).Warn("group_svc.cancelTasksOfLeftMember: find group failed, open tasks left orphaned",
			zap.Int64("groupId", groupID), zap.Int64("memberId", memberID), zap.Error(err))
		return
	}
	tasks, err := group_repo.Task().ListByGroup(ctx, groupID)
	if err != nil {
		logger.Ctx(ctx).Warn("group_svc.cancelTasksOfLeftMember: list tasks failed, open tasks left orphaned",
			zap.Int64("groupId", groupID), zap.Int64("memberId", memberID), zap.Error(err))
		return
	}
	for _, t := range tasks {
		if !t.IsOpen() || t.AssigneeMemberID != memberID {
			continue
		}
		t.Status = group_entity.TaskStatusCanceled
		t.Updatetime = s.now()
		if err := group_repo.Task().Update(ctx, t); err != nil {
			logger.Ctx(ctx).Warn("group_svc.cancelTasksOfLeftMember: update failed",
				zap.Int64("taskId", t.ID), zap.Error(err))
			continue
		}
		content := fmt.Sprintf("任务 #%d 已取消(执行成员离群)", t.TaskNo)
		if _, err := s.persistMessage(ctx, g, group_entity.SenderKindSystem, 0, content, nil, false, 0, t.ID, group_entity.TaskEventCanceled); err != nil {
			logger.Ctx(ctx).Warn("group_svc.cancelTasksOfLeftMember: persist failed", zap.Error(err))
		}
		s.emitTaskUpdated(ctx, t)
	}
}

// activeMemberByID 在成员列表里找 active 成员(找不到/已离群返回 nil)。
func activeMemberByID(members []*group_entity.GroupMember, id int64) *group_entity.GroupMember {
	for _, m := range members {
		if m.ID == id && m.IsActive() {
			return m
		}
	}
	return nil
}

// memberByName 在群成员里按显示名找 active 成员(找不到返回 nil)。
func (s *groupSvc) memberByName(ctx context.Context, members []*group_entity.GroupMember, name string) *group_entity.GroupMember {
	for _, m := range members {
		if m.IsActive() && s.memberDisplayName(ctx, m) == name {
			return m
		}
	}
	return nil
}

// bumpRound 任务事件与成员发言同样计轮(spec §5)。
func (s *groupSvc) bumpRound(ctx context.Context, g *group_entity.Group) {
	g.RoundCount++
	if err := group_repo.Group().Update(ctx, g); err != nil {
		logger.Ctx(ctx).Warn("group_svc.bumpRound: update failed", zap.Int64("groupId", g.ID), zap.Error(err))
	}
}

// emitTaskUpdated 推任务状态变化(前端任务 tab + 历史卡片状态回写,spec §8)。
func (s *groupSvc) emitTaskUpdated(ctx context.Context, t *group_entity.GroupTask) {
	s.emitter.Emit(ctx, groupEventName(t.GroupID), map[string]any{"kind": "task_updated", "task": toGroupTaskEvent(t)})
}
