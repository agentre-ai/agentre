// Package turn 管理一轮 chat turn 的 block 累积与事件 dispatch。
//
// Accumulator 替代旧 chat_svc/chat.go turnBlockAccumulator:
//   - text/thinking 累在 buf,遇 AddToolUse/AddBlock flush 成 TextBlock
//   - thinking 总在 finalize index 0 (Anthropic 协议)
//   - 通过范型 Mutate[B](acc, key, func(*B)) 取代写死的 patchXxx 方法
//
// 与旧 acc 不同:Mutate 必须传 *B(指针),因为 mutate 语义就是 in-place patch;
// addBlock 时传 cagoblocks.ContentBlock 接口指针或值都可,但若想被 Mutate 命中
// 必须传 *B。
package turn

import (
	"strings"
	"sync"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
)

type Accumulator struct {
	mu          sync.Mutex
	finalBlocks []cagoblocks.ContentBlock
	textBuf     strings.Builder
	thinkingBuf strings.Builder
	mutateIndex map[string]int
}

func New() *Accumulator {
	return &Accumulator{mutateIndex: map[string]int{}}
}

func (a *Accumulator) AddText(s string) {
	if s == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.textBuf.WriteString(s)
}

func (a *Accumulator) AddThinking(s string) {
	if s == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.thinkingBuf.WriteString(s)
}

// AddToolUse 与 AddBlock 等价但语义更明确:cago ToolUseBlock 走这条。
func (a *Accumulator) AddToolUse(b cagoblocks.ContentBlock, mutateKey string) {
	a.AddBlock(b, mutateKey)
}

// AddToolResult 不 flush textBuf(tool_use→tool_result 之间一般无文字)。
func (a *Accumulator) AddToolResult(b cagoblocks.ContentBlock) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finalBlocks = append(a.finalBlocks, b)
}

// AddBlock 任意 block 走这条;先 flush textBuf,再 push,记 mutateIndex。
func (a *Accumulator) AddBlock(b cagoblocks.ContentBlock, mutateKey string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.flushTextLocked()
	if mutateKey != "" {
		a.mutateIndex[mutateKey] = len(a.finalBlocks)
	}
	a.finalBlocks = append(a.finalBlocks, b)
}

// HasToolUse 查询当前是否已 push 过该 ID 的 cago.ToolUseBlock(value 或 pointer)。
// 用于孤儿 tool_result 丢弃。
func (a *Accumulator) HasToolUse(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, b := range a.finalBlocks {
		switch tu := b.(type) {
		case *cagoblocks.ToolUseBlock:
			if tu.ID == id {
				return true
			}
		case cagoblocks.ToolUseBlock:
			if tu.ID == id {
				return true
			}
		}
	}
	return false
}

// ToolUseInput 返回已 push 的该 ID cago.ToolUseBlock 的原始 Input(value 或 pointer
// 形态),未找到返回 (nil, false)。SubagentStarted handler 用它读 run_in_background
// 判定一次 local_bash 帧是真后台 bash 还是普通前台 bash。
func (a *Accumulator) ToolUseInput(id string) (map[string]any, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, b := range a.finalBlocks {
		switch tu := b.(type) {
		case *cagoblocks.ToolUseBlock:
			if tu.ID == id {
				return tu.Input, true
			}
		case cagoblocks.ToolUseBlock:
			if tu.ID == id {
				return tu.Input, true
			}
		}
	}
	return nil, false
}

// Empty 反映"无任何内容 + 无 buf";turn 收尾判定是否落 message 用。
func (a *Accumulator) Empty() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.finalBlocks) == 0 && a.textBuf.Len() == 0 && a.thinkingBuf.Len() == 0
}

// Snapshot 中途快照(checkpoint 用),不消费 buf。返回新切片。
func (a *Accumulator) Snapshot() []cagoblocks.ContentBlock {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]cagoblocks.ContentBlock, 0, len(a.finalBlocks)+2)
	if a.thinkingBuf.Len() > 0 {
		out = append(out, &cagoblocks.ThinkingBlock{Text: a.thinkingBuf.String()})
	}
	out = append(out, a.finalBlocks...)
	if a.textBuf.Len() > 0 {
		out = append(out, &cagoblocks.TextBlock{Text: a.textBuf.String()})
	}
	return out
}

// Finalize 收尾:flush textBuf + 把 thinking 插到 index 0。
func (a *Accumulator) Finalize() []cagoblocks.ContentBlock {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.flushTextLocked()
	if a.thinkingBuf.Len() == 0 {
		return a.finalBlocks
	}
	out := make([]cagoblocks.ContentBlock, 0, len(a.finalBlocks)+1)
	out = append(out, &cagoblocks.ThinkingBlock{Text: a.thinkingBuf.String()})
	out = append(out, a.finalBlocks...)
	return out
}

func (a *Accumulator) flushTextLocked() {
	if a.textBuf.Len() == 0 {
		return
	}
	a.finalBlocks = append(a.finalBlocks, &cagoblocks.TextBlock{Text: a.textBuf.String()})
	a.textBuf.Reset()
}

// Mutate[B] 范型 patch: 按 key 查 mutateIndex,断言为 *B 后调 fn。
// 返回是否命中(未命中 = key 缺失或类型断言失败 = B 类型不符)。
//
// B 用 `any` 而不是 ContentBlock 约束:类型参数的指针 *B 不会自动 satisfy 接口,
// 必须改用两步断言(any → *B)。调用方写出 B = 具体 block 类型即可,例:
//
//	Mutate[blocks.UserAskBlock](acc, "user_ask:r-1", func(b *blocks.UserAskBlock) { ... })
func Mutate[B any](a *Accumulator, key string, fn func(*B)) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	idx, ok := a.mutateIndex[key]
	if !ok || idx >= len(a.finalBlocks) {
		return false
	}
	b, ok := any(a.finalBlocks[idx]).(*B)
	if !ok {
		return false
	}
	fn(b)
	return true
}
