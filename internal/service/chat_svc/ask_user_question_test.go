package chat_svc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/service/chat_svc"
	"agentre/internal/service/chat_svc/blocks"
)

// fakeAskRunner 是测试用的 Runtime + AskAnswerSink 组合体;记录 SubmitAnswer 调用。
// Run 是 zero-impl —— AnswerUserQuestion 测试只走断言路径,不真的发起 turn。
type fakeAskRunner struct {
	gotSession int64
	gotReqID   string
	gotAnswers []agentruntime.AskAnswer
	gotSkipped bool
	err        error
	calls      int
}

func (f *fakeAskRunner) Capabilities() capability.Capabilities { return capability.Capabilities{} }

func (f *fakeAskRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event)
	close(ch)
	return ch, &agentruntime.RunResult{}, nil
}

func (f *fakeAskRunner) SubmitAnswer(_ context.Context, sessionID int64, requestID string, _ []agentruntime.AskQuestion, answers []agentruntime.AskAnswer, skipped bool) error {
	f.calls++
	f.gotSession = sessionID
	f.gotReqID = requestID
	f.gotAnswers = answers
	f.gotSkipped = skipped
	return f.err
}

func TestAnswerUserQuestion(t *testing.T) {
	convey.Convey("AnswerUserQuestion", t, func() {
		m := setupChatTest(t)

		convey.Convey("happy path 投递答案给 backend AskAnswerSink", func() {
			fake := &fakeAskRunner{}
			restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, fake)
			defer restore()

			m.session.EXPECT().Find(m.ctx, int64(42)).Return(&chat_entity.Session{
				ID: 42, AgentID: 7, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(m.ctx, int64(7)).Return(&agent_entity.Agent{
				ID: 7, AgentBackendID: 12, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(m.ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
				ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), Status: consts.ACTIVE,
			}, nil)

			resp, err := m.svc.AnswerUserQuestion(m.ctx, &chat_svc.AnswerUserQuestionRequest{
				SessionID: 42,
				RequestID: "req-001",
				Answers: []blocks.AskAnswerDTO{
					{QuestionIndex: 0, Labels: []string{"last_read_at int64"}},
				},
			})

			assert.NoError(t, err)
			assert.NotNil(t, resp)
			assert.Equal(t, 1, fake.calls)
			assert.Equal(t, int64(42), fake.gotSession)
			assert.Equal(t, "req-001", fake.gotReqID)
			assert.False(t, fake.gotSkipped)
			assert.Len(t, fake.gotAnswers, 1)
			assert.Equal(t, []string{"last_read_at int64"}, fake.gotAnswers[0].Labels)
		})

		convey.Convey("codex backend 也通过同一 AskAnswerSink 投递答案", func() {
			fake := &fakeAskRunner{}
			restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, fake)
			defer restore()

			m.session.EXPECT().Find(m.ctx, int64(43)).Return(&chat_entity.Session{
				ID: 43, AgentID: 8, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(m.ctx, int64(8)).Return(&agent_entity.Agent{
				ID: 8, AgentBackendID: 13, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(m.ctx, int64(13)).Return(&agent_backend_entity.AgentBackend{
				ID: 13, Type: string(agent_backend_entity.TypeCodex), Status: consts.ACTIVE,
			}, nil)

			resp, err := m.svc.AnswerUserQuestion(m.ctx, &chat_svc.AnswerUserQuestionRequest{
				SessionID: 43,
				RequestID: "ask-001",
				Answers: []blocks.AskAnswerDTO{
					{QuestionIndex: 0, Labels: []string{"backend"}},
				},
			})

			assert.NoError(t, err)
			assert.NotNil(t, resp)
			assert.Equal(t, 1, fake.calls)
			assert.Equal(t, int64(43), fake.gotSession)
			assert.Equal(t, "ask-001", fake.gotReqID)
			assert.False(t, fake.gotSkipped)
			assert.Len(t, fake.gotAnswers, 1)
			assert.Equal(t, []string{"backend"}, fake.gotAnswers[0].Labels)
		})

		convey.Convey("skipped 路径 Answers 可空", func() {
			fake := &fakeAskRunner{}
			restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, fake)
			defer restore()

			m.session.EXPECT().Find(m.ctx, int64(42)).Return(&chat_entity.Session{
				ID: 42, AgentID: 7, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(m.ctx, int64(7)).Return(&agent_entity.Agent{
				ID: 7, AgentBackendID: 12, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(m.ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
				ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), Status: consts.ACTIVE,
			}, nil)

			_, err := m.svc.AnswerUserQuestion(m.ctx, &chat_svc.AnswerUserQuestionRequest{
				SessionID: 42,
				RequestID: "req-skip",
				Skipped:   true,
			})

			assert.NoError(t, err)
			assert.True(t, fake.gotSkipped)
			assert.Equal(t, "req-skip", fake.gotReqID)
		})

		convey.Convey("空 sessionID 或 requestID 返 InvalidParameter，不调 sink", func() {
			fake := &fakeAskRunner{}
			restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, fake)
			defer restore()

			_, err := m.svc.AnswerUserQuestion(m.ctx, &chat_svc.AnswerUserQuestionRequest{
				SessionID: 0, RequestID: "x",
				Answers: []blocks.AskAnswerDTO{{QuestionIndex: 0, Labels: []string{"a"}}},
			})
			assert.Error(t, err)
			assert.Equal(t, 0, fake.calls)

			_, err = m.svc.AnswerUserQuestion(m.ctx, &chat_svc.AnswerUserQuestionRequest{
				SessionID: 1, RequestID: "",
				Answers: []blocks.AskAnswerDTO{{QuestionIndex: 0, Labels: []string{"a"}}},
			})
			assert.Error(t, err)
			assert.Equal(t, 0, fake.calls)
		})

		convey.Convey("非 skipped 但 answers 空 → InvalidParameter", func() {
			fake := &fakeAskRunner{}
			restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, fake)
			defer restore()

			_, err := m.svc.AnswerUserQuestion(m.ctx, &chat_svc.AnswerUserQuestionRequest{
				SessionID: 42, RequestID: "req-x",
				Skipped: false,
				Answers: nil,
			})
			assert.Error(t, err)
			assert.Equal(t, 0, fake.calls)
		})

		convey.Convey("sink SubmitAnswer 失败错误透传", func() {
			fake := &fakeAskRunner{err: errors.New("waiter missing")}
			restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, fake)
			defer restore()

			m.session.EXPECT().Find(m.ctx, int64(42)).Return(&chat_entity.Session{
				ID: 42, AgentID: 7, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(m.ctx, int64(7)).Return(&agent_entity.Agent{
				ID: 7, AgentBackendID: 12, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(m.ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
				ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), Status: consts.ACTIVE,
			}, nil)

			_, err := m.svc.AnswerUserQuestion(m.ctx, &chat_svc.AnswerUserQuestionRequest{
				SessionID: 42, RequestID: "req-001",
				Answers: []blocks.AskAnswerDTO{{QuestionIndex: 0, Labels: []string{"x"}}},
			})
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "waiter missing")
		})
	})
}
