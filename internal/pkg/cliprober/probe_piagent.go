package cliprober

import (
	"context"
	"os"
	"strings"

	"github.com/agentre-ai/agentre/pkg/piagent"
)

func probePiAgent(ctx context.Context, req ProbeRequest) (*ProbeResponse, error) {
	binary := strings.TrimSpace(req.CLIPath)
	if binary == "" {
		binary = "pi"
	}
	cwd, err := os.MkdirTemp("", "agentre-piagent-test-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(cwd) }()

	opts := []piagent.Option{
		piagent.WithBinary(binary),
		piagent.WithCwd(cwd),
		// 探测会话隔离在临时目录里（defer RemoveAll 清掉），不落进 ~/.pi。
		piagent.WithSessionDir(cwd),
		piagent.WithEnv(req.Env),
	}
	if model := strings.TrimSpace(req.Model); model != "" {
		opts = append(opts, piagent.WithModel(model))
	}
	r := piagent.New(opts...)
	defer func() { _ = r.Close(ctx) }()
	text, err := r.Text(ctx, fixedTestPrompt)
	if err != nil {
		return nil, wrapCLIProberError(err)
	}
	return &ProbeResponse{Text: text}, nil
}
