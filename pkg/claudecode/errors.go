package claudecode

import (
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"
)

// Sentinel errors —— 上层（agentruntime / chat_svc）用 errors.Is 判断。
var (
	ErrBinaryNotFound  = errors.New("claudecode: claude binary not found in PATH or configured CLIPath")
	ErrSessionNotFound = errors.New("claudecode: provider session no longer exists")
	ErrSchemaUnknown   = errors.New("claudecode: stream-json schema unrecognized; please update agentre")
)

// ProcessExitError 子进程非 0 退出时的结构化错误，便于上层（agent_backend_svc/prober）
// 用 errors.As 精确拿到 exit code 和 stderr 文本。
//
// 注意：当 stderr 命中 "Conversation not found" 这类 sentinel 串时仍优先返回
// fmt.Errorf("%w: ...", ErrSessionNotFound, ...) —— 上层先 errors.Is，再 errors.As。
type ProcessExitError struct {
	Code   int
	Stderr string
}

func (e *ProcessExitError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("claudecode: exit %d", e.Code)
	}
	return fmt.Sprintf("claudecode: exit %d: %s", e.Code, e.Stderr)
}

// classifyStderr 把 stderr 文本 + exit code 映射到 sentinel error。未识别且 exit
// 非 0 时返回 *ProcessExitError 让上层结构化解析。
func classifyStderr(stderr string, exitCode int) error {
	low := strings.ToLower(stderr)
	// 真实 Claude Code CLI（resume 失效）输出："No conversation found with session ID: <id>"
	// 历史变体保留兜底：旧版本 / SDK 直连可能写 "Conversation not found ..."。
	if strings.Contains(low, "no conversation found") ||
		strings.Contains(low, "conversation not found") ||
		strings.Contains(low, "no resumable conversation") {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, strings.TrimSpace(stderr))
	}
	if exitCode == 0 {
		return nil
	}
	return &ProcessExitError{Code: exitCode, Stderr: strings.TrimSpace(stderr)}
}

// classifyExecErr 区分启动失败（二进制找不到）与运行时失败。
// PATH 查找失败返回 *exec.Error；绝对路径不存在则返回 *fs.PathError。
func classifyExecErr(err error) error {
	if err == nil {
		return nil
	}
	var ee *exec.Error
	if errors.As(err, &ee) {
		if strings.Contains(ee.Err.Error(), "not found") {
			return fmt.Errorf("%w: %s", ErrBinaryNotFound, ee.Name)
		}
	}
	var pe *fs.PathError
	if errors.As(err, &pe) && errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrBinaryNotFound, pe.Path)
	}
	return err
}
