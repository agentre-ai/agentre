package claudecode

import (
	"bufio"
	"context"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// extractTextField 抓 stdin user frame 里 message.content[0].text 字段的 best-effort 取值。
// 跟原 shell fake 的 sed 正则等价（"text":"..."），失败 fallback "ack"。
var textFieldRE = regexp.MustCompile(`"text":"([^"]*)"`)

func extractTextField(line string) string {
	if m := textFieldRE.FindStringSubmatch(line); len(m) == 2 && m[1] != "" {
		return m[1]
	}
	return "ack"
}

// extractStringField 通用版：抓 JSON 行里 "<key>":"<value>" 模式的字段。
// fake CLI 处理 control_request 时拿 request_id / mode 用。
func extractStringField(line, key string) string {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `":"([^"]*)"`)
	if m := re.FindStringSubmatch(line); len(m) == 2 {
		return m[1]
	}
	return ""
}

// fakePersistent 模拟常驻 claude 子进程：每条 user frame 起一轮，喂 init + 回声
// assistant + result，直到 stdin EOF。
func fakePersistent(stdin io.Reader, stdout io.Writer) {
	const sid = "sess-persistent"
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 0, 64<<10), maxFrameBytes)
	turn := 0
	for sc.Scan() {
		turn++
		reply := extractTextField(sc.Text())
		writeFrame(stdout, `{"type":"system","subtype":"init","session_id":%q,"cwd":"/tmp","model":"m","tools":[]}`, sid)
		writeFrame(stdout, `{"type":"assistant","message":{"id":"m%d","content":[{"type":"text","text":"echo:%s"}]}}`, turn, reply)
		writeFrame(stdout, `{"type":"result","subtype":"success","session_id":%q,"usage":{"input_tokens":%d,"output_tokens":%d}}`, sid, turn, turn)
	}
}

// fakeInterrupt 模拟"长 turn 被中断"：user frame 触发 init+partial，不发 result；
// control_request{interrupt} 触发 control_response{success} + result{interrupted}。
func fakeInterrupt(stdin io.Reader, stdout io.Writer) {
	const sid = "sess-interrupt"
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 0, 64<<10), maxFrameBytes)
	turn := 0
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, `"type":"user"`):
			turn++
			reply := extractTextField(line)
			writeFrame(stdout, `{"type":"system","subtype":"init","session_id":%q,"cwd":"/tmp","model":"m","tools":[]}`, sid)
			writeFrame(stdout, `{"type":"assistant","message":{"id":"m%d","content":[{"type":"text","text":"partial:%s"}]}}`, turn, reply)
		case strings.Contains(line, `"type":"control_request"`):
			reqID := extractStringField(line, "request_id")
			writeFrame(stdout, `{"type":"control_response","response":{"subtype":"success","request_id":%q}}`, reqID)
			writeFrame(stdout, `{"type":"result","subtype":"interrupted","session_id":%q,"usage":{"input_tokens":1,"output_tokens":1}}`, sid)
		}
	}
}

// fakeSetMode 模拟 turn 之间切 mode：control_request → success（request_id 在
// response 内层，对齐真 CLI）；user frame → init + echo + result。
func fakeSetMode(stdin io.Reader, stdout io.Writer) {
	const sid = "sess-set-mode"
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 0, 64<<10), maxFrameBytes)
	turn := 0
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, `"type":"control_request"`):
			reqID := extractStringField(line, "request_id")
			mode := extractStringField(line, "mode")
			writeFrame(stdout, `{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"mode":%q}}}`, reqID, mode)
		case strings.Contains(line, `"type":"user"`):
			turn++
			reply := extractTextField(line)
			writeFrame(stdout, `{"type":"system","subtype":"init","session_id":%q,"cwd":"/tmp","model":"m","tools":[]}`, sid)
			writeFrame(stdout, `{"type":"assistant","message":{"id":"m%d","content":[{"type":"text","text":"echo:%s"}]}}`, turn, reply)
			writeFrame(stdout, `{"type":"result","subtype":"success","session_id":%q,"usage":{"input_tokens":1,"output_tokens":1}}`, sid)
		}
	}
}

// fakeMidTurnSetMode 模拟"长 turn 飞行中切 mode"：user frame 触发 init+partial
// （不发 result）；control_request{set_permission_mode} → success +
// status{permissionMode} + result{success}（结束本轮）。
func fakeMidTurnSetMode(stdin io.Reader, stdout io.Writer) {
	const sid = "sess-mid-turn-set-mode"
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 0, 64<<10), maxFrameBytes)
	turn := 0
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, `"type":"user"`):
			turn++
			reply := extractTextField(line)
			writeFrame(stdout, `{"type":"system","subtype":"init","session_id":%q,"cwd":"/tmp","model":"m","tools":[]}`, sid)
			writeFrame(stdout, `{"type":"assistant","message":{"id":"m%d","content":[{"type":"text","text":"partial:%s"}]}}`, turn, reply)
		case strings.Contains(line, `"type":"control_request"`) && strings.Contains(line, `"subtype":"set_permission_mode"`):
			reqID := extractStringField(line, "request_id")
			mode := extractStringField(line, "mode")
			writeFrame(stdout, `{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"mode":%q}}}`, reqID, mode)
			writeFrame(stdout, `{"type":"system","subtype":"status","session_id":%q,"permissionMode":%q}`, sid, mode)
			writeFrame(stdout, `{"type":"result","subtype":"success","session_id":%q,"usage":{"input_tokens":1,"output_tokens":1}}`, sid)
		}
	}
}

// fakeRetry 模拟 Anthropic SDK 命中可重试错误退避两次：每条 user frame → init +
// 2×api_retry + assistant text + result.success。
func fakeRetry(stdin io.Reader, stdout io.Writer) {
	const sid = "sess-retry"
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 0, 64<<10), maxFrameBytes)
	for sc.Scan() {
		reply := extractTextField(sc.Text())
		writeFrame(stdout, `{"type":"system","subtype":"init","session_id":%q,"cwd":"/tmp","model":"m","tools":[]}`, sid)
		writeFrame(stdout, `{"type":"system","subtype":"api_retry","attempt":1,"max_retries":10,"retry_delay_ms":585.8,"error_status":529,"error":"rate_limit","session_id":%q,"uuid":"u1"}`, sid)
		writeFrame(stdout, `{"type":"system","subtype":"api_retry","attempt":2,"max_retries":10,"retry_delay_ms":1229.3,"error_status":529,"error":"rate_limit","session_id":%q,"uuid":"u2"}`, sid)
		writeFrame(stdout, `{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"echo:%s"}]}}`, reply)
		writeFrame(stdout, `{"type":"result","subtype":"success","session_id":%q,"usage":{"input_tokens":1,"output_tokens":1}}`, sid)
	}
}

// fakeAlive 模拟健康的常驻进程：阻塞读 stdin，直到 EOF 才返回。
// 不写 stdout/stderr —— 用于健康检查窗口存活的回归。
func fakeAlive(stdin io.Reader, _ io.Writer) {
	_, _ = io.Copy(io.Discard, stdin)
}

// fakePassiveMode 模拟 Claude Code 2.1.145 trace：命中 "no-mode" 文本则发不带
// permissionMode 的 status 帧（前向兼容场景）；其它情况发 status{permissionMode:"default"}
// （ExitPlanMode 被批准后 CLI 自动从 plan → default 的回执）。
func fakePassiveMode(stdin io.Reader, stdout io.Writer) {
	const sid = "sess-passive-mode"
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 0, 64<<10), maxFrameBytes)
	turn := 0
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, `"type":"user"`) {
			continue
		}
		turn++
		reply := extractTextField(line)
		writeFrame(stdout, `{"type":"system","subtype":"init","session_id":%q,"cwd":"/tmp","model":"m","tools":[],"permissionMode":"plan"}`, sid)
		writeFrame(stdout, `{"type":"assistant","message":{"id":"m%d","content":[{"type":"text","text":"echo:%s"}]}}`, turn, reply)
		if strings.Contains(reply, "no-mode") {
			// 前向兼容：status 帧无 permissionMode → 不抬事件
			writeFrame(stdout, `{"type":"system","subtype":"status","status":null,"session_id":%q,"uuid":"u-no-mode"}`, sid)
		} else {
			writeFrame(stdout, `{"type":"system","subtype":"status","status":null,"permissionMode":"default","session_id":%q,"uuid":"u-passive"}`, sid)
		}
		writeFrame(stdout, `{"type":"result","subtype":"success","session_id":%q,"usage":{"input_tokens":1,"output_tokens":1}}`, sid)
	}
}

// resumeMissingStderr 复刻真实 CLI 命中 `claude --resume <gone-id>` 时的 stderr 输出。
const resumeMissingStderr = "No conversation found with session ID: 07dcda59-d426-4d66-b6d3-12d6d59bc5a3\n"

// drainText 消费 events channel，把所有 EventTextDelta 拼起来；忽略其它事件。
func drainText(t *testing.T, ch <-chan Event) string {
	t.Helper()
	var b strings.Builder
	for ev := range ch {
		if ev.Kind == EventTextDelta {
			b.WriteString(ev.Text)
		}
	}
	return b.String()
}

// TestSession_MultiTurn 走一遍 OpenSession → Turn × 2 → Close，验证：
//   - 两轮 Turn 用的是同一个子进程（fake 在 stdin EOF 时才退出）
//   - 每轮的事件 channel 在 result 帧后正常关闭，不会跨轮串味
//   - 助手文本能完整透出
func TestSession_MultiTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakePersistent))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)

	// Turn 1
	ch1, err := sess.Turn(ctx, "alpha")
	require.NoError(t, err)
	got1 := drainText(t, ch1)
	assert.Equal(t, "echo:alpha", got1)

	// Turn 2 —— 复用同一个 session
	ch2, err := sess.Turn(ctx, "beta")
	require.NoError(t, err)
	got2 := drainText(t, ch2)
	assert.Equal(t, "echo:beta", got2)

	require.NoError(t, sess.Close(ctx))
}

// TestSession_CloseStopsTurns 验证 Close 之后再 Turn 会拿到错误。
func TestSession_CloseStopsTurns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakePersistent))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	require.NoError(t, sess.Close(ctx))

	_, err = sess.Turn(ctx, "after-close")
	assert.Error(t, err)
}

// TestSession_Interrupt 验证 control_request{interrupt} 路径：
//   - Turn 启动后子进程写出 partial assistant 块，然后阻塞读 stdin（不发 result）；
//   - Interrupt 写 control_request 帧 → fake 回 control_response{success} + result 帧；
//   - Interrupt 调用返回 nil；events channel 自然关闭；partial 文本保留。
func TestSession_Interrupt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakeInterrupt))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	ch, err := sess.Turn(ctx, "long-job")
	require.NoError(t, err)

	// 等 partial 出来再 Interrupt，否则可能在 user frame 被 fake 处理之前就发 ctrl 帧。
	first, ok := <-ch
	require.True(t, ok, "expected at least one event before interrupt")
	assert.Equal(t, EventTextDelta, first.Kind)
	assert.Equal(t, "partial:long-job", first.Text)

	require.NoError(t, sess.Interrupt(ctx))

	// drain 剩余事件（result 帧到达后 channel 关闭）
	for range ch { //nolint:revive // 仅用于 drain
	}
}

// TestSession_InterruptAfterClose 验证 Close 之后 Interrupt 返回错误（不 panic）。
func TestSession_InterruptAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakeInterrupt))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	require.NoError(t, sess.Close(ctx))

	assert.Error(t, sess.Interrupt(ctx))
}

// TestSession_SetPermissionMode 验证 control_request{set_permission_mode} 路径：
//   - fake 收到 control_request → 回 control_response{success}（request_id 在
//     response 内层，对齐真 CLI）；
//   - SetPermissionMode 返 nil；
//   - 切换后 Turn 仍能正常 drain（验证 scanner 状态没被打乱）。
func TestSession_SetPermissionMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakeSetMode))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	require.NoError(t, sess.SetPermissionMode(ctx, "plan"))

	ch, err := sess.Turn(ctx, "after-switch")
	require.NoError(t, err)
	got := drainText(t, ch)
	assert.Equal(t, "echo:after-switch", got)
}

// TestSession_SetPermissionMode_MidTurn 复刻用户报告的核心 bug：
// Turn 已开飞但 result 帧尚未到达时（典型场景：长 turn 中用户点 mode pill），
// SetPermissionMode 必须能立刻把 control_request 写下去并在 control_response
// 回来后返 nil；不能一直阻塞到 Turn 自然 done。
func TestSession_SetPermissionMode_MidTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakeMidTurnSetMode))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	ch, err := sess.Turn(ctx, "long-job")
	require.NoError(t, err)

	// 等 partial 出来再切 mode，保证 Turn goroutine 已经在读 scanner。
	first, ok := <-ch
	require.True(t, ok, "expected at least one event before set-mode")
	assert.Equal(t, EventTextDelta, first.Kind)

	// 给 SetPermissionMode 一个紧凑的截止：当前实现卡在 turnMu 上会让本步超时。
	setCtx, setCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer setCancel()
	require.NoError(t, sess.SetPermissionMode(setCtx, "plan"),
		"SetPermissionMode 必须能在 Turn 在飞时立刻送出 control_request 并拿到 control_response")

	// drain 剩余事件（fake 在 set-mode 之后会发 status + result 让本轮收尾）。
	var sawModeChange bool
	for ev := range ch {
		if ev.Kind == EventPermissionModeChanged && ev.PermissionMode == "plan" {
			sawModeChange = true
		}
	}
	assert.True(t, sawModeChange, "fake 已发 system{status,permissionMode:plan}，应被抬成 EventPermissionModeChanged")
}

// TestSession_SetPermissionMode_InvalidMode 验证白名单校验在写帧之前生效。
func TestSession_SetPermissionMode_InvalidMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakeSetMode))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	assert.Error(t, sess.SetPermissionMode(ctx, "nope"))
	assert.Error(t, sess.SetPermissionMode(ctx, ""))
}

// TestSession_RetryEventsArriveBeforeDone 验证 Session.Turn 能把 system.api_retry
// 帧抬成 EventRetry 推到本轮事件 channel，且不影响后续 text / done 的顺序。
// 这是 Claude 后端"重试可视化"的最底层契约——chat_svc 会用这条信号驱动
// 前端 RetryNoticeCard。
func TestSession_RetryEventsArriveBeforeDone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakeRetry))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	ch, err := sess.Turn(ctx, "alpha")
	require.NoError(t, err)

	var (
		retries  []Event
		text     string
		sawText  bool
		eventLog []EventKind
	)
	for ev := range ch {
		eventLog = append(eventLog, ev.Kind)
		switch ev.Kind {
		case EventRetry:
			retries = append(retries, ev)
		case EventTextDelta:
			text += ev.Text
			sawText = true
		}
	}

	require.Len(t, retries, 2, "fake script emits 2 api_retry frames; event log: %v", eventLog)
	require.NotNil(t, retries[0].Retry)
	assert.Equal(t, 1, retries[0].Retry.Attempt)
	assert.Equal(t, 10, retries[0].Retry.MaxAttempts)
	assert.Equal(t, 529, retries[0].Retry.ErrorStatus)
	assert.Equal(t, "rate_limit", retries[0].Retry.ErrorCode)
	assert.InDelta(t, 585.8, retries[0].Retry.DelayMs, 0.0001)
	assert.Equal(t, "sess-retry", retries[0].SessionID)

	require.NotNil(t, retries[1].Retry)
	assert.Equal(t, 2, retries[1].Retry.Attempt)

	assert.True(t, sawText, "retry 之后的 assistant text 必须到达")
	assert.Equal(t, "echo:alpha", text)
}

// TestSession_SetPermissionModeAfterClose 验证 Close 后调返错而不 panic。
func TestSession_SetPermissionModeAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakeSetMode))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	require.NoError(t, sess.Close(ctx))

	assert.Error(t, sess.SetPermissionMode(ctx, "plan"))
}

// TestSession_OpenSessionRejectsResumeMissing 复刻用户报告的核心 bug：
// 真实场景下 `claude --resume <gone-id>` 会立刻写 stderr 并 exit 1，但是 OpenSession
// 之前 spawn 完就直接返回 handle，错误被 boundedBuffer 静默吃掉 → 前端无任何报错。
// 修复后 OpenSession 在 200ms 早退检测窗口里必须拿到 wrapped ErrSessionNotFound。
func TestSession_OpenSessionRejectsResumeMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, nil, withExitCode(1), withStderr(resumeMissingStderr)))
	sess, err := c.OpenSession(ctx)
	require.Error(t, err, "立刻 exit 的子进程必须让 OpenSession 报错")
	assert.Nil(t, sess, "出错时不应返回半成品 Session")
	assert.ErrorIs(t, err, ErrSessionNotFound, "命中 stderr 'No conversation found' → ErrSessionNotFound")
	assert.Contains(t, err.Error(), "No conversation found",
		"错误文案必须包含 CLI 真实 stderr，方便用户排查")
}

// TestSession_OpenSessionHealthyPassesCheckWindow 健康路径回归：进程 spawn 后只
// 阻塞读 stdin（典型的 claude --print 流式守护行为），200ms 健康检查窗口里没退出
// 也没首帧 → OpenSession 必须正常返回 Session。
func TestSession_OpenSessionHealthyPassesCheckWindow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakeAlive))
	start := time.Now()
	sess, err := c.OpenSession(ctx)
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.NotNil(t, sess)
	t.Cleanup(func() { _ = sess.Close(context.Background()) })

	assert.GreaterOrEqual(t, elapsed, claudeStartupCheckTimeout,
		"OpenSession 必须等满健康检查窗口，确保给坏 spawn 足够时间冒出来")
}

// TestSession_ExitErrSurfacesProviderSessionGone 进程死亡后 Session.ExitErr
// 必须把分类后的 ErrSessionNotFound 露出来。runtime 层 0-frame fallback 用
// 这个方法把 RunResult.StopErr 替换成真错。
func TestSession_ExitErrSurfacesProviderSessionGone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 直接构造 process 模拟 "OpenSession 之后才发现进程已死" 的链路；nil fakeCLI
	// 让 process 在构造时就处于已退出状态（同步关 exit channel）。
	p := newPipeProcess(t, ctx, nil, withExitCode(1), withStderr(resumeMissingStderr))
	require.True(t, p.hasExited(), "nil fakeCLI 构造的 process 应当立刻报 hasExited=true")

	s := &Session{proc: p}
	exitErr := s.ExitErr()
	require.Error(t, exitErr)
	assert.ErrorIs(t, exitErr, ErrSessionNotFound)
}

// TestSession_PassivePermissionModeChange 验证 CLI 自身切换 permission mode
// 后的 system{subtype:"status",permissionMode:...} 帧会被抬成
// EventPermissionModeChanged。真实场景：用户启动 plan mode，AI 调 ExitPlanMode
// 工具被批准后，CLI 自动切到 default 并发这条 status 帧。
// 帧形态来自 Claude Code 2.1.145 trace。
func TestSession_PassivePermissionModeChange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakePassiveMode))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	ch, err := sess.Turn(ctx, "exit-plan")
	require.NoError(t, err)

	var (
		modeChanges []Event
		eventLog    []EventKind
	)
	for ev := range ch {
		eventLog = append(eventLog, ev.Kind)
		if ev.Kind == EventPermissionModeChanged {
			modeChanges = append(modeChanges, ev)
		}
	}

	require.Len(t, modeChanges, 1, "expected exactly one EventPermissionModeChanged; eventLog=%v", eventLog)
	assert.Equal(t, "default", modeChanges[0].PermissionMode, "CLI 退出 plan mode 后切到 default")
	assert.Equal(t, "sess-passive-mode", modeChanges[0].SessionID)

	// EventDone 必须紧随 EventPermissionModeChanged，验证 status 帧没把后续 result
	// 帧打乱。
	var lastIdx int
	for i, k := range eventLog {
		if k == EventPermissionModeChanged {
			lastIdx = i
		}
	}
	require.Less(t, lastIdx, len(eventLog)-1, "status frame must not be the terminal event")
	assert.Equal(t, EventDone, eventLog[len(eventLog)-1], "result 帧产出的 EventDone 必须是最后一条")
}

// TestSession_DoneUsesLastAssistantUsage 同 TestStream_DoneUsesLastAssistantUsage，
// 但走 Session.parseLine（常驻进程多 turn 路径）。额外验证 turn 之间 lastAssistantUsage
// 不串味——第二轮就算 assistant 帧没带 usage，也不能把第一轮的值带过来。
func TestSession_DoneUsesLastAssistantUsage(t *testing.T) {
	s := &Session{}

	// Turn 1：两次内部 API call。result.usage 是累加；正确口径是最后一帧 assistant 的 per-call usage。
	turn1Frames := []string{
		`{"type":"system","subtype":"init","session_id":"sx"}`,
		`{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"X","input":{}}],"usage":{"input_tokens":200,"output_tokens":50,"cache_read_input_tokens":10000,"cache_creation_input_tokens":0}}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`,
		`{"type":"assistant","message":{"id":"m2","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":50,"output_tokens":20,"cache_read_input_tokens":10300,"cache_creation_input_tokens":50}}}`,
		`{"type":"result","subtype":"success","session_id":"sx","usage":{"input_tokens":250,"output_tokens":70,"cache_read_input_tokens":20300,"cache_creation_input_tokens":50}}`,
	}
	var doneEv *Event
	for _, line := range turn1Frames {
		evs, isResult := s.parseLine([]byte(line))
		if isResult {
			require.Len(t, evs, 1)
			doneEv = &evs[0]
		}
	}
	require.NotNil(t, doneEv, "expected EventDone after turn 1 result frame")
	assert.Equal(t, 50, doneEv.Usage.PromptTokens, "Turn1 EventDone.PromptTokens 必须取 last per-call (50)，不是 result 累加 (250)")
	assert.Equal(t, 10300, doneEv.Usage.CachedTokens)
	assert.Equal(t, 50, doneEv.Usage.CacheCreationTokens)
	assert.Equal(t, 20, doneEv.Usage.CompletionTokens)

	// Turn 2 起始：parseLine 应已把 lastAssistantUsage 清空，避免 turn 间串味。
	// 极简 turn：assistant 帧不带 usage → EventDone 必须 fallback 到 result.usage（10/5/3/1），
	// 而不是 turn1 的 50/20 余值。
	turn2Frames := []string{
		`{"type":"assistant","message":{"id":"m3","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"result","subtype":"success","session_id":"sx","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":3,"cache_creation_input_tokens":1}}`,
	}
	doneEv = nil
	for _, line := range turn2Frames {
		evs, isResult := s.parseLine([]byte(line))
		if isResult {
			require.Len(t, evs, 1)
			doneEv = &evs[0]
		}
	}
	require.NotNil(t, doneEv)
	assert.Equal(t, 10, doneEv.Usage.PromptTokens, "Turn2 没 assistant usage → fallback 到 result.usage；不能拿 turn1 的余值")
	assert.Equal(t, 3, doneEv.Usage.CachedTokens)
	assert.Equal(t, 1, doneEv.Usage.CacheCreationTokens)
	assert.Equal(t, 5, doneEv.Usage.CompletionTokens)
}

// TestSession_GLMRealFrameShape 回归 Bug 2：复刻 GLM (https://huu.dqy.ink) 实际返回
// 的 assistant 帧 shape（来自 ~/.claude/projects/.../7470a64f-…jsonl 的实测样本），
// 多余的 server_tool_use / service_tier / cache_creation 对象 / iterations 数组等
// 字段不该让 json.Unmarshal 失败；rawUsage 的 4 个 int 字段必须能从中正确抽出。
//
// 这个 case 跑过 → 说明 parser 在 JSONL 形态的帧上工作正常；usage = 0 的现象一定
// 是 STDOUT 流跟 JSONL 落盘的字段路径不一致（多半 --include-partial-messages
// 让 CLI 把 usage 移到了 stream_event 类帧里，需要 raw dump 进一步定位）。
// 跑不过 → parser 有隐藏 bug，需要直接修。
func TestSession_GLMRealFrameShape(t *testing.T) {
	s := &Session{}

	// 完全照搬 7470a64f-…jsonl:line 实测 assistant 帧 message 段，外层 type
	// 是 STDOUT 协议的样子(没 parentUuid / uuid / timestamp 这些 JSONL 元数据)。
	glmFrame := `{"type":"assistant","message":{"type":"message","id":"02177969507279077fce418cd3a659821a063326c55dce3b59e46","role":"assistant","content":[{"type":"thinking","thinking":"The user wants to see the directory contents.","signature":""}],"model":"glm-5.1","stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":36079,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":61,"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0},"service_tier":"standard","cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":0},"inference_geo":"","iterations":[],"speed":"standard"},"stop_details":null}}`

	// 喂入 init + glm assistant + 一个无 usage 的 result，断言 EventDone.Usage
	// 必须取自 lastAssistantUsage（即 glm assistant 帧里的 usage）。
	frames := []string{
		`{"type":"system","subtype":"init","session_id":"sx","model":"glm-5.1"}`,
		glmFrame,
		`{"type":"result","subtype":"success","session_id":"sx"}`,
	}
	var doneEv *Event
	for _, line := range frames {
		evs, isResult := s.parseLine([]byte(line))
		if isResult {
			require.Len(t, evs, 1)
			doneEv = &evs[0]
		}
	}
	require.NotNil(t, doneEv, "result 帧应当产出 EventDone")
	assert.Equal(t, 36079, doneEv.Usage.PromptTokens, "GLM 实测帧 input_tokens 必须被解出")
	assert.Equal(t, 61, doneEv.Usage.CompletionTokens, "GLM 实测帧 output_tokens 必须被解出")
	assert.Equal(t, "glm-5.1", doneEv.Model, "system.init.model = glm-5.1 必须透到 EventDone.Model")
}

// TestSession_StreamEventMessageDeltaUsage 回归 Bug 2 真正的根因:
// --include-partial-messages 模式下,CLI 把 Anthropic SSE delta 包成 type=stream_event
// 帧推到 STDOUT;每次内部 API call 的最终 usage 在 stream_event.event.type =
// message_delta 上,**不在**随后那条 merged 'assistant' 帧上 —— 后者的 usage 是
// CLI 给的 message_start 状态(input_tokens=0 / output_tokens=0)的副本。parser
// 必须:
//
//	(1) 解 stream_event message_delta 把真 usage 存 lastAssistantUsage;
//	(2) 不让 merged assistant 帧的 0 usage 把它打回 0(zero-clobber guard)。
//
// 数据来自 /tmp/cc-raw.log 实测(GLM via gateway,session_id=a948e6aa-…)。
func TestSession_StreamEventMessageDeltaUsage(t *testing.T) {
	s := &Session{}

	frames := []string{
		`{"type":"system","subtype":"init","session_id":"sx","model":"glm-5.1"}`,
		// 第 1 次 API call:message_start usage 是 0 占位
		`{"type":"stream_event","session_id":"sx","parent_tool_use_id":null,"event":{"type":"message_start","message":{"type":"message","id":"m1","role":"assistant","content":[],"model":"glm-5.1","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}}}}`,
		`{"type":"stream_event","session_id":"sx","parent_tool_use_id":null,"event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}}`,
		`{"type":"stream_event","session_id":"sx","parent_tool_use_id":null,"event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hi"}}}`,
		`{"type":"stream_event","session_id":"sx","parent_tool_use_id":null,"event":{"type":"content_block_stop","index":0}}`,
		// message_delta 才带真 usage
		`{"type":"stream_event","session_id":"sx","parent_tool_use_id":null,"event":{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1180,"output_tokens":61,"cache_read_input_tokens":34496}}}`,
		`{"type":"stream_event","session_id":"sx","parent_tool_use_id":null,"event":{"type":"message_stop"}}`,
		// merged assistant 帧:usage 全 0(CLI 没把 delta 累回去)
		`{"type":"assistant","parent_tool_use_id":null,"message":{"type":"message","id":"m1","role":"assistant","content":[{"type":"thinking","thinking":"hi"}],"model":"glm-5.1","stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}}}`,
		`{"type":"result","subtype":"success","session_id":"sx"}`,
	}

	var doneEv *Event
	var usageEvents []Event
	for _, line := range frames {
		evs, isResult := s.parseLine([]byte(line))
		for _, ev := range evs {
			if ev.Kind == EventUsage {
				usageEvents = append(usageEvents, ev)
			}
		}
		if isResult {
			require.Len(t, evs, 1)
			doneEv = &evs[0]
		}
	}

	require.NotNil(t, doneEv, "result 帧应当产出 EventDone")
	assert.Equal(t, 1180, doneEv.Usage.PromptTokens, "EventDone.PromptTokens 必须取自 message_delta(1180),不是 merged assistant 帧的 0")
	assert.Equal(t, 61, doneEv.Usage.CompletionTokens)
	assert.Equal(t, 34496, doneEv.Usage.CachedTokens)

	// message_delta 应当顺手 emit 一条 EventUsage 让上层 (chat_svc → 前端进度条)
	// 在 turn 内实时刷新「已用上下文」。merged assistant 帧的 0 usage 不应再 emit
	// EventUsage(避免进度条骤降到 0)。
	require.Len(t, usageEvents, 1, "应仅 message_delta emit 一条 EventUsage,merged assistant 帧的 0 usage 不该 emit")
	assert.Equal(t, 1180, usageEvents[0].Usage.PromptTokens)
}

// TestSession_ReplayRealRawLog 端到端回放:如果 AGENTRE_REPLAY_CC_RAW 指向一份
// 真实 /tmp/cc-raw.log,把每一行喂给 parseLine,断言最终 EventDone.Usage 非零。
// 默认 env 未设跳过(CI / 其它开发机上没这个文件)。给 GLM repro 用,排查时打开。
func TestSession_ReplayRealRawLog(t *testing.T) {
	path := os.Getenv("AGENTRE_REPLAY_CC_RAW")
	if path == "" {
		t.Skip("set AGENTRE_REPLAY_CC_RAW to replay an actual raw log")
	}
	f, err := os.Open(path) //nolint:gosec // 测试 helper,path 来自 env。
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	s := &Session{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	var doneEv *Event
	var usageEmits int
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		evs, isResult := s.parseLine(line)
		for _, ev := range evs {
			if ev.Kind == EventUsage {
				usageEmits++
			}
		}
		if isResult {
			require.Len(t, evs, 1)
			cp := evs[0]
			doneEv = &cp
		}
	}
	require.NoError(t, sc.Err())
	require.NotNil(t, doneEv, "raw log 必须包含至少一个 result 帧")
	assert.NotEqual(t, 0, doneEv.Usage.PromptTokens, "回放真 log:PromptTokens 不该是 0")
	t.Logf("replay done: model=%q usage=%+v EventUsage_emits=%d",
		doneEv.Model, doneEv.Usage, usageEmits)
}

// TestSession_StreamEventSubagentMessageDeltaSkipped 锁住:subagent 内部 API call
// (parent_tool_use_id != "") 的 stream_event message_delta 用量不应影响主 agent
// 的 lastAssistantUsage —— 跟现有 assistant 帧的 subagent 过滤语义一致。
func TestSession_StreamEventSubagentMessageDeltaSkipped(t *testing.T) {
	s := &Session{}
	frames := []string{
		`{"type":"system","subtype":"init","session_id":"sx","model":"glm-5.1"}`,
		// 主 agent 一帧:真 usage
		`{"type":"stream_event","session_id":"sx","parent_tool_use_id":null,"event":{"type":"message_delta","delta":{},"usage":{"input_tokens":500,"output_tokens":20,"cache_read_input_tokens":0}}}`,
		// 然后跟一个 subagent 的 message_delta:input_tokens 很小(子会话上下文)
		`{"type":"stream_event","session_id":"sx","parent_tool_use_id":"toolu-A","event":{"type":"message_delta","delta":{},"usage":{"input_tokens":50,"output_tokens":10,"cache_read_input_tokens":0}}}`,
		`{"type":"result","subtype":"success","session_id":"sx"}`,
	}
	var doneEv *Event
	for _, line := range frames {
		evs, isResult := s.parseLine([]byte(line))
		if isResult {
			doneEv = &evs[0]
		}
	}
	require.NotNil(t, doneEv)
	assert.Equal(t, 500, doneEv.Usage.PromptTokens, "subagent message_delta 不能覆盖主 agent 的 500")
}

// TestSession_StatusFrameWithoutPermissionMode 前向兼容回归：CLI 可能在未来给
// system{subtype:"status"} 帧加别的字段（例如 status:running）但没有 permissionMode。
// 我们必须静默忽略，不能产生伪事件，也不能打断后续 result 帧。
func TestSession_StatusFrameWithoutPermissionMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakePassiveMode))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	ch, err := sess.Turn(ctx, "no-mode")
	require.NoError(t, err)

	var modeChanges int
	sawDone := false
	for ev := range ch {
		if ev.Kind == EventPermissionModeChanged {
			modeChanges++
		}
		if ev.Kind == EventDone {
			sawDone = true
		}
	}
	assert.Zero(t, modeChanges, "status 帧没有 permissionMode → 不抬事件")
	assert.True(t, sawDone, "result 帧仍要正常关闭 channel")
}

// TestSession_TurnReturnsExitErrWhenProcessDied 模拟 "session 已开但子进程
// 已经暴毙" 的边界场景：Turn 写 stdin 时拿 broken pipe，方法必须把 broken pipe
// 翻成真正的 ErrSessionNotFound（来自子进程 stderr）。
func TestSession_TurnReturnsExitErrWhenProcessDied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 直接构造 process + Session（绕开 OpenSession 的健康检查，模拟"健康检查
	// 后才发生的进程暴毙"——理论上这种 race 几乎不存在，但要给上层兜底兜得住）。
	p := newPipeProcess(t, ctx, nil, withExitCode(1), withStderr(resumeMissingStderr))
	require.True(t, p.hasExited(), "nil fakeCLI 构造的 process 应当立刻报 hasExited=true")

	sc := bufio.NewScanner(p.stdout)
	s := &Session{proc: p, scanner: sc}

	_, err := s.Turn(ctx, "hello")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSessionNotFound,
		"子进程死了之后 Turn 写 stdin 拿 broken pipe，应当被替换成真实退出错误")
}
