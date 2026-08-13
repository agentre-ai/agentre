package codex

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callStubHandle 是 appClient.Call 的最小 processHandle 桩。Stdin 用 io.Pipe：
// 测试可以精确控制 Call 停在 writeJSON（pipe 写端无人读时 Write 会阻塞），
// 从而在“响应已路由进 pending 通道、但 Call 尚未进入 select”的窗口里布好
// done，构造出竞态所需的交错。
type callStubHandle struct {
	stdinR *io.PipeReader
	stdinW *io.PipeWriter
}

func newCallStubHandle() *callStubHandle {
	r, w := io.Pipe()
	return &callStubHandle{stdinR: r, stdinW: w}
}

func (h *callStubHandle) Stdin() io.Writer         { return h.stdinW }
func (h *callStubHandle) Stdout() io.Reader        { return strings.NewReader("") }
func (h *callStubHandle) Stderr() io.Reader        { return strings.NewReader("") }
func (h *callStubHandle) Wait() error              { return nil }
func (h *callStubHandle) Kill() error              { return nil }
func (h *callStubHandle) Signal(_ os.Signal) error { return nil }

// TestAppClient_CallPrefersBufferedResponseOverProcessDeath 回归：进程在响应已
// 到达、但 Call 尚未醒来时退出。readLoop 只在把读到的响应全部 route 完之后才
// close(done)，因此一旦 done 关闭，任何已到达的响应必然已缓冲在 pending 通道
// 里 —— Call 必须把它取回来，而不是被 ErrNoActiveTurn 吞掉。否则一次成功的中
// 断/转向会偶发误报 no active turn（evalInterrupt 在高并发下的 flake）。
//
// 交错构造：Call 阻塞在 writeJSON（pipe 无人读）→ 测试先把响应 route 进 pending
// 通道、再 close(done) → 读走 pipe 放行 Call → Call 进入 select 时 ch 与 done
// 同时 ready。旧实现（直接 return ErrProcessDead）在这里随机选中 done，循环里
// 每轮约一半概率失败，25 轮全过的概率约 2^-25；修复后必然取回响应。
func TestAppClient_CallPrefersBufferedResponseOverProcessDeath(t *testing.T) {
	for i := 0; i < 25; i++ {
		h := newCallStubHandle()
		c := &appClient{
			proc:    h,
			pending: map[string]chan rpcResponse{},
			done:    make(chan struct{}),
		}

		type callResult struct {
			res json.RawMessage
			err error
		}
		result := make(chan callResult, 1)
		go func() {
			res, err := c.Call(context.Background(), "turn/interrupt", map[string]any{
				"threadId": "thr-1", "turnId": "turn-1",
			})
			result <- callResult{res, err}
		}()

		// 等 Call 把请求注册进 pending（此刻它阻塞在 writeJSON 上）。
		key := waitPendingKey(t, c, i)
		// 1) 响应先到达，被 route 进 pending 通道（Call 不在 select，走缓冲）。
		c.routeMessage(rpcMessage{
			ID:     json.RawMessage(key),
			Result: json.RawMessage(`{"ok":true}`),
		})
		// 2) 进程随后退出：readLoop 收尾时 close(done)。
		close(c.done)
		// 3) 读走 pipe 放行 Call 的 writeJSON；它随即进入 select，ch 与 done 同时 ready。
		buf := make([]byte, 4096)
		_, _ = h.stdinR.Read(buf)

		select {
		case r := <-result:
			require.NoErrorf(t, r.err, "iteration %d: 已到达的响应不能被进程退出吞掉", i)
			assert.JSONEq(t, `{"ok":true}`, string(r.res))
		case <-time.After(2 * time.Second):
			t.Fatalf("Call hung (iteration %d)", i)
		}
	}
}

// waitPendingKey 轮询等 appClient 把请求 ID 注册进 pending map。
func waitPendingKey(t *testing.T, c *appClient, iteration int) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.pendingMu.Lock()
		for k := range c.pending {
			c.pendingMu.Unlock()
			return k
		}
		c.pendingMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("Call never registered a pending request (iteration %d)", iteration)
		}
		time.Sleep(time.Millisecond)
	}
}
