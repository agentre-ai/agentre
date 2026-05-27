package agentruntime

import "context"

// Rewinder 是可选能力接口：runner 实现它表示「能把 provider 端的会话
// 回滚到一个由 anchor 标记的位置之前」。chat_svc 对未知后端用类型断言探测：
//
//	if r, ok := runner.(Rewinder); ok { ... }
//
// anchor 是一个由 runner 自己理解的不透明字符串。已知内置后端当前有自己的
// 显式分支：
//   - claudecode: RunRequest.ForkAnchor 是 stream-json 帧里的 message uuid。
//   - codex:      RunRequest.ForkAnchor 是 thread/rollback 的十进制 numTurns。
//   - builtin:    不需要 provider rewind，history 每轮从 DB 重建。
//
// RewindTo 返回 newSessionID；如果实现使用 provider fork，chat_svc 需要把新 id
// 写回 chat_sessions.provider_session_id。
//
// 实现必须保证：调用成功后，紧接着用 `RunRequest{ProviderSessionID: newSessionID, UserText: ...}`
// 发起的下一个 turn，就从 anchor 点之后的位置开始续写。
type Rewinder interface {
	RewindTo(ctx context.Context, sessionID, anchor string) (newSessionID string, err error)
}
