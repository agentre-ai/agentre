package syncwire_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/pkg/syncwire"
)

// TestGuardPayload_RejectsLocalAutoIncrementIDs R2 的守卫断言：载荷里不出现任何
// 本地自增 ID——「键名以 id 结尾、取值是数字」这个形状只可能是本机主键。
func TestGuardPayload_RejectsLocalAutoIncrementIDs(t *testing.T) {
	for _, payload := range []string{
		`{"name":"x","parent_id":7}`,
		`{"name":"x","agentBackendId":7}`,
		`{"targets":[{"agent-backend-id":7}]}`,
		`{"nested":{"department_id":1}}`,
	} {
		assert.ErrorIs(t, syncwire.GuardPayload([]byte(payload)), syncwire.ErrPayloadLocalID, payload)
	}
}

// TestGuardPayload_AllowsStringReferences 跨机引用一律是字符串：同步标识、指纹、
// provider_key，它们照常放行。
func TestGuardPayload_AllowsStringReferences(t *testing.T) {
	for _, payload := range []string{
		`{"name":"x","parent_sync_id":"01ARZ3ND"}`,
		`{"provider_key":"anthropic-main"}`,
		`{"agent_sync_id":"a","backend_sync_id":"b","sort_order":2}`,
		`{}`,
		``,
	} {
		assert.NoError(t, syncwire.GuardPayload([]byte(payload)), payload)
	}
}

// TestGuardPayload_RejectsCredentialsAndProviderRows llm_providers 整表（含 APIKey）
// 不出本机（决策 6）：跨机只传 provider_key 这个字符串键。
func TestGuardPayload_RejectsCredentialsAndProviderRows(t *testing.T) {
	assert.ErrorIs(t, syncwire.GuardPayload([]byte(`{"api_key":"sk-x"}`)), syncwire.ErrPayloadCredential)
	assert.ErrorIs(t, syncwire.GuardPayload([]byte(`{"apiKey":"sk-x"}`)), syncwire.ErrPayloadCredential)
	assert.ErrorIs(t, syncwire.GuardPayload([]byte(`{"provider":{"api_key":"sk-x"}}`)), syncwire.ErrPayloadCredential)
	assert.ErrorIs(t, syncwire.GuardPayload([]byte(`{"providers":[{"name":"p"}]}`)), syncwire.ErrPayloadCredential)
}

// TestGuardPayload_RejectsAvatarContent R16a 的守卫断言：同步载荷里只出现头像
// 的内容哈希，正文（avatar_data_url）一律不得出现，不管值是 data URL 还是别的。
func TestGuardPayload_RejectsAvatarContent(t *testing.T) {
	for _, payload := range []string{
		`{"name":"x","avatar_data_url":"data:image/png;base64,AAAA"}`,
		`{"avatarDataUrl":"data:image/png;base64,AAAA"}`,
	} {
		assert.ErrorIs(t, syncwire.GuardPayload([]byte(payload)), syncwire.ErrPayloadAvatarContent, payload)
	}
	// avatar_hash 是允许的：它是内容哈希这个字符串引用，不是正文。
	assert.NoError(t, syncwire.GuardPayload([]byte(`{"avatar_hash":"deadbeef"}`)))
}

// TestGuardPayload_RejectsNonObject 载荷必须是一个 JSON 对象。
func TestGuardPayload_RejectsNonObject(t *testing.T) {
	assert.ErrorIs(t, syncwire.GuardPayload([]byte(`[1,2]`)), syncwire.ErrPayloadNotObject)
	assert.Error(t, syncwire.GuardPayload([]byte(`{`)))
}
