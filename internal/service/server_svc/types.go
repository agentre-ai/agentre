package server_svc

import "errors"

// StartLoginResult 是 StartLogin 返回给前端、用来跳出浏览器的 device-flow 元数据。
// 字段对应 hub /v1/oauth/device/authorize 的响应。
type StartLoginResult struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	Interval                int
	ExpiresIn               int
}

// Device 与 hub /v1/devices 的 ListDevicesItem 对应（仅保留桌面端需要的字段）。
type Device struct {
	ID           int64
	Name         string
	Kind         string
	Platform     string
	Version      string
	Fingerprint  string
	Capabilities map[string]bool
	LastSeenAt   int64
	Status       int
	IsThisDevice bool
}

// server_svc 内部用的语义错误。Wails 绑定层会按错误把它们映射到 i18n.NewError。
var (
	ErrAlreadyInProgress = errors.New("server: login already in progress")
	ErrNotLoggedIn       = errors.New("server: not logged in")
	ErrServerUnreachable = errors.New("server: unreachable")
	ErrAccessDenied      = errors.New("server: access denied")
	ErrLoginExpired      = errors.New("server: device code expired")
	ErrRefreshFailed     = errors.New("server: refresh failed")
)
