package chat_repo_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/repository/chat_repo"
)

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
