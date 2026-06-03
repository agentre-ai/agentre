package app

import "agentre/internal/service/notification_svc"

// ShowNotification 弹一条系统通知；文案已由前端按 i18n 生成。
func (a *App) ShowNotification(req *notification_svc.ShowRequest) error {
	return notification_svc.Notification().Show(a.ctx, req)
}
