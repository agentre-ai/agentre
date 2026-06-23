package chat_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/canonical"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/view"
)

// askUserQuestionBlockToChatBlock 把持久化的 blocks.UserAskBlock 投影到前端
// 显示用的 ChatBlock。history 回放走 toChatMessage 时调用。Canonical 字段让
// 前端 CanonicalToolRouter 与 live 路径共用一份渲染入口(UserAskCard)。
func askUserQuestionBlockToChatBlock(b blocks.UserAskBlock) ChatBlock {
	return ChatBlock{
		Type: "ask_user_question",
		AskUserQuestion: &ChatBlockAskUserQuestion{
			RequestID: b.RequestID,
			Questions: b.Questions,
			Answered:  b.Answered,
			Answers:   b.Answers,
			Skipped:   b.Skipped,
		},
		Canonical: view.FromCanonical(canonical.UserAsk{
			RequestID: b.RequestID,
			Questions: b.Questions,
			Answers:   b.Answers,
			Answered:  b.Answered,
			Skipped:   b.Skipped,
		}),
	}
}

// AnswerUserQuestionRequest 前端答完题调 App.AnswerUserQuestion 时的 payload。
// RequestID 必填 —— 它是 agentre runtime 端 waiter 表的主键，也是 CLI
// 端 control_request.request_id。Skipped=true 时 Answers 可为空。
type AnswerUserQuestionRequest struct {
	SessionID int64                 `json:"sessionId"`
	RequestID string                `json:"requestId"`
	Answers   []blocks.AskAnswerDTO `json:"answers,omitempty"`
	Skipped   bool                  `json:"skipped,omitempty"`
}

// AnswerUserQuestionResponse 当前没有载荷；保留结构便于将来扩展
// （比如返回更新后的 ChatBlock 让前端重渲染）。
type AnswerUserQuestionResponse struct{}

// AnswerUserQuestion 把用户答案通过 backend 的 AskAnswerSink 投回正在等待的
// 交互请求，backend 收到答案后在同 turn 内继续推进。
//
// 流程：
//  1. 校验 session 存在 + 取 agent backend
//  2. s.selectRunner(ctx, be, sess.ID) 拿 runner；类型断言为 AskAnswerSink
//     —— claudecode / codex 均实现；其它 backend 接入时沿用同一接口
//  3. 反向转换 DTO → runtime 类型，再调 sink.SubmitAnswer
func (s *chatSvc) AnswerUserQuestion(ctx context.Context, req *AnswerUserQuestionRequest) (*AnswerUserQuestionResponse, error) {
	if req == nil || req.SessionID <= 0 || req.RequestID == "" {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	if !req.Skipped && len(req.Answers) == 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}

	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil || sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}

	a, err := agent_repo.Agent().Find(ctx, sess.AgentID)
	if err != nil || a == nil {
		return nil, i18n.NewError(ctx, code.AgentNotFound)
	}
	if a.AgentBackendID <= 0 {
		return nil, i18n.NewError(ctx, code.AgentBackendRequired)
	}
	be, err := agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID)
	if err != nil || be == nil {
		return nil, i18n.NewError(ctx, code.AgentBackendNotFound)
	}

	runner, err := s.selectRunner(ctx, be, sess.ID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.AgentBackendTypeUnsupported)
	}
	sink, ok := runner.(agentruntime.AskAnswerSink)
	if !ok {
		return nil, i18n.NewError(ctx, code.AgentBackendTypeUnsupported)
	}

	rtAnswers := blocks.AnswersToRuntime(req.Answers)
	if err := sink.SubmitAnswer(ctx, req.SessionID, req.RequestID, nil, rtAnswers, req.Skipped); err != nil {
		return nil, err
	}
	return &AnswerUserQuestionResponse{}, nil
}
