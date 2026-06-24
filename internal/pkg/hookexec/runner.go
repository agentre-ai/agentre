// Package hookexec 按声明的解释器执行脚本 Hook：写临时文件后起子进程，
// 注入 env、限时限输出。新增解释器 = 往 registry 加一条（OCP）。
package hookexec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

var (
	ErrUnknownInterpreter      = errors.New("hookexec: unknown interpreter")
	ErrInterpreterNotInstalled = errors.New("hookexec: interpreter not installed")
)

// ScriptRunner 执行一段脚本并返回采集到的输出。
type ScriptRunner interface {
	Run(ctx context.Context, spec RunSpec) (*RunResult, error)
}

// RunSpec 描述一次脚本执行所需的全部输入。
type RunSpec struct {
	Interpreter    string
	Command        string
	Env            map[string]string
	Timeout        time.Duration
	MaxOutputBytes int
}

// RunResult 是一次脚本执行的采集结果。
type RunResult struct {
	Stdout    []byte
	Stderr    []byte
	ExitCode  int
	Duration  time.Duration
	TimedOut  bool
	Truncated bool
}

// Interp 描述一个解释器如何被调用。
type Interp struct {
	Bin  string   // 已 LookPath 解析的绝对/可执行名
	Args []string // 脚本文件路径之前的固定参数
	Ext  string   // 临时脚本文件扩展名
}

type interpDef struct {
	candidates []string // 按序探测的二进制名
	args       []string
	ext        string
}

var registry = map[string]interpDef{
	"bash":       {candidates: []string{"bash"}, ext: ".sh"},
	"sh":         {candidates: []string{"sh"}, ext: ".sh"},
	"node":       {candidates: []string{"node"}, ext: ".mjs"},
	"python":     {candidates: []string{"python3", "python"}, ext: ".py"},
	"pwsh":       {candidates: []string{"pwsh"}, args: []string{"-NoProfile", "-File"}, ext: ".ps1"},
	"powershell": {candidates: []string{"powershell"}, args: []string{"-NoProfile", "-File"}, ext: ".ps1"},
	"cmd":        {candidates: []string{"cmd"}, args: []string{"/c"}, ext: ".bat"},
}

// Resolve 校验解释器并解析其二进制路径。
func Resolve(interpreter string) (*Interp, error) {
	def, ok := registry[interpreter]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownInterpreter, interpreter)
	}
	for _, name := range def.candidates {
		if bin, err := exec.LookPath(name); err == nil {
			return &Interp{Bin: bin, Args: def.args, Ext: def.ext}, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrInterpreterNotInstalled, interpreter)
}
