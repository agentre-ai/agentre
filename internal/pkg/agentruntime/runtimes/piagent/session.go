package piagent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
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

type steerStream interface {
	Steer(ctx context.Context, text string) error
}

type interruptable interface {
	Interrupt(ctx context.Context) error
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
	// Resume 不在这里下发：会话复用走 Client 级 --session（WithSession），这里只
	// 负责本轮 prompt + 多模态图片 + 可选 permission mode。
	var opts []piagent.RunOption
	if strings.TrimSpace(mode) != "" {
		opts = append(opts, piagent.RunPermissionMode(piagent.PermissionMode(strings.TrimSpace(mode))))
	}
	if len(images) > 0 {
		opts = append(opts, piagent.WithImages(images))
	}
	s, err := a.client.Stream(ctx, prompt, opts...)
	if err != nil {
		return nil, err
	}
	a.sid = s.SessionID()
	a.streamMu.Lock()
	a.stream = s
	a.streamMu.Unlock()
	return s, nil
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
	Compact(ctx context.Context) (stream, error)
	RewindTo(ctx context.Context, anchor string) (string, error)
	ActiveStream() steerStream
	ActiveInterruptor() interruptable
}

// piRawFrameSink reports only safe metadata for each pi-agent stdout frame.
// The callback remains controlled by Debug Logging through logger.Default(), but
// complete RPC frames never enter the operational log sink.
func piRawFrameSink(sessionID int64, providerSessionID string) func([]byte) {
	return func(line []byte) {
		fields := []zap.Field{
			zap.Int64("sessionID", sessionID),
			zap.String("providerSessionID", providerSessionID),
			zap.Int("frameBytes", len(line)),
		}
		var head struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			fields = append(fields, zap.Bool("parseFailed", true))
		} else {
			if frameType, ok := safePiFrameType(head.Type); ok {
				fields = append(fields, zap.String("frameType", frameType))
			}
			if command, ok := safePiResponseCommand(head.Command); ok {
				fields = append(fields, zap.String("responseCommand", command))
			}
		}
		logger.Default().Debug("piagent.piRawFrameSink: frame observed", fields...)
	}
}

func safePiFrameType(frameType string) (string, bool) {
	switch frameType {
	case "response",
		"message_start", "message_update", "message_end",
		"agent_end", "agent_settled",
		"tool_execution_start", "tool_execution_update", "tool_execution_end",
		"compaction_start", "compaction_end", "auto_retry_start":
		return frameType, true
	default:
		return "", false
	}
}

func safePiResponseCommand(command string) (string, bool) {
	switch command {
	case "get_state", "prompt", "get_session_stats", "compact", "get_commands":
		return command, true
	default:
		return "", false
	}
}

// providerRunConfig 装配绑定供应商时的 provider 会话参数（APIKey 校验与 env 注入在
// Run 层完成，见 runtime.go）：返回 --model 值（effectiveModel = firstNonEmpty(
// modelOverride, p.Model) 非空时为 "agentre-<key>/<model>"）与物化后的 provider
// 扩展绝对路径。Provider.Model 与 override 均为空（保存时已拦截，此处仅兜底）时沿用
// 现状：返回零值不报错，不注入模型也不物化扩展。
// 模型名（Type 不可识别 / 模型空）出错一律显式返回，不静默吞掉后走无绑定运行。
func providerRunConfig(p *llm_provider_entity.LLMProvider, modelOverride string) (model string, extPath string, err error) {
	if p == nil {
		return "", "", nil
	}
	m := strings.TrimSpace(modelOverride)
	if m == "" {
		m = strings.TrimSpace(p.Model)
	}
	if m == "" {
		return "", "", nil
	}
	// 复用 PiAgentProviderModelName 的 "agentre-<key>/<model>" 拼装 + Type 校验；
	// override 替换 provider.Model 时用浅拷贝避免改共享实体。
	q := *p
	q.Model = m
	model, err = agentruntime.PiAgentProviderModelName(&q)
	if err != nil {
		return "", "", err
	}
	extPath, err = MaterializeProviderExtension(p)
	if err != nil {
		return "", "", err
	}
	return model, extPath, nil
}

// piModelFallback 未绑 provider（或 provider.Model 空）时的 --model 兜底：
// effectiveModel = firstNonEmpty(req.ModelOverride, defaultModelForBackend)。
// override 是裸 CLI 模型 id，直接作 --model 下发（走 pi 自身登录/配置），不经 agentre
// 网关（specs §模型解析与各后端生效 - 未绑 provider 路径）。
func piModelFallback(req agentruntime.RunRequest) string {
	if m := strings.TrimSpace(req.ModelOverride); m != "" {
		return m
	}
	return defaultModelForBackend(req.Backend)
}

var sessionFactory = func(req agentruntime.RunRequest, env map[string]string, cwd string) (sessionHandle, error) {
	binary := strings.TrimSpace(req.Backend.CLIPath)
	if binary == "" {
		binary = DefaultBinary()
	}
	model := ""
	var providerExtPath string
	if req.Provider != nil {
		var err error
		model, providerExtPath, err = providerRunConfig(req.Provider, req.ModelOverride)
		if err != nil {
			return nil, err
		}
	}
	if model == "" {
		model = piModelFallback(req)
	}
	// MCP 注入：有 RunRequest.MCPServers 时，materialize 内嵌桥扩展 + 渲染会话私有
	// config，扩展路径走 --extension、config 路径走 AGENTRE_PI_MCP_CONFIG env。
	var extPaths []string
	if len(req.MCPServers) > 0 {
		p, err := mcpbridge.Materialize()
		if err != nil {
			return nil, err
		}
		cfgPath, err := mcpbridge.RenderConfig(req.MCPServers, req.SessionID)
		if err != nil {
			return nil, err
		}
		extPaths = append(extPaths, p)
		env = withEnv(env, mcpbridge.ConfigEnvVar, cfgPath)
	}
	// 绑定供应商时：物化 provider 扩展（pi.registerProvider，内容哈希无密钥），
	// 与 MCP 桥扩展并列追加 --extension。
	if providerExtPath != "" {
		extPaths = append(extPaths, providerExtPath)
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
	for _, ep := range extPaths {
		opts = append(opts, piagent.WithExtension(ep))
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
