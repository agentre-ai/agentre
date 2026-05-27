package claudecode

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyStderrError_ConversationNotFound(t *testing.T) {
	stderr := "Error: Conversation not found for session id sess-xxx"
	err := classifyStderr(stderr, 1)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestClassifyStderrError_BinaryMissing(t *testing.T) {
	err := classifyExecErr(&exec.Error{Name: "claude", Err: errors.New("executable file not found in $PATH")})
	assert.ErrorIs(t, err, ErrBinaryNotFound)
}

func TestClassifyStderrError_UnknownPassThrough(t *testing.T) {
	stderr := "boom: surprise"
	err := classifyStderr(stderr, 2)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrSessionNotFound)
	assert.Contains(t, err.Error(), "boom: surprise")
}

// TestClassifyStderr_ReturnsTypedExitError 让上层用 errors.As 拿 exit code + stderr。
// 这是 prober 路径的契约（formatCLIProberError）。
func TestClassifyStderr_ReturnsTypedExitError(t *testing.T) {
	err := classifyStderr("config file missing", 42)

	var pe *ProcessExitError
	if assert.ErrorAs(t, err, &pe) {
		assert.Equal(t, 42, pe.Code)
		assert.Equal(t, "config file missing", pe.Stderr)
	}
}

func TestClassifyStderr_SessionNotFoundTakesPrecedenceOverExitError(t *testing.T) {
	err := classifyStderr("Error: Conversation not found for session id sess-xxx", 1)
	// ErrSessionNotFound 是 sentinel；errors.Is 必须命中。
	assert.ErrorIs(t, err, ErrSessionNotFound)
	// 不应当同时是 *ProcessExitError —— 让上层先 Is 再 As。
	var pe *ProcessExitError
	assert.False(t, errors.As(err, &pe), "ErrSessionNotFound should short-circuit before typed ProcessExitError")
}
