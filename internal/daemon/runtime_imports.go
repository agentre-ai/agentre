package daemon

// 这里的 blank imports 触发各 backend runtime 子包的 init(),让它们
// 把自己 RegisterRuntime 到 agentruntime 注册表里。务必保留此文件 ——
// 没有这些 import,daemon.handlers.RuntimeHandlers.Run 时
// agentruntime.RuntimeFor(bt) 会返 nil 报 "backend %q not registered"。
//
// TypeBuiltin 实际在 daemon 端被拒绝(builtin 只跑在 agentre 桌面端
// 进程内),这里 import 它仅为对称,确保 RegisteredRuntimes() 能枚举
// 完整集合(将来 capability 协商可能用到)。
import (
	_ "agentre/internal/pkg/agentruntime/runtimes/builtin"
	_ "agentre/internal/pkg/agentruntime/runtimes/claudecode"
	_ "agentre/internal/pkg/agentruntime/runtimes/codex"
)
