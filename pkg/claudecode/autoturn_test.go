package claudecode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsBackgroundTaskNotification 钉死「后台命令完成」与「subagent 完成」两类
// task_notification 的辨析 —— 前者起自主续轮,后者是 turn 内的 SubagentDone。
func TestIsBackgroundTaskNotification(t *testing.T) {
	parse := func(s string) rawFrame {
		var f rawFrame
		if err := json.Unmarshal([]byte(s), &f); err != nil {
			t.Fatalf("bad fixture: %v", err)
		}
		return f
	}

	cases := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "后台型(有 output_file,无 subagent_type)",
			line: `{"type":"system","subtype":"task_notification","task_id":"bg1","tool_use_id":"tu1","status":"completed","output_file":"/tmp/tasks/bg1.output","summary":"Background command completed"}`,
			want: true,
		},
		{
			name: "subagent 型(有 subagent_type,无 output_file)",
			line: `{"type":"system","subtype":"task_notification","task_id":"t9","subagent_type":"general","description":"explore","status":"completed","summary":"Subagent finished"}`,
			want: false,
		},
		{
			name: "task_started 非 notification",
			line: `{"type":"system","subtype":"task_started","task_id":"t9","subagent_type":"general"}`,
			want: false,
		},
		{
			name: "init 帧",
			line: `{"type":"system","subtype":"init","session_id":"s","model":"m"}`,
			want: false,
		},
		{
			name: "assistant 帧",
			line: `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, isBackgroundTaskNotification(parse(c.line)))
		})
	}
}
