package claudecode

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	// turnMu 串行化 Turn 生命周期 —— 上一个 Turn 的 routeUntilResult 完成之前
	// 拒绝下一个 Turn 启动。Turn 内部 spawn 的 goroutine 持有该锁直到 channel 关闭。
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
	lastAssistantUsage *rawUsage
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

	return &Session{proc: p, scanner: sc, sessionID: spec.sessionID}, nil
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
	s.turnMu.Lock() // 抢 turn slot —— 上一个 turn 没收尾不让进
	// 防御性清当前 turn 的瞬态：正常情况下 parseLine 看到 result 帧时会清
	// lastAssistantUsage；但 ctx 取消 / scanner EOF 出现在 result 之前时，
	// routeUntilResult 直接返回不走 result 分支，余值会留到下一轮。turnMu 保护
	// 下显式清一次，给 lastAssistantUsage 一个明确的"turn 入口干净"语义。
	s.lastAssistantUsage = nil
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

	ch := make(chan Event, 16)
	go func() {
		defer close(ch)
		defer s.turnMu.Unlock() // routeUntilResult 走完再放 turn 锁
		s.routeUntilResult(ctx, ch)
	}()
	return ch, nil
}

// routeUntilResult 读 stdout 行、parse、把 Event 推给 ch；result 帧到达后返回。
// scanner 错误或 EOF 也返回（caller close ch）。
//
// scanner 拿到 EOF 时如果是子进程已死，把 proc.exitErrIfDone 抓到的真错 snapshot
// 进 Session.lastErr —— runtime 层 0-frame fallback 通过 Session.ExitErr 拿到这个
// 值替换通用错误消息。
func (s *Session) routeUntilResult(ctx context.Context, ch chan<- Event) {
	for s.scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := s.scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		events, done := s.parseLine(line)
		for _, ev := range events {
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
		if done {
			return
		}
	}
	// scanner 退出后（EOF / 错误），如果子进程已死且 stderr 命中了 sentinel，
	// snapshot 真错给 Session.ExitErr 用。stderr 没命中也存着 *ProcessExitError，
	// 让上层比 "0 frames" 那条通用消息精确。
	if exitErr := s.proc.exitErrIfDone(); exitErr != nil {
		s.rememberExitErr(exitErr)
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

	if resp, ok := receiveControlResponse(ch); ok {
		return setPermissionModeResponseErr(resp)
	}

	// Reader 选择：
	//   - turnMu 可获取 → Turn 不在飞，没有别的 reader。我们独占 scanner 自己 drain。
	//   - turnMu 抢不到 → Turn goroutine 在 routeUntilResult 里读 scanner，
	//     parseLine 会把 control_response dispatchControlResponse 到我们的 ch。
	if !s.turnMu.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case resp := <-ch:
			return setPermissionModeResponseErr(resp)
		}
	}
	defer s.turnMu.Unlock()

	if resp, ok := receiveControlResponse(ch); ok {
		return setPermissionModeResponseErr(resp)
	}

	for s.scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := s.scanner.Bytes()
		if len(line) > 0 {
			s.parseLine(line)
		}
		select {
		case resp := <-ch:
			return setPermissionModeResponseErr(resp)
		default:
		}
	}
	if err := s.scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

func receiveControlResponse(ch <-chan controlResponse) (controlResponse, bool) {
	select {
	case resp := <-ch:
		return resp, true
	default:
		return controlResponse{}, false
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
