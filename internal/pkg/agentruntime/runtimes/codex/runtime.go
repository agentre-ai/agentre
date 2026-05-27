// Package codex 是 OpenAI Codex CLI 的 agent runtime,emit sealed agentruntime.Event。
// 本包 init() 时把 *Runtime 注册到 agentruntime.RuntimeFor。
package codex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/pkg/codex"
)

var defaultRuntime = New()

func init() {
	agentruntime.RegisterRuntime(agent_backend_entity.TypeCodex, defaultRuntime)
}

// codexActive 一个 chat session 当前的 codex stream 状态。
//   - stream:turn/steer 入口(*codex.Stream 实现)
//   - interrupter:turn/interrupt 入口
//   - userInput:request_user_input 反向投回入口
//   - pending:本 turn 已发出但还没被 EventUserMessage echo 回来的 steer text
//     (codex 协议 fire-and-forget,本地做 FIFO 配对)
//   - askWaiters:request_user_input 阻塞中的 waiter
//   - out:Run() 期间登记的事件出口,SubmitAnswer 完成后用它 emit UserAskResolved
type codexActive struct {
	mu          sync.Mutex
	stream      cxSteerStream
	interrupter cxInterruptable
	userInput   cxUserInputStream
	pending     []agentruntime.ConsumedSteer
	askWaiters  map[string]codexAskWaiter
	outMu       sync.Mutex
	out         chan<- agentruntime.Event
}

type codexAskWaiter struct {
	questions []agentruntime.AskQuestion
}

// Runtime codex runtime 实现。
type Runtime struct {
	mu     sync.Mutex
	active map[int64]*codexActive
}

func New() *Runtime {
	return &Runtime{active: map[int64]*codexActive{}}
}

// Capabilities 返回 codex runtime 的能力矩阵。
//
// 与 claudecode 的差异:
//   - CapCancelSteer = false(codex turn/steer fire-and-forget,无 withdraw verb)
//   - CapDrainSteer = false(无 hook 队列)
//   - CapToolPermission = false(codex 无 can_use_tool 协议)
//   - CapForkSession = true(走 thread/rollback)
//   - CapReportContextWindow = true(thread/tokenUsage/updated 推 modelContextWindow)
//   - PermissionModeMeta:仅 default / plan;**禁运行时切换**(running/waiting 禁切)
func (r *Runtime) Capabilities() capability.Capabilities {
	return capability.Capabilities{
		Set: map[capability.Capability]bool{
			capability.CapSteer:               true,
			capability.CapAbort:               true,
			capability.CapSetPermission:       true,
			capability.CapAnswerUserAsk:       true,
			capability.CapForkSession:         true,
			capability.CapReportContextWindow: true,
			capability.CapCompact:             true,
		},
		PermissionModeMeta: capability.PermissionModeMeta{
			AllowedModes:         []string{"default", "plan"},
			DefaultMode:          "default",
			SwitchableDuringTurn: false,
			Order:                []string{"default", "plan"},
			// codex 协议要求 launch 时显式 collaboration mode,chat_svc 必须落非空。
			LaunchDefaultMode: "default",
		},
	}
}

func (r *Runtime) register(sessionID int64, a *codexActive) {
	if sessionID <= 0 {
		return
	}
	r.mu.Lock()
	r.active[sessionID] = a
	r.mu.Unlock()
}

func (r *Runtime) unregister(sessionID int64) {
	if sessionID <= 0 {
		return
	}
	r.mu.Lock()
	delete(r.active, sessionID)
	r.mu.Unlock()
}

// Run 启动一轮 codex CLI 发送。语义同顶层 codex.go.Run,emit 类型从
// RuntimeEvent 改为 sealed agentruntime.Event。
func (r *Runtime) Run(ctx context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	if req.Backend == nil {
		return nil, nil, fmt.Errorf("agentruntime/runtimes/codex: nil backend")
	}
	cwd := req.Cwd
	if cwd == "" {
		var err error
		cwd, err = agentruntime.AgentCwd(req.AgentID)
		if err != nil {
			logger.Ctx(ctx).Error("codex runtime: AgentCwd resolve failed",
				zap.Int64("sessionID", req.SessionID),
				zap.Int64("agentID", req.AgentID), zap.Error(err))
			return nil, nil, err
		}
	}
	env, err := BuildCodexEnv(req.Backend, gatewayDeps(req))
	if err != nil {
		logger.Ctx(ctx).Error("codex runtime: BuildCodexEnv failed",
			zap.Int64("sessionID", req.SessionID), zap.Error(err))
		return nil, nil, err
	}

	sess, err := cxSessionFactory(req, env, cwd)
	if err != nil {
		logger.Ctx(ctx).Error("codex runtime: session factory failed",
			zap.Int64("sessionID", req.SessionID),
			zap.String("cwd", cwd), zap.Error(err))
		return nil, nil, err
	}

	if strings.TrimSpace(req.ForkAnchor) != "" {
		if _, err := sess.RewindTo(ctx, req.ForkAnchor); err != nil {
			logger.Ctx(ctx).Error("codex runtime: RewindTo failed",
				zap.Int64("sessionID", req.SessionID),
				zap.String("forkAnchor", req.ForkAnchor), zap.Error(err))
			return nil, nil, err
		}
	}

	var stream cxStream
	if req.Compact {
		stream, err = sess.Compact(ctx)
	} else {
		stream, err = sess.Stream(ctx, req.UserText, req.CollaborationMode)
	}
	if err != nil {
		logger.Ctx(ctx).Error("codex runtime: session run failed",
			zap.Int64("sessionID", req.SessionID),
			zap.Bool("compact", req.Compact),
			zap.String("collaborationMode", req.CollaborationMode), zap.Error(err))
		return nil, nil, err
	}
	logger.Ctx(ctx).Info("codex runtime: turn starting",
		zap.Int64("sessionID", req.SessionID),
		zap.String("providerSessionID", sess.ID()),
		zap.String("collaborationMode", req.CollaborationMode))

	active := &codexActive{stream: sess.ActiveStream(), interrupter: sess.ActiveInterruptor()}
	if ui, ok := active.stream.(cxUserInputStream); ok {
		active.userInput = ui
	}
	r.register(req.SessionID, active)

	out := make(chan agentruntime.Event, 32)
	active.setOut(out)

	modelID := ""
	if req.Provider != nil {
		modelID = strings.TrimSpace(req.Provider.Model)
	}
	if modelID == "" {
		modelID = defaultModelID
	}
	result := &agentruntime.RunResult{ProviderSessionID: sess.ID(), Model: modelID}

	go func() {
		defer close(out)
		defer r.unregister(req.SessionID)
		defer active.setOut(nil)
		drainStream(stream, out, result, active, req.CollaborationMode)
		if sid := stream.SessionID(); sid != "" {
			result.ProviderSessionID = sid
		}
	}()
	return out, result, nil
}

// Abort 软中断当前 turn。语义同顶层 codex.go.Abort。
func (r *Runtime) Abort(ctx context.Context, sessionID int64) error {
	r.mu.Lock()
	a := r.active[sessionID]
	r.mu.Unlock()
	if a == nil {
		return agentruntime.ErrNoActiveTurn
	}
	a.mu.Lock()
	a.pending = nil
	intr := a.interrupter
	a.mu.Unlock()
	if intr == nil {
		return agentruntime.ErrNoActiveTurn
	}
	return intr.Interrupt(ctx)
}

// Steer 把 text dispatch 给 active codex.Stream(turn/steer JSON-RPC)。
// queuedID 仅作本地配对用 —— codex 协议 fire-and-forget。
func (r *Runtime) Steer(ctx context.Context, sessionID int64, queuedID string, text string) error {
	r.mu.Lock()
	a := r.active[sessionID]
	r.mu.Unlock()
	if a == nil || a.stream == nil {
		return agentruntime.ErrNoActiveTurn
	}
	a.addPendingSteer(queuedID, text)
	if err := a.stream.Steer(ctx, text); err != nil {
		a.removePendingSteer(queuedID)
		if errors.Is(err, codex.ErrNoActiveTurn) {
			return agentruntime.ErrNoActiveTurn
		}
		return err
	}
	return nil
}

// SubmitAnswer 把前端提交的 request_user_input 答案反向投回 codex app-server。
// 语义同顶层 codex.go.SubmitAnswer:skipped → 空 answers map(让 LLM 看到拒答);
// 非 skipped → buildUserInputAnswers 拼 codex 期望的 map[questionID][]string。
func (r *Runtime) SubmitAnswer(ctx context.Context, sessionID int64, requestID string, questions []agentruntime.AskQuestion, answers []agentruntime.AskAnswer, skipped bool) error {
	if sessionID <= 0 {
		return fmt.Errorf("agentruntime/runtimes/codex: invalid sessionID %d", sessionID)
	}
	if strings.TrimSpace(requestID) == "" {
		return errors.New("agentruntime/runtimes/codex: empty requestID")
	}
	r.mu.Lock()
	a := r.active[sessionID]
	r.mu.Unlock()
	if a == nil || a.userInput == nil {
		return agentruntime.ErrNoActiveTurn
	}
	waiter := a.askWaiter(requestID)
	if waiter == nil {
		return fmt.Errorf("agentruntime/runtimes/codex: no waiting request_user_input for requestID %s", requestID)
	}
	if len(questions) > 0 && len(questions) != len(waiter.questions) {
		return fmt.Errorf("agentruntime/runtimes/codex: client supplied %d questions but waiter recorded %d", len(questions), len(waiter.questions))
	}
	if skipped {
		if err := a.userInput.SubmitUserInput(ctx, requestID, map[string][]string{}); err != nil {
			return err
		}
		a.removeAskWaiter(requestID)
		emitUserAskResolved(a, requestID, true, nil)
		return nil
	}
	payload, err := buildUserInputAnswers(waiter.questions, answers)
	if err != nil {
		return err
	}
	if err := a.userInput.SubmitUserInput(ctx, requestID, payload); err != nil {
		return err
	}
	a.removeAskWaiter(requestID)
	emitUserAskResolved(a, requestID, false, answers)
	return nil
}

// drainStream 与顶层 drainCodexStream 同构,emit 类型升级到 sealed Event。
func drainStream(stream cxStream, out chan<- agentruntime.Event, result *agentruntime.RunResult, active *codexActive, collaborationMode string) {
	for stream.Next() {
		ev := stream.Event()
		if ev.Kind == codex.EventUserMessage {
			// codex 把 user message echo 回来 —— 对照 pending steer FIFO,
			// 命中就 emit SteerConsumed,让 chat_svc 把对应 queued 状态推进到 consumed。
			if active != nil {
				if steer, ok := active.consumePendingSteer(ev.Text); ok {
					out <- agentruntime.SteerConsumed{Steers: []agentruntime.ConsumedSteer{steer}}
				}
			}
			continue
		}
		if ev.ContextWindow > result.ContextWindow {
			result.ContextWindow = ev.ContextWindow
			out <- agentruntime.ContextWindowUpdated{Tokens: ev.ContextWindow}
		}
		translated, usage, stopErr := translate(ev)
		for _, t := range translated {
			t = attachPlanModeActions(t, collaborationMode)
			// UserAskRequest 同时登记 askWaiter,等 SubmitAnswer 反向唤醒。
			if uar, ok := t.(agentruntime.UserAskRequest); ok && active != nil {
				active.registerAskWaiter(uar.RequestID, uar.Questions)
			}
			out <- t
		}
		if usage != nil {
			result.Usage = usage
		}
		if stopErr != nil {
			result.StopErr = stopErr
		}
	}
}

func (a *codexActive) addPendingSteer(queuedID, text string) {
	if a == nil || queuedID == "" {
		return
	}
	a.mu.Lock()
	a.pending = append(a.pending, agentruntime.ConsumedSteer{QueuedID: queuedID, Text: text})
	a.mu.Unlock()
}

func (a *codexActive) removePendingSteer(queuedID string) {
	if a == nil || queuedID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, p := range a.pending {
		if p.QueuedID == queuedID {
			a.pending = append(a.pending[:i], a.pending[i+1:]...)
			return
		}
	}
}

func (a *codexActive) consumePendingSteer(text string) (agentruntime.ConsumedSteer, bool) {
	if a == nil {
		return agentruntime.ConsumedSteer{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pending) == 0 {
		return agentruntime.ConsumedSteer{}, false
	}
	next := a.pending[0]
	if next.Text == "" {
		if strings.TrimSpace(text) == "" {
			return agentruntime.ConsumedSteer{}, false
		}
		next.Text = text
	} else if strings.TrimSpace(text) != next.Text {
		return agentruntime.ConsumedSteer{}, false
	}
	a.pending = a.pending[1:]
	return next, true
}

func (a *codexActive) registerAskWaiter(requestID string, questions []agentruntime.AskQuestion) {
	if a == nil || strings.TrimSpace(requestID) == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.askWaiters == nil {
		a.askWaiters = map[string]codexAskWaiter{}
	}
	a.askWaiters[requestID] = codexAskWaiter{questions: append([]agentruntime.AskQuestion(nil), questions...)}
}

func (a *codexActive) askWaiter(requestID string) *codexAskWaiter {
	if a == nil || strings.TrimSpace(requestID) == "" {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	w, ok := a.askWaiters[requestID]
	if !ok {
		return nil
	}
	return &w
}

func (a *codexActive) removeAskWaiter(requestID string) {
	if a == nil || strings.TrimSpace(requestID) == "" {
		return
	}
	a.mu.Lock()
	delete(a.askWaiters, requestID)
	a.mu.Unlock()
}

func (a *codexActive) setOut(out chan<- agentruntime.Event) {
	if a == nil {
		return
	}
	a.outMu.Lock()
	a.out = out
	a.outMu.Unlock()
}

func (a *codexActive) outChan() chan<- agentruntime.Event {
	if a == nil {
		return nil
	}
	a.outMu.Lock()
	defer a.outMu.Unlock()
	return a.out
}

// emitUserAskResolved 把答案终态 emit 给 drain 通道。out nil 或 channel 满时
// 不阻塞(前端有乐观更新)。
func emitUserAskResolved(a *codexActive, requestID string, skipped bool, answers []agentruntime.AskAnswer) {
	out := a.outChan()
	if out == nil {
		return
	}
	ev := agentruntime.UserAskResolved{
		RequestID: requestID,
		Skipped:   skipped,
		Answers:   answers,
	}
	select {
	case out <- ev:
	default:
	}
}

// buildUserInputAnswers 把前端 AskAnswer 列表拼成 codex 期望的
// map[questionID][]string。镜像顶层 codex.go.buildCodexUserInputAnswers。
func buildUserInputAnswers(questions []agentruntime.AskQuestion, answers []agentruntime.AskAnswer) (map[string][]string, error) {
	if len(answers) == 0 {
		return nil, errors.New("agentruntime/runtimes/codex: empty answers")
	}
	result := make(map[string][]string, len(answers))
	for _, ans := range answers {
		if ans.QuestionIndex < 0 || ans.QuestionIndex >= len(questions) {
			return nil, fmt.Errorf("agentruntime/runtimes/codex: answer question index %d out of range (have %d questions)", ans.QuestionIndex, len(questions))
		}
		if len(ans.Labels) == 0 {
			return nil, fmt.Errorf("agentruntime/runtimes/codex: question %d has no selected labels", ans.QuestionIndex)
		}
		q := questions[ans.QuestionIndex]
		if strings.TrimSpace(q.ID) == "" {
			return nil, fmt.Errorf("agentruntime/runtimes/codex: question %d missing codex id", ans.QuestionIndex)
		}
		seen := make(map[string]struct{}, len(ans.Labels))
		values := make([]string, 0, len(ans.Labels))
		for _, label := range ans.Labels {
			value := label
			if label == agentruntime.OtherAnswerLabel {
				if strings.TrimSpace(ans.OtherText) == "" {
					return nil, fmt.Errorf("agentruntime/runtimes/codex: question %d picked %q with empty OtherText", ans.QuestionIndex, agentruntime.OtherAnswerLabel)
				}
				value = ans.OtherText
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			values = append(values, value)
		}
		result[q.ID] = values
	}
	return result, nil
}
