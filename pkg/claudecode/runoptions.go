package claudecode

// RunOption 单次 Stream 调用的参数。
type RunOption func(*runSpec)

// Resume 用已有 session id 继续；空字符串等价于不传（新建 session）。
func Resume(sid string) RunOption { return func(s *runSpec) { s.resumeID = sid } }

// ResumeSessionAt 把会话定位到指定 message uuid；必须配 ForkSession 一起用，
// 否则会破坏性 rewind 原 session（Client.Stream 启动前校验）。
func ResumeSessionAt(uuid string) RunOption {
	return func(s *runSpec) { s.resumeSessionAtUUID = uuid }
}

// ForkSession 配 Resume / ResumeSessionAt 用：复制 prefix 到一个新 session id,
// 原 session 保持不变。
func ForkSession() RunOption { return func(s *runSpec) { s.forkSession = true } }

// MaxTurns 限制本次 turn 内 agent 自循环次数；prober 路径常设 1 做一次性 ping。
// n ≤ 0 时不传 --max-turns（CLI 默认无上限）。
func MaxTurns(n int) RunOption { return func(s *runSpec) { s.maxTurns = n } }
