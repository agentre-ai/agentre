package wire

import (
	"encoding/json"
	"errors"
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/jsonrpc"
)

func TestToFromJSONRPCError_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
	}{
		{"no active turn", agentruntime.ErrNoActiveTurn, ErrCodeNoActiveTurn},
		{"steer not found", agentruntime.ErrSteerNotFound, ErrCodeSteerNotFound},
		{"unsupported", agentruntime.ErrUnsupported, ErrCodeUnsupported},
		{"aborted", agentruntime.ErrAborted, ErrCodeAborted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := ToJSONRPCError(tc.err)
			require.NotNil(t, out, "sentinel must map to a code")
			assert.Equal(t, tc.code, out.Code)
			// Wire bytes round-trip preserves code.
			b, err := json.Marshal(out)
			require.NoError(t, err)
			var back jsonrpc.Error
			require.NoError(t, json.Unmarshal(b, &back))
			rehydrated := FromJSONRPCError(&back)
			assert.ErrorIs(t, rehydrated, tc.err)
		})
	}
}

func TestToJSONRPCError_NonSentinel(t *testing.T) {
	// 非 sentinel 错误返 nil,让 daemon 自己用 rpc.ErrInternal 包。
	assert.Nil(t, ToJSONRPCError(errors.New("random")))
	assert.Nil(t, ToJSONRPCError(nil))
}

func TestFromJSONRPCError_Passthrough(t *testing.T) {
	// 未知 code 原样返。
	in := &jsonrpc.Error{Code: -99999, Message: "weird"}
	out := FromJSONRPCError(in)
	assert.Equal(t, "weird", out.Error())
	// 完全非 jsonrpc.Error 也原样返。
	in2 := errors.New("plain error")
	out2 := FromJSONRPCError(in2)
	assert.Same(t, in2, out2)
}

// TestErrCodes_Stable pins error code values — wire protocol contract.
// Bumping these means every released agentred + agentre must upgrade in lock-step.
func TestErrCodes_Stable(t *testing.T) {
	assert.Equal(t, -32010, ErrCodeNoActiveTurn)
	assert.Equal(t, -32011, ErrCodeSteerNotFound)
	assert.Equal(t, -32012, ErrCodeUnsupported)
	assert.Equal(t, -32013, ErrCodeAborted)
}

// TestMethodNames_Stable pins RPC method names — wire protocol contract.
func TestMethodNames_Stable(t *testing.T) {
	for k, v := range map[string]string{
		MethodCapabilities:          "runtime.capabilities",
		MethodRun:                   "runtime.run",
		MethodSteer:                 "runtime.steer",
		MethodCancelSteer:           "runtime.cancelSteer",
		MethodDrainPending:          "runtime.drainPending",
		MethodAbort:                 "runtime.abort",
		MethodSetPermissionMode:     "runtime.setPermissionMode",
		MethodSubmitAnswer:          "runtime.submitAnswer",
		MethodSubmitToolPermission:  "runtime.submitToolPermission",
		MethodGetGoal:               "runtime.goal.get",
		MethodSetGoal:               "runtime.goal.set",
		MethodClearGoal:             "runtime.goal.clear",
		NotifyEvent:                 "runtime.event",
		NotifyRunResultDone:         "runtime.runResultDone",
		NotifyAutonomousTurnStarted: "runtime.autonomousTurn.started",
		NotifyAutonomousTurnEvent:   "runtime.autonomousTurn.event",
		NotifyAutonomousTurnDone:    "runtime.autonomousTurn.done",
	} {
		assert.Equal(t, v, k)
	}
}

func TestAutonomousTurnStartedFrame_RoundTrip(t *testing.T) {
	in := AutonomousTurnStartedFrame{SessionID: 77, Trigger: "background_task"}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out AutonomousTurnStartedFrame
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in, out)
}

func TestEventFrame_RoundTrip(t *testing.T) {
	// 用一个 sealed event 走完整 marshal -> EventFrame -> unmarshal -> UnmarshalEvent 链路。
	ev := agentruntime.TextDelta{Text: "hi"}
	body, err := json.Marshal(ev)
	require.NoError(t, err)

	frame := EventFrame{SessionID: 42, Event: body}
	b, err := json.Marshal(frame)
	require.NoError(t, err)

	var decoded EventFrame
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, int64(42), decoded.SessionID)

	out, err := agentruntime.UnmarshalEvent(decoded.Event)
	require.NoError(t, err)
	assert.Equal(t, ev, out)
}

func TestRunResultDoneFrame_RoundTrip(t *testing.T) {
	in := RunResultDoneFrame{
		SessionID:         99,
		ProviderSessionID: "sess-1",
		Usage: &UsageWire{
			PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150,
		},
		UserAnchor:    "u-1",
		Model:         "claude-sonnet-4-6",
		ContextWindow: 200000,
		StopErrMsg:    "aborted by user",
		StopErrCode:   ErrCodeAborted,
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out RunResultDoneFrame
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in, out)
}

// TestRunParams_RawBackendOpaque verifies Backend is passed as raw bytes,
// keeping wire schema decoupled from agent_backend_entity layout.
func TestRunParams_RawBackendOpaque(t *testing.T) {
	in := RunParams{
		Backend:        json.RawMessage(`{"ID":1,"Type":"claudecode","Name":"x"}`),
		AgentID:        7,
		SessionID:      42,
		UserText:       "hello",
		Compact:        true,
		PermissionMode: "acceptEdits",
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out RunParams
	require.NoError(t, json.Unmarshal(b, &out))
	// JSONEq because key ordering inside Backend may differ.
	assert.JSONEq(t, string(in.Backend), string(out.Backend))
	assert.Equal(t, in.AgentID, out.AgentID)
	assert.Equal(t, in.SessionID, out.SessionID)
	assert.Equal(t, in.UserText, out.UserText)
	assert.Equal(t, in.Compact, out.Compact)
	assert.Equal(t, in.PermissionMode, out.PermissionMode)
}

func TestRunParams_UserBlocksRoundTrip(t *testing.T) {
	// Given a multimodal user message crossing desktop -> agentred,
	// when RunParams is marshaled, then text and inline image bytes survive.
	stored, err := cagoblocks.EncodeAll([]cagoblocks.ContentBlock{
		cagoblocks.TextBlock{Text: "what is this?"},
		cagoblocks.ImageBlock{
			MediaType: "image/png",
			Source:    cagoblocks.BlobSource{Inline: []byte{0x89, 0x50, 0x4e, 0x47}},
		},
	})
	require.NoError(t, err)

	in := RunParams{SessionID: 42, UserText: "what is this?", UserBlocks: stored}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"userBlocks"`)

	var out RunParams
	require.NoError(t, json.Unmarshal(b, &out))
	decoded, err := cagoblocks.DecodeAll(out.UserBlocks)
	require.NoError(t, err)
	require.Len(t, decoded, 2)
	assert.Equal(t, "what is this?", decoded[0].(cagoblocks.TextBlock).Text)
	img := decoded[1].(cagoblocks.ImageBlock)
	assert.Equal(t, "image/png", img.MediaType)
	assert.Equal(t, []byte{0x89, 0x50, 0x4e, 0x47}, img.Source.Inline)
}

// TestParams_FieldShape spot-checks lowerCamelCase tagging by walking a
// representative param set. Adding a struct field without a json tag is a
// common drift; an UPPER-cased key in the wire output would surface here.
func TestParams_FieldShape(t *testing.T) {
	specs := []struct {
		name string
		v    any
		want []string // each expected `"key":` substring
	}{
		{"steer", SteerParams{SessionID: 1, QueuedID: "q", Text: "t"},
			[]string{`"sessionId":1`, `"queuedId":"q"`, `"text":"t"`}},
		{"cancelSteer", CancelSteerParams{SessionID: 1, QueuedID: "q"},
			[]string{`"sessionId":1`, `"queuedId":"q"`}},
		{"drain", DrainParams{SessionID: 1}, []string{`"sessionId":1`}},
		{"abort", AbortParams{SessionID: 1}, []string{`"sessionId":1`}},
		{"setMode", SetPermissionModeParams{SessionID: 1, Mode: "plan"},
			[]string{`"sessionId":1`, `"mode":"plan"`}},
		{"submitAnswer", SubmitAnswerParams{SessionID: 1, RequestID: "r", Skipped: true},
			[]string{`"sessionId":1`, `"requestId":"r"`, `"skipped":true`}},
		{"submitToolPerm", SubmitToolPermissionParams{
			SessionID: 1, RequestID: "r", Allow: true, AlwaysAllowSession: true, DenyReason: "x",
		}, []string{`"sessionId":1`, `"requestId":"r"`, `"allow":true`, `"alwaysAllowSession":true`, `"denyReason":"x"`}},
		{"goalGet", GoalParams{SessionID: 1, AgentID: 9, ProviderSessionID: "thread-1", Backend: json.RawMessage(`{"Type":"codex"}`)},
			[]string{`"sessionId":1`, `"agentId":9`, `"providerSessionId":"thread-1"`, `"backend":`, `"Type":"codex"`}},
		{"goalSet", GoalParams{SessionID: 1, AgentID: 9, ProviderSessionID: "thread-1", Backend: json.RawMessage(`{"Type":"codex"}`), Objective: ptrString("ship"), Status: ptrString("active"), TokenBudget: ptrInt(123)},
			[]string{`"sessionId":1`, `"agentId":9`, `"providerSessionId":"thread-1"`, `"backend":`, `"Type":"codex"`, `"objective":"ship"`, `"status":"active"`, `"tokenBudget":123`}},
		{"goalResult", GoalResult{Goal: &agentruntime.Goal{ThreadID: "thread-1", Objective: "ship", Status: "active"}},
			[]string{`"goal":`, `"threadId":"thread-1"`, `"objective":"ship"`, `"status":"active"`}},
		{"goalClearResult", GoalClearResult{Cleared: true}, []string{`"cleared":true`}},
		{"capabilities", CapabilitiesParams{BackendType: "claudecode"},
			[]string{`"backendType":"claudecode"`}},
		{"runAck", RunAck{SessionID: 42}, []string{`"sessionId":42`}},
		{"runParamsCompact", RunParams{SessionID: 42, Compact: true}, []string{`"sessionId":42`, `"compact":true`}},
		{"cancelSteerResult", CancelSteerResult{Removed: []string{"a", "b"}},
			[]string{`"removed":["a","b"]`}},
	}
	for _, s := range specs {
		t.Run(s.name, func(t *testing.T) {
			b, err := json.Marshal(s.v)
			require.NoError(t, err)
			for _, w := range s.want {
				assert.Contains(t, string(b), w, "missing field: expected %s in %s", w, string(b))
			}
		})
	}
}

// 编译时确认 *jsonrpc.Error 满足 error,这样 ToJSONRPCError 返回的可以直接 return。
var _ error = (*jsonrpc.Error)(nil)

func ptrString(v string) *string { return &v }
func ptrInt(v int) *int          { return &v }
