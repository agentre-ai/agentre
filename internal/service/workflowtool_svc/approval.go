package workflowtool_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/workflow_svc"
)

// handleWriteTool 写工具统一入口:登记审批 → 挂起等待 → 终态分发(用通用网关)。
func (s *workflowtoolSvc) handleWriteTool(w http.ResponseWriter, r *http.Request, rpcID json.RawMessage, ref workflowRef, tool string, rawArgs json.RawMessage) {
	var input map[string]any
	_ = json.Unmarshal(rawArgs, &input)
	requestID := uuid.NewString()
	blk := &blocks.ToolApprovalBlock{ToolKey: agenttool.KeyWorkflow, RequestID: requestID, ToolName: tool, ToolInput: input, Status: "pending"}

	ch, err := s.approval.BeginToolApproval(r.Context(), ref.sessionID, blk)
	if err != nil {
		writeRPCError(w, rpcID, -32000, "审批通道不可用: "+err.Error())
		return
	}
	select {
	case allow := <-ch:
		if !allow {
			_ = s.approval.FinishToolApproval(r.Context(), ref.sessionID, requestID, "denied", "")
			writeRPCResult(w, rpcID, textResult("用户拒绝了此操作"))
			return
		}
		result, execErr := s.execWriteTool(r.Context(), tool, rawArgs)
		if execErr != nil {
			// 业务校验失败(流程不存在/重名等)也算 approved 终态,错误进 Result 给 agent 纠错
			_ = s.approval.FinishToolApproval(r.Context(), ref.sessionID, requestID, "approved", "执行失败: "+execErr.Error())
			writeRPCResult(w, rpcID, textResult("已批准但执行失败: "+execErr.Error()))
			return
		}
		_ = s.approval.FinishToolApproval(r.Context(), ref.sessionID, requestID, "approved", result)
		writeRPCResult(w, rpcID, textResult(result))
	case <-time.After(s.approvalTimeout):
		_ = s.approval.FinishToolApproval(r.Context(), ref.sessionID, requestID, "expired", "")
		writeRPCResult(w, rpcID, textResult("审批超时，操作未执行"))
	case <-r.Context().Done():
		// 请求 ctx 已死,用 Background 调 Finish
		_ = s.approval.FinishToolApproval(context.Background(), ref.sessionID, requestID, "expired", "")
	}
}

func textResult(text string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
}

func (s *workflowtoolSvc) execWriteTool(ctx context.Context, tool string, rawArgs json.RawMessage) (string, error) {
	switch tool {
	case "workflow_create":
		return s.createWorkflow(ctx, rawArgs)
	case "workflow_update":
		return s.updateWorkflow(ctx, rawArgs)
	case "workflow_delete":
		return s.deleteWorkflow(ctx, rawArgs)
	default:
		return "", fmt.Errorf("未知写工具: %s", tool)
	}
}

func (s *workflowtoolSvc) createWorkflow(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args createWorkflowArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	resp, err := s.command.Create(ctx, &workflow_svc.CreateWorkflowRequest{Name: args.Name, Content: args.Content})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("已创建流程「%s」(id=%d)", resp.Item.Name, resp.Item.ID), nil
}

func (s *workflowtoolSvc) updateWorkflow(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args updateWorkflowArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	cur, err := s.loadWorkflow(ctx, args.ID)
	if err != nil {
		return "", err
	}
	name := cur.Name
	if args.Name != nil {
		name = *args.Name
	}
	content := cur.Content
	if args.Content != nil {
		content = *args.Content
	}
	resp, err := s.command.Update(ctx, &workflow_svc.UpdateWorkflowRequest{ID: args.ID, Name: name, Content: content})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("已更新流程「%s」(id=%d)", resp.Item.Name, resp.Item.ID), nil
}

func (s *workflowtoolSvc) deleteWorkflow(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args deleteWorkflowArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	cur, err := s.loadWorkflow(ctx, args.ID)
	if err != nil {
		return "", err
	}
	if _, err := s.command.Delete(ctx, &workflow_svc.DeleteWorkflowRequest{ID: args.ID}); err != nil {
		return "", err
	}
	return fmt.Sprintf("已删除流程「%s」(id=%d,原被 %d 个群使用)", cur.Name, args.ID, cur.GroupCount), nil
}

// loadWorkflow 从 List 里按 id 找现值(update merge / delete 文案需要)。Items 是 []*WorkflowItem。
func (s *workflowtoolSvc) loadWorkflow(ctx context.Context, id int64) (*workflow_svc.WorkflowItem, error) {
	resp, err := s.query.List(ctx, &workflow_svc.ListWorkflowsRequest{})
	if err != nil {
		return nil, err
	}
	for _, it := range resp.Items {
		if it.ID == id {
			return it, nil
		}
	}
	return nil, fmt.Errorf("找不到流程(id=%d)", id)
}
