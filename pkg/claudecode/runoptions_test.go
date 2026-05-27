package claudecode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRunOptions_SettersFillRunSpec 单元覆盖 Resume / ForkSession 两个 closure setter，
// 原来 args_test 通过 runSpec 直接构造，这里补 closure path 的契约。
func TestRunOptions_SettersFillRunSpec(t *testing.T) {
	var s runSpec
	Resume("sid-x")(&s)
	ResumeSessionAt("uuid-y")(&s)
	ForkSession()(&s)

	assert.Equal(t, "sid-x", s.resumeID)
	assert.Equal(t, "uuid-y", s.resumeSessionAtUUID)
	assert.True(t, s.forkSession)
}

func TestRunOptions_MaxTurns(t *testing.T) {
	var s runSpec
	MaxTurns(3)(&s)
	assert.Equal(t, 3, s.maxTurns)

	// n=0 不改变 spec（buildArgs 也不发 --max-turns），交给 CLI 默认值。
	var s2 runSpec
	MaxTurns(0)(&s2)
	assert.Equal(t, 0, s2.maxTurns)
}
