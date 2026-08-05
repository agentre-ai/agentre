package terminal_svc

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"
)

// CommandStartPreemptedError is the stable one-shot command outcome returned
// when TerminalClose or a newer start wins before command registration.
type CommandStartPreemptedError struct{}

func (CommandStartPreemptedError) Error() string {
	return "terminal command start preempted"
}

var (
	ErrInvalidRunCommandRequest           = errors.New("invalid terminal run command request")
	ErrCommandScopeResolverNotInitialized = errors.New("terminal command scope resolver not initialized")
	ErrCommandScopeUnavailable            = errors.New("terminal command scope unavailable")
	ErrCommandStartPreempted              = CommandStartPreemptedError{}
)

type commandStartStage string

const (
	commandStartStageUnknown       commandStartStage = "unknown"
	commandStartStageBackendSelect commandStartStage = "backendSelect"
	commandStartStagePTYOpen       commandStartStage = "ptyOpen"
)

type commandStartError struct {
	stage commandStartStage
	cause error
}

func (e *commandStartError) Error() string {
	return e.cause.Error()
}

func (e *commandStartError) Unwrap() error {
	return e.cause
}

type commandStartErrorCategory string

const (
	commandStartErrorCategoryNotFound         commandStartErrorCategory = "notFound"
	commandStartErrorCategoryPermissionDenied commandStartErrorCategory = "permissionDenied"
	commandStartErrorCategoryTimeout          commandStartErrorCategory = "timeout"
	commandStartErrorCategoryNetwork          commandStartErrorCategory = "network"
	commandStartErrorCategoryUnavailable      commandStartErrorCategory = "unavailable"
	commandStartErrorCategoryUnknown          commandStartErrorCategory = "unknown"
)

// CommandScope 是 terminal_svc 启动命令与返回 Wails 响应共享的设备/cwd 作用域。
type CommandScope struct {
	DeviceID string `json:"deviceId"`
	Cwd      string `json:"cwd"`
}

// ResolveCommandScopeRequest 是 terminal_svc 对执行作用域解析器的最小请求。
type ResolveCommandScopeRequest struct {
	SessionID int64
}

// CommandScopeResolver 由 composition root 注入，terminal_svc 不依赖作用域的生产服务。
type CommandScopeResolver func(
	ctx context.Context,
	req ResolveCommandScopeRequest,
) (*CommandScope, error)

// RunCommandRequest 包含启动一条本地命令所需的完整参数。
type RunCommandRequest struct {
	TerminalID string
	SessionID  int64
	Command    string
	Cols       uint16
	Rows       uint16
}

// RunCommandResponse 返回本次命令实际使用的执行作用域。命令启动错误通过
// StartError 返回，使调用方仍可使用已解析的准确作用域。
type RunCommandResponse struct {
	Scope      CommandScope `json:"scope"`
	StartError string       `json:"startError,omitempty"`
}

const (
	commandExitCodeUnavailable = -1
	commandExitReasonStopped   = "stopped"
	commandExitReasonReplaced  = "replaced"
	commandExitReasonShutdown  = "shutdown"
)

type commandLifecycle struct {
	ctx        context.Context
	sessionID  int64
	terminalID string
	deviceID   string

	started     atomic.Bool
	finalLogged atomic.Bool
}

func newCommandLifecycle(
	ctx context.Context,
	sessionID int64,
	terminalID string,
	deviceID string,
) *commandLifecycle {
	return &commandLifecycle{
		ctx:        context.WithoutCancel(ctx),
		sessionID:  sessionID,
		terminalID: terminalID,
		deviceID:   deviceID,
	}
}

func (l *commandLifecycle) logStarted() {
	logger.Ctx(l.ctx).Info("terminal_svc.RunCommand: command started",
		zap.Int64("sessionId", l.sessionID),
		zap.String("terminalId", l.terminalID),
		zap.String("deviceId", l.deviceID))
	l.started.Store(true)
}

func (l *commandLifecycle) logExited(exitCode int, exitReason string) bool {
	if !l.started.Load() || !l.finalLogged.CompareAndSwap(false, true) {
		return false
	}
	logger.Ctx(l.ctx).Info("terminal_svc.RunCommand: command exited",
		zap.Int64("sessionId", l.sessionID),
		zap.String("terminalId", l.terminalID),
		zap.String("deviceId", l.deviceID),
		zap.Int("exitCode", exitCode),
		zap.String("exitReason", exitReason))
	return true
}

// SetCommandScopeResolver 注入 session → 命令执行作用域的只读解析器。
func (s *Service) SetCommandScopeResolver(resolver CommandScopeResolver) {
	s.commandScopeResolver = resolver
}

// RunCommand 只解析一次执行作用域，并用该作用域只启动一次命令。
func (s *Service) RunCommand(ctx context.Context, req RunCommandRequest) (*RunCommandResponse, error) {
	if strings.TrimSpace(req.TerminalID) == "" || req.SessionID <= 0 ||
		strings.TrimSpace(req.Command) == "" || req.Cols == 0 || req.Rows == 0 {
		return nil, ErrInvalidRunCommandRequest
	}

	resolver := s.commandScopeResolver
	if resolver == nil {
		return nil, ErrCommandScopeResolverNotInitialized
	}

	attempt := s.claimStart(ctx, req.TerminalID)
	defer s.releaseStart(req.TerminalID, attempt)

	scope, err := resolver(attempt.ctx, ResolveCommandScopeRequest{SessionID: req.SessionID})
	if !s.ownsStart(req.TerminalID, attempt) {
		return nil, ErrCommandStartPreempted
	}
	if err != nil {
		return nil, err
	}
	if scope == nil {
		return nil, ErrCommandScopeUnavailable
	}
	response := &RunCommandResponse{Scope: *scope}
	lifecycle := newCommandLifecycle(ctx, req.SessionID, req.TerminalID, scope.DeviceID)
	if err := s.openCommand(
		ctx, attempt, req.TerminalID, scope.DeviceID, scope.Cwd, req.Command, req.Cols, req.Rows, lifecycle,
	); err != nil {
		response.StartError = err.Error()
		if errors.Is(err, ErrCommandStartPreempted) {
			return response, nil
		}
		logger.Ctx(ctx).Warn("terminal_svc.RunCommand: open command failed",
			zap.Int64("sessionId", req.SessionID),
			zap.String("terminalId", req.TerminalID),
			zap.String("deviceId", scope.DeviceID),
			zap.String("startStage", string(commandStartStageOf(err))),
			zap.String("errorCategory", string(classifyCommandStartError(err))),
			zap.String("errorClass", "terminalCommandStartFailed"))
		return response, nil
	}
	return response, nil
}

func annotateCommandStartError(stage commandStartStage, cause error) error {
	return &commandStartError{stage: stage, cause: cause}
}

func commandStartStageOf(err error) commandStartStage {
	var startErr *commandStartError
	if errors.As(err, &startErr) {
		return startErr.stage
	}
	return commandStartStageUnknown
}

func classifyCommandStartError(err error) commandStartErrorCategory {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return commandStartErrorCategoryNotFound
	case errors.Is(err, os.ErrPermission):
		return commandStartErrorCategoryPermissionDenied
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		return commandStartErrorCategoryTimeout
	case errors.Is(err, context.Canceled), errors.Is(err, net.ErrClosed), errors.Is(err, os.ErrClosed):
		return commandStartErrorCategoryUnavailable
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return commandStartErrorCategoryTimeout
		}
		return commandStartErrorCategoryNetwork
	}
	return commandStartErrorCategoryUnknown
}
