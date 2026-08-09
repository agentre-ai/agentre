package sync_svc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// avatarTransport 是头像哈希内容存取的窄接口（ISP）：agentAdapter 上下行都要用到，
// 不需要认得 SyncPush / SyncPull / ReportLocalPaths（R16a）。
//
// nil 时（单机模式、或直接构造 agentAdapter{} 的测试）两个方向都优雅退化：
// 上行只带哈希、不传正文；下行直接把 AvatarDataURL 留空，退回 AgentAvatar 的
// initials 占位字母头像。
type avatarTransport interface {
	// PutAvatar 把本机持有的头像正文按内容哈希推给对端；对端已经有这份内容时
	// 幂等（R16a），调用方不需要先查一遍「对端是否已持有」。
	PutAvatar(ctx context.Context, contentHash, contentType, content string) error
	// GetAvatar 取一份尚未持有的头像正文；取不到（未上传过 / 网络失败 / 超时）
	// 时返回 error，调用方据此降级为占位字母头像。
	GetAvatar(ctx context.Context, contentHash string) (content, contentType string, err error)
}

// avatarHash 是头像内容的哈希——同步载荷里只出现它，从不出现 AvatarDataURL 正文
// 本身（R16a、守卫见 syncwire.GuardPayload）。空字符串的哈希是空字符串：没有
// 自定义头像时载荷里的 avatar_hash 就是空，接收端据此清空本地头像。
func avatarHash(dataURL string) string {
	if dataURL == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(dataURL))
	return hex.EncodeToString(sum[:])
}

// avatarContentType 从 "data:image/png;base64,...." 里取出 MIME 类型，仅供
// server 展示用；解析不出时留空，不影响功能（Content 本身就是完整的 data URL）。
func avatarContentType(dataURL string) string {
	const prefix = "data:"
	if !strings.HasPrefix(dataURL, prefix) {
		return ""
	}
	rest := dataURL[len(prefix):]
	if idx := strings.IndexByte(rest, ';'); idx >= 0 {
		return rest[:idx]
	}
	return ""
}
