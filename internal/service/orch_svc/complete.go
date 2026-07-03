package orch_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// watchCompletion 监听已订阅好的子会话轮次 channel；轮结束且会话真实空闲 → 标 done +
// 回报派发者续轮。ch/cancel 必须由调用方在 Send 之前订阅好后传入(ObserveTurn 契约:订阅
// 须早于 turn 启动,否则快 turn 的终态回执会丢、range 永久阻塞 → 任务永不 done、父永不
// 唤醒、调度槽泄漏、Run 卡死)。watcher 是被派发任务 report+settle 的唯一负责者。
func (s *orchSvc) watchCompletion(ctx context.Context, task *orch_entity.Task, ch <-chan TurnDone, cancel func()) {
	defer cancel()
	defer s.onTaskSettled(task.RunID) // 释放调度槽:无论 idle / error / channel 提前关闭,均恰好执行一次。
	for td := range ch {
		if td.SessionID != task.SessionID {
			continue
		}
		status, err := s.chat.AgentStatus(ctx, task.SessionID)
		if err != nil {
			logger.Ctx(ctx).Error("orch.watchCompletion: 取会话状态失败",
				zap.Int64("session", task.SessionID), zap.Error(err))
			continue
		}
		switch status {
		case "idle":
			// 完整正文始终落 Result(供 read);Summary 由显式 finish 写,决定内联 vs ping。
			result, _ := s.chat.FinalAssistantText(ctx, task.SessionID)
			task.Status = orch_entity.TaskDone
			task.Result = result
			if fresh, _ := s.tasks.Find(ctx, task.ID); fresh != nil && fresh.Summary != "" {
				task.Summary = fresh.Summary
			}
			if err := s.tasks.Update(ctx, task); err != nil {
				logger.Ctx(ctx).Error("orch.watchCompletion: 写子任务终态失败(可被对账纠正)", zap.Int64("task", task.ID), zap.String("status", task.Status), zap.Error(err))
			}
			s.emitRunUpdated(ctx, task.RunID)
			s.reportToParent(ctx, task.ParentTaskID, task)
			return
		case "error":
			s.markTaskError(ctx, task, "运行时崩溃")
			s.emitRunUpdated(ctx, task.RunID)
			return
		default:
			// running/waiting:还有未决事项(子任务/ask/审批),继续等下一轮。
			continue
		}
	}
}

// reportToParent 子任务完成回报:有显式小结 → 内联 task_report;否则 → task_done 轻量通知。
func (s *orchSvc) reportToParent(ctx context.Context, parentTaskID int64, child *orch_entity.Task) {
	var msg string
	if child.Summary != "" {
		msg = taskReportMsg(child.ID, child.AgentID, child.CallSeq, child.Summary, true)
	} else {
		msg = taskDoneMsg(child.ID, child.AgentID, child.CallSeq, firstLine(child.Result, 120))
	}
	s.injectToParent(ctx, parentTaskID, msg)
}

// injectToParent 把一条消息注入父会话续轮,并在无未决子任务时把父翻回 running。
func (s *orchSvc) injectToParent(ctx context.Context, parentTaskID int64, msg string) {
	if parentTaskID == 0 {
		return // 根任务无父:根的收口只认 Leader 显式 finish
	}
	parent, err := s.tasks.Find(ctx, parentTaskID)
	if err != nil || parent == nil {
		return
	}
	if s.allChildrenSettled(ctx, parent) && parent.Status == orch_entity.TaskAwaitingChildren {
		parent.Status = orch_entity.TaskRunning
		if err := s.tasks.Update(ctx, parent); err != nil {
			logger.Ctx(ctx).Error("orch.injectToParent: 父任务翻回 running 失败(可被对账纠正)", zap.Int64("task", parent.ID), zap.Error(err))
		}
		s.emitRunUpdated(ctx, parent.RunID)
	}
	if err := s.chat.SendAndForget(ctx, parent.SessionID, msg); err != nil {
		logger.Ctx(ctx).Error("orch.injectToParent: 续轮注入失败", zap.Int64("parent", parent.SessionID), zap.Error(err))
	}
}

// markTaskError 技术崩溃:标 error,把崩溃当 task_error 轻量通知上抛父会话(与 done 同一续轮路)。
func (s *orchSvc) markTaskError(ctx context.Context, task *orch_entity.Task, reason string) {
	task.Status = orch_entity.TaskError
	task.Result = reason
	if err := s.tasks.Update(ctx, task); err != nil {
		logger.Ctx(ctx).Error("orch.markTaskError: 写子任务 error 态失败(可被对账纠正)", zap.Int64("task", task.ID), zap.String("status", task.Status), zap.Error(err))
	}
	s.injectToParent(ctx, task.ParentTaskID, taskErrorMsg(task.ID, task.AgentID, reason))
}

// allChildrenSettled 该任务的全部 dispatch 子任务是否都到终态。
func (s *orchSvc) allChildrenSettled(ctx context.Context, parent *orch_entity.Task) bool {
	rows, err := s.tasks.ListByRun(ctx, parent.RunID)
	if err != nil {
		return false
	}
	for _, t := range rows {
		if t.ParentTaskID == parent.ID && t.Kind == orch_entity.TaskKindDispatch && !t.IsTerminal() {
			return false
		}
	}
	return true
}
