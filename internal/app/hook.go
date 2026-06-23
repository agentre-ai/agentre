package app

import (
	"github.com/agentre-ai/agentre/internal/service/hook_svc"
)

// LoadHooks 聚合返回 Hook 信号源、路由规则、事件日志与可选目标 Agent。
func (a *App) LoadHooks(req *hook_svc.LoadHooksRequest) (*hook_svc.LoadHooksResponse, error) {
	return hook_svc.Hook().Load(a.ctx, req)
}

// CreateHookSource 新建 Hook 信号源，并自动创建 CEO 兜底规则。
func (a *App) CreateHookSource(req *hook_svc.CreateHookSourceRequest) (*hook_svc.CreateHookSourceResponse, error) {
	return hook_svc.Hook().CreateSource(a.ctx, req)
}

// UpdateHookSource 更新 Hook 信号源配置。
func (a *App) UpdateHookSource(req *hook_svc.UpdateHookSourceRequest) (*hook_svc.UpdateHookSourceResponse, error) {
	return hook_svc.Hook().UpdateSource(a.ctx, req)
}

// DeleteHookSource 软删除 Hook 信号源。
func (a *App) DeleteHookSource(req *hook_svc.DeleteHookSourceRequest) (*hook_svc.DeleteHookSourceResponse, error) {
	return hook_svc.Hook().DeleteSource(a.ctx, req)
}

// CreateHookRule 新建 Hook 路由规则。
func (a *App) CreateHookRule(req *hook_svc.CreateHookRuleRequest) (*hook_svc.CreateHookRuleResponse, error) {
	return hook_svc.Hook().CreateRule(a.ctx, req)
}

// UpdateHookRule 更新 Hook 路由规则。
func (a *App) UpdateHookRule(req *hook_svc.UpdateHookRuleRequest) (*hook_svc.UpdateHookRuleResponse, error) {
	return hook_svc.Hook().UpdateRule(a.ctx, req)
}

// DeleteHookRule 软删除 Hook 路由规则；兜底规则不可删除。
func (a *App) DeleteHookRule(req *hook_svc.DeleteHookRuleRequest) (*hook_svc.DeleteHookRuleResponse, error) {
	return hook_svc.Hook().DeleteRule(a.ctx, req)
}

// TestHookSource 生成一条本地测试事件，验证连接配置和路由规则。
func (a *App) TestHookSource(req *hook_svc.TestHookSourceRequest) (*hook_svc.TestHookSourceResponse, error) {
	return hook_svc.Hook().TestSource(a.ctx, req)
}

// SyncHookEmailSource 真实连接 IMAP 邮箱，拉取未读邮件并写入 Hook 事件日志。
func (a *App) SyncHookEmailSource(req *hook_svc.SyncEmailSourceRequest) (*hook_svc.SyncEmailSourceResponse, error) {
	return hook_svc.Hook().SyncEmailSource(a.ctx, req)
}

// RedeliverHookEvent 重新派发事件；当前仅记录派发意图，不启动 Agent runtime。
func (a *App) RedeliverHookEvent(req *hook_svc.RedeliverHookEventRequest) (*hook_svc.RedeliverHookEventResponse, error) {
	return hook_svc.Hook().RedeliverEvent(a.ctx, req)
}
