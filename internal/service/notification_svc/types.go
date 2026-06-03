// Package notification_svc 暴露应用级系统通知能力。
// 决策（要不要弹、文案 i18n）在前端；本服务只负责把一条已成型的通知交给平台原生实现。
package notification_svc

// ShowRequest 展示一条系统通知。Title/Body 已由前端按 i18n 生成。
type ShowRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}
