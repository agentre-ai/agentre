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
