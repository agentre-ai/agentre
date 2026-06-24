package orch_svc

import (
	"context"
	"fmt"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// watchCompletion 订阅子会话轮次；轮结束且会话真实空闲 → 标 done + 报告回报派发者续轮。
func (s *orchSvc) watchCompletion(ctx context.Context, task *orch_entity.Task) {
	defer s.onTaskSettled(task.RunID) // 释放调度槽：无论 idle / error / channel 提前关闭，均恰好执行一次。
	ch, cancel := s.chat.ObserveTurn(task.SessionID)
	defer cancel()
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
			// 本轮结束 + 无未决 → done（完成态从真实空闲推导，杜绝卡 running）。
			text, _ := s.chat.FinalAssistantText(ctx, task.SessionID)
			task.Status = orch_entity.TaskDone
			task.Result = text
			if err := s.tasks.Update(ctx, task); err != nil {
				logger.Ctx(ctx).Error("orch.watchCompletion: 写子任务终态失败(可被对账纠正)", zap.Int64("task", task.ID), zap.String("status", task.Status), zap.Error(err))
			}
			s.reportToParent(ctx, task.ParentTaskID, task, text)
			return
		case "error":
			s.markTaskError(ctx, task, "运行时崩溃")
			return
		default:
			// running/waiting：还有未决事项（子任务/ask/审批），继续等下一轮。
			continue
		}
	}
}

// reportToParent 把子任务报告作为续轮消息注入派发者会话，并在无其它未决子任务时把父翻回 running。
func (s *orchSvc) reportToParent(ctx context.Context, parentTaskID int64, child *orch_entity.Task, report string) {
	if parentTaskID == 0 {
		return // 根任务无父：根的收口只认 Leader 显式 finish（Task 12）
	}
	parent, err := s.tasks.Find(ctx, parentTaskID)
	if err != nil || parent == nil {
		return
	}
	if s.allChildrenSettled(ctx, parent) && parent.Status == orch_entity.TaskAwaitingChildren {
		parent.Status = orch_entity.TaskRunning
		if err := s.tasks.Update(ctx, parent); err != nil {
			logger.Ctx(ctx).Error("orch.reportToParent: 父任务翻回 running 失败(可被对账纠正)", zap.Int64("task", parent.ID), zap.Error(err))
		}
	}
	msg := fmt.Sprintf("【子任务 #%d 完成 · agent#%d】\n%s", child.ID, child.AgentID, report)
	if err := s.chat.SendAndForget(ctx, parent.SessionID, msg); err != nil {
		logger.Ctx(ctx).Error("orch.reportToParent: 续轮注入失败",
			zap.Int64("parent", parent.SessionID), zap.Error(err))
	}
}

// markTaskError 技术崩溃：标 error 并把崩溃当回报上抛父会话（与 done 同一条续轮路，spec §4）。
func (s *orchSvc) markTaskError(ctx context.Context, task *orch_entity.Task, reason string) {
	task.Status = orch_entity.TaskError
	task.Result = reason
	if err := s.tasks.Update(ctx, task); err != nil {
		logger.Ctx(ctx).Error("orch.markTaskError: 写子任务 error 态失败(可被对账纠正)", zap.Int64("task", task.ID), zap.String("status", task.Status), zap.Error(err))
	}
	s.reportToParent(ctx, task.ParentTaskID, task, "技术中断："+reason+"（请决定重试/换 agent/放弃该分支）")
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

