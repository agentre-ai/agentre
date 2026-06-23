package bootstrap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// proxyPortFromEnv 是 gateway 监听端口的环境变量覆盖(最高优先级)。e2e 用 AGENTRE_PROXY_PORT=0
// 让 OS 选空闲端口,避免与已运行的正式 Agentre(固定 52401)抢端口、导致 gateway 软降级。
func TestProxyPortFromEnv(t *testing.T) {
	t.Run("未设置 → 无覆盖", func(t *testing.T) {
		t.Setenv("AGENTRE_PROXY_PORT", "")
		_, ok := proxyPortFromEnv()
		assert.False(t, ok)
	})
	t.Run("0 → 临时端口覆盖(e2e)", func(t *testing.T) {
		t.Setenv("AGENTRE_PROXY_PORT", "0")
		p, ok := proxyPortFromEnv()
		assert.True(t, ok)
		assert.Equal(t, 0, p)
	})
	t.Run("合法端口 → 覆盖", func(t *testing.T) {
		t.Setenv("AGENTRE_PROXY_PORT", "52417")
		p, ok := proxyPortFromEnv()
		assert.True(t, ok)
		assert.Equal(t, 52417, p)
	})
	t.Run("非数字 → 无覆盖", func(t *testing.T) {
		t.Setenv("AGENTRE_PROXY_PORT", "abc")
		_, ok := proxyPortFromEnv()
		assert.False(t, ok)
	})
	t.Run("超出范围 → 无覆盖", func(t *testing.T) {
		t.Setenv("AGENTRE_PROXY_PORT", "70000")
		_, ok := proxyPortFromEnv()
		assert.False(t, ok)
	})
}
