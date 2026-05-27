package claudecode

import (
	"context"
	"errors"
	"strings"

	"agentre/internal/pkg/agentruntime"
	"agentre/pkg/claudecode"
)

// ccStream 是 pkg/claudecode.Stream 的窄接口,便于测试注入 fake。
type ccStream interface {
	Next() bool
	Event() claudecode.Event
	SessionID() string
}

// ccSessionHandle 包装一次 Stream + Close + 控制协议入口(Interrupt /
// SetPermissionMode / RespondToControl)。
type ccSessionHandle interface {
	Close(context.Context) error
	ID() string
	Stream(ctx context.Context, prompt string) (ccStream, error)
	// RespondToControl 配对响应 claude 端发的 control_request{subtype:"can_use_tool"}。
	// 由 control dispatcher / answer sink 触发;其它场景不调用。
	RespondToControl(ctx context.Context, requestID string, result claudecode.PermissionResult) error
	// Interrupt 发 control_request{subtype:"interrupt"} 软中断当前 turn。
	// CLI 回 control_response 后 Session 收到 result 帧让本轮 Turn 自然返 done,
	// **子进程保留**。失败时 runner 走 Close + cache.Remove 兜底。
	Interrupt(ctx context.Context) error
	// SetPermissionMode 写一帧 control_request{subtype:"set_permission_mode"}
	// 让 CLI 在两个 Turn 之间切换 permission mode。mode 取
	// {default, acceptEdits, plan, bypassPermissions}。只能在 Turn 之间调用,
	// 期间该方法会阻塞到当前 Turn 收尾。
	SetPermissionMode(ctx context.Context, mode string) error
	// ExitErr 子进程已退出时返其分类后的退出错误(如 claudecode.ErrSessionNotFound
	// 或 *claudecode.ProcessExitError);还活着 / 没 spawn 返 nil。
	// 0-frame fallback 用它替换 "subprocess produced no events" 通用消息,
	// 让 chat_svc 能 errors.Is 出准确语义。
	ExitErr() error
}

// ccLaunchSpec 是 ccSessionFactory 的全部入参;具名结构体避免每次新增可选
// 参数就改一遍签名。
type ccLaunchSpec struct {
	Req      agentruntime.RunRequest
	Env      map[string]string
	Cwd      string
	Settings string // 非空时透传 --settings <value>;CLI 接受 JSON 字符串或文件路径
	// SessionUUID 非空时透传 --session-id <uuid>
	SessionUUID    string
	PermissionMode string // 非空时透传 --permission-mode <mode>;空 = 走 args.go 默认
	// DefaultPermissionMode 是 spawn CLI 子进程时下发给 --permission-mode 的备选值。
	// 空串 → 由 pkg/claudecode 兜底(acceptEdits)。优先级低于 spec.PermissionMode。
	DefaultPermissionMode string
}

// ccClientAdapter 把 *claudecode.Session 适配成 ccSessionHandle。
// 与之前每个 turn spawn 一次不同:现在 OpenSession 在 factory 里发生一次,
// Stream 实际上调用 Session.Turn,跨多个 turn 复用同一个子进程。
type ccClientAdapter struct {
	sess *claudecode.Session
	sid  string // 由 OpenSession 时的 --session-id 决定;首个 turn 后用 sess.SessionID() 覆盖
}

func (a *ccClientAdapter) ID() string { return a.sid }

func (a *ccClientAdapter) Close(ctx context.Context) error {
	if a.sess == nil {
		return nil
	}
	return a.sess.Close(ctx)
}

// Interrupt 把 control_request{interrupt} 写到 CLI stdin;CLI 软中断当前 turn
// 后会发 result 帧让 Session.Turn 自然收尾。**子进程不动**。
func (a *ccClientAdapter) Interrupt(ctx context.Context) error {
	if a.sess == nil {
		return nil
	}
	return a.sess.Interrupt(ctx)
}

// SetPermissionMode 转发到底层 claudecode.Session.SetPermissionMode。抢 turnMu,
// 所以会阻塞到当前 Turn 收尾 —— caller 不应该在 Stream 还没 drain 完的状态下
// 调用,否则要等到当前 turn 自然 done。
func (a *ccClientAdapter) SetPermissionMode(ctx context.Context, mode string) error {
	if a.sess == nil {
		return errors.New("agentruntime/runtimes/claudecode: session not opened")
	}
	return a.sess.SetPermissionMode(ctx, mode)
}

// ExitErr 透传 claudecode.Session.ExitErr。
func (a *ccClientAdapter) ExitErr() error {
	if a.sess == nil {
		return nil
	}
	return a.sess.ExitErr()
}

// RespondToControl 转发到底层 claudecode.Session。stdinMu 由 Session 内部保护,
// 多个并发 control_request 可以串行写。
func (a *ccClientAdapter) RespondToControl(ctx context.Context, requestID string, result claudecode.PermissionResult) error {
	if a.sess == nil {
		return errors.New("agentruntime/runtimes/claudecode: session not opened")
	}
	return a.sess.RespondToControl(ctx, requestID, result)
}

// Stream 在持久化 session 上发起一轮 Turn,把 Session.Turn 返回的 <-chan Event
// 转成 ccStream iterator,让 drain 逻辑那侧不动。
func (a *ccClientAdapter) Stream(ctx context.Context, prompt string) (ccStream, error) {
	ch, err := a.sess.Turn(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &ccChanStream{ch: ch, sidFn: a.sess.SessionID}, nil
}

// ccChanStream 把 <-chan claudecode.Event 适配成 ccStream(Next/Event/SessionID)。
// 一个 channel 对应一个 turn;result 帧到达后 channel 关闭,Next 返回 false。
type ccChanStream struct {
	ch    <-chan claudecode.Event
	cur   claudecode.Event
	sidFn func() string
}

func (s *ccChanStream) Next() bool {
	ev, ok := <-s.ch
	if !ok {
		return false
	}
	s.cur = ev
	return true
}

func (s *ccChanStream) Event() claudecode.Event { return s.cur }

func (s *ccChanStream) SessionID() string {
	if s.sidFn != nil {
		return s.sidFn()
	}
	return ""
}

// resolveLaunchMode 选 --permission-mode 值。优先级:用户 turn override (perTurn)
// → backend admin default (backendDefault) → ""。
//
// 例外: backendDefault == "bypassPermissions" 时 launch 永远锁 bypass —— 这是
// 「先 plan 后 bypass」工作流的承重柱: bypass-lockout 按 permission_mode_at_launch
// 判定, 必须 = bypass 才能解锁运行时切回 bypass; 同时 PlanApproveCard 主按钮也
// 按 launch == bypass 决定显示「批准并跳过权限确认」。stored mode 与 launch 不
// 一致时, spawn 后由 runtime 主动发 SetPermissionMode(perTurn) 把 CLI 校准到
// 用户当前选的 mode。
func resolveLaunchMode(perTurn, backendDefault string) string {
	if backendDefault == "bypassPermissions" {
		return "bypassPermissions"
	}
	if perTurn != "" {
		return perTurn
	}
	return backendDefault
}

// ccSessionFactory 由 init 写为真实路径;测试通过 SetSessionFactoryForTest 替换。
//
// 每个 chat session 调用一次(首轮或 fork 时),spawn 一个常驻 claude 子进程。
// runner 会缓存返回的 handle 给后续 Turn 复用。
// ccBuildClientOpts 把 ccLaunchSpec 翻译成 claudecode.Client 选项列表。抽成
// 独立函数是为了让单测在不 spawn 真子进程的前提下断言「绑了 provider 的后端
// 会下发 --model」这条不变量(spec §B token contract;Bug 1 防回归)。
// binary 由 caller 决定:真路径走 ccSessionFactory 解析,测试可以传 stub 串。
func ccBuildClientOpts(spec ccLaunchSpec, binary string) []claudecode.Option {
	opts := []claudecode.Option{
		claudecode.WithBinary(binary),
		claudecode.WithCwd(spec.Cwd),
		claudecode.WithEnv(spec.Env),
		claudecode.WithSystemPrompt(spec.Req.SystemPrompt),
		// 启用 stdio control protocol:把 AskUserQuestion 这种交互式工具的
		// permission gate 从 CLI 的 TUI 拉到 agentre UI;headless 下不开
		// 这个 flag,AskUserQuestion 会被 CLI 自动 deny,turn 直接挂掉。
		claudecode.WithPermissionPromptTool("stdio"),
	}
	// 绑了 LLM provider 的 claudecode 后端(GLM / openrouter 等非 Anthropic
	// 直连场景):必须把 provider.Model 下发成 --model,CLI 才能在 system.init
	// 帧里报真实模型 id,result.Model → assistantMsg.Model 链才能写对。不传时
	// CLI 落到本地登录态默认 model(如 claude-opus-4-7),经 gateway 透明改写
	// 仍能调通 LLM 但 UI 显示错。
	if spec.Req.Provider != nil {
		if model := strings.TrimSpace(spec.Req.Provider.Model); model != "" {
			opts = append(opts, claudecode.WithModel(model))
		}
	}
	if spec.SessionUUID != "" {
		opts = append(opts, claudecode.WithSessionID(spec.SessionUUID))
	}
	if spec.Settings != "" {
		opts = append(opts, claudecode.WithSettings(spec.Settings))
	}
	if mode := resolveLaunchMode(spec.PermissionMode, spec.DefaultPermissionMode); mode != "" {
		opts = append(opts, claudecode.WithPermissionMode(mode))
	}
	if eff := spec.Req.Backend.ReasoningEffort; eff != "" {
		opts = append(opts, claudecode.WithEffort(eff))
	}
	return opts
}

var ccSessionFactory = func(spec ccLaunchSpec) (ccSessionHandle, error) {
	binary := strings.TrimSpace(spec.Req.Backend.CLIPath)
	if binary == "" {
		binary = DefaultBinary()
	}
	client := claudecode.New(ccBuildClientOpts(spec, binary)...)

	var runOpts []claudecode.RunOption
	if spec.Req.ProviderSessionID != "" {
		runOpts = append(runOpts, claudecode.Resume(spec.Req.ProviderSessionID))
	}
	if spec.Req.ForkAnchor != "" {
		runOpts = append(runOpts, claudecode.ResumeSessionAt(spec.Req.ForkAnchor), claudecode.ForkSession())
	}

	sess, err := client.OpenSession(context.Background(), runOpts...)
	if err != nil {
		return nil, err
	}
	sid := spec.Req.ProviderSessionID
	if sid == "" {
		sid = spec.SessionUUID
	}
	return &ccClientAdapter{sess: sess, sid: sid}, nil
}

// SetSessionFactoryForTest 仅测试用;restore 闭包恢复默认。
func SetSessionFactoryForTest(fn func(ccLaunchSpec) (ccSessionHandle, error)) func() {
	old := ccSessionFactory
	ccSessionFactory = fn
	return func() { ccSessionFactory = old }
}
