package agent_backend_entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// IsValidReasoningEffort 是 service / API 层做预校验的入口，
// 保持与 entity.Check 的 reasoning_effort 校验完全一致的取值集合。
func TestIsValidReasoningEffort(t *testing.T) {
	valid := []string{"", "low", "medium", "high", "xhigh", "max"}
	for _, v := range valid {
		assert.True(t, IsValidReasoningEffort(v), "expected %q to be valid", v)
	}

	invalid := []string{"LOW", "off", "none", "ultra", "1024", "high ", " medium"}
	for _, v := range invalid {
		assert.False(t, IsValidReasoningEffort(v), "expected %q to be invalid", v)
	}
}
