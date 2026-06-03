package piagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/paths"
	"agentre/pkg/piagent"
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

// piAgentSessionsDir 是 Agentre 专用的 Pi session 存储目录：
//
//	<AppDataDir>/piagent/sessions/
//
// 独立于 Agent 工作目录（cwd），避免 Pi 把 session JSONL 写进用户项目里。
func piAgentSessionsDir() (string, error) {
	root, err := paths.AppDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "piagent", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// sessionFilePath 把 chat session id 映射到一个确定的 Pi session 文件路径，
// 让同一会话跨 turn 用相同路径 resume。sessionID<=0 或 dir 为空时返回空串，
// 表示不做 resume（如连通性探测）。
func sessionFilePath(dir string, sessionID int64) string {
	if dir == "" || sessionID <= 0 {
		return ""
	}
	return filepath.Join(dir, fmt.Sprintf("agentre-%d.jsonl", sessionID))
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
	opts := []piagent.Option{
		piagent.WithBinary(binary),
		piagent.WithCwd(cwd),
		piagent.WithEnv(env),
		piagent.WithModel(model),
		piagent.WithSystemPrompt(req.SystemPrompt),
		piagent.WithThinking(req.Backend.ReasoningEffort),
	}
	// 跨 turn 上下文：把 session 存到专用目录，并按 chat session id 解析出确定的
	// session 文件路径，Pi 第一轮新建、后续轮 resume。解析目录失败时退化为不
	// resume（仍能跑单轮），不阻断 turn。
	if sessionDir, derr := piAgentSessionsDir(); derr == nil {
		opts = append(opts, piagent.WithSessionDir(sessionDir))
		if path := sessionFilePath(sessionDir, req.SessionID); path != "" {
			opts = append(opts, piagent.WithSession(path))
		}
	}
	client := piagent.New(opts...)
	return &clientAdapter{client: client, sid: req.ProviderSessionID}, nil
}

func SetSessionFactoryForTest(fn func(agentruntime.RunRequest, map[string]string, string) (sessionHandle, error)) func() {
	old := sessionFactory
	sessionFactory = fn
	return func() { sessionFactory = old }
}
