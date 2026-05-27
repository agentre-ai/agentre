// Package llmcatalog 在 cago agents 内置目录 (provider/models) 之上提供宽容的模型元数据查询。
//
// 真实场景里 LLM 供应商 /v1/models 返回的 ID 经常带版本/日期后缀 (claude-opus-4-7-20251201)
// 或带 vendor 路径前缀 (anthropic/claude-opus-4-7、openrouter/anthropic/claude-opus-4-7)，
// cago 的 models.Get 只做精确匹配会全部 miss，前端 ContextWindow 直接为 0。此处给出一层
// 归一化 + 最长前缀匹配，避免每出一个新版本号就要去 cago 加别名。
package llmcatalog

import (
	"strings"

	"github.com/cago-frame/agents/provider/models"
)

// Lookup 查 cago 目录，按以下顺序：
//  1. 归一化：取最后一个 `/` 之后的末段 + 转小写；
//  2. models.Get 精确匹配（含别名）；
//  3. 以 catalog ID / 别名作为输入前缀，最长匹配优先，且要求前缀后是非字母数字字符，
//     避免 "gpt-5.5" 误命中 "gpt-5.5xyz"、"glm-5" 抢走 "glm-5.1-foo"；
//     同时能 cover 各种 vendor 后缀标记：`-20251201`、`.codex`、`(xhigh)`、`[fast]`、`:beta` 等。
func Lookup(id string) (models.Info, bool) {
	id = normalize(id)
	if id == "" {
		return models.Info{}, false
	}
	if info, ok := models.Get(id); ok {
		return info, true
	}
	return matchByPrefix(id)
}

func normalize(id string) string {
	id = strings.TrimSpace(id)
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	return strings.ToLower(id)
}

func matchByPrefix(id string) (models.Info, bool) {
	var (
		best    models.Info
		bestLen int
		found   bool
	)
	for _, info := range models.All() {
		keys := append([]string{info.ID}, info.Aliases...)
		for _, key := range keys {
			k := strings.ToLower(key)
			if k == "" || !strings.HasPrefix(id, k) {
				continue
			}
			if len(k) < len(id) {
				next := id[len(k)]
				if isAlnum(next) {
					continue
				}
			}
			if len(k) > bestLen {
				best, bestLen, found = info, len(k), true
			}
		}
	}
	return best, found
}

func isAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}
