// Package notification_svc 暴露应用级系统通知能力。
// 决策（要不要弹、文案 i18n）在前端；本服务只负责把一条已成型的通知交给平台原生实现。
package notification_svc

// ShowRequest 展示一条系统通知。Title/Body 已由前端按 i18n 生成。
// SessionID 标识来源会话，供点击通知时聚焦/跳转（0 = 无具体会话）。
type ShowRequest struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	SessionID int64  `json:"sessionId"`
}
