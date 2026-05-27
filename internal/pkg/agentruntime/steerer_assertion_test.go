package agentruntime_test

import (
	"testing"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/pkg/agentruntime"

	// 触发 NEW runtime 子包 init() 注册到 RuntimeFor 表
	_ "agentre/internal/pkg/agentruntime/runtimes/builtin"
	_ "agentre/internal/pkg/agentruntime/runtimes/claudecode"
	_ "agentre/internal/pkg/agentruntime/runtimes/codex"
)

// TestAllRegisteredRunnersImplementSteerer 守护契约:每个 backend type 注册的
// runner 都必须实现 Steerer。chat_svc.Enqueue 走 type-assertion,缺一个就会
// 让该后端整个回退到 ChatSteerUnsupported。
func TestAllRegisteredRunnersImplementSteerer(t *testing.T) {
	for _, bt := range []agent_backend_entity.BackendType{
		agent_backend_entity.TypeBuiltin,
		agent_backend_entity.TypeClaudeCode,
		agent_backend_entity.TypeCodex,
	} {
		r := agentruntime.RuntimeFor(bt)
		if r == nil {
			t.Errorf("backend %q has no registered runner", bt)
			continue
		}
		if _, ok := r.(agentruntime.Steerer); !ok {
			t.Errorf("backend %q runner does not implement Steerer", bt)
		}
	}
}
