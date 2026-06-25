package hooktool_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/hook_svc"
)

// handleWriteTool 写工具统一入口:经 chat_svc 通用网关登记审批 → 挂起等返回的 channel →
// 终态分发。waiter 与前端应答路由(AnswerToolApproval)由 chat_svc 统一持有。
// hook_run(含 dryRun)也走此门:每次在用户机上执行脚本都应让用户点头(设计 §7.2)。
func (s *hooktoolSvc) handleWriteTool(w http.ResponseWriter, r *http.Request, rpcID json.RawMessage, ref hookRef, tool string, rawArgs json.RawMessage) {
	var input map[string]any
	_ = json.Unmarshal(rawArgs, &input)
	requestID := uuid.NewString()
	blk := &blocks.ToolApprovalBlock{ToolKey: agenttool.KeyHook, RequestID: requestID, ToolName: tool, ToolInput: input, Status: "pending"}

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
			// 业务校验失败(重名/解释器非法/cron 非法等)也算 approved 终态,错误进 Result 给 agent 纠错
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
		_ = s.approval.FinishToolApproval(context.Background(), ref.sessionID, requestID, "expired", "")
	}
}

// execWriteTool 把已批准的写工具分发到 hook_svc。每分支只解参数 + 调 deps 接口,不写业务逻辑。
func (s *hooktoolSvc) execWriteTool(ctx context.Context, tool string, rawArgs json.RawMessage) (string, error) {
	switch tool {
	case "hook_create":
		return s.createHook(ctx, rawArgs)
	case "hook_update":
		return s.updateHook(ctx, rawArgs)
	case "hook_delete":
		return s.deleteHook(ctx, rawArgs)
	case "hook_run":
		return s.runHook(ctx, rawArgs)
	default:
		return "", fmt.Errorf("未知写工具: %s", tool)
	}
}

func (s *hooktoolSvc) createHook(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args createHookArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	enabled := true
	if args.Enabled != nil {
		enabled = *args.Enabled
	}
	item, err := s.hooks.CreateHook(ctx, &hook_svc.CreateHookRequest{
		Name:            args.Name,
		Interpreter:     args.Interpreter,
		InterpreterPath: args.InterpreterPath,
		Command:         args.Command,
		ScheduleExpr:    args.ScheduleExpr,
		Timezone:        args.Timezone,
		Env:             args.Env,
		Enabled:         enabled,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("已创建 Hook「%s」(id=%d)", item.Name, item.ID), nil
}

// updateHook 先取现值再 merge 未提供字段(指针非 nil 才覆盖);env 非 nil 即整体替换,
// 其中值为 ******** 的密钥由 hook_svc.preserveSecrets 保留原值。
func (s *hooktoolSvc) updateHook(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args updateHookArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	cur, err := s.loadHook(ctx, args.ID)
	if err != nil {
		return "", err
	}
	req := &hook_svc.UpdateHookRequest{ID: args.ID}
	req.Name = orStr(args.Name, cur.Name)
	req.Interpreter = orStr(args.Interpreter, cur.Interpreter)
	req.InterpreterPath = orStr(args.InterpreterPath, cur.InterpreterPath)
	req.Command = orStr(args.Command, cur.Command)
	req.ScheduleExpr = orStr(args.ScheduleExpr, cur.ScheduleExpr)
	req.Timezone = orStr(args.Timezone, cur.Timezone)
	if args.Env != nil {
		req.Env = *args.Env
	} else {
		req.Env = cur.Env // cur.Env 的密钥已是 ******** ,hook_svc.preserveSecrets 会保留原值
	}
	req.Enabled = cur.Enabled
	if args.Enabled != nil {
		req.Enabled = *args.Enabled
	}
	item, err := s.hooks.UpdateHook(ctx, req)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("已更新 Hook「%s」(id=%d)", item.Name, item.ID), nil
}

func (s *hooktoolSvc) deleteHook(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args deleteHookArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	if err := s.hooks.DeleteHook(ctx, args.ID); err != nil {
		return "", err
	}
	return fmt.Sprintf("已删除 Hook(id=%d)", args.ID), nil
}

func (s *hooktoolSvc) runHook(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args runHookArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	dryRun := true
	if args.DryRun != nil {
		dryRun = *args.DryRun
	}
	res, err := s.hooks.RunHook(ctx, &hook_svc.RunHookRequest{ID: args.ID, DryRun: dryRun})
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

// loadHook 从全量里按 id 找 hook 现值(update merge 需要)。
func (s *hooktoolSvc) loadHook(ctx context.Context, id int64) (*hook_svc.HookItem, error) {
	resp, err := s.hooks.Load(ctx, &hook_svc.LoadHooksRequest{HookID: id})
	if err != nil {
		return nil, err
	}
	for _, h := range resp.Hooks {
		if h.ID == id {
			return h, nil
		}
	}
	return nil, fmt.Errorf("找不到 Hook(id=%d)", id)
}

// orStr 指针非 nil 取其值,否则取兜底现值。
func orStr(p *string, fallback string) string {
	if p != nil {
		return *p
	}
	return fallback
}
