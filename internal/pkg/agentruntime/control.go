package agentruntime

// 控制平面小接口:Runtime 按需实现;chat_svc 通过 type assertion 取得。
// Capabilities() 中的 bool 与"该 runtime 是否实现对应接口"必须一致(capability
// matrix 测试强制)。
//
// UserAskAnswerer / ToolPermissionResponder 是 AskAnswerSink / ToolPermissionSink
// 的同签名别名 —— 旧名仍在 runner.go 被 chat_svc 直接消费;新名给新 dispatcher 路径用。
// 切完后再 dedupe(目前两套都有 caller)。
// Steerer / Aborter / SteerCanceler / SteerDrainer / PermissionModeSetter 仍住 runner.go。

import (
	"context"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/llm_provider_entity"
)

// UserAskAnswerer 反向投回用户答案。旧名 AskAnswerSink。签名严格一致。
type UserAskAnswerer interface {
	SubmitAnswer(ctx context.Context, sessionID int64, requestID string,
		questions []AskQuestion, answers []AskAnswer, skipped bool) error
}

// ToolPermissionResponder 反向投回工具审批决策。旧名 ToolPermissionSink。
// 签名严格一致 —— 同一个实现可同时满足两个 interface (control_test.go 验证)。
type ToolPermissionResponder interface {
	SubmitToolPermission(ctx context.Context, sessionID int64, requestID string,
		allow, alwaysAllowSession bool, denyReason string) error
}

type Goal struct {
	ThreadID        string `json:"threadId"`
	Objective       string `json:"objective"`
	Status          string `json:"status"`
	TokenBudget     *int   `json:"tokenBudget,omitempty"`
	TokensUsed      int    `json:"tokensUsed"`
	TimeUsedSeconds int    `json:"timeUsedSeconds"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

type GoalRequest struct {
	SessionID         int64
	AgentID           int64
	ProviderSessionID string
	Backend           *agent_backend_entity.AgentBackend
	Provider          *llm_provider_entity.LLMProvider
	Cwd               string
	GatewayURL        string
	GatewayToken      string
	Objective         *string
	Status            *string
	TokenBudget       *int
}

type GoalController interface {
	GetGoal(ctx context.Context, req GoalRequest) (*Goal, error)
	SetGoal(ctx context.Context, req GoalRequest) (*Goal, error)
	ClearGoal(ctx context.Context, req GoalRequest) (bool, error)
}
