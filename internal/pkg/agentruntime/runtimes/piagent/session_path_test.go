package piagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 同一 chat session 跨 turn 必须解析到同一个 Pi session 文件，才能 resume 上下文。
func TestSessionFilePathIsDeterministicPerSession(t *testing.T) {
	p1 := sessionFilePath("/data/pi-sessions", 7)
	p2 := sessionFilePath("/data/pi-sessions", 7)

	assert.Equal(t, "/data/pi-sessions/agentre-7.jsonl", p1)
	assert.Equal(t, p1, p2, "same session id → same path → resume")
	assert.NotEqual(t, p1, sessionFilePath("/data/pi-sessions", 8))
}

// 没有有效 session id（如连通性探测）时不强制 resume，返回空串。
func TestSessionFilePathEmptyWithoutSessionID(t *testing.T) {
	assert.Equal(t, "", sessionFilePath("/data/pi-sessions", 0))
	assert.Equal(t, "", sessionFilePath("", 7))
}
