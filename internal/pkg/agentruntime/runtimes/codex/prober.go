package codex

// DefaultBinary codex runtime 默认要求的 CLI 二进制名(发布渠道:OpenAI Codex
// CLI)。agent_backend_svc 启动时用这个名字探针 PATH 是否可达。
func DefaultBinary() string { return "codex" }
