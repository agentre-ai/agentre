package claudecode

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/pkg/claudecode"
)

// TestCCBuildClientOpts_ProviderModelDownToCLI 锁住 Bug 1 修复:
// 绑了 LLM provider 的 claudecode 后端必须把 provider.Model 通过 WithModel
// 透到 *claudecode.Client,后续 OpenSession 装配 argv 才会带 --model。
// 不传时 CLI 用本地默认模型名报 system.init.model,result.Model 会把
// chat_svc 创建消息时写好的 prov.Model 占位值覆盖成 CLI 默认,前端展示错。
func TestCCBuildClientOpts_ProviderModelDownToCLI(t *testing.T) {
	Convey("provider.Model 非空 → Client.Model() = provider.Model", t, func() {
		spec := ccLaunchSpec{
			Req: agentruntime.RunRequest{
				Backend:  &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)},
				Provider: &llm_provider_entity.LLMProvider{Model: "glm-5.1"},
			},
		}
		c := claudecode.New(ccBuildClientOpts(spec, "claude")...)
		So(c.Model(), ShouldEqual, "glm-5.1")
	})

	Convey("provider = nil(CLI 登录模式) → Client.Model() 留空", t, func() {
		spec := ccLaunchSpec{
			Req: agentruntime.RunRequest{
				Backend: &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)},
				// Provider nil:用户没绑 LLM provider,CLI 自身 OAuth 走 anthropic.com。
			},
		}
		c := claudecode.New(ccBuildClientOpts(spec, "claude")...)
		So(c.Model(), ShouldEqual, "")
	})

	Convey("provider 非空但 Model 空(罕见配置) → 不下发 --model,留给 CLI 默认", t, func() {
		spec := ccLaunchSpec{
			Req: agentruntime.RunRequest{
				Backend:  &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)},
				Provider: &llm_provider_entity.LLMProvider{Model: "   "}, // 只有空白
			},
		}
		c := claudecode.New(ccBuildClientOpts(spec, "claude")...)
		// strings.TrimSpace 之后空串不应当下发 WithModel,留给 CLI 走默认。
		So(c.Model(), ShouldEqual, "")
	})
}

// TestResolveLaunchMode_BypassDefaultLocksLaunchToBypass 锁住"backend admin 配
// bypass 时 CLI 永远以 bypass 启动"的不变量。stored mode(perTurn)可以是 plan /
// acceptEdits / default 任意值, launch 永远 bypass —— 这是「先 plan 后 bypass」
// 工作流的承重柱:permission_mode_at_launch 必须 == bypass, 才能让 bypass-lockout
// 解锁 + PlanApproveCard 主按钮走 Bypass 分支。
//
// 其它 backend default 走原来的"perTurn → backendDefault → ”"优先级。
func TestResolveLaunchMode_BypassDefaultLocksLaunchToBypass(t *testing.T) {
	Convey("Given backendDefault=bypassPermissions", t, func() {
		Convey("When perTurn=plan, Then launch=bypassPermissions", func() {
			So(resolveLaunchMode("plan", "bypassPermissions"), ShouldEqual, "bypassPermissions")
		})
		Convey("When perTurn=acceptEdits, Then launch=bypassPermissions", func() {
			So(resolveLaunchMode("acceptEdits", "bypassPermissions"), ShouldEqual, "bypassPermissions")
		})
		Convey("When perTurn=default, Then launch=bypassPermissions", func() {
			So(resolveLaunchMode("default", "bypassPermissions"), ShouldEqual, "bypassPermissions")
		})
		Convey("When perTurn 空, Then launch=bypassPermissions", func() {
			So(resolveLaunchMode("", "bypassPermissions"), ShouldEqual, "bypassPermissions")
		})
	})

	Convey("Given backendDefault != bypassPermissions, 原有 perTurn → default 优先级不变", t, func() {
		Convey("backendDefault=acceptEdits + perTurn=plan → plan(perTurn 优先)", func() {
			So(resolveLaunchMode("plan", "acceptEdits"), ShouldEqual, "plan")
		})
		Convey("backendDefault=acceptEdits + perTurn='' → acceptEdits(回落 default)", func() {
			So(resolveLaunchMode("", "acceptEdits"), ShouldEqual, "acceptEdits")
		})
		Convey("backendDefault='' + perTurn='' → ''(走 pkg/claudecode 兜底)", func() {
			So(resolveLaunchMode("", ""), ShouldEqual, "")
		})
	})
}
