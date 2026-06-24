package chat_repo_test

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
)

// blocksJSONContainsMatcher 是 sqlmock 自定义参数匹配器:校验 UPDATE 时传给 blocks_json
// 列的值包含全部指定子串,捕捉 AnyArg() 查不出的「未改写(原样回传 JSON)」缺陷。
type blocksJSONContainsMatcher struct {
	substrings []string
}

func (m blocksJSONContainsMatcher) Match(v driver.Value) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, sub := range m.substrings {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// TestFlipSubagentInBlocksJSON 直接单测 JSON 改写核心:翻转命中块的 status,其余字段
// (含 total_tokens/duration_ms/tool_uses 数字 + nested_tool_call_ids 数组)字节级保留,
// 防 float64 强转把整数写成 1e+03 之类。
func TestFlipSubagentInBlocksJSON(t *testing.T) {
	// 一条 subagent_state(running,带数字 + 数组字段)+ 一条 text。
	const input = `[` +
		`{"type":"subagent_state","data":{"parent_tool_call_id":"tu1","kind":"local_bash","description":"sleep 20","total_tokens":12345,"duration_ms":6789,"status":"running","tool_uses":42,"nested_tool_call_ids":["n1","n2"]}},` +
		`{"type":"text","data":{"text":"hi"}}` +
		`]`

	t.Run("命中块翻转 status,其余字段全保留", func(t *testing.T) {
		out, flipped, err := chat_repo.FlipSubagentInBlocksJSON(input, "tu1", "completed", "")
		require.NoError(t, err)
		assert.True(t, flipped)

		inData := subagentData(t, input)
		outData := subagentData(t, out)

		// status 翻成 completed。
		assert.Equal(t, "completed", outData["status"])
		// 其余字段逐项保留(数字仍是整数语义,数组仍是数组)—— 删掉 status 后 deep-equal。
		delete(inData, "status")
		delete(outData, "status")
		assert.Equal(t, inData, outData)
		// 显式校验数字 / 数组没被破坏(json.Number 比较,排除 1e+04 之类科学计数)。
		assert.Equal(t, json.Number("12345"), outData["total_tokens"])
		assert.Equal(t, json.Number("6789"), outData["duration_ms"])
		assert.Equal(t, json.Number("42"), outData["tool_uses"])
		assert.Equal(t, []any{"n1", "n2"}, outData["nested_tool_call_ids"])
		// 非命中块(text)原样保留。
		assert.Contains(t, out, `{"type":"text","data":{"text":"hi"}}`)
	})

	t.Run("非空 summary 同时写入", func(t *testing.T) {
		out, flipped, err := chat_repo.FlipSubagentInBlocksJSON(input, "tu1", "completed", "Background command completed")
		require.NoError(t, err)
		assert.True(t, flipped)
		outData := subagentData(t, out)
		assert.Equal(t, "completed", outData["status"])
		assert.Equal(t, "Background command completed", outData["summary"])
		// 其余字段(数字/数组)未被破坏。
		assert.Equal(t, json.Number("12345"), outData["total_tokens"])
	})

	t.Run("无命中块返回 false 且 JSON 不变", func(t *testing.T) {
		out, flipped, err := chat_repo.FlipSubagentInBlocksJSON(input, "tu-missing", "completed", "")
		require.NoError(t, err)
		assert.False(t, flipped)
		assert.Equal(t, input, out)
	})

	t.Run("空 JSON 返回 false 不报错", func(t *testing.T) {
		out, flipped, err := chat_repo.FlipSubagentInBlocksJSON("", "tu1", "completed", "")
		require.NoError(t, err)
		assert.False(t, flipped)
		assert.Equal(t, "", out)
	})

	t.Run("非法 JSON 返回 error", func(t *testing.T) {
		_, flipped, err := chat_repo.FlipSubagentInBlocksJSON("{not json", "tu1", "completed", "")
		require.Error(t, err)
		assert.False(t, flipped)
	})
}

// subagentData 解出 blocksJSON 里第一个 subagent_state 块的 data map(数字按
// json.Number 保留,以便检测整数是否被破坏)。
func subagentData(t *testing.T, blocksJSON string) map[string]any {
	t.Helper()
	var stored []struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(blocksJSON), &stored))
	for _, sb := range stored {
		if sb.Type != "subagent_state" {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(sb.Data))
		dec.UseNumber()
		var data map[string]any
		require.NoError(t, dec.Decode(&data))
		return data
	}
	t.Fatalf("no subagent_state block in %s", blocksJSON)
	return nil
}

func TestMessageRepo_List(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE session_id = \\? ORDER BY seq ASC").
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "blocks_json", "seq"}).
			AddRow(1, 3, "user", `[]`, 1).
			AddRow(2, 3, "assistant", `[]`, 2))

	got, err := chat_repo.NewMessage().List(ctx, 3)
	assert.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "user", got[0].Role)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_NextSeq(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(seq\\), 0\\) \\+ 1 FROM `chat_messages` WHERE session_id = \\?").
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"next"}).AddRow(5))

	got, err := chat_repo.NewMessage().NextSeq(ctx, 3)
	assert.NoError(t, err)
	assert.Equal(t, 5, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_Create(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `chat_messages`").
		WithArgs(
			int64(3), "", "user", "[]", "",
			0, 0, 0, 0, 0, 0, 0,
			"", "", 1,
			sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectCommit()

	m := &chat_entity.Message{SessionID: 3, Role: "user", BlocksJSON: "[]", Seq: 1}
	err := chat_repo.NewMessage().Create(ctx, m)
	assert.NoError(t, err)
	assert.Equal(t, int64(42), m.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_Find(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE id = \\? ORDER BY `chat_messages`.`id` LIMIT \\?").
		WithArgs(int64(42), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "blocks_json", "seq"}).
			AddRow(42, 3, "assistant", `[]`, 4))

	got, err := chat_repo.NewMessage().Find(ctx, 42)
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, int64(42), got.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_Find_NotFound(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE id = \\? ORDER BY `chat_messages`.`id` LIMIT \\?").
		WithArgs(int64(99), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, err := chat_repo.NewMessage().Find(ctx, 99)
	assert.NoError(t, err)
	assert.Nil(t, got, "missing row 应返回 nil 而不是 ErrRecordNotFound")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_DeleteFromSeq(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `chat_messages` WHERE session_id = \\? AND seq >= \\?").
		WithArgs(int64(3), 5).
		WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectCommit()

	deleted, err := chat_repo.NewMessage().DeleteFromSeq(ctx, 3, 5)
	assert.NoError(t, err)
	assert.Equal(t, int64(4), deleted)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_FlipSubagentStatus_FlipsMatchingBlock(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	blocksJSON := `[{"type":"subagent_state","data":{"parent_tool_call_id":"tu1","kind":"local_bash","description":"sleep 20","status":"running"}}]`

	// 倒序拉近 N 条 assistant 消息。
	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE session_id = \\? AND role = \\? ORDER BY seq DESC LIMIT \\?").
		WithArgs(int64(3), "assistant", 50).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "blocks_json", "seq"}).
			AddRow(42, 3, "assistant", blocksJSON, 4))

	// 命中后重写该条:status 翻成 completed。
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `chat_messages` SET ").
		WithArgs(
			sqlmock.AnyArg(),                                                                         // session_id
			sqlmock.AnyArg(),                                                                         // device_id
			sqlmock.AnyArg(),                                                                         // role
			sqlmock.AnyArg(),                                                                         // blocks_json (翻转后)
			sqlmock.AnyArg(),                                                                         // model
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), // token 列
			sqlmock.AnyArg(),                   // total_input_tokens
			sqlmock.AnyArg(),                   // duration_ms
			sqlmock.AnyArg(),                   // fork_anchor
			sqlmock.AnyArg(),                   // error_text
			sqlmock.AnyArg(),                   // seq
			sqlmock.AnyArg(), sqlmock.AnyArg(), // createtime / updatetime
			int64(42), // WHERE id
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := chat_repo.NewMessage().FlipSubagentStatus(ctx, 3, "tu1", "completed", "Background command completed")
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_FlipSubagentStatus_NoMatchSilentNil(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	// 没有任何 subagent_state 命中 → 不写库,静默返回 nil。
	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE session_id = \\? AND role = \\? ORDER BY seq DESC LIMIT \\?").
		WithArgs(int64(3), "assistant", 50).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "blocks_json", "seq"}).
			AddRow(42, 3, "assistant", `[{"type":"text","data":{"text":"hi"}}]`, 4))

	err := chat_repo.NewMessage().FlipSubagentStatus(ctx, 3, "tu-missing", "completed", "")
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAppendSubagentChildrenInBlocksJSON 直接单测 JSON 改写核心:把子块追加进 subagent_state。
func TestAppendSubagentChildrenInBlocksJSON(t *testing.T) {
	const baseBlocks = `[` +
		`{"type":"tool_use","data":{"id":"toolu_agent","name":"Task","input":{"description":"run something"}}},` +
		`{"type":"subagent_state","data":{"parent_tool_call_id":"toolu_agent","kind":"local_bash","description":"run something","status":"running","nested_tool_call_ids":[]}}` +
		`]`

	childBlocks := `[` +
		`{"type":"tool_use","data":{"id":"sub_bash","name":"Bash","input":{"command":"ls"}}},` +
		`{"type":"tool_result","data":{"id":"sub_bash","content":"file1.txt"}}` +
		`]`

	t.Run("追加子块并更新 nested_tool_call_ids", func(t *testing.T) {
		out, ok, err := chat_repo.AppendSubagentChildrenInBlocksJSON(baseBlocks, "toolu_agent", childBlocks, []string{"sub_bash"})
		require.NoError(t, err)
		assert.True(t, ok)
		// nested_tool_call_ids 应包含 sub_bash。
		data := subagentData(t, out)
		ids, _ := data["nested_tool_call_ids"].([]any)
		assert.Equal(t, []any{"sub_bash"}, ids)
		// 子块被追加到末尾。
		assert.Contains(t, out, `"sub_bash"`)
		assert.Contains(t, out, `"Bash"`)
		assert.Contains(t, out, `"tool_result"`)
		// 原有块仍在。
		assert.Contains(t, out, `"tool_use"`)
		assert.Contains(t, out, `"toolu_agent"`)
	})

	t.Run("childIDs 去重", func(t *testing.T) {
		// nested_tool_call_ids 已有 existing_id。
		withExisting := `[{"type":"subagent_state","data":{"parent_tool_call_id":"toolu_agent","status":"running","nested_tool_call_ids":["existing_id"]}}]`
		out, ok, err := chat_repo.AppendSubagentChildrenInBlocksJSON(withExisting, "toolu_agent", `[]`, []string{"existing_id", "new_id"})
		require.NoError(t, err)
		assert.True(t, ok)
		data := subagentData(t, out)
		ids, _ := data["nested_tool_call_ids"].([]any)
		// existing_id 不重复,new_id 补进去。
		assert.Equal(t, []any{"existing_id", "new_id"}, ids)
	})

	t.Run("无命中返回 false", func(t *testing.T) {
		out, ok, err := chat_repo.AppendSubagentChildrenInBlocksJSON(baseBlocks, "toolu_missing", childBlocks, []string{"sub_bash"})
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Equal(t, baseBlocks, out)
	})

	t.Run("空 blocksJSON 返回 false", func(t *testing.T) {
		out, ok, err := chat_repo.AppendSubagentChildrenInBlocksJSON("", "toolu_agent", childBlocks, []string{"sub_bash"})
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Equal(t, "", out)
	})

	t.Run("非法 blocksJSON 返回 error", func(t *testing.T) {
		_, ok, err := chat_repo.AppendSubagentChildrenInBlocksJSON("{not json", "toolu_agent", childBlocks, []string{"sub_bash"})
		require.Error(t, err)
		assert.False(t, ok)
	})
}

// TestFlipAndAppendCompose 证明两个纯 JSON 改写 helper 在任意执行顺序下可以安全组合:
// Flip(Append(...)) 和 Append(Flip(...)) 产出的结果都同时包含嵌套子块和 completed 状态。
// 这是 per-session mutex 序列化并发写的正确性依据 —— 先后无关,两个路径不互相覆写对方的字段。
func TestFlipAndAppendCompose(t *testing.T) {
	// 基础 blocks_json:一个空 subagent_state(running,nested_tool_call_ids 为空)。
	const base = `[` +
		`{"type":"subagent_state","data":{"parent_tool_call_id":"toolu_agent","kind":"local_bash","description":"run something","status":"running","nested_tool_call_ids":[]}}` +
		`]`

	nestedBlock := `[{"type":"tool_use","data":{"id":"sub_bash","name":"Bash","input":{"command":"ls"}}}]`

	assertBothPresent := func(t *testing.T, result string) {
		t.Helper()
		data := subagentData(t, result)
		// Flip 的效果:status == "completed"。
		assert.Equal(t, "completed", data["status"])
		// Append 的效果:nested_tool_call_ids 包含 "sub_bash"。
		ids, _ := data["nested_tool_call_ids"].([]any)
		assert.Contains(t, ids, "sub_bash")
		// 子块被追加到顶层数组末尾。
		assert.Contains(t, result, `"sub_bash"`)
	}

	t.Run("Flip-then-Append", func(t *testing.T) {
		// 先 Flip(status running→completed),再 Append(追加子块)。
		flipped, _, err := chat_repo.FlipSubagentInBlocksJSON(base, "toolu_agent", "completed", "done")
		require.NoError(t, err)
		result, _, err := chat_repo.AppendSubagentChildrenInBlocksJSON(flipped, "toolu_agent", nestedBlock, []string{"sub_bash"})
		require.NoError(t, err)
		assertBothPresent(t, result)
	})

	t.Run("Append-then-Flip", func(t *testing.T) {
		// 先 Append(追加子块),再 Flip(status running→completed)。
		appended, _, err := chat_repo.AppendSubagentChildrenInBlocksJSON(base, "toolu_agent", nestedBlock, []string{"sub_bash"})
		require.NoError(t, err)
		result, _, err := chat_repo.FlipSubagentInBlocksJSON(appended, "toolu_agent", "completed", "done")
		require.NoError(t, err)
		assertBothPresent(t, result)
	})
}

func TestMessageRepo_AppendSubagentChildren_AppendsBlocks(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	blocksJSON := `[` +
		`{"type":"tool_use","data":{"id":"toolu_agent","name":"Task","input":{"description":"run something"}}},` +
		`{"type":"subagent_state","data":{"parent_tool_call_id":"toolu_agent","kind":"local_bash","description":"run something","status":"running","nested_tool_call_ids":[]}}` +
		`]`
	childBlocksJSON := `[{"type":"tool_use","data":{"id":"sub_bash","name":"Bash","input":{"command":"ls"}}}]`

	// 倒序拉近 N 条 assistant 消息。
	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE session_id = \\? AND role = \\? ORDER BY seq DESC LIMIT \\?").
		WithArgs(int64(3), "assistant", 50).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "blocks_json", "seq"}).
			AddRow(42, 3, "assistant", blocksJSON, 4))

	// 命中后重写该条。blocks_json 参数必须包含追加的子块 id("sub_bash")和子块
	// 的 name("Bash"),以确保方法不会把原始(未重写)的 JSON 传给 Update。
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `chat_messages` SET ").
		WithArgs(
			sqlmock.AnyArg(), // session_id
			sqlmock.AnyArg(), // device_id
			sqlmock.AnyArg(), // role
			blocksJSONContainsMatcher{substrings: []string{"sub_bash", "\"Bash\""}}, // blocks_json (追加后,含子块 id 及子块 name)
			sqlmock.AnyArg(),                                                                         // model
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), // token 列
			sqlmock.AnyArg(),                   // total_input_tokens
			sqlmock.AnyArg(),                   // duration_ms
			sqlmock.AnyArg(),                   // fork_anchor
			sqlmock.AnyArg(),                   // error_text
			sqlmock.AnyArg(),                   // seq
			sqlmock.AnyArg(), sqlmock.AnyArg(), // createtime / updatetime
			int64(42), // WHERE id
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := chat_repo.NewMessage().AppendSubagentChildren(ctx, 3, "toolu_agent", childBlocksJSON, []string{"sub_bash"})
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_AppendSubagentChildren_NoMatchSilentNil(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	// 没有 subagent_state 命中 → 不写库,静默返回 nil。
	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE session_id = \\? AND role = \\? ORDER BY seq DESC LIMIT \\?").
		WithArgs(int64(3), "assistant", 50).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "blocks_json", "seq"}).
			AddRow(42, 3, "assistant", `[{"type":"text","data":{"text":"hi"}}]`, 4))

	err := chat_repo.NewMessage().AppendSubagentChildren(ctx, 3, "toolu_missing", `[{"type":"tool_use","data":{}}]`, []string{"x"})
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_FindAssistantBySubagentToolUseID_ReturnsMatch(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	matchBlocks := `[{"type":"subagent_state","data":{"parent_tool_call_id":"toolu_agent","kind":"local_agent","description":"do work","status":"running"}}]`
	otherBlocks := `[{"type":"text","data":{"text":"hi"}}]`

	// 倒序拉近 N 条 assistant 消息;返回第一条 blocks 含命中 subagent_state 的。
	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE session_id = \\? AND role = \\? ORDER BY seq DESC LIMIT \\?").
		WithArgs(int64(3), "assistant", 50).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "blocks_json", "seq"}).
			AddRow(43, 3, "assistant", otherBlocks, 5).
			AddRow(42, 3, "assistant", matchBlocks, 4))

	got, err := chat_repo.NewMessage().FindAssistantBySubagentToolUseID(ctx, 3, "toolu_agent")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(42), got.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_FindAssistantBySubagentToolUseID_NoMatchNil(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE session_id = \\? AND role = \\? ORDER BY seq DESC LIMIT \\?").
		WithArgs(int64(3), "assistant", 50).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "blocks_json", "seq"}).
			AddRow(42, 3, "assistant", `[{"type":"text","data":{"text":"hi"}}]`, 4))

	got, err := chat_repo.NewMessage().FindAssistantBySubagentToolUseID(ctx, 3, "toolu_missing")
	require.NoError(t, err)
	assert.Nil(t, got, "无命中 subagent_state 应返回 (nil, nil)")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_FindAssistantBySubagentToolUseID_EmptyToolUseID(t *testing.T) {
	ctx, _, _ := testutils.Database(t)

	got, err := chat_repo.NewMessage().FindAssistantBySubagentToolUseID(ctx, 3, "")
	require.NoError(t, err)
	assert.Nil(t, got, "空 toolUseID 短路返回 (nil, nil),不查库")
}

func TestMessageRepo_Update(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `chat_messages` SET ").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	m := &chat_entity.Message{ID: 42, SessionID: 3, Role: "assistant", BlocksJSON: `[{"type":"text"}]`, Seq: 2}
	err := chat_repo.NewMessage().Update(ctx, m)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
