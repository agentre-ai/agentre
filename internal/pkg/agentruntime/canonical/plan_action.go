package canonical

// PlanAction 后端装配的"plan 卡上能点的按钮"。
//
// 背景:不同 runtime 的 plan 流转机制不同,但前端只关心"画哪些按钮、点完之后
// 调一个统一 binding 把决定回给后端"。Actions 由 runtime/handler 装配,前端按
// provider-neutral action ID + kind 渲染,不再分支 backendType / source。
//
// ID 命名空间(chat_svc.ResolvePlanAction 分发):
//   - plan.approve.bypass_permissions -> AnswerToolPermission(allow, mode=bypassPermissions)
//   - plan.approve.accept_edits       -> AnswerToolPermission(allow, mode=acceptEdits)
//   - plan.approve.manual             -> AnswerToolPermission(allow, mode=default)
//   - plan.execute                    -> Send("Implement the plan.", mode=default)
//   - plan.refine                     -> requestId 非空时 AnswerToolPermission(deny);
//     requestId 为空时 Send(feedback or 默认文案, mode=plan)
type PlanAction struct {
	ID               string         `json:"id"`
	Kind             PlanActionKind `json:"kind"`
	RequiresFeedback bool           `json:"requiresFeedback,omitempty"`
}

const (
	PlanActionIDApproveBypassPermissions = "plan.approve.bypass_permissions"
	PlanActionIDApproveAcceptEdits       = "plan.approve.accept_edits"
	PlanActionIDApproveManual            = "plan.approve.manual"
	PlanActionIDExecute                  = "plan.execute"
	PlanActionIDRefine                   = "plan.refine"
)

// PlanActionKind 给前端选 button variant/icon。三态足够覆盖当前 6 个 actionId。
type PlanActionKind string

const (
	PlanActionApprove PlanActionKind = "approve"
	PlanActionRefine  PlanActionKind = "refine"
	PlanActionReject  PlanActionKind = "reject"
)
