package chat_svc

import (
	"context"
	"encoding/json"
	"fmt"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/turn"
)

// startSubagentActivityWatcher 为某 claudecode 会话惰性启动一个 watcher goroutine,订阅
// runtime 的「后台 subagent 内部活动流」(run_in_background 的 subagent 在会话**空闲态**
// 自主产出的内部工具调用流),把每轮活动嵌套渲染回发起卡所在的消息并跨消息落库。
// 每会话只起一个(subagentActivityWatchers 去重);底层 SubagentActivity channel 在子进程
// evict / CloseSession 时 close,watcher 随之退出并清去重位。
//
// 并发约束(关键,镜像 startAutonomousWatcher):watcher 在 driveSubagentActivity 里
// **绝不持 chat 会话锁** drain。否则与 pkg/claudecode.Session 常驻 reader 死锁 ——
// 活动事件出口 channel 不被 drain → Session 活跃槽位不释放 → 用户 turn 卡死。
func (s *chatSvc) startSubagentActivityWatcher(sessionID int64, be *agent_backend_entity.AgentBackend, src agentruntime.SubagentActivitySource) {
	if sessionID <= 0 || be == nil || src == nil {
		return
	}
	if _, loaded := s.subagentActivityWatchers.LoadOrStore(sessionID, struct{}{}); loaded {
		return
	}
	beCopy := *be
	go func() {
		defer s.subagentActivityWatchers.Delete(sessionID)
		for act := range src.SubagentActivity(sessionID) {
			s.driveSubagentActivity(context.Background(), sessionID, &beCopy, act)
		}
	}()
}

// driveSubagentActivity 把一轮后台 subagent 内部活动落回**发起消息**(不新建消息):
//  1. 定位发起消息(含 subagent_state{parent_tool_call_id==act.ToolUseID} 的 assistant 消息);
//     找不到 → 抽干 act.Events 返回(别让 Session reader 阻塞)。
//  2. 经会话级旁路 emit StreamSubagentActivityStarted —— 把发起消息 id + per-turn 流名推给
//     前端,让它重开 per-turn 流把活动块嵌套渲染回 AgentSpawnCard。
//  3. 用 dispatcher drain act.Events(ToolCallHandler 已把 ParentToolCallID!="" 路由成
//     NestedToolUseBlock / NestedToolResultBlock,实时 stream 走发起卡的 per-turn 流)。
//  4. 收尾:取本轮新产出的嵌套块(ParentToolCallID==act.ToolUseID),序列化成 StoredBlock JSON
//     + 收集其 id,跨消息 AppendSubagentChildren 进发起消息;然后 emit StreamDone。
//
// 与 driveAutonomousTurn 不同:这是**空闲态后台活动**,不新建消息 / 不取 NextSeq /
// 不翻 session running —— 会话保持 idle。
func (s *chatSvc) driveSubagentActivity(ctx context.Context, sessionID int64, be *agent_backend_entity.AgentBackend, act agentruntime.SubagentActivity) {
	if act.ToolUseID == "" {
		drainAndDiscard(act.Events)
		return
	}

	launchMsg, err := chat_repo.Message().FindAssistantBySubagentToolUseID(ctx, sessionID, act.ToolUseID)
	if err != nil || launchMsg == nil {
		logger.Ctx(ctx).Warn("chat_svc: driveSubagentActivity launch message not found; draining events",
			zap.Int64("sessionId", sessionID), zap.String("toolUseId", act.ToolUseID), zap.Error(err))
		drainAndDiscard(act.Events)
		return
	}

	sess, err := chat_repo.Session().Find(ctx, sessionID)
	if err != nil || sess == nil {
		logger.Ctx(ctx).Warn("chat_svc: driveSubagentActivity load session failed; draining events",
			zap.Int64("sessionId", sessionID), zap.Error(err))
		drainAndDiscard(act.Events)
		return
	}

	stream := StreamName(sessionID, launchMsg.ID)
	logger.Ctx(ctx).Info("chat_svc: subagent activity started",
		zap.Int64("sessionId", sessionID),
		zap.Int64("launchMessageId", launchMsg.ID),
		zap.String("toolUseId", act.ToolUseID))
	// 会话级旁路:让前端定位发起卡并 openStream 订阅 per-turn 流。不插入新 assistant 行
	// (发起消息已存在),区别于 StreamAutonomousStarted。
	s.emitter.Emit(ctx, AutonomousStreamName(sessionID), ChatStreamEvent{
		Kind:            StreamSubagentActivityStarted,
		Stream:          stream,
		LaunchMessageID: launchMsg.ID,
		ToolUseID:       act.ToolUseID,
	})

	// 新建空 accumulator:只累积本轮活动产出的块,Finalize 即"本次新增"。发起消息既有的块
	// 不必 seed —— 它们已落库,本路径只追加新嵌套子块。
	acc := turn.New()
	dispEmit := &dispatcherEmitter{svc: s}
	turnCtx := s.newTurnContext(launchMsg, sess, stream, be.Type)
	for ev := range act.Events {
		if err := s.dispatcher.Apply(ctx, ev, acc, dispEmit, nil, turnCtx); err != nil {
			logger.Ctx(ctx).Warn("chat_svc: subagent activity dispatcher Apply failed",
				zap.String("eventType", fmt.Sprintf("%T", ev)), zap.Error(err))
		}
	}

	// 取本轮新产出的嵌套块(ParentToolCallID==act.ToolUseID),序列化 + 收集 tool_use id,
	// 跨消息追加进发起消息。finalCtx 去掉 cancel 但保留 DB 句柄 —— 已流出的内容必须落库。
	childBlocks, childIDs := subagentChildBlocks(acc.Finalize(), act.ToolUseID)
	finalCtx := context.WithoutCancel(ctx)
	if len(childBlocks) > 0 {
		childJSON, err := encodeStoredBlocks(childBlocks)
		if err != nil {
			logger.Ctx(finalCtx).Warn("chat_svc: driveSubagentActivity encode child blocks failed",
				zap.Int64("sessionId", sessionID), zap.String("toolUseId", act.ToolUseID), zap.Error(err))
		} else if err := chat_repo.Message().AppendSubagentChildren(finalCtx, sessionID, act.ToolUseID, childJSON, childIDs); err != nil {
			logger.Ctx(finalCtx).Warn("chat_svc: driveSubagentActivity AppendSubagentChildren failed",
				zap.Int64("sessionId", sessionID), zap.String("toolUseId", act.ToolUseID), zap.Error(err))
		}
	}

	logger.Ctx(finalCtx).Info("chat_svc: subagent activity finalized",
		zap.Int64("sessionId", sessionID),
		zap.Int64("launchMessageId", launchMsg.ID),
		zap.Int("childBlocks", len(childBlocks)))
	s.emitter.Emit(finalCtx, stream, ChatStreamEvent{Kind: StreamDone})
	s.emitter.Emit(finalCtx, stream, ChatStreamEvent{Kind: StreamClosed})
}

// subagentChildBlocks 从一轮活动的 Finalize 结果里挑出属于 parentToolUseID 的嵌套块
// (NestedToolUseBlock / NestedToolResultBlock),并收集 NestedToolUseBlock 的 id 作为
// childIDs(对齐 subagent_state.nested_tool_call_ids 反向索引,该数组索引的是 tool_use 块)。
func subagentChildBlocks(finalBlocks []cagoblocks.ContentBlock, parentToolUseID string) ([]cagoblocks.ContentBlock, []string) {
	var children []cagoblocks.ContentBlock
	var ids []string
	for _, b := range finalBlocks {
		switch nb := b.(type) {
		case *chatblocks.NestedToolUseBlock:
			if nb.ParentToolCallID != parentToolUseID {
				continue
			}
			children = append(children, nb)
			if nb.ID != "" {
				ids = append(ids, nb.ID)
			}
		case *chatblocks.NestedToolResultBlock:
			if nb.ParentToolCallID != parentToolUseID {
				continue
			}
			children = append(children, nb)
		}
	}
	return children, ids
}

// encodeStoredBlocks 把 ContentBlock 序列化成与 chat_messages.blocks_json 同构的
// StoredBlock-数组 JSON(信封 {type,data}),供 AppendSubagentChildren 解码追加。
// 与 chat_entity.Message.SetBlocks 走同一 cago EncodeAll 路径,保证 wire 形态一致。
func encodeStoredBlocks(bs []cagoblocks.ContentBlock) (string, error) {
	stored, err := cagoblocks.EncodeAll(bs)
	if err != nil {
		return "", err
	}
	buf, err := json.Marshal(stored)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}
