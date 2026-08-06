package piagent

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/piagent/mcpbridge"
	"github.com/agentre-ai/agentre/pkg/piagent"
)

type stream interface {
	Next() bool
	Event() piagent.Event
	SessionID() string
	Err() error
}

type userAnchorStream interface {
	UserAnchor() string
}

type turnSpec struct {
	forkAnchor string
}

type steerStream interface {
	Steer(ctx context.Context, text string) error
}

type interruptable interface {
	Interrupt(ctx context.Context) error
}

type preparedTurnStream interface {
	Start(context.Context) (stream, error)
	SessionID() string
	Close(context.Context) error
}

type turnStreamPreparer interface {
	PrepareStreamTurn(ctx context.Context, prompt string, mode string, images []piagent.Image, turn turnSpec) (preparedTurnStream, error)
}

type clientAdapter struct {
	client *piagent.Client
	sid    string

	streamMu sync.Mutex
	stream   *piagent.Stream
}

func (a *clientAdapter) ID() string { return a.sid }
func (a *clientAdapter) Close(ctx context.Context) error {
	a.streamMu.Lock()
	stream := a.stream
	a.stream = nil
	a.streamMu.Unlock()
	if stream != nil {
		if err := stream.Close(ctx); err != nil {
			return err
		}
	}
	return a.client.Close(ctx)
}

func (a *clientAdapter) Stream(ctx context.Context, prompt string, mode string, images []piagent.Image) (stream, error) {
	return a.startStream(ctx, prompt, mode, images, nil)
}

func (a *clientAdapter) StreamTurn(ctx context.Context, prompt string, mode string, images []piagent.Image, turn turnSpec) (stream, error) {
	return a.startStream(ctx, prompt, mode, images, &turn)
}

func (a *clientAdapter) startStream(ctx context.Context, prompt string, mode string, images []piagent.Image, turn *turnSpec) (stream, error) {
	opts, err := turnRunOptions(mode, images, turn)
	if err != nil {
		return nil, err
	}
	s, err := a.client.Stream(ctx, prompt, opts...)
	if err != nil {
		return nil, err
	}
	a.setActiveStream(s)
	return s, nil
}

func (a *clientAdapter) PrepareStreamTurn(
	ctx context.Context,
	prompt string,
	mode string,
	images []piagent.Image,
	turn turnSpec,
) (preparedTurnStream, error) {
	opts, err := turnRunOptions(mode, images, &turn)
	if err != nil {
		return nil, err
	}
	prepared, err := a.client.PrepareStream(ctx, prompt, opts...)
	if err != nil {
		return nil, err
	}
	a.sid = prepared.SessionID()
	return &clientPreparedTurn{adapter: a, prepared: prepared}, nil
}

func turnRunOptions(mode string, images []piagent.Image, turn *turnSpec) ([]piagent.RunOption, error) {
	// Resume 不在这里下发：会话复用走 Client 级 --session（WithSession）。每个
	// runtime turn 都记录原生 user anchor；分叉 turn 由同一个 per-turn option
	// 在当前 RPC 进程里先 fork，再发送 prompt。
	var opts []piagent.RunOption
	if turn != nil {
		switch {
		case turn.forkAnchor == "":
			opts = append(opts, piagent.RunCaptureUserAnchor())
		case strings.TrimSpace(turn.forkAnchor) != turn.forkAnchor:
			return nil, errors.New("piagent runtime: invalid fork anchor")
		default:
			opts = append(opts, piagent.RunForkAnchor(turn.forkAnchor))
		}
	}
	if strings.TrimSpace(mode) != "" {
		opts = append(opts, piagent.RunPermissionMode(piagent.PermissionMode(strings.TrimSpace(mode))))
	}
	if len(images) > 0 {
		opts = append(opts, piagent.WithImages(images))
	}
	return opts, nil
}

func (a *clientAdapter) setActiveStream(s *piagent.Stream) {
	a.sid = s.SessionID()
	a.streamMu.Lock()
	a.stream = s
	a.streamMu.Unlock()
}

type clientPreparedTurn struct {
	adapter  *clientAdapter
	prepared *piagent.PreparedStream
}

func (p *clientPreparedTurn) Start(ctx context.Context) (stream, error) {
	s, err := p.prepared.Start(ctx)
	if err != nil {
		return nil, err
	}
	p.adapter.setActiveStream(s)
	return s, nil
}

func (p *clientPreparedTurn) SessionID() string { return p.prepared.SessionID() }
func (p *clientPreparedTurn) Close(ctx context.Context) error {
	return p.prepared.Close(ctx)
}

func (a *clientAdapter) Compact(ctx context.Context) (stream, error) {
	s, err := a.client.Compact(ctx, a.sid)
	if err != nil {
		return nil, err
	}
	a.sid = s.SessionID()
	a.streamMu.Lock()
	a.stream = s
	a.streamMu.Unlock()
	return s, nil
}

func (a *clientAdapter) RewindTo(context.Context, string) (string, error) {
	return "", agentruntime.ErrUnsupported
}

func (a *clientAdapter) ActiveStream() steerStream {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if a.stream == nil {
		return nil
	}
	return a.stream
}

func (a *clientAdapter) ActiveInterruptor() interruptable {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if a.stream == nil {
		return nil
	}
	return a.stream
}

type sessionHandle interface {
	Close(context.Context) error
	ID() string
	Stream(ctx context.Context, prompt string, mode string, images []piagent.Image) (stream, error)
	StreamTurn(ctx context.Context, prompt string, mode string, images []piagent.Image, turn turnSpec) (stream, error)
	Compact(ctx context.Context) (stream, error)
	RewindTo(ctx context.Context, anchor string) (string, error)
	ActiveStream() steerStream
	ActiveInterruptor() interruptable
}

// piRawFrameSink 返回一个把 pkg/piagent 已脱敏的 stdout 诊断摘要打到 debug 日志的回调。
// prompt、图片、Session 内容和凭证不会进入这里；摘要由「Debug Logging」开关热控
// (关时 zap 直接丢弃,近零开销),用 logger.Default() 取当前全局 logger 故热重载即时生效。
func piRawFrameSink(sessionID int64, providerSessionID string) func([]byte) {
	return func(line []byte) {
		logger.Default().Debug("piagent runtime: raw frame",
			zap.Int64("sessionID", sessionID),
			zap.String("providerSessionID", providerSessionID),
			zap.ByteString("frame", line))
	}
}

var sessionFactory = func(req agentruntime.RunRequest, env map[string]string, cwd string) (sessionHandle, error) {
	binary := strings.TrimSpace(req.Backend.CLIPath)
	if binary == "" {
		binary = DefaultBinary()
	}
	model := ""
	if req.Provider != nil {
		model = strings.TrimSpace(req.Provider.Model)
	}
	if model == "" {
		model = defaultModelForBackend(req.Backend)
	}
	// MCP 注入：有 RunRequest.MCPServers 时，materialize 内嵌桥扩展 + 渲染会话私有
	// config，扩展路径走 --extension、config 路径走 AGENTRE_PI_MCP_CONFIG env。
	var extPath string
	if len(req.MCPServers) > 0 {
		p, err := mcpbridge.Materialize()
		if err != nil {
			return nil, err
		}
		cfgPath, err := mcpbridge.RenderConfig(req.MCPServers, req.SessionID)
		if err != nil {
			return nil, err
		}
		extPath = p
		env = withEnv(env, mcpbridge.ConfigEnvVar, cfgPath)
	}
	opts := []piagent.Option{
		piagent.WithBinary(binary),
		piagent.WithCwd(cwd),
		piagent.WithEnv(env),
		piagent.WithModel(model),
		piagent.WithSystemPrompt(req.SystemPrompt),
		piagent.WithThinking(req.Backend.ReasoningEffort),
		piagent.WithRawSink(piRawFrameSink(req.SessionID, req.ProviderSessionID)),
	}
	if extPath != "" {
		opts = append(opts, piagent.WithExtension(extPath))
	}
	// 跨 turn 上下文由 Pi 原生 Session ID 绑定。首轮不下发任何 Session flag，
	// 让 Pi 遵循自己的默认/用户配置存储；后续轮仅用 --session <native-id> 恢复。
	if sessionID := strings.TrimSpace(req.ProviderSessionID); sessionID != "" {
		opts = append(opts, piagent.WithSession(sessionID))
	}
	client := piagent.New(opts...)
	return &clientAdapter{client: client, sid: req.ProviderSessionID}, nil
}

func SetSessionFactoryForTest(fn func(agentruntime.RunRequest, map[string]string, string) (sessionHandle, error)) func() {
	old := sessionFactory
	sessionFactory = fn
	return func() { sessionFactory = old }
}

// withEnv 返回 env 的副本并设置一个键，避免就地改调用方的 map。
func withEnv(env map[string]string, key, val string) map[string]string {
	out := make(map[string]string, len(env)+1)
	for k, v := range env {
		out[k] = v
	}
	out[key] = val
	return out
}
