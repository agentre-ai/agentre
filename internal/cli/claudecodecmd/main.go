// Package claudecodecmd is the entry point for "agentre claudecode <…>"
// subcommands. agentre's main() routes early when os.Args[1] == "claudecode"
// so the agentre binary can serve as a CLI helper without booting the wails
// app.
package claudecodecmd

import (
	"fmt"
	"io"
	"os"

	"agentre/internal/pkg/claudecodehook"
)

// Main is the entry point. It runs the subcommand and exits the process.
// Never returns.
func Main(args []string) {
	exit := run(args, os.Stdin, os.Stdout, os.Stderr, os.LookupEnv)
	os.Exit(exit)
}

// run is the testable core. It returns the exit code; the env getter is
// injected so tests can pass a fake.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, lookupEnv func(string) (string, bool)) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "claudecode: missing subcommand (hook)")
		return 2
	}
	switch args[0] {
	case "hook":
		return runHook(args[1:], stdin, stdout, stderr, lookupEnv)
	default:
		_, _ = fmt.Fprintf(stderr, "claudecode: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runHook(args []string, stdin io.Reader, stdout, stderr io.Writer, lookupEnv func(string) (string, bool)) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "claudecode hook: missing event (post-tool)")
		return 2
	}
	event := args[0]
	// AGENTRE_GATEWAY_* 是 agentre runtime 永远会设的专用 env（CLI 登录模式也设），
	// 给 hook 子进程访问 /hook/v1/inbox 用。不复用 ANTHROPIC_* 是因为后者会强迫
	// claude CLI 用 Bearer 覆盖 OAuth 走 LLM 转发，CLI 登录模式下转发端没 provider
	// 配置，请求会直接挂。ANTHROPIC_* fallback 保留给：
	//   1. 老 agentre 二进制（hook settings.json 里写的还是老路径）；
	//   2. 用户手动用 launch-command 拷贝命令出来跑，且 backend 有 LLM provider。
	base, _ := lookupEnv("AGENTRE_GATEWAY_URL")
	tok, _ := lookupEnv("AGENTRE_GATEWAY_TOKEN")
	if base == "" || tok == "" {
		base, _ = lookupEnv("ANTHROPIC_BASE_URL")
		tok, _ = lookupEnv("ANTHROPIC_AUTH_TOKEN")
	}
	if base == "" || tok == "" {
		claudecodehook.EmitNoop(event, stdout)
		return 0
	}
	switch event {
	case "post-tool":
		claudecodehook.RunPostTool(base, tok, stdin, stdout)
	default:
		_, _ = fmt.Fprintf(stderr, "claudecode hook: unknown event %q\n", event)
		return 2
	}
	return 0
}
