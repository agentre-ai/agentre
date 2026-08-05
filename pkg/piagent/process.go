package piagent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/agentre-ai/agentre/internal/pkg/clienv"
	"github.com/agentre-ai/agentre/internal/pkg/cliprocess"
	"github.com/agentre-ai/agentre/internal/pkg/procattr"
)

type ExitError struct {
	// Err is retained for compatibility, but errors produced by pkg/piagent store
	// only a normalized exit classification here. Raw command/process errors must
	// not cross the package boundary.
	Err error
	// Stderr is retained for compatibility with callers that construct ExitError
	// values themselves. pkg/piagent-produced errors always leave it empty.
	Stderr string
}

func (e *ExitError) Error() string {
	if e == nil {
		return ""
	}
	cause := normalizeProcessExitCause(e.Err)
	if cause == nil {
		return "piagent: process exited"
	}
	return fmt.Sprintf("piagent: process exited: %s", cause.Error())
}

func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return normalizeProcessExitCause(e.Err)
}

func (e *ExitError) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	cause := normalizeProcessExitCause(e.Err)
	if errors.Is(cause, target) {
		return true
	}
	targetCause := normalizeProcessExitCause(target)
	if classified, ok := targetCause.(*processExitCause); ok && classified.classification == "process failure" {
		return false
	}
	return cause != nil && targetCause != nil && cause.Error() == targetCause.Error()
}

type processExitCause struct {
	classification string
}

func (e *processExitCause) Error() string {
	if e == nil {
		return ""
	}
	return e.classification
}

func normalizeProcessExitCause(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, ErrBinaryNotFound) {
		return ErrBinaryNotFound
	}
	if errors.Is(err, ErrProcessDead) {
		return ErrProcessDead
	}
	if errors.Is(err, ErrSessionNotFound) {
		return ErrSessionNotFound
	}
	if classified, ok := err.(*processExitCause); ok {
		return classified
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return &processExitCause{classification: fmt.Sprintf("exit status %d", exitErr.ExitCode())}
	}
	text := strings.TrimSpace(err.Error())
	if isSafeExitStatus(text) || isSafeSignalStatus(text) {
		return &processExitCause{classification: text}
	}
	return &processExitCause{classification: "process failure"}
}

func isSafeExitStatus(text string) bool {
	const prefix = "exit status "
	if !strings.HasPrefix(text, prefix) {
		return false
	}
	code := strings.TrimPrefix(text, prefix)
	if code == "" || strings.TrimSpace(code) != code {
		return false
	}
	_, err := strconv.Atoi(code)
	return err == nil
}

func isSafeSignalStatus(text string) bool {
	const prefix = "signal: "
	if !strings.HasPrefix(text, prefix) {
		return false
	}
	signal := strings.TrimPrefix(text, prefix)
	if signal == "" || len(signal) > 32 {
		return false
	}
	for _, r := range signal {
		if (r < 'a' || r > 'z') && r != ' ' && r != '-' {
			return false
		}
	}
	return true
}

func processBoundaryError(operation, command string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, ErrBinaryNotFound) {
		return ErrBinaryNotFound
	}
	if errors.Is(err, ErrProcessDead) {
		return ErrProcessDead
	}
	if errors.Is(err, ErrSessionNotFound) {
		return ErrSessionNotFound
	}
	if operation == "read" && isFrameSafetyLimitError(err) {
		return errors.New("piagent: process read failed: token too long")
	}
	command = safeRPCCommand(command)
	if command == "request" {
		return fmt.Errorf("piagent: process %s failed", operation)
	}
	return fmt.Errorf("piagent: %s %s failed", command, operation)
}

func isFrameSafetyLimitError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "token too long")
}

func safeRPCCommand(command string) string {
	switch strings.TrimSpace(command) {
	case "get_state", "fork", "get_entries", "prompt", "compact", "get_session_stats", "get_commands", "steer", "abort":
		return strings.TrimSpace(command)
	default:
		return "request"
	}
}

func requestCommand(v any) string {
	request, ok := v.(map[string]any)
	if !ok {
		return "request"
	}
	command, _ := request["type"].(string)
	return safeRPCCommand(command)
}

func missingSessionIdentity(stderr string) (string, bool) {
	const marker = "no session found matching"
	trimmed := strings.TrimSpace(stderr)
	lower := strings.ToLower(trimmed)
	index := strings.Index(lower, marker)
	if index < 0 {
		return "", false
	}
	remainder := strings.TrimSpace(trimmed[index+len(marker):])
	if remainder == "" {
		return "", true
	}
	var candidate string
	if remainder[0] == '\'' || remainder[0] == '"' {
		quote := remainder[0]
		if end := strings.IndexByte(remainder[1:], quote); end >= 0 {
			candidate = remainder[1 : end+1]
		}
	} else {
		candidate = strings.Fields(remainder)[0]
	}
	candidate = strings.TrimSpace(candidate)
	if !isSafeSessionIdentity(candidate) {
		return "", true
	}
	return candidate, true
}

func isSafeSessionIdentity(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("-_.:/\\", r):
		default:
			return false
		}
	}
	return true
}

type procOptions = cliprocess.Options
type processHandle = cliprocess.Handle

type processRunner interface {
	Start(ctx context.Context, opts procOptions) (processHandle, error)
}

type execProcessRunner struct{}

type treeProcessHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (h *treeProcessHandle) Stdin() io.Writer  { return h.stdin }
func (h *treeProcessHandle) Stdout() io.Reader { return h.stdout }
func (h *treeProcessHandle) Stderr() io.Reader { return h.stderr }
func (h *treeProcessHandle) Wait() error       { return h.cmd.Wait() }

func (h *treeProcessHandle) Kill() error {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	return killProcessTree(h.cmd.Process)
}

func (h *treeProcessHandle) Signal(sig os.Signal) error {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	return signalProcessTree(h.cmd.Process, sig)
}

func (r execProcessRunner) Start(ctx context.Context, opts procOptions) (processHandle, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	extraEnv := processEnvListToMap(opts.Env)
	searchEnv := clienv.BuildEnv(extraEnv, opts.Binary)
	binary, ok := clienv.ResolveBinaryForEnv(opts.Binary, searchEnv)
	if !ok {
		return nil, ErrBinaryNotFound
	}
	// Do not bind the accepted Pi RPC process to the turn context. The stream owns
	// a short post-cancel settlement window and then terminates this process group.
	// #nosec G204 -- the configured Pi binary plus fixed RPC flags is intentionally launched.
	cmd := exec.Command(binary, opts.Args...)
	procattr.ApplyNoConsoleWindow(cmd)
	applyProcessTreeAttributes(cmd)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = clienv.BuildEnv(extraEnv, binary)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return nil, ErrBinaryNotFound
		}
		return nil, err
	}
	return &treeProcessHandle{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

func processEnvListToMap(items []string) map[string]string {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]string, len(items))
	for _, item := range items {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}

func applyProcessTreeAttributes(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	attrs := reflect.ValueOf(cmd.SysProcAttr)
	if attrs.Kind() != reflect.Pointer || attrs.IsNil() {
		return
	}
	attrs = attrs.Elem()
	if runtime.GOOS == "windows" {
		const createNewProcessGroup = 0x00000200
		if field := attrs.FieldByName("CreationFlags"); field.IsValid() && field.CanSet() {
			field.SetUint(field.Uint() | createNewProcessGroup)
		}
		return
	}
	if field := attrs.FieldByName("Setpgid"); field.IsValid() && field.CanSet() {
		field.SetBool(true)
	}
}

func signalProcessTree(process *os.Process, sig os.Signal) error {
	if process == nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		return process.Signal(sig)
	}
	signalNumber, ok := sig.(syscall.Signal)
	if !ok {
		return process.Signal(sig)
	}
	// #nosec G204 -- executable is fixed; arguments are the OS-assigned PID and signal number.
	if err := exec.Command(
		"/bin/kill",
		"-"+strconv.Itoa(int(signalNumber)),
		"-"+strconv.Itoa(process.Pid),
	).Run(); err == nil {
		return nil
	}
	return process.Signal(sig)
}

func killProcessTree(process *os.Process) error {
	if process == nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		// #nosec G204 -- executable is fixed; PID is assigned by the OS.
		if err := exec.Command("taskkill", "/PID", strconv.Itoa(process.Pid), "/T", "/F").Run(); err == nil {
			return nil
		}
		return ignoreProcessDone(process.Kill())
	}
	// #nosec G204 -- executable is fixed; argument is the OS-assigned process-group ID.
	if err := exec.Command("/bin/kill", "-9", "-"+strconv.Itoa(process.Pid)).Run(); err == nil {
		return nil
	}
	return ignoreProcessDone(process.Kill())
}

func ignoreProcessDone(err error) error {
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

type lockedBuffer = cliprocess.LockedBuffer
