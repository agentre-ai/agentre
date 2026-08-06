package agentruntime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
)

type stubRuntime struct{ name string }

func (stubRuntime) Capabilities() capability.Capabilities { return capability.Capabilities{} }

func (s stubRuntime) Run(_ context.Context, _ RunRequest) (<-chan Event, *RunResult, error) {
	ch := make(chan Event)
	close(ch)
	return ch, &RunResult{ProviderSessionID: s.name}, nil
}

func TestRuntimeFor_ReturnsNilForUnknownType(t *testing.T) {
	assert.Nil(t, RuntimeFor("definitely-not-a-real-backend"))
	assert.Nil(t, RuntimeFor(""))
}

func TestSwapRuntimeForTest_RoundTrip(t *testing.T) {
	t.Run("替换未注册的 type 后 restore 会清除", func(t *testing.T) {
		const fakeType agent_backend_entity.BackendType = "swaptest-unknown"
		assert.Nil(t, RuntimeFor(fakeType))
		restore := SwapRuntimeForTest(fakeType, stubRuntime{name: "x"})
		assert.NotNil(t, RuntimeFor(fakeType))
		restore()
		assert.Nil(t, RuntimeFor(fakeType))
	})

	t.Run("替换已注册的 type 后 restore 会还原", func(t *testing.T) {
		const fakeType agent_backend_entity.BackendType = "swaptest-existing"
		original := stubRuntime{name: "orig"}
		RegisterRuntime(fakeType, original)
		t.Cleanup(func() {
			registryMu.Lock()
			delete(registry, fakeType)
			registryMu.Unlock()
		})

		restore := SwapRuntimeForTest(fakeType, stubRuntime{name: "swapped"})
		got := RuntimeFor(fakeType)
		s, ok := got.(stubRuntime)
		assert.True(t, ok)
		assert.Equal(t, "swapped", s.name)

		restore()
		got = RuntimeFor(fakeType)
		s2, ok := got.(stubRuntime)
		assert.True(t, ok)
		assert.Equal(t, "orig", s2.name)
	})
}

// TestWaiterSnapshot_ToolInputCrossesTheWireAsRawJSON 钉住 R7 的载荷形状:
// 待决策查询要让重连的客户端重建**同一张**审批卡,所以枚举出来的 Input 在
// JSON 上必须与实时 ToolPermissionRequest 帧里的 input 同形 —— 一个可以直接
// JSON.parse 的对象,而不是 base64 字符串。
//
// 会被拒绝的错误实现:把 Input 声明成 []byte(encoding/json 对 []byte 走
// base64),客户端在实时路径拿到 {"command":"ls -la"}、在补齐路径拿到
// "eyJjb21tYW5kIjoi…",同一张卡要走两套解析。
func TestWaiterSnapshot_ToolInputCrossesTheWireAsRawJSON(t *testing.T) {
	raw := json.RawMessage(`{"command":"ls -la"}`)

	live, err := json.Marshal(ToolPermissionRequest{RequestID: "p-1", ToolName: "Bash", Input: raw})
	require.NoError(t, err)
	assert.Contains(t, string(live), `"input":{"command":"ls -la"}`,
		"实时事件路径的基准:input 是原样 JSON 对象")

	snap, err := json.Marshal(WaiterSnapshot{
		ToolPermissions: []PendingToolPermission{{RequestID: "p-1", ToolName: "Bash", Input: raw}},
	})
	require.NoError(t, err)

	var decoded struct {
		ToolPermissions []struct {
			Input any
		}
	}
	require.NoError(t, json.Unmarshal(snap, &decoded))
	require.Len(t, decoded.ToolPermissions, 1)
	assert.IsType(t, map[string]any{}, decoded.ToolPermissions[0].Input,
		"审批卡的 ToolInput 是对象,客户端直接读字段;base64 字符串重建不出卡片")

	var back WaiterSnapshot
	require.NoError(t, json.Unmarshal(snap, &back))
	assert.JSONEq(t, string(raw), string(back.ToolPermissions[0].Input),
		"往返后字节等价,SubmitToolPermission 的 UpdatedInput 才拼得回去")
}

// TestWaiterSnapshot_QuestionsSurviveTheWire 覆盖提问卡一半:AskQuestion 的
// 选项/多选/header 都要跨进程活下来,否则重连后重建的提问卡少了可选项。
func TestWaiterSnapshot_QuestionsSurviveTheWire(t *testing.T) {
	want := WaiterSnapshot{
		AskUserQuestions: []PendingAskUserQuestion{{
			RequestID: "a-1",
			Questions: []AskQuestion{{
				ID:          "q1",
				Question:    "继续吗？",
				Header:      "确认",
				MultiSelect: true,
				Options:     []AskOption{{Label: "好", Description: "继续", Preview: "p"}},
			}},
		}},
	}

	blob, err := json.Marshal(want)
	require.NoError(t, err)
	var got WaiterSnapshot
	require.NoError(t, json.Unmarshal(blob, &got))

	assert.Equal(t, want, got)
}
