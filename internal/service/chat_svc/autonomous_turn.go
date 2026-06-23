package chat_svc

import (
	"context"
	"fmt"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/handlers"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/turn"
)

// startAutonomousWatcher 为某 claudecode 会话惰性启动一个 watcher goroutine,订阅
// runtime 的「自主续轮」(CLI 在 run_in_background 任务完成后**自主**跑的一轮),
// 逐轮落成纯 assistant 轮。每会话只起一个(autoWatchers 去重);底层 AutonomousTurns
// channel 在子进程 evict / CloseSession 时 close,watcher 随之退出并清去重位。
//
// 并发约束(关键):watcher 在 driveAutonomousTurn 里 **绝不持 chat 会话锁** drain。
// 否则与 pkg/claudecode.Session 常驻 reader 死锁 —— evOut 不被 drain → Session 活跃
// 槽位不释放 → 用户 turn 卡在 Session.turnMu 上(且它持着 chat 锁)→ watcher 永远拿
// 不到锁。自主轮与用户 turn 的串行由底层 Session 单活跃槽位天然保证(FIFO);跨 turn
// 的 session 行写按 last-write-wins,极少数重叠(用户在自主轮进行中又发消息)靠
// 前端 StreamDone→reloadSession 收敛。
func (s *chatSvc) startAutonomousWatcher(sessionID int64, be *agent_backend_entity.AgentBackend, src agentruntime.AutonomousTurnSource) {
	if sessionID <= 0 || be == nil || src == nil {
		return
	}
	if _, loaded := s.autoWatchers.LoadOrStore(sessionID, struct{}{}); loaded {
		return
	}
	beCopy := *be
	go func() {
		defer s.autoWatchers.Delete(sessionID)
		for at := range src.AutonomousTurns(sessionID) {
			s.driveAutonomousTurn(context.Background(), sessionID, &beCopy, at)
		}
	}()
}

// driveAutonomousTurn 把一轮自主续轮落成 **纯 assistant 消息(无 user 行)**:
//  1. 加载 session(取最新状态);
//  2. 事务建 assistant 消息(seq 续在末尾)+ 翻 running;
//  3. 经会话级旁路 emit StreamAutonomousStarted —— per-turn 流只有用户 Send 才有入口,
//     自主轮没有,所以把 stream 名 + 新 assistant 行推给前端,让它插入并 openStream;
//  4. 用 dispatcher drain at.Events(实时 stream chunk / tool / plan ...);
//  5. 收尾:落 blocks + usage/model、翻 idle、emit StreamDone。
//
// 任何一步加载/落库失败 → log + 把 at.Events 抽干(别让 Session reader 阻塞)+ 返回。
func (s *chatSvc) driveAutonomousTurn(ctx context.Context, sessionID int64, be *agent_backend_entity.AgentBackend, at agentruntime.AutonomousTurn) {
	sess, err := chat_repo.Session().Find(ctx, sessionID)
	if err != nil || sess == nil {
		logger.Ctx(ctx).Warn("chat_svc: driveAutonomousTurn load session failed; draining events",
			zap.Int64("sessionId", sessionID), zap.Error(err))
		drainAndDiscard(at.Events)
		return
	}

	assistantMsg := &chat_entity.Message{
		SessionID:  sessionID,
		DeviceID:   be.DeviceID,
		Role:       "assistant",
		BlocksJSON: "[]",
	}
	if at.Result != nil && at.Result.Model != "" {
		assistantMsg.Model = at.Result.Model
	}
	if err := db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		txCtx := db.WithContextDB(ctx, tx)
		nextSeq, err := chat_repo.Message().NextSeq(txCtx, sessionID)
		if err != nil {
			return err
		}
		assistantMsg.Seq = nextSeq
		if err := chat_repo.Message().Create(txCtx, assistantMsg); err != nil {
			return err
		}
		sess.AgentStatus = "running"
		sess.NeedsAttention = false
		sess.LastMessageAt = time.Now().UnixMilli()
		return chat_repo.Session().Update(txCtx, sess)
	}); err != nil {
		logger.Ctx(ctx).Error("chat_svc: driveAutonomousTurn persist assistant failed; draining events",
			zap.Int64("sessionId", sessionID), zap.Error(err))
		drainAndDiscard(at.Events)
		return
	}

	// 若本自主轮由后台命令完成触发,带上完成任务身份,供前端即时翻转上一条消息里
	// 的 subagent_state 块,并在收尾后落库定向翻转。remote 转发当前不携带 CompletedTask
	// (v1 已知限制),此处对 nil/空 ToolUseID 全程 no-op。
	var completedRef *CompletedTaskRef
	if at.CompletedTask != nil && at.CompletedTask.ToolUseID != "" {
		st := at.CompletedTask.Status
		if st == "" {
			st = "completed"
		}
		completedRef = &CompletedTaskRef{
			ToolUseID: at.CompletedTask.ToolUseID,
			Status:    st,
			Summary:   at.CompletedTask.Summary,
		}
	}

	stream := StreamName(sessionID, assistantMsg.ID)
	logger.Ctx(ctx).Info("chat_svc: autonomous turn started",
		zap.Int64("sessionId", sessionID),
		zap.Int64("assistantMsgId", assistantMsg.ID),
		zap.String("trigger", at.Trigger))
	// 会话级旁路:让前端插入新 assistant 行并 openStream 订阅 per-turn 流。
	s.emitter.Emit(ctx, AutonomousStreamName(sessionID), ChatStreamEvent{
		Kind:             StreamAutonomousStarted,
		Stream:           stream,
		Trigger:          at.Trigger,
		AssistantMessage: chatMessageForEvent(sess, assistantMsg),
		CompletedTask:    completedRef,
	})

	acc := turn.New()
	dispEmit := &dispatcherEmitter{svc: s}
	turnCtx := s.newTurnContext(assistantMsg, sess, stream, be.Type)
	for ev := range at.Events {
		if err := s.dispatcher.Apply(ctx, ev, acc, dispEmit, nil, turnCtx); err != nil {
			logger.Ctx(ctx).Warn("chat_svc: autonomous dispatcher Apply failed",
				zap.String("eventType", fmt.Sprintf("%T", ev)), zap.Error(err))
		}
		if shouldCheckpointAssistantAfterEvent(ev) {
			s.checkpointAssistantNew(ctx, assistantMsg, acc)
		}
	}

	finalBlocks := acc.Finalize()
	// 镜像 Send 路径(chat.go):本自主轮结束时仍 running 的 subagent(没等到
	// SubagentDone,如轮被中断)翻成 "canceled",否则原样落 DB 让前端后台任务芯片
	// 永远 spin。只动本轮 finalBlocks,不碰更早消息里的后台 bash 块(那条由
	// FlipSubagentStatus 定向翻转)。
	handlers.MarkRunningSubagentsCancelled(finalBlocks)
	_ = assistantMsg.SetBlocks(finalBlocks)
	if at.Result != nil {
		if at.Result.Usage != nil {
			assistantMsg.PromptTokens = at.Result.Usage.PromptTokens
			assistantMsg.CompletionTokens = at.Result.Usage.CompletionTokens
			assistantMsg.CachedTokens = at.Result.Usage.CachedTokens
			assistantMsg.CacheCreationTokens = at.Result.Usage.CacheCreationTokens
			assistantMsg.ReasoningTokens = at.Result.Usage.ReasoningTokens
		}
		if at.Result.Model != "" {
			assistantMsg.Model = at.Result.Model
		}
		if at.Result.ProviderSessionID != "" {
			sess.SetProviderSession(at.Result.ProviderSessionID)
		}
	}
	// finalCtx 去掉 cancel 信号但保留 DB 句柄 —— 已经流出去的内容必须落库。
	finalCtx := context.WithoutCancel(ctx)
	_ = chat_repo.Message().Update(finalCtx, assistantMsg)

	sess.AgentStatus = "idle"
	sess.NeedsAttention = false
	sess.LastMessageAt = time.Now().UnixMilli()
	_ = s.persistSessionStatus(finalCtx, sess)
	logger.Ctx(finalCtx).Info("chat_svc: autonomous turn finalized",
		zap.Int64("sessionId", sessionID),
		zap.Int64("assistantMsgId", assistantMsg.ID),
		zap.String("agentStatus", sess.AgentStatus))

	// 后台命令在本自主轮才完成:它发起的 subagent_state 块住在更早的消息里,过不了
	// per-turn accumulator,只能定向重写持久化态。completedRef 为 nil(含 remote
	// 不携带 CompletedTask 的情形)时跳过。
	if completedRef != nil {
		if err := chat_repo.Message().FlipSubagentStatus(finalCtx, sessionID, completedRef.ToolUseID, completedRef.Status, completedRef.Summary); err != nil {
			logger.Ctx(finalCtx).Warn("chat_svc.driveAutonomousTurn: FlipSubagentStatus failed",
				zap.Int64("sessionId", sessionID),
				zap.String("toolUseId", completedRef.ToolUseID),
				zap.Error(err))
		}
	}

	final := chatMessageForEvent(sess, assistantMsg)
	s.emitter.Emit(finalCtx, stream, ChatStreamEvent{Kind: StreamDone, Message: final})
	s.emitter.Emit(finalCtx, stream, ChatStreamEvent{Kind: StreamClosed})
}

// drainAndDiscard 把事件 channel 抽干丢弃。关键不是丢内容,而是别让底层
// Session reader 因为出口 channel 没人 drain 而阻塞(活跃槽位不释放 → 后续用户
// turn 卡死)。失败路径用它兜底。
func drainAndDiscard(events <-chan agentruntime.Event) {
	for range events { //nolint:revive // 故意抽干丢弃
	}
}
