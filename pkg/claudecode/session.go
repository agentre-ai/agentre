package claudecode

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cago-frame/agents/provider"
)

// claudeStartupCheckTimeout 是 OpenSession 在 spawn 完之后等子进程"是不是立刻
// 自杀"的窗口。健康 CLI 启动后只会阻塞读 stdin（几毫秒内不会自然 exit），坏
// CLI（resume 不存在 / 二进制路径错）会在百毫秒内写 stderr + exit。200ms 留出
// 慢机的余量，错过这个窗口的早退由 Turn / 0-frame fallback 兜底（也走 ExitErr）。
var claudeStartupCheckTimeout = 200 * time.Millisecond

// Session 是一个常驻的 claude 子进程。多个 Turn 复用同一个 stdin/stdout，
// 适合"每个 chat session 一个子进程"的部署形态——避免每轮 spawn 的 cold start，
// 也让 hooks/--settings 注入只做一次。
//
// 调用约束：
//   - Turn 串行调用；上一个 Turn 的事件 channel 必须完全 drain（或上下文取消）
//     之后再调下一个 Turn。
//   - Close 之后 Turn 必须返回错误。
//
// 与 Stream 的区别：Stream 是一次性会话（spawn+drain+exit），Session 跨多轮。
// pkg 内两套并存：probe / 简单一次性问答继续用 Stream；chat_svc 这类需要长会话
// 的走 Session。
type Session struct {
	proc *process

	scanner *bufio.Scanner

	sessionID string // 由 system init 帧填入；首次 Turn 后稳定
	model     string // 由 system init 帧 model 字段填入；新一轮如果换 model CLI 会重新发 init

	// turnMu 串行化 user Turn 生命周期 —— 上一个 Turn 的事件 channel 收尾(done)之前
	// 拒绝下一个 Turn 启动。Turn 的 waiter goroutine 持有该锁直到本轮 done。
	// 自主轮不走 turnMu(它没有对应的 Turn 调用)。
	turnMu sync.Mutex
	// stdinMu 串行化对 proc.stdin 的写 —— Turn 的 user frame 写完就释放，
	// Interrupt 的 control_request 写时再单独获取。**绝不**跨 turn 生命周期持有。
	stdinMu sync.Mutex
	closed  bool // Close 已调用，stdinMu 保护

	// control_request → 等 control_response 的 channel registry。
	// key = request_id（Interrupt 生成的 v4 UUID）；parseLine 看到 control_response
	// 帧时按 request_id 路由。
	ctrlMu      sync.Mutex
	ctrlPending map[string]chan controlResponse

	// lastErr 来自 routeUntilResult / Close 路径检测到的退出错误（typically
	// proc.exitErrIfDone），ExitErr 优先读它再 fallback 到 proc。原子的 *error
	// 兼容多 goroutine 读写。
	lastErr atomic.Value // holds *error

	// lastAssistantUsage 跟踪当前 turn 内最后一帧 assistant.message.usage。
	// result.usage 是整轮所有内部 API call 用量的累加，不能直接拿来当"当前
	// 上下文占用"（工具循环越多越虚高）。EventDone 优先吐 lastAssistantUsage
	// 反映模型这一刻看到的输入大小；缺省 fallback 到 result.usage。
	// 每个 result 帧吐完 EventDone 后置 nil，避免跨 turn 串味。
	// 仅 readLoop(单 goroutine)读写,无需额外锁。
	lastAssistantUsage *rawUsage

	// —— 常驻 demux reader 状态(sinkMu 保护)——
	// readLoop 占住 scanner 整个子进程生命周期,把每帧 demux 到「当前活跃轮」的 ch。
	// 一刻只有一个活跃轮(CLI 串行 emit,每轮 result 收尾)。归属规则:某轮以「后台型
	// task_notification」开头 → 自主轮(经 autoCh 吐出);否则按 FIFO 取一个 pendingTurns
	// 里等待的 user Turn。
	sinkMu       sync.Mutex
	active       *activeTurn      // 当前正在投递帧的轮;nil = 轮间空闲
	pendingTurns chan *activeTurn // 已写 stdin、等待其帧到达的 user Turn(FIFO)
	autoCh       chan *AutoTurn   // AutonomousTurns() 返回的 channel;子进程退出时 close
}

// controlResponse 是 control_response 帧 response 字段的最小子集。
// subtype 通常是 "success" / "error"。
type controlResponse struct {
	Subtype string `json:"subtype"`
	Error   string `json:"error,omitempty"`
}

// OpenSession spawns a persistent claude subprocess in stream-json mode.
//
// 与 Client.Stream 的差异：不写任何 user frame，留给后续 Turn 触发。返回时
// 进程已启动但还没有 frame 流过——首个 frame 由首个 Turn 的 user input 触发。
func (c *Client) OpenSession(ctx context.Context, opts ...RunOption) (*Session, error) {
	spec := runSpec{
		model:                c.model,
		systemPrompt:         c.systemPrompt,
		permissionMode:       c.permissionMode,
		sessionID:            c.sessionID,
		settings:             c.settings,
		mcpConfig:            c.mcpConfig,
		allowedTools:         c.allowedTools,
		effort:               c.effort,
		permissionPromptTool: c.permissionPromptTool,
	}
	for _, o := range opts {
		o(&spec)
	}
	if spec.resumeSessionAtUUID != "" && !spec.forkSession {
		return nil, errors.New("claudecode: ResumeSessionAt requires ForkSession (would destructively rewind source session)")
	}

	p, err := c.spawn(ctx, processSpec{
		binary: c.binary,
		args:   buildArgs(spec),
		cwd:    c.cwd,
		env:    c.env,
	})
	if err != nil {
		return nil, err
	}

	// 健康检查窗口：claude CLI 命中 "No conversation found"、二进制路径错、
	// 启动参数被拒等启动期失败都会几十毫秒内 exit + 写 stderr。这里 200ms 内
	// 等 reaper goroutine close exit channel，命中就把分类后的退出错误返出去，
	// 避免后续 Turn 拿到 broken pipe + 用户看不到真错。
	select {
	case <-p.exit:
		exitErr := p.exitErrIfDone()
		if exitErr == nil {
			// exit 0 但根本没 frame —— 不太可能但兜底一下。
			exitErr = errors.New("claudecode: subprocess exited during OpenSession without emitting init frame")
		}
		return nil, exitErr
	case <-time.After(claudeStartupCheckTimeout):
		// 进程仍存活 → 视为健康，由后续 Turn 接管 stdout。
	case <-ctx.Done():
		// 调用方取消（极少见）→ 关 stdin 触发 CLI 退出，再返 ctx.Err。
		_ = p.stdin.Close()
		return nil, ctx.Err()
	}

	sc := bufio.NewScanner(p.stdout)
	buf := make([]byte, 0, 64<<10)
	sc.Buffer(buf, maxFrameBytes)

	s := &Session{
		proc:         p,
		scanner:      sc,
		sessionID:    spec.sessionID,
		pendingTurns: make(chan *activeTurn, 4),
		autoCh:       make(chan *AutoTurn, 8),
	}
	go s.readLoop()
	return s, nil
}

// SessionID 返回 claude 报告的 session_id。Open 后到首个 Turn 完成之前可能为空——
// 优先取 system init / result 帧里的 session_id；调用方如果只关心"我们传给 CLI
// 的那个 UUID"，应直接保留 WithSessionID 的入参。
func (s *Session) SessionID() string { return s.sessionID }

// Turn 写一条 user frame，返回本轮的事件 channel。channel 在 result 帧到达后关闭。
//
// 并发约束：同一时刻只能有一个 Turn 在飞。第二个 Turn 调用会阻塞直到上一个 Turn
// 的事件 channel 被完全 drain（result 帧出现 → goroutine 关 channel → 释放 mu）。
func (s *Session) Turn(ctx context.Context, prompt string, images ...Image) (<-chan Event, error) {
	s.turnMu.Lock() // 抢 user turn slot —— 上一个 turn 没收尾(done)不让进
	s.stdinMu.Lock()
	if s.closed {
		s.stdinMu.Unlock()
		s.turnMu.Unlock()
		return nil, errors.New("claudecode: session closed")
	}

	// images 非空时 user frame 携带 base64 image content block(图片在前,文本在后)。
	enc, err := buildUserFrame(prompt, images)
	if err != nil {
		s.stdinMu.Unlock()
		s.turnMu.Unlock()
		return nil, err
	}
	if _, err := fmt.Fprintf(s.proc.stdin, "%s\n", enc); err != nil {
		s.stdinMu.Unlock()
		s.turnMu.Unlock()
		// broken pipe 几乎一定意味着子进程已经死了 —— 这种情况下用 reaper
		// 抓到的分类后退出错误（含 ErrSessionNotFound）替换原始 err，让上层
		// 能用 errors.Is 判定 + 给用户人话提示。
		if exitErr := s.proc.exitErrIfDone(); exitErr != nil {
			s.rememberExitErr(exitErr)
			return nil, exitErr
		}
		return nil, err
	}
	s.stdinMu.Unlock() // stdin 写完立刻释放，给 Interrupt 让路

	// 注册到 FIFO,等 readLoop 在本轮帧到达时取走。stdin 写在前、push 在后:CLI 的
	// 响应帧一定晚于这次本地 push;即便极端调度下 reader 先读到首帧,它在 currentTurn
	// 里阻塞 <-pendingTurns 等这次 push,不会错配。
	at := newActiveTurn(false)
	s.pendingTurns <- at

	go func() {
		defer s.turnMu.Unlock() // 本轮 done(result/EOF)后再放 turn 锁
		select {
		case <-at.done:
		case <-ctx.Done():
			// 消费方放弃:标记 abandon 让 reader 停投递、丢弃余帧;等 reader 真正
			// 读到本轮 result(或子进程 EOF)关 done 后再放 turnMu,避免下一轮帧串味。
			s.markAbandoned(at)
			<-at.done
		}
	}()
	return at.ch, nil
}

// AutonomousTurns 返回 CLI 自主续轮(后台任务完成续轮)的 channel。子进程退出
// (scanner EOF / Close)时 close。消费方 range 它,每个 *AutoTurn 是一轮独立的
// 事件流。无消费方时缓冲(8)兜底,满后 readLoop 在投递下一轮时阻塞(back-pressure)。
func (s *Session) AutonomousTurns() <-chan *AutoTurn { return s.autoCh }

// readLoop 占住 scanner 整个子进程生命周期,把每帧 demux 到当前活跃轮。
// 归属:某轮以「后台型 task_notification」开头 → 自主轮(经 AutonomousTurns 吐出);
// 否则按 FIFO 取一个 pendingTurns 里等待的 user Turn。每轮以 result 收尾。
//
// scanner 退出(EOF / 错误)= 子进程死亡:snapshot 真错给 Session.ExitErr 用,
// 再收尾所有未决轮 + close autoCh。
func (s *Session) readLoop() {
	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		events, done := s.parseLine(line) // 同时把 control_response dispatch 给 ctrlPending
		var f rawFrame
		_ = json.Unmarshal(line, &f) // 仅供归属判定(后台型 task_notification)
		s.route(f, events, done)
	}
	if exitErr := s.proc.exitErrIfDone(); exitErr != nil {
		s.rememberExitErr(exitErr)
	}
	s.shutdownReader()
}

// route 把一帧的事件投给当前活跃轮;done 时收尾该轮。
func (s *Session) route(f rawFrame, events []Event, done bool) {
	at := s.currentTurn(f)
	if at == nil {
		// 自主轮起始标记(已建立 active 并吐 autoCh),或空闲态的非 turn 帧
		// (control_response / status):均无归属轮,本帧事件不下发。
		return
	}
	s.feed(at, events)
	if done {
		s.finishActiveTurn(at)
	}
}

// currentTurn 返回当前活跃轮;轮间(active==nil)时按归属规则建立新轮:
//   - 后台型 task_notification → 自主轮,经 autoCh 吐出,返回 nil(调用方丢弃起始标记)。
//   - 非 turn 帧(control_response / 空闲 status)→ 返回 nil,不认领排队的 user Turn;
//     否则读循环会被这些会话级帧卡死在 <-pendingTurns 上,后续 Turn / 自主轮再也读不
//     到 stdout(见 isNonTurnFrame)。
//   - 否则 → 取 FIFO 队首 user Turn(stdin 已写 → push 紧随,阻塞极短)。
func (s *Session) currentTurn(f rawFrame) *activeTurn {
	s.sinkMu.Lock()
	if s.active != nil {
		at := s.active
		s.sinkMu.Unlock()
		return at
	}
	if isBackgroundTaskNotification(f) {
		at := newActiveTurn(true)
		s.active = at
		s.sinkMu.Unlock()
		s.autoCh <- &AutoTurn{
			Events:    at.ch,
			SessionID: s.sessionID,
			Trigger:   triggerBackgroundTask,
			CompletedTask: &CompletedBackgroundTask{
				ToolUseID: f.ToolUseID,
				TaskID:    f.TaskID,
				Status:    f.Status,
				Summary:   f.Summary,
			},
		}
		return nil
	}
	if isNonTurnFrame(f) {
		s.sinkMu.Unlock()
		return nil // 会话级帧,空闲到达无归属轮:不认领 user Turn slot
	}
	s.sinkMu.Unlock()
	at := <-s.pendingTurns // user 轮起始:取队首(对应的 Turn 已 push)
	s.sinkMu.Lock()
	s.active = at
	s.sinkMu.Unlock()
	return at
}

// isNonTurnFrame 判定一帧是否「不归属任何一轮」—— 即便在轮间(空闲)到达,也不该认领
// 一个排队的 user Turn。三类:
//   - control_response:control_request(Interrupt / SetPermissionMode)的回执,已在
//     parseLine 阶段按 request_id dispatch 给等待者,不携带 turn 事件。
//   - system{subtype:"status"}:会话级状态推送(permissionMode / 运行态),从不作为某
//     一轮的起始帧 —— 轮内到达由 active 轮承接(currentTurn 在 active!=nil 时已先返回),
//     空闲到达则无归属轮,其事件随 route 的 nil 返回被丢弃(set_permission_mode 的回执
//     已由 SetPermissionMode 调用方拿到,主动切 mode 不依赖这条空闲 status)。
//   - system{subtype:"task_started"/"task_updated"/"task_progress"}:后台任务(及
//     subagent)生命周期的状态推送。真 CLI 2.1.162 在后台任务完成、自主续轮的
//     task_notification 之前先吐一帧 task_updated(状态 patch);它空闲到达时既非
//     后台 task_notification 也非 status,旧逻辑会把读循环卡死在 <-pendingTurns,
//     后续 task_notification / 自主续轮永远读不到(sess-429「续不上对话」复发)。
//     这些状态帧从不作为一轮的起始帧:轮内到达由 active 轮承接,空闲到达直接丢弃。
//     注意后台型 task_notification 不在此列 —— 它正是自主轮的起始标记(见
//     isBackgroundTaskNotification),由 currentTurn 在本判定之前优先处理。
func isNonTurnFrame(f rawFrame) bool {
	if f.Type == "control_response" {
		return true
	}
	if f.Type != "system" {
		return false
	}
	switch f.Subtype {
	case "status", "task_started", "task_updated", "task_progress":
		return true
	}
	return false
}

// feed 把事件投给 at.ch;at 已被消费方放弃(abandon)时丢弃余帧,避免 reader 阻塞。
func (s *Session) feed(at *activeTurn, events []Event) {
	for _, ev := range events {
		select {
		case at.ch <- ev:
		case <-at.abandon:
			return
		}
	}
}

// finishActiveTurn 收尾一轮:清 active 槽 + close ch(唤醒消费方 range)+ close done(唤醒 waiter)。
func (s *Session) finishActiveTurn(at *activeTurn) {
	s.sinkMu.Lock()
	if s.active == at {
		s.active = nil
	}
	s.sinkMu.Unlock()
	close(at.ch)
	close(at.done)
}

// markAbandoned 标记某轮消费方已放弃(Turn 的 ctx 取消)。close abandon 让 feed 停
// 投递;done 仍由 readLoop 在 result/EOF 时 close。幂等。
func (s *Session) markAbandoned(at *activeTurn) {
	select {
	case <-at.abandon:
	default:
		close(at.abandon)
	}
}

// shutdownReader 在 scanner 退出后收尾:close 当前活跃轮 + 排空 pendingTurns(让
// 各自 Turn 的 waiter 解除阻塞)+ close autoCh。
func (s *Session) shutdownReader() {
	s.sinkMu.Lock()
	at := s.active
	s.active = nil
	s.sinkMu.Unlock()
	if at != nil {
		close(at.ch)
		close(at.done)
	}
	for {
		select {
		case p := <-s.pendingTurns:
			close(p.ch)
			close(p.done)
		default:
			close(s.autoCh)
			return
		}
	}
}

// rememberExitErr 写到 lastErr，多次写以第一次为准（首因优先 —— 后续路径多半
// 是 broken pipe 这种 derivative error）。
func (s *Session) rememberExitErr(err error) {
	if err == nil {
		return
	}
	s.lastErr.CompareAndSwap(nil, &err)
}

// ExitErr 子进程已退出时返其分类后的退出错误（含 ErrSessionNotFound）；
// 还活着或 exit 0 + 无 stderr 命中 → 返 nil。
//
// 优先读 Session.lastErr（首次检测点的真错），fallback 到 proc.exitErrIfDone
// 拿当前快照。runtime 层 0-frame fallback 用它替换通用 StopErr 消息。
func (s *Session) ExitErr() error {
	if v := s.lastErr.Load(); v != nil {
		if pe, ok := v.(*error); ok && pe != nil {
			return *pe
		}
	}
	if s.proc == nil {
		return nil
	}
	return s.proc.exitErrIfDone()
}

// parseLine 是 frameDecoder.decodeLine 的"无状态副本"——session 多轮场景下不能
// 用 frameDecoder.done 把 reader 钉死。返回 (events, isResult)。
func (s *Session) parseLine(line []byte) ([]Event, bool) {
	var f rawFrame
	if err := json.Unmarshal(line, &f); err != nil {
		return nil, false
	}
	switch f.Type {
	case "system":
		if f.Subtype == "init" {
			if f.SessionID != "" {
				s.sessionID = f.SessionID
			}
			if f.Model != "" {
				s.model = f.Model
				return []Event{{Kind: EventInit, SessionID: s.sessionID, Model: f.Model}}, false
			}
		}
		if ev, ok := parseSystemTask(f, s.sessionID); ok {
			return []Event{ev}, false
		}
		// system{subtype:"status",...} — CLI 通报会话级状态。两个独立维度,允许同一帧同时填:
		//   - permissionMode: mode 变化 (主动 set_permission_mode 回执 / 被动 ExitPlanMode 切换)
		//   - status: 运行态字符串 ("compacting" 等)
		// 两者都空 → 静默忽略 (前向兼容,可能是 CLI 引入了新字段)。
		if f.Subtype == "status" {
			return statusEvents(s.sessionID, f), false
		}
		return nil, false
	case "assistant":
		events, usage := parseAssistantContentWithUsage(f.Message, s.sessionID, f.ParentToolUseID)
		// 仅记录主 agent 帧的 usage：parent_tool_use_id != "" 的帧来自 Task/Agent
		// subagent 内部 API call，那是独立 Anthropic 会话（自己的 system prompt /
		// context window），用它的用量覆盖主 agent 的会让进度条骤降到 subagent 的
		// 小上下文，明显错。
		//
		// 额外:--include-partial-messages 模式下 CLI 把每次 API call 的真实 usage
		// 放在 stream_event message_delta 上,随后这条 merged assistant 帧的
		// usage 字段是 message_start 状态的 0 拷贝。zero-clobber guard:全 0 视为
		// "没新信息",不要覆盖已经从 stream_event 抓到的真值。
		if usage != nil && f.ParentToolUseID == "" && !isZeroUsage(usage) {
			s.lastAssistantUsage = usage
			// 每个主 agent 帧附加一条 EventUsage，让上层在 turn 内实时刷新
			// 「已用上下文」。EventDone 仍按 resolveDoneUsage 兜底，不变。
			events = append(events, Event{
				Kind:      EventUsage,
				SessionID: s.sessionID,
				Usage: provider.Usage{
					PromptTokens:        usage.InputTokens,
					CompletionTokens:    usage.OutputTokens,
					CachedTokens:        usage.CacheReadInputTokens,
					CacheCreationTokens: usage.CacheCreationInputTokens,
				},
			})
		}
		return events, false
	case "stream_event":
		return s.parseStreamEvent(f), false
	case "user":
		return parseUserContent(f.Message, s.sessionID, f.ParentToolUseID, f.ToolUseResult), false
	case "result":
		if f.SessionID != "" {
			s.sessionID = f.SessionID
		}
		ev := Event{Kind: EventDone, SessionID: s.sessionID, Model: s.model}
		ev.Usage = resolveDoneUsage(s.lastAssistantUsage, f.Usage)
		// 当前 turn 结束，下一轮重新累积 lastAssistantUsage。
		s.lastAssistantUsage = nil
		return []Event{ev}, true
	case "control_response":
		// 路由给在 ctrlPending 上等的 Interrupt 调用者；不产生 Event。
		// 解 request_id 失败 / 没有匹配的等待者 → 直接丢，下游 Interrupt 会
		// 因 ctx 超时返回 ctx.Err。
		s.dispatchControlResponse(line)
		return nil, false
	case "control_request":
		// claude → host：can_use_tool 许可请求。把 ControlRequestEvent 塞到
		// 主 Event 流，runtime 层异步处理（解析 input、emit EventAskUserQuestion、
		// 等用户答完后 Session.RespondToControl 回写）。
		if ev, ok := parseControlRequest(line); ok {
			return []Event{{Kind: EventControlRequest, SessionID: s.sessionID, ControlRequest: ev}}, false
		}
		return nil, false
	}
	return nil, false
}

// dispatchControlResponse 按 request_id 投递 control_response 给 ctrlPending
// 上的等待者。frame schema（实测 Claude CLI 2.1.145 + SDK 0.1.77，
// request_id 在 response **内层**，不是顶层）：
//
//	{"type":"control_response","response":{"subtype":"success"|"error","request_id":"...","error":"..."}}
func (s *Session) dispatchControlResponse(line []byte) {
	resp, reqID, ok := parseControlResponse(line)
	if !ok {
		return
	}
	s.ctrlMu.Lock()
	ch := s.ctrlPending[reqID]
	s.ctrlMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- resp:
	default:
	}
}

// parseControlResponse 拆 control_response 帧；返回 (response payload, request_id, ok)。
// 与真 CLI 对齐：request_id 在 response 内层。空 request_id 视为无效。
func parseControlResponse(line []byte) (controlResponse, string, bool) {
	var f struct {
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Error     string `json:"error,omitempty"`
		} `json:"response"`
	}
	if err := json.Unmarshal(line, &f); err != nil || f.Response.RequestID == "" {
		return controlResponse{}, "", false
	}
	return controlResponse{Subtype: f.Response.Subtype, Error: f.Response.Error}, f.Response.RequestID, true
}

// Interrupt 写一帧 control_request{subtype:"interrupt"} 让 CLI 软中断当前 turn。
// CLI 会回一帧 control_response 标 success / error，本方法阻塞等这条回执（或 ctx
// 取消）。中断成功后 CLI 还会发一个 result 帧（subtype 通常是 "interrupted" /
// "error_during_execution"）让正在 drain 的 Turn 自然返 done —— **子进程保留**，
// 下一轮可以直接复用同一个 Session。
//
// 调用约束：可以和 Turn 并发调用（stdinMu 只在写帧期间持有，turnMu 不参与）。
// Close 之后调返错。同一个 Session 并发多次 Interrupt 不冲突（各自 request_id）
// 但意义不大 —— CLI 一时刻只一个 turn。
func (s *Session) Interrupt(ctx context.Context) error {
	s.stdinMu.Lock()
	if s.closed {
		s.stdinMu.Unlock()
		return errors.New("claudecode: session closed")
	}
	reqID := newControlRequestID()
	ch := make(chan controlResponse, 1)
	s.ctrlMu.Lock()
	if s.ctrlPending == nil {
		s.ctrlPending = map[string]chan controlResponse{}
	}
	s.ctrlPending[reqID] = ch
	s.ctrlMu.Unlock()

	frame := map[string]any{
		"type":       "control_request",
		"request_id": reqID,
		"request":    map[string]any{"subtype": "interrupt"},
	}
	enc, mErr := json.Marshal(frame)
	if mErr != nil {
		s.stdinMu.Unlock()
		s.forgetControlRequest(reqID)
		return mErr
	}
	if _, err := fmt.Fprintf(s.proc.stdin, "%s\n", enc); err != nil {
		s.stdinMu.Unlock()
		s.forgetControlRequest(reqID)
		return err
	}
	s.stdinMu.Unlock()

	defer s.forgetControlRequest(reqID)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-ch:
		if resp.Subtype != "success" {
			if resp.Error != "" {
				return fmt.Errorf("claudecode: interrupt rejected: %s", resp.Error)
			}
			return fmt.Errorf("claudecode: interrupt rejected (subtype=%q)", resp.Subtype)
		}
		return nil
	}
}

func (s *Session) forgetControlRequest(reqID string) {
	s.ctrlMu.Lock()
	delete(s.ctrlPending, reqID)
	s.ctrlMu.Unlock()
}

// validPermissionModes 是 claude --permission-mode 接受的全集。运行时切换走
// control_request{set_permission_mode} 也用同一组取值。
var validPermissionModes = map[string]struct{}{
	"default":           {},
	"acceptEdits":       {},
	"plan":              {},
	"bypassPermissions": {},
}

// SetPermissionMode 写一帧 control_request{subtype:"set_permission_mode"} 让 CLI
// 切换 permission mode，对齐 Claude TUI 的 Shift+Tab 行为 —— 包括 Turn 在飞时
// 用户点击 mode pill 的场景。
//
// 调用约束：
//   - mode 必须在 {default, acceptEdits, plan, bypassPermissions} 之中，否则
//     直接返错，不发任何帧。
//   - Close 之后调用返错。
//   - 可在 Turn 期间并发调用（与 Interrupt 同模型）—— 写帧只持 stdinMu；等
//     control_response 走 ctrlPending channel，由 Turn goroutine 的 scanner
//     reader 通过 parseLine.dispatchControlResponse 路由回来。Turn 之间调用
//     则用 TryLock 抢 turnMu 自己 drain 一次 scanner（避免 Turn 不在场时
//     channel 永远等不到 dispatcher）。
//
// CLI 在 set_permission_mode 之后还会发 system{subtype:"status",permissionMode:...}
// 帧；同 ExitPlanMode 路径，会被 parseLine 抬成 EventPermissionModeChanged
// 让上层把 DB / UI mode 同步到 CLI 实际状态。
func (s *Session) SetPermissionMode(ctx context.Context, mode string) error {
	if _, ok := validPermissionModes[mode]; !ok {
		return fmt.Errorf("claudecode: invalid permission mode %q (want default|acceptEdits|plan|bypassPermissions)", mode)
	}

	s.stdinMu.Lock()
	if s.closed {
		s.stdinMu.Unlock()
		return errors.New("claudecode: session closed")
	}
	reqID := newControlRequestID()
	ch := make(chan controlResponse, 1)
	s.ctrlMu.Lock()
	if s.ctrlPending == nil {
		s.ctrlPending = map[string]chan controlResponse{}
	}
	s.ctrlPending[reqID] = ch
	s.ctrlMu.Unlock()

	frame := map[string]any{
		"type":       "control_request",
		"request_id": reqID,
		"request":    map[string]any{"subtype": "set_permission_mode", "mode": mode},
	}
	enc, mErr := json.Marshal(frame)
	if mErr != nil {
		s.stdinMu.Unlock()
		s.forgetControlRequest(reqID)
		return mErr
	}
	if _, err := fmt.Fprintf(s.proc.stdin, "%s\n", enc); err != nil {
		s.stdinMu.Unlock()
		s.forgetControlRequest(reqID)
		return err
	}
	s.stdinMu.Unlock()

	defer s.forgetControlRequest(reqID)

	// 持久 readLoop 一直在 drain scanner,control_response 一定被 dispatch 到 ch
	// (不论此刻有没有 user turn 在飞),这里只需等 ch 或 ctx —— 不再需要在 Turn
	// 不在场时自己 TryLock turnMu drain scanner(那会和 readLoop 抢同一个 scanner)。
	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-ch:
		return setPermissionModeResponseErr(resp)
	}
}

// setPermissionModeResponseErr 把 control_response 翻译成 SetPermissionMode 的
// 返回错误。subtype=="success" → nil；其它 subtype 带原始 error 文本以便排查。
func setPermissionModeResponseErr(resp controlResponse) error {
	if resp.Subtype == "success" {
		return nil
	}
	if resp.Error != "" {
		return fmt.Errorf("claudecode: set_permission_mode rejected: %s", resp.Error)
	}
	return fmt.Errorf("claudecode: set_permission_mode rejected (subtype=%q)", resp.Subtype)
}

// newControlRequestID 生成 v4 UUID 作 request_id。crypto/rand 失败时退到
// 时间戳兜底（不破坏唯一性 —— Interrupt 调频极低）。
func newControlRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", len(b))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Close 关 stdin（触发 CLI exit）并 wait 子进程。
// 重入安全：多次调用只第一次生效。
func (s *Session) Close(ctx context.Context) error {
	s.stdinMu.Lock()
	if s.closed {
		s.stdinMu.Unlock()
		return nil
	}
	s.closed = true
	stdin := s.proc.stdin
	s.stdinMu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	_ = s.proc.stdout.Close()
	_, err := s.proc.wait(ctx)
	return err
}

// parseAssistantContentWithUsage 把 assistant 帧 inner message 解码成 Event 列表，
// 同时返回 inner message.usage（本次 API call 的 per-call 用量）。
//
// parentToolUseID 对应原始帧顶层的 parent_tool_use_id；主 agent 自己的帧传 ""；
// subagent 内部帧透传外层 Agent.tool_use_id。
//
// 返回的 *rawUsage == nil 表示这一帧没带 usage 字段（老 CLI 或简化 stub）；
// 调用方据此跟踪"最后一次 per-call 用量"以正确计算上下文窗口占用，参见
// [resolveDoneUsage]。
// parseStreamEvent 处理 type=stream_event 帧。--include-partial-messages 模式
// 下,CLI 把每次内部 API call 的 Anthropic SSE delta 包成这种帧推到 STDOUT。
// 我们只关心 event.type == message_delta 上挂的 usage —— 那是本次 API call
// 的最终 per-call 用量,GLM / openrouter 等 provider 经 gateway 走时这是唯一
// 可信来源(随后 merged assistant 帧的 usage 是 0 占位)。
//
// 其它子类型(message_start / content_block_* / message_stop)目前不消费 —— 内容
// 仍由 merged assistant 帧承载,parser 不需要重复解。
//
// subagent 过滤:沿用 assistant 帧的语义,parent_tool_use_id != "" 的 stream_event
// 来自 Task/Agent 子会话,其 message_delta usage 不能影响主 agent 的进度条。
func (s *Session) parseStreamEvent(f rawFrame) []Event {
	if f.ParentToolUseID != "" || len(f.Event) == 0 {
		return nil
	}
	var ev rawStreamEvent
	if err := json.Unmarshal(f.Event, &ev); err != nil {
		return nil
	}
	if ev.Type != "message_delta" || ev.Usage == nil || isZeroUsage(ev.Usage) {
		return nil
	}
	s.lastAssistantUsage = ev.Usage
	return []Event{{
		Kind:      EventUsage,
		SessionID: s.sessionID,
		Usage: provider.Usage{
			PromptTokens:        ev.Usage.InputTokens,
			CompletionTokens:    ev.Usage.OutputTokens,
			CachedTokens:        ev.Usage.CacheReadInputTokens,
			CacheCreationTokens: ev.Usage.CacheCreationInputTokens,
		},
	}}
}

// isZeroUsage 判定一份 rawUsage 是否四项全 0。用于 zero-clobber guard:
// stream_event message_delta 写过的真值,不能被随后 merged assistant 帧的全 0
// usage 打回 0。
func isZeroUsage(u *rawUsage) bool {
	if u == nil {
		return true
	}
	return u.InputTokens == 0 && u.OutputTokens == 0 &&
		u.CacheReadInputTokens == 0 && u.CacheCreationInputTokens == 0
}

func parseAssistantContentWithUsage(raw json.RawMessage, sid, parentToolUseID string) ([]Event, *rawUsage) {
	if len(raw) == 0 {
		return nil, nil
	}
	var m rawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil
	}
	out := make([]Event, 0, len(m.Content))
	for _, c := range m.Content {
		switch c.Type {
		case "text":
			if c.Text == "" {
				continue
			}
			out = append(out, Event{Kind: EventTextDelta, SessionID: sid, Text: c.Text, ParentToolUseID: parentToolUseID})
		case "thinking":
			if c.Thinking == "" {
				continue
			}
			out = append(out, Event{Kind: EventThinkingDelta, SessionID: sid, Text: c.Thinking, ParentToolUseID: parentToolUseID})
		case "tool_use":
			out = append(out, Event{
				Kind:            EventPreToolUse,
				SessionID:       sid,
				Tool:            &ToolEvent{ID: c.ID, Name: c.Name, Input: c.Input},
				ParentToolUseID: parentToolUseID,
			})
		}
	}
	return out, m.Usage
}

// parseUserContent 把 user 帧的 message + 顶层 tool_use_result 翻译成
// EventPostToolUse 列表。
//
// toolUseResult 是 CLI 在 user 帧顶层（跟 message 同级）吐的工具结构化元数据
// （TaskCreate 的 {"task":{"id":"1"}} 之类）；一条 user 帧通常只承载一个 tool_result
// block,所以 meta 与 block 一对一,直接挂到对应 ToolEvent.ResultMeta 上即可。
// 缺省（普通工具帧没这个字段）时为 nil,ResultMeta 也留 nil。
func parseUserContent(raw json.RawMessage, sid, parentToolUseID string, toolUseResult json.RawMessage) []Event {
	if len(raw) == 0 {
		return nil
	}
	var m struct {
		Content []rawContentBlock `json:"content"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	out := make([]Event, 0, len(m.Content))
	for _, c := range m.Content {
		if c.Type != "tool_result" {
			continue
		}
		tool := &ToolEvent{
			ID:         c.ToolUseID,
			Response:   decodeToolResultContent(c.Content),
			ResultMeta: toolUseResult,
		}
		if c.IsError {
			tool.Err = errIfToolError(c.IsError)
		}
		out = append(out, Event{Kind: EventPostToolUse, SessionID: sid, Tool: tool, ParentToolUseID: parentToolUseID})
	}
	return out
}

// parseSystemTask 与 frameDecoder.decodeSystemTask 等价，给 Session 共用。
//
// 处理两类系统帧：
//   - task_started / task_progress / task_notification ── subagent 生命周期
//   - api_retry ── CLI 把 Anthropic SDK 的可重试错误抬成 first-class 协议帧，
//     字段（attempt / max_retries / retry_delay_ms / error_status / error）直接在帧顶层
//
// 返回 (Event, false) 表示既不是 task_* 也不是 api_retry。
func parseSystemTask(f rawFrame, sid string) (Event, bool) {
	if f.Subtype == "api_retry" {
		return Event{
			Kind:      EventRetry,
			SessionID: sid,
			Retry: &RetryEvent{
				Attempt:     f.Attempt,
				MaxAttempts: f.MaxRetries,
				DelayMs:     f.RetryDelayMs,
				ErrorStatus: f.ErrorStatus,
				ErrorCode:   f.ErrorField,
			},
		}, true
	}
	if f.Subtype == "compact_boundary" {
		ev := Event{Kind: EventCompactBoundary, SessionID: sid, Compact: &CompactEvent{}}
		if len(f.CompactMetadata) > 0 {
			var m struct {
				PreTokens  int    `json:"pre_tokens"`
				PostTokens int    `json:"post_tokens"`
				Trigger    string `json:"trigger"`
				DurationMs int    `json:"duration_ms"`
			}
			// 字段缺失/类型不符时保持零值,UI 自行退化展示。
			_ = json.Unmarshal(f.CompactMetadata, &m)
			ev.Compact.PreTokens = m.PreTokens
			ev.Compact.PostTokens = m.PostTokens
			ev.Compact.Trigger = m.Trigger
			ev.Compact.DurationMs = m.DurationMs
		}
		return ev, true
	}
	var kind EventKind
	switch f.Subtype {
	case "task_started":
		kind = EventTaskStarted
	case "task_progress":
		kind = EventTaskProgress
	case "task_notification":
		kind = EventTaskNotification
	default:
		return Event{}, false
	}
	meta := &SubagentMeta{
		TaskID:          f.TaskID,
		SubagentType:    f.SubagentType,
		TaskType:        f.TaskType,
		TaskDescription: f.Description,
		Prompt:          f.Prompt,
		LastToolName:    f.LastToolName,
		Status:          f.Status,
	}
	if len(f.Usage) > 0 {
		var u taskUsage
		if err := json.Unmarshal(f.Usage, &u); err == nil {
			meta.TotalTokens = u.TotalTokens
			meta.ToolUses = u.ToolUses
			meta.DurationMs = u.DurationMs
		}
	}
	return Event{
		Kind:      kind,
		SessionID: sid,
		Tool:      &ToolEvent{ID: f.ToolUseID, Subagent: meta},
	}, true
}

// statusEvents 把 system{subtype:"status"} 帧拆成最多两条独立事件 (Status / PermissionMode),
// 互相不互斥 —— 同一帧两字段都非空时两条都 emit。两字段都空则返回 nil
// (前向兼容:未来 CLI 加新字段不要因为 Status / PermissionMode 都空就误触发).
func statusEvents(sid string, f rawFrame) []Event {
	var out []Event
	if f.Status != "" {
		out = append(out, Event{Kind: EventStatus, SessionID: sid, Status: f.Status})
	}
	if f.PermissionMode != "" {
		out = append(out, Event{Kind: EventPermissionModeChanged, SessionID: sid, PermissionMode: f.PermissionMode})
	}
	return out
}

// errIfToolError 把 tool_result.is_error 翻译成 error。抽出来仅是为了让
// parseUserContent 保持纯函数（与 frameDecoder.decodeUser 的内联 errors.New
// 等价）。
func errIfToolError(isErr bool) error {
	if !isErr {
		return nil
	}
	return errToolReported
}

var errToolReported = errors.New("tool reported error")
