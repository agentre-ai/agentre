package claudecode

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readProcessStdout(t *testing.T, p *process) string {
	t.Helper()
	out := strings.Builder{}
	buf := make([]byte, 64)
	for {
		n, rerr := p.stdout.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if rerr != nil {
			if rerr != io.EOF {
				t.Fatalf("read stdout: %v", rerr)
			}
			break
		}
	}
	return out.String()
}

func runShellProcess(t *testing.T, script string, env map[string]string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p, err := startProcess(ctx, processSpec{
		binary: "/bin/sh",
		args:   []string{"-c", script},
		env:    env,
	})
	require.NoError(t, err)

	out := readProcessStdout(t, p)
	exit, _ := p.wait(ctx)
	require.Equal(t, 0, exit)
	return out
}

// TestProcess_StreamsStdoutAndWaitsForExit 用 /bin/sh -c 'printf ...' 作为 fake
// binary，验证 Start → 读 stdout → Wait 的链路。
func TestProcess_StreamsStdoutAndWaitsForExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p, err := startProcess(ctx, processSpec{
		binary: "/bin/sh",
		args:   []string{"-c", `printf 'a\nb\n'`},
		cwd:    "",
		env:    nil,
	})
	require.NoError(t, err)

	out := readProcessStdout(t, p)
	exitCode, _ := p.wait(ctx)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "a\nb\n", out)
}

// TestProcess_KillTerminatesWedgedSubprocess 钉死硬杀路径:对一个长睡、不读 stdin
// 的子进程(模拟 CLI 卡在 MCP 初始化、Close 关 stdin 救不回来)调 kill() 必须 SIGKILL
// 掉它,reaper 的 cmd.Wait 随即返回 → exit channel close,上层 readLoop 拿 EOF 解阻塞。
func TestProcess_KillTerminatesWedgedSubprocess(t *testing.T) {
	ctx := context.Background()
	p, err := startProcess(ctx, processSpec{
		binary: "/bin/sh",
		args:   []string{"-c", "sleep 60"},
	})
	require.NoError(t, err)
	require.False(t, p.hasExited(), "子进程应先存活")

	p.kill()

	select {
	case <-p.exit:
	case <-time.After(5 * time.Second):
		t.Fatal("kill() 没能终止长睡子进程")
	}
	assert.True(t, p.hasExited(), "kill 后 reaper 应已收尾")
}

// TestProcess_EnvInheritsOSEnviron 验证传入 spec.env 时不会把整个进程环境清空。
// claude CLI 依赖 HOME 找 ~/.claude/projects、PATH 找 git/bash 等；如果直接
// cmd.Env = envList 把 PATH/HOME 也丢掉，子进程会卡在初始化阶段不出任何 frame —
// 用户视角就是「发出去了但一直没返回消息」。
func TestProcess_EnvInheritsOSEnviron(t *testing.T) {
	t.Setenv("CLAUDECODE_TEST_INHERIT", "from_parent")

	// 同时 echo: 调用方注入的 key + 父进程继承的 key。两者都应该出现。
	out := runShellProcess(t,
		`printf '%s\n%s\n' "$ANTHROPIC_AUTH_TOKEN" "$CLAUDECODE_TEST_INHERIT"`,
		map[string]string{"ANTHROPIC_AUTH_TOKEN": "from_caller"},
	)
	assert.Equal(t, "from_caller\nfrom_parent\n", out,
		"子进程应同时拿到调用方注入的 env 和父进程继承的 env")
}

// TestProcess_EnvCallerOverridesOSEnviron 验证调用方传入的同名 key 会覆盖
// 父进程的值（execve 后者胜出）—— 比如让单元测试可以临时改 HOME。
func TestProcess_EnvCallerOverridesOSEnviron(t *testing.T) {
	t.Setenv("CLAUDECODE_TEST_OVERRIDE", "parent_value")

	out := runShellProcess(t,
		`printf '%s\n' "$CLAUDECODE_TEST_OVERRIDE"`,
		map[string]string{"CLAUDECODE_TEST_OVERRIDE": "caller_value"},
	)
	assert.Equal(t, "caller_value\n", out, "调用方注入的值应当覆盖父进程")
}

func TestProcess_BinaryMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := startProcess(ctx, processSpec{binary: "/definitely/not/a/real/binary-xyz"})
	assert.ErrorIs(t, err, ErrBinaryNotFound)
}

// TestBoundedBuffer_DropsOldestBytesOverCapacity 覆盖 stderr 超 64KB 时的丢前路径。
// 之前这条 Write 0% 覆盖，trim-front 算错就会静默截掉 stderr。
func TestBoundedBuffer_DropsOldestBytesOverCapacity(t *testing.T) {
	b := newBoundedBuffer(4)
	_, _ = b.Write([]byte("abcdefgh"))
	assert.Equal(t, "efgh", b.String())

	// 二次写入继续丢前：'efgh' + 'ijkl' → 末尾 4 字节 'ijkl'。
	_, _ = b.Write([]byte("ijkl"))
	assert.Equal(t, "ijkl", b.String())
}

// TestProcess_WaitClassifiesResumeMissingStderr 启动一个立刻写 stderr "No conversation found"
// 并 exit 1 的子进程，验证 wait() 返回的 err errors.Is ErrSessionNotFound。
// 这是 OpenSession 健康检查能识别 "resume 失效" 的最底层依据。
//
// 这条 test 是真子进程的集成验证（startProcess + reaper + classifyStderr 一条链），
// 故走 /bin/sh -c 起真进程，不走 pipeSpawner。
func TestProcess_WaitClassifiesResumeMissingStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p := mustStartFakeResumeMissing(t, ctx)
	code, werr := p.wait(ctx)
	assert.Equal(t, 1, code)
	require.Error(t, werr)
	assert.ErrorIs(t, werr, ErrSessionNotFound)
	assert.Contains(t, werr.Error(), "No conversation found")
}

// TestProcess_WaitIdempotent 多次 wait 必须返回同一个分类后错误，且不 hang。
// 上层（Session.Close 兜底 + 0-frame fallback 可能各自调一次 wait）需要这个保证。
func TestProcess_WaitIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p := mustStartFakeResumeMissing(t, ctx)
	code1, err1 := p.wait(ctx)
	code2, err2 := p.wait(ctx)
	assert.Equal(t, code1, code2)
	assert.ErrorIs(t, err1, ErrSessionNotFound)
	assert.ErrorIs(t, err2, ErrSessionNotFound)
}

// TestProcess_HasExitedAndExitErrIfDoneAliveProcess 健康存活的进程 hasExited 应当为 false
// 且 exitErrIfDone 返回 nil；关 stdin 触发退出后两个 accessor 必须反映已退出。
func TestProcess_HasExitedAndExitErrIfDoneAliveProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 健康常驻进程：只 read stdin 直到 EOF；不写 stdout/stderr。
	p, err := startProcess(ctx, processSpec{
		binary: "/bin/sh",
		args:   []string{"-c", `while IFS= read -r _line; do :; done`},
	})
	require.NoError(t, err)

	// 刚启动：fake 脚本在 `while read` 阻塞，进程必然存活。
	assert.False(t, p.hasExited(), "新起的健康进程不应当报 hasExited=true")
	assert.NoError(t, p.exitErrIfDone(), "存活进程的 exitErrIfDone 必须返 nil")

	// 关 stdin → 让 fake 脚本 `while read` 拿到 EOF 后正常退出（exit 0）。
	require.NoError(t, p.stdin.Close())

	// 等 reaper goroutine 抓到 exit。
	require.Eventually(t, p.hasExited, time.Second, 10*time.Millisecond,
		"stdin 关后 reaper 应当在百毫秒内拿到 exit")

	// 正常退出（exit 0）+ 无 stderr → exitErrIfDone 必须返 nil。
	assert.NoError(t, p.exitErrIfDone(), "exit 0 且无 stderr 时 exitErrIfDone 必须返 nil")
}

// TestProcess_HasExitedDetectsImmediateExit 启动后立刻 exit 1 的子进程，reaper
// goroutine 应当很快把 exit channel 关掉，让 OpenSession 的健康检查在 200ms
// 窗口内通过 select 拿到错误。
func TestProcess_HasExitedDetectsImmediateExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p := mustStartFakeResumeMissing(t, ctx)

	require.Eventually(t, p.hasExited, 500*time.Millisecond, 5*time.Millisecond,
		"立刻 exit 1 的进程必须在百毫秒内被 reaper 抓到")

	err := p.exitErrIfDone()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

// mustStartFakeResumeMissing 起一个真子进程模拟 `claude --resume <gone-id>` 行为：
// /bin/sh 一行写 stderr「No conversation found …」+ exit 1，让 classifyStderr
// 命中 ErrSessionNotFound sentinel。
func mustStartFakeResumeMissing(t *testing.T, ctx context.Context) *process {
	t.Helper()
	p, err := startProcess(ctx, processSpec{
		binary: "/bin/sh",
		args: []string{"-c",
			`echo "No conversation found with session ID: 07dcda59-d426-4d66-b6d3-12d6d59bc5a3" 1>&2; exit 1`,
		},
	})
	require.NoError(t, err)
	return p
}
