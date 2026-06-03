package claudecode

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeFrameContent 解开 buildUserFrame 产出的 stream-json user frame,
// 返回 message.content 块数组,便于断言图片 / 文本块的顺序与字段。
func decodeFrameContent(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var f struct {
		Type    string `json:"type"`
		Message struct {
			Role    string           `json:"role"`
			Content []map[string]any `json:"content"`
		} `json:"message"`
	}
	require.NoError(t, json.Unmarshal(raw, &f))
	assert.Equal(t, "user", f.Type)
	assert.Equal(t, "user", f.Message.Role)
	return f.Message.Content
}

// Given 一条带文字 + 一张 PNG 的用户输入,When buildUserFrame,Then content 里
// 图片块在前(Anthropic 建议图片先于文本以获得更好的视觉理解)、文本块在后,
// 且图片走 base64 inline source。
func TestBuildUserFrame_ImageBeforeText(t *testing.T) {
	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	raw, err := buildUserFrame("what is this?", []Image{{Data: data, MediaType: "image/png"}})
	require.NoError(t, err)

	content := decodeFrameContent(t, raw)
	require.Len(t, content, 2)

	img := content[0]
	assert.Equal(t, "image", img["type"])
	src, ok := img["source"].(map[string]any)
	require.True(t, ok, "image block must carry a source object")
	assert.Equal(t, "base64", src["type"])
	assert.Equal(t, "image/png", src["media_type"])
	assert.Equal(t, base64.StdEncoding.EncodeToString(data), src["data"])

	txt := content[1]
	assert.Equal(t, "text", txt["type"])
	assert.Equal(t, "what is this?", txt["text"])
}

// 无图片时保持历史行为:content 永远是单个 text block(即使空串),不引入 image 块。
// 这条钉死 Client.Stream / Session.Turn 在 text-only 路径上字节级不变。
func TestBuildUserFrame_TextOnlyUnchanged(t *testing.T) {
	raw, err := buildUserFrame("hello", nil)
	require.NoError(t, err)
	content := decodeFrameContent(t, raw)
	require.Len(t, content, 1)
	assert.Equal(t, "text", content[0]["type"])
	assert.Equal(t, "hello", content[0]["text"])

	// 空 prompt 也照发一个空 text block(旧实现 content 永远非空)。
	rawEmpty, err := buildUserFrame("", nil)
	require.NoError(t, err)
	emptyContent := decodeFrameContent(t, rawEmpty)
	require.Len(t, emptyContent, 1)
	assert.Equal(t, "text", emptyContent[0]["type"])
	assert.Equal(t, "", emptyContent[0]["text"])
}

// 多张图片 + 空文字:只发图片块,不追加空 text block。
func TestBuildUserFrame_ImagesOnlyOmitsEmptyText(t *testing.T) {
	raw, err := buildUserFrame("", []Image{
		{Data: []byte{0x01}, MediaType: "image/png"},
		{Data: []byte{0x02}, MediaType: "image/jpeg"},
	})
	require.NoError(t, err)
	content := decodeFrameContent(t, raw)
	require.Len(t, content, 2)
	assert.Equal(t, "image", content[0]["type"])
	assert.Equal(t, "image", content[1]["type"])
}

// 边界:没有 inline 字节的 Image 跳过(URL-only 当前不支持);MediaType 缺省回退 image/png。
func TestBuildUserFrame_SkipsEmptyDataAndDefaultsMediaType(t *testing.T) {
	raw, err := buildUserFrame("x", []Image{
		{Data: nil, MediaType: "image/png"}, // 无字节 → 跳过
		{Data: []byte{0x09}, MediaType: ""}, // 缺省 MediaType → image/png
	})
	require.NoError(t, err)
	content := decodeFrameContent(t, raw)
	require.Len(t, content, 2) // 1 image + 1 text
	assert.Equal(t, "image", content[0]["type"])
	src := content[0]["source"].(map[string]any)
	assert.Equal(t, "image/png", src["media_type"])
	assert.Equal(t, "text", content[1]["type"])
	assert.Equal(t, "x", content[1]["text"])
}

// Given 一个常驻 Session,When Turn 带一张 inline 图片,Then 实际写到子进程 stdin
// 的 user frame 携带 base64 image block + 文本块 —— 端到端钉死图片透传到 CLI。
func TestSession_TurnSendsImageBlocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	captured := make(chan string, 1)
	fakeCLI := func(stdin io.Reader, stdout io.Writer) {
		sc := bufio.NewScanner(stdin)
		sc.Buffer(make([]byte, 0, 64<<10), maxFrameBytes)
		for sc.Scan() {
			line := sc.Text()
			if !strings.Contains(line, `"type":"user"`) {
				continue
			}
			select {
			case captured <- line:
			default:
			}
			writeFrame(stdout, `{"type":"system","subtype":"init","session_id":"s","cwd":"/tmp","model":"m","tools":[]}`)
			writeFrame(stdout, `{"type":"result","subtype":"success","session_id":"s","usage":{"input_tokens":1,"output_tokens":1}}`)
		}
	}

	c := New(WithBinary("fake"), pipeSpawner(t, fakeCLI))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)

	data := []byte{0x01, 0x02, 0x03}
	ch, err := sess.Turn(ctx, "describe this", Image{Data: data, MediaType: "image/jpeg"})
	require.NoError(t, err)
	for range ch {
	}
	require.NoError(t, sess.Close(ctx))

	var line string
	select {
	case line = <-captured:
	case <-time.After(2 * time.Second):
		t.Fatal("fake CLI never received a user frame")
	}

	content := decodeFrameContent(t, []byte(line))
	require.Len(t, content, 2)
	assert.Equal(t, "image", content[0]["type"])
	src, ok := content[0]["source"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "base64", src["type"])
	assert.Equal(t, "image/jpeg", src["media_type"])
	assert.Equal(t, base64.StdEncoding.EncodeToString(data), src["data"])
	assert.Equal(t, "text", content[1]["type"])
	assert.Equal(t, "describe this", content[1]["text"])
}
