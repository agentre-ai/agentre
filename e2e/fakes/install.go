//go:build e2e

// Package fakes 提供 e2e 构建(`-tags e2e`)专用的确定性 fake 装配。
// 它和 e2e/ 下的 Playwright 工程同处一个目录树,但单独成包,避免 Go 源码与
// TS/Playwright 工具链在同一目录里混在一起。
package fakes

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	fakert "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/fake"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/service/agent_backend_svc"
	"github.com/agentre-ai/agentre/internal/service/agent_svc"
	"github.com/agentre-ai/agentre/internal/service/department_svc"
)

type codexSkillDiscoverer struct{}

func (codexSkillDiscoverer) Discover(context.Context, agentskill.DiscoverQuery) ([]agentskill.SkillPack, error) {
	return []agentskill.SkillPack{
		{
			ID:              "browser@openai-bundled",
			Name:            "browser",
			Skills:          []string{"browser"},
			Source:          agentskill.SourceInstalled,
			Installed:       true,
			GloballyEnabled: true,
		},
		{
			ID:              "superpowers@openai-curated",
			Name:            "superpowers",
			Skills:          []string{"tdd"},
			Source:          agentskill.SourceInstalled,
			Installed:       true,
			GloballyEnabled: false,
		},
	}, nil
}

// Install 仅在 `-tags e2e` 构建中编译:
//  1. 用确定性 fake 覆盖 claudecode runtime(无子进程/无登录);
//  2. seed 一个本地 claudecode backend 并挂到默认 CEO agent,
//     让前端"建会话→发消息→看回复"无需真实 CLI 即可跑通。
//
// 失败只记日志不 panic:e2e 环境异常应让 Playwright 用例红,而不是让 app 崩。
func Install(ctx context.Context) {
	agentruntime.RegisterRuntime(agent_backend_entity.TypeClaudeCode, fakert.New())
	agentskill.RegisterDiscoverer(agent_backend_entity.TypeCodex, codexSkillDiscoverer{})

	// 幂等:正常每次 e2e run 用全新 AGENTRE_DATA_DIR(临时目录),但 wails dev 热重载
	// 会重启 app 进程,backend 可能已存在 —— 命中则复用,避免重名报错后跳过挂载。
	const backendName = "E2E Local Backend"
	var backendID int64
	if existing, err := agent_backend_repo.AgentBackend().FindByName(ctx, backendName); err != nil {
		logger.Ctx(ctx).Error("e2efakes.Install: lookup backend failed", zap.Error(err))
		return
	} else if existing != nil {
		backendID = existing.ID
	} else {
		resp, err := agent_backend_svc.AgentBackend().Create(ctx, &agent_backend_svc.CreateBackendRequest{
			Type: string(agent_backend_entity.TypeClaudeCode),
			Name: backendName,
		})
		if err != nil {
			logger.Ctx(ctx).Error("e2efakes.Install: create backend failed", zap.Error(err))
			return
		}
		backendID = resp.Item.ID
	}

	ceo, err := agent_repo.Agent().FindSystem(ctx)
	if err != nil {
		logger.Ctx(ctx).Error("e2efakes.Install: find system agent failed", zap.Error(err))
		return
	}
	if ceo == nil {
		logger.Ctx(ctx).Error("e2efakes.Install: system agent not found (migration gap?)")
		return
	}

	if _, err := agent_svc.Agent().Update(ctx, &agent_svc.UpdateAgentRequest{
		ID:             ceo.ID,
		Name:           ceo.Name,
		AgentBackendID: backendID,
		// 开启工具:本 Update 会整体覆写工具数组(丢掉 migration 默认),故所有 e2e 用到
		// 的工具都要在这里显式开。让 CEO 单聊轮注入对应 MCP server:
		//   - workflow → /mcp/workflow/(workflow-tool.spec:建流程审批 + 拉群带流程)
		//   - group_create → /mcp/group/(group-create.spec / workflow-tool.spec 拉群步骤)
		//   - subagent → /mcp/subagent/(subagent-tool.spec:agent_call 委派,无审批)
		//   - org → /mcp/org/(org-tool.spec:org_create_department 写工具审批)
		Tools: []department_svc.AgentToolDTO{
			{Key: agenttool.KeyWorkflow, Enabled: true},
			{Key: agenttool.KeyGroupCreate, Enabled: true},
			{Key: agenttool.KeySubagent, Enabled: true},
			{Key: agenttool.KeyOrg, Enabled: true},
		},
	}); err != nil {
		logger.Ctx(ctx).Error("e2efakes.Install: attach backend to agent failed", zap.Error(err))
		return
	}

	// seed 群聊成员(挂 CEO 汇报线;子 agent 与建群弹窗 eligible 池同口径)。
	// E2E Member 覆盖执行人链路;E2E Reviewer 覆盖验证/审查/动态招募链路。
	for _, memberName := range []string{"E2E Member", "E2E Reviewer"} {
		if existing, err := agent_repo.Agent().FindByName(ctx, memberName); err != nil {
			logger.Ctx(ctx).Error("e2efakes.Install: lookup member agent failed",
				zap.String("name", memberName), zap.Error(err))
			return
		} else if existing == nil {
			if _, err := agent_svc.Agent().Create(ctx, &agent_svc.CreateAgentRequest{
				Name:           memberName,
				ParentAgentID:  ceo.ID,
				AgentBackendID: backendID,
			}); err != nil {
				logger.Ctx(ctx).Error("e2efakes.Install: create member agent failed",
					zap.String("name", memberName), zap.Error(err))
				return
			}
		}
	}

	const codexBackendName = "E2E Codex Backend"
	var codexBackendID int64
	if existing, err := agent_backend_repo.AgentBackend().FindByName(ctx, codexBackendName); err != nil {
		logger.Ctx(ctx).Error("e2efakes.Install: lookup codex backend failed", zap.Error(err))
		return
	} else if existing != nil {
		codexBackendID = existing.ID
	} else {
		resp, err := agent_backend_svc.AgentBackend().Create(ctx, &agent_backend_svc.CreateBackendRequest{
			Type: string(agent_backend_entity.TypeCodex),
			Name: codexBackendName,
		})
		if err != nil {
			logger.Ctx(ctx).Error("e2efakes.Install: create codex backend failed", zap.Error(err))
			return
		}
		codexBackendID = resp.Item.ID
	}

	const codexAgentName = "E2E Codex Agent"
	if existing, err := agent_repo.Agent().FindByName(ctx, codexAgentName); err != nil {
		logger.Ctx(ctx).Error("e2efakes.Install: lookup codex agent failed", zap.Error(err))
		return
	} else if existing == nil {
		if _, err := agent_svc.Agent().Create(ctx, &agent_svc.CreateAgentRequest{
			Name:           codexAgentName,
			ParentAgentID:  ceo.ID,
			AgentBackendID: codexBackendID,
		}); err != nil {
			logger.Ctx(ctx).Error("e2efakes.Install: create codex agent failed", zap.Error(err))
			return
		}
	}

	logger.Ctx(ctx).Info("e2efakes.Install: e2e fakes installed",
		zap.Int64("backendID", backendID), zap.Int64("agentID", ceo.ID))
}
