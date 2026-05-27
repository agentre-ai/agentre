package claudecode

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadSessionJSONL_ParsesUUIDParentRoleAndText(t *testing.T) {
	root, err := filepath.Abs("testdata/jsonl_session")
	require.NoError(t, err)

	msgs, err := ReadSessionJSONL(root, "sess-x")
	require.NoError(t, err)
	require.Len(t, msgs, 6)

	assert.Equal(t, "u-1", msgs[0].UUID)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Empty(t, msgs[0].ParentUUID)
	assert.Equal(t, "hello", msgs[0].Text)

	assert.Equal(t, "a-1", msgs[1].UUID)
	assert.Equal(t, "u-1", msgs[1].ParentUUID)
	assert.Equal(t, "hi", msgs[1].Text)

	assert.Equal(t, "u-3-toolres", msgs[4].UUID)
	assert.Equal(t, "user", msgs[4].Role)
	// tool_result user msg 没 text block → Text 留空，正好用来排除合成 user。
	assert.Empty(t, msgs[4].Text)
}

func TestReadSessionJSONL_MissingSessionFileReturnsErrNotFound(t *testing.T) {
	root, err := filepath.Abs("testdata/jsonl_session")
	require.NoError(t, err)

	_, err = ReadSessionJSONL(root, "sess-nonexistent")
	assert.ErrorIs(t, err, ErrSessionJSONLNotFound)
}

// TestFindUserAnchorByText 验证 anchor 提取的核心契约：
// 给定用户发送的 prompt 文本，返回 JSONL 里对应 user msg 的 parentUuid。
// tool_result 类合成 user 应当被排除（Text 空，不匹配）。
func TestFindUserAnchorByText(t *testing.T) {
	root, err := filepath.Abs("testdata/jsonl_session")
	require.NoError(t, err)

	msgs, err := ReadSessionJSONL(root, "sess-x")
	require.NoError(t, err)

	// 第二条 user msg "list files" 的 parent 是上一条 assistant a-1
	assert.Equal(t, "a-1", FindUserAnchorByText(msgs, "list files"))

	// 第一条 user msg "hello" 是首轮，parent 为空（CLI 写 null）
	assert.Equal(t, "", FindUserAnchorByText(msgs, "hello"))

	// 不存在的文本：返回空
	assert.Equal(t, "", FindUserAnchorByText(msgs, "nope"))
}

func TestFindUserAnchorByText_IgnoresToolResultSyntheticUsers(t *testing.T) {
	// 模拟历史里有 tool_result 合成 user（Text 空）：不能误命中也不能误锚。
	msgs := []SessionMessage{
		{UUID: "a-prev", Role: "assistant", Text: "prior reply"},
		{UUID: "u-real", ParentUUID: "a-prev", Role: "user", Text: "list files"},
		{UUID: "a-tool", ParentUUID: "u-real", Role: "assistant"},
		{UUID: "u-toolres", ParentUUID: "a-tool", Role: "user", Text: ""},
	}
	// 按 text 反查只会命中真正的 user "list files"，然后往回走找到 a-prev。
	// 中间的 a-tool（tool_use 块）排在 u-real **后面**，所以反查命中 u-real 后
	// 往前走只能见到 a-prev，不会跨回 a-tool。
	assert.Equal(t, "a-prev", FindUserAnchorByText(msgs, "list files"))
}

// TestFindUserAnchorByText_SkipsAttachmentsBetweenAssistantAndUser 模拟真实
// claude CLI 的 JSONL：每条 user msg 前都有若干 attachment（hook_success /
// skill_listing / mcp_instructions_delta 等），user.ParentUUID 实际是 attachment。
// anchor 必须**绕开 attachment** 锚到上一条 assistant —— 否则 fork 时 CLI 的
// interrupted-state 检测会注入 "Continue from where you left off."。
func TestFindUserAnchorByText_SkipsAttachmentsBetweenAssistantAndUser(t *testing.T) {
	msgs := []SessionMessage{
		{UUID: "att-pre1", Role: "attachment"},
		{UUID: "att-pre2", ParentUUID: "att-pre1", Role: "attachment"},
		{UUID: "u-1", ParentUUID: "att-pre2", Role: "user", Text: "hello"},
		{UUID: "att-post1", ParentUUID: "u-1", Role: "attachment"},
		{UUID: "a-1-think", ParentUUID: "att-post1", Role: "assistant" /*thinking block, no text*/},
		{UUID: "a-1-text", ParentUUID: "a-1-think", Role: "assistant", Text: "hi"},
		{UUID: "att-stop", ParentUUID: "a-1-text", Role: "attachment"}, // Stop hook 触发的 post-turn 附件
		{UUID: "u-2", ParentUUID: "att-stop", Role: "user", Text: "list files"},
	}

	// 关键契约：anchor 是 a-1-text（最后一条 assistant），不是 u-2.ParentUUID="att-stop"。
	assert.Equal(t, "a-1-text", FindUserAnchorByText(msgs, "list files"),
		"anchor 必须跳过 attachment 锚到上一条 assistant")

	// 首轮（u-1 前面只有 attachment，没有 assistant）→ 空。
	assert.Equal(t, "", FindUserAnchorByText(msgs, "hello"),
		"首轮 user msg 前没有 assistant，返回空让上层 drop session")
}

// TestFindUserAnchorByText_PicksLatestAssistantBlockForMultiBlockTurn 多 block
// 的 assistant turn（thinking + text）会在 JSONL 里写多条 assistant 记录。
// anchor 应当取**最靠近 user 的那一条**（最后一条 assistant 记录），不然 fork
// 会丢掉 text block。
func TestFindUserAnchorByText_PicksLatestAssistantBlockForMultiBlockTurn(t *testing.T) {
	msgs := []SessionMessage{
		{UUID: "u-prev", Role: "user", Text: "go"},
		{UUID: "a-think", ParentUUID: "u-prev", Role: "assistant"},
		{UUID: "a-tool", ParentUUID: "a-think", Role: "assistant"},
		{UUID: "u-toolres", ParentUUID: "a-tool", Role: "user", Text: ""},
		{UUID: "a-text", ParentUUID: "u-toolres", Role: "assistant", Text: "done"},
		{UUID: "u-target", ParentUUID: "a-text", Role: "user", Text: "next"},
	}
	assert.Equal(t, "a-text", FindUserAnchorByText(msgs, "next"),
		"应当取最后一条 assistant 而不是中间任何一条 thinking/tool_use")
}
