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
	ID          int64
	Name        string
	Kind        string
	Platform    string
	Version     string
	Fingerprint string
	LastSeenAt  int64
	Status      int
	// Online 是 daemon 在 server 上的中继在线登记(R20),即中转路径此刻是否可达。
	// 与 Status(账号侧授权标志)不是一回事:设备面板按 R15 呈现「可达路径」用的是它。
	Online       bool
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
