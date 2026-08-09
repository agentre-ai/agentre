package sync_svc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
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

// avatarUploaded 记下本进程已经成功推给对端的头像哈希（R16a：「接收方尚未持有该
// 哈希时才单独取一次那份正文」）。同一份内容只推一次——改个 Agent 名字就把几 MB
// 的正文重发一遍，正是决策 28 把正文挪出载荷要避免的那个代价。
//
// 只活在进程内：重启后每份头像最多多推一次，而 server 侧按哈希幂等落库，代价是
// 一次冗余上传、不会写坏任何东西。为它建一张持久表换不来什么。
type avatarUploaded struct {
	mu     sync.Mutex
	hashes map[string]bool
}

// mark 记下这个哈希，并报告它之前是否已经推过。
func (u *avatarUploaded) mark(hash string) (already bool) {
	if u == nil || hash == "" {
		return false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.hashes[hash] {
		return true
	}
	if u.hashes == nil {
		u.hashes = map[string]bool{}
	}
	u.hashes[hash] = true
	return false
}

// forget 把一个哈希从「已推过」里去掉：上传失败时调用，下一轮才会再试。
func (u *avatarUploaded) forget(hash string) {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.hashes, hash)
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
