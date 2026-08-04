package terminal_svc

import (
	"context"
	"errors"
	"strings"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"
)

var (
	ErrInvalidRunCommandRequest           = errors.New("invalid terminal run command request")
	ErrCommandScopeResolverNotInitialized = errors.New("terminal command scope resolver not initialized")
	ErrCommandScopeUnavailable            = errors.New("terminal command scope unavailable")
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
	scope, err := resolver(ctx, ResolveCommandScopeRequest{SessionID: req.SessionID})
	if err != nil {
		return nil, err
	}
	if scope == nil {
		return nil, ErrCommandScopeUnavailable
	}
	response := &RunCommandResponse{Scope: *scope}
	if err := s.OpenCommand(
		ctx, req.TerminalID, scope.DeviceID, scope.Cwd, req.Command, req.Cols, req.Rows,
	); err != nil {
		logger.Ctx(ctx).Warn("terminal_svc.RunCommand: open command failed",
			zap.Int64("sessionId", req.SessionID),
			zap.String("terminalId", req.TerminalID),
			zap.String("deviceId", scope.DeviceID),
			zap.String("errorClass", "terminalCommandStartFailed"))
		response.StartError = err.Error()
		return response, nil //nolint:nilerr // startup failure is surfaced via response.StartError, not as an RPC error
	}
	return response, nil
}
