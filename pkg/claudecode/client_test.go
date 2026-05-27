package claudecode

import (
	"bufio"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStreamHappy 一次性 prompt 的 happy-path fake CLI：读（并丢弃）stdin，
// 喂一段固定 stream-json transcript（init + assistant text + result）后退出。
// stdin 在独立 goroutine 里 drain，直到 reader 端 EOF 才回头 ——避免 fakeCLI
// 退出时把 stdin pipe 关掉、外面写帧拿 broken pipe。
func fakeStreamHappy(stdin io.Reader, stdout io.Writer) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, stdin)
	}()
	writeFrame(stdout, `{"type":"system","subtype":"init","session_id":"sess-fake","cwd":"/tmp","model":"m","tools":[]}`)
	writeFrame(stdout, `{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"hi"}]}}`)
	writeFrame(stdout, `{"type":"result","subtype":"success","session_id":"sess-fake","usage":{"input_tokens":1,"output_tokens":1}}`)
	<-done
}

func TestClient_StreamHappyEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakeStreamHappy))
	stream, err := c.Stream(ctx, "hello")
	require.NoError(t, err)

	var kinds []EventKind
	for stream.Next() {
		kinds = append(kinds, stream.Event().Kind)
	}
	require.NoError(t, stream.Close(ctx))

	assert.Equal(t, "sess-fake", stream.SessionID())
	assert.Contains(t, kinds, EventTextDelta)
	assert.Contains(t, kinds, EventDone)
}

func TestClient_RejectsResumeSessionAtWithoutFork(t *testing.T) {
	c := New(WithBinary("/bin/true"))
	_, err := c.Stream(context.Background(), "hi", ResumeSessionAt("uuid-1"))
	assert.Error(t, err, "ResumeSessionAt 必须叠 ForkSession，否则会破坏原 session")
}

// TestClient_TextConcatsTextDeltas 验证 prober 路径：起 stream → 串接 text → 返回。
// fake CLI 喂一条 text="hi"，Text 应当返回 "hi"。
func TestClient_TextConcatsTextDeltas(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakeStreamHappy))
	text, err := c.Text(ctx, "hello", MaxTurns(1))
	require.NoError(t, err)
	assert.Equal(t, "hi", text)
}

// TestClient_CloseIsNoOp Client.Close 是契约对齐用的 no-op，不能 panic / 不能漏 ctx。
func TestClient_CloseIsNoOp(t *testing.T) {
	c := New()
	assert.NoError(t, c.Close(context.Background()))
}

func TestClient_WithSessionIDAndSettings_StoredOnClient(t *testing.T) {
	c := New(
		WithBinary("/bin/true"),
		WithSessionID("550e8400-e29b-41d4-a716-446655440000"),
		WithSettings("/tmp/foo.json"),
	)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", c.sessionID)
	assert.Equal(t, "/tmp/foo.json", c.settings)
}

// TestClient_WithEffort_StoredOnClient 校验 WithEffort 把档位固化在 client，
// 后续每轮 Stream 都会带 --effort <level>。
func TestClient_WithEffort_StoredOnClient(t *testing.T) {
	c := New(WithBinary("/bin/true"), WithEffort("high"))
	assert.Equal(t, "high", c.effort)

	// 空 effort 也能构造（不下发标志）。
	c2 := New(WithBinary("/bin/true"))
	assert.Equal(t, "", c2.effort)
}

// TestStream_KeepsStdinOpenUntilClose ensures Stream does not close stdin
// after writing the user frame — needed so the same process can later host
// additional turns / control frames. Close() handles the final teardown.
func TestStream_KeepsStdinOpenUntilClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 这个测试要确认 Stream 之后 stdin 还能写入。fakeCLI 让 stdin 读取走一条独立
	// goroutine（io.Pipe 的 Read 一对一阻塞配对 Write），既保证 Stream 写 user frame
	// 不卡死，也能在 Stream 返回后接收测试主动写入的 "{}\n"。
	gotSecondLine := make(chan struct{}, 1)
	fakeCLI := func(stdin io.Reader, stdout io.Writer) {
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			sc := bufio.NewScanner(stdin)
			seen := 0
			for sc.Scan() {
				seen++
				if seen == 2 {
					select {
					case gotSecondLine <- struct{}{}:
					default:
					}
				}
			}
		}()
		writeFrame(stdout, `{"type":"system","subtype":"init","session_id":"sess-fake","cwd":"/tmp","model":"m","tools":[]}`)
		writeFrame(stdout, `{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"hi"}]}}`)
		writeFrame(stdout, `{"type":"result","subtype":"success","session_id":"sess-fake","usage":{"input_tokens":1,"output_tokens":1}}`)
		<-drained
	}

	c := New(WithBinary("fake"), pipeSpawner(t, fakeCLI))
	stream, err := c.Stream(ctx, "hello")
	require.NoError(t, err)
	require.NotNil(t, stream.proc, "stream should hold an open process")
	require.NotNil(t, stream.proc.stdin, "stdin should remain accessible after Stream()")

	_, werr := stream.proc.stdin.Write([]byte("{}\n"))
	assert.NoError(t, werr, "writing to stdin should succeed before Close")

	for stream.Next() {
	}
	require.NoError(t, stream.Close(ctx))

	select {
	case <-gotSecondLine:
	case <-time.After(time.Second):
		t.Fatal("fake CLI never received the second stdin frame")
	}
}
