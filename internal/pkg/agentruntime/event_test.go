package agentruntime

import "testing"

// TestEventSealedMarker 编译期断言所有 Event 实现都走 isEvent() 标记接口。
// 不能 build = sealed marker 缺失 → CI 立即报错。
func TestEventSealedMarker(t *testing.T) {
	var _ Event = TextDelta{}
	var _ Event = ThinkingDelta{}
	var _ Event = ToolCall{}
	var _ Event = ToolResult{}
	var _ Event = SteerConsumed{}
	var _ Event = UserAskRequest{}
	var _ Event = UserAskResolved{}
	var _ Event = ToolPermissionRequest{}
	var _ Event = ToolPermissionResolved{}
	var _ Event = PermissionModeChanged{}
	var _ Event = SubagentStarted{}
	var _ Event = SubagentProgress{}
	var _ Event = SubagentDone{}
	var _ Event = Retry{}
	var _ Event = UsageUpdate{}
	var _ Event = ContextWindowUpdated{}
	var _ Event = Done{}
	var _ Event = ErrorEvent{}
}
