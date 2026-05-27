package httpgateway

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"agentre/internal/model/entity/agent_backend_entity"
)

// TokenEntry 一条 token → backend 路由记录。
//
// 持有 backend 的 ID / 类型 / 主 provider key / model_routes 解析后的快照；
// 不持有 provider 实体本身——转发时由 llmforward 通过 llm_provider_repo 查实时数据。
type TokenEntry struct {
	BackendID       int64
	BackendType     agent_backend_entity.BackendType
	MainProviderKey string
	// Routes 把 alias（OPUS / SONNET / HAIKU 等，**统一大写**）映射到 LLMProvider.ProviderKey。
	// 空 map 表示没有 tier 路由（codex / 没配 model_routes 的 claudecode）。
	Routes   map[string]string
	ExpireAt time.Time // 0 = 永不过期（chat flow 长 token 用）
}

// IsExpired 当前是否已过期。0 时区时间视作未过期。
func (e TokenEntry) IsExpired(now time.Time) bool {
	if e.ExpireAt.IsZero() {
		return false
	}
	return !now.Before(e.ExpireAt)
}

// ResolveModel 在 routes 里找 alias 对应的 provider key；没命中返回 (mainProviderKey, false)。
// alias 比较前会先转大写——子进程发的请求 body 里 model 字段可能是 "opus" 等小写。
func (e TokenEntry) ResolveModel(modelField string) (providerKey string, hit bool) {
	if len(e.Routes) == 0 {
		return e.MainProviderKey, false
	}
	if p, ok := e.Routes[strings.ToUpper(strings.TrimSpace(modelField))]; ok {
		return p, true
	}
	return e.MainProviderKey, false
}

// TokenRegistry 内存 token 表。App 退出即清空；不落盘。
//
// 并发模型：RWMutex；Resolve 读锁、Issue/Revoke 写锁；过期 entry 在 Resolve 命中时
// 顺手删除（lazy expire），不启动后台 sweep goroutine。
type TokenRegistry struct {
	mu     sync.RWMutex
	tokens map[string]TokenEntry
	now    func() time.Time
}

// NewTokenRegistry 构造空 registry。
func NewTokenRegistry() *TokenRegistry {
	return &TokenRegistry{
		tokens: make(map[string]TokenEntry),
		now:    time.Now,
	}
}

// ErrInvalidBackend 内部哨兵：Issue 传 nil 时返回。
var ErrInvalidBackend = errors.New("httpgateway: invalid backend for token issue")

// Issue 把 backend 转成 TokenEntry 并存入表，返回随机 token 字符串。
// ttl <= 0 时视为永久（chat flow 长 token 用；TestAgentBackend 传 60s）。
//
// LLMProviderKey == ""（CLI 登录模式，没绑 provider）也允许发 token：
// 这种 token 在 /hook/v1/inbox 上正常用（gateway handler 只 Resolve 不看
// provider），LLM 转发端点会因 ResolveModel→"" 找不到 provider 自然 502，
// 互不干扰。**不允许**会让 hook 子进程在 CLI 登录模式下永远拿不到 token，
// 排队消息没法 mid-turn 注入。
func (r *TokenRegistry) Issue(b *agent_backend_entity.AgentBackend, ttl time.Duration) (string, error) {
	if b == nil {
		return "", ErrInvalidBackend
	}
	routes, err := agent_backend_entity.ParseModelRoutes(b.ModelRoutes)
	if err != nil {
		return "", err
	}
	upper := make(map[string]string, len(routes))
	for k, v := range routes {
		upper[strings.ToUpper(k)] = v
	}

	tok, err := RandomToken(24)
	if err != nil {
		return "", err
	}
	entry := TokenEntry{
		BackendID:       b.ID,
		BackendType:     agent_backend_entity.BackendType(b.Type),
		MainProviderKey: b.LLMProviderKey,
		Routes:          upper,
	}
	if ttl > 0 {
		entry.ExpireAt = r.now().Add(ttl)
	}

	r.mu.Lock()
	r.tokens[tok] = entry
	r.mu.Unlock()
	return tok, nil
}

// Resolve 按 token 查 entry；命中但已过期则原地删并返回 (zero, false)。
func (r *TokenRegistry) Resolve(token string) (TokenEntry, bool) {
	r.mu.RLock()
	entry, ok := r.tokens[token]
	r.mu.RUnlock()
	if !ok {
		return TokenEntry{}, false
	}
	if entry.IsExpired(r.now()) {
		r.mu.Lock()
		// double check 避免并发 issue 同 token 删错
		if cur, ok2 := r.tokens[token]; ok2 && cur.IsExpired(r.now()) {
			delete(r.tokens, token)
		}
		r.mu.Unlock()
		return TokenEntry{}, false
	}
	return entry, true
}

// Revoke 删除 token；找不到忽略。
func (r *TokenRegistry) Revoke(token string) {
	r.mu.Lock()
	delete(r.tokens, token)
	r.mu.Unlock()
}

// Size 返回当前 token 数量。
func (r *TokenRegistry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tokens)
}

// RandomToken 生成 n 字节随机 token 的 hex 编码字符串。
func RandomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
