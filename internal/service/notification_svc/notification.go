package notification_svc

import (
	"context"
	"strings"
)

//go:generate mockgen -source notification.go -destination mock_notification_svc/mock_notification.go

// Notifier 平台原生通知能力，由 internal/pkg/sysnotify 提供实现，bootstrap 注入。
type Notifier interface {
	Notify(title, body string) error
}

// NotificationSvc 应用通知服务。
type NotificationSvc interface {
	Show(ctx context.Context, req *ShowRequest) error
}

type notificationSvc struct {
	notifier Notifier
}

var defaultSvc = &notificationSvc{}

// Notification 取默认服务单例。
func Notification() NotificationSvc { return defaultSvc }

// RegisterNotifier 由 bootstrap 注入平台通知实现。
func RegisterNotifier(n Notifier) { defaultSvc.notifier = n }

// Show 弹一条系统通知；未注入实现或 req 为空时安全 no-op。空 title 兜底为 "Agentre"。
func (s *notificationSvc) Show(_ context.Context, req *ShowRequest) error {
	if s.notifier == nil || req == nil {
		return nil
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Agentre"
	}
	return s.notifier.Notify(title, req.Body)
}
