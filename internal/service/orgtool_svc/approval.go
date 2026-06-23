package orgtool_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
	"github.com/agentre-ai/agentre/internal/service/agent_svc"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/department_svc"
)

// handleWriteTool 写工具统一入口:经 chat_svc 通用网关登记审批 → 挂起等返回的 channel →
// 终态分发。waiter 与前端应答路由(AnswerToolApproval)由 chat_svc 统一持有。
func (s *orgtoolSvc) handleWriteTool(w http.ResponseWriter, r *http.Request, rpcID json.RawMessage, ref orgRef, tool string, rawArgs json.RawMessage) {
	var input map[string]any
	_ = json.Unmarshal(rawArgs, &input)
	requestID := uuid.NewString()
	blk := &blocks.ToolApprovalBlock{ToolKey: agenttool.KeyOrg, RequestID: requestID, ToolName: tool, ToolInput: input, Status: "pending"}

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
		result, execErr := s.execWriteTool(r.Context(), ref, tool, rawArgs)
		if execErr != nil {
			// 业务校验失败(循环挂载/CEO 不可删等)也算 approved 终态,错误进 Result 给 agent 纠错
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

// textResult 把一段文本包成 MCP tool result 结构。
func textResult(text string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
}

// execWriteTool 把已批准的写工具分发到 department_svc / agent_svc。每分支只解参数 + 调
// deps 接口,不写业务逻辑;update 类先 Load 现值再 merge(沿用未给字段)。错误原样上抛。
func (s *orgtoolSvc) execWriteTool(ctx context.Context, ref orgRef, tool string, rawArgs json.RawMessage) (string, error) {
	switch tool {
	case "org_create_department":
		return s.createDepartment(ctx, rawArgs)
	case "org_update_department":
		return s.updateDepartment(ctx, rawArgs)
	case "org_delete_department":
		return s.deleteDepartment(ctx, rawArgs)
	case "org_create_agent":
		return s.createAgent(ctx, ref, rawArgs)
	case "org_update_agent":
		return s.updateAgent(ctx, rawArgs)
	case "org_delete_agent":
		return s.deleteAgent(ctx, rawArgs)
	default:
		return "", fmt.Errorf("未知写工具: %s", tool)
	}
}

func (s *orgtoolSvc) createDepartment(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args createDepartmentArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	resp, err := s.deptCommand.Create(ctx, &department_svc.CreateDepartmentRequest{
		Name:        args.Name,
		Description: args.Description,
		ParentID:    args.ParentID,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("已创建部门「%s」(id=%d)", resp.Item.Name, resp.Item.ID), nil
}

func (s *orgtoolSvc) updateDepartment(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args updateDepartmentArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	cur, err := s.loadDepartment(ctx, args.ID)
	if err != nil {
		return "", err
	}
	name := cur.Name
	if args.Name != "" {
		name = args.Name
	}
	description := cur.Description
	if args.Description != nil {
		description = *args.Description
	}
	leadAgentID := cur.LeadAgentID
	if args.LeadAgentID != nil {
		leadAgentID = *args.LeadAgentID
	}
	if _, err := s.deptCommand.Update(ctx, &department_svc.UpdateDepartmentRequest{
		ID:          args.ID,
		Name:        name,
		Description: description,
		Icon:        cur.Icon,
		AccentColor: cur.AccentColor,
		LeadAgentID: leadAgentID,
	}); err != nil {
		return "", err
	}
	if args.ParentID != nil && *args.ParentID != cur.ParentID {
		if _, err := s.deptCommand.Move(ctx, &department_svc.MoveDepartmentRequest{
			ID:          args.ID,
			NewParentID: *args.ParentID,
		}); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("已更新部门「%s」(id=%d)", name, args.ID), nil
}

func (s *orgtoolSvc) deleteDepartment(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args deleteDepartmentArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	if _, err := s.deptCommand.Delete(ctx, &department_svc.DeleteDepartmentRequest{
		ID:       args.ID,
		Strategy: args.Strategy,
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("已删除部门(id=%d)", args.ID), nil
}

func (s *orgtoolSvc) createAgent(ctx context.Context, ref orgRef, rawArgs json.RawMessage) (string, error) {
	var args createAgentArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	backendID := args.BackendID
	if backendID == 0 {
		caller, err := s.agentLookup.Find(ctx, ref.agentID)
		if err != nil {
			return "", err
		}
		if caller == nil {
			return "", fmt.Errorf("找不到调用者 agent(id=%d)", ref.agentID)
		}
		backendID = caller.AgentBackendID
	}
	resp, err := s.agentCommand.Create(ctx, &agent_svc.CreateAgentRequest{
		Name:           args.Name,
		Description:    args.Description,
		DepartmentID:   args.DepartmentID,
		ParentAgentID:  args.ParentAgentID,
		AgentBackendID: backendID,
		Prompt:         args.Prompt,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("已创建 agent「%s」(id=%d)", resp.Item.Name, resp.Item.ID), nil
}

func (s *orgtoolSvc) updateAgent(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args updateAgentArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	cur, err := s.loadAgent(ctx, args.ID)
	if err != nil {
		return "", err
	}
	name := cur.Name
	if args.Name != "" {
		name = args.Name
	}
	description := cur.Description
	if args.Description != nil {
		description = *args.Description
	}
	prompt := cur.Prompt
	if args.Prompt != nil {
		prompt = args.Prompt
	}
	if _, err := s.agentCommand.Update(ctx, &agent_svc.UpdateAgentRequest{
		ID:             args.ID,
		Name:           name,
		Description:    description,
		AvatarColor:    cur.AvatarColor,
		AvatarIcon:     cur.AvatarIcon,
		AgentBackendID: cur.AgentBackendID,
		Prompt:         prompt,
		Skills:         cur.Skills,
		Tools:          cur.Tools,
	}); err != nil {
		return "", err
	}
	// 挂载位置: department / parentAgent 互斥(agent_svc.Move 语义),只给其一时另一个传 0。
	moveDept := args.DepartmentID != nil && *args.DepartmentID != cur.DepartmentID
	moveParent := args.ParentAgentID != nil && *args.ParentAgentID != cur.ParentAgentID
	if moveDept || moveParent {
		move := &agent_svc.MoveAgentRequest{ID: args.ID}
		if args.DepartmentID != nil {
			move.NewDepartmentID = *args.DepartmentID
		}
		if args.ParentAgentID != nil {
			move.NewParentAgentID = *args.ParentAgentID
		}
		if _, err := s.agentCommand.Move(ctx, move); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("已更新 agent「%s」(id=%d)", name, args.ID), nil
}

func (s *orgtoolSvc) deleteAgent(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args deleteAgentArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	if _, err := s.agentCommand.Delete(ctx, &agent_svc.DeleteAgentRequest{ID: args.ID}); err != nil {
		return "", err
	}
	return fmt.Sprintf("已删除 agent(id=%d)", args.ID), nil
}

// loadDepartment 从 org 全量里按 id 找部门现值(merge 沿用未给字段需要)。
func (s *orgtoolSvc) loadDepartment(ctx context.Context, id int64) (*department_svc.DepartmentItem, error) {
	resp, err := s.orgQuery.Load(ctx, &department_svc.LoadOrgRequest{})
	if err != nil {
		return nil, err
	}
	for _, d := range resp.Departments {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, fmt.Errorf("找不到部门(id=%d)", id)
}

// loadAgent 从 org 全量里按 id 找 agent 现值(merge 沿用未给字段需要)。
func (s *orgtoolSvc) loadAgent(ctx context.Context, id int64) (*department_svc.AgentItem, error) {
	resp, err := s.orgQuery.Load(ctx, &department_svc.LoadOrgRequest{})
	if err != nil {
		return nil, err
	}
	for _, a := range resp.Agents {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, fmt.Errorf("找不到 agent(id=%d)", id)
}
