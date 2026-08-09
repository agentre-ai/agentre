package skill_svc

import (
	"context"
	"strings"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
)

// Service 技能包组合服务。依赖通过消费者侧窄接口注入(DIP)。
type Service struct {
	agent      AgentLookup
	backend    BackendLookup
	execTarget ExecTargetLookup // 技能授权挂在执行目标行上(R15e),不再挂在 Agent 行
	remote     RemoteDiscoverer // 远端 backend 走 daemon 发现;本地 backend 不用
}

type discoveryResult struct {
	backendType agent_backend_entity.BackendType
	backend     *agent_backend_entity.AgentBackend
	packs       []agentskill.SkillPack
}

// discover 拿该 agent backend 的已安装包(无发现器的 backend 为空)。
func (s *Service) discover(ctx context.Context, a *agent_entity.Agent) (discoveryResult, error) {
	be, err := s.backend.Find(ctx, a.AgentBackendID)
	if err != nil || be == nil {
		return discoveryResult{}, err
	}
	backendType := agent_backend_entity.BackendType(be.Type)
	// 远端 backend:技能包装在 daemon 那台机器上,desktop 本地的 claude plugin list
	// 看不到。经 RemoteDiscoverer 走 daemon skills.list 发现(借 device 连接池)。
	if be.IsRemote() {
		deviceID, ok := be.DeviceIDInt()
		if !ok || s.remote == nil {
			return discoveryResult{backendType: backendType, backend: be, packs: []agentskill.SkillPack{}}, nil
		}
		packs, err := s.remote.ListSkills(ctx, deviceID, be.Type)
		if err != nil {
			return discoveryResult{}, err
		}
		if packs == nil {
			packs = []agentskill.SkillPack{}
		}
		return discoveryResult{backendType: backendType, backend: be, packs: packs}, nil
	}
	d, ok := agentskill.DiscovererFor(backendType)
	if !ok {
		return discoveryResult{backendType: backendType, backend: be, packs: []agentskill.SkillPack{}}, nil
	}
	packs, err := d.Discover(ctx, agentskill.DiscoverQuery{
		BackendType: backendType,
		CLIPath:     be.CLIPath,
	})
	return discoveryResult{backendType: backendType, backend: be, packs: packs}, err
}

// authorizedSkills 取 agentID 那一档(sort_order 最小的一档)的技能授权。存放位置
// 已从 agents.skills_json 下沉到 agent_exec_targets(R15e),这里不再读 Agent 行 ——
// 也不做跨档并集:agentID 有几档就有几份互不相干的授权,这里只取最靠前那一档的。
func (s *Service) authorizedSkills(ctx context.Context, agentID int64) ([]agent_entity.AgentSkillItem, error) {
	targets, err := s.execTarget.ListByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 || targets[0] == nil {
		return nil, nil
	}
	return targets[0].GetSkills(), nil
}

// mergeResult 合并后的包列表及对应的 enabled 标注。
type mergeResult struct {
	packs            []agentskill.SkillPack
	enabled          []bool
	effectiveEnabled []bool
}

// merge 推荐 + 发现 按 id 去重,标注 enabled。
// installed 先入,recommended 后 OR 入 Recommended 旗标。
func merge(recommended, installed []agentskill.SkillPack, overrides []agent_entity.AgentSkillItem) mergeResult {
	overrideByID := map[string]bool{}
	for _, override := range overrides {
		overrideByID[override.ID] = override.Enabled
	}
	type entry struct {
		pack agentskill.SkillPack
		idx  int
	}
	byID := map[string]*entry{}
	order := []string{}

	add := func(p agentskill.SkillPack) {
		if ex, ok := byID[p.ID]; ok {
			if p.Recommended {
				ex.pack.Recommended = true
			}
			if p.Installed {
				ex.pack.Installed = true
				ex.pack.Source = agentskill.SourceInstalled
			}
			return
		}
		idx := len(order)
		cp := p
		byID[cp.ID] = &entry{pack: cp, idx: idx}
		order = append(order, cp.ID)
	}

	for _, p := range installed {
		add(p)
	}
	for _, p := range recommended {
		add(p)
	}

	packs := make([]agentskill.SkillPack, len(order))
	enabledFlags := make([]bool, len(order))
	effectiveFlags := make([]bool, len(order))
	for _, id := range order {
		e := byID[id]
		packs[e.idx] = e.pack
		override, overridden := overrideByID[id]
		enabledFlags[e.idx] = overridden && override
		effectiveFlags[e.idx] = e.pack.Installed && e.pack.GloballyEnabled
		if overridden {
			effectiveFlags[e.idx] = e.pack.Installed && override
		}
	}
	return mergeResult{
		packs:            packs,
		enabled:          enabledFlags,
		effectiveEnabled: effectiveFlags,
	}
}

// ListAgentSkillPacks 合并推荐 + 发现 + agent 授权,产出目录。refresh 预留(未来强制重发现),当前忽略。
func (s *Service) ListAgentSkillPacks(ctx context.Context, agentID int64, _ bool) (SkillCatalogDTO, error) {
	a, err := s.agent.Find(ctx, agentID)
	if err != nil || a == nil {
		return SkillCatalogDTO{}, err
	}
	discovered, err := s.discover(ctx, a)
	if err != nil {
		return SkillCatalogDTO{}, err
	}
	authorized, err := s.authorizedSkills(ctx, agentID)
	if err != nil {
		return SkillCatalogDTO{}, err
	}
	mr := merge(agentskill.RecommendedFor(discovered.backendType), discovered.packs, authorized)
	dto := make([]SkillPackDTO, 0, len(mr.packs))
	for i, p := range mr.packs {
		dto = append(dto, SkillPackDTO{
			ID:               p.ID,
			Name:             p.Name,
			Description:      p.Description,
			Skills:           p.Skills,
			Source:           string(p.Source),
			Recommended:      p.Recommended,
			Installed:        p.Installed,
			Enabled:          mr.enabled[i],
			GloballyEnabled:  p.GloballyEnabled,
			EffectiveEnabled: mr.effectiveEnabled[i],
		})
	}
	return SkillCatalogDTO{Packs: dto}, nil
}

// ListAgentSkillCommands 返回当前 agent 在 cwd 中可调用的 Skill 命令。
// 已安装 plugin 的生效态由目录合并结果决定；本地 backend 再合并 CLI 自己解析的
// user/project/system Skill。远端 backend 当前由 daemon 的 plugin 目录提供命令。
func (s *Service) ListAgentSkillCommands(ctx context.Context, agentID int64, cwd string) (SkillCommandCatalogDTO, error) {
	a, err := s.agent.Find(ctx, agentID)
	if err != nil || a == nil {
		return SkillCommandCatalogDTO{}, err
	}
	discovered, err := s.discover(ctx, a)
	if err != nil {
		return SkillCommandCatalogDTO{}, err
	}
	authorized, err := s.authorizedSkills(ctx, agentID)
	if err != nil {
		return SkillCommandCatalogDTO{}, err
	}

	mr := merge(agentskill.RecommendedFor(discovered.backendType), discovered.packs, authorized)
	commands := make([]SkillCommandDTO, 0)
	seen := map[string]struct{}{}
	appendCommand := func(name, description string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		commands = append(commands, SkillCommandDTO{
			Name:        name,
			Description: strings.TrimSpace(description),
		})
	}

	for i, pack := range mr.packs {
		if !mr.effectiveEnabled[i] {
			continue
		}
		for _, rawSkill := range pack.Skills {
			skill := strings.TrimSpace(rawSkill)
			if skill == "" {
				continue
			}
			name := skill
			if !strings.Contains(skill, ":") && strings.TrimSpace(pack.Name) != "" {
				name = strings.TrimSpace(pack.Name) + ":" + skill
			}
			appendCommand(name, pack.Description)
		}
	}

	if discovered.backend != nil && discovered.backend.IsLocal() {
		if commandDiscoverer, ok := agentskill.CommandDiscovererFor(discovered.backendType); ok {
			native, err := commandDiscoverer.DiscoverCommands(ctx, agentskill.CommandDiscoverQuery{
				BackendType:    discovered.backendType,
				CLIPath:        discovered.backend.CLIPath,
				Cwd:            strings.TrimSpace(cwd),
				EnabledPlugins: enabledPlugins(authorized),
			})
			if err != nil {
				return SkillCommandCatalogDTO{}, err
			}
			for _, command := range native {
				appendCommand(command.Name, command.Description)
			}
		}
	}

	return SkillCommandCatalogDTO{Commands: commands}, nil
}

func enabledPlugins(items []agent_entity.AgentSkillItem) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item.ID] = item.Enabled
	}
	return out
}

// EnabledPluginsMap 返回该 agent 的显式覆盖(强制开=true / 强制关=false)。
// 其余(含全局已开但未覆盖)不出现在 map → CLI 沿用全局 enabledPlugins,实现继承。
func (s *Service) EnabledPluginsMap(ctx context.Context, agentID int64) (map[string]bool, error) {
	a, err := s.agent.Find(ctx, agentID)
	if err != nil || a == nil {
		return nil, err
	}
	authorized, err := s.authorizedSkills(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return enabledPlugins(authorized), nil
}
