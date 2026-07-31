package piagent

import "errors"

var (
	ErrBinaryNotFound  = errors.New("piagent: pi binary not found in PATH or configured CLIPath")
	ErrProcessDead     = errors.New("piagent: process exited unexpectedly")
	ErrSessionNotFound = errors.New("piagent: provider session no longer exists")
)
