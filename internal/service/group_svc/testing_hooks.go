package group_svc

import "context"

// SetEmitterForTest 给外部测试包注入自定义 emitter(观察 run_status 转移而非裸读共享指针, 防 -race)。
func SetEmitterForTest(svc GroupSvc, e Emitter) {
	if s, ok := svc.(*groupSvc); ok && e != nil {
		s.emitter = e
	}
}

// EnqueueForTest 暴露内部 enqueueDeliveries 给外部测试包(驱动调度器, 不经 Send 路径)。
func EnqueueForTest(svc GroupSvc, groupID int64, recipientIDs []int64, content, fromName string) {
	if s, ok := svc.(*groupSvc); ok {
		s.enqueueDeliveries(groupID, recipientIDs, content, fromName)
	}
}

// KickForTest 暴露内部 kick 给外部测试包(手动踢一次调度)。
func KickForTest(svc GroupSvc, ctx context.Context, groupID int64) {
	if s, ok := svc.(*groupSvc); ok {
		s.kick(ctx, groupID)
	}
}

// MarkInflightForTest 手动把某成员置为在跑(单测用, 模拟一个进行中的 turn)。
func MarkInflightForTest(svc GroupSvc, groupID, memberID int64) {
	if g, ok := svc.(*groupSvc); ok {
		sc := g.schedulerFor(groupID)
		sc.mu.Lock()
		sc.inflight[memberID] = true
		sc.mu.Unlock()
	}
}

// MintTokenForTest 在该 svc 的 group MCP 上签一个 (group, member) token(单测验证吊销用)。
func MintTokenForTest(svc GroupSvc, groupID, memberID int64) string {
	if s, ok := svc.(*groupSvc); ok {
		return s.mcp.MintToken(groupID, memberID)
	}
	return ""
}

// TokenValidForTest 报告该 token 是否仍能解析到成员(吊销后应为 false)。
func TokenValidForTest(svc GroupSvc, token string) bool {
	if s, ok := svc.(*groupSvc); ok {
		_, ok := s.mcp.lookup(token)
		return ok
	}
	return false
}
