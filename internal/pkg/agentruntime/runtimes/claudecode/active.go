package claudecode

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/httpgateway"
)

// claudeActive 一个 chat session 当前的常驻 claude 子进程状态。
// 与现有顶层 claudecode.go.claudeActive 平行,emit channel 类型由
// chan<- RuntimeEvent 换成 chan<- agentruntime.Event(其它字段一致)。
type claudeActive struct {
	sessionUUID string
	handle      ccSessionHandle
	steer       *httpgateway.SteerInbox
	pool        *agentruntime.CLISessionPool
	poolKey     string
	inTurn      atomic.Bool
	// launchedEffort 记录 spawn 时下发给 claude CLI 的 --effort <level>。
	// --effort 是启动期 flag,运行时改不掉;下一轮如果 backend.ReasoningEffort
	// 变了,acquireSession 会用这个字段比对、强制 evict 重 spawn。
	launchedEffort string

	// askWaiters 记录当前阻塞中的 AskUserQuestion control_request。
	askMu      sync.Mutex
	askWaiters map[string]*askWaiter

	// permWaiters 记录当前阻塞中的非-AskUserQuestion can_use_tool control_request。
	permMu      sync.Mutex
	permWaiters map[string]*permWaiter

	// permissionMode CLI 当前的 permission mode。spawn 时写入,drain 期间收到
	// EventPermissionModeChanged 时同步更新。读写都只发生在同一个 drain
	// goroutine 里,无需加锁。
	permissionMode string

	// outMu/out Run() 期间登记的事件出口 channel,SubmitAnswer 完成后用它
	// emit UserAskResolved。drain 退出前清回 nil 避免向已关闭 channel 写入。
	outMu sync.Mutex
	out   chan<- agentruntime.Event

	// tasks 跨 turn 聚合 TaskCreate / TaskUpdate 工具调用为 canonical.PlanUpdate
	// 快照流。drainStream 每帧调 observePreToolUse / observePostToolUse,变更时
	// 合成 agentruntime.PlanUpdated 推 out。详见 task_aggregator.go 头注。
	tasks *taskAggregator
}

func (a *claudeActive) setOut(out chan<- agentruntime.Event) {
	a.outMu.Lock()
	a.out = out
	a.outMu.Unlock()
}

func (a *claudeActive) clearOut() {
	a.outMu.Lock()
	a.out = nil
	a.outMu.Unlock()
}

func (a *claudeActive) outChan() chan<- agentruntime.Event {
	a.outMu.Lock()
	defer a.outMu.Unlock()
	return a.out
}

// askWaiter 单次 AskUserQuestion 调用的状态。
type askWaiter struct {
	questions []agentruntime.AskQuestion
	rawInput  json.RawMessage
}

// permWaiter 单次非-AskUserQuestion 工具审批的状态。
type permWaiter struct {
	toolName string
	rawInput json.RawMessage
}

// Close 释放 claudeActive 持有的所有资源。幂等。
func (a *claudeActive) Close(ctx context.Context) error {
	if a.handle != nil {
		_ = a.handle.Close(ctx)
	}
	if a.steer != nil && a.sessionUUID != "" {
		a.steer.Forget(a.sessionUUID)
	}
	a.askMu.Lock()
	a.askWaiters = nil
	a.askMu.Unlock()
	a.permMu.Lock()
	a.permWaiters = nil
	a.permMu.Unlock()
	return nil
}

func (a *claudeActive) registerPermWaiter(reqID, toolName string, rawInput json.RawMessage) {
	a.permMu.Lock()
	defer a.permMu.Unlock()
	if a.permWaiters == nil {
		a.permWaiters = make(map[string]*permWaiter)
	}
	a.permWaiters[reqID] = &permWaiter{toolName: toolName, rawInput: rawInput}
	if a.pool != nil && a.poolKey != "" {
		a.pool.MarkWaiting(a.poolKey)
	}
}

func (a *claudeActive) takePermWaiter(reqID string) *permWaiter {
	a.permMu.Lock()
	defer a.permMu.Unlock()
	w := a.permWaiters[reqID]
	if w != nil {
		delete(a.permWaiters, reqID)
	}
	if a.pool != nil && a.poolKey != "" {
		a.pool.MarkActive(a.poolKey)
	}
	return w
}

func (a *claudeActive) registerAskWaiter(reqID string, questions []agentruntime.AskQuestion, rawInput json.RawMessage) {
	a.askMu.Lock()
	defer a.askMu.Unlock()
	if a.askWaiters == nil {
		a.askWaiters = make(map[string]*askWaiter)
	}
	a.askWaiters[reqID] = &askWaiter{questions: questions, rawInput: rawInput}
	if a.pool != nil && a.poolKey != "" {
		a.pool.MarkWaiting(a.poolKey)
	}
}

func (a *claudeActive) takeAskWaiter(reqID string) *askWaiter {
	a.askMu.Lock()
	defer a.askMu.Unlock()
	w := a.askWaiters[reqID]
	if w != nil {
		delete(a.askWaiters, reqID)
	}
	if a.pool != nil && a.poolKey != "" {
		a.pool.MarkActive(a.poolKey)
	}
	return w
}

// newUUIDv4 generates a v4 UUID without adding a dependency.
func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		binary.BigEndian.PutUint64(b[0:8], uint64(time.Now().UnixNano()))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// buildHookSettingsJSONString 生成注册 PostToolUse hook 的 settings JSON。
// 返回值直接走 claude CLI 的 --settings <json>(CLI 原生接受 JSON 字符串或文件
// 路径,见 `claude --help`)。
func buildHookSettingsJSONString(executablePath string) (string, error) {
	bin := shellEscapePath(executablePath)
	settings := map[string]any{
		"hooks": map[string]any{
			"PostToolUse": []any{map[string]any{
				"matcher": "",
				"hooks": []any{map[string]any{
					"type":    "command",
					"command": bin + " claudecode hook post-tool",
				}},
			}},
		},
	}
	body, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func shellEscapePath(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// projectsRoot is claude CLI's default project root, overridable via
// AGENTRE_CLAUDE_PROJECTS_DIR. Used by acquireSession to read JSONL for
// UserAnchor extraction.
func projectsRoot() string {
	if env := strings.TrimSpace(os.Getenv("AGENTRE_CLAUDE_PROJECTS_DIR")); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}
