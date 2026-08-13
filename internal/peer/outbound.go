// outbound.go 是桌面端会话级的**出站**对端客户端（任务 10，R18/R19 的出站半边；
// 入站半边在 inbound.go）。传输层复用 server_svc 经账号中继产出的 *client.Client
// （DialDaemonRelay / DialDesktopRelay），本文件不新增任何传输：它只把既有的 wire
// 会话族（list / attach / pull / run / steer / answer / tool-permission）包装成
// 类型化方法，并提供 runtime.event 通知订阅——桌面 A 派活给桌面 B、以及按
// 「设备 → 会话列表 → 一条会话」接入 B 上既有会话，走的都是这一套。
package peer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/agentre-ai/agentre/internal/daemon/client"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// Outbound 是拨到另一台桌面端（或 agentred）的会话级客户端。调用方把
// server_svc.DialDesktopRelay 产出的已握手 *client.Client 交给它，peer 指纹
// 只在构造时记录为只读标识，不参与鉴权。
type Outbound struct {
	c  *client.Client
	fp string
}

// NewOutbound 包装一条已握手、已鉴权的对端中继连接。peerFingerprint 是该目标的
// 设备指纹（DialDesktopRelay 的目标），仅用于会话清单的 PeerFingerprint 语义。
func NewOutbound(c *client.Client, peerFingerprint string) *Outbound {
	return &Outbound{c: c, fp: peerFingerprint}
}

// PeerFingerprint 返回这条连接指向的对端指纹（会话合并键的一半，见 R20）。
func (o *Outbound) PeerFingerprint() string { return o.fp }

// Closed 在底层中继连接断开时关闭。
func (o *Outbound) Closed() <-chan struct{} { return o.c.Closed() }

// Close 释放中继连接。幂等。
func (o *Outbound) Close() error { return o.c.Close() }

// ListSessions 列出对端桌面上的全部会话（R19 / R4）。应答形状复用
// wire.SessionSummary，标题、状态、等待输入、最后活动与 Agent 身份齐全，
// 不存在轮 A 的退化形态。
func (o *Outbound) ListSessions(ctx context.Context) (*wire.SessionListResult, error) {
	var result wire.SessionListResult
	if err := o.c.Call(ctx, wire.MethodSessionList, struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Attach 把这条连接登记为某条远程会话的实时订阅者（R19 / R6）：此后对端把该会话
// 的 canonical 事件经 runtime.event 推回本连接，直到 Close。LatestSeq 是补齐历史
// 的高水位游标。
func (o *Outbound) Attach(ctx context.Context, params wire.SessionAttachParams) (wire.SessionAttachResult, error) {
	var result wire.SessionAttachResult
	if err := o.c.Call(ctx, wire.MethodSessionAttach, params, &result); err != nil {
		return result, err
	}
	return result, nil
}

// Pull 拉一页游标之后的 journaled 历史（R19 / R7）。桌面端的历史不回收，因此
// OldestSeq 恒为第一条（空历史为 0），与 agentred 的回收语义区分。
func (o *Outbound) Pull(ctx context.Context, params wire.SessionPullParams) (wire.SessionPullResult, error) {
	var result wire.SessionPullResult
	if err := o.c.Call(ctx, wire.MethodSessionPull, params, &result); err != nil {
		return result, err
	}
	return result, nil
}

// RunFresh 在对端桌面端上新建一条会话并跑首轮（R18）。wire 契约要求 SessionID
// 为正占位（对端按「本机查无此会话」判定建新会话），真正要新建的是它：AgentSyncID
// 是账号级 Agent 标识、Cwd 是本端上报的该机器项目路径。对端建出真会话后把真实 id
// 放进 RunAck.SessionID 返回。FreshSession 恒置 true：即便对端落库里有同号旧上下文
// 也不许续，杜绝派活撞上挂账残留。
func (o *Outbound) RunFresh(ctx context.Context, params wire.RunParams) (wire.RunAck, error) {
	params.FreshSession = true
	var ack wire.RunAck
	if err := o.c.Call(ctx, wire.MethodRun, params, &ack); err != nil {
		return ack, err
	}
	return ack, nil
}

// Steer 往已接入的远程会话发一条新消息（R19 / R9），走对端既有发送路径。
func (o *Outbound) Steer(ctx context.Context, params wire.SteerParams) error {
	return o.c.Call(ctx, wire.MethodSteer, params, &wire.OK{})
}

// SubmitAnswer 回答对端会话上挂起的用户提问（R10）。AlreadyHandled 报告同一待决策
// 已被别的端处理过；旧对端返回空对象时保持 false（task 5 的兼容语义）。
func (o *Outbound) SubmitAnswer(ctx context.Context, params wire.SubmitAnswerParams) (wire.PeerSessionControlResult, error) {
	var result wire.PeerSessionControlResult
	if err := o.c.Call(ctx, wire.MethodSubmitAnswer, params, &result); err != nil {
		return result, err
	}
	return result, nil
}

// SubmitToolPermission 决定对端会话上挂起的工具权限（R10），AlreadyHandled 语义
// 同 SubmitAnswer。
func (o *Outbound) SubmitToolPermission(ctx context.Context, params wire.SubmitToolPermissionParams) (wire.PeerSessionControlResult, error) {
	var result wire.PeerSessionControlResult
	if err := o.c.Call(ctx, wire.MethodSubmitToolPermission, params, &result); err != nil {
		return result, err
	}
	return result, nil
}

// HandleEvent 注册 runtime.event 通知订阅：对端把 attached 会话的 canonical 事件
// 帧推回本连接时，每帧解成 wire.EventFrame 交给 fn。每条连接只注册一次；返回值
// 错误会以 RPC 应答错误形式回给对端（对端忽略通知应答）。
func (o *Outbound) HandleEvent(fn func(wire.EventFrame) error) {
	o.c.Handle(wire.NotifyEvent, func(_ context.Context, raw json.RawMessage) (any, error) {
		var frame wire.EventFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			return nil, fmt.Errorf("peer.HandleEvent: decode event frame: %w", err)
		}
		return nil, fn(frame)
	})
}
