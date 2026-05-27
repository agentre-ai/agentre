package chat_svc

import (
	"context"
	"strings"

	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/canonical"
	"agentre/internal/repository/chat_repo"
	"agentre/internal/service/chat_svc/handlers"
	"agentre/internal/service/chat_svc/turn"
)

// dispatcher_adapters.go 给 turn dispatcher 注入持久化 + 数据写入能力。
//
// 设计意图:handlers/ 包不能依赖 chat_svc(避免循环);它声明几组小接口
// (MessageUpdater / SessionUpdater / UsageWriter / ErrorWriter / ContextWindowWriter),
// chat_svc 在这里实现并通过 TurnContext / handler 字段注入。

// messageUpdaterAdapter 实现 turn.MessageUpdater,转回 chat_repo.Message().Update。
// any 是 *chat_entity.Message;类型不符默默 no-op。
type messageUpdaterAdapter struct{}

func (messageUpdaterAdapter) Update(ctx context.Context, msg any) error {
	m, ok := msg.(*chat_entity.Message)
	if !ok || m == nil {
		return nil
	}
	return chat_repo.Message().Update(ctx, m)
}

// sessionUpdaterAdapter 实现 turn.SessionUpdater。
// PermissionModeChangedHandler 调它时,assistantMsg 是 *chat_entity.Session。
type sessionUpdaterAdapter struct{}

func (sessionUpdaterAdapter) Update(ctx context.Context, sess any) error {
	s, ok := sess.(*chat_entity.Session)
	if !ok || s == nil {
		return nil
	}
	return chat_repo.Session().Update(ctx, s)
}

// usageWriterAdapter 实现 handlers.UsageWriter:把 agentruntime.UsageUpdate
// patch 到 *chat_entity.Message 的 token 列。
type usageWriterAdapter struct{}

func (usageWriterAdapter) WriteUsage(msg any, u *agentruntime.UsageUpdate) {
	m, ok := msg.(*chat_entity.Message)
	if !ok || m == nil || u == nil || u.Usage == nil {
		return
	}
	m.PromptTokens = u.Usage.PromptTokens
	m.CompletionTokens = u.Usage.CompletionTokens
	m.CachedTokens = u.Usage.CachedTokens
	m.CacheCreationTokens = u.Usage.CacheCreationTokens
	m.ReasoningTokens = u.Usage.ReasoningTokens
	if u.TotalInputTokens > 0 {
		m.TotalInputTokens = u.TotalInputTokens
	}
}

func (usageWriterAdapter) MessageID(msg any) int64 {
	m, ok := msg.(*chat_entity.Message)
	if !ok || m == nil {
		return 0
	}
	return m.ID
}

// errorWriterAdapter 实现 handlers.ErrorWriter。
type errorWriterAdapter struct{}

func (errorWriterAdapter) WriteErrorText(msg any, errText string) {
	m, ok := msg.(*chat_entity.Message)
	if !ok || m == nil {
		return
	}
	m.ErrorText = errText
}

// contextWindowWriterAdapter 实现 handlers.ContextWindowWriter。
type contextWindowWriterAdapter struct{}

func (contextWindowWriterAdapter) WriteContextWindow(sess any, tokens int) {
	s, ok := sess.(*chat_entity.Session)
	if !ok || s == nil {
		return
	}
	s.ContextWindow = tokens
}

// permissionModeWriterAdapter 实现 handlers.PermissionModeWriter:
// CurrentMode 读 sess.PermissionMode(handler 幂等判断);SetMode 把
// sess.PermissionMode 同步为新值 + 调 chat_repo.Session().UpdatePermissionMode
// (column-only,避免 Update 写整行覆盖其它字段)。
type permissionModeWriterAdapter struct{}

func (permissionModeWriterAdapter) CurrentMode(sess any) string {
	s, ok := sess.(*chat_entity.Session)
	if !ok || s == nil {
		return ""
	}
	return s.PermissionMode
}

func (permissionModeWriterAdapter) SetMode(ctx context.Context, sess any, mode string) error {
	s, ok := sess.(*chat_entity.Session)
	if !ok || s == nil {
		return nil
	}
	s.PermissionMode = mode
	return chat_repo.Session().UpdatePermissionMode(ctx, s.ID, mode)
}

// planWriterAdapter 实现 handlers.PlanWriter:把 canonical PlanUpdate 投影到
// chat_svc.PlanBlock,通过 acc.AddBlock 落到 turn accumulator。
type planWriterAdapter struct{}

func (planWriterAdapter) WritePlan(acc *turn.Accumulator, plan canonical.PlanUpdate) {
	if acc == nil {
		return
	}
	text := plan.Text
	if strings.TrimSpace(text) != "" {
		acc.AddBlock(PlanBlock{Text: text, Actions: plan.Actions}, "")
		return
	}
	steps := plan.Steps
	if len(steps) == 0 {
		return
	}
	dtoSteps := make([]PlanStepDTO, 0, len(steps))
	for _, s := range steps {
		st := strings.TrimSpace(s.Step)
		if st == "" {
			continue
		}
		dtoSteps = append(dtoSteps, PlanStepDTO{Step: st, Status: string(s.Status)})
	}
	blk := PlanBlock{Steps: dtoSteps, Text: formatPlanText(dtoSteps), Actions: plan.Actions}
	if strings.TrimSpace(blk.Text) == "" {
		return
	}
	acc.AddBlock(blk, "")
}

// compactInspectorAdapter 暴露 *chat_entity.Message 的 ID/Seq 给
// handlers.CompactBoundaryHandler 用,不让 handler 包反向依赖 chat_entity。
type compactInspectorAdapter struct{}

func (compactInspectorAdapter) MessageID(msg any) int64 {
	m, ok := msg.(*chat_entity.Message)
	if !ok || m == nil {
		return 0
	}
	return m.ID
}

func (compactInspectorAdapter) MessageSeq(msg any) int {
	m, ok := msg.(*chat_entity.Message)
	if !ok || m == nil {
		return 0
	}
	return m.Seq
}

// buildHandlersWithAdapters 返回填充了 chat_svc 适配器的 handler 实例。
func buildHandlersWithAdapters(_ *chatSvc) (
	handlers.UsageUpdateHandler,
	handlers.ErrorHandler,
	handlers.ContextWindowUpdatedHandler,
	handlers.PermissionModeChangedHandler,
	handlers.PlanUpdatedHandler,
	handlers.CompactBoundaryHandler,
) {
	return handlers.UsageUpdateHandler{Writer: usageWriterAdapter{}},
		handlers.ErrorHandler{Writer: errorWriterAdapter{}},
		handlers.ContextWindowUpdatedHandler{Writer: contextWindowWriterAdapter{}},
		handlers.PermissionModeChangedHandler{Writer: permissionModeWriterAdapter{}},
		handlers.PlanUpdatedHandler{Writer: planWriterAdapter{}},
		handlers.CompactBoundaryHandler{Inspector: compactInspectorAdapter{}}
}

// sessionTransitionerAdapter 把 turn.SessionTransitioner 调到 chatSvc 的
// markSessionWaiting / markSessionRunning。需要持 *chatSvc 引用(单例方法,
// 不是 receiver-less helper)。
type sessionTransitionerAdapter struct{ svc *chatSvc }

func (a sessionTransitionerAdapter) MarkWaiting(ctx context.Context, sess any, stream string) {
	if a.svc == nil {
		return
	}
	s, ok := sess.(*chat_entity.Session)
	if !ok || s == nil {
		return
	}
	a.svc.markSessionWaiting(ctx, s, stream)
}

func (a sessionTransitionerAdapter) MarkRunning(ctx context.Context, sess any, stream string) {
	if a.svc == nil {
		return
	}
	s, ok := sess.(*chat_entity.Session)
	if !ok || s == nil {
		return
	}
	a.svc.markSessionRunning(ctx, s, stream)
}

// newTurnContext 构造每轮 turn 的 TurnContext。stream 由调用方填(每轮 chat
// session 不同)。
func (s *chatSvc) newTurnContext(
	assistantMsg *chat_entity.Message,
	sess *chat_entity.Session,
	stream string,
	backendType string,
) *turn.TurnContext {
	launch := ""
	if sess != nil {
		launch = sess.PermissionModeAtLaunch
	}
	return &turn.TurnContext{
		AssistantMsg:         assistantMsg,
		Session:              sess,
		Stream:               stream,
		BackendType:          backendType,
		LaunchPermissionMode: launch,
		MessageUpdater:       messageUpdaterAdapter{},
		SessionUpdater:       sessionUpdaterAdapter{},
		SessionTransitioner:  sessionTransitionerAdapter{svc: s},
	}
}
