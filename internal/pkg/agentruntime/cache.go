package agentruntime

import (
	"container/list"
	"context"
	"sync"
)

// ctxCloser 是缓存条目能关闭子进程的最小接口。
// cago claudecode.Session / codex.Session / claudecode.Runner / codex.Runner 都满足。
type ctxCloser interface {
	Close(context.Context) error
}

// SessionCache 是 cago cliagent Session 的 per-chat-session LRU 缓存。
// 上限默认 8（agentre 同时活跃的 CLI 子进程兜底）；超出时 Close 并 evict 最老的。
// 线程安全。Close 在 goroutine 内异步执行，不阻塞 Put。
type SessionCache struct {
	mu    sync.Mutex
	cap   int
	ll    *list.List
	index map[string]*list.Element
}

type sessionEntry struct {
	key string
	val ctxCloser
}

// NewSessionCache 构造 LRU 缓存；capacity<=0 自动按 1 处理。
func NewSessionCache(capacity int) *SessionCache {
	if capacity <= 0 {
		capacity = 1
	}
	return &SessionCache{cap: capacity, ll: list.New(), index: map[string]*list.Element{}}
}

// Get 取一个 session；命中后被提到 LRU 最新位置。
func (c *SessionCache) Get(key string) (ctxCloser, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.index[key]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*sessionEntry).val, true
}

// Put 插入 / 替换。容量超限时关掉最老的并 evict。
// 老条目通过 background goroutine 关闭，避免阻塞调用方；Close 错误丢弃。
func (c *SessionCache) Put(key string, v ctxCloser) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.index[key]; ok {
		old := el.Value.(*sessionEntry).val
		el.Value = &sessionEntry{key: key, val: v}
		c.ll.MoveToFront(el)
		go closeWithTimeout(old)
		return
	}
	el := c.ll.PushFront(&sessionEntry{key: key, val: v})
	c.index[key] = el
	for c.ll.Len() > c.cap {
		back := c.ll.Back()
		if back == nil {
			break
		}
		ent := back.Value.(*sessionEntry)
		c.ll.Remove(back)
		delete(c.index, ent.key)
		go closeWithTimeout(ent.val)
	}
}

// RemoveAll 原子地清空所有条目并 background-close 每个 value。
// 语义与 Put 超容时一致：列表先腾空，Close 在 goroutine 里跑，不阻塞调用方。
// 给 app shutdown 用——一次性把全部活着的 CLI 子进程释放掉。
func (c *SessionCache) RemoveAll() {
	c.mu.Lock()
	olds := make([]ctxCloser, 0, c.ll.Len())
	for el := c.ll.Front(); el != nil; el = el.Next() {
		olds = append(olds, el.Value.(*sessionEntry).val)
	}
	c.ll.Init()
	c.index = map[string]*list.Element{}
	c.mu.Unlock()
	for _, v := range olds {
		go closeWithTimeout(v)
	}
}

// Remove 删除一个 key 并关闭其 session；不存在则 no-op。
func (c *SessionCache) Remove(key string) {
	c.mu.Lock()
	el, ok := c.index[key]
	if !ok {
		c.mu.Unlock()
		return
	}
	ent := el.Value.(*sessionEntry)
	c.ll.Remove(el)
	delete(c.index, key)
	c.mu.Unlock()
	go closeWithTimeout(ent.val)
}

// Len 仅供测试 / 观测；不暴露的话外部测难写。
func (c *SessionCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// closeWithTimeout 给被 evict 的 session 一个 background ctx 关闭机会。
// cago Session.Close 通常立刻返回；这里不另设 timeout，避免遮蔽 cago 自身重试逻辑。
func closeWithTimeout(c ctxCloser) { _ = c.Close(context.Background()) }

// RunnerCache 按 (backendID, updatetime) 缓存 cliagent.Runner-likes。
// updatetime 变化 = entity 重配；老 Runner 关掉重建。
type RunnerCache struct {
	mu      sync.Mutex
	entries map[int64]*runnerEntry
}

type runnerEntry struct {
	updatetime int64
	runner     ctxCloser
}

// NewRunnerCache 构造空缓存。
func NewRunnerCache() *RunnerCache { return &RunnerCache{entries: map[int64]*runnerEntry{}} }

// GetOrCreate 命中则返回；updatetime 变了则关旧建新。
// build 由调用方提供——claudecode.Runner 和 codex.Runner 类型不同，无法在缓存层抽象。
func (c *RunnerCache) GetOrCreate(backendID, updatetime int64, build func() (ctxCloser, error)) (ctxCloser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[backendID]; ok {
		if e.updatetime == updatetime {
			return e.runner, nil
		}
		go closeWithTimeout(e.runner)
	}
	r, err := build()
	if err != nil {
		return nil, err
	}
	c.entries[backendID] = &runnerEntry{updatetime: updatetime, runner: r}
	return r, nil
}

// Drop 移除并关闭某 backend 的 Runner。
func (c *RunnerCache) Drop(backendID int64) {
	c.mu.Lock()
	e, ok := c.entries[backendID]
	if ok {
		delete(c.entries, backendID)
	}
	c.mu.Unlock()
	if ok {
		go closeWithTimeout(e.runner)
	}
}
