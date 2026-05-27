package chat_svc

import "agentre/internal/pkg/agentruntime"

// ConvertOldEventToNewForTest 暴露 convertOldEventToNew 给 chat_svc_test 包的
// fake runner。生产路径 (runTurn drain) 直接吃 NEW Event channel,本函数仅给
// 老 fixture 当桥接(用 RuntimeEvent{Kind: ...} 字面量驱动,内部转 NEW Event
// 再回吐给 chat_svc dispatcher)。fixture 全部改写成直接构造 NEW Event 后即可删除。
func ConvertOldEventToNewForTest(ev agentruntime.RuntimeEvent) agentruntime.Event {
	return convertOldEventToNew(ev)
}
