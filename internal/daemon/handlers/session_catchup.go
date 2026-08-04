// Package handlers — session_catchup.go 实现断连重连的补齐族 RPC:会话清单 / 增量
// 拉取 / 待决策查询 / 显式接管。它是客户端重连后的第一站。
//
// 与 runtime.go 的三条区别决定了它为什么是**独立的一族**而不是 RuntimeHandlers 的
// 几个新方法:
//
//  1. 它是 daemon 级的,不随连接生灭。RuntimeHandlers 是 per-connection 构造的,它的
//     内存会话表在重连后是空的 —— 补齐要是按那张表解会话,重连的客户端就永远拿不到
//     自己的会话,而重连正是这一族存在的全部理由。这里一律按持久化的会话行解。
//  2. 它只读存储 + 只读实时状态,不启动、不推进、不中止任何一轮执行。唯一的例外是
//     显式接管,而接管改的是「通知推给谁」,不是会话本身(且由 daemon.go 在受理后
//     执行,见 MethodSessionAttach 的注册)。
//  3. 它的范围一律是调用方自己的对端(R16)。对端指纹取自那条连接的鉴权状态,不从
//     参数读 —— 参数里的对端标识等于让任何已配对设备点名读别人的会话。
package handlers

import (
	"context"
	"fmt"
	"strconv"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// SessionCatchupDeps 是 SessionCatchupHandlers 的显式构造入参。
type SessionCatchupDeps struct {
	// Sessions 读会话行(身份 / 元数据 / 生命周期)。
	Sessions SessionQueryPort
	// Journal 读通知日志(增量拉取与最新 seq)。
	Journal JournalReaderPort
	// RuntimeFor 解 backend 类型 → 本进程里那个 runtime 单例,用来问它此刻有哪些
	// 阻塞中的 waiter。留空取 agentruntime.RuntimeFor。
	RuntimeFor func(agent_backend_entity.BackendType) agentruntime.Runtime
}

// SessionCatchupHandlers 实现补齐族的四个 RPC。无状态:它的全部事实要么在存储里,
// 要么在 backend runtime 的内存里。
type SessionCatchupHandlers struct {
	deps SessionCatchupDeps
}

// NewSessionCatchupHandlers 组装补齐族 handler。
func NewSessionCatchupHandlers(deps SessionCatchupDeps) *SessionCatchupHandlers {
	if deps.RuntimeFor == nil {
		deps.RuntimeFor = agentruntime.RuntimeFor
	}
	return &SessionCatchupHandlers{deps: deps}
}

// List 返回调用方那个对端在本 daemon 上的全部会话(R16)。
//
// 每条会话的「最新 seq」取自通知日志的 MAX(seq) —— 唯一真相源。会话一条通知都还没
// 发出时报 0。「是否正在等待输入」现算,见 waitingForInput。
func (h *SessionCatchupHandlers) List(ctx context.Context) (wire.SessionListResult, error) {
	peer := peerFingerprint(ctx)
	rows, err := h.deps.Sessions.List(ctx, peer)
	if err != nil {
		// 报错而不是回一份空清单:空清单与「这台 daemon 上没有你的会话」无法区分,
		// 客户端会据此把还活着的会话当成已消失。
		return wire.SessionListResult{}, fmt.Errorf("list sessions: %w", err)
	}
	latest, err := h.deps.Journal.LatestSeqByPeer(ctx, peer)
	if err != nil {
		return wire.SessionListResult{}, fmt.Errorf("read latest seq: %w", err)
	}
	out := wire.SessionListResult{Sessions: make([]wire.SessionSummary, 0, len(rows))}
	for _, row := range rows {
		sid, err := strconv.ParseInt(row.PeerSessionID, 10, 64)
		if err != nil {
			// 会话 id 是客户端的本地自增主键,解不出来说明这一行不是本协议写的。
			// 跳过而不是整份清单失败 —— 一条坏行不该让客户端连自己的会话都看不到。
			continue
		}
		out.Sessions = append(out.Sessions, wire.SessionSummary{
			SessionID:       sid,
			AgentID:         row.AgentID,
			Cwd:             row.Cwd,
			BackendType:     row.BackendType,
			LifecycleState:  row.LifecycleState,
			WaitingForInput: h.waitingForInput(ctx, row, sid),
			LatestSeq:       latest[row.PeerSessionID],
		})
	}
	return out, nil
}

// Pull 按游标取回该会话其后的通知(seq 升序)。
//
// 拉取不校验会话归属:日志行本身以 (对端, 会话) 为键,别的对端拉同一个会话 id 只会
// 拿到自己那条会话的日志(通常是空),跨对端泄漏在 SQL 层就不成立。
func (h *SessionCatchupHandlers) Pull(ctx context.Context, p wire.SessionPullParams) (wire.SessionPullResult, error) {
	peer := peerFingerprint(ctx)
	sid := strconv.FormatInt(p.SessionID, 10)
	rows, hasMore, err := h.deps.Journal.ListSince(ctx, peer, sid, p.Cursor, clampPullLimit(p.Limit))
	if err != nil {
		return wire.SessionPullResult{}, fmt.Errorf("pull notifications: %w", err)
	}
	// 空页保持游标不变:回退到 0 会让客户端把整段日志重放一遍。
	out := wire.SessionPullResult{Cursor: p.Cursor, HasMore: hasMore}
	out.Notifications = make([]wire.JournaledNotification, 0, len(rows))
	for _, row := range rows {
		out.Notifications = append(out.Notifications, wire.JournaledNotification{
			Seq:    row.Seq,
			Method: row.Method,
			Params: row.Payload,
		})
		out.Cursor = row.Seq
	}
	return out, nil
}

// PendingWaiters 返回该会话此刻仍在阻塞的全部待决策(R7)。
//
// 快照来自 daemon 内存里的 waiter,不来自数据库;会话行只用来解「这条会话是不是调用方
// 的、跑的是哪个 backend」。不属于调用方的会话、未实现审批协议的 backend,都回空列表
// 而不是报错 —— 两者都是正常情况,报错会让客户端误判为故障。
func (h *SessionCatchupHandlers) PendingWaiters(ctx context.Context, p wire.SessionPendingWaitersParams) (wire.SessionPendingWaitersResult, error) {
	row, err := h.findOwnSession(ctx, p.SessionID)
	if err != nil {
		return wire.SessionPendingWaitersResult{}, err
	}
	if row == nil {
		return wire.SessionPendingWaitersResult{}, nil
	}
	snap := h.pendingWaiters(ctx, *row, p.SessionID)
	return wire.SessionPendingWaitersResult{
		ToolPermissions:  snap.ToolPermissions,
		AskUserQuestions: snap.AskUserQuestions,
	}, nil
}

// Attach 是显式接管的受理侧:校验这条会话确实是调用方的、且还接得回去,然后交回它
// 接着补齐要用的生命周期状态、backend 类型与此刻的最新 seq。
//
// 把推送目标真正改到这条连接上的动作在 daemon.go 的注册处,且只在本方法**成功返回
// 之后**执行 —— 被拒的接管不得改变任何东西。
func (h *SessionCatchupHandlers) Attach(ctx context.Context, p wire.SessionAttachParams) (wire.SessionAttachResult, error) {
	row, err := h.findOwnSession(ctx, p.SessionID)
	if err != nil {
		return wire.SessionAttachResult{}, err
	}
	if row == nil {
		// 接管会改变通知的推送目标,允许接管别人的(或不存在的)会话等于把别人的事件流
		// 引到自己的连接上。
		return wire.SessionAttachResult{}, agentruntime.ErrSessionNotFound
	}
	if row.LifecycleState == wire.SessionLifecycleInterrupted {
		// R10「不可续跑」:那一轮的子进程随上一个 daemon 进程消亡了,接回实时流等于让
		// 客户端对着一条永远不会再产出任何东西的会话无限期等下去。历史仍可 Pull。
		return wire.SessionAttachResult{}, agentruntime.ErrNoActiveTurn
	}
	latest, err := h.deps.Journal.LatestSeq(ctx, peerFingerprint(ctx), row.PeerSessionID)
	if err != nil {
		return wire.SessionAttachResult{}, fmt.Errorf("read latest seq: %w", err)
	}
	return wire.SessionAttachResult{
		SessionID:      p.SessionID,
		BackendType:    row.BackendType,
		LifecycleState: row.LifecycleState,
		LatestSeq:      latest,
	}, nil
}

// findOwnSession 取调用方自己那个对端名下的一条会话;不是它的(或不存在)返回
// (nil, nil)。这是 R16 在读侧的唯一入口,补齐族的每个按会话的方法都必须过它。
func (h *SessionCatchupHandlers) findOwnSession(ctx context.Context, sessionID int64) (*SessionRecord, error) {
	row, err := h.deps.Sessions.Find(ctx, peerFingerprint(ctx), strconv.FormatInt(sessionID, 10))
	if err != nil {
		return nil, fmt.Errorf("find session: %w", err)
	}
	return row, nil
}

// waitingForInput 现算「这条会话是不是正在等用户操作」:问 backend 此刻有没有阻塞中的
// waiter(R11)。它永远不落库 —— 落库的等待标志会活过 daemon 重启,变成一个没人能回答
// 的问题(那一轮的子进程已经不在了)。
func (h *SessionCatchupHandlers) waitingForInput(ctx context.Context, row SessionRecord, sessionID int64) bool {
	snap := h.pendingWaiters(ctx, row, sessionID)
	return len(snap.ToolPermissions) > 0 || len(snap.AskUserQuestions) > 0
}

// pendingWaiters 问该会话的 backend 要一份 waiter 快照。backend 没注册、或没实现审批
// 协议时回零值 —— 未实现者返回空列表而非报错是 R7 明写的。
//
// 问的是**按对端隔离过的会话键**(runtimeSID),不是客户端报的裸数字:backend 的 waiter
// 表是进程内一份、只按会话 id 索引,而会话 id 各客户端本地自增、必然重号。拿裸 id 去问,
// 拿回来的可能是别的对端那条同号会话的 requestID / 工具名 / 完整工具入参 —— 交出去等于
// 泄漏别人的审批载荷,对方还能照着 requestID 替人回答(R16)。
//
// 中断态会话一律不问 backend:那一轮的子进程随上一个 daemon 进程消亡了(R10),它不可能
// 还有活的 waiter,任何答案都只会是别人的。
func (h *SessionCatchupHandlers) pendingWaiters(ctx context.Context, row SessionRecord, sessionID int64) agentruntime.WaiterSnapshot {
	if row.LifecycleState == wire.SessionLifecycleInterrupted {
		return agentruntime.WaiterSnapshot{}
	}
	rt := h.deps.RuntimeFor(agent_backend_entity.BackendType(row.BackendType))
	if rt == nil {
		return agentruntime.WaiterSnapshot{}
	}
	lister, ok := rt.(agentruntime.WaiterLister)
	if !ok {
		return agentruntime.WaiterSnapshot{}
	}
	return lister.PendingWaiters(ctx, runtimeSID(ctx, sessionID))
}

// clampPullLimit 把客户端报的单页条数收进 daemon 的上限。
func clampPullLimit(limit int) int {
	if limit <= 0 {
		return wire.DefaultSessionPullLimit
	}
	if limit > wire.MaxSessionPullLimit {
		return wire.MaxSessionPullLimit
	}
	return limit
}
