package claudecode

// DefaultBinary 是 claudecode runtime 默认要求的 CLI 二进制名(发布渠道:
// npm @anthropic-ai/claude-code 或 brew tap)。agent_backend_svc 启动时用这个
// 名字探针 PATH 是否可达,缺失时引导用户安装。
func DefaultBinary() string { return "claude" }
