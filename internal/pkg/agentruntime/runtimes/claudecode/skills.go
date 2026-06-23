package claudecode

import "encoding/json"

// buildSkillsSettings 把 enabledPlugins 覆盖合进 --settings 的 JSON。
// base 为空或非 JSON 对象时以空对象起。enabled 为空 → 原样返回 base。
func buildSkillsSettings(enabled map[string]bool, base string) string {
	if len(enabled) == 0 {
		return base
	}
	m := map[string]any{}
	if base != "" {
		_ = json.Unmarshal([]byte(base), &m) // 非对象/坏 JSON → 空起,不阻断
	}
	m["enabledPlugins"] = enabled
	b, err := json.Marshal(m)
	if err != nil || len(b) == 0 {
		return base // 极端:marshal 失败不应清空既有 settings
	}
	return string(b)
}
