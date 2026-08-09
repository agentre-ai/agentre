package syncwire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrPayloadLocalID 表示载荷里出现了本地自增 ID。
var ErrPayloadLocalID = errors.New("sync payload carries a local auto-increment id")

// ErrPayloadCredential 表示载荷里出现了凭据或 llm_providers 行正文。
var ErrPayloadCredential = errors.New("sync payload carries a credential or a provider row")

// ErrPayloadNotObject 表示载荷不是一个 JSON 对象。
var ErrPayloadNotObject = errors.New("sync payload must be a json object")

// GuardPayload 是上行前的载荷守卫，挡住两类不该过机的东西（R2、决策 6）：
//
//   - 本地自增 ID。跨机引用一律用同步标识、agentred 指纹或 provider_key 表达，
//     它们全是字符串；「键名以 id 结尾、取值是数字」这个形状只可能是本机主键，
//     在别的机器上指向完全不同的对象。
//   - 凭据与 provider 行正文。llm_providers 整表（含 APIKey）不出本机，跨机只传
//     provider_key 这个字符串引用。
//
// server 侧有一份同样的守卫（sync_entity.ValidatePayload）。两边都要有：server 那份
// 保护账号里的其它设备，这一份保证坏载荷根本发不出去——发出去再被拒，本端会把它
// 当成一次同步失败反复重试。
//
// 空载荷合法：墓碑不带正文。
func GuardPayload(payload []byte) error {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return fmt.Errorf("decode sync payload: %w", err)
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return ErrPayloadNotObject
	}
	return walkPayload(obj)
}

func walkPayload(node any) error {
	switch t := node.(type) {
	case map[string]any:
		for key, value := range t {
			if err := checkPayloadKey(key, value); err != nil {
				return err
			}
			if err := walkPayload(value); err != nil {
				return err
			}
		}
	case []any:
		for _, value := range t {
			if err := walkPayload(value); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkPayloadKey(key string, value any) error {
	norm := normalizePayloadKey(key)
	if norm == "apikey" {
		return ErrPayloadCredential
	}
	if norm == "provider" || norm == "providers" {
		// provider_key 归一化后是 providerkey，不落进这一条：字符串引用照常放行，
		// 被挡住的是整行 provider 正文。
		switch value.(type) {
		case map[string]any, []any:
			return ErrPayloadCredential
		}
	}
	if strings.HasSuffix(norm, "id") {
		if _, isNumber := value.(json.Number); isNumber {
			return ErrPayloadLocalID
		}
	}
	return nil
}

// normalizePayloadKey 把 agent_backend_id / agentBackendId / agent-backend-id 归一成
// 同一个形状，免得换个命名风格就绕过守卫。
func normalizePayloadKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range strings.ToLower(key) {
		if r == '_' || r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
